package projection_test

import (
	"slices"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
)

const settle = 500 * time.Millisecond

// at applies evs with the forest's clock at t.
func at(f *projection.Forest, t time.Time, evs ...domain.Event) {
	f.SetNow(t)
	for _, ev := range evs {
		f.Apply(ev)
	}
}

func TestSettle(t *testing.T) {
	t0 := time.Unix(1000, 0)

	t.Run("settled, then live again, then settled again", func(t *testing.T) {
		f := projection.New()
		at(f, t0, domain.SpanUpserted{TraceID: "a", SpanID: "r"}, domain.SpanClosed{TraceID: "a", SpanID: "r"})

		f.SetNow(t0.Add(settle - time.Millisecond))
		if got := f.Settle(settle); len(got) != 0 {
			t.Fatalf("settled too early: %v", got)
		}
		f.SetNow(t0.Add(settle))
		if got := f.Settle(settle); !slices.Equal(got, []string{"a"}) || f.State("a") != projection.Settled {
			t.Fatalf("Settle = %v, state %v", got, f.State("a"))
		}
		rev := f.Revision("a")

		later := t0.Add(2 * settle)
		at(f, later, domain.SpanUpserted{TraceID: "a", SpanID: "late"}, domain.SpanClosed{TraceID: "a", SpanID: "late"})
		if f.State("a") != projection.Live || f.Revision("a") <= rev {
			t.Fatalf("late event: state %v revision %d (was %d)", f.State("a"), f.Revision("a"), rev)
		}
		f.SetNow(later.Add(settle))
		if got := f.Settle(settle); !slices.Equal(got, []string{"a"}) {
			t.Fatalf("second Settle = %v", got)
		}
	})

	t.Run("an open real span keeps the trace live; synthetic and declared spans do not", func(t *testing.T) {
		f := projection.New()
		at(f, t0,
			domain.SpanUpserted{TraceID: "open", SpanID: "r"},
			domain.SpanUpserted{TraceID: "orphans", SpanID: "o", Synthetic: true},
			domain.LogReceived{ID: "l", DeclaredTraceID: "orphans", DeclaredSpanID: "never"},
		)
		f.SetNow(t0.Add(time.Hour))
		if got := f.Settle(settle); !slices.Equal(got, []string{"orphans"}) {
			t.Fatalf("Settle = %v", got)
		}
	})

	t.Run("an evicted trace is forgotten", func(t *testing.T) {
		f := projection.New()
		at(f, t0, domain.SpanUpserted{TraceID: "a", SpanID: "r"}, domain.ForestEvicted{TraceID: "a"})
		f.SetNow(t0.Add(time.Hour))
		if got := f.Settle(settle); len(got) != 0 {
			t.Fatalf("Settle = %v", got)
		}
		if ids := slices.Collect(f.TraceIDs()); len(ids) != 0 {
			t.Fatalf("TraceIDs = %v", ids)
		}
	})

	t.Run("a drain finalizes live and settled traces", func(t *testing.T) {
		f := projection.New()
		at(f, t0,
			domain.SpanUpserted{TraceID: "done", SpanID: "r"}, domain.SpanClosed{TraceID: "done", SpanID: "r"},
			domain.SpanUpserted{TraceID: "busy", SpanID: "r"},
		)
		f.SetNow(t0.Add(settle))
		f.Settle(settle)
		if got := f.Finalize(); !slices.Equal(got, []string{"busy", "done"}) {
			t.Fatalf("Finalize = %v", got)
		}
		if f.State("busy") != projection.Final || f.State("done") != projection.Final {
			t.Fatal("not final")
		}
	})
}

func TestIncomplete(t *testing.T) {
	f := projection.New()
	f.Apply(domain.SpanUpserted{TraceID: "a", SpanID: "c"})
	f.Apply(domain.SpanParentLinked{TraceID: "a", SpanID: "c", ParentSpanID: "gone"})
	f.Apply(domain.LogReceived{ID: "l", DeclaredTraceID: "a", DeclaredSpanID: "never"})
	f.Apply(domain.SpanUpserted{TraceID: "b", SpanID: "r"})
	f.Apply(domain.SpanClosed{TraceID: "b", SpanID: "r"})

	if got := f.Incomplete("a"); !slices.Equal(got, []string{"missing_span", "unknown_parent"}) {
		t.Fatalf("a: %v", got)
	}
	if got := f.Incomplete("b"); len(got) != 0 {
		t.Fatalf("b: %v", got)
	}
	f.Apply(domain.ForestShed{})
	f.Finalize()
	if got := f.Incomplete("a"); !slices.Equal(got, []string{"missing_span", "open_span", "shed", "unknown_parent"}) {
		t.Fatalf("a after shed and drain: %v", got)
	}
}

func TestOneTrace(t *testing.T) {
	f := projection.New()
	f.Apply(domain.SpanUpserted{TraceID: "a", SpanID: "r"})
	f.Apply(domain.LogReceived{ID: "la", TraceID: "a", SpanID: "r"})
	f.Apply(domain.SpanUpserted{TraceID: "b", SpanID: "r"})
	f.Apply(domain.LogReceived{ID: "lb", TraceID: "b", SpanID: "r"})

	r := projection.OneTrace(f, "a")
	if r.Len() != 1 || r.Span("b", "r") != nil || r.Span("a", "r") == nil {
		t.Fatalf("spans: Len %d", r.Len())
	}
	if r.Log("lb") != nil || r.Log("la") == nil {
		t.Fatal("logs")
	}
	if ids := slices.Collect(r.TraceIDs()); !slices.Equal(ids, []string{"a"}) {
		t.Fatalf("TraceIDs = %v", ids)
	}
	if c := projection.Copy(r); c.Len() != 1 || c.Span("a", "r") == nil {
		t.Fatalf("Copy holds %d spans", c.Len())
	}
}
