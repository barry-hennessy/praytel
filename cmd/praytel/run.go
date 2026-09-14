package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/capture"
	"github.com/barry-hennessy/praytel/internal/correlator"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/receiver"
	"github.com/barry-hennessy/praytel/internal/render/stream"
	"golang.org/x/sync/errgroup"
)

func run(ctx context.Context, lt observability.LoggingTracer, cfg config, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	ctx, log, span := lt.Start(ctx)
	defer span.End()

	rec, err := receiver.Listen(lt, receiver.Ports{
		GRPC: cfg.grpcPort,
		HTTP: cfg.httpPort,
	})
	if err != nil {
		return 1, fmt.Errorf("bind otlp: %w", err)
	}

	events := make(chan domain.Event, 64)
	corr := correlator.New(lt, events)
	st := stream.New(lt, events, stdout)

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

	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()

	gg, gctx := errgroup.WithContext(runCtx)
	gg.Go(func() error {
		return corr.Run(ctx)
	})
	gg.Go(func() error {
		return st.Run(ctx)
	})
	gg.Go(func() error {
		return rec.Serve(gctx)
	})

	var capWG sync.WaitGroup
	capWG.Add(2)
	go func() {
		defer capWG.Done()
		defer outR.Close()
		_ = capture.Copy(ctx, lt, domain.SourceStdout, outR, corr.Logs(), gate)
	}()
	go func() {
		defer capWG.Done()
		defer errR.Close()
		_ = capture.Copy(ctx, lt, domain.SourceStderr, errR, corr.Logs(), gate)
	}()

	code, started, err := startChild(cfg.command, rec.ChildEnv(), stdin, outW, errW)

	_ = outW.Close()
	_ = errW.Close()
	capWG.Wait()
	corr.Stop()
	stopRun()

	waitErr := gg.Wait()
	if waitErr != nil {
		log.Error("lifecycle error", "err", waitErr)
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

func startChild(command []string, extraEnv []string, stdin io.Reader, stdout, stderr *os.File) (int, bool, error) {
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
