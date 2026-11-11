// Package projection is the read model of a run: the spans, logs and traces
// that the correlator's events describe. Apply folds one event in; the
// accessors read the result. It does no I/O and keeps the raw data, so every
// view can select, mask and order it differently. See design/PIPELINE.md.
//
// A Forest is not safe for concurrent use. One goroutine applies events and
// reads it.
package projection

import (
	"iter"
	"maps"
	"slices"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
)

// Span is one span as the events have described it so far.
type Span struct {
	ID        string
	TraceID   string
	ParentID  string
	Name      string
	Component string
	// StatusMsg is the status message or, for a span in error with none, the
	// message of the last exception recorded on it. It is raw: views sanitise
	// it for display.
	StatusMsg string
	// Declared is true for a placeholder: a span that logs named but that has
	// not arrived itself.
	Declared  bool
	Synthetic bool
	Start     time.Time
	End       time.Time
	// Observed is when the forest first heard of the span (see SetNow).
	Observed time.Time
	Status   domain.SpanStatus
	Closed   bool
	// Order is the arrival order of the span among spans and logs.
	Order      int
	Attributes []domain.Attr
	// Kind and Resource are kept for queries; views do not show them.
	Kind     domain.SpanKind
	Resource []domain.Attr
	// Logs are the IDs of the logs assigned to the span, in arrival order.
	Logs []string
}

// Log is one log line and its current assignment.
type Log struct {
	ID      string
	Body    string
	TraceID string
	SpanID  string
	// Source, severity and attributes are kept for queries.
	Source         domain.Source
	SeverityNumber int32
	SeverityText   string
	Attributes     []domain.Attr
}

// Forest is the projection of one run.
type Forest struct {
	spans     map[domain.SpanKey]*Span
	logs      map[string]*Log
	revisions map[string]int
	traces    map[string]*traceInfo
	nextOrder int
	version   int
	now       time.Time
	shed      bool
	final     bool
}

// New returns an empty forest.
func New() *Forest {
	return &Forest{
		spans:     make(map[domain.SpanKey]*Span),
		logs:      make(map[string]*Log),
		revisions: make(map[string]int),
		traces:    make(map[string]*traceInfo),
	}
}

// SetNow sets the time that Apply records as Observed on spans it has not
// seen before.
func (f *Forest) SetNow(t time.Time) { f.now = t }

// Version counts the events applied. A reader can compare it to tell whether
// what it built from the forest is still current.
func (f *Forest) Version() int { return f.version }

// Revision counts the events that changed the trace.
func (f *Forest) Revision(traceID string) int { return f.revisions[traceID] }

// Len is the number of spans, placeholders and synthetic spans included.
func (f *Forest) Len() int { return len(f.spans) }

// Span returns the span, or nil if the forest has none with that key.
func (f *Forest) Span(traceID, spanID string) *Span {
	return f.spans[domain.SpanKey{TraceID: traceID, SpanID: spanID}]
}

// Log returns the log, or nil if the forest has none with that ID.
func (f *Forest) Log(id string) *Log { return f.logs[id] }

// Spans yields every span in no particular order.
func (f *Forest) Spans() iter.Seq[*Span] {
	return func(yield func(*Span) bool) {
		for _, sp := range f.spans {
			if !yield(sp) {
				return
			}
		}
	}
}

// Logs yields every log in no particular order.
func (f *Forest) Logs() iter.Seq[*Log] {
	return func(yield func(*Log) bool) {
		for _, l := range f.logs {
			if !yield(l) {
				return
			}
		}
	}
}

// Clone returns a deep copy that holds only the spans keep accepts (all of
// them if keep is nil). The owner of the copy may change it; the original
// does not see the changes.
func (f *Forest) Clone(keep func(*Span) bool) *Forest {
	c := &Forest{
		spans:     make(map[domain.SpanKey]*Span, len(f.spans)),
		logs:      make(map[string]*Log, len(f.logs)),
		revisions: make(map[string]int, len(f.revisions)),
		traces:    make(map[string]*traceInfo, len(f.traces)),
		nextOrder: f.nextOrder,
		version:   f.version,
		now:       f.now,
		shed:      f.shed,
		final:     f.final,
	}
	for id, t := range f.traces {
		cp := *t
		c.traces[id] = &cp
	}
	for k, sp := range f.spans {
		if keep != nil && !keep(sp) {
			continue
		}
		cp := *sp
		cp.Attributes = slices.Clone(sp.Attributes)
		cp.Logs = slices.Clone(sp.Logs)
		c.spans[k] = &cp
	}
	for id, l := range f.logs {
		cp := *l
		c.logs[id] = &cp
	}
	maps.Copy(c.revisions, f.revisions)
	return c
}

