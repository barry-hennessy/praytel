package filter

import (
	"fmt"
	"path"

	"github.com/barry-hennessy/praytel/internal/domain"
)

// Query is the v1 compiled-flag surface: glob focus (union) and exclude.
type Query struct {
	Focus   []string
	Exclude []string
}

// Filter projects correlator events and answers live-admit / InFocus.
// It does not sit as a drop-pipe: callers still see every event.
type Filter struct {
	focus   []string
	exclude []string

	spans map[string]*spanNode
	logs  map[string]*logRec
	order []string

	inFocus map[string]struct{}
}

type spanNode struct {
	id        string
	parent    string
	name      string
	component string
	children  map[string]struct{}
}

type logRec struct {
	id       string
	spanID   string
	admitted bool
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
		inFocus: make(map[string]struct{}),
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
	f.recomputeFocus()

	return f.collectNewAdmits()
}

// InFocus reports whether spanID is in the current expanded (then excluded) set.
func (f *Filter) InFocus(spanID string) bool {
	if spanID == "" {
		return f.passAll() || len(f.focus) == 0
	}

	_, ok := f.inFocus[spanID]

	return ok
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
		if e.Component != "" {
			n.component = e.Component
		}
	case domain.SpanParentLinked:
		if e.SpanID == "" || e.ParentSpanID == "" || e.SpanID == e.ParentSpanID {
			return
		}

		child := f.ensureSpan(e.SpanID)
		parent := f.ensureSpan(e.ParentSpanID)
		if child.parent != "" && child.parent != e.ParentSpanID {
			if old, ok := f.spans[child.parent]; ok {
				delete(old.children, child.id)
			}
		}

		child.parent = e.ParentSpanID
		parent.children[child.id] = struct{}{}
	case domain.LogReceived:
		f.rememberLog(e.ID, e.SpanID)
		if e.SpanID != "" {
			n := f.ensureSpan(e.SpanID)
			if e.SpanName != "" && n.name == "" {
				n.name = e.SpanName
			}
			if e.Component != "" {
				n.component = e.Component
			}
		}
	case domain.LogAssigned:
		f.rememberLog(e.ID, e.SpanID)
		if e.SpanID != "" {
			n := f.ensureSpan(e.SpanID)
			if e.SpanName != "" {
				n.name = e.SpanName
			}
		}
	}
}

func (f *Filter) rememberLog(id, spanID string) {
	if id == "" {
		return
	}

	rec, ok := f.logs[id]
	if !ok {
		rec = &logRec{id: id}
		f.logs[id] = rec
		f.order = append(f.order, id)
	}

	if spanID != "" {
		rec.spanID = spanID
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

	return n
}

func (f *Filter) recomputeFocus() {
	next := make(map[string]struct{})
	if f.passAll() {
		for id := range f.spans {
			next[id] = struct{}{}
		}

		f.inFocus = next

		return
	}

	if len(f.focus) == 0 {
		for id, n := range f.spans {
			if !f.excluded(n) {
				next[id] = struct{}{}
			}
		}

		f.inFocus = next

		return
	}

	pathSet := make(map[string]struct{})
	for id, n := range f.spans {
		if f.matchesFocus(n) {
			f.expandPath(id, pathSet)
		}
	}

	for id := range pathSet {
		n := f.spans[id]
		if n == nil || f.excluded(n) {
			continue
		}

		next[id] = struct{}{}
	}

	f.inFocus = next
}

func (f *Filter) expandPath(start string, pathSet map[string]struct{}) {
	for id := start; id != ""; {
		if _, seen := pathSet[id]; seen {
			break
		}

		pathSet[id] = struct{}{}
		n := f.spans[id]
		if n == nil {
			break
		}

		id = n.parent
	}

	f.walkDown(start, pathSet)
}

func (f *Filter) walkDown(id string, pathSet map[string]struct{}) {
	pathSet[id] = struct{}{}
	n := f.spans[id]
	if n == nil {
		return
	}

	for c := range n.children {
		if _, seen := pathSet[c]; seen {
			continue
		}

		f.walkDown(c, pathSet)
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
	ok, err := path.Match(glob, name)
	if err != nil {
		return false
	}

	return ok
}

func (f *Filter) collectNewAdmits() []string {
	var out []string
	for _, id := range f.order {
		rec := f.logs[id]
		if rec.admitted {
			continue
		}

		if !f.shouldAdmit(rec) {
			continue
		}

		rec.admitted = true
		out = append(out, id)
	}

	return out
}

func (f *Filter) shouldAdmit(rec *logRec) bool {
	if f.passAll() {
		return true
	}

	if rec.spanID == "" {
		return len(f.focus) == 0
	}

	_, ok := f.inFocus[rec.spanID]

	return ok
}
