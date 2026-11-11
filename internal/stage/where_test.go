package stage_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/stage"
)

// shop is two traces: an order that fails in the database, and a health
// check.
var shop = []domain.Event{
	domain.SpanUpserted{TraceID: "order", SpanID: "front", Name: "POST /order", Component: "api"},
	domain.SpanUpserted{TraceID: "order", SpanID: "pay", Name: "pay", Component: "payments"},
	domain.SpanUpserted{TraceID: "order", SpanID: "db", Name: "insert", Component: "db", Status: domain.SpanStatusError},
	domain.SpanUpserted{TraceID: "order", SpanID: "audit", Name: "audit", Component: "api"},
	domain.SpanParentLinked{TraceID: "order", SpanID: "pay", ParentSpanID: "front"},
	domain.SpanParentLinked{TraceID: "order", SpanID: "db", ParentSpanID: "pay"},
	domain.SpanParentLinked{TraceID: "order", SpanID: "audit", ParentSpanID: "front"},
	domain.LogReceived{ID: "l1", TraceID: "order", SpanID: "db", Body: "constraint failed", Source: domain.SourceStderr},
	domain.LogReceived{ID: "l2", TraceID: "order", SpanID: "audit", Body: "audited", Source: domain.SourceStdout},
	domain.SpanUpserted{TraceID: "health", SpanID: "h", Name: "GET /health", Component: "api"},
	domain.LogReceived{ID: "l3", TraceID: "health", SpanID: "h", Body: "ok", Source: domain.SourceStdout},
	domain.DrainComplete{},
}

func where(t *testing.T, q string) stage.Stage {
	t.Helper()
	st, err := stage.Build(plan.Stage{Kind: plan.StageWhere, Query: q})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func lensed(t *testing.T, q string) projection.Reader {
	t.Helper()
	f := projection.New()
	for _, ev := range shop {
		f.Apply(ev)
	}
	return where(t, q).Lens(f)
}

func ids(r projection.Reader) (spans, logs string) {
	var s, l []string
	for sp := range r.Spans() {
		s = append(s, sp.ID)
	}
	for lg := range r.Logs() {
		l = append(l, lg.ID)
	}
	slices.Sort(s)
	slices.Sort(l)
	return strings.Join(s, " "), strings.Join(l, " ")
}

func TestWhereLensIsThePathView(t *testing.T) {
	cases := map[string][2]string{
		// The match, its ancestors and descendants, and their logs.
		`span{ span:name = "pay" }`: {"db front pay", "l1"},
		// Structure: errors below the API.
		`span{ span:component = "api" } >> span{ span:status = error }`: {"db front pay", "l1"},
		// A log query keeps only the logs it selects.
		`log{ log:source = stdout }`: {"audit front h", "l2 l3"},
		`span{ span:name = "nope" }`: {"", ""},
	}
	for q, want := range cases {
		r := lensed(t, q)
		spans, logs := ids(r)
		if spans != want[0] || logs != want[1] {
			t.Fatalf("%s: spans %q logs %q, want %q %q", q, spans, logs, want[0], want[1])
		}
		if r.Span("health", "h") != nil && !strings.Contains(want[0], "h") {
			t.Fatalf("%s: Span() shows a hidden span", q)
		}
		if got := slices.Collect(r.TraceIDs()); len(want[0]) > 0 && len(got) == 0 {
			t.Fatalf("%s: no traces", q)
		}
	}
}

func TestWhereLensFollowsTheForest(t *testing.T) {
	f := projection.New()
	r := where(t, `span{ span:status = error }`).Lens(f)
	if r.Len() != 0 {
		t.Fatal("empty forest")
	}
	f.Apply(domain.SpanUpserted{TraceID: "t", SpanID: "s", Status: domain.SpanStatusError})
	if r.Len() != 1 {
		t.Fatal("the lens did not see the new span")
	}
}

func TestWhereOnEventsKeepsSelectedLogs(t *testing.T) {
	st := where(t, `log{ log:source = stdout }`)
	var kept []string
	types := map[string]bool{}
	emit := func(ev domain.Event) {
		switch e := ev.(type) {
		case domain.LogReceived:
			kept = append(kept, e.ID)
		case domain.LogAssigned:
			kept = append(kept, "assigned:"+e.ID)
		}
		types[strings.TrimPrefix(fmt.Sprintf("%T", ev), "domain.")] = true
	}
	for _, ev := range append(slices.Clone(shop), domain.LogAssigned{ID: "l1"}, domain.LogAssigned{ID: "l2"}) {
		st.OnEvent(ev, emit)
	}
	if got := strings.Join(kept, " "); got != "l2 l3 assigned:l2" {
		t.Fatalf("kept %q", got)
	}
	for _, want := range []string{"SpanUpserted", "SpanParentLinked", "DrainComplete"} {
		if !types[want] {
			t.Fatalf("%s did not pass: %v", want, slices.Collect(maps.Keys(types)))
		}
	}
}
