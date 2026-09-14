package stream_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
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

	return stream.New(observability.LoggingTracer{}, ch, &bytes.Buffer{})
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
			s:   stream.New(observability.LoggingTracer{}, ch, &out),
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
			s:   stream.New(observability.LoggingTracer{}, ch, &out),
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
