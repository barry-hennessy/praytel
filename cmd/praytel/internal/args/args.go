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
