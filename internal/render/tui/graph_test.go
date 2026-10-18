package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/praytel/internal/graph"
	"github.com/barry-hennessy/praytel/internal/graph/draw"
	"github.com/barry-hennessy/praytel/internal/query"
	tea "github.com/charmbracelet/bubbletea"
)

// The pure graph-aggregation and layering algorithm (containerKey,
// Hierarchy, componentGraph, detectFeedbackEdges, and graph.Build's own
// aggregation rule) is tested directly, as an external caller, in
// internal/render/tui/internal/graph, and node placement in its layout
// subpackage. The tests below exercise
// Model's live-filtering integration -- focus globs, typed queries, and
// fuzzy search narrowing the graph -- which has no meaning apart from
// Model's own state, so there is no cleaner surface to test it through
// than Model itself.

func TestBuildGraphRespectsFocusFilter(t *testing.T) {
	// Focus keeps a matched span's whole ancestor/descendant path, so the
	// fixture uses two unrelated roots (no parent/child link) to isolate
	// the effect of the glob itself rather than path expansion.
	filt, err := filter.New(filter.Query{Focus: []string{"api"}})
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(nil, filt)
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "api-root", Component: "api", Name: "request"})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "db-root", Component: "db", Name: "query"})

	g := m.buildGraph()
	if _, ok := g.Nodes[graph.ContainerKey([]string{"db", ".query"})]; ok {
		t.Fatalf("expected db to be filtered out by focus, nodes = %v", nodeKeys(g))
	}
	if _, ok := g.Nodes[graph.ContainerKey([]string{"api", ".request"})]; !ok {
		t.Fatalf("expected api to remain in focus, nodes = %v", nodeKeys(g))
	}
}

func TestBuildGraphRespectsTypedQuery(t *testing.T) {
	q, err := query.Parse(`span{ span:status = error } | path`)
	if err != nil {
		t.Fatal(err)
	}
	m := NewQueryModel(nil, nil, q)
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "root", Component: "api", Name: "request"})
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "bad", Component: "db", Name: "query", Status: domain.SpanStatusError})
	m.apply(domain.SpanParentLinked{TraceID: "t", SpanID: "bad", ParentSpanID: "root"})
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "sibling", Component: "cache", Name: "get"})
	m.apply(domain.SpanParentLinked{TraceID: "t", SpanID: "sibling", ParentSpanID: "root"})

	g := m.buildGraph()
	if _, ok := g.Nodes[graph.ContainerKey([]string{"cache", ".get"})]; ok {
		t.Fatalf("expected cache to be excluded by the typed query's path view, nodes = %v", nodeKeys(g))
	}
	if _, ok := g.Nodes[graph.ContainerKey([]string{"db", ".query"})]; !ok {
		t.Fatalf("expected db (the error span) to remain, nodes = %v", nodeKeys(g))
	}
}

func TestBuildGraphRespectsLiveFuzzySearchTraceMode(t *testing.T) {
	m := NewModel(nil, nil)
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Component: "api", Name: "orders"})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "health", Component: "api", Name: "health"})

	m.query = "ord"
	g := m.buildGraph()
	if len(g.Nodes) != 1 {
		t.Fatalf("expected only the matching trace's node, got %v", nodeKeys(g))
	}
	if g.Nodes[graph.ContainerKey([]string{"api", ".orders"})] == nil {
		t.Fatalf("expected api.orders node, got %v", nodeKeys(g))
	}
}

func TestBuildGraphRespectsLiveFuzzySearchLogMode(t *testing.T) {
	m := NewModel(nil, nil)
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Component: "api", Name: "orders"})
	m.apply(domain.LogReceived{ID: "1", TraceID: "t1", SpanID: "orders", Body: "payment accepted"})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "health", Component: "api", Name: "health"})
	m.apply(domain.LogReceived{ID: "2", TraceID: "t2", SpanID: "health", Body: "database unavailable"})

	m.mode = searchLogs
	m.query = "db unav"
	g := m.buildGraph()

	if len(g.Nodes) != 1 || g.Nodes[graph.ContainerKey([]string{"api", ".health"})] == nil {
		t.Fatalf("expected only the health span to survive log search, got %v", nodeKeys(g))
	}
}

func nodeKeys(g *graph.Graph) []string {
	keys := make([]string, 0, len(g.Nodes))
	for k := range g.Nodes {
		keys = append(keys, k)
	}
	return keys
}

func TestMatchGraphNodeMarksOnlyTheLabelsMatchedCharacters(t *testing.T) {
	g := graph.New()
	n := g.EnsureNode([]string{"api", ".orders"}, 1)

	ok, h := matchGraphNode("apiord", n)
	if !ok {
		t.Fatalf("expected query to match node label %q", n.Label())
	}
	// label is "api.orders"; "apiord" greedily consumes a-p-i-o-r-d, leaving
	// the trailing "ers" unmatched.
	want := []bool{true, true, true, false, true, true, true, false, false, false}
	if !reflect.DeepEqual(h.label, want) {
		t.Fatalf("label mask = %v, want %v", h.label, want)
	}
}

