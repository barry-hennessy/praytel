package tree_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/praytel/internal/render/tree"
	"github.com/barry-hennessy/test/sweet"
)

func TestRunReturnsWhenEventsClose(t *testing.T) {
	sweet.Run(t, "closed event channel ends Run", closedTreeFactory, func(t *testing.T, tr tree.Tree) {
		if err := tr.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func closedTreeFactory(t *testing.T) tree.Tree {
	t.Helper()

	ch := make(chan domain.Event)
	close(ch)

	return tree.New(observability.LoggingTracer{}, ch, &bytes.Buffer{}, mustPass(t))
}

func TestSimpleHierarchyPaintsOnTraceClose(t *testing.T) {
	type deps struct {
		tr  tree.Tree
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 16)

		return deps{
			tr:  tree.New(observability.LoggingTracer{}, ch, &out, mustPass(t)),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "root and child indent when trace closes", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.tr.Run(context.Background())
		}()

		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "root", Name: "http", Component: "billing"}
		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Name: "orders", Component: "billing"}
		d.ch <- domain.SpanParentLinked{TraceID: "t1", SpanID: "orders", ParentSpanID: "root"}
		d.ch <- domain.LogReceived{ID: "L1", Body: "GET /orders", Source: domain.SourceStdout, TraceID: "t1", SpanID: "root"}
		d.ch <- domain.LogReceived{ID: "L2", Body: "created", Source: domain.SourceStdout, TraceID: "t1", SpanID: "orders"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "root"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "orders"}
		d.ch <- domain.DrainComplete{}
		close(d.ch)

		waitRun(t, done)

		got := d.out.String()
		if !strings.Contains(got, "=== trace t1 ===") {
			t.Fatalf("missing trace header:\n%s", got)
		}
		if !strings.Contains(got, "http [billing]") {
			t.Fatalf("missing root span:\n%s", got)
		}
		if !strings.Contains(got, "  orders [billing]") {
			t.Fatalf("missing indented child:\n%s", got)
		}
		if !strings.Contains(got, "GET /orders") || !strings.Contains(got, "created") {
			t.Fatalf("missing logs:\n%s", got)
		}
	})
}

func TestChildBeforeParentPaintsCorrectly(t *testing.T) {
	type deps struct {
		tr  tree.Tree
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 16)

		return deps{
			tr:  tree.New(observability.LoggingTracer{}, ch, &out, mustPass(t)),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "child before parent still indents after close", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.tr.Run(context.Background())
		}()

		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "child", Name: "db"}
		d.ch <- domain.LogReceived{ID: "L1", Body: "query", Source: domain.SourceStdout, SpanID: "child"}
		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "root", Name: "http"}
		d.ch <- domain.SpanParentLinked{TraceID: "t1", SpanID: "child", ParentSpanID: "root"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "child"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "root"}
		d.ch <- domain.DrainComplete{}
		close(d.ch)

		waitRun(t, done)

		got := d.out.String()
		if !strings.Contains(got, "http") || !strings.Contains(got, "  db") {
			t.Fatalf("expected nested db under http:\n%s", got)
		}
	})
}

func TestFocusPathOmitsSibling(t *testing.T) {
	type deps struct {
		tr  tree.Tree
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 32)
		f, err := filter.New(filter.Query{Focus: []string{"orders"}})
		if err != nil {
			t.Fatal(err)
		}

		return deps{
			tr:  tree.New(observability.LoggingTracer{}, ch, &out, f),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "focus path keeps ancestor and descendant not sibling", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.tr.Run(context.Background())
		}()

		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "root", Name: "http"}
		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Name: "orders"}
		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "health", Name: "health"}
		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "db", Name: "db"}
		d.ch <- domain.SpanParentLinked{SpanID: "orders", ParentSpanID: "root"}
		d.ch <- domain.SpanParentLinked{SpanID: "health", ParentSpanID: "root"}
		d.ch <- domain.SpanParentLinked{SpanID: "db", ParentSpanID: "orders"}
		d.ch <- domain.LogReceived{ID: "Lroot", Body: "root-log", Source: domain.SourceStdout, SpanID: "root"}
		d.ch <- domain.LogReceived{ID: "Lorders", Body: "orders-log", Source: domain.SourceStdout, SpanID: "orders"}
		d.ch <- domain.LogReceived{ID: "Lhealth", Body: "health-log", Source: domain.SourceStdout, SpanID: "health"}
		d.ch <- domain.LogReceived{ID: "Ldb", Body: "db-log", Source: domain.SourceStdout, SpanID: "db"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "root"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "orders"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "health"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "db"}
		d.ch <- domain.DrainComplete{}
		close(d.ch)

		waitRun(t, done)

		got := d.out.String()
		if strings.Contains(got, "health-log") {
			t.Fatalf("sibling leaked:\n%s", got)
		}
		if !strings.Contains(got, "root-log") || !strings.Contains(got, "orders-log") || !strings.Contains(got, "db-log") {
			t.Fatalf("path logs missing:\n%s", got)
		}
	})
}

