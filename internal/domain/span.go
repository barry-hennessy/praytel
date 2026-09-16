package domain

import "time"

// SpanStatus is the terminal outcome reported by OpenTelemetry.
type SpanStatus uint8

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

// Span is one OTLP span after the receiver/capture edge. Join lives in the correlator.
type Span struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
	Component    string
	StartTime    time.Time
	EndTime      time.Time
	Status       SpanStatus
	// Ended is true when the span has finished. A trace is closed when every
	// real (non-synthetic) span in it has ended.
	Ended bool
}
