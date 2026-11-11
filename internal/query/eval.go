package query

import (
	"maps"
	"math"
	"strings"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
)

// Result is what a query selects in one trace: span IDs and log IDs. A log
// selector also selects the span its log is assigned to, so logs can take
// part in > and >>.
type Result struct {
	Spans map[string]bool
	Logs  map[string]bool
}

// Empty reports whether the query selected nothing.
func (r Result) Empty() bool { return len(r.Spans) == 0 && len(r.Logs) == 0 }

// Env holds the values of variables. A comparison field = $x with $x not
// yet bound binds $x to the field's value; the selection on the other side
// of >, >> or ~> then sees it, once for each span that bound it.
type Env map[string]domain.Value

// Trace is one trace, indexed for evaluation.
type Trace struct {
	ID    string
	spans map[string]*projection.Span
	logs  []*projection.Log

	duration    time.Duration
	hasDuration bool
	durationOK  bool
}

// TraceOf indexes the trace traceID of r. Synthetic spans (praytel's own
// orphan holders) and placeholders (spans logs named that never arrived)
// are not selectable.
func TraceOf(r projection.Reader, traceID string) *Trace {
	t := &Trace{ID: traceID, spans: map[string]*projection.Span{}}
	for sp := range r.Spans() {
		if sp.TraceID == traceID && !sp.Synthetic && !sp.Declared {
			t.spans[sp.ID] = sp
		}
	}
	for l := range r.Logs() {
		if l.TraceID == traceID {
			t.logs = append(t.logs, l)
		}
	}
	return t
}

// TracesOf indexes every trace of r in one pass.
func TracesOf(r projection.Reader) map[string]*Trace {
	out := map[string]*Trace{}
	get := func(id string) *Trace {
		t := out[id]
		if t == nil {
			t = &Trace{ID: id, spans: map[string]*projection.Span{}}
			out[id] = t
		}
		return t
	}
	for sp := range r.Spans() {
		t := get(sp.TraceID)
		if !sp.Synthetic && !sp.Declared {
			t.spans[sp.ID] = sp
		}
	}
	for l := range r.Logs() {
		t := get(l.TraceID)
		t.logs = append(t.logs, l)
	}
	return out
}

// Logs are the trace's logs.
func (t *Trace) Logs() []*projection.Log { return t.logs }

// Span is the selectable span with this ID, or nil.
func (t *Trace) Span(id string) *projection.Span { return t.spans[id] }

// Duration is from the first span's start to the last span's end.
func (t *Trace) Duration() (time.Duration, bool) {
	if !t.hasDuration {
		t.hasDuration = true
		var first, last time.Time
		for _, sp := range t.spans {
			if sp.Start.IsZero() || sp.End.IsZero() {
				continue
			}
			if first.IsZero() || sp.Start.Before(first) {
				first = sp.Start
			}
			if sp.End.After(last) {
				last = sp.End
			}
		}
		if !first.IsZero() {
			t.duration, t.durationOK = last.Sub(first), true
		}
	}
	return t.duration, t.durationOK
}

// Eval runs e on the trace.
func Eval(e Expr, t *Trace) Result { return EvalEnv(e, t, nil) }

