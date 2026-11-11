package check_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/check"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/view"
)

func span(trace, id, parent, name string, status domain.SpanStatus) []domain.Event {
	evs := []domain.Event{
		domain.SpanUpserted{TraceID: trace, SpanID: id, Name: name, Status: status},
		domain.SpanClosed{TraceID: trace, SpanID: id},
	}
	if parent != "" {
		evs = append(evs, domain.SpanParentLinked{TraceID: trace, SpanID: id, ParentSpanID: parent})
	}
	return evs
}

// shop: two requests. r1 calls the db twice and fails in it; r2 calls it
// once. A cron job has no request.
func shop() *projection.Forest {
	f := projection.New()
	var evs []domain.Event
	evs = append(evs, span("t1", "r1", "", "request", 0)...)
	evs = append(evs, span("t1", "q1", "r1", "query", domain.SpanStatusError)...)
	evs = append(evs, span("t1", "q2", "r1", "query", 0)...)
	evs = append(evs, span("t2", "r2", "", "request", 0)...)
	evs = append(evs, span("t2", "q3", "r2", "query", 0)...)
	evs = append(evs, span("t3", "c", "", "cron", 0)...)
	for _, ev := range evs {
		f.Apply(ev)
	}
	return f
}

func judge(t *testing.T, c plan.Check, f *projection.Forest) (*check.View, []string) {
	t.Helper()
	if c.Name == "" {
		c.Name = c.Kind
	}
	v, err := check.New(c, check.Text)
	if err != nil {
		t.Fatal(err)
	}
	f.Finalize()
	return v, v.Finish(f)
}

func TestOutcomes(t *testing.T) {
	q := `span{ span:name = "query" }`
	r := `span{ span:name = "request" }`
	cases := map[string]struct {
		c    plan.Check
		want check.Outcome
		note string
	}{
		"never passes": {plan.Check{Kind: plan.CheckNever, Select: `span{ span:name = "DROP" }`}, check.Pass, ""},
		"never fails with the span": {plan.Check{Kind: plan.CheckNever, Select: `span{ span:status = error }`},
			check.Fail, "trace t1 revision"},
		"never with structure": {plan.Check{Kind: plan.CheckNever, Select: r + ` >> span{ span:status = error }`},
			check.Fail, "query (q1)"},
		"never with an anchor that never matched": {plan.Check{Kind: plan.CheckNever, Select: `span{ span:name = "checkout" } >> span{}`},
			check.Fail, "check never exercised"},
		"every passes": {plan.Check{Kind: plan.CheckEvery, Anchor: r, Select: q}, check.Pass, ""},
		"every fails for the anchor without one": {plan.Check{Kind: plan.CheckEvery, Anchor: `span{}`, Select: q},
			check.Fail, "query (q1)"},
		"every with at_most": {plan.Check{Kind: plan.CheckEvery, Anchor: r, Select: q, Count: &plan.Count{Op: "at_most", N: 1}},
			check.Fail, "request (r1) has 2"},
		"every with at_least": {plan.Check{Kind: plan.CheckEvery, Anchor: r, Select: q, Count: &plan.Count{Op: "at_least", N: 1}},
			check.Pass, ""},
		"every whose anchor never matched": {plan.Check{Kind: plan.CheckEvery, Anchor: `span{ span:name = "checkout" }`, Select: q},
			check.Fail, "check never exercised"},
		"some passes":  {plan.Check{Kind: plan.CheckSome, Select: `span{ span:name = "cron" }`}, check.Pass, ""},
		"some fails":   {plan.Check{Kind: plan.CheckSome, Select: `span{ span:name = "backup" }`}, check.Fail, "nothing matched"},
		"count passes": {plan.Check{Kind: plan.CheckCount, Select: q, Count: &plan.Count{Op: "exactly", N: 3}}, check.Pass, ""},
		"count fails":  {plan.Check{Kind: plan.CheckCount, Select: q, Count: &plan.Count{Op: "at_most", N: 2}}, check.Fail, "3 matched, want at most 2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			v, lines := judge(t, c.c, shop())
			if v.Outcome() != c.want {
				t.Fatalf("outcome %s, want %s\n%s", v.Outcome(), c.want, strings.Join(lines, "\n"))
			}
			if !strings.Contains(strings.Join(lines, "\n"), c.note) {
				t.Fatalf("report lacks %q:\n%s", c.note, strings.Join(lines, "\n"))
			}
			want := map[check.Outcome]int{check.Pass: 0, check.Fail: check.FailExit, check.Incomplete: check.IncompleteExit}[c.want]
			if v.ExitCode() != want {
				t.Fatalf("exit %d, want %d", v.ExitCode(), want)
			}
		})
	}
}

