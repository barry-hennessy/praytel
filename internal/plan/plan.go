// Package plan is the contract between praytel's front-ends (the flags and,
// later, --execute) and its pipeline: a list of outputs, each a list of
// stages, one view, one trigger and one sink. A plan is plain data. It can
// be written as JSON, validated and compared. See design/PIPELINE.md.
package plan

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/barry-hennessy/praytel/internal/attrspec"
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
)

// Stage is a record-local filter or change.
type Stage struct {
	Kind  string `json:"kind"`
	Globs string `json:"globs,omitempty"`
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
)

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
		default:
			errs = append(errs, fmt.Errorf("unknown stage %q", s.Kind))
		}
	}

	switch o.View.Kind {
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
	if o.View.Kind == ViewSnapshot && o.Emit != Final {
		errs = append(errs, errors.New("view snapshot needs the trigger final"))
	}

	switch o.Sink.Kind {
	case SinkStdout, SinkStderr:
	case SinkFile:
		if o.Sink.Path == "" {
			errs = append(errs, errors.New("sink file needs a path"))
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

// Explain is the plan as stable, indented JSON.
func (p Plan) Explain() []byte {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		// Plan holds only strings, bools and slices of them.
		panic(err)
	}
	return append(b, '\n')
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
