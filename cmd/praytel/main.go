package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"

	"github.com/barry-hennessy/bones"
	"github.com/barry-hennessy/bones/observability"
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

		if errors.Is(err, ErrUsage) {
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
	_, _ = io.WriteString(w, ErrUsage.Error()+"\n")
}

func runProcess(ctx context.Context, lt observability.LoggingTracer, mapErr bones.ErrMapper[int], args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "mcp" {
		if err := runMCP(ctx, lt); err != nil {
			return mapErr(err)
		}
		return 0
	}
	if len(args) > 0 && args[0] == "agent" {
		if err := runAgentCommand(args[1:], stdout); err != nil {
			return mapErr(err)
		}
		return 0
	}
	if len(args) > 0 && args[0] == "query" {
		if err := runQueryCommand(args[1:], stdout); err != nil {
			return mapErr(err)
		}
		return 0
	}
	cfg, err := parseArgs(args, stderr)
	if err != nil {
		return mapErr(err)
	}

	code, err := run(ctx, lt, cfg, stdin, stdout, stderr)
	if err != nil {
		return mapErr(err)
	}

	return code
}
