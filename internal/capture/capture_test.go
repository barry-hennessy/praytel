package capture_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/capture"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/test/sweet"
)

type capDeps struct {
	lt   observability.LoggingTracer
	logs *bytes.Buffer
}

func capFactory(t *testing.T) capDeps {
	t.Helper()

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	return capDeps{
		lt:   observability.LoggingTracer{Log: *log},
		logs: &buf,
	}
}

func TestPlainTextNoWarnAndKeepsBody(t *testing.T) {
	sweet.Run(t, "plain text is not an error", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 1)
		err := capture.Copy(context.Background(), d.lt, domain.SourceStdout, strings.NewReader("hello world\n"), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		line := recvLine(t, out)
		if line.Body != "hello world" {
			t.Fatalf("body=%q", line.Body)
		}
		if line.Source != domain.SourceStdout {
			t.Fatalf("source=%q", line.Source)
		}
		if line.TraceID != "" || line.SpanID != "" {
			t.Fatalf("unexpected ids %#v", line)
		}
		if strings.Contains(d.logs.String(), "malformed") {
			t.Fatalf("plain text warned: %s", d.logs.String())
		}
	})
}

func TestMalformedJSONWarnsRateLimited(t *testing.T) {
	sweet.Run(t, "broken JSON warns once when gated", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 4)
		input := "{not json\n{also broken\n{still broken\n"
		err := capture.Copy(context.Background(), d.lt, domain.SourceStderr, strings.NewReader(input), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		for i := 0; i < 3; i++ {
			line := recvLine(t, out)
			if !strings.Contains(line.Body, "{") {
				t.Fatalf("body=%q", line.Body)
			}
			if line.Source != domain.SourceStderr {
				t.Fatalf("source=%q", line.Source)
			}
		}

		warns := strings.Count(d.logs.String(), "malformed json")
		if warns != 1 {
			t.Fatalf("warns=%d log=%s", warns, d.logs.String())
		}
	})
}

func TestValidJSONExtractsIDsWithoutWarn(t *testing.T) {
	sweet.Run(t, "JSON trace_id and span_id are parsed", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 1)
		body := `{"msg":"ok","trace_id":"aaa","span_id":"bbb"}`
		err := capture.Copy(context.Background(), d.lt, domain.SourceStdout, strings.NewReader(body+"\n"), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		line := recvLine(t, out)
		if line.TraceID != "aaa" || line.SpanID != "bbb" {
			t.Fatalf("ids %#v", line)
		}
		if line.Body != body {
			t.Fatalf("body=%q", line.Body)
		}
		if strings.Contains(d.logs.String(), "malformed") {
			t.Fatalf("valid JSON warned: %s", d.logs.String())
		}
	})
}

func TestEOFWithoutNewline(t *testing.T) {
	sweet.Run(t, "last line without newline is kept", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 1)
		err := capture.Copy(context.Background(), d.lt, domain.SourceStdout, strings.NewReader("tail"), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		if recvLine(t, out).Body != "tail" {
			t.Fatal("missing tail line")
		}
	})
}

func recvLine(t *testing.T, out <-chan domain.LogLine) domain.LogLine {
	t.Helper()

	select {
	case line := <-out:
		return line
	default:
		t.Fatal("expected a log line")
		return domain.LogLine{}
	}
}
