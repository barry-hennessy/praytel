package query_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/query"
)

func TestParseFormat(t *testing.T) {
	cases := map[string]string{
		`span{}`:                       `span{}`,
		`span{ name = "GET" }`:         `span{ span.name = "GET" }`,
		`span{span:name="GET"}`:        `span{ span:name = "GET" }`,
		`span{ span:status = error }`:  `span{ span:status = error }`,
		`span{ span:status = "error"}`: `span{ span:status = error }`,
		`log{ log:body *= 'refused' && log:severity >= 17 }`:            `log{ log:body *= "refused" && log:severity >= 17 }`,
		`span{ a = 1 || b = 2 && c = 3 }`:                               `span{ span.a = 1 || span.b = 2 && span.c = 3 }`,
		`span{ (a = 1 || b = 2) && c = 3 }`:                             `span{ (span.a = 1 || span.b = 2) && span.c = 3 }`,
		`span{ !exists(resource.service.name) }`:                        `span{ !exists(resource.service.name) }`,
		`span{ x = 1.5 } > span{ y = true }`:                            `span{ span.x = 1.5 } > span{ span.y = true }`,
		`span{a=1} >> span{b=2} > span{c=3}`:                            `span{ span.a = 1 } >> span{ span.b = 2 } > span{ span.c = 3 }`,
		`span{a=1} >> (span{b=2} > span{c=3})`:                          `span{ span.a = 1 } >> (span{ span.b = 2 } > span{ span.c = 3 })`,
		`span{a=1} && log{} || span{b=2}`:                               `span{ span.a = 1 } && log{} || span{ span.b = 2 }`,
		`span{a=1} && (log{} || span{b=2})`:                             `span{ span.a = 1 } && (log{} || span{ span.b = 2 })`,
		`!span{a=1} & span{} > span{}`:                                  `!span{ span.a = 1 } & span{} > span{}`,
		`!(span{a=1} & span{})`:                                         `!(span{ span.a = 1 } & span{})`,
		`span{ span:name =~ "GET .*" && span.http.status_code != 500 }`: `span{ span:name =~ "GET .*" && span.http.status_code != 500 }`,
	}
	for in, want := range cases {
		e, err := query.Parse(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		got := query.Format(e)
		if got != want {
			t.Fatalf("%s\n got %s\nwant %s", in, got, want)
		}
		again, err := query.Parse(got)
		if err != nil || query.Format(again) != got {
			t.Fatalf("%s does not round-trip: %v", got, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		``:                             "want span{…}",
		`span`:                         `want "{"`,
		`span{ a = }`:                  "want a value",
		`span{ a ? 1 }`:                `unexpected '?'`,
		`span{ a = 1 } extra`:          `unexpected "extra"`,
		`span{ "x" = 1 }`:              "want a field",
		`span{ a = "unterminated }`:    "unterminated string",
		`span{ span:nope = 1 }`:        "unknown field span:nope",
		`span{ log:body = "x" }`:       "a span has no log field",
		`log{ resource.x = 1 }`:        "a log has no resource field",
		`span{ span:status = broken }`: "span:status is one of",
		`span{ span:status > ok }`:     "compares only with = and !=",
		`span{ a = bareword }`:         "quote it",
		`span{ a =~ "(" }`:             "missing closing )",
		`span{ a =~ 1 }`:               "needs a string pattern",
		`log{ log:severity = "high" }`: "is a number",
		`span{ a > true }`:             "cannot order a bool",
		`span{ a = 1 } > `:             "the end",
	}
	for in, want := range cases {
		_, err := query.Parse(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: error %v, want it to contain %q", in, err, want)
		}
	}
	if _, err := query.Parse(`span{ a = 1 } x`); err == nil || !strings.Contains(err.Error(), "query:15:") {
		t.Fatalf("no column: %v", err)
	}
}

func attr(k string, v domain.Value) domain.Attr { return domain.Attr{Key: k, Value: v} }

// tree is an asymmetric trace:
//
//	front (frontend, server)
//	├── checkout (checkout)          log "retrying" (otlp, severity 13)
//	│   └── db (postgres, client)    status error, db.statement "DROP TABLE x"
//	│       └── cache (redis)
//	└── health (frontend)            log "ok"
func tree() *query.Trace {
	f := projection.New()
	for _, ev := range []domain.Event{
		domain.SpanUpserted{TraceID: "t", SpanID: "front", Name: "GET /", Component: "frontend", Kind: domain.SpanKindServer,
			Resource:   domain.Resource{Attributes: []domain.Attr{attr("service.name", domain.StringValue("frontend"))}},
			Attributes: []domain.Attr{attr("http.status_code", domain.Int64Value(500))}},
		domain.SpanUpserted{TraceID: "t", SpanID: "checkout", Name: "checkout", Component: "checkout",
			Resource: domain.Resource{Attributes: []domain.Attr{attr("service.name", domain.StringValue("checkout"))}}},
		domain.SpanUpserted{TraceID: "t", SpanID: "db", Name: "query", Component: "postgres", Kind: domain.SpanKindClient,
			Status:     domain.SpanStatusError,
			Attributes: []domain.Attr{attr("db.statement", domain.StringValue("DROP TABLE x")), attr("rows", domain.Float64Value(2.5))}},
		domain.SpanUpserted{TraceID: "t", SpanID: "cache", Name: "get", Component: "redis"},
		domain.SpanUpserted{TraceID: "t", SpanID: "health", Name: "health", Component: "frontend"},
		domain.SpanUpserted{TraceID: "t", SpanID: "orphans", Synthetic: true},
		domain.SpanParentLinked{TraceID: "t", SpanID: "checkout", ParentSpanID: "front"},
		domain.SpanParentLinked{TraceID: "t", SpanID: "db", ParentSpanID: "checkout"},
		domain.SpanParentLinked{TraceID: "t", SpanID: "cache", ParentSpanID: "db"},
		domain.SpanParentLinked{TraceID: "t", SpanID: "health", ParentSpanID: "front"},
		domain.LogReceived{ID: "retry", TraceID: "t", SpanID: "checkout", Body: "retrying", Source: domain.SourceOTLP, SeverityNumber: 13},
		domain.LogReceived{ID: "ok", TraceID: "t", SpanID: "health", Body: "ok", Source: domain.SourceStdout},
		domain.SpanUpserted{TraceID: "other", SpanID: "x", Name: "checkout"},
	} {
		f.Apply(ev)
	}
	return query.TraceOf(f, "t")
}

func spans(r query.Result) string {
	return strings.Join(slices.Sorted(maps.Keys(r.Spans)), " ")
}

func TestEval(t *testing.T) {
	tr := tree()
	cases := map[string]string{
		`span{}`:                                                           "cache checkout db front health",
		`span{ span:name = "checkout" }`:                                   "checkout",
		`span{ span:component = "frontend" }`:                              "front health",
		`span{ resource.service.name = "checkout" }`:                       "checkout",
		`span{ span:kind = server }`:                                       "front",
		`span{ span:status = error }`:                                      "db",
		`span{ db.statement =~ "DROP.*" }`:                                 "db",
		`span{ db.statement =~ "DROP" }`:                                   "", // anchored
		`span{ db.statement *= "TABLE" }`:                                  "db",
		`span{ http.status_code = 500 }`:                                   "front",
		`span{ http.status_code = "500" }`:                                 "", // no conversion
		`span{ http.status_code >= 400 }`:                                  "front",
		`span{ rows > 2 }`:                                                 "db",
		`span{ rows != 1 }`:                                                "db",
		`span{ missing != 1 }`:                                             "",
		`span{ !exists(missing) }`:                                         "cache checkout db front health",
		`span{ exists(rows) }`:                                             "db",
		`span{ span:parent_id = "db" }`:                                    "cache",
		`span{ trace:id = "t" } & span{ span:name = "get" }`:               "cache",
		`log{ log:body = "retrying" }`:                                     "checkout",
		`log{ log:source = stdout }`:                                       "health",
		`log{ log:severity >= 13 }`:                                        "checkout",
		`span{ span:component = "frontend" } > span{}`:                     "checkout health",
		`span{ span:component = "frontend" } >> span{}`:                    "cache checkout db health",
		`span{ span:name = "checkout" } > span{ span:name = "get" }`:       "",
		`span{ span:name = "checkout" } >> span{ span:name = "get" }`:      "cache",
		`log{ log:body = "retrying" } >> span{ span:status = error }`:      "db",
		`span{ span:status = error } && log{ log:body = "ok" }`:            "db health",
		`span{ span:status = error } && log{ log:body = "nope" }`:          "",
		`span{ span:name = "get" } || span{ span:name = "health" }`:        "cache health",
		`!span{ span:component = "frontend" }`:                             "cache checkout db",
		`span{ span:component = "frontend" } & span{ span:kind = server }`: "front",
	}
	for q, want := range cases {
		e, err := query.Parse(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if got := spans(query.Eval(e, tr)); got != want {
			t.Fatalf("%s: got %q, want %q", q, got, want)
		}
	}
}

func TestEvalLogsEvidence(t *testing.T) {
	tr := tree()
	e, _ := query.Parse(`span{ span:component = "frontend" } >> log{ log:body = "retrying" }`)
	r := query.Eval(e, tr)
	if !r.Logs["retry"] || r.Logs["ok"] || spans(r) != "checkout" {
		t.Fatalf("result %+v", r)
	}
}

func TestPath(t *testing.T) {
	tr := tree()
	e, _ := query.Parse(`span{ span:name = "checkout" }`)
	keep := tr.Path(query.Eval(e, tr))
	if got := strings.Join(slices.Sorted(maps.Keys(keep)), " "); got != "cache checkout db front" {
		t.Fatalf("path %q", got)
	}
}

func TestRecordLocal(t *testing.T) {
	cases := map[string]bool{
		`span{ a = 1 }`:                  true,
		`span{ a = 1 } & !span{ b = 2 }`: true,
		`log{} || log{ x = 1 }`:          true,
		`span{} || log{}`:                false,
		`span{} && span{}`:               false,
		`span{} > span{}`:                false,
		`span{} >> log{}`:                false,
	}
	for q, want := range cases {
		e, err := query.Parse(q)
		if err != nil {
			t.Fatal(err)
		}
		if _, got := query.RecordLocal(e); got != want {
			t.Fatalf("%s: RecordLocal = %v", q, got)
		}
	}
}

func TestGlobRegexp(t *testing.T) {
	cases := []struct {
		glob, name string
		want       bool
	}{
		{"orders*", "orders/v2", true},
		{"orders/*", "orders/v2", true},
		{"o?ders", "orders", true},
		{"db", "db", true},
		{"db", "dbx", false},
		{"a.b", "axb", false},
		{"[a-c]x", "bx", true},
		{"[^a-c]x", "bx", false},
		{`\*`, "*", true},
		{"*", "", true},
	}
	for _, c := range cases {
		re, err := query.GlobRegexp(c.glob)
		if err != nil {
			t.Fatal(err)
		}
		e, err := query.Parse(`span{ span:name =~ ` + quote(re) + ` }`)
		if err != nil {
			t.Fatalf("%s -> %s: %v", c.glob, re, err)
		}
		sp := &projection.Span{Name: c.name}
		if got := query.Matches(e, sp); got != c.want {
			t.Fatalf("glob %q (regexp %q) on %q = %v", c.glob, re, c.name, got)
		}
	}
	if _, err := query.GlobRegexp("["); err == nil {
		t.Fatal("bad glob accepted")
	}
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		`span{}`, `span{ a = 1 } >> log{ log:body *= "x" }`, `!(span{a=1} & span{b="2"}) || log{}`,
		`span{ (a = 1 || b = 2) && !exists(c) }`, `span{ x =~ "a|b" } > span{ y = 1.5e3 }`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		e, err := query.Parse(src)
		if err != nil {
			return
		}
		text := query.Format(e)
		again, err := query.Parse(text)
		if err != nil {
			t.Fatalf("%q formats as %q, which does not parse: %v", src, text, err)
		}
		if query.Format(again) != text {
			t.Fatalf("%q: format is not stable: %q then %q", src, text, query.Format(again))
		}
	})
}
