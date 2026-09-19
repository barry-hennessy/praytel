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
	grpcPort     int
	httpPort     int
	drainIdle    time.Duration
	drainTimeout time.Duration
	maxSpans     int
	maxLogLines  int
	focus        []string
	exclude      []string
	tui          bool
	query        string
	queryFile    string
	format       string
	emit         string
	settleAfter  time.Duration
	resultLimit  int
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
	tuiMode := fs.Bool("tui", false, "render an interactive live trace tree")
	queryText := fs.String("query", "", "select logs and traces with a typed query")
	fs.StringVar(queryText, "q", "", "shorthand for --query")
	queryFile := fs.String("query-file", "", "read a typed query from a file")
	format := fs.String("format", "text", "query output format: text, json, or ndjson")
	emit := fs.String("emit", "final", "query lifecycle to emit: final or settled")
	settleAfter := fs.Duration("settle-after", 250*time.Millisecond, "quiet period before a closed trace is settled")
	resultLimit := fs.Int("limit", 100, "maximum query results per output envelope")
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
	if *queryText != "" && *queryFile != "" {
		return config{}, errors.New("--query and --query-file are mutually exclusive")
	}
	if *format != "text" && *format != "json" && *format != "ndjson" {
		return config{}, fmt.Errorf("invalid --format %q", *format)
	}
	if *emit != "final" && *emit != "settled" {
		return config{}, fmt.Errorf("invalid --emit %q", *emit)
	}
	if *emit == "settled" && *format != "ndjson" {
		return config{}, errors.New("--emit settled requires --format ndjson")
	}
	if (*format != "text" || *emit != "final") && *queryText == "" && *queryFile == "" {
		return config{}, errors.New("--format and --emit require --query or --query-file")
	}
	if *tuiMode && (*format != "text" || *emit != "final") {
		return config{}, errors.New("--format and --emit are not used with --tui")
	}
	if (*queryText != "" || *queryFile != "") && (len(focus) > 0 || len(exclude) > 0) {
		return config{}, errors.New("--query cannot be combined with --focus or --exclude")
	}
	if *resultLimit <= 0 {
		return config{}, errors.New("--limit must be positive")
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
		grpcPort:     *grpcPort,
		httpPort:     *httpPort,
		drainIdle:    idle,
		drainTimeout: hard,
		maxSpans:     *maxSpans,
		maxLogLines:  *maxLogLines,
		focus:        append([]string(nil), focus...),
		exclude:      append([]string(nil), exclude...),
		tui:          *tuiMode,
		query:        *queryText,
		queryFile:    *queryFile,
		format:       *format,
		emit:         *emit,
		settleAfter:  max(time.Duration(0), *settleAfter),
		resultLimit:  *resultLimit,
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
