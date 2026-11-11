package starlark

import (
	"errors"
	"fmt"

	sl "go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/query"
)

// selection is a query as a Starlark value: span(...), log(...) and the
// operators on them make the same tree as the query string would.
type selection struct{ e query.Expr }

var (
	_ sl.HasBinary = selection{}
	_ sl.HasUnary  = selection{}
	_ sl.HasAttrs  = selection{}
)

func (s selection) String() string        { return query.Format(s.e) }
func (s selection) Type() string          { return "selection" }
func (s selection) Freeze()               {}
func (s selection) Truth() sl.Bool        { return sl.True }
func (s selection) Hash() (uint32, error) { return 0, errors.New("unhashable: selection") }

// Binary: a & b (items in both), a | b (items in either), a >> b (items of b
// below an item of a).
func (s selection) Binary(op syntax.Token, y sl.Value, side sl.Side) (sl.Value, error) {
	other, ok := y.(selection)
	if !ok {
		if _, isPart := y.(*part); isPart && op == syntax.PIPE {
			return nil, errors.New("a selection is not part of an output: use where(selection)")
		}
		return nil, nil
	}
	l, r := s.e, other.e
	if side == sl.Right {
		l, r = r, l
	}
	switch op {
	case syntax.AMP:
		return selection{query.Inter{L: l, R: r}}, nil
	case syntax.PIPE:
		return selection{query.Either{L: l, R: r}}, nil
	case syntax.GTGT:
		return selection{query.Desc{Anc: l, Kid: r}}, nil
	}
	return nil, nil
}

// Unary: ~a, the spans not in a.
func (s selection) Unary(op syntax.Token) (sl.Value, error) {
	if op == syntax.TILDE {
		return selection{query.Not{X: s.e}}, nil
	}
	return nil, nil
}

func (s selection) AttrNames() []string { return []string{"child_of"} }

// Attr: b.child_of(a), the items of b whose parent is in a.
func (s selection) Attr(name string) (sl.Value, error) {
	if name != "child_of" {
		return nil, nil
	}
	return sl.NewBuiltin("child_of", func(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
		var parent sl.Value
		if err := sl.UnpackArgs(b.Name(), args, kwargs, "parent", &parent); err != nil {
			return nil, err
		}
		p, err := asSelection(parent)
		if err != nil {
			return nil, err
		}
		return selection{query.Child{Parent: p, Kid: s.e}}, nil
	}), nil
}

// matcher is match(re), glob(g) or contains(s): a comparison other than =.
type matcher struct {
	op    query.Op
	value string
	text  string
}

func (m matcher) String() string        { return m.text }
func (m matcher) Type() string          { return "matcher" }
func (m matcher) Freeze()               {}
func (m matcher) Truth() sl.Bool        { return sl.True }
func (m matcher) Hash() (uint32, error) { return 0, errors.New("unhashable: matcher") }

func matchFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var re string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "pattern", &re); err != nil {
		return nil, err
	}
	return matcher{op: query.Re, value: re, text: fmt.Sprintf("match(%q)", re)}, nil
}

func globFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var g string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "pattern", &g); err != nil {
		return nil, err
	}
	re, err := query.GlobRegexp(g)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	return matcher{op: query.Re, value: re, text: fmt.Sprintf("glob(%q)", g)}, nil
}

func containsFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var s string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "text", &s); err != nil {
		return nil, err
	}
	return matcher{op: query.Contains, value: s, text: fmt.Sprintf("contains(%q)", s)}, nil
}

