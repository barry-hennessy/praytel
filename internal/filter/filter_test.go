package filter_test

import (
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/test/sweet"
)

func TestFocusPathIncludesAncestorsAndDescendantsNotSiblings(t *testing.T) {
	type deps struct {
		f *filter.Filter
	}

	factory := func(t *testing.T) deps {
		t.Helper()

		f, err := filter.New(filter.Query{Focus: []string{"orders"}})
		if err != nil {
			t.Fatal(err)
		}

		return deps{f: f}
	}

	sweet.Run(t, "focus path keeps parent and child logs; sibling stays out", factory, func(t *testing.T, d deps) {
		got := play(d.f,
			domain.SpanUpserted{SpanID: "root", Name: "http"},
			domain.SpanUpserted{SpanID: "orders", Name: "orders"},
			domain.SpanUpserted{SpanID: "health", Name: "health"},
			domain.SpanUpserted{SpanID: "db", Name: "db"},
			domain.SpanParentLinked{SpanID: "orders", ParentSpanID: "root"},
			domain.SpanParentLinked{SpanID: "health", ParentSpanID: "root"},
			domain.SpanParentLinked{SpanID: "db", ParentSpanID: "orders"},
			domain.LogReceived{ID: "Lroot", SpanID: "root"},
			domain.LogReceived{ID: "Lorders", SpanID: "orders"},
			domain.LogReceived{ID: "Lhealth", SpanID: "health"},
			domain.LogReceived{ID: "Ldb", SpanID: "db"},
		)
		if !equalIDs(got, []string{"Lroot", "Lorders", "Ldb"}) {
			t.Fatalf("admitted=%v", got)
		}
		if d.f.InFocus("health") {
			t.Fatal("sibling health should be off the path")
		}
		if !d.f.InFocus("root") || !d.f.InFocus("orders") || !d.f.InFocus("db") {
			t.Fatal("path should include ancestor, match, and descendant")
		}
	})
}

