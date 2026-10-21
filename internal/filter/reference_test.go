package filter_test

import (
	"fmt"
	"math/rand"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
)

// reference is the filter's rules worked out from scratch after every
// event, the slow way: a span is in focus when it, an ancestor or a
// descendant matches a focus glob (or there are none), and it doesn't
// match an exclude glob; a held log is let in once its component matches,
// or its span is in focus.
type reference struct {
	q     filter.Query
	spans map[string]*refSpan
	logs  map[string]*refLog
	seq   int
}

type refSpan struct{ name, component, parent string }

type refLog struct {
	spanID, component string
	seq               int
	admitted          bool
}

func (r *reference) span(id string) *refSpan {
	if r.spans[id] == nil {
		r.spans[id] = &refSpan{}
	}
	return r.spans[id]
}

func (r *reference) log(id, spanID, component string) {
	if id == "" {
		return
	}
	l := r.logs[id]
	if l == nil {
		r.seq++
		l = &refLog{seq: r.seq}
		r.logs[id] = l
	}
	if spanID != "" {
		l.spanID = spanID
	}
	if component != "" {
		l.component = component
	}
}

func (r *reference) apply(ev domain.Event) {
	switch e := ev.(type) {
	case domain.SpanUpserted:
		if e.SpanID == "" {
			return
		}
		s := r.span(e.SpanID)
		if e.Name != "" {
			s.name = e.Name
		}
		if e.Component != "" && !e.Synthetic {
			s.component = e.Component
		}
	case domain.SpanParentLinked:
		if e.SpanID == "" || e.ParentSpanID == "" || e.SpanID == e.ParentSpanID {
			return
		}
		child := r.span(e.SpanID)
		r.span(e.ParentSpanID)
		for p := e.ParentSpanID; p != ""; p = r.spans[p].parent {
			if p == e.SpanID {
				return // would make a cycle
			}
		}
		child.parent = e.ParentSpanID
	case domain.LogReceived:
		r.log(e.ID, e.SpanID, e.Component)
		if e.SpanID != "" {
			s := r.span(e.SpanID)
			if e.SpanName != "" && s.name == "" {
				s.name = e.SpanName
			}
			if e.Component != "" && !e.Synthetic {
				s.component = e.Component
			}
		}
	case domain.LogAssigned:
		r.log(e.ID, e.SpanID, "")
		if e.SpanID != "" {
			s := r.span(e.SpanID)
			if e.SpanName != "" {
				s.name = e.SpanName
			}
		}
	}
}

func refMatch(globs []string, names ...string) bool {
	for _, g := range globs {
		for _, name := range names {
			if ok, _ := path.Match(g, name); ok {
				return true
			}
			if ok, _ := path.Match(strings.ReplaceAll(g, "/", "\x1f"), strings.ReplaceAll(name, "/", "\x1f")); ok {
				return true
			}
		}
	}
	return false
}

func (r *reference) inFocus(id string) bool {
	if len(r.q.Focus) == 0 && len(r.q.Exclude) == 0 {
		return true
	}
	if id == "" {
		return len(r.q.Focus) == 0
	}
	s := r.spans[id]
	if s == nil || refMatch(r.q.Exclude, s.name, s.component) {
		return false
	}
	if len(r.q.Focus) == 0 {
		return true
	}
	matches := func(id string) bool { return refMatch(r.q.Focus, r.spans[id].name, r.spans[id].component) }
	for p := id; p != ""; p = r.spans[p].parent {
		if matches(p) {
			return true
		}
	}
	for other := range r.spans {
		if !matches(other) {
			continue
		}
		for p := other; p != ""; p = r.spans[p].parent {
			if p == id {
				return true
			}
		}
	}
	return false
}

func (r *reference) admitted() []string {
	var ids []string
	for id, l := range r.logs {
		if l.admitted {
			continue
		}
		component := l.component != "" && refMatch(r.q.Focus, l.component) && (len(r.q.Exclude) == 0 || !refMatch(r.q.Exclude, l.component))
		if (len(r.q.Focus) == 0 && len(r.q.Exclude) == 0) || component || r.inFocus(l.spanID) {
			l.admitted = true
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int { return r.logs[a].seq - r.logs[b].seq })
	return ids
}

func TestFilterMatchesTheReferenceRules(t *testing.T) {
	queries := []filter.Query{
		{Focus: []string{"orders*"}},
		{Focus: []string{"orders/*", "db"}},
		{Exclude: []string{"health"}},
		{Focus: []string{"orders*"}, Exclude: []string{"health", "db"}},
		{Focus: []string{"*"}},
		{Focus: []string{"orders"}, Exclude: []string{"*"}},
		{},
	}
	names := []string{"", "orders", "orders/v2", "health", "work"}
	components := []string{"", "api", "db", "health"}

	for qi, q := range queries {
		for seed := range int64(25) {
			t.Run(fmt.Sprintf("query%d/seed%d", qi, seed), func(t *testing.T) {
				rng := rand.New(rand.NewSource(seed))
				f, err := filter.New(q)
				if err != nil {
					t.Fatal(err)
				}
				ref := &reference{q: q, spans: map[string]*refSpan{}, logs: map[string]*refLog{}}
				spanID := func() string { return fmt.Sprintf("s%d", rng.Intn(25)) }
				pick := func(from []string) string { return from[rng.Intn(len(from))] }

				var history []string
				for step := range 300 {
					var ev domain.Event
					switch rng.Intn(5) {
					case 0, 1:
						ev = domain.SpanUpserted{SpanID: spanID(), Name: pick(names), Component: pick(components), Synthetic: rng.Intn(5) == 0}
					case 2:
						ev = domain.SpanParentLinked{SpanID: spanID(), ParentSpanID: spanID()}
					case 3:
						id := ""
						if rng.Intn(4) > 0 {
							id = spanID()
						}
						ev = domain.LogReceived{ID: fmt.Sprintf("L%d", rng.Intn(120)), SpanID: id, Component: pick(components), SpanName: pick(names), Synthetic: rng.Intn(5) == 0}
					case 4:
						ev = domain.LogAssigned{ID: fmt.Sprintf("L%d", rng.Intn(120)), SpanID: spanID(), SpanName: pick(names)}
					}
					history = append(history, fmt.Sprintf("%#v", ev))

					got := f.Observe(ev)
					ref.apply(ev)
					if want := ref.admitted(); !slices.Equal(got, want) {
						t.Fatalf("step %d: admitted %v, want %v\n%s", step, got, want, strings.Join(history, "\n"))
					}
					for _, id := range append(slices.Collect(func(yield func(string) bool) {
						for id := range ref.spans {
							if !yield(id) {
								return
							}
						}
					}), "", "unknown") {
						if got, want := f.InFocus(id), ref.inFocus(id); got != want {
							t.Fatalf("step %d: InFocus(%q) = %v, want %v\n%s", step, id, got, want, strings.Join(history, "\n"))
						}
					}
				}
			})
		}
	}
}