// EvalEnv runs e on the trace with the variables of env bound.
func EvalEnv(e Expr, t *Trace, env Env) Result {
	switch e := e.(type) {
	case Select:
		return t.selectAll(e, env)
	case Both:
		l, r := EvalEnv(e.L, t, env), EvalEnv(e.R, t, env)
		if l.Empty() || r.Empty() {
			return empty()
		}
		return union(l, r)
	case Either:
		return union(EvalEnv(e.L, t, env), EvalEnv(e.R, t, env))
	case Inter:
		l, r := EvalEnv(e.L, t, env), EvalEnv(e.R, t, env)
		out := empty()
		for id := range l.Spans {
			if r.Spans[id] {
				out.Spans[id] = true
			}
		}
		for id := range l.Logs {
			if r.Logs[id] {
				out.Logs[id] = true
			}
		}
		return out
	case Not:
		x := EvalEnv(e.X, t, env)
		out := empty()
		for id := range t.spans {
			if !x.Spans[id] {
				out.Spans[id] = true
			}
		}
		return out
	case Child:
		return t.related(e.Parent, e.Kid, env, func(sp *projection.Span, anchors map[string]bool) bool {
			return anchors[sp.ParentID]
		})
	case Desc:
		return t.related(e.Anc, e.Kid, env, func(sp *projection.Span, anchors map[string]bool) bool {
			return t.hasAncestor(sp, anchors)
		})
	case After:
		return t.related(e.Prev, e.Next, env, func(sp *projection.Span, prev map[string]bool) bool {
			for id := range prev {
				if p := t.spans[id]; p != nil && p.ID != sp.ID && p.ParentID == sp.ParentID &&
					!p.Start.IsZero() && p.Start.Before(sp.Start) {
					return true
				}
			}
			return false
		})
	case Count:
		x := EvalEnv(e.X, t, env)
		n := len(x.Spans)
		if k, ok := RecordLocal(e.X); ok && k == KindLog {
			n = len(x.Logs)
		}
		if compareNumbers(float64(n), float64(e.N), e.Op) {
			return x
		}
		return empty()
	}
	return empty()
}

// related is the items of kid whose span stands in rel to a span of
// anchor. If anchor binds variables that kid uses, kid is evaluated once for
// each anchor span, with that span's values.
func (t *Trace) related(anchor, kid Expr, env Env, rel func(*projection.Span, map[string]bool) bool) Result {
	sel, ok := anchor.(Select)
	if !ok || sel.Kind != KindSpan || !bindsFor(sel, kid, env) {
		anchors := EvalEnv(anchor, t, env).Spans
		return t.keep(EvalEnv(kid, t, env), func(sp *projection.Span) bool { return rel(sp, anchors) })
	}
	out := empty()
	for id, sp := range t.spans {
		bound, ok := BindSpan(sel, sp, t, env)
		if !ok {
			continue
		}
		one := map[string]bool{id: true}
		out = union(out, t.keep(EvalEnv(kid, t, bound), func(s *projection.Span) bool { return rel(s, one) }))
	}
	return out
}

// bindsFor reports whether sel binds a variable that kid uses and env does
// not hold.
func bindsFor(sel Select, kid Expr, env Env) bool {
	for _, v := range Vars(kid) {
		if _, ok := env[v]; !ok && binds(sel.Pred, v) {
			return true
		}
	}
	return false
}

func binds(p Pred, v string) bool {
	switch p := p.(type) {
	case *Cmp:
		return p.Op == Eq && p.Value.Kind == Arith && p.Value.E.Var == v
	case PAnd:
		return binds(p.L, v) || binds(p.R, v)
	case POr:
		return binds(p.L, v) && binds(p.R, v)
	}
	return false
}

// BindSpan reports whether the selection matches sp, and the variables its
// unbound $x comparisons take from sp, added to env. A query that is not a
// span selector is only matched.
func BindSpan(e Expr, sp *projection.Span, t *Trace, env Env) (Env, bool) {
	sel, ok := e.(Select)
	if !ok || sel.Kind != KindSpan {
		return env, EvalEnv(e, t, env).Spans[sp.ID]
	}
	if sel.Pred == nil {
		return env, true
	}
	out := maps.Clone(env)
	if out == nil {
		out = Env{}
	}
	return out, bind(sel.Pred, func(f Field) (domain.Value, bool) { return spanField(sp, t, f) }, out)
}

// bind is match that also records the values unbound variables meet.
func bind(p Pred, get func(Field) (domain.Value, bool), env Env) bool {
	switch p := p.(type) {
	case *Cmp:
		if p.Op == Eq && p.Value.Kind == Arith && p.Value.E.Var != "" {
			if _, bound := env[p.Value.E.Var]; !bound {
				v, ok := get(p.Field)
				if ok {
					env[p.Value.E.Var] = v
				}
				return ok
			}
		}
		return matchEnv(p, get, env)
	case PAnd:
		return bind(p.L, get, env) && bind(p.R, get, env)
	case POr:
		trial := maps.Clone(env)
		if bind(p.L, get, trial) {
			maps.Copy(env, trial)
			return true
		}
		return bind(p.R, get, env)
	}
	return matchEnv(p, get, env)
}

