package query

import "testing"

// TestLogRelatesToItsOwnSpan reproduces issue #6: a log emitted directly inside
// a span is that span's direct child, so span > log and span >> log must match
// it. Python's Flask instrumentation logs straight onto the server span; pino
// under Node usually logs onto a nested span, which is why only Python failed.
func TestLogRelatesToItsOwnSpan(t *testing.T) {
	flask := Trace{ID: "flask", Spans: []Span{{ID: "server", Name: "GET /rolldice", Resource: Resource{[]Attr{{"service.name", StringValue("dice-server")}}}}}, Logs: []Log{{ID: "roll", SpanID: "server", Body: "alice is rolling the dice: 2"}}}
	nested := Trace{ID: "nested", Spans: []Span{{ID: "server", Name: "GET /rolldice", Resource: Resource{[]Attr{{"service.name", StringValue("dice-server")}}}}, {ID: "handler", ParentID: "server", Name: "handler"}}, Logs: []Log{{ID: "roll", SpanID: "handler", Body: "alice is rolling the dice: 2"}}}
	cases := []struct {
		name, q string
		tr      Trace
		want    bool
	}{
		{"log on span, direct child", `span{ span:name = "GET /rolldice" } > log{ log:body *= "rolling" }`, flask, true},
		{"log on span, descendant", `span{ resource.service.name = "dice-server" } >> log{ log:body *= "rolling" }`, flask, true},
		{"log on child span, descendant", `span{ span:name = "GET /rolldice" } >> log{ log:body *= "rolling" }`, nested, true},
		{"log on child span, not a direct child", `span{ span:name = "GET /rolldice" } > log{ log:body *= "rolling" }`, nested, false},
		{"log on child span, direct child of child", `span{ span:name = "handler" } > log{ log:body *= "rolling" }`, nested, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := Parse(tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if got := q.Eval(tc.tr).Match; got != tc.want {
				t.Errorf("%s on %s trace: match = %v, want %v", tc.q, tc.tr.ID, got, tc.want)
			}
		})
	}
}
