package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/praytel/internal/query"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHierarchyPathSplitsComponentSectionsAndFunction(t *testing.T) {
	cases := []struct {
		name          string
		component     string
		spanName      string
		wantContainer []string
		wantFunction  string
	}{
		{
			name:          "component with nested sections",
			component:     "Foo",
			spanName:      "Bar.Qux.Baz",
			wantContainer: []string{"Foo", "Bar", "Qux"},
			wantFunction:  "Baz",
		},
		{
			name:          "no dots in name",
			component:     "Foo",
			spanName:      "health",
			wantContainer: []string{"Foo"},
			wantFunction:  "health",
		},
		{
			name:          "empty name",
			component:     "Foo",
			spanName:      "",
			wantContainer: []string{"Foo"},
			wantFunction:  "",
		},
		{
			name:          "empty component",
			component:     "",
			spanName:      "Bar.Baz",
			wantContainer: []string{"Bar"},
			wantFunction:  "Baz",
		},
		{
			name:          "empty component and no dots in name",
			component:     "",
			spanName:      "span/12345678",
			wantContainer: []string{"span/12345678"},
			wantFunction:  "",
		},
		{
			name:          "both empty",
			component:     "",
			spanName:      "",
			wantContainer: nil,
			wantFunction:  "",
		},
		{
			name:          "name redundantly prefixed with component",
			component:     "Foo",
			spanName:      "Foo.Bar.Baz",
			wantContainer: []string{"Foo", "Bar"},
			wantFunction:  "Baz",
		},
		{
			name:          "name equals component",
			component:     "Foo",
			spanName:      "Foo",
			wantContainer: []string{"Foo"},
			wantFunction:  "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotContainer, gotFunction := hierarchyPath(c.component, c.spanName)
			if !reflect.DeepEqual(gotContainer, c.wantContainer) {
				t.Fatalf("hierarchyPath(%q, %q) container = %v, want %v", c.component, c.spanName, gotContainer, c.wantContainer)
			}
			if gotFunction != c.wantFunction {
				t.Fatalf("hierarchyPath(%q, %q) function = %q, want %q", c.component, c.spanName, gotFunction, c.wantFunction)
			}
		})
	}
}

func TestBuildGraphProducesCrossAndIntraComponentEdgesFromTheSameRule(t *testing.T) {
	m := NewModel(nil, nil)
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "root", Component: "api", Name: "Handlers.Orders.validate"})
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "sibling", Component: "api", Name: "Handlers.Payments.charge"})
	m.apply(domain.SpanParentLinked{TraceID: "t", SpanID: "sibling", ParentSpanID: "root"})
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "query", Component: "db", Name: "query"})
	m.apply(domain.SpanParentLinked{TraceID: "t", SpanID: "query", ParentSpanID: "root"})

	g := m.buildGraph()

	orders := containerKey([]string{"api", "Handlers", "Orders"})
	payments := containerKey([]string{"api", "Handlers", "Payments"})
	db := containerKey([]string{"db"})

	for _, key := range []string{orders, payments, db} {
		if _, ok := g.nodes[key]; !ok {
			t.Fatalf("missing node %q; nodes = %v", key, nodeKeys(g))
		}
	}

	intra, ok := g.edges[[2]string{orders, payments}]
	if !ok || intra.count != 1 {
		t.Fatalf("expected one intra-component edge Orders->Payments, got %+v (ok=%v)", intra, ok)
	}
	cross, ok := g.edges[[2]string{orders, db}]
	if !ok || cross.count != 1 {
		t.Fatalf("expected one cross-component edge Orders->db, got %+v (ok=%v)", cross, ok)
	}
}

func TestBuildGraphFoldsSameContainerCallsIntoFunctionTallyNotEdges(t *testing.T) {
	m := NewModel(nil, nil)
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "root", Component: "api", Name: "Handlers.Orders.validate"})
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "child", Component: "api", Name: "Handlers.Orders.create"})
	m.apply(domain.SpanParentLinked{TraceID: "t", SpanID: "child", ParentSpanID: "root"})

	g := m.buildGraph()

	if len(g.edges) != 0 {
		t.Fatalf("expected no edges for calls within the same container, got %v", g.edges)
	}
	orders := containerKey([]string{"api", "Handlers", "Orders"})
	node, ok := g.nodes[orders]
	if !ok {
		t.Fatalf("missing node %q", orders)
	}
	if node.spanCount != 2 {
		t.Fatalf("spanCount = %d, want 2", node.spanCount)
	}
	if node.functions["validate"] != 1 || node.functions["create"] != 1 {
		t.Fatalf("functions tally = %v, want validate:1 create:1", node.functions)
	}
}

func TestBuildGraphAggregatesRepeatedEdgesIntoOneCount(t *testing.T) {
	m := NewModel(nil, nil)
	for i, id := range []string{"t1", "t2"} {
		m.apply(domain.SpanUpserted{TraceID: id, SpanID: "root", Component: "api", Name: "request"})
		m.apply(domain.SpanUpserted{TraceID: id, SpanID: "child", Component: "db", Name: "query"})
		m.apply(domain.SpanParentLinked{TraceID: id, SpanID: "child", ParentSpanID: "root"})
		_ = i
	}

	g := m.buildGraph()
	api := containerKey([]string{"api"})
	db := containerKey([]string{"db"})
	e, ok := g.edges[[2]string{api, db}]
	if !ok || e.count != 2 {
		t.Fatalf("expected aggregated edge count 2 across two traces, got %+v (ok=%v)", e, ok)
	}
}

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
	if _, ok := g.nodes[containerKey([]string{"db"})]; ok {
		t.Fatalf("expected db to be filtered out by focus, nodes = %v", nodeKeys(g))
	}
	if _, ok := g.nodes[containerKey([]string{"api"})]; !ok {
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
	if _, ok := g.nodes[containerKey([]string{"cache"})]; ok {
		t.Fatalf("expected cache to be excluded by the typed query's path view, nodes = %v", nodeKeys(g))
	}
	if _, ok := g.nodes[containerKey([]string{"db"})]; !ok {
		t.Fatalf("expected db (the error span) to remain, nodes = %v", nodeKeys(g))
	}
}

