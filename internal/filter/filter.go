package filter

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/barry-hennessy/praytel/internal/domain"
)

// Query is the v1 compiled-flag surface: glob focus (union) and exclude.
type Query struct {
	Focus   []string
	Exclude []string
}

// Filter projects correlator events and answers live-admit / InFocus.
// It does not sit as a drop-pipe: callers still see every event.
//
// A span is in focus when it, one of its ancestors or one of its
// descendants matches a focus glob -- or when there are no focus globs --
// and it doesn't itself match an exclude glob. That is kept up to date as
// each event arrives, from counts each span holds of the matches above and
// below it, so an event costs about the depth of the tree it lands in
// rather than the size of everything seen so far.
type Filter struct {
	focus   []string
	exclude []string

	spans  map[string]*spanNode
	logs   map[string]*logRec
	logSeq int

	// held is every log not yet admitted, by the span it belongs to ("" for
	// none), so a span coming into focus can let its own go.
	held map[string]map[*logRec]struct{}

	// What the event being applied may have let in: spans whose focus may
	// have changed, and logs whose span or component did.
	touchedSpans []*spanNode
	touchedLogs  []*logRec
}

type spanNode struct {
	id        string
	parent    string
	name      string
	component string
	children  map[string]struct{}

	matches  bool // its own name or component matches a focus glob
	excluded bool // its own name or component matches an exclude glob
	below    int  // spans that match among it and its descendants
	above    int  // spans that match among its ancestors
}

type logRec struct {
	id        string
	seq       int // arrival order
	spanID    string
	component string
	admitted  bool
}

func New(q Query) (*Filter, error) {
	if err := validateGlobs("focus", q.Focus); err != nil {
		return nil, err
	}

	if err := validateGlobs("exclude", q.Exclude); err != nil {
		return nil, err
	}

	return &Filter{
		focus:   append([]string(nil), q.Focus...),
		exclude: append([]string(nil), q.Exclude...),
		spans:   make(map[string]*spanNode),
		logs:    make(map[string]*logRec),
		held:    make(map[string]map[*logRec]struct{}),
	}, nil
}

func validateGlobs(kind string, patterns []string) error {
	for _, p := range patterns {
		if p == "" {
			return fmt.Errorf("%s glob is empty", kind)
		}

		if _, err := path.Match(p, "x"); err != nil {
			return fmt.Errorf("%s glob %q: %w", kind, p, err)
		}
	}

	return nil
}

func (f *Filter) passAll() bool {
	return len(f.focus) == 0 && len(f.exclude) == 0
}

// Observe updates the projection from an event and returns log IDs
// that entered the live-admit set for the first time.
func (f *Filter) Observe(ev domain.Event) []string {
	f.apply(ev)

	return f.collectNewAdmits()
}

// InFocus reports whether spanID is in the current expanded (then excluded) set.
func (f *Filter) InFocus(spanID string) bool {
	if f.passAll() {
		return true
	}

	if spanID == "" {
		return len(f.focus) == 0
	}

	n := f.spans[spanID]

	return n != nil && f.focused(n)
}

func (f *Filter) focused(n *spanNode) bool {
	return !n.excluded && (len(f.focus) == 0 || n.below > 0 || n.above > 0)
}

func (f *Filter) apply(ev domain.Event) {
	switch e := ev.(type) {
	case domain.SpanUpserted:
		if e.SpanID == "" {
			return
		}

		n := f.ensureSpan(e.SpanID)
		if e.Name != "" {
			n.name = e.Name
		}
		if e.Component != "" && !e.Synthetic {
			n.component = e.Component
		}
		f.rematch(n)
	case domain.SpanParentLinked:
		if e.SpanID == "" || e.ParentSpanID == "" || e.SpanID == e.ParentSpanID {
			return
		}

		f.link(f.ensureSpan(e.SpanID), f.ensureSpan(e.ParentSpanID))
	case domain.LogReceived:
		f.rememberLog(e.ID, e.SpanID, e.Component)
		if e.SpanID != "" {
			n := f.ensureSpan(e.SpanID)
			if e.SpanName != "" && n.name == "" {
				n.name = e.SpanName
			}
			if e.Component != "" && !e.Synthetic {
				n.component = e.Component
			}
			f.rematch(n)
		}
	case domain.LogAssigned:
		f.rememberLog(e.ID, e.SpanID, "")
		if e.SpanID != "" {
			n := f.ensureSpan(e.SpanID)
			if e.SpanName != "" {
				n.name = e.SpanName
			}
			f.rematch(n)
		}
	}
}

func (f *Filter) rememberLog(id, spanID, component string) {
	if id == "" {
		return
	}

	rec, ok := f.logs[id]
	if !ok {
		f.logSeq++
		rec = &logRec{id: id, seq: f.logSeq}
		f.logs[id] = rec
		f.hold(rec)
	}

	if spanID != "" && spanID != rec.spanID {
		f.release(rec)
		rec.spanID = spanID
		f.hold(rec)
	}
	if component != "" {
		rec.component = component
	}
	f.touchedLogs = append(f.touchedLogs, rec)
}

// hold files a log not yet admitted under its span.
func (f *Filter) hold(rec *logRec) {
	if rec.admitted {
		return
	}
	waiting := f.held[rec.spanID]
	if waiting == nil {
		waiting = make(map[*logRec]struct{})
		f.held[rec.spanID] = waiting
	}
	waiting[rec] = struct{}{}
}