func TestIncompleteNeverPasses(t *testing.T) {
	f := shop()
	// A span in t2 names a parent that never arrived.
	f.Apply(domain.SpanUpserted{TraceID: "t2", SpanID: "lost", Name: "query"})
	f.Apply(domain.SpanParentLinked{TraceID: "t2", SpanID: "lost", ParentSpanID: "gone"})
	f.Apply(domain.SpanClosed{TraceID: "t2", SpanID: "lost"})
	v, lines := judge(t, plan.Check{Kind: plan.CheckNever, Select: `span{ span:name = "DROP" }`}, f)
	if v.Outcome() != check.Incomplete || v.ExitCode() != check.IncompleteExit {
		t.Fatalf("outcome %s\n%s", v.Outcome(), strings.Join(lines, "\n"))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "incomplete: unknown_parent") {
		t.Fatalf("no reason:\n%s", strings.Join(lines, "\n"))
	}
}

// A late span that arrives after a trace was judged gives it a new revision
// and a new judgement.
func TestLateSpanReopensTheJudgement(t *testing.T) {
	f := projection.New()
	for _, ev := range span("t", "r", "", "request", 0) {
		f.Apply(ev)
	}
	v, err := check.New(plan.Check{Kind: plan.CheckEvery, Name: "every",
		Anchor: `span{ span:name = "request" }`, Select: `span{ span:name = "auth" }`}, check.Text)
	if err != nil {
		t.Fatal(err)
	}
	v.Render(projection.OneTrace(f, "t"), view.Viewport{}) // settled: no auth yet

	for _, ev := range span("t", "a", "r", "auth", 0) {
		f.Apply(ev)
	}
	f.Finalize()
	lines := v.Finish(f)
	if v.Outcome() != check.Pass {
		t.Fatalf("outcome %s\n%s", v.Outcome(), strings.Join(lines, "\n"))
	}
}

// A judgement made before the trace was evicted stays.
func TestEvictedTraceKeepsItsJudgement(t *testing.T) {
	f := shop()
	v, err := check.New(plan.Check{Kind: plan.CheckNever, Name: "never", Select: `span{ span:status = error }`}, check.Text)
	if err != nil {
		t.Fatal(err)
	}
	v.Render(projection.OneTrace(f, "t1"), view.Viewport{})
	f.Apply(domain.ForestEvicted{TraceID: "t1"})
	f.Finalize()
	v.Finish(f)
	if v.Outcome() != check.Fail {
		t.Fatalf("outcome %s", v.Outcome())
	}
}

func TestNDJSON(t *testing.T) {
	v, err := check.New(plan.Check{Kind: plan.CheckNever, Name: "no errors", Select: `span{ span:status = error }`}, check.NDJSON)
	if err != nil {
		t.Fatal(err)
	}
	f := shop()
	f.Finalize()
	lines := v.Finish(f)
	if len(lines) != 4 { // three traces and the run
		t.Fatalf("%d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	var first, last map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[3]), &last); err != nil {
		t.Fatal(err)
	}
	if first["check"] != "no errors" || first["trace_id"] != "t1" || first["outcome"] != "fail" ||
		first["evidence"].([]any)[0] != "t1/q1" || first["revision"] == nil {
		t.Fatalf("first record %v", first)
	}
	if last["outcome"] != "fail" || last["trace_id"] != nil {
		t.Fatalf("run record %v", last)
	}
}
