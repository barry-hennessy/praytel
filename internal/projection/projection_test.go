package projection_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/test/sweet"
)

func forest(t *testing.T) *projection.Forest {
	t.Helper()
	return projection.New()
}

func apply(f *projection.Forest, evs ...domain.Event) *projection.Forest {
	for _, ev := range evs {
		f.Apply(ev)
	}
	return f
}

func mustSpan(t *testing.T, f *projection.Forest, traceID, spanID string) *projection.Span {
	t.Helper()
	sp := f.Span(traceID, spanID)
	if sp == nil {
		t.Fatalf("no span %s/%s", traceID, spanID)
	}
	return sp
}

func TestSpanUpserted(t *testing.T) {
	start := time.Unix(10, 0)
	end := time.Unix(11, 0)
	attrs := []domain.Attr{{Key: "k", Value: domain.StringValue("v")}}

	sweet.Run(t, "copies the span's fields", forest, func(t *testing.T, f *projection.Forest) {
		apply(f, domain.SpanUpserted{
			TraceID: "t", SpanID: "s", Name: "get", Component: "api",
			StartTime: start, EndTime: end, Status: domain.SpanStatusOK,
			StatusMessage: "fine", Attributes: attrs,
		})
		sp := mustSpan(t, f, "t", "s")
		want := projection.Span{
			ID: "s", TraceID: "t", Name: "get", Component: "api",
			StatusMsg: "fine", Start: start, End: end,
			Status: domain.SpanStatusOK, Order: 1, Attributes: attrs,
		}
		if !reflect.DeepEqual(*sp, want) {
			t.Fatalf("span\n got %+v\nwant %+v", *sp, want)
		}
	})

	sweet.Run(t, "an error with no status message takes the last exception's message", forest, func(t *testing.T, f *projection.Forest) {
		exc := func(msg string) domain.SpanEvent {
			return domain.SpanEvent{Name: "exception", Attributes: []domain.Attr{
				{Key: "exception.message", Value: domain.StringValue(msg)},
			}}
		}
		apply(f, domain.SpanUpserted{
			TraceID: "t", SpanID: "s", Status: domain.SpanStatusError,
			Events: []domain.SpanEvent{exc("first"), {Name: "retry"}, exc("last\tone")},
		})
		if got := mustSpan(t, f, "t", "s").StatusMsg; got != "last\tone" {
			t.Fatalf("StatusMsg = %q, want the raw last exception message", got)
		}
	})

	sweet.Run(t, "a span that is not in error has no fallback message", forest, func(t *testing.T, f *projection.Forest) {
		apply(f, domain.SpanUpserted{
			TraceID: "t", SpanID: "s", Status: domain.SpanStatusOK,
			Events: []domain.SpanEvent{{Name: "exception", Attributes: []domain.Attr{
				{Key: "exception.message", Value: domain.StringValue("ignored")},
			}}},
		})
		if got := mustSpan(t, f, "t", "s").StatusMsg; got != "" {
			t.Fatalf("StatusMsg = %q, want empty", got)
		}
	})

	sweet.Run(t, "hydrates a declared placeholder", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.LogReceived{ID: "l", DeclaredTraceID: "t", DeclaredSpanID: "s", Component: "api"},
			domain.SpanUpserted{TraceID: "t", SpanID: "s", Name: "get", Component: "api"},
		)
		sp := mustSpan(t, f, "t", "s")
		if sp.Declared || sp.Name != "get" {
			t.Fatalf("span = %+v, want hydrated", *sp)
		}
	})
}

func TestSpanParentLinkedAndClosed(t *testing.T) {
	sweet.Run(t, "set the parent and close the span", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.SpanUpserted{TraceID: "t", SpanID: "c"},
			domain.SpanParentLinked{TraceID: "t", SpanID: "c", ParentSpanID: "p"},
			domain.SpanClosed{TraceID: "t", SpanID: "c"},
		)
		sp := mustSpan(t, f, "t", "c")
		if sp.ParentID != "p" || !sp.Closed {
			t.Fatalf("span = %+v", *sp)
		}
		if f.Span("t", "p") != nil {
			t.Fatal("linking to a parent must not create the parent")
		}
	})

	sweet.Run(t, "create the span if it has not arrived", forest, func(t *testing.T, f *projection.Forest) {
		apply(f, domain.SpanClosed{TraceID: "t", SpanID: "s"})
		if sp := mustSpan(t, f, "t", "s"); !sp.Closed || sp.Order != 1 {
			t.Fatalf("span = %+v", *sp)
		}
	})
}

