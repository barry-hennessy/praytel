package graph_test

import (
	"reflect"
	"testing"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
)

func TestHierarchyPathNestsNamesUnderSharedAndObservedPrefixes(t *testing.T) {
	type span struct{ component, name string }
	cases := []struct {
		name  string
		spans []span
		want  [][]string // parallel to spans
	}{
		{
			name: "names diverging after a shared prefix nest under it",
			spans: []span{
				{"api", "Handlers.Orders.validate"},
				{"api", "Handlers.Orders.create"},
				{"api", "Handlers.Payments.charge"},
			},
			want: [][]string{
				{"api", ".Handlers", ".Orders", ".validate"},
				{"api", ".Handlers", ".Orders", ".create"},
				{"api", ".Handlers", ".Payments.charge"},
			},
		},
		{
			name: "no separator is assumed",
			spans: []span{
				{"api", "orders/validate::check"},
				{"api", "orders/validate::reject"},
				{"api", "orders/ship"},
			},
			want: [][]string{
				{"api", ".orders", "/validate", "::check"},
				{"api", ".orders", "/validate", "::reject"},
				{"api", ".orders", "/ship"},
			},
		},
		{
			name: "an observed name that others extend is their parent",
			spans: []span{
				{"api", "orders"},
				{"api", "orders/validate"},
				{"api", "orders/validate/check"},
			},
			want: [][]string{
				{"api", ".orders"},
				{"api", ".orders", "/validate"},
				{"api", ".orders", "/validate", "/check"},
			},
		},
		{
			name: "prefixes only end at word boundaries",
			spans: []span{
				{"api", "get"},
				{"api", "getAll"},
				{"api", "validate"},
				{"api", "view"},
				{"api", "load_user"},
				{"api", "load_order"},
			},
			want: [][]string{
				{"api", ".get"},
				{"api", ".getAll"},
				{"api", ".validate"},
				{"api", ".view"},
				{"api", ".load_user"},
				{"api", ".load_order"},
			},
		},
		{
			name:  "a name alone is one component under its Component",
			spans: []span{{"Foo", "Bar.Qux.Baz"}},
			want:  [][]string{{"Foo", ".Bar.Qux.Baz"}},
		},
		{
			name:  "empty name",
			spans: []span{{"Foo", ""}},
			want:  [][]string{{"Foo"}},
		},
		{
			name:  "name equals component",
			spans: []span{{"Foo", "Foo"}},
			want:  [][]string{{"Foo"}},
		},
		{
			name: "name redundantly prefixed with component",
			spans: []span{
				{"Foo", "Foo.Bar.Baz"},
				{"Foo", "Bar.Qux"},
			},
			want: [][]string{
				{"Foo", ".Bar", ".Baz"},
				{"Foo", ".Bar", ".Qux"},
			},
		},
		{
			name: "empty component",
			spans: []span{
				{"", "Bar.Baz"},
				{"", "Bar.Qux"},
			},
			want: [][]string{
				{"Bar", ".Baz"},
				{"Bar", ".Qux"},
			},
		},
		{
			name:  "empty component and nothing shared",
			spans: []span{{"", "span/12345678"}},
			want:  [][]string{{"span/12345678"}},
		},
		{
			name:  "both empty",
			spans: []span{{"", ""}},
			want:  [][]string{nil},
		},
		{
			name: "components do not share prefixes",
			spans: []span{
				{"api", "Orders.create"},
				{"db", "Orders.insert"},
			},
			want: [][]string{
				{"api", ".Orders.create"},
				{"db", ".Orders.insert"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := graph.NewHierarchy()
			for _, s := range c.spans {
				h.Add(s.component, s.name)
			}
			for i, s := range c.spans {
				if got := h.Path(s.component, s.name); !reflect.DeepEqual(got, c.want[i]) {
					t.Fatalf("Path(%q, %q) = %q, want %q", s.component, s.name, got, c.want[i])
				}
			}
		})
	}
}

func TestNodeLabelIsTheComponentsFullName(t *testing.T) {
	g := graph.Build([]graph.SpanFact{
		{Component: "api", Name: "orders/validate::check", Order: 1},
		{Component: "api", Name: "orders/validate::reject", Order: 2},
	})

	node, ok := g.Nodes[graph.ContainerKey([]string{"api", ".orders/validate", "::check"})]
	if !ok {
		t.Fatalf("missing node; nodes = %q", nodeKeys(g))
	}
	if got, want := node.Label(), "api.orders/validate::check"; got != want {
		t.Fatalf("Label() = %q, want %q", got, want)
	}
}

func TestBuildGivesEveryNamedSpanItsOwnNode(t *testing.T) {
	g := graph.Build([]graph.SpanFact{
		{Component: "api", Name: "Handlers.Orders.validate", Order: 1},
		{Component: "api", Name: "Handlers.Orders.create", Order: 2, HasParent: true, ParentComponent: "api", ParentName: "Handlers.Orders.validate", ParentOrder: 1},
		{Component: "api", Name: "Handlers.Orders.create", Order: 3},
	})

	validate := graph.ContainerKey([]string{"api", ".Handlers.Orders", ".validate"})
	create := graph.ContainerKey([]string{"api", ".Handlers.Orders", ".create"})
	if len(g.Nodes) != 2 {
		t.Fatalf("expected one node per distinct span name and none for the shared prefix, got %q", nodeKeys(g))
	}
	if n := g.Nodes[validate]; n == nil || n.SpanCount != 1 {
		t.Fatalf("validate node = %+v, want SpanCount 1", n)
	}
	if n := g.Nodes[create]; n == nil || n.SpanCount != 2 {
		t.Fatalf("create node = %+v, want SpanCount 2", n)
	}
	if e, ok := g.Edges[[2]string{validate, create}]; !ok || e.Count != 1 {
		t.Fatalf("expected one edge validate->create, got %+v (ok=%v)", e, ok)
	}
}

func TestBuildFoldsSameNameCallsIntoSpanCountNotSelfEdges(t *testing.T) {
	g := graph.Build([]graph.SpanFact{
		{Component: "api", Name: "walk", Order: 1},
		{Component: "api", Name: "walk", Order: 2, HasParent: true, ParentComponent: "api", ParentName: "walk", ParentOrder: 1},
	})

	if len(g.Edges) != 0 {
		t.Fatalf("expected no self-edge for a span calling its own name, got %v", g.Edges)
	}
	if n := g.Nodes[graph.ContainerKey([]string{"api", ".walk"})]; n == nil || n.SpanCount != 2 {
		t.Fatalf("walk node = %+v, want SpanCount 2", n)
	}
}

func TestBuildProducesCrossAndIntraComponentEdgesFromTheSameRule(t *testing.T) {
	g := graph.Build([]graph.SpanFact{
		{Component: "api", Name: "Handlers.Orders.validate", Order: 1},
		{Component: "api", Name: "Handlers.Payments.charge", Order: 2, HasParent: true, ParentComponent: "api", ParentName: "Handlers.Orders.validate", ParentOrder: 1},
		{Component: "db", Name: "query", Order: 3, HasParent: true, ParentComponent: "api", ParentName: "Handlers.Orders.validate", ParentOrder: 1},
	})

	validate := graph.ContainerKey([]string{"api", ".Handlers", ".Orders.validate"})
	charge := graph.ContainerKey([]string{"api", ".Handlers", ".Payments.charge"})
	query := graph.ContainerKey([]string{"db", ".query"})

	for _, key := range []string{validate, charge, query} {
		if _, ok := g.Nodes[key]; !ok {
			t.Fatalf("missing node %q; nodes = %q", key, nodeKeys(g))
		}
	}

	intra, ok := g.Edges[[2]string{validate, charge}]
	if !ok || intra.Count != 1 {
		t.Fatalf("expected one intra-component edge validate->charge, got %+v (ok=%v)", intra, ok)
	}
	cross, ok := g.Edges[[2]string{validate, query}]
	if !ok || cross.Count != 1 {
		t.Fatalf("expected one cross-component edge validate->query, got %+v (ok=%v)", cross, ok)
	}
}

func TestBuildAggregatesRepeatedEdgesIntoOneCount(t *testing.T) {
	g := graph.Build([]graph.SpanFact{
		{Component: "api", Name: "request", Order: 1},
		{Component: "db", Name: "query", Order: 2, HasParent: true, ParentComponent: "api", ParentName: "request", ParentOrder: 1},
		{Component: "api", Name: "request", Order: 3},
		{Component: "db", Name: "query", Order: 4, HasParent: true, ParentComponent: "api", ParentName: "request", ParentOrder: 3},
	})

	api := graph.ContainerKey([]string{"api", ".request"})
	db := graph.ContainerKey([]string{"db", ".query"})
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

func TestAssignLayersRanksALinearChain(t *testing.T) {
	g := graph.New()
	a := g.EnsureNode([]string{"A"}, 1)
	b := g.EnsureNode([]string{"B"}, 2)
	c := g.EnsureNode([]string{"C"}, 3)
	g.EnsureEdge(a.Key, b.Key, 1).Count++
	g.EnsureEdge(b.Key, c.Key, 2).Count++

	forward, feedback := graph.DetectFeedbackEdges(g)
	if len(feedback) != 0 {
		t.Fatalf("expected no feedback edges in a linear chain, got %v", feedback)
	}
	layer := graph.AssignLayers(g, forward)
	if layer[a.Key] != 0 || layer[b.Key] != 1 || layer[c.Key] != 2 {
		t.Fatalf("layers = %v, want A:0 B:1 C:2", layer)
	}
}

func TestDetectFeedbackEdgesTerminatesAndFlagsExactlyOneEdgeInATwoCycle(t *testing.T) {
	g := graph.New()
	a := g.EnsureNode([]string{"A"}, 1)
	b := g.EnsureNode([]string{"B"}, 2)
	g.EnsureEdge(a.Key, b.Key, 1).Count++
	g.EnsureEdge(b.Key, a.Key, 2).Count++

	forward, feedback := graph.DetectFeedbackEdges(g)
	if len(feedback) != 1 {
		t.Fatalf("expected exactly one feedback edge in a 2-cycle, got %d: %v", len(feedback), feedback)
	}
	layer := graph.AssignLayers(g, forward)
	if _, ok := layer[a.Key]; !ok {
		t.Fatalf("node A has no assigned layer")
	}
	if _, ok := layer[b.Key]; !ok {
		t.Fatalf("node B has no assigned layer")
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
	l1 := graph.AssignLayers(g1, f1)
	l2 := graph.AssignLayers(g2, f2)
	if !reflect.DeepEqual(l1, l2) {
		t.Fatalf("layer assignment not deterministic: %v vs %v", l1, l2)
	}
}
