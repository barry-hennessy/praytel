// Package args parses praytel's command-line flags into a Config.
package args

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/barry-hennessy/praytel/internal/attrspec"
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
	TUI          bool

	// Snapshot selects the one-shot, deterministic output mode; the views
	// say what it prints.
	Snapshot      bool
	SnapshotTree  bool
	SnapshotGraph bool
	SnapshotAttrs attrspec.Spec
	SnapshotLogs  bool
}

// attrsFlag is --snapshot-attrs. It is a boolean flag so that the bare form
// means "all attributes"; a selection must therefore be given with "=".
type attrsFlag struct {
	set  bool
	spec attrspec.Spec
}

func (f *attrsFlag) IsBoolFlag() bool { return true }

func (f *attrsFlag) String() string { return "" }

func (f *attrsFlag) Set(s string) error {
	f.set = true
	if s == "true" {
		f.spec = attrspec.All()
		return nil
	}
	spec, err := attrspec.Parse(s)
	if err != nil {
		return err
	}
	f.spec = spec

	return nil
}

// parseViews reads --snapshot's space-separated view list.
func parseViews(s string) (tree, graph bool, err error) {
	views := strings.Fields(s)
	if len(views) == 0 {
		return false, false, errors.New(`--snapshot needs views: "tree", "graph" or "tree graph"`)
	}
	for _, v := range views {
		switch v {
		case "tree":
			tree = true
		case "graph":
			graph = true
		default:
			return false, false, fmt.Errorf("unknown --snapshot view %q (want tree or graph)", v)
		}
	}

	return tree, graph, nil
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

	snapshot := fs.String("snapshot", "", `print a stable snapshot of the views "tree" and/or "graph" instead of streaming`)
	var attrs attrsFlag
	fs.Var(&attrs, "snapshot-attrs", `span attributes to show in a snapshot: key globs, "-" excludes; bare means all (give a value as =GLOBS)`)
	snapshotLogs := fs.Bool("snapshot-logs", false, "include log lines in a snapshot")

	if err := fs.Parse(flags); err != nil {
		return Config{}, err
	}

	if fs.NArg() != 0 {
		return Config{}, ErrUsage
	}

	var snapTree, snapGraph bool
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["snapshot"] {
		var err error
		if snapTree, snapGraph, err = parseViews(*snapshot); err != nil {
			fmt.Fprintln(errOut, err)
			return Config{}, ErrUsage
		}
		if *tuiMode {
			fmt.Fprintln(errOut, "--snapshot cannot be combined with --tui")
			return Config{}, ErrUsage
		}
	} else if set["snapshot-attrs"] || set["snapshot-logs"] {
		fmt.Fprintln(errOut, "--snapshot-attrs and --snapshot-logs need --snapshot")
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
		TUI:          *tuiMode,

		Snapshot:      set["snapshot"],
		SnapshotTree:  snapTree,
		SnapshotGraph: snapGraph,
		SnapshotAttrs: attrs.spec,
		SnapshotLogs:  *snapshotLogs,
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
