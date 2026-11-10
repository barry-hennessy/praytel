package stage_test

import (
	"reflect"
	"testing"

	"github.com/barry-hennessy/praytel/internal/attrspec"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/stage"
)

func attr(k, v string) domain.Attr { return domain.Attr{Key: k, Value: domain.StringValue(v)} }

var events = []domain.Event{
	domain.SpanUpserted{TraceID: "t", SpanID: "a", Name: "get",
		Attributes: []domain.Attr{attr("http.method", "GET"), attr("http.port", "8080"), attr("db/table", "x")}},
	domain.SpanUpserted{TraceID: "t", SpanID: "b", Name: "query",
		Attributes: []domain.Attr{attr("db.system", "pg")}},
	domain.SpanParentLinked{TraceID: "t", SpanID: "b", ParentSpanID: "a"},
	domain.LogReceived{ID: "l", TraceID: "t", SpanID: "b", Body: "rows"},
	domain.SpanClosed{TraceID: "t", SpanID: "b"},
	domain.ForestShed{},
	domain.ForestEvicted{TraceID: "gone"},
	domain.DrainComplete{},
}

type spanAttrs map[string][]domain.Attr

func read(r projection.Reader) spanAttrs {
	out := spanAttrs{}
	for sp := range r.Spans() {
		out[sp.ID] = sp.Attributes
	}
	return out
}

// OnEvent and Lens must agree: a view sees the same whether the stage ran
// on the events or wraps the projection.
func TestFormsAgree(t *testing.T) {
	cases := map[string]plan.Stage{
		"select":  {Kind: plan.StageAttrs, Globs: "http.* -http.port"},
		"all":     {Kind: plan.StageAttrs},
		"exclude": {Kind: plan.StageAttrs, Globs: "-db.*"},
		"none":    {Kind: plan.StageNoAttrs},
	}
	for name, ps := range cases {
		t.Run(name, func(t *testing.T) {
			st, err := stage.Build(ps)
			if err != nil {
				t.Fatal(err)
			}

			byEvent := projection.New()
			for _, ev := range events {
				st.OnEvent(ev, byEvent.Apply)
			}

			raw := projection.New()
			for _, ev := range events {
				raw.Apply(ev)
			}
			lensed := st.Lens(raw)

			if got, want := read(lensed), read(byEvent); !reflect.DeepEqual(got, want) {
				t.Fatalf("lens %v\n events %v", got, want)
			}
			if got, want := lensed.Span("t", "a").Attributes, byEvent.Span("t", "a").Attributes; !reflect.DeepEqual(got, want) {
				t.Fatalf("Span: lens %v, events %v", got, want)
			}
			if raw.Span("t", "a").Attributes[1].Key != "http.port" {
				t.Fatal("the lens changed the forest")
			}
		})
	}
}

func TestAttrsSelection(t *testing.T) {
	spec, err := attrspec.Parse("http.* -http.port")
	if err != nil {
		t.Fatal(err)
	}
	var got []domain.Event
	stage.Attrs(spec).OnEvent(events[0], func(ev domain.Event) { got = append(got, ev) })
	want := []domain.Attr{attr("http.method", "GET")}
	if len(got) != 1 || !reflect.DeepEqual(got[0].(domain.SpanUpserted).Attributes, want) {
		t.Fatalf("got %+v", got)
	}

	var none []domain.Event
	stage.Attrs(attrspec.Spec{}).OnEvent(events[0], func(ev domain.Event) { none = append(none, ev) })
	if a := none[0].(domain.SpanUpserted).Attributes; len(a) != 0 {
		t.Fatalf("no_attrs kept %v", a)
	}
}

// No stage removes or changes a control event.
func TestControlEventsPass(t *testing.T) {
	control := []domain.Event{domain.DrainComplete{}, domain.ForestShed{}, domain.ForestEvicted{TraceID: "x"}}
	stages, err := stage.BuildAll([]plan.Stage{{Kind: plan.StageNoAttrs}, {Kind: plan.StageAttrs, Globs: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []domain.Event
	for _, ev := range control {
		stage.Chain(stages, ev, func(ev domain.Event) { got = append(got, ev) })
	}
	if !reflect.DeepEqual(got, control) {
		t.Fatalf("got %v, want %v", got, control)
	}
}

func TestBuildRejectsUnknown(t *testing.T) {
	if _, err := stage.Build(plan.Stage{Kind: "nope"}); err == nil {
		t.Fatal("unknown stage built")
	}
	if _, err := stage.Build(plan.Stage{Kind: plan.StageAttrs, Globs: "["}); err == nil {
		t.Fatal("bad glob built")
	}
}
