package correlator_test

import (
	"context"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/correlator"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/test/sweet"
)

type corrDeps struct {
	c   *correlator.Correlator
	out chan domain.Event
}

func correlatorFactory(t *testing.T) corrDeps {
	t.Helper()

	out := make(chan domain.Event, 32)
	c := correlator.New(observability.LoggingTracer{}, out)
	done := make(chan error, 1)
	go func() {
		done <- c.Run(context.Background())
	}()
	t.Cleanup(func() {
		c.Stop()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Run did not return after Stop")
		}
	})

	return corrDeps{c: c, out: out}
}

func TestStopClosesEvents(t *testing.T) {
	sweet.Run(t, "Stop unblocks Run and closes outbound events", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Stop()

		if _, ok := recvEvent(t, d.out); ok {
			t.Fatal("expected outbound events to be closed")
		}
	})
}

func TestUntaggedLogAssignedToGlobalOrphan(t *testing.T) {
	sweet.Run(t, "no IDs yield LogReceived on the global synthetic orphan", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Logs() <- domain.LogLine{Body: "untagged", Source: domain.SourceStdout}

		up := mustUpserted(t, d.out)
		if !up.Synthetic || up.Name != "orphan" {
			t.Fatalf("upsert %#v", up)
		}

		rec := mustReceived(t, d.out)
		if rec.Body != "untagged" {
			t.Fatalf("body=%q", rec.Body)
		}
		if rec.Source != domain.SourceStdout {
			t.Fatalf("source=%q", rec.Source)
		}
		if rec.ID == "" {
			t.Fatal("expected log id")
		}
		if !rec.Synthetic {
			t.Fatal("expected synthetic orphan")
		}
		if rec.SpanName != "orphan" {
			t.Fatalf("span name=%q", rec.SpanName)
		}
		if rec.TraceID == "" || rec.SpanID == "" {
			t.Fatal("expected hydrated orphan ids")
		}
		if rec.SpanID != up.SpanID || rec.TraceID != up.TraceID {
			t.Fatalf("received %#v upsert %#v", rec, up)
		}

		as := mustAssigned(t, d.out)
		if as.ID != rec.ID || as.TraceID != rec.TraceID || as.SpanID != rec.SpanID || !as.Synthetic {
			t.Fatalf("assigned %#v received %#v", as, rec)
		}
	})
}

func TestUntaggedLinesShareOneGlobalOrphan(t *testing.T) {
	sweet.Run(t, "two untagged lines share the same orphan span", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Logs() <- domain.LogLine{Body: "a", Source: domain.SourceStdout}
		_ = mustUpserted(t, d.out)
		first := mustReceived(t, d.out)
		_ = mustAssigned(t, d.out)

		d.c.Logs() <- domain.LogLine{Body: "b", Source: domain.SourceStderr}
		second := mustReceived(t, d.out)

		if first.TraceID != second.TraceID || first.SpanID != second.SpanID {
			t.Fatalf("orphans differ: %#v vs %#v", first, second)
		}
		if second.Source != domain.SourceStderr {
			t.Fatalf("source=%q", second.Source)
		}
		if first.ID == second.ID {
			t.Fatal("log ids should be unique")
		}
	})
}

func TestJoinLogToExistingSpan(t *testing.T) {
	sweet.Run(t, "span_id already in the tree attaches to that span", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "s1", Name: "work"}
		up := mustUpserted(t, d.out)
		if up.Synthetic || up.SpanID != "s1" || up.Name != "work" {
			t.Fatalf("upsert %#v", up)
		}

		d.c.Logs() <- domain.LogLine{Body: "joined", Source: domain.SourceStdout, TraceID: "t1", SpanID: "s1"}
		rec := mustReceived(t, d.out)
		if rec.Synthetic || rec.SpanID != "s1" || rec.TraceID != "t1" || rec.SpanName != "work" {
			t.Fatalf("received %#v", rec)
		}

		as := mustAssigned(t, d.out)
		if as.ID != rec.ID || as.SpanID != "s1" || as.Synthetic {
			t.Fatalf("assigned %#v", as)
		}
	})
}