func TestLogs(t *testing.T) {
	sweet.Run(t, "a log attaches to its span", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.SpanUpserted{TraceID: "t", SpanID: "s"},
			domain.LogReceived{ID: "l", TraceID: "t", SpanID: "s", Body: "hello",
				Source: domain.SourceOTLP, SeverityNumber: 17, SeverityText: "ERROR",
				Attributes: []domain.Attr{{Key: "k", Value: domain.StringValue("v")}}},
		)
		if got := mustSpan(t, f, "t", "s").Logs; !slices.Equal(got, []string{"l"}) {
			t.Fatalf("Logs = %v", got)
		}
		want := projection.Log{ID: "l", Body: "hello", TraceID: "t", SpanID: "s",
			Source: domain.SourceOTLP, SeverityNumber: 17, SeverityText: "ERROR",
			Attributes: []domain.Attr{{Key: "k", Value: domain.StringValue("v")}}}
		if got := f.Log("l"); got == nil || !reflect.DeepEqual(*got, want) {
			t.Fatalf("Log = %+v, want %+v", got, want)
		}
	})

	sweet.Run(t, "a log that names an unseen span declares a placeholder", forest, func(t *testing.T, f *projection.Forest) {
		apply(f, domain.LogReceived{
			ID: "l", DeclaredTraceID: "t", DeclaredSpanID: "0123456789abcdef", Component: "api",
		})
		sp := mustSpan(t, f, "t", "0123456789abcdef")
		if !sp.Declared || sp.Name != "span/01234567" || sp.Component != "api" ||
			!slices.Equal(sp.Logs, []string{"l"}) {
			t.Fatalf("placeholder = %+v", *sp)
		}
	})

	sweet.Run(t, "an assignment moves the log and drops the empty placeholder", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.SpanUpserted{TraceID: "t", SpanID: "orphan", Synthetic: true},
			domain.LogReceived{ID: "l", DeclaredTraceID: "t", DeclaredSpanID: "gone"},
			domain.LogAssigned{ID: "l", TraceID: "t", SpanID: "orphan"},
		)
		if f.Span("t", "gone") != nil {
			t.Fatal("the empty placeholder should be gone")
		}
		if got := mustSpan(t, f, "t", "orphan").Logs; !slices.Equal(got, []string{"l"}) {
			t.Fatalf("orphan Logs = %v", got)
		}
		if l := f.Log("l"); l.SpanID != "orphan" {
			t.Fatalf("log assignment = %+v", *l)
		}
	})

	sweet.Run(t, "an assignment keeps a placeholder that still has logs", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.LogReceived{ID: "a", DeclaredTraceID: "t", DeclaredSpanID: "p"},
			domain.LogReceived{ID: "b", DeclaredTraceID: "t", DeclaredSpanID: "p"},
			domain.LogAssigned{ID: "a", TraceID: "t", SpanID: "other"},
		)
		if got := mustSpan(t, f, "t", "p").Logs; !slices.Equal(got, []string{"b"}) {
			t.Fatalf("placeholder Logs = %v", got)
		}
	})

	sweet.Run(t, "an assignment for an unknown log does nothing", forest, func(t *testing.T, f *projection.Forest) {
		apply(f, domain.LogAssigned{ID: "nope", TraceID: "t", SpanID: "s"})
		if f.Len() != 0 {
			t.Fatalf("Len = %d", f.Len())
		}
	})
}

func TestForestEvicted(t *testing.T) {
	sweet.Run(t, "removes the trace's spans and their logs, and nothing else", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.SpanUpserted{TraceID: "old", SpanID: "a"},
			domain.LogReceived{ID: "la", TraceID: "old", SpanID: "a"},
			domain.SpanUpserted{TraceID: "new", SpanID: "b"},
			domain.LogReceived{ID: "lb", TraceID: "new", SpanID: "b"},
			domain.ForestEvicted{TraceID: "old"},
		)
		if f.Span("old", "a") != nil || f.Log("la") != nil {
			t.Fatal("evicted trace is still there")
		}
		if f.Span("new", "b") == nil || f.Log("lb") == nil {
			t.Fatal("the other trace was removed")
		}
		if f.Revision("old") != 0 {
			t.Fatalf("Revision(old) = %d, want 0", f.Revision("old"))
		}
	})
}

// shape is what the forest says about a span that does not depend on the
// order the events arrived in.
type shape struct {
	Name, Component, ParentID, StatusMsg string
	Declared, Synthetic, Closed          bool
	Status                               domain.SpanStatus
	Logs                                 []string
}

func shapes(f *projection.Forest) map[domain.SpanKey]shape {
	out := make(map[domain.SpanKey]shape)
	for sp := range f.Spans() {
		logs := slices.Clone(sp.Logs)
		slices.Sort(logs)
		out[domain.SpanKey{TraceID: sp.TraceID, SpanID: sp.ID}] = shape{
			Name: sp.Name, Component: sp.Component, ParentID: sp.ParentID,
			StatusMsg: sp.StatusMsg, Declared: sp.Declared, Synthetic: sp.Synthetic,
			Closed: sp.Closed, Status: sp.Status, Logs: logs,
		}
	}
	return out
}

