// Package plan is the contract between praytel's front-ends (the flags and,
// later, --execute) and its pipeline: a list of outputs, each a list of
// stages, one view, one trigger and one sink. A plan is plain data. It can
// be written as JSON, validated and compared. See design/PIPELINE.md.
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/barry-hennessy/praytel/internal/attrspec"
	"github.com/barry-hennessy/praytel/internal/query"
)

// Plan is a list of outputs.
type Plan struct {
	Outputs []Output `json:"outputs"`
}

// Output is one set of stages, one view, one trigger and one sink.
type Output struct {
	Name   string  `json:"name,omitempty"`
	Stages []Stage `json:"stages,omitempty"`
	View   View    `json:"view"`
	Emit   Trigger `json:"emit"`
	Sink   Sink    `json:"sink"`
}

// Stage kinds.
const (
	// StageAttrs shows only the attributes whose keys Globs selects (see
	// attrspec.Parse). Empty Globs selects every attribute.
	StageAttrs = "attrs"
	// StageNoAttrs hides every attribute.
	StageNoAttrs = "no_attrs"
	// StageWhere keeps what Query selects (see internal/query).
	StageWhere = "where"
)

// Stage is a record-local filter or change.
type Stage struct {
	Kind  string `json:"kind"`
	Globs string `json:"globs,omitempty"`
	Query string `json:"query,omitempty"`
}

// View kinds.
const (
	// ViewStream is the append-only line stream.
	ViewStream = "stream"
	// ViewSnapshot is the deterministic document of Sections ("tree",
	// "graph") that --snapshot prints.
	ViewSnapshot = "snapshot"
	// ViewTree is the live span tree.
	ViewTree = "tree"
	// ViewGraph is the live component graph.
	ViewGraph = "graph"
	// ViewCheck judges a statement about selections (see Check).
	ViewCheck = "check"
)

// Check kinds; see design/PIPELINE.md, "Checks".
const (
	// CheckNever: no item matches Select.
	CheckNever = "never"
	// CheckSome: at least one item in the run matches Select.
	CheckSome = "some"
	// CheckEvery: each item of Anchor has an item of Select below it (or,
	// with Count, a number of them that Count accepts).
	CheckEvery = "every"
	// CheckCount: the number of items of Select in the run is one that
	// Count accepts.
	CheckCount = "count"
)

// Count bounds a number of items.
type Count struct {
	Op string `json:"op"` // "at_least", "at_most" or "exactly"
	N  int    `json:"n"`
}

// Accepts reports whether n satisfies the bound.
func (c Count) Accepts(n int) bool {
	switch c.Op {
	case "at_least":
		return n >= c.N
	case "at_most":
		return n <= c.N
	}
	return n == c.N
}

// Check is a statement about selections. Its queries are query text.
type Check struct {
	Kind   string `json:"kind"`
	Select string `json:"select"`
	Anchor string `json:"anchor,omitempty"`
	Count  *Count `json:"count,omitempty"`
	Name   string `json:"name"`
}

// Snapshot sections.
const (
	SectionTree  = "tree"
	SectionGraph = "graph"
)

// View is a function that makes output from the projection.
type View struct {
	Kind     string   `json:"kind"`
	Sections []string `json:"sections,omitempty"`
	// Logs includes log lines and the synthetic spans that hold them.
	Logs bool `json:"logs,omitempty"`
	// Check is the statement of a check view.
	Check *Check `json:"check,omitempty"`
}

// Trigger tells a view when to send output.
type Trigger string

// Triggers.
const (
	Each    Trigger = "each"
	Tick    Trigger = "tick"
	Settled Trigger = "settled"
	Final   Trigger = "final"
)

// Sink kinds.
const (
	SinkStdout = "stdout"
	SinkStderr = "stderr"
	SinkFile   = "file"
	SinkTUI    = "tui"
)

// Sink is the destination of an output.
type Sink struct {
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
	Pane string `json:"pane,omitempty"`
	// Exit lets the output decide praytel's exit code when the child
	// succeeds: an incomplete output writes nothing and exits with
	// IncompleteExit. When the child fails, its captured output is replayed
	// to stderr and its code wins.
	Exit bool `json:"exit,omitempty"`
}

// IncompleteExit is praytel's exit code when the child succeeded but an
// output with an exit sink could not be proved complete.
const IncompleteExit = 3

// Validate reports every problem with the plan.
func (p Plan) Validate() error {
	var errs []error
	if len(p.Outputs) == 0 {
		errs = append(errs, errors.New("plan has no outputs"))
	}
	for i, o := range p.Outputs {
		if err := o.validate(); err != nil {
			errs = append(errs, fmt.Errorf("output %d: %w", i+1, err))
		}
	}
	return errors.Join(errs...)
}

