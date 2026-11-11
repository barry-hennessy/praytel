package query

import (
	"maps"
	"strings"

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

// Trace is one trace, indexed for evaluation.
type Trace struct {
	ID    string
	spans map[string]*projection.Span
	logs  []*projection.Log
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

// Span is the selectable span with this ID, or nil.
func (t *Trace) Span(id string) *projection.Span { return t.spans[id] }

// Eval runs e on the trace.
func Eval(e Expr, t *Trace) Result {
	switch e := e.(type) {
	case Select:
		return t.selectAll(e)
	case Both:
		l, r := Eval(e.L, t), Eval(e.R, t)
		if l.Empty() || r.Empty() {
			return empty()
		}
		return union(l, r)
	case Either:
		return union(Eval(e.L, t), Eval(e.R, t))
	case Inter:
		l, r := Eval(e.L, t), Eval(e.R, t)
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
		x := Eval(e.X, t)
		out := empty()
		for id := range t.spans {
			if !x.Spans[id] {
				out.Spans[id] = true
			}
		}
		return out
	case Child:
		parents := Eval(e.Parent, t).Spans
		return t.keep(Eval(e.Kid, t), func(sp *projection.Span) bool {
			return parents[sp.ParentID]
		})
	case Desc:
		ancestors := Eval(e.Anc, t).Spans
		return t.keep(Eval(e.Kid, t), func(sp *projection.Span) bool {
			return t.hasAncestor(sp, ancestors)
		})
	}
	return empty()
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

func (t *Trace) selectAll(s Select) Result {
	out := empty()
	if s.Kind == KindSpan {
		for id, sp := range t.spans {
			if s.Pred == nil || MatchSpan(s.Pred, sp) {
				out.Spans[id] = true
			}
		}
		return out
	}
	for _, l := range t.logs {
		if s.Pred == nil || MatchLog(s.Pred, l) {
			out.Logs[l.ID] = true
			if t.spans[l.SpanID] != nil {
				out.Spans[l.SpanID] = true
			}
		}
	}
	return out
}

// MatchSpan reports whether p holds for sp.
func MatchSpan(p Pred, sp *projection.Span) bool {
	return match(p, func(f Field) (domain.Value, bool) { return spanField(sp, f) })
}

// MatchLog reports whether p holds for l.
func MatchLog(p Pred, l *projection.Log) bool {
	return match(p, func(f Field) (domain.Value, bool) { return logField(l, f) })
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

func match(p Pred, get func(Field) (domain.Value, bool)) bool {
	switch p := p.(type) {
	case *Cmp:
		v, ok := get(p.Field)
		return ok && compare(v, p)
	case Exists:
		_, ok := get(p.Field)
		return ok
	case PNot:
		return !match(p.P, get)
	case PAnd:
		return match(p.L, get) && match(p.R, get)
	case POr:
		return match(p.L, get) || match(p.R, get)
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

func spanField(sp *projection.Span, f Field) (domain.Value, bool) {
	switch {
	case f.Scope == "trace" && f.Name == "id":
		return str(sp.TraceID)
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
	case Int, Float:
		x, ok := number(v)
		if !ok {
			return 0, false
		}
		y := lit.F
		if lit.Kind == Int {
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
