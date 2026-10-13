package query_test

import (
	"errors"
	"fmt"

	"github.com/barry-hennessy/praytel/internal/query"
)

func ExampleQuery_Eval() {
	q, _ := query.Parse(`span{ span:status = error } | matches`)
	r := q.Eval(query.Trace{Spans: []query.Span{{ID: "ok", Status: "ok"}, {ID: "bad", Status: "error"}}})
	fmt.Println(r.Match, r.EvidenceSpanIDs)
	// Output: true [bad]
}

func ExampleQuery_Eval_joinedLog() {
	q, _ := query.Parse(`log{ log:body *= "refused" && log:severity_number >= 17 }`)
	r := q.Eval(query.Trace{Logs: []query.Log{{ID: "L1", Body: "connection refused", SeverityNumber: 17}}})
	fmt.Println(r.EvidenceLogIDs)
	// Output: [L1]
}

func ExampleQuery_Eval_descendant() {
	q, _ := query.Parse(`span{ span:name = "front" } >> span{ span:status = error } | matches`)
	t := query.Trace{Spans: []query.Span{{ID: "a", Name: "front"}, {ID: "b", ParentID: "a"}, {ID: "c", ParentID: "b", Status: "error"}}}
	fmt.Println(q.Eval(t).Match)
	// Output: true
}

func ExampleQuery_Eval_sameTrace() {
	q, _ := query.Parse(`log{ log:body *= "retry" } && span{ span:kind = client }`)
	t := query.Trace{Spans: []query.Span{{ID: "s", Kind: "client"}}, Logs: []query.Log{{ID: "l", Body: "retrying"}}}
	fmt.Println(q.Eval(t).Match)
	// Output: true
}

func ExampleQuery_Eval_resourceAttribute() {
	q, _ := query.Parse(`span{ resource.service.name = "checkout" } | matches`)
	t := query.Trace{Spans: []query.Span{{
		ID: "request",
		Resource: query.Resource{Attributes: []query.Attr{
			{Key: "service.name", Value: query.StringValue("checkout")},
		}},
	}}}
	fmt.Println(q.Eval(t).EvidenceSpanIDs)
	// Output: [request]
}

func ExampleQuery_Eval_missingField() {
	q, _ := query.Parse(`span{ !exists(span.error.type) } | matches`)
	t := query.Trace{Spans: []query.Span{
		{ID: "clean"},
		{ID: "failed", Attributes: []query.Attr{{Key: "error.type", Value: query.StringValue("timeout")}}},
	}}
	fmt.Println(q.Eval(t).EvidenceSpanIDs)
	// Output: [clean]
}

func ExampleCheck() {
	q, err := query.Check(`span{span:status=error}&&log{log:body *= "retry"}|matches`)
	fmt.Println(err)
	fmt.Println(query.Format(q))
	// Output:
	// <nil>
	// span{ span:status = error } && log{ log:body *= "retry" } | matches
}

func ExampleError() {
	_, err := query.Parse(`span{ span:name = }`)
	var diagnostic *query.Error
	errors.As(err, &diagnostic)
	fmt.Println(diagnostic.Start, diagnostic.End, diagnostic.Message)
	// Output: 18 19 expected value
}

func ExampleExplain() {
	q, _ := query.Parse(`span{ span:name = "api" } > log{ log:body *= "failed" } | path`)
	fmt.Print(query.Explain(q))
	// Output:
	// view: path (evidence, span ancestors and descendants, and their logs)
	// required finality: none for matches; final for absence claims
	// expression:
	//   direct child
	//     span selector (span:name = "api")
	//       field span:name: string
	//     log selector (log:body *= "failed")
	//       field log:body: string
}