func (o Output) validate() error {
	var errs []error
	for _, s := range o.Stages {
		switch s.Kind {
		case StageAttrs:
			if _, err := attrspec.Parse(s.Globs); err != nil {
				errs = append(errs, fmt.Errorf("stage attrs: %w", err))
			}
		case StageNoAttrs:
		case StageWhere:
			if err := o.validateWhere(s.Query); err != nil {
				errs = append(errs, fmt.Errorf("stage where: %w", err))
			}
		default:
			errs = append(errs, fmt.Errorf("unknown stage %q", s.Kind))
		}
	}

	switch o.View.Kind {
	case ViewCheck:
		if err := o.View.Check.validate(); err != nil {
			errs = append(errs, err)
		}
	case ViewStream, ViewTree, ViewGraph:
		if len(o.View.Sections) > 0 {
			errs = append(errs, fmt.Errorf("view %s has no sections", o.View.Kind))
		}
	case ViewSnapshot:
		if len(o.View.Sections) == 0 {
			errs = append(errs, errors.New("view snapshot needs sections"))
		}
		for _, s := range o.View.Sections {
			if s != SectionTree && s != SectionGraph {
				errs = append(errs, fmt.Errorf("unknown snapshot section %q", s))
			}
		}
	default:
		errs = append(errs, fmt.Errorf("unknown view %q", o.View.Kind))
	}

	switch o.Emit {
	case Each, Tick, Settled, Final:
	default:
		errs = append(errs, fmt.Errorf("unknown trigger %q", o.Emit))
	}
	if !triggerFits(o.View.Kind, o.Emit) {
		errs = append(errs, fmt.Errorf("view %s cannot have the trigger %s", o.View.Kind, o.Emit))
	}
	if o.Emit == Tick && o.Sink.Kind != SinkTUI {
		errs = append(errs, errors.New("the trigger tick is for TUI panes"))
	}
	if o.Sink.Exit && ((o.Emit != Final && o.View.Kind != ViewCheck) || o.Sink.Templated()) {
		errs = append(errs, errors.New("an exit sink needs the trigger final (or a check) and one output file"))
	}

	switch o.Sink.Kind {
	case SinkStdout, SinkStderr:
	case SinkFile:
		if o.Sink.Path == "" {
			errs = append(errs, errors.New("sink file needs a path"))
		}
		if o.Sink.Templated() && o.Emit != Final && o.Emit != Settled {
			errs = append(errs, errors.New("a file path with {placeholders} needs the trigger final or settled"))
		}
	case SinkTUI:
		if o.Emit != Tick {
			errs = append(errs, errors.New("sink tui needs the trigger tick"))
		}
		if o.Sink.Exit {
			errs = append(errs, errors.New("sink tui cannot decide the exit code"))
		}
	default:
		errs = append(errs, fmt.Errorf("unknown sink %q", o.Sink.Kind))
	}

	return errors.Join(errs...)
}

// validateWhere checks a where query. The stream writes each line as its
// event comes and never takes one back, so on a stream a query must be one
// that a single log decides (design/PIPELINE.md, rule 4). Every other view
// reads the projection, where any query works.
func (o Output) validateWhere(src string) error {
	e, err := query.Parse(src)
	if err != nil {
		return err
	}
	if o.View.Kind == ViewStream {
		if k, local := query.RecordLocal(e); !local || k != query.KindLog {
			return fmt.Errorf("the stream can only keep logs one at a time: %s needs a log{…} query of single logs, or a view that reads the trace", query.Format(e))
		}
	}
	return nil
}

func (c *Check) validate() error {
	if c == nil {
		return errors.New("view check needs a check")
	}
	var errs []error
	parse := func(what, src string) {
		if _, err := query.Parse(src); err != nil {
			errs = append(errs, fmt.Errorf("check %s: %s: %w", c.Name, what, err))
		}
	}
	parse("select", c.Select)
	switch c.Kind {
	case CheckNever, CheckSome:
	case CheckEvery:
		parse("anchor", c.Anchor)
	case CheckCount:
		if c.Count == nil {
			errs = append(errs, fmt.Errorf("check %s needs a count", c.Name))
		}
	default:
		errs = append(errs, fmt.Errorf("unknown check %q", c.Kind))
	}
	if c.Count != nil {
		switch c.Count.Op {
		case "at_least", "at_most", "exactly":
		default:
			errs = append(errs, fmt.Errorf("check %s: unknown count %q", c.Name, c.Count.Op))
		}
		if c.Count.N < 0 {
			errs = append(errs, fmt.Errorf("check %s: a count cannot be negative", c.Name))
		}
	}
	return errors.Join(errs...)
}

// triggerFits reports whether a view can run with a trigger. The stream
// makes lines per event (each, or a pane's tick); the tree and graph render
// the projection (a pane's tick, or a trace or the run when it is settled or
// final); the snapshot renders once a trace or the run is done.
func triggerFits(view string, t Trigger) bool {
	switch view {
	case ViewStream:
		return t == Each || t == Tick
	case ViewTree, ViewGraph:
		return t == Tick || t == Settled || t == Final
	case ViewSnapshot, ViewCheck:
		return t == Settled || t == Final
	}
	return true // an unknown view is reported on its own
}

// Templated reports whether the sink's file path names the trace it holds:
// {trace_id}, {name} (the root span's name) or {<attribute key>} of the root
// span. Such a sink writes one file per trace.
func (s Sink) Templated() bool {
	return s.Kind == SinkFile && strings.Contains(s.Path, "{")
}

// Explain is the plan as stable, indented JSON.
func (p Plan) Explain() []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	// Queries hold > and &: keep them readable.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		// Plan holds only strings, numbers, bools and slices of them.
		panic(err)
	}
	return b.Bytes()
}

// HasSink reports whether any output writes to a sink of this kind.
func (p Plan) HasSink(kind string) bool {
	for _, o := range p.Outputs {
		if o.Sink.Kind == kind {
			return true
		}
	}
	return false
}
