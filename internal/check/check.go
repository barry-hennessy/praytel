// Package check judges the checks of a plan: never, some, every and counts
// over selections (see design/PIPELINE.md, "Checks"). A check judges each
// trace when it settles, again when a late event gives it a new revision,
// and once more when the run is final; it keeps a trace's judgement after
// the trace is evicted. Run-wide statements (some, a count on its own and
// the anchor rule) are decided when the run is final.
package check

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/query"
	"github.com/barry-hennessy/praytel/internal/view"
)

// Exit codes, when the child succeeded.
const (
	// FailExit is praytel's exit code when a check fails.
	FailExit = 4
	// IncompleteExit is praytel's exit code when no check fails but one
	// cannot be decided because data is missing.
	IncompleteExit = plan.IncompleteExit
)

// Outcome is the result of a check, or of a check on one trace.
type Outcome string

// Outcomes.
const (
	Pass       Outcome = "pass"
	Fail       Outcome = "fail"
	Incomplete Outcome = "incomplete"
)

// Format is how a check reports.
type Format string

// Formats.
const (
	Text   Format = "text"
	NDJSON Format = "ndjson"
)

// View is one check as a pipeline view.
type View struct {
	spec    plan.Check
	sel     query.Expr
	anchor  query.Expr // every only
	anchors []query.Expr
	format  Format

	traces  map[string]*judged
	outcome Outcome
}

var (
	_ view.View     = (*View)(nil)
	_ view.Finisher = (*View)(nil)
	_ view.Verdict  = (*View)(nil)
)

// judged is the last judgement of one trace.
type judged struct {
	revision int
	outcome  Outcome
	evidence []string // trace/span or trace/log:id keys
	shown    []string // the evidence for people: span names and short IDs
	reasons  []string // why the trace is incomplete
	anchors  []int    // matches of each anchor
	items    int      // items of Select, for run-wide counts
}

// New compiles a check.
func New(spec plan.Check, format Format) (*View, error) {
	v := &View{spec: spec, format: format, traces: map[string]*judged{}}
	var err error
	if v.sel, err = query.Parse(spec.Select); err != nil {
		return nil, err
	}
	if spec.Kind == plan.CheckEvery {
		if v.anchor, err = query.Parse(spec.Anchor); err != nil {
			return nil, err
		}
		v.anchors = append(v.anchors, v.anchor)
	}
	v.anchors = append(v.anchors, Anchors(v.sel)...)
	if v.anchor != nil {
		v.anchors = append(v.anchors, Anchors(v.anchor)...)
	}
	return v, nil
}

// Anchors are the selections that set the scope of a query: the left side
// of each > and >>. Each must match in the run, or the check never ran.
func Anchors(e query.Expr) []query.Expr {
	switch e := e.(type) {
	case query.Child:
		return append(append([]query.Expr{e.Parent}, Anchors(e.Parent)...), Anchors(e.Kid)...)
	case query.Desc:
		return append(append([]query.Expr{e.Anc}, Anchors(e.Anc)...), Anchors(e.Kid)...)
	case query.Both:
		return append(Anchors(e.L), Anchors(e.R)...)
	case query.Either:
		return append(Anchors(e.L), Anchors(e.R)...)
	case query.Inter:
		return append(Anchors(e.L), Anchors(e.R)...)
	case query.Not:
		return Anchors(e.X)
	}
	return nil
}

// Observe does nothing: a check judges whole traces.
func (v *View) Observe(domain.Event, projection.Reader) {}

// Render judges every trace r holds (one, when the driver sends a settled
// trace) and reports nothing yet: the report comes at Finish.
func (v *View) Render(r projection.Reader, _ view.Viewport) []string {
	for id, t := range query.TracesOf(r) {
		v.judge(r, id, t)
	}
	return nil
}

func (v *View) judge(r projection.Reader, id string, t *query.Trace) {
	rev := r.Revision(id)
	if j := v.traces[id]; j != nil && j.revision == rev && rev != 0 {
		return
	}
	j := &judged{revision: rev, outcome: Pass, reasons: r.Incomplete(id)}
	for _, a := range v.anchors {
		j.anchors = append(j.anchors, len(query.Eval(a, t).Spans))
	}
	res := query.Eval(v.sel, t)
	j.items = items(v.sel, res)

	switch v.spec.Kind {
	case plan.CheckNever:
		if !res.Empty() {
			// A bad item is bad whatever else is missing.
			j.outcome, j.evidence = Fail, keys(id, res)
			for _, sp := range slices.Sorted(maps.Keys(res.Spans)) {
				j.shown = append(j.shown, shown(t, sp))
			}
		}
	case plan.CheckEvery:
		for a := range query.Eval(v.anchor, t).Spans {
			n := below(t, a, res, v.sel)
			ok := n > 0
			if v.spec.Count != nil {
				ok = v.spec.Count.Accepts(n)
			}
			if !ok {
				j.outcome = Fail
				j.evidence = append(j.evidence, id+"/"+a)
				j.shown = append(j.shown, fmt.Sprintf("%s has %d", shown(t, a), n))
			}
		}
		slices.Sort(j.evidence)
		slices.Sort(j.shown)
	}
	if len(j.reasons) > 0 && (j.outcome == Pass || v.spec.Kind == plan.CheckEvery) {
		// Missing data could hide a failure, or a span that would pass.
		j.outcome = Incomplete
	}
	v.traces[id] = j
}