func empty() Result { return Result{Spans: map[string]bool{}, Logs: map[string]bool{}} }

func union(a, b Result) Result {
	out := empty()
	maps.Copy(out.Spans, a.Spans)
	maps.Copy(out.Spans, b.Spans)
	maps.Copy(out.Logs, a.Logs)
	maps.Copy(out.Logs, b.Logs)
	return out
}

// keep is the part of r whose spans pass ok, with the logs on those spans.
func (t *Trace) keep(r Result, ok func(*projection.Span) bool) Result {
	out := empty()
	for id := range r.Spans {
		if sp := t.spans[id]; sp != nil && ok(sp) {
			out.Spans[id] = true
		}
	}
	for _, l := range t.logs {
		if r.Logs[l.ID] && out.Spans[l.SpanID] {
			out.Logs[l.ID] = true
		}
	}
	return out
}

// hasAncestor walks up from sp's parent; a parent cycle stops the walk.
func (t *Trace) hasAncestor(sp *projection.Span, set map[string]bool) bool {
	seen := map[string]bool{sp.ID: true}
	for id := sp.ParentID; id != "" && !seen[id]; {
		if set[id] {
			return true
		}
		seen[id] = true
		p := t.spans[id]
		if p == nil {
			return false
		}
		id = p.ParentID
	}
	return false
}

func (t *Trace) selectAll(s Select, env Env) Result {
	out := empty()
	if s.Kind == KindSpan {
		for id, sp := range t.spans {
			if s.Pred == nil || matchEnv(s.Pred, func(f Field) (domain.Value, bool) { return spanField(sp, t, f) }, env) {
				out.Spans[id] = true
			}
		}
		return out
	}
	for _, l := range t.logs {
		if s.Pred == nil || matchEnv(s.Pred, func(f Field) (domain.Value, bool) { return logField(l, f) }, env) {
			out.Logs[l.ID] = true
			if t.spans[l.SpanID] != nil {
				out.Spans[l.SpanID] = true
			}
		}
	}
	return out
}

// MatchSpan reports whether p holds for sp. trace:duration needs the trace
// and is missing here.
func MatchSpan(p Pred, sp *projection.Span) bool {
	return matchEnv(p, func(f Field) (domain.Value, bool) { return spanField(sp, nil, f) }, nil)
}

// MatchLog reports whether p holds for l.
func MatchLog(p Pred, l *projection.Log) bool {
	return matchEnv(p, func(f Field) (domain.Value, bool) { return logField(l, f) }, nil)
}

// Matches reports whether a record-local query (see RecordLocal) selects
// the span; a log selector never does.
func Matches(e Expr, sp *projection.Span) bool {
	return matchesRecord(e, KindSpan, func(p Pred) bool { return MatchSpan(p, sp) })
}

// MatchesLog reports whether a record-local query selects the log.
func MatchesLog(e Expr, l *projection.Log) bool {
	return matchesRecord(e, KindLog, func(p Pred) bool { return MatchLog(p, l) })
}

func matchesRecord(e Expr, k Kind, pred func(Pred) bool) bool {
	switch e := e.(type) {
	case Select:
		return e.Kind == k && (e.Pred == nil || pred(e.Pred))
	case Inter:
		return matchesRecord(e.L, k, pred) && matchesRecord(e.R, k, pred)
	case Either:
		return matchesRecord(e.L, k, pred) || matchesRecord(e.R, k, pred)
	case Not:
		return !matchesRecord(e.X, k, pred)
	}
	return false
}

