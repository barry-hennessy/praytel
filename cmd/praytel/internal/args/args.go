// Package args parses praytel's command-line flags into a Config.
package args

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

// ErrUsage is returned for any malformed invocation; callers print the usage
// line and exit non-zero.
var ErrUsage = errors.New("usage: praytel [flags] -- command [args ...]")

// Config is a validated set of flags plus the child command to run.
type Config struct {
	Command      []string
	GRPCPort     int
	HTTPPort     int
	DrainIdle    time.Duration
	DrainTimeout time.Duration
	MaxSpans     int
	MaxLogLines  int
	Focus        []string
	Exclude      []string
	TUI          bool
	Query        string
	QueryFile    string
	Format       string
	Emit         string
	SettleAfter  time.Duration
	ResultLimit  int
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(v string) error {
	*s = append(*s, v)

	return nil
}

// Parse splits args on "--" into flags and a child command, parses the
// flags, and validates their combination.
func Parse(args []string, errOut io.Writer) (Config, error) {
	flags, command, ok := splitDashDash(args)
	if !ok {
		return Config{}, ErrUsage
	}

	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return Config{}, ErrUsage
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
		return Config{}, err
	}

	if fs.NArg() != 0 {
		return Config{}, ErrUsage
	}
	if *queryText != "" && *queryFile != "" {
		return Config{}, errors.New("--query and --query-file are mutually exclusive")
	}
	if *format != "text" && *format != "json" && *format != "ndjson" {
		return Config{}, fmt.Errorf("invalid --format %q", *format)
	}
	if *emit != "final" && *emit != "settled" {
		return Config{}, fmt.Errorf("invalid --emit %q", *emit)
	}
	if *emit == "settled" && *format != "ndjson" {
		return Config{}, errors.New("--emit settled requires --format ndjson")
	}
	if (*format != "text" || *emit != "final") && *queryText == "" && *queryFile == "" {
		return Config{}, errors.New("--format and --emit require --query or --query-file")
	}
	if *tuiMode && (*format != "text" || *emit != "final") {
		return Config{}, errors.New("--format and --emit are not used with --tui")
	}
	if (*queryText != "" || *queryFile != "") && (len(focus) > 0 || len(exclude) > 0) {
		return Config{}, errors.New("--query cannot be combined with --focus or --exclude")
	}
	if *resultLimit <= 0 {
		return Config{}, errors.New("--limit must be positive")
	}

	idle := max(0, *drainIdle)
	hard := max(0, *drainTimeout)

	return Config{
		Command:      command,
		GRPCPort:     *grpcPort,
		HTTPPort:     *httpPort,
		DrainIdle:    idle,
		DrainTimeout: hard,
		MaxSpans:     *maxSpans,
		MaxLogLines:  *maxLogLines,
		Focus:        append([]string(nil), focus...),
		Exclude:      append([]string(nil), exclude...),
		TUI:          *tuiMode,
		Query:        *queryText,
		QueryFile:    *queryFile,
		Format:       *format,
		Emit:         *emit,
		SettleAfter:  max(time.Duration(0), *settleAfter),
		ResultLimit:  *resultLimit,
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
