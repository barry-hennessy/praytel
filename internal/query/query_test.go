package query

import (
	"reflect"
	"testing"
	"time"
)

func TestSemantics(t *testing.T) {
	tr := Trace{Spans: []Span{{ID: "root", Name: "root"}, {ID: "mid", ParentID: "root", Attributes: []Attr{{"empty", StringValue("")}, {"n", IntValue(500)}}}, {ID: "leaf", ParentID: "mid", Status: "error"}}, Logs: []Log{{ID: "pending", Body: "x"}, {ID: "assigned", SpanID: "leaf", Body: "x"}}}
	cases := []struct {
		q    string
		want bool
	}{
		{`span{ span:name = "root" } > span{ span:status = error }`, false},
		{`span{ span:name = "root" } >> span{ span:status = error }`, true},
		{`span{ empty = "" }`, true}, {`span{ missing != "x" }`, false},
		{`span{ n = "500" }`, false}, {`span{ n = 500 }`, true},
		{`log{ log:body = "x" } && span{ span:status = error }`, true},
	}
	for _, tc := range cases {
		q, err := Parse(tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := q.Eval(tr).Match; got != tc.want {
			t.Errorf("%s: %v", tc.q, got)
		}
	}
	q, _ := Parse(`span{ span:name = "root" } >> log{ log:body = "x" } | matches`)
	r := q.Eval(tr)
	if !reflect.DeepEqual(r.EvidenceLogIDs, []string{"assigned"}) {
		t.Fatalf("evidence %v", r.EvidenceLogIDs)
	}
}

func TestRoundTripAndDiagnostics(t *testing.T) {
	s := `span{ a = 1 || b = 2 && !exists(c) } >> log{ log:body =~ "err.*" } | matches`
	q, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := Parse(Format(q))
	if err != nil {
		t.Fatal(err)
	}
	if Format(q) != Format(q2) {
		t.Fatal("unstable format")
	}
	if _, err = Parse(`span{ a = }`); err == nil {
		t.Fatal("expected diagnostic")
	}
}

func TestDurationAndPathLogs(t *testing.T) {
	q, err := Parse(`span{ span:duration >= 100ms } | path`)
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(q); got != `span{ span:duration >= 100ms }` {
		t.Fatalf("format=%q", got)
	}
	r := q.Eval(Trace{
		Spans: []Span{{ID: "root"}, {ID: "slow", ParentID: "root", Duration: int64(100 * time.Millisecond)}, {ID: "child", ParentID: "slow"}},
		Logs:  []Log{{ID: "root-log", SpanID: "root"}, {ID: "child-log", SpanID: "child"}},
	})
	if !reflect.DeepEqual(r.SelectedLogIDs, []string{"child-log", "root-log"}) {
		t.Fatalf("selected logs=%v", r.SelectedLogIDs)
	}
	if _, err := Parse(`span{ log:body = "wrong" }`); err == nil {
		t.Fatal("accepted log intrinsic in span selector")
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"", `span{}`, `log{ log:body *= "x" }`, `span{a=1} >> span{b=true}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		q, err := Parse(s)
		if err == nil {
			if _, err := Parse(Format(q)); err != nil {
				t.Fatalf("formatted query: %v", err)
			}
		}
	})
}