// Apply folds one event into the forest.
func (f *Forest) Apply(ev domain.Event) {
	f.version++
	switch ev := ev.(type) {
	case domain.SpanUpserted:
		f.touch(ev.TraceID)
		sp := f.ensureSpan(ev.TraceID, ev.SpanID)
		sp.TraceID = ev.TraceID
		sp.Name = ev.Name
		sp.Component = ev.Component
		sp.Declared = false
		sp.Synthetic = ev.Synthetic
		sp.Start = ev.StartTime
		sp.End = ev.EndTime
		sp.Status = ev.Status
		sp.StatusMsg = statusMessage(ev)
		sp.Attributes = ev.Attributes
		sp.Kind = ev.Kind
		sp.Resource = ev.Resource.Attributes
	case domain.SpanParentLinked:
		f.touch(ev.TraceID)
		f.ensureSpan(ev.TraceID, ev.SpanID).ParentID = ev.ParentSpanID
	case domain.SpanClosed:
		f.touch(ev.TraceID)
		f.ensureSpan(ev.TraceID, ev.SpanID).Closed = true
	case domain.LogReceived:
		f.nextOrder++
		spanID := ev.SpanID
		if spanID == "" {
			spanID = ev.DeclaredSpanID
		}
		traceID := ev.TraceID
		if traceID == "" {
			traceID = ev.DeclaredTraceID
		}
		f.touch(traceID)
		f.logs[ev.ID] = &Log{
			ID: ev.ID, Body: ev.Body, TraceID: traceID, SpanID: spanID,
			Source: ev.Source, SeverityNumber: ev.SeverityNumber, SeverityText: ev.SeverityText,
			Attributes: ev.Attributes,
		}
		if ev.SpanID == "" && ev.DeclaredSpanID != "" {
			sp := f.ensureSpan(ev.DeclaredTraceID, ev.DeclaredSpanID)
			sp.TraceID = ev.DeclaredTraceID
			sp.Component = ev.Component
			sp.Name = "span/" + shortID(ev.DeclaredSpanID)
			sp.Declared = true
		}
		f.attachLog(ev.ID, "", "", traceID, spanID)
	case domain.LogAssigned:
		log := f.logs[ev.ID]
		if log == nil {
			return
		}
		oldTrace, old := log.TraceID, log.SpanID
		f.touch(oldTrace)
		f.touch(ev.TraceID)
		log.TraceID = ev.TraceID
		log.SpanID = ev.SpanID
		f.attachLog(ev.ID, oldTrace, old, ev.TraceID, ev.SpanID)
		oldKey := domain.SpanKey{TraceID: oldTrace, SpanID: old}
		sp := f.spans[oldKey]
		if (oldTrace != ev.TraceID || old != ev.SpanID) && sp != nil &&
			sp.Declared && len(sp.Logs) == 0 {
			delete(f.spans, oldKey)
		}
	case domain.ForestEvicted:
		f.removeTrace(ev.TraceID)
	case domain.ForestShed:
		f.shed = true
	}
}

// touch counts a change to the trace and makes a settled trace live again.
func (f *Forest) touch(traceID string) {
	if traceID == "" {
		return
	}
	f.revisions[traceID]++
	t := f.traces[traceID]
	if t == nil {
		t = &traceInfo{}
		f.traces[traceID] = t
	}
	t.touched = f.now
	if t.state == Settled {
		t.state = Live
	}
}

func (f *Forest) ensureSpan(traceID, id string) *Span {
	key := domain.SpanKey{TraceID: traceID, SpanID: id}
	if sp := f.spans[key]; sp != nil {
		return sp
	}
	f.nextOrder++
	sp := &Span{ID: id, TraceID: traceID, Observed: f.now, Order: f.nextOrder}
	f.spans[key] = sp
	return sp
}

func (f *Forest) attachLog(id, oldTraceID, oldSpanID, traceID, spanID string) {
	if oldSpanID != "" && (oldTraceID != traceID || oldSpanID != spanID) {
		if old := f.Span(oldTraceID, oldSpanID); old != nil {
			if i := slices.Index(old.Logs, id); i >= 0 {
				old.Logs = slices.Delete(old.Logs, i, i+1)
			}
		}
	}
	if spanID == "" {
		return
	}
	sp := f.ensureSpan(traceID, spanID)
	if slices.Contains(sp.Logs, id) {
		return
	}
	sp.Logs = append(sp.Logs, id)
}

func (f *Forest) removeTrace(traceID string) {
	for key, sp := range f.spans {
		if sp.TraceID != traceID {
			continue
		}
		for _, logID := range sp.Logs {
			delete(f.logs, logID)
		}
		delete(f.spans, key)
	}
	delete(f.revisions, traceID)
	delete(f.traces, traceID)
}

// statusMessage is the span's status message. For a span in error with
// none, it falls back to the message of the last exception recorded on it,
// which is where many SDKs put the error and leave the status message
// empty.
func statusMessage(ev domain.SpanUpserted) string {
	if ev.StatusMessage != "" {
		return ev.StatusMessage
	}
	if ev.Status != domain.SpanStatusError {
		return ""
	}
	for i := len(ev.Events) - 1; i >= 0; i-- {
		if ev.Events[i].Name != "exception" {
			continue
		}
		for _, a := range ev.Events[i].Attributes {
			if a.Key == "exception.message" {
				return a.Value.String()
			}
		}
	}
	return ""
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
