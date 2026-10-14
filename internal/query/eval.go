package query

import (
	"regexp"
	"sort"
	"strings"
)

// Result is an evaluation result. Evidence IDs identify records that directly
// satisfied selectors. Selected IDs apply the requested view. All slices are
// deduplicated and lexicographically sorted for deterministic output.
type Result struct {
	Match                                                            bool
	EvidenceSpanIDs, EvidenceLogIDs, SelectedSpanIDs, SelectedLogIDs []string
}
type set map[string]bool
type outcome struct {
	ok                   bool
	spans, logs, anchors set
}

// Eval evaluates q independently against trace. Structural log operands use
// assigned SpanID as their anchor; pending logs therefore cannot participate in
// a relation, while remaining valid standalone evidence.
func (q *Query) Eval(t Trace) Result {
	if q == nil || q.root == nil {
		return Result{}
	}
	o := evalExpr(q.root, t)
	r := Result{Match: o.ok}
	if !o.ok {
		return r
	}
	r.EvidenceSpanIDs = keys(o.spans)
	r.EvidenceLogIDs = keys(o.logs)
	ss := clone(o.anchors)
	ls := clone(o.logs)
	switch q.view {
	case WholeTrace:
		ss = set{}
		ls = set{}
		for _, s := range t.Spans {
			ss[s.ID] = true
		}
		for _, l := range t.Logs {
			ls[l.ID] = true
		}
	case Path:
		parents := map[string]string{}
		for _, s := range t.Spans {
			parents[s.ID] = s.ParentID
		}
		matches := clone(ss)
		for id := range matches {
			seen := set{}
			for p := parents[id]; p != "" && !seen[p]; p = parents[p] {
				seen[p] = true
				ss[p] = true
			}
		}
		changed := true
		for changed {
			changed = false
			for _, s := range t.Spans {
				if matches[s.ParentID] && !matches[s.ID] {
					matches[s.ID] = true
					ss[s.ID] = true
					changed = true
				}
			}
		}
		for _, l := range t.Logs {
			if ss[l.SpanID] {
				ls[l.ID] = true
			}
		}
	}
	r.SelectedSpanIDs = keys(ss)
	r.SelectedLogIDs = keys(ls)
	return r
}
func evalExpr(e expr, t Trace) outcome {
	switch x := e.(type) {
	case *selector:
		o := outcome{spans: set{}, logs: set{}, anchors: set{}}
		if x.kind == "span" {
			for _, s := range t.Spans {
				if evalPred(x.pred, func(f string) (Value, bool) { return spanField(t, s, f) }) {
					o.spans[s.ID] = true
					o.anchors[s.ID] = true
				}
			}
		} else {
			for _, l := range t.Logs {
				if evalPred(x.pred, func(f string) (Value, bool) { return logField(t, l, f) }) {
					o.logs[l.ID] = true
					if l.SpanID != "" {
						o.anchors[l.SpanID] = true
					}
				}
			}
		}
		o.ok = len(o.spans)+len(o.logs) > 0
		return o
	case *binary:
		a, b := evalExpr(x.left, t), evalExpr(x.right, t)
		if x.op == "&&" || x.op == "||" {
			ok := a.ok && b.ok
			if x.op == "||" {
				ok = a.ok || b.ok
			}
			if !ok {
				return outcome{}
			}
			return outcome{true, union(a.spans, b.spans), union(a.logs, b.logs), union(a.anchors, b.anchors)}
		}
		parents := map[string]string{}
		for _, s := range t.Spans {
			parents[s.ID] = s.ParentID
		}
		leftRelated, rightRelated := set{}, set{}
		for l := range a.anchors {
			for r := range b.anchors {
				if (x.op == ">" && parents[r] == l) || (x.op == ">>" && ancestor(l, r, parents)) {
					leftRelated[l], rightRelated[r] = true, true
				}
			}
		}
		if len(leftRelated) == 0 {
			return outcome{}
		}
		spans := union(intersect(a.spans, leftRelated), intersect(b.spans, rightRelated))
		logs := set{}
		for _, l := range t.Logs {
			if (a.logs[l.ID] && leftRelated[l.SpanID]) || (b.logs[l.ID] && rightRelated[l.SpanID]) {
				logs[l.ID] = true
			}
		}
		return outcome{true, spans, logs, union(leftRelated, rightRelated)}
	}
	return outcome{}
}
func ancestor(a, b string, p map[string]string) bool {
	seen := set{}
	for x := p[b]; x != "" && !seen[x]; x = p[x] {
		if x == a {
			return true
		}
		seen[x] = true
	}
	return false
}
func evalPred(p pred, get func(string) (Value, bool)) bool {
	switch x := p.(type) {
	case nil:
		return true
	case *pbin:
		if x.op == "&&" {
			return evalPred(x.left, get) && evalPred(x.right, get)
		}
		return evalPred(x.left, get) || evalPred(x.right, get)
	case *pnot:
		return !evalPred(x.x, get)
	case *exists:
		_, ok := get(x.field)
		return ok
	case *compare:
		v, ok := get(x.field)
		if !ok {
			return false
		}
		return compareValues(v, x.value, x.op)
	}
	return false
}
func compareValues(a, b Value, op string) bool {
	if a.k != b.k {
		return false
	}
	if op == "=" {
		return equal(a, b)
	}
	if op == "!=" {
		return !equal(a, b)
	}
	if op == "*=" {
		return a.k == StringKind && strings.Contains(a.s, b.s)
	}
	if op == "=~" || op == "!~" {
		if a.k != StringKind {
			return false
		}
		r, e := regexp.Compile("^(?:" + b.s + ")$")
		if e != nil {
			return false
		}
		m := r.MatchString(a.s)
		if op == "!~" {
			return !m
		}
		return m
	}
	cmp := 0
	switch a.k {
	case StringKind:
		cmp = strings.Compare(a.s, b.s)
	case IntKind:
		if a.i < b.i {
			cmp = -1
		} else if a.i > b.i {
			cmp = 1
		}
	case FloatKind:
		if a.f < b.f {
			cmp = -1
		} else if a.f > b.f {
			cmp = 1
		}
	default:
		return false
	}
	switch op {
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	}
	return false
}
func spanField(t Trace, s Span, f string) (Value, bool) {
	switch f {
	case "span:name":
		return StringValue(s.Name), true
	case "span:kind":
		return StringValue(s.Kind), true
	case "span:status":
		return StringValue(s.Status), true
	case "span:status_message":
		return StringValue(s.StatusMessage), true
	case "span:duration":
		return IntValue(s.Duration), true
	case "trace:id":
		return StringValue(t.ID), true
	case "trace:duration":
		return IntValue(t.Duration), true
	}
	return attrs(f, s.Resource, s.Scope, s.Attributes, "span.")
}
func logField(t Trace, l Log, f string) (Value, bool) {
	switch f {
	case "log:body":
		return StringValue(l.Body), true
	case "log:source":
		return StringValue(l.Source), true
	case "log:severity_number":
		return IntValue(l.SeverityNumber), true
	case "log:severity_text":
		return StringValue(l.SeverityText), true
	case "trace:id":
		return StringValue(t.ID), true
	case "trace:duration":
		return IntValue(t.Duration), true
	}
	return attrs(f, l.Resource, l.Scope, l.Attributes, "log.")
}
func attrs(f string, r Resource, s Scope, a []Attr, prefix string) (Value, bool) {
	var xs []Attr
	if strings.HasPrefix(f, "resource.") {
		xs = r.Attributes
		f = strings.TrimPrefix(f, "resource.")
	} else if strings.HasPrefix(f, "scope.") {
		if f == "scope.name" {
			return StringValue(s.Name), true
		}
		if f == "scope.version" {
			return StringValue(s.Version), true
		}
		xs = s.Attributes
		f = strings.TrimPrefix(f, "scope.")
	} else {
		xs = a
		f = strings.TrimPrefix(f, prefix)
	}
	for _, x := range xs {
		if x.Key == f {
			return x.Value, true
		}
	}
	return Value{}, false
}
func union(a, b set) set {
	c := clone(a)
	for x := range b {
		c[x] = true
	}
	return c
}
func intersect(a, b set) set {
	c := set{}
	for x := range a {
		if b[x] {
			c[x] = true
		}
	}
	return c
}
func clone(a set) set {
	c := set{}
	for x := range a {
		c[x] = true
	}
	return c
}
func keys(s set) []string {
	r := make([]string, 0, len(s))
	for x := range s {
		r = append(r, x)
	}
	sort.Strings(r)
	return r
}
