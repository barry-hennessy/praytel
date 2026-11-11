package projection

import (
	"iter"
	"slices"

	"github.com/barry-hennessy/praytel/internal/domain"
)

// Reader is the read-only face of a Forest. Stages wrap a Reader to change
// what a view sees without changing the forest (see design/PIPELINE.md,
// "Stages").
type Reader interface {
	Span(traceID, spanID string) *Span
	Log(id string) *Log
	Spans() iter.Seq[*Span]
	Logs() iter.Seq[*Log]
	Len() int
	Version() int
	Revision(traceID string) int
	TraceIDs() iter.Seq[string]
	State(traceID string) TraceState
	Incomplete(traceID string) []string
}

var _ Reader = (*Forest)(nil)

// Copy makes a Forest that holds what r shows. The owner of the copy may
// change it; r does not see the changes.
func Copy(r Reader) *Forest {
	if f, ok := r.(*Forest); ok {
		return f.Clone(nil)
	}
	c := New()
	c.version = r.Version()
	for sp := range r.Spans() {
		cp := *sp
		cp.Attributes = slices.Clone(sp.Attributes)
		cp.Logs = slices.Clone(sp.Logs)
		c.spans[domain.SpanKey{TraceID: sp.TraceID, SpanID: sp.ID}] = &cp
		c.nextOrder = max(c.nextOrder, sp.Order)
		if _, ok := c.revisions[sp.TraceID]; !ok {
			c.revisions[sp.TraceID] = r.Revision(sp.TraceID)
			c.traces[sp.TraceID] = &traceInfo{state: r.State(sp.TraceID)}
		}
	}
	for l := range r.Logs() {
		cp := *l
		c.logs[l.ID] = &cp
	}
	// A lens may hide logs that its spans still list.
	for _, sp := range c.spans {
		sp.Logs = slices.DeleteFunc(sp.Logs, func(id string) bool { return c.logs[id] == nil })
	}
	return c
}
