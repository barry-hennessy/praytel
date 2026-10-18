package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/barry-hennessy/bones"
	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/cmd/praytel/internal/args"
)

func main() {
	ctx, stop := bones.RootCtx()
	defer stop()

	lt := newLoggingTracer(os.Stderr)
	mapErr := newErrMapper(lt, os.Stderr)

	code := runProcess(ctx, lt, mapErr, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	os.Exit(code)
}

func newLoggingTracer(stderr io.Writer) observability.LoggingTracer {
	log := slog.New(slog.NewTextHandler(stderr, nil))

	return observability.LoggingTracer{
		Log:    *log,
		Tracer: nil,
	}.Name("praytel")
}

func newErrMapper(lt observability.LoggingTracer, stderr io.Writer) bones.ErrMapper[int] {
	return func(err error) int {
		if err == nil {
			return 0
		}

		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		if errors.Is(err, args.ErrUsage) {
			fmtUsage(stderr)
			return 1
		}

		_, log, span := lt.Start(context.Background())
		defer span.End()
		log.Error("praytel failed", "err", err)

		return 1
	}
}

func fmtUsage(w io.Writer) {
	_, _ = io.WriteString(w, args.ErrUsage.Error()+"\n")
}

func runProcess(ctx context.Context, lt observability.LoggingTracer, mapErr bones.ErrMapper[int], argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(argv) == 1 && argv[0] == "mcp" {
		if err := runMCP(ctx, lt); err != nil {
			return mapErr(err)
		}
		return 0
	}
	if len(argv) > 0 && argv[0] == "agent" {
		if err := runAgentCommand(argv[1:], stdout); err != nil {
			return mapErr(err)
		}
		return 0
	}
	if len(argv) > 0 && argv[0] == "map" {
		return runMapCommand(argv[1:], stdout, stderr)
	}
	if len(argv) > 0 && argv[0] == "query" {
		if err := runQueryCommand(argv[1:], stdout); err != nil {
			return mapErr(err)
		}
		return 0
	}
	cfg, err := args.Parse(argv, stderr)
	if err != nil {
		return mapErr(err)
	}

	if cfg.SelfTrace {
		tracer, shutdown, err := newSelfTracer(ctx, os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
		if err != nil {
			return mapErr(err)
		}
		// Flush before exiting, or the outer praytel drains before the
		// spans arrive.
		defer func() {
			flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = shutdown(flushCtx)
		}()
		lt.Tracer = tracer
	}

	code, err := run(ctx, lt, cfg, stdin, stdout, stderr)
	if err != nil {
		return mapErr(err)
	}

	return code
}
