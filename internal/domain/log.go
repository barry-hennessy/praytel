package domain

import "time"

// Source is how a [LogLine] arrived.
type Source string

const (
	// SourceStdout identifies a line captured from the child standard output.
	SourceStdout Source = "stdout"
	// SourceStderr identifies a line captured from the child standard error.
	SourceStderr Source = "stderr"
	// SourceOTLP identifies a log received through OpenTelemetry.
	SourceOTLP Source = "otlp"
)

// LogLine is one captured or ingested log. Join logic lives in the correlator.
type LogLine struct {
	ID                string
	Timestamp         time.Time
	ObservedTimestamp time.Time
	SeverityNumber    int32
	SeverityText      string
	Body              Value
	Resource          Resource
	Scope             Scope
	Attributes        []Attr
	Source            Source
	Component         string
	TraceID           string
	SpanID            string
}