// selectorFn makes span(...) or log(...). Each keyword names a field, and
// attrs = {...} holds attributes by their OpenTelemetry keys. A value is a
// literal (compared with =) or a matcher.
func selectorFn(kind query.Kind, fields map[string]query.Field) builtinFn {
	return func(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
		if len(args) > 0 {
			return nil, fmt.Errorf("%s: takes keyword arguments only", b.Name())
		}
		var pred query.Pred
		add := func(f query.Field, v sl.Value) error {
			c, err := cmpOf(f, v)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", b.Name(), query.FormatField(f), err)
			}
			if pred == nil {
				pred = c
			} else {
				pred = query.PAnd{L: pred, R: c}
			}
			return nil
		}
		for _, kw := range kwargs {
			name := string(kw[0].(sl.String))
			if name == "attrs" {
				d, ok := kw[1].(*sl.Dict)
				if !ok {
					return nil, fmt.Errorf("%s: attrs is a dict, not a %s", b.Name(), kw[1].Type())
				}
				for _, item := range d.Items() {
					key, ok := sl.AsString(item[0])
					if !ok {
						return nil, fmt.Errorf("%s: an attrs key is a string, not a %s", b.Name(), item[0].Type())
					}
					if err := add(query.Field{Scope: kind.String(), Name: key}, item[1]); err != nil {
						return nil, err
					}
				}
				continue
			}
			f, ok := fields[name]
			if !ok {
				return nil, fmt.Errorf("%s: unexpected keyword argument %q", b.Name(), name)
			}
			if err := add(f, kw[1]); err != nil {
				return nil, err
			}
		}
		e := query.Select{Kind: kind, Pred: pred}
		if err := query.Check(e); err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return selection{e}, nil
	}
}

func cmpOf(f query.Field, v sl.Value) (*query.Cmp, error) {
	switch v := v.(type) {
	case matcher:
		return &query.Cmp{Field: f, Op: v.op, Value: query.Value{Kind: query.String, S: v.value}}, nil
	case sl.String:
		return &query.Cmp{Field: f, Op: query.Eq, Value: query.Value{Kind: query.String, S: string(v)}}, nil
	case sl.Bool:
		return &query.Cmp{Field: f, Op: query.Eq, Value: query.Value{Kind: query.Bool, B: bool(v)}}, nil
	case sl.Int:
		i, ok := v.Int64()
		if !ok {
			return nil, errors.New("integer out of range")
		}
		return &query.Cmp{Field: f, Op: query.Eq, Value: query.Value{Kind: query.Int, I: i}}, nil
	case sl.Float:
		return &query.Cmp{Field: f, Op: query.Eq, Value: query.Value{Kind: query.Float, F: float64(v)}}, nil
	}
	return nil, fmt.Errorf("a value is a string, number, bool or matcher, not a %s", v.Type())
}

var (
	spanFields = map[string]query.Field{
		"name":      {Scope: "span", Intrinsic: true, Name: "name"},
		"component": {Scope: "span", Intrinsic: true, Name: "component"},
		"kind":      {Scope: "span", Intrinsic: true, Name: "kind"},
		"status":    {Scope: "span", Intrinsic: true, Name: "status"},
		"service":   {Scope: "resource", Name: "service.name"},
		"trace_id":  {Scope: "trace", Intrinsic: true, Name: "id"},
	}
	logFields = map[string]query.Field{
		"body":     {Scope: "log", Intrinsic: true, Name: "body"},
		"source":   {Scope: "log", Intrinsic: true, Name: "source"},
		"severity": {Scope: "log", Intrinsic: true, Name: "severity"},
		"trace_id": {Scope: "trace", Intrinsic: true, Name: "id"},
	}
)

// asSelection reads a selection or a query string.
func asSelection(v sl.Value) (query.Expr, error) {
	switch v := v.(type) {
	case selection:
		return v.e, nil
	case sl.String:
		return query.Parse(string(v))
	}
	return nil, fmt.Errorf("want a selection or a query string, not a %s", v.Type())
}

// where(s) keeps what the selection or query string s selects.
func whereFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var v sl.Value
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "selection", &v); err != nil {
		return nil, err
	}
	e, err := asSelection(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	q := query.Format(e)
	return &part{stages: []plan.Stage{{Kind: plan.StageWhere, Query: q}}, desc: []string{"where(" + q + ")"}}, nil
}
