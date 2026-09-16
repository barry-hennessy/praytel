package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/barry-hennessy/praytel/internal/correlator"
	"github.com/barry-hennessy/praytel/internal/receiver"
)

var (
	ErrUsage = errors.New("usage: praytel [flags] -- command [args ...]")
)

type config struct {
	command      []string
	tree         bool
	grpcPort     int
	httpPort     int
	drainIdle    time.Duration
	drainTimeout time.Duration
	maxSpans     int
	maxLogLines  int
	focus        []string
	exclude      []string
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(v string) error {
	*s = append(*s, v)

	return nil
}

func parseArgs(args []string, errOut io.Writer) (config, error) {
	flags, command, ok := splitDashDash(args)
	if !ok {
		return config{}, ErrUsage
	}

	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return config{}, ErrUsage
	}

	fs := flag.NewFlagSet("praytel", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintln(errOut, ErrUsage.Error())
	}

	grpcPort := fs.Int("grpc-port", receiver.DefaultGRPCPort, "OTLP gRPC listen port")
	httpPort := fs.Int("http-port", receiver.DefaultHTTPPort, "OTLP HTTP listen port")
	drainIdle := fs.Duration("drain-idle", 250*time.Millisecond, "idle time with no OTLP after child exit before drain completes")
	drainTimeout := fs.Duration("drain-timeout", 2*time.Second, "hard cap on drain after child exit")
	maxSpans := fs.Int("max-spans", correlator.DefaultMaxSpans, "max correlator-retained spans including pending and orphans")
	maxLogLines := fs.Int("max-log-lines", correlator.DefaultMaxLogLines, "max correlator-retained log lines including pending and orphans")
	tree := fs.Bool("tree", false, "render an indented call-graph tree instead of the append-only stream")
	var focus stringList
	var exclude stringList
	fs.Var(&focus, "focus", "admit the span path matching this glob (repeatable, union)")
	fs.Var(&exclude, "exclude", "drop matching names after focus expansion (repeatable)")

	if err := fs.Parse(flags); err != nil {
		return config{}, err
	}

	if fs.NArg() != 0 {
		return config{}, ErrUsage
	}

	idle := *drainIdle
	if idle < 0 {
		idle = 0
	}
	hard := *drainTimeout
	if hard < 0 {
		hard = 0
	}

	return config{
		command:      command,
		tree:         *tree,
		grpcPort:     *grpcPort,
		httpPort:     *httpPort,
		drainIdle:    idle,
		drainTimeout: hard,
		maxSpans:     *maxSpans,
		maxLogLines:  *maxLogLines,
		focus:        append([]string(nil), focus...),
		exclude:      append([]string(nil), exclude...),
	}, nil
}

func splitDashDash(args []string) (flags, command []string, ok bool) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:], true
		}
	}

	return nil, nil, false
}