func TestTraceOnlyUnknownUsesGlobalOrphan(t *testing.T) {
	sweet.Run(t, "trace_id only on an unseen trace uses the global orphan", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Logs() <- domain.LogLine{Body: "lone-trace", Source: domain.SourceStdout, TraceID: "t-unknown"}
		up := mustUpserted(t, d.out)
		if !up.Synthetic {
			t.Fatal("expected global orphan upsert")
		}

		rec := mustReceived(t, d.out)
		if !rec.Synthetic || rec.TraceID != up.TraceID || rec.SpanID != up.SpanID {
			t.Fatalf("received %#v upsert %#v", rec, up)
		}
		if rec.TraceID == "t-unknown" {
			t.Fatal("must not invent a per-trace orphan for an unseen trace")
		}

		_ = mustAssigned(t, d.out)
	})
}

func TestTraceOnlyKnownUsesPerTraceOrphan(t *testing.T) {
	sweet.Run(t, "trace_id only on a known trace uses that trace's orphan", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "s1", Name: "work"}
		_ = mustUpserted(t, d.out)

		d.c.Logs() <- domain.LogLine{Body: "trace-only", Source: domain.SourceStdout, TraceID: "t1"}
		orphan := mustUpserted(t, d.out)
		if !orphan.Synthetic || orphan.TraceID != "t1" || orphan.SpanID == "s1" {
			t.Fatalf("orphan upsert %#v", orphan)
		}

		rec := mustReceived(t, d.out)
		if !rec.Synthetic || rec.TraceID != "t1" || rec.SpanID != orphan.SpanID || rec.SpanName != "orphan" {
			t.Fatalf("received %#v", rec)
		}

		as := mustAssigned(t, d.out)
		if as.ID != rec.ID || as.SpanID != orphan.SpanID || !as.Synthetic {
			t.Fatalf("assigned %#v", as)
		}
	})
}

func TestPendingThenAssignedWhenSpanArrives(t *testing.T) {
	sweet.Run(t, "unseen span_id stays pending then LogAssigned with a stable id", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Logs() <- domain.LogLine{Body: "early", Source: domain.SourceStdout, TraceID: "t1", SpanID: "s1"}
		rec := mustReceived(t, d.out)
		if rec.ID == "" {
			t.Fatal("expected log id")
		}
		if rec.SpanID != "" || rec.SpanName != "" || rec.Synthetic {
			t.Fatalf("pending received must not guess a span: %#v", rec)
		}

		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "s1", Name: "work"}
		up := mustUpserted(t, d.out)
		if up.SpanID != "s1" || up.Synthetic {
			t.Fatalf("upsert %#v", up)
		}

		as := mustAssigned(t, d.out)
		if as.ID != rec.ID {
			t.Fatalf("log id changed: received %q assigned %q", rec.ID, as.ID)
		}
		if as.SpanID != "s1" || as.TraceID != "t1" || as.SpanName != "work" || as.Synthetic {
			t.Fatalf("assigned %#v", as)
		}
	})
}

func TestPendingResolvedOnStop(t *testing.T) {
	sweet.Run(t, "Stop drains pending onto the global orphan when the trace was never seen", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Logs() <- domain.LogLine{Body: "waiting", Source: domain.SourceStderr, SpanID: "missing"}
		rec := mustReceived(t, d.out)
		if rec.SpanID != "" {
			t.Fatalf("pending received %#v", rec)
		}

		d.c.Stop()

		up := mustUpserted(t, d.out)
		if !up.Synthetic {
			t.Fatalf("upsert %#v", up)
		}

		as := mustAssigned(t, d.out)
		if as.ID != rec.ID || !as.Synthetic || as.SpanID != up.SpanID {
			t.Fatalf("assigned %#v received %#v upsert %#v", as, rec, up)
		}
	})
}