func TestGlobalOrphanAtDrain(t *testing.T) {
	type deps struct {
		tr  tree.Tree
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 8)

		return deps{
			tr:  tree.New(observability.LoggingTracer{}, ch, &out, mustPass(t)),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "untagged lines appear under untagged header at drain", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.tr.Run(context.Background())
		}()

		d.ch <- domain.SpanUpserted{SpanID: "orphan", Name: "orphan", Synthetic: true}
		d.ch <- domain.LogReceived{ID: "L1", Body: "plain", Source: domain.SourceStdout, SpanID: "orphan", Synthetic: true}
		d.ch <- domain.DrainComplete{}
		close(d.ch)

		waitRun(t, done)

		got := d.out.String()
		if strings.Contains(got, "=== trace") {
			t.Fatalf("orphan should not print as trace:\n%s", got)
		}
		if !strings.Contains(got, "=== untagged ===") || !strings.Contains(got, "plain") {
			t.Fatalf("missing untagged section:\n%s", got)
		}
	})
}

func TestContinuedTracePrintsSecondBlock(t *testing.T) {
	type deps struct {
		tr  tree.Tree
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 16)

		return deps{
			tr:  tree.New(observability.LoggingTracer{}, ch, &out, mustPass(t)),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "reopened trace prints continued header", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.tr.Run(context.Background())
		}()

		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "s1", Name: "first"}
		d.ch <- domain.LogReceived{ID: "L1", Body: "one", Source: domain.SourceStdout, SpanID: "s1"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "s1"}
		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "s2", Name: "second"}
		d.ch <- domain.LogReceived{ID: "L2", Body: "two", Source: domain.SourceStdout, SpanID: "s2"}
		d.ch <- domain.SpanClosed{TraceID: "t1", SpanID: "s2"}
		d.ch <- domain.DrainComplete{}
		close(d.ch)

		waitRun(t, done)

		got := d.out.String()
		if strings.Count(got, "=== trace t1 ===") != 1 {
			t.Fatalf("expected one initial header:\n%s", got)
		}
		if !strings.Contains(got, "(continued)") {
			t.Fatalf("missing continued header:\n%s", got)
		}
		if !strings.Contains(got, "one") || !strings.Contains(got, "two") {
			t.Fatalf("missing logs:\n%s", got)
		}
	})
}

func TestDrainPartialTrace(t *testing.T) {
	type deps struct {
		tr  tree.Tree
		out *bytes.Buffer
		ch  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		var out bytes.Buffer
		ch := make(chan domain.Event, 8)

		return deps{
			tr:  tree.New(observability.LoggingTracer{}, ch, &out, mustPass(t)),
			out: &out,
			ch:  ch,
		}
	}

	sweet.Run(t, "open trace still prints at drain", factory, func(t *testing.T, d deps) {
		done := make(chan error, 1)
		go func() {
			done <- d.tr.Run(context.Background())
		}()

		d.ch <- domain.SpanUpserted{TraceID: "t1", SpanID: "s1", Name: "live"}
		d.ch <- domain.LogReceived{ID: "L1", Body: "still-open", Source: domain.SourceStdout, SpanID: "s1"}
		d.ch <- domain.DrainComplete{}
		close(d.ch)

		waitRun(t, done)

		got := d.out.String()
		if !strings.Contains(got, "=== trace t1 ===") || !strings.Contains(got, "still-open") {
			t.Fatalf("partial trace missing:\n%s", got)
		}
	})
}

func waitRun(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
	}
}

func mustPass(t *testing.T) *filter.Filter {
	t.Helper()

	f, err := filter.New(filter.Query{})
	if err != nil {
		t.Fatal(err)
	}

	return f
}