func TestGraphViewRendersBoxesLabelsAndAggregatedEdgeCount(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 30
	m.width = 200
	for _, trace := range []string{"t1", "t2"} {
		m.apply(domain.SpanUpserted{TraceID: trace, SpanID: "root", Component: "api", Name: "request"})
		m.apply(domain.SpanUpserted{TraceID: trace, SpanID: "child", Component: "db", Name: "query"})
		m.apply(domain.SpanParentLinked{TraceID: trace, SpanID: "child", ParentSpanID: "root"})
	}
	m.view = viewGraph

	view := m.View()
	for _, want := range []string{"api", "db", "\u25b6", "\u00d72"} {
		if !strings.Contains(view, want) {
			t.Fatalf("graph view missing %q:\n%s", want, view)
		}
	}
	if !strings.ContainsAny(view, "\u256d\u256e\u2570\u256f") {
		t.Fatalf("graph view has no box corners:\n%s", view)
	}
}

func TestGraphViewTogglesIndependentlyOfSearchMode(t *testing.T) {
	m := NewModel(nil, nil)
	if m.view != viewTree {
		t.Fatalf("expected default view to be viewTree")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(Model)
	if m.view != viewGraph {
		t.Fatalf("ctrl+g did not switch to graph view")
	}
	if m.mode != searchTraces {
		t.Fatalf("toggling view should not change search mode")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(Model)
	if m.view != viewTree {
		t.Fatalf("second ctrl+g did not switch back to tree view")
	}
}

// fanOutModel is one trace where root calls a, b (twice) and c, and a calls
// c as well -- so root has several outgoing calls, c several incoming, and
// root->c spans two layers.
func fanOutModel() Model {
	m := NewModel(nil, nil)
	add := func(id, parent, name string) {
		m.apply(domain.SpanUpserted{TraceID: "t", SpanID: id, Component: "svc", Name: name})
		if parent != "" {
			m.apply(domain.SpanParentLinked{TraceID: "t", SpanID: id, ParentSpanID: parent})
		}
	}
	add("root", "", "root")
	add("a", "root", "a")
	add("b1", "root", "b")
	add("b2", "root", "b")
	add("c1", "root", "c")
	add("c2", "a", "c")
	return m
}

func TestGraphDrawsAnArrowheadForEveryCall(t *testing.T) {
	g := fanOutModel().buildGraph()
	cv := layoutGraph(g, nil).c

	// Calls leaving one component share a trunk, but each must still reach
	// an arrowhead of its own.
	arrows := 0
	for _, row := range cv.Cells() {
		for _, cell := range row {
			switch cell.Rune {
			case '▶', '◀':
				arrows++
			}
		}
	}
	if arrows != len(g.Edges) {
		t.Fatalf("drew %d arrowheads for %d calls:\n%s", arrows, len(g.Edges), plainLines(cv))
	}
}

func TestGraphDrawsAComponentsBorderAndOutgoingCallsInItsColor(t *testing.T) {
	g := fanOutModel().buildGraph()
	cv := layoutGraph(g, nil).c

	// root is the first component seen calling another and makes three
	// calls; a is the second and makes one; b and c call nothing.
	first, second := stylePalette, stylePalette+1
	wantArrows := map[cellStyle]int{first: 3, second: 1}
	wantBoxes := map[cellStyle]int{first: 1, second: 1, styleNeutral: 2}
	gotArrows, gotBoxes := map[cellStyle]int{}, map[cellStyle]int{}
	for _, row := range cv.Cells() {
		for _, cell := range row {
			switch {
			case cell.Rune == '▶':
				gotArrows[cell.Style]++
			case cell.Rune == '╭' && !cell.Line: // a box's corner, not an edge's bend
				gotBoxes[cell.Style]++
			}
		}
	}
	if !reflect.DeepEqual(gotArrows, wantArrows) {
		t.Fatalf("arrowheads per style = %v, want %v:\n%s", gotArrows, wantArrows, plainLines(cv))
	}
	if !reflect.DeepEqual(gotBoxes, wantBoxes) {
		t.Fatalf("boxes per border style = %v, want %v:\n%s", gotBoxes, wantBoxes, plainLines(cv))
	}
	for i, color := range componentPalette {
		if cellStyles[stylePalette+cellStyle(i)].GetForeground() != color {
			t.Fatalf("palette style %d is not the tree's color %d", i, i)
		}
	}
}

func plainLines(cv *draw.Canvas) string {
	return strings.Join(cv.Lines(nil), "\n")
}
