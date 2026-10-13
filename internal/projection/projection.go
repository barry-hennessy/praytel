package projection

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/query"
)

// State describes a trace's emission lifecycle.
type State string

const (
	// Live means the trace may still change or is not yet quiet.
	Live State = "live"
	// Settled means all known real spans are closed and the trace was quiet.
	Settled State = "settled"
	// Final means the run has drained; finality does not imply completeness.
	Final State = "final"
)

// IncompleteReason describes known data loss or structural incompleteness.
type IncompleteReason string

const (
	// IncompleteEvicted means the trace was subject to forest eviction.
	IncompleteEvicted IncompleteReason = "evicted"
	// IncompleteShed means telemetry was shed during the run.
	IncompleteShed IncompleteReason = "shed"
	// IncompleteUnknownParent means a final span names a parent not in the trace.
	IncompleteUnknownParent IncompleteReason = "unknown_parent"
	// IncompleteRunFailed means collection failed before the run drained normally.
	IncompleteRunFailed IncompleteReason = "run_failed"
)

// Evidence identifies records directly satisfying a query and records selected
// by its view.
type Evidence struct {
	SpanIDs         []string `json:"span_ids"`
	LogIDs          []string `json:"log_ids"`
	SelectedSpanIDs []string `json:"selected_span_ids"`
	SelectedLogIDs  []string `json:"selected_log_ids"`
}

// Result is a matching trace revision suitable for machine output.
type Result struct {
	TraceID           string
	Revision          uint64
	State             State
	Evidence          Evidence
	MatchedAt         time.Time
	IncompleteReasons []IncompleteReason
}

// RunSummary is compact run metadata. Complete only reports whether known data
// loss occurred; callers must also require Final before asserting absence.
type RunSummary struct {
	State             State
	Traces            int
	Complete          bool
	IncompleteReasons []IncompleteReason
}

// TraceSummary is compact per-trace metadata.
type TraceSummary struct {
	TraceID             string
	Revision            uint64
	State               State
	SpanCount, LogCount int
	UpdatedAt           time.Time
	IncompleteReasons   []IncompleteReason
}

// Page reports bounded-result accounting.
type Page struct {
	Matched, Returned int
	Truncated         bool
}

