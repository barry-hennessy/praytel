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
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/check"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/render/stream"
	"github.com/barry-hennessy/praytel/internal/render/tui"
	"github.com/barry-hennessy/praytel/internal/stage"
	"github.com/barry-hennessy/praytel/internal/table"
	"github.com/barry-hennessy/praytel/internal/view"
)

// DefaultSettleAfter is how long a trace with every real span ended must go
// without an event before it counts as settled.
const DefaultSettleAfter = 500 * time.Millisecond

// Deps is what the outputs of a plan write to and read from.
type Deps struct {
	LT     observability.LoggingTracer
	Events <-chan domain.Event
	Stdout io.Writer
	Stderr io.Writer

	// SettleAfter is the quiet time before a trace settles; zero means
	// DefaultSettleAfter.
	SettleAfter time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Tick wakes the driver to settle traces; nil means a ticker of its
	// own, started only if an output has the trigger settled.
	Tick <-chan time.Time
	// Format is how checks report: "text" (or empty) or "ndjson".
	Format string
}

type output struct {
	name    string
	stages  []stage.Stage
	reader  projection.Reader
	view    view.View
	trigger plan.Trigger
	sink    Sink
	// perTrace outputs render one trace at a time: the trigger settled, or
	// a file path with placeholders. emitted is the revision of each trace
	// they last sent.
	perTrace bool
	emitted  map[string]int
}

// Driver runs the outputs of a plan.
type Driver struct {
	lt      observability.LoggingTracer
	events  <-chan domain.Event
	forest  *projection.Forest
	outputs []*output

	now         func() time.Time
	tick        <-chan time.Time
	stopTick    func()
	settleAfter time.Duration
	settling    bool

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
	d := &Driver{
		lt:          deps.LT,
		events:      deps.Events,
		forest:      projection.New(),
		now:         deps.Now,
		tick:        deps.Tick,
		stopTick:    func() {},
		settleAfter: deps.SettleAfter,
	}
	if d.now == nil {
		d.now = time.Now
	}
	if d.settleAfter <= 0 {
		d.settleAfter = DefaultSettleAfter
	}
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
		d.settling = d.settling || o.Emit == plan.Settled
	}
	if d.settling && d.tick == nil {
		t := time.NewTicker(min(max(d.settleAfter/4, 10*time.Millisecond), 250*time.Millisecond))
		d.tick, d.stopTick = t.C, t.Stop
	}
	return d, nil
}

func (d *Driver) compile(o plan.Output, deps Deps) (*output, error) {
	stages, err := stage.BuildAll(o.Stages)
	if err != nil {
		return nil, err
	}
	if o.Emit == plan.Tick {
		return nil, errors.New("the trigger tick is for TUI panes")
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
	case plan.ViewTree:
		// Away from the TUI, the tree and the graph are drawn as a snapshot
		// draws them: plain text that does not depend on arrival order.
		v = tui.NewSnapshotView(tui.SnapshotOptions{Tree: true})
	case plan.ViewGraph:
		v = tui.NewSnapshotView(tui.SnapshotOptions{Graph: true})
	case plan.ViewCheck:
		format := check.Text
		if deps.Format == string(check.NDJSON) {
			format = check.NDJSON
		}
		c, err := check.New(*o.View.Check, format)
		if err != nil {
			return nil, err
		}
		v = c
	case plan.ViewTable:
		tv, err := table.New(o.View.Select, o.View.Columns, deps.Format == string(check.NDJSON))
		if err != nil {
			return nil, err
		}
		v = tv
	default:
		return nil, fmt.Errorf("view %q is not run by the pipeline", o.View.Kind)
	}

	sink, err := buildSink(o, v, deps)
	if err != nil {
		return nil, err
	}
	return &output{
		name:     o.Name,
		stages:   stages,
		reader:   stage.LensAll(stages, d.forest),
		view:     v,
		trigger:  o.Emit,
		sink:     sink,
		perTrace: o.Emit == plan.Settled || o.Sink.Templated(),
		emitted:  map[string]int{},
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
// triggers. Meanwhile each tick settles quiet traces for "settled" outputs.
// If ctx ends first, views that care are told the run was cut short, and
// Run keeps reading so the producer never blocks.
func (d *Driver) Run(ctx context.Context) error {
	_, log, span := d.lt.Name("pipeline.Driver.Run").Start(ctx)
	defer span.End()
	defer d.stopTick()

	done := ctx.Done()
	for {
		select {
		case ev, ok := <-d.events:
			if !ok {
				d.finish(log.Error)
				if d.tui != nil {
					close(d.tui)
				}
				return nil
			}
			d.apply(ev, log.Error)
		case <-d.tick:
			d.settle(log.Error)
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
	d.forest.SetNow(d.now())
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

// settle sends each trace that has just settled to the "settled" outputs.
func (d *Driver) settle(logErr func(string, ...any)) {
	if !d.settling {
		return
	}
	d.forest.SetNow(d.now())
	for _, id := range d.forest.Settle(d.settleAfter) {
		for _, o := range d.outputs {
			if o.trigger == plan.Settled {
				d.sendTrace(o, id, logErr)
			}
		}
	}
}

// finish makes every trace final, then sends the "final" outputs and every
// per-trace output's traces it has not sent at their last revision.
func (d *Driver) finish(logErr func(string, ...any)) {
	d.forest.SetNow(d.now())
	ids := d.forest.Finalize()
	for _, o := range d.outputs {
		switch {
		case o.perTrace:
			for _, id := range ids {
				if rev, ok := o.emitted[id]; !ok || rev != d.forest.Revision(id) {
					d.sendTrace(o, id, logErr)
				}
			}
		case o.trigger == plan.Final:
			d.send(o, logErr)
		}
		if f, ok := o.view.(view.Finisher); ok {
			if lines := f.Finish(o.reader); len(lines) > 0 {
				if err := o.sink.Write(lines); err != nil {
					logErr("write output", "output", o.name, "err", err)
				}
			}
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

// sendTrace renders one trace. A sink with a file per trace gets it as is;
// any other sink gets a header line naming the trace, its revision and its
// state first, since one sink holds many traces and a trace can come again.
func (d *Driver) sendTrace(o *output, id string, logErr func(string, ...any)) {
	r := projection.OneTrace(o.reader, id)
	rev := d.forest.Revision(id)
	o.emitted[id] = rev
	lines := o.view.Render(r, view.Viewport{})
	if len(lines) == 0 {
		return
	}
	if ts, ok := o.sink.(traceSink); ok {
		if err := ts.WriteTrace(r, id, lines); err != nil {
			logErr("write output", "output", o.name, "trace", id, "err", err)
		}
		return
	}
	header := fmt.Sprintf("# trace %s revision %d %s", id, rev, d.forest.State(id))
	if err := o.sink.Write(append([]string{header}, lines...)); err != nil {
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
