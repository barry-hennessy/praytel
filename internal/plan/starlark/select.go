package starlark

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

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

func (s selection) AttrNames() []string { return []string{"after", "child_of"} }

// Attr: b.child_of(a), the items of b whose parent is in a; b.after(a),
// the items of b with an earlier sibling in a.
func (s selection) Attr(name string) (sl.Value, error) {
	var make func(other query.Expr) query.Expr
	switch name {
	case "child_of":
		make = func(a query.Expr) query.Expr { return query.Child{Parent: a, Kid: s.e} }
	case "after":
		make = func(a query.Expr) query.Expr { return query.After{Prev: a, Next: s.e} }
	default:
		return nil, nil
	}
	return sl.NewBuiltin(name, func(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
		var other sl.Value
		if err := sl.UnpackArgs(b.Name(), args, kwargs, "other", &other); err != nil {
			return nil, err
		}
		a, err := asSelection(other)
		if err != nil {
			return nil, err
		}
		return selection{make(a)}, nil
	}), nil
}

// matcher is match(re), glob(g) or contains(s): a comparison other than =.
type matcher struct {
	op    query.Op
	value query.Value
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
	return matcher{op: query.Re, value: query.Value{Kind: query.String, S: re}, text: fmt.Sprintf("match(%q)", re)}, nil
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
	return matcher{op: query.Re, value: query.Value{Kind: query.String, S: re}, text: fmt.Sprintf("glob(%q)", g)}, nil
}

func containsFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var s string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "text", &s); err != nil {
		return nil, err
	}
	return matcher{op: query.Contains, value: query.Value{Kind: query.String, S: s}, text: fmt.Sprintf("contains(%q)", s)}, nil
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
		return &query.Cmp{Field: f, Op: v.op, Value: v.value}, nil
	case variable:
		return &query.Cmp{Field: f, Op: query.Eq, Value: query.Value{Kind: query.Arith, E: v.e}}, nil
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
		"duration":  {Scope: "span", Intrinsic: true, Name: "duration"},
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

// variable is var("x"), and arithmetic on variables: a value that a check
// binds in an anchor and uses below it. See query.Env.
type variable struct{ e *query.Expression }

var (
	_ sl.HasBinary = variable{}
	_ sl.HasUnary  = variable{}
)

func (v variable) String() string {
	return query.FormatValue(query.Value{Kind: query.Arith, E: v.e})
}
func (v variable) Type() string          { return "variable" }
func (v variable) Freeze()               {}
func (v variable) Truth() sl.Bool        { return sl.True }
func (v variable) Hash() (uint32, error) { return 0, errors.New("unhashable: variable") }

var arithOps = map[syntax.Token]byte{syntax.PLUS: '+', syntax.MINUS: '-', syntax.STAR: '*', syntax.SLASH: '/'}

// Binary: a variable with a number or another variable, either side.
func (v variable) Binary(op syntax.Token, y sl.Value, side sl.Side) (sl.Value, error) {
	o, ok := arithOps[op]
	if !ok {
		return nil, nil
	}
	other, err := operand(y)
	if err != nil {
		return nil, err
	}
	l, r := v.e, other
	if side == sl.Right {
		l, r = r, l
	}
	return variable{&query.Expression{Op: o, L: l, R: r}}, nil
}

// Unary: -x.
func (v variable) Unary(op syntax.Token) (sl.Value, error) {
	if op != syntax.MINUS {
		return nil, nil
	}
	zero := query.Value{Kind: query.Int}
	return variable{&query.Expression{Op: '-', L: &query.Expression{Num: &zero}, R: v.e}}, nil
}

func operand(y sl.Value) (*query.Expression, error) {
	switch y := y.(type) {
	case variable:
		return y.e, nil
	case sl.Int:
		i, ok := y.Int64()
		if !ok {
			return nil, errors.New("integer out of range")
		}
		return &query.Expression{Num: &query.Value{Kind: query.Int, I: i}}, nil
	case sl.Float:
		return &query.Expression{Num: &query.Value{Kind: query.Float, F: float64(y)}}, nil
	}
	return nil, fmt.Errorf("arithmetic with a variable needs a number or a variable, not a %s", y.Type())
}

func varFn(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
	var name string
	if err := sl.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	if name == "" || strings.ContainsFunc(name, func(r rune) bool {
		return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		return nil, fmt.Errorf("%s: a name is letters, digits and _, not %q", b.Name(), name)
	}
	return variable{&query.Expression{Var: name}}, nil
}

// longer_than("200ms") and shorter_than("1s"): span(duration = ...).
func durationFn(op query.Op) builtinFn {
	return func(_ *sl.Thread, b *sl.Builtin, args sl.Tuple, kwargs []sl.Tuple) (sl.Value, error) {
		var text string
		if err := sl.UnpackArgs(b.Name(), args, kwargs, "duration", &text); err != nil {
			return nil, err
		}
		d, err := time.ParseDuration(text)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return matcher{op: op, value: query.Value{Kind: query.Duration, I: int64(d)}, text: fmt.Sprintf("%s(%q)", b.Name(), text)}, nil
	}
}