func TestArrivalOrderConverges(t *testing.T) {
	parent := domain.SpanUpserted{TraceID: "t", SpanID: "p", Name: "request", Component: "api"}
	child := domain.SpanUpserted{TraceID: "t", SpanID: "c", Name: "query", Component: "db"}
	link := domain.SpanParentLinked{TraceID: "t", SpanID: "c", ParentSpanID: "p"}
	closeP := domain.SpanClosed{TraceID: "t", SpanID: "p"}
	closeC := domain.SpanClosed{TraceID: "t", SpanID: "c"}
	logC := domain.LogReceived{ID: "l1", TraceID: "t", SpanID: "c", Body: "rows=3"}
	pending := domain.LogReceived{ID: "l2", DeclaredTraceID: "t", DeclaredSpanID: "c", Body: "late"}
	assigned := domain.LogAssigned{ID: "l2", TraceID: "t", SpanID: "c"}
	other := domain.SpanUpserted{TraceID: "u", SpanID: "x", Name: "other"}
	evict := domain.ForestEvicted{TraceID: "u"}

	// The correlator sends a pending (declared) log only while its span is
	// unseen, so every sequence has pending before child.
	reference := shapes(apply(projection.New(),
		parent, pending, child, link, logC, assigned, closeC, closeP,
	))

	cases := map[string][]domain.Event{
		"the child arrives before the parent": {
			pending, child, link, closeC, parent, closeP, logC, assigned,
		},
		"the log arrives before the span": {
			logC, pending, parent, child, link, assigned, closeC, closeP,
		},
		"the assignment arrives after everything else": {
			pending, parent, child, link, logC, closeC, closeP, assigned,
		},
		"another trace comes and is evicted": {
			other, pending, parent, child, link, logC, evict, assigned, closeC, closeP,
		},
	}
	for name, evs := range cases {
		sweet.Run(t, name, forest, func(t *testing.T, f *projection.Forest) {
			got := shapes(apply(f, evs...))
			if !reflect.DeepEqual(got, reference) {
				t.Fatalf("forest\n got %+v\nwant %+v", got, reference)
			}
		})
	}
}

func TestClone(t *testing.T) {
	sweet.Run(t, "the copy is independent of the original", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.SpanUpserted{TraceID: "t", SpanID: "s", Name: "a",
				Attributes: []domain.Attr{{Key: "k", Value: domain.StringValue("v")}}},
			domain.LogReceived{ID: "l", TraceID: "t", SpanID: "s", Body: "b"},
		)
		c := f.Clone(nil)
		sp := mustSpan(t, c, "t", "s")
		sp.Name = "changed"
		sp.Logs[0] = "changed"
		sp.Attributes[0].Key = "changed"
		c.Log("l").Body = "changed"
		c.Apply(domain.SpanUpserted{TraceID: "t", SpanID: "new"})

		orig := mustSpan(t, f, "t", "s")
		if orig.Name != "a" || orig.Logs[0] != "l" || orig.Attributes[0].Key != "k" ||
			f.Log("l").Body != "b" || f.Span("t", "new") != nil {
			t.Fatalf("original changed: %+v", *orig)
		}
	})

	sweet.Run(t, "keep selects the spans", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.SpanUpserted{TraceID: "t", SpanID: "real"},
			domain.SpanUpserted{TraceID: "t", SpanID: "orphan", Synthetic: true},
		)
		c := f.Clone(func(sp *projection.Span) bool { return !sp.Synthetic })
		if c.Len() != 1 || c.Span("t", "real") == nil {
			t.Fatalf("clone has %d spans", c.Len())
		}
		if f.Len() != 2 {
			t.Fatalf("original has %d spans, want 2", f.Len())
		}
	})
}

func TestVersionRevisionAndNow(t *testing.T) {
	sweet.Run(t, "version counts every event; revision counts a trace's", forest, func(t *testing.T, f *projection.Forest) {
		apply(f,
			domain.SpanUpserted{TraceID: "a", SpanID: "s"},
			domain.SpanClosed{TraceID: "a", SpanID: "s"},
			domain.SpanUpserted{TraceID: "b", SpanID: "s"},
			domain.LogReceived{ID: "l", TraceID: "a", SpanID: "s"},
			domain.LogAssigned{ID: "l", TraceID: "b", SpanID: "s"},
			domain.DrainComplete{},
		)
		if f.Version() != 6 {
			t.Fatalf("Version = %d, want 6", f.Version())
		}
		if a, b := f.Revision("a"), f.Revision("b"); a != 4 || b != 2 {
			t.Fatalf("Revision(a)=%d Revision(b)=%d, want 4 and 2", a, b)
		}
	})

	sweet.Run(t, "a new span records the time given to SetNow", forest, func(t *testing.T, f *projection.Forest) {
		now := time.Unix(42, 0)
		f.SetNow(now)
		apply(f, domain.SpanUpserted{TraceID: "t", SpanID: "s"})
		f.SetNow(now.Add(time.Hour))
		apply(f, domain.SpanClosed{TraceID: "t", SpanID: "s"})
		if got := mustSpan(t, f, "t", "s").Observed; !got.Equal(now) {
			t.Fatalf("Observed = %v, want %v", got, now)
		}
	})
}
