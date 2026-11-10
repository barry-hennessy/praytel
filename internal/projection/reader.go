package projection

import "iter"

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
}

var _ Reader = (*Forest)(nil)
