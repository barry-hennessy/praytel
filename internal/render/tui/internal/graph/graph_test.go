package graph_test

import (
	"reflect"
	"testing"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
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
			gotContainer, gotFunction := graph.HierarchyPath(c.component, c.spanName)
			if !reflect.DeepEqual(gotContainer, c.wantContainer) {
				t.Fatalf("HierarchyPath(%q, %q) container = %v, want %v", c.component, c.spanName, gotContainer, c.wantContainer)
			}
			if gotFunction != c.wantFunction {
				t.Fatalf("HierarchyPath(%q, %q) function = %q, want %q", c.component, c.spanName, gotFunction, c.wantFunction)
			}
		})
	}
}

func TestBuildFoldsSameContainerCallsIntoFunctionTallyNotEdges(t *testing.T) {
	g := graph.Build([]graph.SpanFact{
		{Component: "api", Name: "Handlers.Orders.validate", Order: 1},
		{Component: "api", Name: "Handlers.Orders.create", Order: 2, HasParent: true, ParentComponent: "api", ParentName: "Handlers.Orders.validate", ParentOrder: 1},
	})

	if len(g.Edges) != 0 {
		t.Fatalf("expected no edges for calls within the same container, got %v", g.Edges)
	}
	orders := graph.ContainerKey([]string{"api", "Handlers", "Orders"})
	node, ok := g.Nodes[orders]
	if !ok {
		t.Fatalf("missing node %q", orders)
	}
	if node.SpanCount != 2 {
		t.Fatalf("SpanCount = %d, want 2", node.SpanCount)
	}
	if node.Functions["validate"] != 1 || node.Functions["create"] != 1 {
		t.Fatalf("functions tally = %v, want validate:1 create:1", node.Functions)
	}
}

func TestBuildProducesCrossAndIntraComponentEdgesFromTheSameRule(t *testing.T) {
	g := graph.Build([]graph.SpanFact{
		{Component: "api", Name: "Handlers.Orders.validate", Order: 1},
		{Component: "api", Name: "Handlers.Payments.charge", Order: 2, HasParent: true, ParentComponent: "api", ParentName: "Handlers.Orders.validate", ParentOrder: 1},
		{Component: "db", Name: "query", Order: 3, HasParent: true, ParentComponent: "api", ParentName: "Handlers.Orders.validate", ParentOrder: 1},
	})

	orders := graph.ContainerKey([]string{"api", "Handlers", "Orders"})
	payments := graph.ContainerKey([]string{"api", "Handlers", "Payments"})
	db := graph.ContainerKey([]string{"db"})

	for _, key := range []string{orders, payments, db} {
		if _, ok := g.Nodes[key]; !ok {
			t.Fatalf("missing node %q; nodes = %v", key, nodeKeys(g))
		}
	}

	intra, ok := g.Edges[[2]string{orders, payments}]
	if !ok || intra.Count != 1 {
		t.Fatalf("expected one intra-component edge Orders->Payments, got %+v (ok=%v)", intra, ok)
	}
	cross, ok := g.Edges[[2]string{orders, db}]
	if !ok || cross.Count != 1 {
		t.Fatalf("expected one cross-component edge Orders->db, got %+v (ok=%v)", cross, ok)
	}
}

func TestBuildAggregatesRepeatedEdgesIntoOneCount(t *testing.T) {
	g := graph.Build([]graph.SpanFact{
		{Component: "api", Name: "request", Order: 1},
		{Component: "db", Name: "query", Order: 2, HasParent: true, ParentComponent: "api", ParentName: "request", ParentOrder: 1},
		{Component: "api", Name: "request", Order: 3},
		{Component: "db", Name: "query", Order: 4, HasParent: true, ParentComponent: "api", ParentName: "request", ParentOrder: 3},
	})

	api := graph.ContainerKey([]string{"api"})
	db := graph.ContainerKey([]string{"db"})
	e, ok := g.Edges[[2]string{api, db}]
	if !ok || e.Count != 2 {
		t.Fatalf("expected aggregated edge count 2 across two traces, got %+v (ok=%v)", e, ok)
	}
}

func nodeKeys(g *graph.Graph) []string {
	keys := make([]string, 0, len(g.Nodes))
	for k := range g.Nodes {
		keys = append(keys, k)
	}
	return keys
}

func TestDetectFeedbackEdgesFindsNoneInALinearChain(t *testing.T) {
	g := graph.New()
	a := g.EnsureNode([]string{"A"}, 1)
	b := g.EnsureNode([]string{"B"}, 2)
	c := g.EnsureNode([]string{"C"}, 3)
	g.EnsureEdge(a.Key, b.Key, 1).Count++
	g.EnsureEdge(b.Key, c.Key, 2).Count++

	_, feedback := graph.DetectFeedbackEdges(g)
	if len(feedback) != 0 {
		t.Fatalf("expected no feedback edges in a linear chain, got %v", feedback)
	}
}

func TestDetectFeedbackEdgesTerminatesAndFlagsExactlyOneEdgeInATwoCycle(t *testing.T) {
	g := graph.New()
	a := g.EnsureNode([]string{"A"}, 1)
	b := g.EnsureNode([]string{"B"}, 2)
	g.EnsureEdge(a.Key, b.Key, 1).Count++
	g.EnsureEdge(b.Key, a.Key, 2).Count++

	_, feedback := graph.DetectFeedbackEdges(g)
	if len(feedback) != 1 {
		t.Fatalf("expected exactly one feedback edge in a 2-cycle, got %d: %v", len(feedback), feedback)
	}
}

func TestDetectFeedbackEdgesIsDeterministic(t *testing.T) {
	build := func() *graph.Graph {
		g := graph.New()
		a := g.EnsureNode([]string{"A"}, 1)
		b := g.EnsureNode([]string{"B"}, 2)
		c := g.EnsureNode([]string{"C"}, 3)
		g.EnsureEdge(a.Key, b.Key, 1).Count++
		g.EnsureEdge(b.Key, c.Key, 2).Count++
		g.EnsureEdge(c.Key, a.Key, 3).Count++
		return g
	}

	g1, g2 := build(), build()
	f1, fb1 := graph.DetectFeedbackEdges(g1)
	f2, fb2 := graph.DetectFeedbackEdges(g2)
	if !reflect.DeepEqual(f1, f2) {
		t.Fatalf("forward adjacency not deterministic: %v vs %v", f1, f2)
	}
	if !reflect.DeepEqual(fb1, fb2) {
		t.Fatalf("feedback set not deterministic: %v vs %v", fb1, fb2)
	}
}
