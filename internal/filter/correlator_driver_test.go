package filter_test

import (
	"context"
	"testing"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/correlator"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/test/sweet"
)

func TestCorrelatorEventsDriveFocusAdmit(t *testing.T) {
	type deps struct {
		c    *correlator.Correlator
		filt *filter.Filter
		out  chan domain.Event
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		out := make(chan domain.Event, 32)
		c := correlator.New(observability.LoggingTracer{}, out, correlator.Drain{}, correlator.Limits{})
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

		f, err := filter.New(filter.Query{Focus: []string{"orders"}})
		if err != nil {
			t.Fatal(err)
		}

		return deps{c: c, filt: f, out: out}
	}

	sweet.Run(t, "correlator events through filter admit path logs only", factory, func(t *testing.T, d deps) {
		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "root", Name: "http"}
		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "orders", ParentSpanID: "root", Name: "orders"}
		d.c.Spans() <- domain.Span{TraceID: "t1", SpanID: "health", ParentSpanID: "root", Name: "health"}
		d.c.Logs() <- domain.LogLine{ID: "Lroot", Body: "root", Source: domain.SourceStdout, TraceID: "t1", SpanID: "root"}
		d.c.Logs() <- domain.LogLine{ID: "Lorders", Body: "orders", Source: domain.SourceStdout, TraceID: "t1", SpanID: "orders"}
		d.c.Logs() <- domain.LogLine{ID: "Lhealth", Body: "health", Source: domain.SourceStdout, TraceID: "t1", SpanID: "health"}
		d.c.Stop()

		var admitted []string
		deadline := time.After(2 * time.Second)
		for {
			select {
			case ev, ok := <-d.out:
				if !ok {
					if !containsAll(admitted, "Lroot", "Lorders") {
						t.Fatalf("admitted=%v", admitted)
					}
					if containsAll(admitted, "Lhealth") {
						t.Fatalf("sibling admitted: %v", admitted)
					}

					return
				}

				for _, id := range d.filt.Observe(ev) {
					admitted = append(admitted, id)
				}
			case <-deadline:
				t.Fatalf("timeout, admitted=%v", admitted)
			}
		}
	})
}

func containsAll(got []string, want ...string) bool {
	set := make(map[string]struct{}, len(got))
	for _, g := range got {
		set[g] = struct{}{}
	}

	for _, w := range want {
		if _, ok := set[w]; !ok {
			return false
		}
	}

	return true
}
