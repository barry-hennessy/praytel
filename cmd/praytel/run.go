package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/barry-hennessy/praytel/internal/filter"
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

	filt, err := filter.New(filter.Query{Focus: cfg.Focus, Exclude: cfg.Exclude})
	if err != nil {
		return 1, fmt.Errorf("filter: %w", err)
	}

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
		view = tui.New(lt, events, stdin, stdout, filt)
	} else {
		st := stream.New(lt, events, stdout, filt)
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

	// SIGINT and SIGTERM cancel the root context while they are forwarded to
	// the child. Keep data collection alive long enough to capture its shutdown
	// output and drain OTLP. The renderer still receives the root context.
	runCtx, stopRun := context.WithCancel(context.WithoutCancel(ctx))
	defer stopRun()

	var corrGroup errgroup.Group
	corrGroup.Go(func() error {
		return corr.Run(runCtx)
	})

	renderCtx, stopRender := context.WithCancel(ctx)
	defer stopRender()
	var renderGroup errgroup.Group
	renderGroup.Go(func() error {
		return view.Run(renderCtx)
	})

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
	)

	_ = outW.Close()
	_ = errW.Close()
	capWG.Wait()
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

func startChild(
	command []string,
	extraEnv []string,
	stdin io.Reader,
	stdout *os.File,
	stderr *os.File,
) (int, bool, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = withOTelExporterEnv(os.Environ(), extraEnv)

	if err := cmd.Start(); err != nil {
		return 1, false, err
	}

	_ = stdout.Close()
	_ = stderr.Close()

	stopFwd := forwardSignals(cmd.Process, syscall.SIGINT, syscall.SIGTERM)
	defer stopFwd()

	err := cmd.Wait()
	if err == nil {
		return 0, true, nil
	}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true, nil
	}

	return 1, true, err
}

// forwardSignals delivers SIGINT/SIGTERM received by praytel to proc.
// Lifecycle (correlator drain, stream, OTLP listener) uses a context that is
// not cancelled by those signals, so post-child idle drain still runs.
func forwardSignals(proc *os.Process, sigs ...os.Signal) func() {
	if proc == nil || len(sigs) == 0 {
		return func() {}
	}

	ch := make(chan os.Signal, len(sigs))
	signal.Notify(ch, sigs...)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				_ = proc.Signal(sig)
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
