package domain

// Source is how a [LogLine] arrived.
type Source string

const (
	SourceStdout Source = "stdout"
	SourceStderr Source = "stderr"
	SourceOTLP   Source = "otlp"
)

// LogLine is one captured or ingested log. Join logic lives in the correlator.
type LogLine struct {
	ID      string
	Body    string
	Source  Source
	TraceID string
	SpanID  string
}