// items counts what a selection holds: logs for a log query, else spans.
func items(e query.Expr, r query.Result) int {
	if k, ok := query.RecordLocal(e); ok && k == query.KindLog {
		return len(r.Logs)
	}
	return len(r.Spans)
}

// below counts the items of res under the anchor span a: its descendant
// spans, or for a log query, the logs on a or under it.
func below(t *query.Trace, a string, res query.Result, sel query.Expr) int {
	under := func(spanID string) bool {
		for id, seen := spanID, map[string]bool{}; id != "" && !seen[id]; {
			seen[id] = true
			sp := t.Span(id)
			if sp == nil {
				return false
			}
			if sp.ParentID == a {
				return true
			}
			id = sp.ParentID
		}
		return false
	}
	if k, ok := query.RecordLocal(sel); ok && k == query.KindLog {
		n := 0
		for _, l := range t.Logs() {
			if res.Logs[l.ID] && (l.SpanID == a || under(l.SpanID)) {
				n++
			}
		}
		return n
	}
	n := 0
	for id := range res.Spans {
		if under(id) {
			n++
		}
	}
	return n
}

// shown is a span for people: its name and the start of its ID.
func shown(t *query.Trace, id string) string {
	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	if sp := t.Span(id); sp != nil && sp.Name != "" {
		return sp.Name + " (" + short + ")"
	}
	return short
}

func keys(trace string, r query.Result) []string {
	var out []string
	for id := range r.Spans {
		out = append(out, trace+"/"+id)
	}
	for id := range r.Logs {
		out = append(out, trace+"/log:"+id)
	}
	slices.Sort(out)
	return out
}

// Finish judges what has not been judged, decides the run-wide statements
// and reports.
func (v *View) Finish(r projection.Reader) []string {
	v.Render(r, view.Viewport{})

	ids := slices.Sorted(maps.Keys(v.traces))
	v.outcome = Pass
	var notes []string
	for _, id := range ids {
		switch v.traces[id].outcome {
		case Fail:
			v.outcome = Fail
		case Incomplete:
			if v.outcome == Pass {
				v.outcome = Incomplete
			}
		}
	}

	// The anchor rule: each anchor must match somewhere in the run.
	for i, a := range v.anchors {
		n := 0
		for _, j := range v.traces {
			if i < len(j.anchors) {
				n += j.anchors[i]
			}
		}
		if n == 0 {
			v.outcome = Fail
			notes = append(notes, "check never exercised: "+query.Format(a)+" matched nothing")
		}
	}

	total := 0
	for _, j := range v.traces {
		total += j.items
	}
	switch v.spec.Kind {
	case plan.CheckSome:
		if total == 0 {
			v.outcome = Fail
			notes = append(notes, "nothing matched "+query.Format(v.sel))
		}
	case plan.CheckCount:
		if !v.spec.Count.Accepts(total) {
			v.outcome = Fail
			notes = append(notes, fmt.Sprintf("%d matched, want %s %d", total, strings.ReplaceAll(v.spec.Count.Op, "_", " "), v.spec.Count.N))
		}
	}

	if v.format == NDJSON {
		return v.ndjson(ids, notes)
	}
	return v.text(ids, notes)
}

func (v *View) text(ids []string, notes []string) []string {
	lines := []string{fmt.Sprintf("praytel: check %s: %s", v.spec.Name, strings.ToUpper(string(v.outcome)))}
	for _, id := range ids {
		j := v.traces[id]
		switch j.outcome {
		case Fail:
			lines = append(lines, fmt.Sprintf("  trace %s revision %d: %s", id, j.revision, strings.Join(j.shown, ", ")))
		case Incomplete:
			lines = append(lines, fmt.Sprintf("  trace %s revision %d: incomplete: %s", id, j.revision, strings.Join(j.reasons, ", ")))
		}
	}
	for _, n := range notes {
		lines = append(lines, "  "+n)
	}
	return lines
}

type record struct {
	Check             string   `json:"check"`
	TraceID           string   `json:"trace_id,omitempty"`
	Revision          int      `json:"revision,omitempty"`
	Outcome           Outcome  `json:"outcome"`
	Evidence          []string `json:"evidence,omitempty"`
	IncompleteReasons []string `json:"incomplete_reasons,omitempty"`
	Notes             []string `json:"notes,omitempty"`
}

func (v *View) ndjson(ids []string, notes []string) []string {
	var lines []string
	emit := func(r record) {
		b, _ := json.Marshal(r)
		lines = append(lines, string(b))
	}
	for _, id := range ids {
		j := v.traces[id]
		emit(record{Check: v.spec.Name, TraceID: id, Revision: j.revision, Outcome: j.outcome,
			Evidence: j.evidence, IncompleteReasons: j.reasons})
	}
	emit(record{Check: v.spec.Name, Outcome: v.outcome, Notes: notes})
	return lines
}

// ExitCode is FailExit if the check failed, IncompleteExit if it could not
// be decided, and 0 if it passed. It is only meaningful after Finish.
func (v *View) ExitCode() int {
	switch v.outcome {
	case Fail:
		return FailExit
	case Incomplete:
		return IncompleteExit
	}
	return 0
}

// Outcome is the check's result, after Finish.
func (v *View) Outcome() Outcome { return v.outcome }