func TestBuildGraphRespectsLiveFuzzySearchTraceMode(t *testing.T) {
	m := NewModel(nil, nil)
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Component: "api", Name: "orders"})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "health", Component: "api", Name: "health"})

	m.query = "ord"
	g := m.buildGraph()
	if len(g.nodes) != 1 {
		t.Fatalf("expected only the matching trace's node, got %v", nodeKeys(g))
	}
	api := containerKey([]string{"api"})
	if g.nodes[api] == nil || g.nodes[api].functions["orders"] == 0 {
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

	api := containerKey([]string{"api"})
	node := g.nodes[api]
	if node == nil || node.functions["health"] == 0 || node.functions["orders"] != 0 {
		t.Fatalf("expected only the health span to survive log search, got %+v", node)
	}
}

func nodeKeys(g *componentGraph) []string {
	keys := make([]string, 0, len(g.nodes))
	for k := range g.nodes {
		keys = append(keys, k)
	}
	return keys
}

func TestAssignLayersRanksALinearChain(t *testing.T) {
	g := newComponentGraph()
	a := g.ensureNode([]string{"A"}, 1)
	b := g.ensureNode([]string{"B"}, 2)
	c := g.ensureNode([]string{"C"}, 3)
	g.ensureEdge(a.key, b.key, 1).count++
	g.ensureEdge(b.key, c.key, 2).count++

	forward, feedback := detectFeedbackEdges(g)
	if len(feedback) != 0 {
		t.Fatalf("expected no feedback edges in a linear chain, got %v", feedback)
	}
	layer := assignLayers(g, forward)
	if layer[a.key] != 0 || layer[b.key] != 1 || layer[c.key] != 2 {
		t.Fatalf("layers = %v, want A:0 B:1 C:2", layer)
	}
}

func TestDetectFeedbackEdgesTerminatesAndFlagsExactlyOneEdgeInATwoCycle(t *testing.T) {
	g := newComponentGraph()
	a := g.ensureNode([]string{"A"}, 1)
	b := g.ensureNode([]string{"B"}, 2)
	g.ensureEdge(a.key, b.key, 1).count++
	g.ensureEdge(b.key, a.key, 2).count++

	forward, feedback := detectFeedbackEdges(g)
	if len(feedback) != 1 {
		t.Fatalf("expected exactly one feedback edge in a 2-cycle, got %d: %v", len(feedback), feedback)
	}
	layer := assignLayers(g, forward)
	if _, ok := layer[a.key]; !ok {
		t.Fatalf("node A has no assigned layer")
	}
	if _, ok := layer[b.key]; !ok {
		t.Fatalf("node B has no assigned layer")
	}
}

func TestDetectFeedbackEdgesIsDeterministic(t *testing.T) {
	build := func() *componentGraph {
		g := newComponentGraph()
		a := g.ensureNode([]string{"A"}, 1)
		b := g.ensureNode([]string{"B"}, 2)
		c := g.ensureNode([]string{"C"}, 3)
		g.ensureEdge(a.key, b.key, 1).count++
		g.ensureEdge(b.key, c.key, 2).count++
		g.ensureEdge(c.key, a.key, 3).count++
		return g
	}

	g1, g2 := build(), build()
	f1, fb1 := detectFeedbackEdges(g1)
	f2, fb2 := detectFeedbackEdges(g2)
	if !reflect.DeepEqual(f1, f2) {
		t.Fatalf("forward adjacency not deterministic: %v vs %v", f1, f2)
	}
	if !reflect.DeepEqual(fb1, fb2) {
		t.Fatalf("feedback set not deterministic: %v vs %v", fb1, fb2)
	}
	l1 := assignLayers(g1, f1)
	l2 := assignLayers(g2, f2)
	if !reflect.DeepEqual(l1, l2) {
		t.Fatalf("layer assignment not deterministic: %v vs %v", l1, l2)
	}
}

func TestMatchGraphNodeMarksOnlyTheLabelsMatchedCharacters(t *testing.T) {
	g := newComponentGraph()
	n := g.ensureNode([]string{"api", "orders"}, 1)
	n.functions["create"] = 1

	ok, h := matchGraphNode("apiord", n)
	if !ok {
		t.Fatalf("expected query to match node label %q", strings.Join(n.path, "."))
	}
	// label is "api.orders"; "apiord" greedily consumes a-p-i-o-r-d, leaving
	// the trailing "ers" unmatched.
	want := []bool{true, true, true, false, true, true, true, false, false, false}
	if !reflect.DeepEqual(h.label, want) {
		t.Fatalf("label mask = %v, want %v", h.label, want)
	}
}

func TestMatchGraphNodeMatchesOnAFoldedFunctionName(t *testing.T) {
	g := newComponentGraph()
	n := g.ensureNode([]string{"api", "orders"}, 1)
	n.functions["frobnicate"] = 1

	ok, _ := matchGraphNode("frobnicate", n)
	if !ok {
		t.Fatalf("expected query matching a folded function name to still mark the node visible")
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
	for _, want := range []string{"api", "db", "\u25b6", "(\u00d72)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("graph view missing %q:\n%s", want, view)
		}
	}
	if !strings.ContainsAny(view, "\u250c\u2510\u2514\u2518") {
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
