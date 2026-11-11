package query_test

import (
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/query"
)

func TestParseFormatExpressions(t *testing.T) {
	cases := map[string]string{
		`span{ input = $x } >> span{ output = $x * 2 }`:      `span{ span.input = $x } >> span{ span.output = $x * 2 }`,
		`span{ out = ($x + 1) * 2 }`:                         `span{ span.out = ($x + 1) * 2 }`,
		`span{ out = $x + 1 * 2 }`:                           `span{ span.out = $x + 1 * 2 }`,
		`span{ out = $x - ($y - 1) }`:                        `span{ span.out = $x - ($y - 1) }`,
		`span{ out = 2 * $x }`:                               `span{ span.out = 2 * $x }`,
		`count(span{ span:name = "q" }) > 10`:                `count(span{ span:name = "q" }) > 10`,
		`span{} > count(span{}) >= 2`:                        `span{} > count(span{}) >= 2`,
		`span{ span:name = "a" } ~> span{ span:name = "b" }`: `span{ span:name = "a" } ~> span{ span:name = "b" }`,
		`span{ span:duration > 200ms }`:                      `span{ span:duration > 200ms }`,
		`span{ trace:duration <= 1h30m }`:                    `span{ trace:duration <= 1h30m0s }`,
		`span{ span:duration >= 1.5s }`:                      `span{ span:duration >= 1.5s }`,
	}
	for in, want := range cases {
		e, err := query.Parse(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := query.Format(e); got != want {
			t.Fatalf("%s\n got %s\nwant %s", in, got, want)
		}
		again, err := query.Parse(want)
		if err != nil || query.Format(again) != want {
			t.Fatalf("%s does not round-trip: %v", want, err)
		}
	}
}

func TestParseErrorsExpressions(t *testing.T) {
	cases := map[string]string{
		`span{ span:duration > 200 }`:  "is a duration",
		`span{ span:duration > "1s" }`: "is a duration",
		`span{ x > 200ms }`:            "a duration compares only with",
		`span{ x =~ $y }`:              "needs a string, not arithmetic",
		`span{ x = $ }`:                "a variable needs a name",
		`span{ x = $y / 0 }`:           "division by zero",
		`count(span{}) =~ 2`:           "a comparison of the count",
		`count(span{}) > x`:            "a whole number",
		`span{ x = ($y + 1 }`:          `want ")"`,
	}
	for in, want := range cases {
		_, err := query.Parse(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: error %v, want it to contain %q", in, err, want)
		}
	}
}

func at(s int) time.Time { return time.Unix(1000, 0).Add(time.Duration(s) * time.Millisecond) }

// tests is a property-test run: three tests of a doubling function, one of
// which returns the wrong answer; plus a request that queries three times.
func tests() map[string]*query.Trace {
	f := projection.New()
	test := func(trace string, in, out int64) {
		f.Apply(domain.SpanUpserted{TraceID: trace, SpanID: "test", Name: "test.TestDouble",
			StartTime: at(0), EndTime: at(50),
			Attributes: []domain.Attr{attr("test.name", domain.StringValue("TestDouble")), attr("input", domain.Int64Value(in))}})
		f.Apply(domain.SpanUpserted{TraceID: trace, SpanID: "double", Name: "double",
			StartTime: at(10), EndTime: at(20),
			Attributes: []domain.Attr{attr("output", domain.Int64Value(out))}})
		f.Apply(domain.SpanParentLinked{TraceID: trace, SpanID: "double", ParentSpanID: "test"})
	}
	test("ok1", 2, 4)
	test("ok2", 5, 10)
	test("bad", 3, 7)
	for _, ev := range []domain.Event{
		domain.SpanUpserted{TraceID: "req", SpanID: "r", Name: "request", StartTime: at(0), EndTime: at(900)},
		domain.SpanUpserted{TraceID: "req", SpanID: "auth", Name: "auth", StartTime: at(10), EndTime: at(20)},
		domain.SpanUpserted{TraceID: "req", SpanID: "q1", Name: "query", StartTime: at(30), EndTime: at(40)},
		domain.SpanUpserted{TraceID: "req", SpanID: "q2", Name: "query", StartTime: at(50), EndTime: at(400)},
		domain.SpanUpserted{TraceID: "req", SpanID: "q3", Name: "query", StartTime: at(5), EndTime: at(8)},
		domain.SpanParentLinked{TraceID: "req", SpanID: "auth", ParentSpanID: "r"},
		domain.SpanParentLinked{TraceID: "req", SpanID: "q1", ParentSpanID: "r"},
		domain.SpanParentLinked{TraceID: "req", SpanID: "q2", ParentSpanID: "r"},
		domain.SpanParentLinked{TraceID: "req", SpanID: "q3", ParentSpanID: "r"},
	} {
		f.Apply(ev)
	}
	return query.TracesOf(f)
}

func TestEvalExpressions(t *testing.T) {
	traces := tests()
	cases := []struct {
		trace, q, want string
	}{
		// A variable binds in the test and is used below it.
		{"ok1", `span{ input = $x } >> span{ output = $x * 2 }`, "double"},
		{"ok2", `span{ input = $x } >> span{ output = $x * 2 }`, "double"},
		{"bad", `span{ input = $x } >> span{ output = $x * 2 }`, ""},
		{"bad", `span{ input = $x } > span{ output != $x * 2 }`, "double"},
		{"bad", `span{ input = $x } > span{ output = $x * 2 + 1 }`, "double"},
		// Durations.
		{"req", `span{ span:duration > 100ms }`, "q2 r"},
		{"req", `span{ span:duration < 11ms && span:name = "query" }`, "q1 q3"},
		{"req", `span{ span:name = "auth" && trace:duration >= 900ms }`, "auth"},
		// Counts within a trace.
		{"req", `count(span{ span:name = "query" }) >= 3`, "q1 q2 q3"},
		{"req", `count(span{ span:name = "query" }) > 3`, ""},
		{"req", `span{ span:name = "request" } >> count(span{ span:name = "query" }) = 3`, "q1 q2 q3"},
		// Order among siblings.
		{"req", `span{ span:name = "auth" } ~> span{ span:name = "query" }`, "q1 q2"},
		{"req", `span{ span:name = "query" } ~> span{ span:name = "auth" }`, "auth"},
	}
	for _, c := range cases {
		e, err := query.Parse(c.q)
		if err != nil {
			t.Fatalf("%s: %v", c.q, err)
		}
		if got := spans(query.Eval(e, traces[c.trace])); got != c.want {
			t.Fatalf("%s on %s: got %q, want %q", c.q, c.trace, got, c.want)
		}
	}
}

func TestBindSpan(t *testing.T) {
	tr := tests()["ok2"]
	e, _ := query.Parse(`span{ test.name = "TestDouble" && input = $x }`)
	env, ok := query.BindSpan(e, tr.Span("test"), tr, nil)
	if !ok {
		t.Fatal("no match")
	}
	if v, _ := env["x"].AsInt64(); v != 5 {
		t.Fatalf("x = %v", env["x"])
	}
	if _, ok := query.BindSpan(e, tr.Span("double"), tr, nil); ok {
		t.Fatal("the child matched the anchor")
	}
	if got := query.Vars(e); len(got) != 1 || got[0] != "x" {
		t.Fatalf("Vars = %v", got)
	}
}
