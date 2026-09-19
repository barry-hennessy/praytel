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
		if line.Body.String() != "hello world" {
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
			if !strings.Contains(line.Body.String(), "{") {
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

func TestLogfmtExtractsIDsWithoutWarn(t *testing.T) {
	sweet.Run(t, "slog-shaped logfmt trace_id and span_id are parsed", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 1)
		body := `time=2026-01-01T00:00:00Z level=INFO msg="ok" trace_id=aaa span_id=bbb`
		err := capture.Copy(context.Background(), d.lt, domain.SourceStdout, strings.NewReader(body+"\n"), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		line := recvLine(t, out)
		if line.TraceID != "aaa" || line.SpanID != "bbb" {
			t.Fatalf("ids %#v", line)
		}
		if line.Body.String() != body {
			t.Fatalf("body=%q", line.Body)
		}
		if strings.Contains(d.logs.String(), "malformed") {
			t.Fatalf("valid logfmt warned: %s", d.logs.String())
		}
	})
}

func TestLogfmtQuotedValuesExtractIDs(t *testing.T) {
	sweet.Run(t, "quoted logfmt values still extract ids", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 1)
		body := `msg="hello world" trace_id=aaa span_id=bbb`
		err := capture.Copy(context.Background(), d.lt, domain.SourceStdout, strings.NewReader(body+"\n"), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		line := recvLine(t, out)
		if line.TraceID != "aaa" || line.SpanID != "bbb" {
			t.Fatalf("ids %#v", line)
		}
	})
}

func TestLogfmtTraceIDOnly(t *testing.T) {
	sweet.Run(t, "logfmt trace_id only leaves span empty", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 1)
		body := `msg=ok trace_id=aaa`
		err := capture.Copy(context.Background(), d.lt, domain.SourceStdout, strings.NewReader(body+"\n"), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		line := recvLine(t, out)
		if line.TraceID != "aaa" || line.SpanID != "" {
			t.Fatalf("ids %#v", line)
		}
	})
}

func TestLogfmtWithoutIDsIsPlainText(t *testing.T) {
	sweet.Run(t, "logfmt without ids is unstructured and does not warn", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 2)
		input := "foo=bar\nbaz=qux\n"
		err := capture.Copy(context.Background(), d.lt, domain.SourceStdout, strings.NewReader(input), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		for i := 0; i < 2; i++ {
			line := recvLine(t, out)
			if line.TraceID != "" || line.SpanID != "" {
				t.Fatalf("unexpected ids %#v", line)
			}
		}
		if strings.Contains(d.logs.String(), "malformed") {
			t.Fatalf("logfmt without ids warned: %s", d.logs.String())
		}
	})
}

func TestJSONPrecedenceOverLogfmt(t *testing.T) {
	sweet.Run(t, "JSON lines are not parsed as logfmt", capFactory, func(t *testing.T, d capDeps) {
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
		if line.Body.String() != body {
			t.Fatalf("body=%q", line.Body)
		}
		if strings.Contains(d.logs.String(), "malformed") {
			t.Fatalf("valid JSON warned: %s", d.logs.String())
		}
	})
}

func TestAnsiColorizedConsoleLineIsStrippedAndExtractsIDs(t *testing.T) {
	sweet.Run(t, "zerolog-style colorized console output loses its ANSI codes and still yields ids", capFactory, func(t *testing.T, d capDeps) {
		out := make(chan domain.LogLine, 1)
		esc := "\x1b"
		body := esc + "[2mSep 18 11:09:23.508" + esc + "[0m " +
			esc + "[93mWRN" + esc + "[0m " +
			esc + "[2menergy-meterpoints/appointment.go:164" + esc + "[0m " +
			"appointment service using stub adaptors " +
			esc + "[36mtrace_id=" + esc + "[0m" + esc + "[1mabc123" + esc + "[0m " +
			esc + "[36mspan_id=" + esc + "[0m" + esc + "[1mdef456" + esc + "[0m"

		err := capture.Copy(context.Background(), d.lt, domain.SourceStdout, strings.NewReader(body+"\n"), out, capture.NewWarnGate(time.Hour))
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}

		line := recvLine(t, out)
		if line.TraceID != "abc123" || line.SpanID != "def456" {
			t.Fatalf("ids %#v", line)
		}
		if strings.Contains(line.Body.String(), "\x1b") || strings.Contains(line.Body.String(), "[2m") || strings.Contains(line.Body.String(), "[0m") {
			t.Fatalf("body still contains ANSI codes: %q", line.Body)
		}
		if !strings.Contains(line.Body.String(), "appointment service using stub adaptors") {
			t.Fatalf("body lost its message text: %q", line.Body)
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

		if recvLine(t, out).Body.String() != "tail" {
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
