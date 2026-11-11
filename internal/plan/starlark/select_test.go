package starlark_test

import (
	"io"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/plan/starlark"
	"github.com/barry-hennessy/praytel/internal/query"
)

// whereQuery compiles `where(sel) | snapshot("tree") | final | stdout` and
// returns the where stage's query.
func whereQuery(t *testing.T, sel string) (string, error) {
	t.Helper()
	p, err := starlark.Compile([]string{`where(` + sel + `) | snapshot("tree") | final | stdout`}, t.TempDir(), io.Discard)
	if err != nil {
		return "", err
	}
	st := p.Outputs[0].Stages[0]
	if st.Kind != plan.StageWhere {
		t.Fatalf("stage %+v", st)
	}
	return st.Query, nil
}

// Each builder makes the same tree as the query string it stands for.
func TestBuildersEqualQueryStrings(t *testing.T) {
	cases := map[string]string{
		`span(name = "GET /")`:                                 `span{ span:name = "GET /" }`,
		`span(service = "checkout", status = "error")`:         `span{ resource.service.name = "checkout" && span:status = error }`,
		`span(kind = "server", component = match("api.*"))`:    `span{ span:kind = server && span:component =~ "api.*" }`,
		`span(attrs = {"db.system": "postgresql", "rows": 3})`: `span{ span.db.system = "postgresql" && span.rows = 3 }`,
		`span(attrs = {"ok": True, "ratio": 0.5})`:             `span{ span.ok = true && span.ratio = 0.5 }`,
		`span(name = glob("orders*"))`:                         `span{ span:name =~ "orders.*" }`,
		`log(body = contains("refused"), source = "otlp")`:     `log{ log:body *= "refused" && log:source = otlp }`,
		`log(severity = 17)`:                                   `log{ log:severity = 17 }`,
		`span(service = "checkout") >> span(status = "error")`: `span{ resource.service.name = "checkout" } >> span{ span:status = error }`,
		`span(name = "a") & ~span(name = "b")`:                 `span{ span:name = "a" } & !span{ span:name = "b" }`,
		`span(name = "a") | span(name = "b")`:                  `span{ span:name = "a" } || span{ span:name = "b" }`,
		`span(name = "q").child_of(span(name = "p"))`:          `span{ span:name = "p" } > span{ span:name = "q" }`,
		`span()`:                             `span{}`,
		`"span{ span:name = 'x' } >> log{}"`: `span{ span:name = "x" } >> log{}`,
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

func TestBuilderErrors(t *testing.T) {
	cases := map[string]string{
		`span("x")`:               "keyword arguments only",
		`span(colour = "red")`:    `unexpected keyword argument "colour"`,
		`span(status = "broken")`: "span:status is one of",
		`span(attrs = ["x"])`:     "attrs is a dict",
		`span(name = [1])`:        "not a list",
		`span(name = match("("))`: "missing closing )",
		`span(name = glob("["))`:  "glob",
		`"span{ nope"`:            "want",
		`42`:                      "want a selection or a query string",
	}
	for sel, want := range cases {
		_, err := whereQuery(t, sel)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error %v, want it to contain %q", sel, err, want)
		}
	}
	_, err := starlark.Compile([]string{`span() | stream() | each | stdout`}, t.TempDir(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "use where(selection)") {
		t.Fatalf("piping a selection: %v", err)
	}
}

func TestStreamWhereNeedsSingleLogs(t *testing.T) {
	if _, err := starlark.Compile([]string{`where(log(source = "stderr")) | stream() | each | stdout`}, t.TempDir(), io.Discard); err != nil {
		t.Fatalf("a log query on the stream: %v", err)
	}
	_, err := starlark.Compile([]string{`where(span(name = "x") >> log()) | stream() | each | stdout`}, t.TempDir(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "the stream can only keep logs one at a time") {
		t.Fatalf("a structural query on the stream: %v", err)
	}
}
