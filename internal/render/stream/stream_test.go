package stream_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/praytel/internal/render/stream"
	"github.com/barry-hennessy/test/sweet"
)

func TestRunReturnsWhenEventsClose(t *testing.T) {
	sweet.Run(t, "closed event channel ends Run", closedStreamFactory, func(t *testing.T, s stream.Stream) {
		if err := s.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func closedStreamFactory(t *testing.T) stream.Stream {
	t.Helper()

	ch := make(chan domain.Event)
	close(ch)

	return stream.New(observability.LoggingTracer{}, ch, &bytes.Buffer{}, mustPass(t))
}

func TestLogReceivedPrintedOnceAppendOnly(t *testing.T) {
	type deps struct {
		s   stream.Stream
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 2)

		return deps{
			s:   stream.New(observability.LoggingTracer{}, ch, &out, mustPass(t)),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "LogReceived is printed once and not rewritten", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.s.Run(context.Background())
		}()

		d.ch <- domain.LogReceived{
			ID:        "1",
			Body:      "hello-line",
			Source:    domain.SourceStdout,
			TraceID:   "t1",
			SpanID:    "s1",
			SpanName:  "orphan",
			Synthetic: true,
		}
		d.ch <- domain.LogAssigned{ID: "1", TraceID: "t1", SpanID: "s1", SpanName: "orphan", Synthetic: true}
		close(d.ch)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return")
		}

		got := d.out.String()
		if strings.Count(got, "hello-line") != 1 {
			t.Fatalf("want body once, got %q", got)
		}
		if !strings.Contains(got, "stdout") || !strings.Contains(got, "orphan") {
			t.Fatalf("want greppable prefix, got %q", got)
		}
		if strings.Contains(got, "\x1b[") {
			t.Fatalf("non-tty should be monochrome: %q", got)
		}
	})
}

func TestLogAssignedHydratesWithoutRewriting(t *testing.T) {
	type deps struct {
		s   stream.Stream
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 2)

		return deps{
			s:   stream.New(observability.LoggingTracer{}, ch, &out, mustPass(t)),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "LogAssigned appends a hydrate line and leaves the original", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.s.Run(context.Background())
		}()

		d.ch <- domain.LogReceived{
			ID:     "1",
			Body:   "later-line",
			Source: domain.SourceStdout,
		}
		d.ch <- domain.LogAssigned{ID: "1", TraceID: "t1", SpanID: "s1", SpanName: "work"}
		close(d.ch)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return")
		}

		got := d.out.String()
		if strings.Count(got, "later-line") != 2 {
			t.Fatalf("want body twice, got %q", got)
		}

		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("want two lines, got %q", got)
		}
		if strings.Contains(lines[0], "work") || strings.Contains(lines[0], "t1") {
			t.Fatalf("original line should stay unhydrated: %q", lines[0])
		}
		if !strings.Contains(lines[1], "work") || !strings.Contains(lines[1], "t1") || !strings.Contains(lines[1], "s1") {
			t.Fatalf("hydrate line missing context: %q", lines[1])
		}
		if strings.Contains(got, "\x1b[") {
			t.Fatalf("non-tty should be monochrome: %q", got)
		}
	})
}

