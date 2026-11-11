package starlark

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	sl "go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/query"
)

// part is an output or a piece of one. The | operator joins parts; an
// output has stages, exactly one view, one trigger and one sink.
type part struct {
	stages []plan.Stage
	view   *plan.View
	emit   plan.Trigger
	sink   *plan.Sink
	exit   bool
	desc   []string
}

var (
	_ sl.Value     = (*part)(nil)
	_ sl.HasBinary = (*part)(nil)
)

func (p *part) String() string        { return strings.Join(p.desc, " | ") }
func (p *part) Type() string          { return "output" }
func (p *part) Freeze()               {}
func (p *part) Truth() sl.Bool        { return sl.True }
func (p *part) Hash() (uint32, error) { return 0, errors.New("unhashable: output") }

// Binary joins two parts with |.
func (p *part) Binary(op syntax.Token, y sl.Value, side sl.Side) (sl.Value, error) {
	q, ok := y.(*part)
	if op != syntax.PIPE || !ok {
		return nil, nil // not handled
	}
	a, b := p, q
	if side == sl.Right {
		a, b = q, p
	}
	return join(a, b)
}

func join(a, b *part) (*part, error) {
	out := &part{
		stages: slices.Concat(a.stages, b.stages),
		exit:   a.exit || b.exit,
		desc:   slices.Concat(a.desc, b.desc),
	}
	var err error
	if out.view, err = one("view", a.view, b.view); err != nil {
		return nil, err
	}
	if out.sink, err = one("sink", a.sink, b.sink); err != nil {
		return nil, err
	}
	switch {
	case a.emit != "" && b.emit != "":
		return nil, errors.New("an output has one trigger")
	case a.emit != "":
		out.emit = a.emit
	default:
		out.emit = b.emit
	}
	return out, nil
}

func one[T any](what string, a, b *T) (*T, error) {
	if a != nil && b != nil {
		return nil, fmt.Errorf("an output has one %s", what)
	}
	if a != nil {
		return a, nil
	}
	return b, nil
}

// output is the plan output that p declares.
func (p *part) output() (plan.Output, error) {
	var missing []string
	if p.view == nil {
		missing = append(missing, "a view")
	}
	if p.emit == "" {
		missing = append(missing, "a trigger")
	}
	if p.sink == nil {
		missing = append(missing, "a sink")
	}
	if len(missing) > 0 {
		return plan.Output{}, fmt.Errorf("output %q needs %s", p.String(), strings.Join(missing, ", "))
	}
	o := plan.Output{
		Name:   p.view.Kind,
		Stages: p.stages,
		View:   *p.view,
		Emit:   p.emit,
		Sink:   *p.sink,
	}
	if o.Sink.Kind == plan.SinkTUI && o.Sink.Pane != "" {
		o.Name = o.Sink.Pane
	}
	o.Sink.Exit = p.exit
	return o, nil
}

var predeclared = sl.StringDict{
	// Views.
	"stream":   builtin("stream", viewFn(plan.ViewStream)),
	"tree":     builtin("tree", viewFn(plan.ViewTree)),
	"graph":    builtin("graph", viewFn(plan.ViewGraph)),
	"snapshot": builtin("snapshot", snapshotFn),

	// Stages.
	"attrs":    builtin("attrs", attrsFn),
	"no_attrs": builtin("no_attrs", noAttrsFn),
	"where":    builtin("where", whereFn),

	// Selections.
	"span":     builtin("span", selectorFn(query.KindSpan, spanFields)),
	"log":      builtin("log", selectorFn(query.KindLog, logFields)),
	"match":    builtin("match", matchFn),
	"glob":     builtin("glob", globFn),
	"contains": builtin("contains", containsFn),

	// Triggers.
	"each":    &part{emit: plan.Each, desc: []string{"each"}},
	"tick":    &part{emit: plan.Tick, desc: []string{"tick"}},
	"settled": &part{emit: plan.Settled, desc: []string{"settled"}},
	"final":   &part{emit: plan.Final, desc: []string{"final"}},

	// Sinks.
	"stdout": &part{sink: &plan.Sink{Kind: plan.SinkStdout}, desc: []string{"stdout"}},
	"stderr": &part{sink: &plan.Sink{Kind: plan.SinkStderr}, desc: []string{"stderr"}},
	"file":   builtin("file", fileFn),
	"pane":   builtin("pane", paneFn),
	"exit":   &part{exit: true, desc: []string{"exit"}},

	// Statements.
	"output": builtin("output", outputFn),
}

type builtinFn func(th *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error)

func builtin(name string, fn builtinFn) *sl.Builtin { return sl.NewBuiltin(name, fn) }

func viewFn(kind string) builtinFn {
	return func(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
		if err := sl.UnpackArgs(b.Name(), args, kwargs); err != nil {
			return nil, err
		}
		return &part{view: &plan.View{Kind: kind}, desc: []string{kind + "()"}}, nil
	}
}

// snapshot(*sections, logs=False): the sections render in a fixed order,
// tree before graph, whatever order they are given in.
func snapshotFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var logs bool
	if err := sl.UnpackArgs(b.Name(), nil, kwargs, "logs?", &logs); err != nil {
		return nil, err
	}
	given := map[string]bool{}
	for _, a := range args {
		s, ok := sl.AsString(a)
		if !ok {
			return nil, fmt.Errorf("%s: a section is a string, not a %s", b.Name(), a.Type())
		}
		if s != plan.SectionTree && s != plan.SectionGraph {
			return nil, fmt.Errorf("%s: unknown section %q (want %q or %q)", b.Name(), s, plan.SectionTree, plan.SectionGraph)
		}
		given[s] = true
	}
	if len(given) == 0 {
		return nil, fmt.Errorf("%s: needs a section: %q, %q or both", b.Name(), plan.SectionTree, plan.SectionGraph)
	}
	var sections []string
	for _, s := range []string{plan.SectionTree, plan.SectionGraph} {
		if given[s] {
			sections = append(sections, s)
		}
	}
	return &part{
		view: &plan.View{Kind: plan.ViewSnapshot, Sections: sections, Logs: logs},
		desc: []string{fmt.Sprintf("snapshot(%s)", strings.Join(sections, ", "))},
	}, nil
}

func attrsFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var globs string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "globs?", &globs); err != nil {
		return nil, err
	}
	return &part{stages: []plan.Stage{{Kind: plan.StageAttrs, Globs: globs}}, desc: []string{fmt.Sprintf("attrs(%q)", globs)}}, nil
}

func noAttrsFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	if err := sl.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}
	return &part{stages: []plan.Stage{{Kind: plan.StageNoAttrs}}, desc: []string{"no_attrs()"}}, nil
}

func fileFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var p string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "path", &p); err != nil {
		return nil, err
	}
	return &part{sink: &plan.Sink{Kind: plan.SinkFile, Path: p}, desc: []string{fmt.Sprintf("file(%q)", p)}}, nil
}

func paneFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var name string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	return &part{sink: &plan.Sink{Kind: plan.SinkTUI, Pane: name}, desc: []string{fmt.Sprintf("pane(%q)", name)}}, nil
}

// output(*outputs) declares outputs from a script with statements.
func outputFn(th *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("%s: takes no keyword arguments", b.Name())
	}
	c, ok := th.Local(collectorKey).(*compiler)
	if !ok {
		return nil, fmt.Errorf("%s: a module cannot declare outputs while it loads", b.Name())
	}
	pos := th.CallFrame(1).Pos
	for _, a := range args {
		if err := c.addValue(a, pos); err != nil {
			return nil, err
		}
	}
	return sl.None, nil
}
