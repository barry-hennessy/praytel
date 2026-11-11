// Package pipeline runs a plan: one driver goroutine reads the correlator's
// events, applies each to the one projection, and passes it to every
// output's stages and view; each output's trigger says when its view sends
// lines to its sink. See design/PIPELINE.md.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/render/stream"
	"github.com/barry-hennessy/praytel/internal/render/tui"
	"github.com/barry-hennessy/praytel/internal/stage"
	"github.com/barry-hennessy/praytel/internal/view"
)

// Deps is what the outputs of a plan write to and read from.
type Deps struct {
	LT     observability.LoggingTracer
	Events <-chan domain.Event
	Stdout io.Writer
	Stderr io.Writer
}

type output struct {
	name    string
	stages  []stage.Stage
	reader  projection.Reader
	view    view.View
	trigger plan.Trigger
	sink    Sink
}

// Driver runs the outputs of a plan.
type Driver struct {
	lt      observability.LoggingTracer
	events  <-chan domain.Event
	forest  *projection.Forest
	outputs []*output

	// panes are the plan's TUI outputs. The TUI runs them on its own
	// goroutine with its own projection; the driver passes it every event
	// on tui until the TUI ends (tuiDone).
	panes    []tui.Pane
	tui      chan domain.Event
	tuiDone  chan struct{}
	tuiEnded sync.Once
}

// Compile builds a driver for p. It does not start it.
func Compile(p plan.Plan, deps Deps) (*Driver, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	d := &Driver{lt: deps.LT, events: deps.Events, forest: projection.New()}
	if p.HasSink(plan.SinkTUI) {
		for _, o := range p.Outputs {
			if o.Sink.Kind == plan.SinkStdout || o.Sink.Kind == plan.SinkStderr {
				return nil, errors.New("the TUI owns the terminal: send the other outputs to files")
			}
		}
		d.tui = make(chan domain.Event, 64)
		d.tuiDone = make(chan struct{})
	}
	for i, o := range p.Outputs {
		if o.Sink.Kind == plan.SinkTUI {
			pane, err := compilePane(o)
			if err != nil {
				return nil, fmt.Errorf("output %d: %w", i+1, err)
			}
			d.panes = append(d.panes, pane)
			continue
		}
		out, err := d.compile(o, deps)
		if err != nil {
			d.Close(0)
			return nil, fmt.Errorf("output %d: %w", i+1, err)
		}
		d.outputs = append(d.outputs, out)
	}
	return d, nil
}

func (d *Driver) compile(o plan.Output, deps Deps) (*output, error) {
	stages, err := stage.BuildAll(o.Stages)
	if err != nil {
		return nil, err
	}
	switch o.Emit {
	case plan.Each, plan.Final:
	default:
		return nil, fmt.Errorf("the trigger %q is not run by the pipeline yet", o.Emit)
	}

	var v view.View
	switch o.View.Kind {
	case plan.ViewStream:
		log := deps.LT.Log
		color := o.Sink.Kind == plan.SinkStdout && stream.ColorEnabled(deps.Stdout)
		v = stream.NewView(&log, color, nil)
	case plan.ViewSnapshot:
		opts := tui.SnapshotOptions{Logs: o.View.Logs}
		for _, s := range o.View.Sections {
			opts.Tree = opts.Tree || s == plan.SectionTree
			opts.Graph = opts.Graph || s == plan.SectionGraph
		}
		v = tui.NewSnapshotView(opts)
	default:
		return nil, fmt.Errorf("view %q is not run by the pipeline yet", o.View.Kind)
	}

	sink, err := buildSink(o, v, deps)
	if err != nil {
		return nil, err
	}
	return &output{
		name:    o.Name,
		stages:  stages,
		reader:  stage.LensAll(stages, d.forest),
		view:    v,
		trigger: o.Emit,
		sink:    sink,
	}, nil
}

func compilePane(o plan.Output) (tui.Pane, error) {
	stages, err := stage.BuildAll(o.Stages)
	if err != nil {
		return tui.Pane{}, err
	}
	var kind string
	switch o.View.Kind {
	case plan.ViewTree:
		kind = tui.PaneTree
	case plan.ViewGraph:
		kind = tui.PaneGraph
	case plan.ViewStream:
		kind = tui.PaneStream
	default:
		return tui.Pane{}, fmt.Errorf("view %q cannot be a TUI pane", o.View.Kind)
	}
	name := o.Sink.Pane
	if name == "" {
		name = o.View.Kind
	}
	return tui.Pane{Name: name, Kind: kind, Stages: stages}, nil
}

// Panes are the plan's TUI panes, in order; none if the plan has no TUI.
func (d *Driver) Panes() []tui.Pane { return d.panes }

// TUIEvents is every event, for the TUI to run its panes on. It closes
// when the driver's events end.
func (d *Driver) TUIEvents() <-chan domain.Event { return d.tui }

// TUIEnded tells the driver the TUI has stopped reading, so it stops
// passing events on.
func (d *Driver) TUIEnded() {
	if d.tuiDone != nil {
		d.tuiEnded.Do(func() { close(d.tuiDone) })
	}
}

// Run reads events until the channel closes, then fires the "final"
// triggers. If ctx ends first, views that care are told the run was cut
// short, and Run keeps reading so the producer never blocks.
func (d *Driver) Run(ctx context.Context) error {
	_, log, span := d.lt.Name("pipeline.Driver.Run").Start(ctx)
	defer span.End()

	done := ctx.Done()
	for {
		select {
		case ev, ok := <-d.events:
			if !ok {
				d.fire(plan.Final, log.Error)
				if d.tui != nil {
					close(d.tui)
				}
				return nil
			}
			d.apply(ev, log.Error)
		case <-done:
			done = nil
			for _, o := range d.outputs {
				if i, ok := o.view.(view.Interrupter); ok {
					i.Interrupt()
				}
			}
		}
	}
}

func (d *Driver) apply(ev domain.Event, logErr func(string, ...any)) {
	if d.tui != nil {
		select {
		case d.tui <- ev:
		case <-d.tuiDone:
		}
	}
	d.forest.Apply(ev)
	for _, o := range d.outputs {
		stage.Chain(o.stages, ev, func(ev domain.Event) {
			o.view.Observe(ev, o.reader)
		})
		if o.trigger == plan.Each {
			d.send(o, logErr)
		}
	}
}

func (d *Driver) fire(t plan.Trigger, logErr func(string, ...any)) {
	for _, o := range d.outputs {
		if o.trigger == t {
			d.send(o, logErr)
		}
	}
}

func (d *Driver) send(o *output, logErr func(string, ...any)) {
	lines := o.view.Render(o.reader, view.Viewport{})
	if len(lines) == 0 {
		return
	}
	if err := o.sink.Write(lines); err != nil {
		logErr("write output", "output", o.name, "err", err)
	}
}

// Close ends every output and returns praytel's exit code: the child's if
// it failed, else the highest code an output asks for.
func (d *Driver) Close(childCode int) int {
	code := 0
	for _, o := range d.outputs {
		code = max(code, o.sink.Close(childCode))
	}
	if childCode != 0 {
		return childCode
	}
	return code
}
