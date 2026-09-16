package domain

import "time"

// Event is a value-copy message from the correlator to renderers.
// See ARCHITECTURE.md.
type Event interface {
	isEvent()
}

type (
	SpanUpserted struct {
		TraceID   string
		SpanID    string
		Name      string
		Component string
		Synthetic bool
		StartTime time.Time
		EndTime   time.Time
		Status    SpanStatus
	}
	SpanParentLinked struct {
		TraceID      string
		SpanID       string
		ParentSpanID string
	}
	SpanClosed struct {
		TraceID string
		SpanID  string
	}
	LogReceived struct {
		ID        string
		Body      string
		Source    Source
		Component string
		TraceID   string
		SpanID    string
		SpanName  string
		Synthetic bool
		// Declared IDs are the context carried by the log before final assignment.
		DeclaredTraceID string
		DeclaredSpanID  string
	}
	LogAssigned struct {
		ID        string
		TraceID   string
		SpanID    string
		SpanName  string
		Synthetic bool
	}
	DrainComplete struct{}
	ForestEvicted struct {
		TraceID string
	}
	ForestShed struct{}
)

func (SpanUpserted) isEvent()     {}
func (SpanParentLinked) isEvent() {}
func (SpanClosed) isEvent()       {}
func (LogReceived) isEvent()      {}
func (LogAssigned) isEvent()      {}
func (DrainComplete) isEvent()    {}
func (ForestEvicted) isEvent()    {}
func (ForestShed) isEvent()       {}