func matchEnv(p Pred, get func(Field) (domain.Value, bool), env Env) bool {
	switch p := p.(type) {
	case *Cmp:
		v, ok := get(p.Field)
		if !ok {
			return false
		}
		if p.Value.Kind == Arith {
			if p.Value.E.Var != "" && p.Op == Eq {
				if _, bound := env[p.Value.E.Var]; !bound {
					return true // binds; see bind
				}
			}
			want, ok := evalExpression(p.Value.E, env)
			if !ok {
				return false
			}
			return compareValues(v, want, p.Op)
		}
		return compare(v, p)
	case Exists:
		_, ok := get(p.Field)
		return ok
	case PNot:
		return !matchEnv(p.P, get, env)
	case PAnd:
		return matchEnv(p.L, get, env) && matchEnv(p.R, get, env)
	case POr:
		return matchEnv(p.L, get, env) || matchEnv(p.R, get, env)
	}
	return false
}

// evalExpression computes arithmetic. A bare variable keeps its type; any
// operator works on numbers (durations are nanoseconds).
func evalExpression(e *Expression, env Env) (domain.Value, bool) {
	switch {
	case e == nil:
		return domain.Value{}, false
	case e.Var != "":
		v, ok := env[e.Var]
		return v, ok
	case e.Num != nil:
		switch e.Num.Kind {
		case Int, Duration:
			return domain.Int64Value(e.Num.I), true
		}
		return domain.Float64Value(e.Num.F), true
	}
	l, ok := evalExpression(e.L, env)
	if !ok {
		return domain.Value{}, false
	}
	r, ok := evalExpression(e.R, env)
	if !ok {
		return domain.Value{}, false
	}
	li, lInt := l.AsInt64()
	ri, rInt := r.AsInt64()
	if lInt && rInt && e.Op != '/' {
		switch e.Op {
		case '+':
			return domain.Int64Value(li + ri), true
		case '-':
			return domain.Int64Value(li - ri), true
		case '*':
			return domain.Int64Value(li * ri), true
		}
	}
	x, ok := number(l)
	if !ok {
		return domain.Value{}, false
	}
	y, ok := number(r)
	if !ok {
		return domain.Value{}, false
	}
	var z float64
	switch e.Op {
	case '+':
		z = x + y
	case '-':
		z = x - y
	case '*':
		z = x * y
	case '/':
		if y == 0 {
			return domain.Value{}, false
		}
		z = x / y
	}
	if z == math.Trunc(z) && lInt && rInt {
		return domain.Int64Value(int64(z)), true
	}
	return domain.Float64Value(z), true
}

// compareValues compares two record values: strings with strings, bools
// with bools, numbers with numbers.
func compareValues(v, w domain.Value, op Op) bool {
	if s, ok := w.AsString(); ok {
		return compare(v, &Cmp{Op: op, Value: Value{Kind: String, S: s}})
	}
	if b, ok := w.AsBool(); ok {
		return compare(v, &Cmp{Op: op, Value: Value{Kind: Bool, B: b}})
	}
	x, ok := number(v)
	y, ok2 := number(w)
	if !ok || !ok2 {
		return op == Ne
	}
	return compareNumbers(x, y, op)
}

func compareNumbers(x, y float64, op Op) bool {
	switch op {
	case Eq:
		return x == y
	case Ne:
		return x != y
	case Gt:
		return x > y
	case Ge:
		return x >= y
	case Lt:
		return x < y
	case Le:
		return x <= y
	}
	return false
}

func attr(attrs []domain.Attr, key string) (domain.Value, bool) {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value, true
		}
	}
	return domain.Value{}, false
}

var (
	statusNames = map[domain.SpanStatus]string{
		domain.SpanStatusUnset: "unset", domain.SpanStatusOK: "ok", domain.SpanStatusError: "error",
	}
	kindNames = map[domain.SpanKind]string{
		domain.SpanKindUnspecified: "unspecified", domain.SpanKindInternal: "internal",
		domain.SpanKindServer: "server", domain.SpanKindClient: "client",
		domain.SpanKindProducer: "producer", domain.SpanKindConsumer: "consumer",
	}
)

func str(s string) (domain.Value, bool) { return domain.StringValue(s), true }