// TopologySpan is a depth-first structural span view without attributes or log bodies.
type TopologySpan struct {
	ID        string `json:"span_id"`
	ParentID  string `json:"parent_span_id,omitempty"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Depth     int    `json:"depth"`
	Duration  int64  `json:"duration_ns"`
	Closed    bool   `json:"closed"`
	Synthetic bool   `json:"synthetic"`
}

// Topology contains a bounded trace skeleton.
type Topology struct {
	TraceID string
	Page    Page
	Spans   []TopologySpan
}

// SpanDetail is the rich, typed representation returned for requested spans.
type SpanDetail struct {
	TopologySpan
	StatusMessage string
	Start, End    time.Time
	Resource      domain.Resource
	Scope         domain.Scope
	Attributes    []domain.Attr
	Events        []domain.SpanEvent
	Links         []domain.SpanLink
}

// LogDetail is the rich, typed representation of a final assigned log.
type LogDetail struct {
	ID, TraceID, SpanID, Body    string
	Source                       domain.Source
	Timestamp, ObservedTimestamp time.Time
	SeverityNumber               int32
	SeverityText                 string
	BodyValue                    domain.Value
	Resource                     domain.Resource
	Scope                        domain.Scope
	Attributes                   []domain.Attr
}

// ErrNotFound indicates that a requested trace or record does not exist.
var ErrNotFound = errors.New("projection: not found")

// InvalidLimitError reports a non-positive bounded-read limit.
type InvalidLimitError struct{ Limit int }

// Error implements error.
func (e *InvalidLimitError) Error() string { return "projection: limit must be positive" }

type span struct {
	e      domain.SpanUpserted
	parent string
	closed bool
}
type logRecord struct {
	e           domain.LogReceived
	trace, span string
	assigned    bool
}
type trace struct {
	id                        string
	spans                     map[string]*span
	logs                      map[string]*logRecord
	revision, settledRevision uint64
	updated                   time.Time
	state                     State
	reasons                   map[IncompleteReason]bool
}

// Projection is a thread-safe read model for a single run.
type Projection struct {
	mu          sync.RWMutex
	traces      map[string]*trace
	logs        map[string]*logRecord
	final, shed bool
}

// New constructs an empty live projection.
func New() *Projection {
	return &Projection{traces: map[string]*trace{}, logs: map[string]*logRecord{}}
}

func (p *Projection) get(id string) *trace {
	t := p.traces[id]
	if t == nil {
		t = &trace{id: id, spans: map[string]*span{}, logs: map[string]*logRecord{}, state: Live, reasons: map[IncompleteReason]bool{}}
		p.traces[id] = t
		if p.shed {
			t.reasons[IncompleteShed] = true
		}
	}
	return t
}
func touch(t *trace, at time.Time) { t.revision++; t.updated = at; t.state = Live }

// Observe incorporates event at the projection clock time observedAt.
func (p *Projection) Observe(event domain.Event, observedAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch e := event.(type) {
	case domain.SpanUpserted:
		t := p.get(e.TraceID)
		s := t.spans[e.SpanID]
		if s == nil {
			s = &span{}
			t.spans[e.SpanID] = s
		}
		s.e = e
		s.parent = e.ParentSpanID
		if !e.EndTime.IsZero() {
			s.closed = true
		}
		touch(t, observedAt)
	case domain.SpanParentLinked:
		t := p.get(e.TraceID)
		s := t.spans[e.SpanID]
		if s == nil {
			s = &span{}
			t.spans[e.SpanID] = s
		}
		s.parent = e.ParentSpanID
		touch(t, observedAt)
	case domain.SpanClosed:
		t := p.get(e.TraceID)
		s := t.spans[e.SpanID]
		if s == nil {
			s = &span{}
			t.spans[e.SpanID] = s
		}
		s.closed = true
		touch(t, observedAt)
	case domain.LogReceived:
		l := p.logs[e.ID]
		if l == nil {
			l = &logRecord{}
			p.logs[e.ID] = l
		}
		old := l.trace
		l.e = e
		if !l.assigned {
			l.trace = e.TraceID
			l.span = e.SpanID
		}
		if old != "" && old != l.trace {
			delete(p.get(old).logs, e.ID)
			touch(p.get(old), observedAt)
		}
		if l.trace != "" {
			t := p.get(l.trace)
			t.logs[e.ID] = l
			touch(t, observedAt)
		}
	case domain.LogAssigned:
		l := p.logs[e.ID]
		if l == nil {
			l = &logRecord{}
			p.logs[e.ID] = l
		}
		old := l.trace
		if old != "" && old != e.TraceID {
			delete(p.get(old).logs, e.ID)
			touch(p.get(old), observedAt)
		}
		l.trace, l.span = e.TraceID, e.SpanID
		l.assigned = true
		t := p.get(e.TraceID)
		t.logs[e.ID] = l
		touch(t, observedAt)
	case domain.ForestEvicted:
		t := p.get(e.TraceID)
		t.reasons[IncompleteEvicted] = true
		touch(t, observedAt)
	case domain.ForestShed:
		p.shed = true
		for _, t := range p.traces {
			t.reasons[IncompleteShed] = true
			touch(t, observedAt)
		}
	case domain.DrainComplete:
		p.final = true
		for _, t := range p.traces {
			t.state = Final
		}
	}
}

func ready(t *trace, now time.Time, quiet time.Duration) bool {
	real := 0
	for _, s := range t.spans {
		if !s.e.Synthetic {
			real++
			if !s.closed {
				return false
			}
		}
	}
	return real > 0 && now.Sub(t.updated) >= quiet && t.settledRevision != t.revision
}

// ReadySettled atomically evaluates and marks every settlement-ready matching
// revision. Non-matches are also marked, preventing repeated evaluation.
func (p *Projection) ReadySettled(q *query.Query, now time.Time, quiet time.Duration) []Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Result
	for _, t := range sortedTraces(p.traces) {
		if p.final || !ready(t, now, quiet) {
			continue
		}
		t.settledRevision = t.revision
		t.state = Settled
		if r := q.Eval(snapshot(t)); r.Match {
			out = append(out, result(t, Settled, now, r, false))
		}
	}
	return out
}

// FinalResults evaluates all matching traces as final, normally after drain.
func (p *Projection) FinalResults(q *query.Query, matchedAt time.Time) []Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.final = true
	var out []Result
	for _, t := range sortedTraces(p.traces) {
		t.state = Final
		if r := q.Eval(snapshot(t)); r.Match {
			out = append(out, result(t, Final, matchedAt, r, true))
		}
	}
	return out
}

// Results evaluates q against the current immutable snapshot of every trace.
// It does not settle or finalize traces and is suitable for live views. Results
// are returned in trace-ID order and carry each trace's current lifecycle state.
func (p *Projection) Results(q *query.Query, matchedAt time.Time) []Result {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []Result
	for _, t := range sortedTraces(p.traces) {
		if r := q.Eval(snapshot(t)); r.Match {
			out = append(out, result(t, t.state, matchedAt, r, p.final))
		}
	}
	return out
}

func result(t *trace, s State, at time.Time, r query.Result, final bool) Result {
	return Result{t.id, t.revision, s, Evidence{r.EvidenceSpanIDs, r.EvidenceLogIDs, r.SelectedSpanIDs, r.SelectedLogIDs}, at, reasons(t, final)}
}
func reasons(t *trace, final bool) []IncompleteReason {
	m := map[IncompleteReason]bool{}
	for x := range t.reasons {
		m[x] = true
	}
	if final {
		for _, s := range t.spans {
			if s.parent != "" && t.spans[s.parent] == nil {
				m[IncompleteUnknownParent] = true
			}
		}
	}
	out := make([]IncompleteReason, 0, len(m))
	for x := range m {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func sortedTraces(m map[string]*trace) []*trace {
	a := make([]*trace, 0, len(m))
	for _, t := range m {
		a = append(a, t)
	}
	sort.Slice(a, func(i, j int) bool { return a[i].id < a[j].id })
	return a
}

func snapshot(t *trace) query.Trace {
	qt := query.Trace{ID: t.id}
	ids := make([]string, 0, len(t.spans))
	var min, max time.Time
	for id := range t.spans {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := t.spans[id]
		qs := query.Span{ID: id, ParentID: s.parent, Name: s.e.Name, Kind: kind(s.e.Kind), Status: status(s.e.Status), StatusMessage: s.e.StatusMessage, Resource: resource(s.e.Resource), Scope: scope(s.e.Scope), Attributes: attrs(s.e.Attributes)}
		qs.Duration = duration(s.e.StartTime, s.e.EndTime)
		qt.Spans = append(qt.Spans, qs)
		if min.IsZero() || s.e.StartTime.Before(min) {
			min = s.e.StartTime
		}
		if s.e.EndTime.After(max) {
			max = s.e.EndTime
		}
	}
	lids := make([]string, 0, len(t.logs))
	for id := range t.logs {
		lids = append(lids, id)
	}
	sort.Strings(lids)
	for _, id := range lids {
		l := t.logs[id]
		body := l.e.Body
		if l.e.BodyValue.Kind() != domain.ValueInvalid {
			body = l.e.BodyValue.String()
		}
		qt.Logs = append(qt.Logs, query.Log{ID: id, SpanID: l.span, Body: body, Source: string(l.e.Source), SeverityNumber: int64(l.e.SeverityNumber), SeverityText: l.e.SeverityText, Resource: resource(l.e.Resource), Scope: scope(l.e.Scope), Attributes: attrs(l.e.Attributes)})
	}
	if !min.IsZero() && !max.IsZero() {
		qt.Duration = max.Sub(min).Nanoseconds()
	}
	return qt
}
func status(x domain.SpanStatus) string {
	return [...]string{"unset", "ok", "error"}[minInt(int(x), 2)]
}
func kind(x domain.SpanKind) string {
	a := []string{"unspecified", "internal", "server", "client", "producer", "consumer"}
	if int(x) >= len(a) {
		return "unspecified"
	}
	return a[x]
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func duration(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Nanoseconds()
}
func value(v domain.Value) query.Value {
	switch v.Kind() {
	case domain.ValueString:
		x, _ := v.AsString()
		return query.StringValue(x)
	case domain.ValueInt64:
		x, _ := v.AsInt64()
		return query.IntValue(x)
	case domain.ValueFloat64:
		x, _ := v.AsFloat64()
		return query.FloatValue(x)
	case domain.ValueBool:
		x, _ := v.AsBool()
		return query.BoolValue(x)
	case domain.ValueBytes:
		x, _ := v.AsBytes()
		return query.BytesValue(x)
	case domain.ValueArray:
		x, _ := v.AsArray()
		a := make([]query.Value, len(x))
		for i := range x {
			a[i] = value(x[i])
		}
		return query.ArrayValue(a)
	case domain.ValueMap:
		x, _ := v.AsMap()
		m := map[string]query.Value{}
		for _, a := range x {
			if _, ok := m[a.Key]; !ok {
				m[a.Key] = value(a.Value)
			}
		}
		return query.MapValue(m)
	}
	return query.Value{}
}
func attrs(a []domain.Attr) []query.Attr {
	o := make([]query.Attr, len(a))
	for i, x := range a {
		o[i] = query.Attr{Key: x.Key, Value: value(x.Value)}
	}
	return o
}
func resource(r domain.Resource) query.Resource {
	return query.Resource{Attributes: attrs(r.Attributes)}
}
func scope(s domain.Scope) query.Scope {
	return query.Scope{Name: s.Name, Version: s.Version, Attributes: attrs(s.Attributes)}
}

// Summary returns current run metadata.
func (p *Projection) Summary() RunSummary {
	p.mu.RLock()
	defer p.mu.RUnlock()
	st := Live
	if p.final {
		st = Final
	}
	m := map[IncompleteReason]bool{}
	if p.shed {
		m[IncompleteShed] = true
	}
	for _, t := range p.traces {
		for x := range t.reasons {
			m[x] = true
		}
	}
	rs := make([]IncompleteReason, 0, len(m))
	for x := range m {
		rs = append(rs, x)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	return RunSummary{st, len(p.traces), len(rs) == 0, rs}
}

// Traces returns summaries in trace-ID order.
func (p *Projection) Traces() []TraceSummary {
	p.mu.RLock()
	defer p.mu.RUnlock()
	o := []TraceSummary{}
	for _, t := range sortedTraces(p.traces) {
		o = append(o, TraceSummary{t.id, t.revision, t.state, len(t.spans), len(t.logs), t.updated, reasons(t, p.final)})
	}
	return o
}

func validLimit(n int) error {
	if n <= 0 {
		return &InvalidLimitError{n}
	}
	return nil
}

// TraceTopology returns a bounded deterministic depth-first skeleton.
func (p *Projection) TraceTopology(traceID string, limit int) (Topology, error) {
	if e := validLimit(limit); e != nil {
		return Topology{}, e
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	t := p.traces[traceID]
	if t == nil {
		return Topology{}, ErrNotFound
	}
	children := map[string][]string{}
	for id, s := range t.spans {
		children[s.parent] = append(children[s.parent], id)
	}
	for k := range children {
		sort.Strings(children[k])
	}
	all := []TopologySpan{}
	seen := map[string]bool{}
	var walk func(string, int)
	walk = func(id string, d int) {
		if seen[id] {
			return
		}
		seen[id] = true
		s := t.spans[id]
		all = append(all, TopologySpan{id, s.parent, s.e.Name, kind(s.e.Kind), status(s.e.Status), d, duration(s.e.StartTime, s.e.EndTime), s.closed, s.e.Synthetic})
		for _, c := range children[id] {
			walk(c, d+1)
		}
	}
	for _, id := range children[""] {
		walk(id, 0)
	}
	orph := make([]string, 0)
	for id := range t.spans {
		if !seen[id] {
			orph = append(orph, id)
		}
	}
	sort.Strings(orph)
	for _, id := range orph {
		walk(id, 0)
	}
	n := minInt(limit, len(all))
	return Topology{traceID, Page{len(all), n, n < len(all)}, all[:n]}, nil
}

// SpanDetails returns requested span details, bounded by limit, in request order.
func (p *Projection) SpanDetails(traceID string, ids []string, limit int) ([]SpanDetail, Page, error) {
	if e := validLimit(limit); e != nil {
		return nil, Page{}, e
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	t := p.traces[traceID]
	if t == nil {
		return nil, Page{}, ErrNotFound
	}
	n := minInt(limit, len(ids))
	o := make([]SpanDetail, 0, n)
	for _, id := range ids[:n] {
		s := t.spans[id]
		if s == nil {
			return nil, Page{}, ErrNotFound
		}
		o = append(o, SpanDetail{TopologySpan: TopologySpan{ID: id, ParentID: s.parent, Name: s.e.Name, Kind: kind(s.e.Kind), Status: status(s.e.Status), Duration: duration(s.e.StartTime, s.e.EndTime), Closed: s.closed, Synthetic: s.e.Synthetic}, StatusMessage: s.e.StatusMessage, Start: s.e.StartTime, End: s.e.EndTime, Resource: s.e.Resource, Scope: s.e.Scope, Attributes: append([]domain.Attr(nil), s.e.Attributes...), Events: append([]domain.SpanEvent(nil), s.e.Events...), Links: append([]domain.SpanLink(nil), s.e.Links...)})
	}
	return o, Page{len(ids), n, n < len(ids)}, nil
}

// Logs returns requested final log records, bounded by limit, in request order.
func (p *Projection) Logs(traceID string, ids []string, limit int) ([]LogDetail, Page, error) {
	if e := validLimit(limit); e != nil {
		return nil, Page{}, e
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	t := p.traces[traceID]
	if t == nil {
		return nil, Page{}, ErrNotFound
	}
	n := minInt(limit, len(ids))
	o := make([]LogDetail, 0, n)
	for _, id := range ids[:n] {
		l := t.logs[id]
		if l == nil {
			return nil, Page{}, ErrNotFound
		}
		body := l.e.Body
		if l.e.BodyValue.Kind() != domain.ValueInvalid {
			body = l.e.BodyValue.String()
		}
		o = append(o, LogDetail{id, l.trace, l.span, body, l.e.Source, l.e.Timestamp, l.e.ObservedTimestamp, l.e.SeverityNumber, l.e.SeverityText, l.e.BodyValue, l.e.Resource, l.e.Scope, append([]domain.Attr(nil), l.e.Attributes...)})
	}
	return o, Page{len(ids), n, n < len(ids)}, nil
}