func (f *Filter) release(rec *logRec) {
	waiting := f.held[rec.spanID]
	delete(waiting, rec)
	if len(waiting) == 0 {
		delete(f.held, rec.spanID)
	}
}

func (f *Filter) ensureSpan(id string) *spanNode {
	n, ok := f.spans[id]
	if ok {
		return n
	}

	n = &spanNode{
		id:       id,
		children: make(map[string]struct{}),
	}
	f.spans[id] = n
	// Even a span known only by ID may match: "*" matches an empty name.
	f.rematch(n)

	return n
}

// rematch brings n's own match and exclusion up to date with its name and
// component, and its ancestors' and descendants' counts with its match.
func (f *Filter) rematch(n *spanNode) {
	if f.passAll() {
		return
	}

	if excluded := f.excluded(n); excluded != n.excluded {
		n.excluded = excluded
		f.touchedSpans = append(f.touchedSpans, n)
	}

	matches := f.matchesFocus(n)
	if matches == n.matches {
		return
	}
	n.matches = matches
	delta := -1
	if matches {
		delta = 1
	}
	f.addBelow(n, delta)
	for c := range n.children {
		f.addAbove(f.spans[c], delta)
	}
}

// link makes parent child's parent. A link that would make child its own
// ancestor is ignored: only malformed telemetry asks for one, and the
// counts above and below every span rely on there being no cycles.
func (f *Filter) link(child, parent *spanNode) {
	if child.parent == parent.id {
		return
	}
	for p := parent; p != nil; p = f.parentOf(p) {
		if p == child {
			return
		}
	}

	if old := f.parentOf(child); old != nil {
		delete(old.children, child.id)
		f.addBelow(old, -child.below)
		f.addAbove(child, -(old.above + matchCount(old)))
	}

	child.parent = parent.id
	parent.children[child.id] = struct{}{}
	f.addBelow(parent, child.below)
	f.addAbove(child, parent.above+matchCount(parent))
}

func (f *Filter) parentOf(n *spanNode) *spanNode {
	if n.parent == "" {
		return nil
	}

	return f.spans[n.parent]
}

func matchCount(n *spanNode) int {
	if n.matches {
		return 1
	}

	return 0
}

// addBelow adds delta matches to the count below n and each of its
// ancestors.
func (f *Filter) addBelow(n *spanNode, delta int) {
	if delta == 0 {
		return
	}
	for ; n != nil; n = f.parentOf(n) {
		n.below += delta
		f.touchedSpans = append(f.touchedSpans, n)
	}
}

// addAbove adds delta matches to the count above n and each of its
// descendants.
func (f *Filter) addAbove(n *spanNode, delta int) {
	if delta == 0 {
		return
	}
	stack := []*spanNode{n}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n.above += delta
		f.touchedSpans = append(f.touchedSpans, n)
		for c := range n.children {
			stack = append(stack, f.spans[c])
		}
	}
}

func (f *Filter) matchesFocus(n *spanNode) bool {
	for _, g := range f.focus {
		if nameMatches(g, n.name) || nameMatches(g, n.component) {
			return true
		}
	}

	return false
}

func (f *Filter) excluded(n *spanNode) bool {
	for _, g := range f.exclude {
		if nameMatches(g, n.name) || nameMatches(g, n.component) {
			return true
		}
	}

	return false
}

func nameMatches(glob, name string) bool {
	if matchOne(glob, name) {
		return true
	}

	return matchOne(strings.ReplaceAll(glob, "/", "\x1f"), strings.ReplaceAll(name, "/", "\x1f"))
}

func matchOne(glob, name string) bool {
	ok, err := path.Match(glob, name)
	if err != nil {
		return false
	}

	return ok
}

// collectNewAdmits admits every held log that the event just applied let
// in, in the order the logs arrived.
func (f *Filter) collectNewAdmits() []string {
	var admitted []*logRec
	admit := func(rec *logRec) {
		if rec.admitted || !f.shouldAdmit(rec) {
			return
		}
		rec.admitted = true
		f.release(rec)
		admitted = append(admitted, rec)
	}

	for _, rec := range f.touchedLogs {
		admit(rec)
	}
	for _, n := range f.touchedSpans {
		if f.focused(n) {
			for rec := range f.held[n.id] {
				admit(rec)
			}
		}
	}
	f.touchedLogs, f.touchedSpans = f.touchedLogs[:0], f.touchedSpans[:0]

	slices.SortFunc(admitted, func(a, b *logRec) int { return a.seq - b.seq })
	out := make([]string, len(admitted))
	for i, rec := range admitted {
		out[i] = rec.id
	}

	return out
}

func (f *Filter) shouldAdmit(rec *logRec) bool {
	if f.passAll() {
		return true
	}

	if rec.component != "" && f.matchesAny(f.focus, rec.component) && !f.matchesAny(f.exclude, rec.component) {
		return true
	}

	if rec.spanID == "" {
		return len(f.focus) == 0
	}

	return f.InFocus(rec.spanID)
}

func (f *Filter) matchesAny(globs []string, name string) bool {
	if len(globs) == 0 || name == "" {
		return false
	}

	for _, g := range globs {
		if nameMatches(g, name) {
			return true
		}
	}

	return false
}