func TestFocusHidesSiblingAndLateAdmitsParent(t *testing.T) {
	type deps struct {
		s   stream.Stream
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 16)
		filt, err := filter.New(filter.Query{Focus: []string{"orders"}})
		if err != nil {
			t.Fatal(err)
		}

		return deps{
			s:   stream.New(observability.LoggingTracer{}, ch, &out, filt),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "stream prints admitted lines only and late-admits without rewrite", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.s.Run(context.Background())
		}()

		d.ch <- domain.SpanUpserted{SpanID: "orders", Name: "orders"}
		d.ch <- domain.SpanUpserted{SpanID: "root", Name: "http"}
		d.ch <- domain.SpanUpserted{SpanID: "health", Name: "health"}
		d.ch <- domain.LogReceived{ID: "1", Body: "orders-line", Source: domain.SourceStdout, SpanID: "orders", SpanName: "orders"}
		d.ch <- domain.LogReceived{ID: "2", Body: "root-line", Source: domain.SourceStdout, SpanID: "root", SpanName: "http"}
		d.ch <- domain.LogReceived{ID: "3", Body: "health-line", Source: domain.SourceStdout, SpanID: "health", SpanName: "health"}
		d.ch <- domain.SpanParentLinked{SpanID: "orders", ParentSpanID: "root"}
		close(d.ch)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return")
		}

		got := d.out.String()
		if strings.Count(got, "orders-line") != 1 {
			t.Fatalf("orders-line once, got %q", got)
		}
		if strings.Count(got, "root-line") != 1 {
			t.Fatalf("late-admitted root-line once, got %q", got)
		}
		if strings.Contains(got, "health-line") {
			t.Fatalf("sibling leaked: %q", got)
		}

		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("want two append-only lines, got %q", got)
		}
		if !strings.Contains(lines[0], "orders-line") {
			t.Fatalf("first line should stay orders-line: %q", lines[0])
		}
		if !strings.Contains(lines[1], "root-line") {
			t.Fatalf("late admit should append: %q", lines[1])
		}
	})
}

func TestForestPressureWarnsOnLoggerNotStream(t *testing.T) {
	type deps struct {
		s      stream.Stream
		out    *bytes.Buffer
		logBuf *bytes.Buffer
		ch     chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out, logBuf bytes.Buffer
		ch := make(chan domain.Event, 2)
		lt := observability.LoggingTracer{Log: *slog.New(slog.NewTextHandler(&logBuf, nil))}

		return deps{
			s:      stream.New(lt, ch, &out, mustPass(t)),
			out:    &out,
			logBuf: &logBuf,
			ch:     ch,
		}
	}

	sweet.Run(t, "ForestEvicted and ForestShed warn on the logger, not the stream", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.s.Run(context.Background())
		}()

		d.ch <- domain.ForestEvicted{}
		d.ch <- domain.ForestShed{}
		close(d.ch)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return")
		}

		if d.out.Len() != 0 {
			t.Fatalf("pressure events leaked onto the stream: %q", d.out.String())
		}

		logs := d.logBuf.String()
		if !strings.Contains(logs, "evicted oldest closed trace") {
			t.Fatalf("missing eviction warning: %q", logs)
		}
		if !strings.Contains(logs, "shed newest inbound data") {
			t.Fatalf("missing shed warning: %q", logs)
		}
	})
}

func TestOTLPControlCharsStripped(t *testing.T) {
	type deps struct {
		s   stream.Stream
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 1)

		return deps{
			s:   stream.New(observability.LoggingTracer{}, ch, &out, mustPass(t)),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "OTLP span name CSI and body C0 do not reach the stream", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.s.Run(context.Background())
		}()

		d.ch <- domain.LogReceived{
			ID:       "1",
			Body:     "ok\x1b[31mhid\nnext",
			Source:   domain.SourceOTLP,
			TraceID:  "t1",
			SpanID:   "s1",
			SpanName: "work\x1b[0m",
		}
		close(d.ch)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return")
		}

		got := d.out.String()
		if strings.Contains(got, "\x1b") || strings.Contains(got, "[31m") || strings.Contains(got, "[0m") {
			t.Fatalf("escape leaked: %q", got)
		}
		if strings.Count(got, "\n") != 1 {
			t.Fatalf("want one output line, got %q", got)
		}
		if !strings.Contains(got, "ok") || !strings.Contains(got, "hid") || !strings.Contains(got, "next") {
			t.Fatalf("sanitized body missing text: %q", got)
		}
	})
}

func mustPass(t *testing.T) *filter.Filter {
	t.Helper()

	f, err := filter.New(filter.Query{})
	if err != nil {
		t.Fatal(err)
	}

	return f
}
