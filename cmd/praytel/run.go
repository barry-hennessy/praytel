package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/cmd/praytel/internal/args"
	"github.com/barry-hennessy/praytel/internal/capture"
	"github.com/barry-hennessy/praytel/internal/correlator"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/ingest"
	"github.com/barry-hennessy/praytel/internal/receiver"
	"github.com/barry-hennessy/praytel/internal/render/stream"
	"github.com/barry-hennessy/praytel/internal/render/tui"
	"golang.org/x/sync/errgroup"
)

type renderer interface {
	Run(context.Context) error
}

func run(
	ctx context.Context,
	lt observability.LoggingTracer,
	cfg args.Config,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	ctx, log, span := lt.Start(ctx)
	defer span.End()

	pipe, err := ingest.New(lt, correlator.Drain{
		Idle:    cfg.DrainIdle,
		Timeout: cfg.DrainTimeout,
	}, correlator.Limits{
		MaxSpans:    cfg.MaxSpans,
		MaxLogLines: cfg.MaxLogLines,
	}, receiver.Ports{
		GRPC: cfg.GRPCPort,
		HTTP: cfg.HTTPPort,
	})
	if err != nil {
		return 1, fmt.Errorf("bind otlp: %w", err)
	}
	events, corr, rec := pipe.Events, pipe.Corr, pipe.Rec

	var view renderer
	if cfg.TUI {
		view = tui.New(lt, events, stdin, stdout, nil)
	} else {
		st := stream.New(lt, events, stdout, nil)
		view = st
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		return 1, fmt.Errorf("stdout pipe: %w", err)
	}

	errR, errW, err := os.Pipe()
	if err != nil {
		_ = outR.Close()
		_ = outW.Close()

		return 1, fmt.Errorf("stderr pipe: %w", err)
	}

	gate := capture.NewWarnGate(0)

	// SIGINT and SIGTERM are forwarded to the child rather than cancelling
	// anything directly. Keep data collection alive long enough to capture its
	// shutdown output and drain OTLP; a repeated signal forces the issue.
	runCtx, stopRun := context.WithCancel(context.WithoutCancel(ctx))
	defer stopRun()

	renderCtx, stopRender := context.WithCancel(context.WithoutCancel(ctx))
	defer stopRender()

	intr := newInterrupter(stopRender, stopRun)
	stopListen := intr.listen(syscall.SIGINT, syscall.SIGTERM)
	defer stopListen()

	var corrGroup errgroup.Group
	corrGroup.Go(func() error {
		return corr.Run(runCtx)
	})

	renderDone := make(chan struct{})
	var renderGroup errgroup.Group
	renderGroup.Go(func() error {
		defer close(renderDone)
		return view.Run(renderCtx)
	})
	go func() {
		// Quitting the TUI while the child still runs must stop the child,
		// or praytel waits on it with nothing left on screen.
		<-renderDone
		intr.quit()
	}()

	var receiverGroup errgroup.Group
	receiverGroup.Go(func() error {
		return rec.Serve(runCtx)
	})

	var capWG sync.WaitGroup
	capWG.Add(2)
	go func() {
		defer capWG.Done()
		defer outR.Close()
		_ = capture.Copy(
			runCtx,
			lt,
			domain.SourceStdout,
			outR,
			corr.Logs(),
			gate,
		)
	}()
	go func() {
		defer capWG.Done()
		defer errR.Close()
		_ = capture.Copy(
			runCtx,
			lt,
			domain.SourceStderr,
			errR,
			corr.Logs(),
			gate,
		)
	}()

	childIn := stdin
	if cfg.TUI {
		// The terminal belongs to Bubble Tea in interactive mode.
		childIn = nil
	}
	code, started, err := startChild(
		cfg.Command,
		rec.ChildEnv(),
		childIn,
		outW,
		errW,
		intr,
	)

	_ = outW.Close()
	_ = errW.Close()
	waitCapture(log, &capWG, max(cfg.DrainTimeout, minCaptureHold), intr.forced(), outR, errR)
	corr.Stop()
	corrErr := corrGroup.Wait()
	rec.Drop()

	// Keep the listener in drop mode briefly so a late export hits warn
	// instead of a closed port (issue 07). Drain idle/hard-cap already ran.
	hold := 50 * time.Millisecond
	if cfg.DrainTimeout > 0 && hold > cfg.DrainTimeout {
		hold = cfg.DrainTimeout
	}
	holdTimer := time.NewTimer(hold)
	<-holdTimer.C
	stopRun()
	receiverErr := receiverGroup.Wait()
	if !started {
		stopRender()
	}
	renderErr := renderGroup.Wait()

	for _, lifecycleErr := range []error{corrErr, receiverErr, renderErr} {
		if lifecycleErr != nil {
			log.Error("lifecycle error", "err", lifecycleErr)
		}
	}

	if !started {
		if err == nil {
			err = errors.New("child did not start")
		}

		return 1, fmt.Errorf("start child: %w", err)
	}

	if err != nil {
		log.Error("wait child", "err", err)
	}

	return code, nil
}

