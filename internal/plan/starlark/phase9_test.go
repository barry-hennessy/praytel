package starlark_test

import (
	"io"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/plan/starlark"
	"github.com/barry-hennessy/praytel/internal/query"
)

func TestExpressionBuilders(t *testing.T) {
	cases := map[string]string{
		`span(duration = longer_than("200ms"))`:                                           `span{ span:duration > 200ms }`,
		`span(duration = shorter_than("1s"))`:                                             `span{ span:duration < 1s }`,
		`span(name = "b").after(span(name = "a"))`:                                        `span{ span:name = "a" } ~> span{ span:name = "b" }`,
		`span(attrs = {"input": var("x")}) >> span(attrs = {"output": var("x") * 2 + 1})`: `span{ span.input = $x } >> span{ span.output = $x * 2 + 1 }`,
		`span(attrs = {"o": 2 * (var("x") - var("y"))})`:                                  `span{ span.o = 2 * ($x - $y) }`,
		`span(attrs = {"o": -var("x")})`:                                                  `span{ span.o = 0 - $x }`,
	}
	for builder, str := range cases {
		got, err := whereQuery(t, builder)
		if err != nil {
			t.Fatalf("%s: %v", builder, err)
		}
		e, err := query.Parse(str)
		if err != nil {
			t.Fatalf("%s: %v", str, err)
		}
		if want := query.Format(e); got != want {
			t.Fatalf("%s\n got %s\nwant %s", builder, got, want)
		}
	}
}

func TestExpressionBuilderErrors(t *testing.T) {
	cases := map[string]string{
		`span(duration = longer_than("soon"))`: "longer_than",
		`span(attrs = {"o": var("x") * "a"})`:  "needs a number or a variable",
		`span(attrs = {"o": var("bad name")})`: "a name is letters",
	}
	for sel, want := range cases {
		_, err := whereQuery(t, sel)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error %v, want it to contain %q", sel, err, want)
		}
	}
}

func TestPropertyCheckAndTable(t *testing.T) {
	src := `
x = var("x")
every(span(attrs = {"test.name": "TestDouble", "input": x}), has = span(attrs = {"output": x * 2}))
output(table(span(name = "test.TestDouble"), columns = ["input", "span:duration"]) | final | stdout)
`
	p, err := starlark.Compile([]string{src}, t.TempDir(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Outputs) != 2 {
		t.Fatalf("%d outputs", len(p.Outputs))
	}
	c := p.Outputs[0].View.Check
	if c == nil || c.Kind != plan.CheckEvery || !strings.Contains(c.Select, "$x * 2") || !strings.Contains(c.Anchor, "span.input = $x") {
		t.Fatalf("check %+v", c)
	}
	tv := p.Outputs[1].View
	if tv.Kind != plan.ViewTable || len(tv.Columns) != 2 {
		t.Fatalf("table %+v", tv)
	}
	if _, err := starlark.Compile([]string{`table(span(), columns = ["span:nope"]) | final | stdout`}, t.TempDir(), io.Discard); err == nil {
		t.Fatal("a bad column compiled")
	}
}