func TestPendingDrainKnownTraceUsesTraceOrphan(t *testing.T) {
	sweet.Run(t, "drain parks pending on the per-trace orphan when the trace was seen", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "s1", Name: "work"}
		_ = mustUpserted(t, d.out)

		d.c.Logs() <- domain.LogLine{Body: "late", Source: domain.SourceStdout, TraceID: "t1", SpanID: "never"}
		rec := mustReceived(t, d.out)

		d.c.Stop()

		orphan := mustUpserted(t, d.out)
		if !orphan.Synthetic || orphan.TraceID != "t1" || orphan.SpanID == "s1" {
			t.Fatalf("orphan %#v", orphan)
		}

		as := mustAssigned(t, d.out)
		if as.ID != rec.ID || as.SpanID != orphan.SpanID || !as.Synthetic {
			t.Fatalf("assigned %#v", as)
		}
	})
}

func TestChildBeforeParentEmitsUpsertThenLink(t *testing.T) {
	sweet.Run(t, "child before parent upserts the child then links when the parent arrives", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "child", ParentSpanID: "parent", Name: "child"}
		child := mustUpserted(t, d.out)
		if child.SpanID != "child" || child.Synthetic {
			t.Fatalf("child upsert %#v", child)
		}

		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "parent", Name: "parent"}
		par := mustUpserted(t, d.out)
		if par.SpanID != "parent" {
			t.Fatalf("parent upsert %#v", par)
		}

		link := mustParentLinked(t, d.out)
		if link.SpanID != "child" || link.ParentSpanID != "parent" {
			t.Fatalf("link %#v", link)
		}
	})
}

func TestAssignmentNotReparented(t *testing.T) {
	sweet.Run(t, "LogAssigned is sticky when a matching trace appears later", correlatorFactory, func(t *testing.T, d corrDeps) {
		d.c.Logs() <- domain.LogLine{Body: "early-trace", Source: domain.SourceStdout, TraceID: "t1"}
		_ = mustUpserted(t, d.out)
		rec := mustReceived(t, d.out)
		as := mustAssigned(t, d.out)
		if !as.Synthetic {
			t.Fatal("expected global orphan assignment")
		}

		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "s1", Name: "work"}
		up := mustUpserted(t, d.out)
		if up.SpanID != "s1" {
			t.Fatalf("upsert %#v", up)
		}

		d.c.Stop()
		ev, ok := recvEvent(t, d.out)
		if ok {
			if _, isAssigned := ev.(domain.LogAssigned); isAssigned {
				t.Fatalf("assignment reparented: first %#v later %#v received %#v", as, ev, rec)
			}
		}
	})
}

func recvEvent(t *testing.T, out <-chan domain.Event) (domain.Event, bool) {
	t.Helper()

	select {
	case ev, ok := <-out:
		return ev, ok
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
		return nil, false
	}
}

func mustReceived(t *testing.T, out <-chan domain.Event) domain.LogReceived {
	t.Helper()

	ev, ok := recvEvent(t, out)
	if !ok {
		t.Fatal("channel closed")
	}
	rec, ok := ev.(domain.LogReceived)
	if !ok {
		t.Fatalf("got %T", ev)
	}

	return rec
}

func mustAssigned(t *testing.T, out <-chan domain.Event) domain.LogAssigned {
	t.Helper()

	ev, ok := recvEvent(t, out)
	if !ok {
		t.Fatal("channel closed")
	}
	as, ok := ev.(domain.LogAssigned)
	if !ok {
		t.Fatalf("got %T", ev)
	}

	return as
}

func mustUpserted(t *testing.T, out <-chan domain.Event) domain.SpanUpserted {
	t.Helper()

	ev, ok := recvEvent(t, out)
	if !ok {
		t.Fatal("channel closed")
	}
	up, ok := ev.(domain.SpanUpserted)
	if !ok {
		t.Fatalf("got %T", ev)
	}

	return up
}

func mustParentLinked(t *testing.T, out <-chan domain.Event) domain.SpanParentLinked {
	t.Helper()

	ev, ok := recvEvent(t, out)
	if !ok {
		t.Fatal("channel closed")
	}
	link, ok := ev.(domain.SpanParentLinked)
	if !ok {
		t.Fatalf("got %T", ev)
	}

	return link
}
