package check_test

import (
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/check"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/projection"
)

// A property over inputs not known before the run: each test's output is
// twice its input. One test is wrong.
func TestPropertyWithAVariable(t *testing.T) {
	f := projection.New()
	run := func(trace string, in, out int64) {
		for _, ev := range []domain.Event{
			domain.SpanUpserted{TraceID: trace, SpanID: "t", Name: "test.TestDouble",
				Attributes: []domain.Attr{{Key: "test.name", Value: domain.StringValue("TestDouble")}, {Key: "input", Value: domain.Int64Value(in)}}},
			domain.SpanUpserted{TraceID: trace, SpanID: "d", Name: "double",
				Attributes: []domain.Attr{{Key: "output", Value: domain.Int64Value(out)}}},
			domain.SpanParentLinked{TraceID: trace, SpanID: "d", ParentSpanID: "t"},
			domain.SpanClosed{TraceID: trace, SpanID: "d"},
			domain.SpanClosed{TraceID: trace, SpanID: "t"},
		} {
			f.Apply(ev)
		}
	}
	run("a", 2, 4)
	run("b", 7, 14)
	property := plan.Check{Kind: plan.CheckEvery, Name: "doubles",
		Anchor: `span{ span.test.name = "TestDouble" && span.input = $x }`,
		Select: `span{ span.output = $x * 2 }`}

	v, lines := judge(t, property, f)
	if v.Outcome() != check.Pass {
		t.Fatalf("all correct: %s\n%s", v.Outcome(), strings.Join(lines, "\n"))
	}

	run("c", 3, 7)
	v, lines = judge(t, property, f)
	report := strings.Join(lines, "\n")
	if v.Outcome() != check.Fail || !strings.Contains(report, "trace c") || strings.Contains(report, "trace a") {
		t.Fatalf("one wrong: %s\n%s", v.Outcome(), report)
	}
}

// The N+1 check of design/PIPELINE.md: no request makes more than N
// database calls.
func TestNPlusOne(t *testing.T) {
	f := projection.New()
	f.Apply(domain.SpanUpserted{TraceID: "t", SpanID: "r", Name: "GET /orders", Kind: domain.SpanKindServer})
	for _, id := range []string{"q1", "q2", "q3", "q4"} {
		f.Apply(domain.SpanUpserted{TraceID: "t", SpanID: id, Name: "select",
			Attributes: []domain.Attr{{Key: "db.system", Value: domain.StringValue("postgresql")}}})
		f.Apply(domain.SpanParentLinked{TraceID: "t", SpanID: id, ParentSpanID: "r"})
	}
	c := plan.Check{Kind: plan.CheckEvery, Name: "n+1",
		Anchor: `span{ span:kind = server }`, Select: `span{ span.db.system = "postgresql" }`,
		Count: &plan.Count{Op: "at_most", N: 3}}
	v, lines := judge(t, c, f)
	if v.Outcome() != check.Fail || !strings.Contains(strings.Join(lines, "\n"), "GET /orders (r) has 4") {
		t.Fatalf("%s\n%s", v.Outcome(), strings.Join(lines, "\n"))
	}
}