// minCaptureHold is the least time the child's stdout/stderr get to reach EOF
// after it exits, so output still buffered in the pipes is read first.
const minCaptureHold = time.Second

// waitCapture waits for the capture goroutines to see EOF on the child's
// stdout/stderr. A process the child left running in the background can hold
// those pipes open forever, so stop reading after hold, or at once when
// shutdown is forced.
func waitCapture(log *slog.Logger, wg *sync.WaitGroup, hold time.Duration, force <-chan struct{}, pipes ...*os.File) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(hold)
	defer timer.Stop()

	select {
	case <-done:
		return
	case <-timer.C:
		log.Warn("child exited but its stdout/stderr is still open, likely held by a background process; no longer capturing it", "after", hold)
	case <-force:
	}

	for _, p := range pipes {
		_ = p.SetReadDeadline(time.Now())
	}
	<-done
}

func startChild(
	command []string,
	extraEnv []string,
	stdin io.Reader,
	stdout *os.File,
	stderr *os.File,
	intr *interrupter,
) (int, bool, error) {
	cmd := exec.Command(command[0], command[1:]...) //nolint:gosec // running the command praytel was given is its job
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = withOTelExporterEnv(os.Environ(), extraEnv)

	if err := cmd.Start(); err != nil {
		return 1, false, err
	}

	_ = stdout.Close()
	_ = stderr.Close()

	intr.setChild(cmd.Process)

	err := cmd.Wait()
	if err == nil {
		return 0, true, nil
	}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return signalExitCode(ws.Signal()), true, nil
		}

		return ee.ExitCode(), true, nil
	}

	return 1, true, err
}

// interrupter escalates repeated SIGINT/SIGTERM so praytel can always be
// stopped. The first is forwarded to the child for a graceful shutdown with a
// full drain. The second kills the child and stops waiting on its output and
// OTLP. Any further one exits praytel at once.
type interrupter struct {
	stopRender func()
	stopRun    func()
	exit       func(code int)

	mu    sync.Mutex
	count int
	first os.Signal
	proc  *os.Process
	force chan struct{}
}

func newInterrupter(stopRender, stopRun func()) *interrupter {
	return &interrupter{
		stopRender: stopRender,
		stopRun:    stopRun,
		exit:       os.Exit,
		force:      make(chan struct{}),
	}
}

// listen feeds sigs received by praytel to the interrupter until the returned
// stop function is called.
func (i *interrupter) listen(sigs ...os.Signal) func() {
	ch := make(chan os.Signal, len(sigs))
	signal.Notify(ch, sigs...)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				i.interrupt(sig)
			case <-done:
				return
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(ch)
			close(done)
		})
	}
}

func (i *interrupter) interrupt(sig os.Signal) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.count++
	switch i.count {
	case 1:
		i.first = sig
		i.stopRender()
		if i.proc != nil {
			_ = i.proc.Signal(sig)
		}
	case 2:
		if i.proc != nil {
			_ = i.proc.Kill()
		}
		i.stopRun()
		close(i.force)
	default:
		code := 1
		if s, ok := sig.(syscall.Signal); ok {
			code = signalExitCode(s)
		}
		i.exit(code)
	}
}

// quit stops the child when the user leaves the view, unless a signal is
// already doing so.
func (i *interrupter) quit() {
	i.mu.Lock()
	count := i.count
	i.mu.Unlock()

	if count == 0 {
		i.interrupt(syscall.SIGINT)
	}
}

// setChild records the started child and delivers any interrupt that arrived
// before it existed.
func (i *interrupter) setChild(proc *os.Process) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.proc = proc
	switch {
	case i.count == 1:
		_ = proc.Signal(i.first)
	case i.count >= 2:
		_ = proc.Kill()
	}
}

func (i *interrupter) forced() <-chan struct{} {
	return i.force
}

// signalExitCode is the shell convention for a process ended by sig.
func signalExitCode(sig syscall.Signal) int {
	return 128 + int(sig)
}

func withOTelExporterEnv(base, extra []string) []string {
	out := make([]string, 0, len(base)+len(extra))
	for _, e := range base {
		if strings.HasPrefix(e, "OTEL_EXPORTER_OTLP_") {
			continue
		}

		out = append(out, e)
	}

	return append(out, extra...)
}
