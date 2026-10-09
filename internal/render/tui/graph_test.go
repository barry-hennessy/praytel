package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/praytel/internal/query"
	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
	tea "github.com/charmbracelet/bubbletea"
)

// The pure graph-aggregation and layering algorithm (containerKey,
// hierarchyPath, componentGraph, detectFeedbackEdges, and graph.Build's own
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
	if _, ok := g.Nodes[graph.ContainerKey([]string{"db"})]; ok {
		t.Fatalf("expected db to be filtered out by focus, nodes = %v", nodeKeys(g))
	}
	if _, ok := g.Nodes[graph.ContainerKey([]string{"api"})]; !ok {
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
	if _, ok := g.Nodes[graph.ContainerKey([]string{"cache"})]; ok {
		t.Fatalf("expected cache to be excluded by the typed query's path view, nodes = %v", nodeKeys(g))
	}
	if _, ok := g.Nodes[graph.ContainerKey([]string{"db"})]; !ok {
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
	api := graph.ContainerKey([]string{"api"})
	if g.Nodes[api] == nil || g.Nodes[api].Functions["orders"] == 0 {
		t.Fatalf("expected api node with orders function, got %v", nodeKeys(g))
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

	api := graph.ContainerKey([]string{"api"})
	node := g.Nodes[api]
	if node == nil || node.Functions["health"] == 0 || node.Functions["orders"] != 0 {
		t.Fatalf("expected only the health span to survive log search, got %+v", node)
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
	n := g.EnsureNode([]string{"api", "orders"}, 1)
	n.Functions["create"] = 1

	ok, h := matchGraphNode("apiord", n)
	if !ok {
		t.Fatalf("expected query to match node label %q", strings.Join(n.Path, "."))
	}
	// label is "api.orders"; "apiord" greedily consumes a-p-i-o-r-d, leaving
	// the trailing "ers" unmatched.
	want := []bool{true, true, true, false, true, true, true, false, false, false}
	if !reflect.DeepEqual(h.label, want) {
		t.Fatalf("label mask = %v, want %v", h.label, want)
	}
}

func TestMatchGraphNodeMatchesOnAFoldedFunctionName(t *testing.T) {
	g := graph.New()
	n := g.EnsureNode([]string{"api", "orders"}, 1)
	n.Functions["frobnicate"] = 1

	ok, _ := matchGraphNode("frobnicate", n)
	if !ok {
		t.Fatalf("expected query matching a folded function name to still mark the node visible")
	}
}

func TestMatchGraphNodeMarksTheMatchedCharactersOfAFunctionName(t *testing.T) {
	n := &graph.Node{Path: []string{"api"}, Functions: map[string]int{"frobnicate": 1, "list": 1}}

	ok, h := matchGraphNode("frob", n)
	if !ok {
		t.Fatalf("expected a match on the function name")
	}
	want := []bool{true, true, true, true}
	if got := h.fns["frobnicate"]; len(got) < 4 || !reflect.DeepEqual(got[:4], want) {
		t.Fatalf("function mask %v, want it to start %v", got, want)
	}
	for _, hit := range h.fns["list"] {
		if hit {
			t.Fatalf("unmatched function marked: %v", h.fns["list"])
		}
	}
}

func TestBuildGraphGivesNodesTheirTreeDepth(t *testing.T) {
	m := NewModel(nil, nil)
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "root", Component: "api", Name: "request"})
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "mid", Component: "orders", Name: "create"})
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "leaf", Component: "db", Name: "insert"})
	m.apply(domain.SpanParentLinked{TraceID: "t", SpanID: "mid", ParentSpanID: "root"})
	m.apply(domain.SpanParentLinked{TraceID: "t", SpanID: "leaf", ParentSpanID: "mid"})

	g := m.buildGraph()
	for name, want := range map[string]int{"api": 0, "orders": 1, "db": 2} {
		if got := g.Nodes[graph.ContainerKey([]string{name})].Depth; got != want {
			t.Fatalf("node %s at depth %d, want %d", name, got, want)
		}
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