func spanField(sp *projection.Span, t *Trace, f Field) (domain.Value, bool) {
	switch {
	case f.Scope == "trace" && f.Name == "id":
		return str(sp.TraceID)
	case f.Scope == "trace" && f.Name == "duration":
		if t == nil {
			return domain.Value{}, false
		}
		d, ok := t.Duration()
		return domain.Int64Value(int64(d)), ok
	case f.Scope == "resource":
		return attr(sp.Resource, f.Name)
	case f.Scope != "span":
		return domain.Value{}, false
	case !f.Intrinsic:
		return attr(sp.Attributes, f.Name)
	}
	switch f.Name {
	case "name":
		return str(sp.Name)
	case "component":
		return str(sp.Component)
	case "id":
		return str(sp.ID)
	case "parent_id":
		if sp.ParentID == "" {
			return domain.Value{}, false
		}
		return str(sp.ParentID)
	case "status":
		return str(statusNames[sp.Status])
	case "kind":
		return str(kindNames[sp.Kind])
	case "duration":
		if sp.Start.IsZero() || sp.End.IsZero() {
			return domain.Value{}, false
		}
		return domain.Int64Value(int64(sp.End.Sub(sp.Start))), true
	}
	return domain.Value{}, false
}

func logField(l *projection.Log, f Field) (domain.Value, bool) {
	switch {
	case f.Scope == "trace" && f.Name == "id":
		return str(l.TraceID)
	case f.Scope != "log":
		return domain.Value{}, false
	case !f.Intrinsic:
		return attr(l.Attributes, f.Name)
	}
	switch f.Name {
	case "body":
		return str(l.Body)
	case "source":
		return str(string(l.Source))
	case "severity":
		if l.SeverityNumber == 0 {
			return domain.Value{}, false
		}
		return domain.Int64Value(int64(l.SeverityNumber)), true
	case "severity_text":
		if l.SeverityText == "" {
			return domain.Value{}, false
		}
		return str(l.SeverityText)
	}
	return domain.Value{}, false
}

// compare applies c to v. Types do not convert: 500 is not "500". A
// comparison between types that do not compare is false, except != which
// is true.
func compare(v domain.Value, c *Cmp) bool {
	switch c.Op {
	case Re, NRe:
		s, ok := v.AsString()
		return ok && c.re != nil && c.re.MatchString(s) == (c.Op == Re)
	case Contains:
		s, ok := v.AsString()
		return ok && strings.Contains(s, c.Value.S)
	}
	cmp, ok := order(v, c.Value)
	if !ok {
		return c.Op == Ne
	}
	switch c.Op {
	case Eq:
		return cmp == 0
	case Ne:
		return cmp != 0
	case Gt:
		return cmp > 0
	case Ge:
		return cmp >= 0
	case Lt:
		return cmp < 0
	case Le:
		return cmp <= 0
	}
	return false
}

// order compares a record's value with a literal: -1, 0 or 1, and whether
// they compare at all.
func order(v domain.Value, lit Value) (int, bool) {
	switch lit.Kind {
	case String, Ident:
		s, ok := v.AsString()
		return strings.Compare(s, lit.S), ok
	case Bool:
		b, ok := v.AsBool()
		if !ok {
			return 0, false
		}
		switch {
		case b == lit.B:
			return 0, true
		case b:
			return 1, true
		}
		return -1, true
	case Int, Float, Duration:
		x, ok := number(v)
		if !ok {
			return 0, false
		}
		y := lit.F
		if lit.Kind != Float {
			y = float64(lit.I)
		}
		switch {
		case x < y:
			return -1, true
		case x > y:
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func number(v domain.Value) (float64, bool) {
	if i, ok := v.AsInt64(); ok {
		return float64(i), true
	}
	return v.AsFloat64()
}

// FieldValue is a span's field as text, and whether the span has it. A
// duration is written as a Go duration.
func FieldValue(sp *projection.Span, t *Trace, f Field) (string, bool) {
	v, ok := spanField(sp, t, f)
	if !ok {
		return "", false
	}
	if f.Intrinsic && f.Name == "duration" {
		if i, ok := v.AsInt64(); ok {
			return time.Duration(i).String(), true
		}
	}
	return v.String(), true
}
