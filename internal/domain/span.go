package domain

// Span is one OTLP span after the receiver/capture edge. Join lives in the correlator.
type Span struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
}
