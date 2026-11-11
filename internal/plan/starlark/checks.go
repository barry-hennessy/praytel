package starlark

import (
	"errors"
	"fmt"

	sl "go.starlark.net/starlark"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/query"
)

// A check is a call at the top level of a script (or in a function it
// calls) that adds the check to the plan, as a rule call does in a Bazel
// BUILD file. Each check is an output: the check view, the trigger settled
// (it judges each trace as it settles, and the run at the end), and an exit
// sink on stderr. See design/PIPELINE.md, "Checks".

func checkOutput(c plan.Check) plan.Output {
	return plan.Output{
		Name: c.Name,
		View: plan.View{Kind: plan.ViewCheck, Check: &c},
		Emit: plan.Settled,
		Sink: plan.Sink{Kind: plan.SinkStderr, Exit: true},
	}
}

// declare adds a check to the plan of the script running on th.
func declare(th *sl.Thread, name string, c plan.Check) (*located, error) {
	comp, ok := th.Local(collectorKey).(*compiler)
	if !ok {
		return nil, fmt.Errorf("%s: a module cannot declare checks while it loads", name)
	}
	l := &located{out: checkOutput(c), pos: th.CallFrame(1).Pos}
	comp.outputs = append(comp.outputs, l)
	return l, nil
}

func statement(kind, args string, given string) string {
	if given != "" {
		return given
	}
	return kind + "(" + args + ")"
}

// never(s, name = "") and some(s, name = "").
func neverSomeFn(kind string) builtinFn {
	return func(th *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
		var v sl.Value
		var name string
		if err := sl.UnpackArgs(b.Name(), args, kwargs, "selection", &v, "name?", &name); err != nil {
			return nil, err
		}
		e, err := asSelection(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		q := query.Format(e)
		_, err = declare(th, b.Name(), plan.Check{Kind: kind, Select: q, Name: statement(kind, q, name)})
		return sl.None, err
	}
}

// count is at_least(n, s), at_most(n, s) or exactly(n, s). On its own it
// is a check over the whole run; given to every(has = ...), it bounds the
// items below each anchor instead, and every takes it over.
type count struct {
	op   string
	n    int
	sel  query.Expr
	name string
	decl *located
}

func (c *count) String() string        { return c.statement() }
func (c *count) Type() string          { return "count" }
func (c *count) Freeze()               {}
func (c *count) Truth() sl.Bool        { return sl.True }
func (c *count) Hash() (uint32, error) { return 0, errors.New("unhashable: count") }

func (c *count) statement() string {
	return fmt.Sprintf("%s(%d, %s)", c.op, c.n, query.Format(c.sel))
}

func countFn(op string) builtinFn {
	return func(th *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
		var n int
		var v sl.Value
		var name string
		if err := sl.UnpackArgs(b.Name(), args, kwargs, "n", &n, "selection", &v, "name?", &name); err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, fmt.Errorf("%s: a count cannot be negative", b.Name())
		}
		e, err := asSelection(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		c := &count{op: op, n: n, sel: e, name: name}
		if _, ok := th.Local(collectorKey).(*compiler); ok {
			// Declared now, so it holds its place among the checks; every()
			// takes it back if it is given there.
			c.decl, _ = declare(th, b.Name(), plan.Check{
				Kind:   plan.CheckCount,
				Select: query.Format(e),
				Count:  &plan.Count{Op: op, N: n},
				Name:   statement(op, fmt.Sprintf("%d, %s", n, query.Format(e)), name),
			})
		}
		return c, nil
	}
}

// every(anchor, has = s or count, name = "").
func everyFn(th *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var av, hv sl.Value
	var name string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "anchor", &av, "has", &hv, "name?", &name); err != nil {
		return nil, err
	}
	anchor, err := asSelection(av)
	if err != nil {
		return nil, fmt.Errorf("%s: anchor: %w", b.Name(), err)
	}
	c := plan.Check{Kind: plan.CheckEvery, Anchor: query.Format(anchor)}
	var has string
	if cv, ok := hv.(*count); ok {
		if cv.decl != nil {
			cv.decl.taken = true
		}
		c.Select = query.Format(cv.sel)
		c.Count = &plan.Count{Op: cv.op, N: cv.n}
		has = cv.statement()
	} else {
		sel, err := asSelection(hv)
		if err != nil {
			return nil, fmt.Errorf("%s: has: %w", b.Name(), err)
		}
		c.Select = query.Format(sel)
		has = c.Select
	}
	c.Name = statement("every", c.Anchor+", has = "+has, name)
	_, err = declare(th, b.Name(), c)
	return sl.None, err
}
