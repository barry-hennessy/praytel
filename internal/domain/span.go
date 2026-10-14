package domain

import "time"

// SpanStatus is the terminal outcome reported by OpenTelemetry.
type SpanStatus uint8

const (
	// SpanStatusUnset indicates that no status was recorded.
	SpanStatusUnset SpanStatus = iota
	// SpanStatusOK indicates an explicitly successful operation.
	SpanStatusOK
	// SpanStatusError indicates an operation that ended in error.
	SpanStatusError
)

// Attr is a single telemetry attribute key/value pair, in source order.
type Attr struct {
	Key   string
	Value Value
}

// Resource describes the entity that produced telemetry.
type Resource struct{ Attributes []Attr }

// Scope identifies the instrumentation scope that produced telemetry.
type Scope struct {
	Name, Version string
	Attributes    []Attr
}

// SpanKey uniquely identifies a span within a run.
type SpanKey struct{ TraceID, SpanID string }

// SpanKind describes the relationship between a span and its caller.
type SpanKind uint8

const (
	// SpanKindUnspecified indicates that no span kind was recorded.
	SpanKindUnspecified SpanKind = iota
	// SpanKindInternal represents work internal to one process.
	SpanKindInternal
	// SpanKindServer represents handling a synchronous request.
	SpanKindServer
	// SpanKindClient represents making a synchronous request.
	SpanKindClient
	// SpanKindProducer represents initiating asynchronous work.
	SpanKindProducer
	// SpanKindConsumer represents processing asynchronous work.
	SpanKindConsumer
)

// SpanEvent is a timestamped event attached to a span.
type SpanEvent struct {
	Name                   string
	Timestamp              time.Time
	Attributes             []Attr
	DroppedAttributesCount uint32
}

// SpanLink is a link from a span to another span context.
type SpanLink struct {
	TraceID, SpanID, TraceState string
	Attributes                  []Attr
	DroppedAttributesCount      uint32
}

// Span is one OTLP span after the receiver/capture edge. Join lives in the correlator.
type Span struct {
	TraceID       string
	SpanID        string
	ParentSpanID  string
	Name          string
	Component     string
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
	// Ended is true when the span has finished. A trace is closed when every
	// real (non-synthetic) span in it has ended.
	Ended bool
}
