// dlvspans runs a binary under `dlv trace` and turns each traced call/return
// into an OpenTelemetry span, exported via the standard OTEL_EXPORTER_OTLP_* env.
//
//	dlvspans [-match regexp] ./binary [args...]
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	callRe = regexp.MustCompile(`^> goroutine\((\d+)\): ([^(]+)\((.*)\)$`)
	retRe  = regexp.MustCompile(`^>> goroutine\((\d+)\): (\S+) => \((.*)\)$`)
)

type frame struct {
	name string
	ctx  context.Context
	span trace.Span
}

func main() {
	match := flag.String("match", `main\.`, "regexp of functions to trace")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: dlvspans [-match re] ./binary [args...]")
		os.Exit(2)
	}
	bin := flag.Arg(0)
	ctx := context.Background()

	exp, err := otlptracegrpc.New(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewSchemaless(semconv.ServiceName(filepath.Base(bin)))),
	)
	tr := tp.Tracer("dlvspans")

	args := []string{"trace", "--exec", bin, *match}
	if flag.NArg() > 1 {
		args = append(append(args, "--"), flag.Args()[1:]...)
	}
	cmd := exec.Command("dlv", args...)
	cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
	stderr, _ := cmd.StderrPipe()

	rootCtx, root := tr.Start(ctx, "run "+filepath.Base(bin),
		trace.WithAttributes(attribute.StringSlice("process.command_args", flag.Args())))
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	stacks := map[string][]frame{} // per goroutine
	gctx := map[string]context.Context{}
	var gspans []trace.Span
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if m := callRe.FindStringSubmatch(line); m != nil {
			g, fn := m[1], m[2]
			parent := rootCtx
			if st := stacks[g]; len(st) > 0 {
				parent = st[len(st)-1].ctx
			} else if c, ok := gctx[g]; ok {
				parent = c
			} else {
				c, gs := tr.Start(rootCtx, "goroutine "+g)
				gspans = append(gspans, gs)
				gctx[g], parent = c, c
			}
			c, s := tr.Start(parent, fn, trace.WithAttributes(
				attribute.String("code.function", fn),
				attribute.String("go.args", m[3]),
				attribute.String("go.goroutine", g)))
			stacks[g] = append(stacks[g], frame{fn, c, s})
			continue
		}
		if m := retRe.FindStringSubmatch(line); m != nil {
			g, fn := m[1], m[2]
			st := stacks[g]
			// pop to the matching frame (panics/unrecovered frames may skip returns)
			for i := len(st) - 1; i >= 0; i-- {
				if st[i].name == fn {
					for _, f := range st[i+1:] {
						f.span.End()
					}
					st[i].span.SetAttributes(attribute.String("go.return", m[3]))
					st[i].span.End()
					stacks[g] = st[:i]
					break
				}
			}
			continue
		}
		if !strings.HasPrefix(line, "Process ") {
			fmt.Fprintln(os.Stderr, line) // target's own stderr / dlv messages
		}
	}
	werr := cmd.Wait()
	for _, st := range stacks {
		for i := len(st) - 1; i >= 0; i-- {
			st[i].span.End()
		}
	}
	for _, gs := range gspans {
		gs.End()
	}
	root.End()
	_ = tp.Shutdown(ctx)
	if werr != nil {
		os.Exit(1)
	}
}
