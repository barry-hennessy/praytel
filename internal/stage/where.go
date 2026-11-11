package stage

import (
	"iter"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/query"
)

// Where keeps what a query selects. As a lens it is the "path" view: each
// selected span with its ancestors and descendants, and the logs on them
// (for a log query, only the selected logs). On events, only a query that
// one log decides (see query.RecordLocal) can filter: it drops the logs it
// does not select and passes everything else. Plan validation sends any
// other query to views that read the projection.
func Where(e query.Expr) Stage {
	k, local := query.RecordLocal(e)
	return &where{e: e, logLocal: local && k == query.KindLog}
}

type where struct {
	e        query.Expr
	logLocal bool
	dropped  map[string]bool
}

func (w *where) OnEvent(ev domain.Event, emit func(domain.Event)) {
	if !w.logLocal {
		emit(ev)
		return
	}
	switch e := ev.(type) {
	case domain.LogReceived:
		l := &projection.Log{
			ID: e.ID, Body: e.Body, TraceID: e.TraceID, SpanID: e.SpanID, Source: e.Source,
			SeverityNumber: e.SeverityNumber, SeverityText: e.SeverityText, Attributes: e.Attributes,
		}
		if l.TraceID == "" {
			l.TraceID = e.DeclaredTraceID
		}
		if !query.MatchesLog(w.e, l) {
			if w.dropped == nil {
				w.dropped = map[string]bool{}
			}
			w.dropped[e.ID] = true
			return
		}
	case domain.LogAssigned:
		if w.dropped[e.ID] {
			return
		}
	}
	emit(ev)
}

func (w *where) Lens(r projection.Reader) projection.Reader {
	return &whereReader{Reader: r, w: w, version: -1}
}

// whereReader shows what the query keeps, worked out again whenever the
// projection under it changes.
type whereReader struct {
	projection.Reader
	w       *where
	version int
	spans   map[domain.SpanKey]bool
	logs    map[string]bool
	traces  map[string]bool
}

func (r *whereReader) refresh() {
	if r.Version() == r.version {
		return
	}
	r.version = r.Version()
	r.spans = map[domain.SpanKey]bool{}
	r.logs = map[string]bool{}
	r.traces = map[string]bool{}
	for id, t := range query.TracesOf(r.Reader) {
		res := query.Eval(r.w.e, t)
		for spanID := range t.Path(res) {
			r.spans[domain.SpanKey{TraceID: id, SpanID: spanID}] = true
			r.traces[id] = true
		}
		for logID := range res.Logs {
			r.logs[logID] = true
		}
	}
	if !r.w.logLocal {
		// The logs of the spans on show.
		for l := range r.Reader.Logs() {
			if r.spans[domain.SpanKey{TraceID: l.TraceID, SpanID: l.SpanID}] {
				r.logs[l.ID] = true
			}
		}
	}
}

func (r *whereReader) Span(traceID, spanID string) *projection.Span {
	r.refresh()
	if !r.spans[domain.SpanKey{TraceID: traceID, SpanID: spanID}] {
		return nil
	}
	return r.Reader.Span(traceID, spanID)
}

func (r *whereReader) Spans() iter.Seq[*projection.Span] {
	r.refresh()
	return func(yield func(*projection.Span) bool) {
		for sp := range r.Reader.Spans() {
			if r.spans[domain.SpanKey{TraceID: sp.TraceID, SpanID: sp.ID}] && !yield(sp) {
				return
			}
		}
	}
}

func (r *whereReader) Log(id string) *projection.Log {
	r.refresh()
	if !r.logs[id] {
		return nil
	}
	return r.Reader.Log(id)
}

func (r *whereReader) Logs() iter.Seq[*projection.Log] {
	r.refresh()
	return func(yield func(*projection.Log) bool) {
		for l := range r.Reader.Logs() {
			if r.logs[l.ID] && !yield(l) {
				return
			}
		}
	}
}

func (r *whereReader) Len() int {
	r.refresh()
	return len(r.spans)
}

func (r *whereReader) TraceIDs() iter.Seq[string] {
	r.refresh()
	return func(yield func(string) bool) {
		for id := range r.Reader.TraceIDs() {
			if r.traces[id] && !yield(id) {
				return
			}
		}
	}
}