func TestTwoFocusFlagsUnion(t *testing.T) {
	factory := func(t *testing.T) *filter.Filter {
		t.Helper()

		f, err := filter.New(filter.Query{Focus: []string{"orders", "health"}})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	sweet.Run(t, "repeatable --focus unions paths", factory, func(t *testing.T, f *filter.Filter) {
		ids := play(f,
			domain.SpanUpserted{SpanID: "root", Name: "http"},
			domain.SpanUpserted{SpanID: "orders", Name: "orders"},
			domain.SpanUpserted{SpanID: "health", Name: "health"},
			domain.SpanParentLinked{SpanID: "orders", ParentSpanID: "root"},
			domain.SpanParentLinked{SpanID: "health", ParentSpanID: "root"},
			domain.LogReceived{ID: "Lroot", SpanID: "root"},
			domain.LogReceived{ID: "Lorders", SpanID: "orders"},
			domain.LogReceived{ID: "Lhealth", SpanID: "health"},
		)
		if !equalIDs(ids, []string{"Lroot", "Lorders", "Lhealth"}) {
			t.Fatalf("admitted=%v", ids)
		}
	})
}

func TestExcludeAfterExpansion(t *testing.T) {
	factory := func(t *testing.T) *filter.Filter {
		t.Helper()

		f, err := filter.New(filter.Query{Focus: []string{"http"}, Exclude: []string{"health*"}})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	sweet.Run(t, "--exclude drops matching names after path expansion", factory, func(t *testing.T, f *filter.Filter) {
		ids := play(f,
			domain.SpanUpserted{SpanID: "root", Name: "http"},
			domain.SpanUpserted{SpanID: "orders", Name: "orders"},
			domain.SpanUpserted{SpanID: "health", Name: "health-check"},
			domain.SpanParentLinked{SpanID: "orders", ParentSpanID: "root"},
			domain.SpanParentLinked{SpanID: "health", ParentSpanID: "root"},
			domain.LogReceived{ID: "Lroot", SpanID: "root"},
			domain.LogReceived{ID: "Lorders", SpanID: "orders"},
			domain.LogReceived{ID: "Lhealth", SpanID: "health"},
		)
		if !equalIDs(ids, []string{"Lroot", "Lorders"}) {
			t.Fatalf("admitted=%v", ids)
		}
		if f.InFocus("health") {
			t.Fatal("excluded span still in focus")
		}
	})
}

func TestFocusMatchesComponentOrSpanName(t *testing.T) {
	factory := func(t *testing.T) *filter.Filter {
		t.Helper()

		f, err := filter.New(filter.Query{Focus: []string{"billing"}})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	sweet.Run(t, "glob matches component when span name differs", factory, func(t *testing.T, f *filter.Filter) {
		ids := play(f,
			domain.SpanUpserted{SpanID: "s1", Name: "POST /pay", Component: "billing"},
			domain.SpanUpserted{SpanID: "s2", Name: "other", Component: "web"},
			domain.LogReceived{ID: "L1", SpanID: "s1"},
			domain.LogReceived{ID: "L2", SpanID: "s2"},
		)
		if !equalIDs(ids, []string{"L1"}) {
			t.Fatalf("admitted=%v", ids)
		}
	})
}

func TestFocusMatchesLogReceivedComponent(t *testing.T) {
	factory := func(t *testing.T) *filter.Filter {
		t.Helper()

		f, err := filter.New(filter.Query{Focus: []string{"helper-svc"}})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	sweet.Run(t, "glob matches service.name copied from LogReceived", factory, func(t *testing.T, f *filter.Filter) {
		ids := play(f,
			domain.LogReceived{ID: "L1", SpanID: "orphan", Component: "helper-svc"},
			domain.LogReceived{ID: "L2", SpanID: "other", Component: "web"},
		)
		if !equalIDs(ids, []string{"L1"}) {
			t.Fatalf("admitted=%v", ids)
		}
	})
}

func TestLateParentLinkAdmitsHeldLine(t *testing.T) {
	factory := func(t *testing.T) *filter.Filter {
		t.Helper()

		f, err := filter.New(filter.Query{Focus: []string{"orders"}})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	sweet.Run(t, "SpanParentLinked late-admits an ancestor log", factory, func(t *testing.T, f *filter.Filter) {
		step := func(ev domain.Event) []string {
			return f.Observe(ev)
		}

		if got := step(domain.SpanUpserted{SpanID: "orders", Name: "orders"}); len(got) != 0 {
			t.Fatalf("upsert orders: %v", got)
		}
		if got := step(domain.SpanUpserted{SpanID: "root", Name: "http"}); len(got) != 0 {
			t.Fatalf("upsert root: %v", got)
		}
		if got := step(domain.LogReceived{ID: "Lroot", SpanID: "root"}); len(got) != 0 {
			t.Fatalf("held root log should wait for the path, got %v", got)
		}
		if got := step(domain.LogReceived{ID: "Lorders", SpanID: "orders"}); !equalIDs(got, []string{"Lorders"}) {
			t.Fatalf("orders log should admit immediately, got %v", got)
		}

		got := step(domain.SpanParentLinked{SpanID: "orders", ParentSpanID: "root"})
		if !equalIDs(got, []string{"Lroot"}) {
			t.Fatalf("late admit want Lroot, got %v", got)
		}

		if again := step(domain.SpanClosed{SpanID: "orders"}); len(again) != 0 {
			t.Fatalf("must not retract or re-admit: %v", again)
		}
	})
}

func TestGlobalOrphanOnlyIfGlobOrPath(t *testing.T) {
	factory := func(t *testing.T) *filter.Filter {
		t.Helper()

		f, err := filter.New(filter.Query{Focus: []string{"orders"}})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	sweet.Run(t, "global orphan stays out unless the glob matches it", factory, func(t *testing.T, f *filter.Filter) {
		ids := play(f,
			domain.SpanUpserted{SpanID: "orders", Name: "orders"},
			domain.SpanUpserted{SpanID: "orphan", Name: "orphan", Synthetic: true},
			domain.LogReceived{ID: "Lorders", SpanID: "orders"},
			domain.LogReceived{ID: "Lorphan", SpanID: "orphan"},
		)
		if !equalIDs(ids, []string{"Lorders"}) {
			t.Fatalf("admitted=%v", ids)
		}
	})
}

func TestPerTraceOrphanNotIncludedByTraceID(t *testing.T) {
	factory := func(t *testing.T) *filter.Filter {
		t.Helper()

		f, err := filter.New(filter.Query{Focus: []string{"orders"}})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	sweet.Run(t, "per-trace orphan is not in focus just because TraceID matches", factory, func(t *testing.T, f *filter.Filter) {
		ids := play(f,
			domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Name: "orders"},
			domain.SpanUpserted{TraceID: "t1", SpanID: "torphan", Name: "orphan", Synthetic: true},
			domain.LogReceived{ID: "Lorders", SpanID: "orders", TraceID: "t1"},
			domain.LogReceived{ID: "Lorphan", SpanID: "torphan", TraceID: "t1"},
		)
		if !equalIDs(ids, []string{"Lorders"}) {
			t.Fatalf("admitted=%v", ids)
		}
		if f.InFocus("torphan") {
			t.Fatal("per-trace orphan should not share focus via TraceID")
		}
	})
}

func TestPassAllWithNoFocus(t *testing.T) {
	factory := func(t *testing.T) *filter.Filter {
		t.Helper()

		f, err := filter.New(filter.Query{})
		if err != nil {
			t.Fatal(err)
		}

		return f
	}

	sweet.Run(t, "empty query admits every log", factory, func(t *testing.T, f *filter.Filter) {
		ids := play(f,
			domain.LogReceived{ID: "a"},
			domain.LogReceived{ID: "b", SpanID: "s1"},
		)
		if !equalIDs(ids, []string{"a", "b"}) {
			t.Fatalf("admitted=%v", ids)
		}
	})
}

func TestInvalidGlob(t *testing.T) {
	factory := func(t *testing.T) struct{} {
		t.Helper()

		return struct{}{}
	}

	sweet.Run(t, "bad glob fails at construction", factory, func(t *testing.T, _ struct{}) {
		_, err := filter.New(filter.Query{Focus: []string{"["}})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func play(f *filter.Filter, events ...domain.Event) []string {
	var ids []string
	seen := make(map[string]struct{})
	for _, ev := range events {
		for _, id := range f.Observe(ev) {
			if _, ok := seen[id]; ok {
				continue
			}

			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}

	return ids
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
