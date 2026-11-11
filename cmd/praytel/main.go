package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

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

		_, log, span := lt.Name("main.newErrMapper").Start(context.Background())
		defer span.End()
		log.Error("praytel failed", "err", err)

		return 1
	}
}

func fmtUsage(w io.Writer) {
	_, _ = io.WriteString(w, args.ErrUsage.Error()+"\n")
}

func runProcess(ctx context.Context, lt observability.LoggingTracer, mapErr bones.ErrMapper[int], argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := args.Parse(argv, stderr)
	if err != nil {
		return mapErr(err)
	}

	// A plan error names a script's file, line and column; print it as is.
	pl, err := cfg.Plan()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "praytel: plan: %v\n", err)

		return 1
	}

	if cfg.PrintPlan {
		_, _ = stdout.Write(pl.Explain())

		return 0
	}

	code, err := run(ctx, lt, cfg, pl, stdin, stdout, stderr)
	if err != nil {
		return mapErr(err)
	}

	return code
}
