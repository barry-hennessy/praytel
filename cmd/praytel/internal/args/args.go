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
	// SettleAfter is how long a trace with every real span ended must go
	// without an event before outputs with the trigger "settled" send it.
	SettleAfter time.Duration
	// Format is how checks report: "text" or "ndjson".
	Format      string
	MaxSpans    int
	MaxLogLines int
	TUI         bool

	// Snapshot selects the one-shot, deterministic output mode; the views
	// say what it prints.
	Snapshot      bool
	SnapshotTree  bool
	SnapshotGraph bool
	SnapshotAttrs attrspec.Spec
	SnapshotLogs  bool

	// snapshotGlobs is --snapshot-attrs as given, for the plan; nil when
	// the flag is absent.
	snapshotGlobs *string

	// PrintPlan prints the plan the other flags make, as JSON, and exits.
	// It does not need a command.
	PrintPlan bool

	// Execute is each --execute (-e) value in order: Starlark code, or a
	// path to a .star file. It replaces the mode flags.
	Execute []string
}

// executeFlag collects every --execute and -e value.
type executeFlag []string

func (f *executeFlag) String() string { return strings.Join(*f, " ") }

func (f *executeFlag) Set(s string) error {
	*f = append(*f, s)
	return nil
}

// attrsFlag is --snapshot-attrs. It is a boolean flag so that the bare form
// means "all attributes"; a selection must therefore be given with "=".
type attrsFlag struct {
	set   bool
	spec  attrspec.Spec
	globs string
}

func (f *attrsFlag) IsBoolFlag() bool { return true }

func (f *attrsFlag) String() string { return "" }

func (f *attrsFlag) Set(s string) error {
	f.set = true
	if s == "true" {
		f.spec = attrspec.All()
		f.globs = ""
		return nil
	}
	f.globs = s
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
		// Only --print-plan runs without a command; checked after parsing.
		flags, command = args, nil
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
	settleAfter := fs.Duration("settle-after", 500*time.Millisecond, "quiet time before a finished trace counts as settled")
	format := fs.String("format", "text", `how checks report: "text" or "ndjson"`)
	maxSpans := fs.Int("max-spans", correlator.DefaultMaxSpans, "max correlator-retained spans including pending and orphans")
	maxLogLines := fs.Int("max-log-lines", correlator.DefaultMaxLogLines, "max correlator-retained log lines including pending and orphans")
	tuiMode := fs.Bool("tui", false, "render an interactive live trace tree")

	snapshot := fs.String("snapshot", "", `print a stable snapshot of the views "tree" and/or "graph" instead of streaming`)
	var attrs attrsFlag
	fs.Var(&attrs, "snapshot-attrs", `span attributes to show in a snapshot: key globs, "-" excludes; bare means all (give a value as =GLOBS)`)
	snapshotLogs := fs.Bool("snapshot-logs", false, "include log lines in a snapshot")
	printPlan := fs.Bool("print-plan", false, "print the plan the other flags make, as JSON, and exit")
	var execute executeFlag
	fs.Var(&execute, "execute", "Starlark that declares the outputs, or a path to a .star file; repeatable")
	fs.Var(&execute, "e", "short for --execute")

	if err := fs.Parse(flags); err != nil {
		return Config{}, err
	}

	if fs.NArg() != 0 {
		return Config{}, ErrUsage
	}

	if !*printPlan && (len(command) == 0 || strings.TrimSpace(command[0]) == "") {
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

	if len(execute) > 0 && (*tuiMode || set["snapshot"]) {
		fmt.Fprintln(errOut, "--execute cannot be combined with --tui or --snapshot")
		return Config{}, ErrUsage
	}

	if *format != "text" && *format != "ndjson" {
		fmt.Fprintf(errOut, "--format is \"text\" or \"ndjson\", not %q\n", *format)
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
		SettleAfter:  max(0, *settleAfter),
		Format:       *format,
		MaxSpans:     *maxSpans,
		MaxLogLines:  *maxLogLines,
		TUI:          *tuiMode,

		Snapshot:      set["snapshot"],
		SnapshotTree:  snapTree,
		SnapshotGraph: snapGraph,
		SnapshotAttrs: attrs.spec,
		SnapshotLogs:  *snapshotLogs,
		snapshotGlobs: globsOf(attrs),

		PrintPlan: *printPlan,
		Execute:   execute,
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

func globsOf(f attrsFlag) *string {
	if !f.set {
		return nil
	}
	g := f.globs
	return &g
}
