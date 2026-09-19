package domain

import "time"

// Event is a value-copy message from the correlator to renderers.
// See ARCHITECTURE.md.
type Event interface {
	isEvent()
}

type (
	// SpanUpserted announces the latest complete value-copy snapshot of a span.
	SpanUpserted struct {
		TraceID       string
		SpanID        string
		ParentSpanID  string
		Name          string
		Component     string
		Synthetic     bool
		StartTime     time.Time
		EndTime       time.Time
		Status        SpanStatus
		Kind          SpanKind
		StatusMessage string
		Resource      Resource
		Scope         Scope
		Attributes    []Attr
		Events        []SpanEvent
		Links         []SpanLink
	}
	// SpanParentLinked announces a parent relationship that became known after
	// the span was first observed.
	SpanParentLinked struct {
		TraceID      string
		SpanID       string
		ParentSpanID string
	}
	// SpanClosed announces that a span has ended.
	SpanClosed struct {
		TraceID string
		SpanID  string
	}
	// LogReceived announces a captured or OTLP log before or after assignment.
	// Declared IDs preserve the context carried by the original record; TraceID
	// and SpanID are the correlator's current assignment.
	LogReceived struct {
		ID                string
		Body              string
		Source            Source
		Component         string
		TraceID           string
		SpanID            string
		SpanName          string
		Synthetic         bool
		DeclaredTraceID   string
		DeclaredSpanID    string
		Timestamp         time.Time
		ObservedTimestamp time.Time
		SeverityNumber    int32
		SeverityText      string
		BodyValue         Value
		Resource          Resource
		Scope             Scope
		Attributes        []Attr
	}
	// LogAssigned announces a log's final, sticky trace/span assignment.
	LogAssigned struct {
		ID        string
		TraceID   string
		SpanID    string
		SpanName  string
		Synthetic bool
	}
	// DrainComplete announces that ingest is closed and no more events follow.
	DrainComplete struct{}
	// ForestEvicted announces that a trace was removed under memory pressure.
	ForestEvicted struct {
		TraceID string
	}
	// ForestShed announces that incoming telemetry was dropped under pressure.
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
