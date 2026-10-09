package layout_test

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph/layout"
)

var opts = layout.Options{NodeHeight: 4, RowGap: 1}

// build makes a graph from "from>to" edge specs and bare node names, in the
// order given.
func build(specs ...string) *graph.Graph {
	g := graph.New()
	for i, spec := range specs {
		from, to, isEdge := strings.Cut(spec, ">")
		a := g.EnsureNode([]string{from}, i)
		if !isEdge {
			continue
		}
		b := g.EnsureNode([]string{to}, i)
		g.EnsureEdge(a.Key, b.Key, i).Count++
	}
	return g
}

func key(name string) string { return graph.ContainerKey([]string{name}) }

func edge(from, to string) [2]string { return [2]string{key(from), key(to)} }

func TestPlaceKeepsALinearChainOnOneRow(t *testing.T) {
	p := layout.Place(build("a>b", "b>c", "c>d"), opts)

	for i, name := range []string{"a", "b", "c", "d"} {
		n := p.Nodes[key(name)]
		if n.Layer != i || n.Row != 0 {
			t.Fatalf("node %s at layer %d row %d, want layer %d row 0", name, n.Layer, n.Row, i)
		}
	}
	if p.Layers != 4 || p.Rows != opts.NodeHeight {
		t.Fatalf("extent %dx%d, want 4x%d", p.Layers, p.Rows, opts.NodeHeight)
	}
}

func TestPlaceCentersADiamondsEndsBetweenItsMiddleNodes(t *testing.T) {
	p := layout.Place(build("a>b", "a>c", "b>d", "c>d"), opts)

	a, b, c, d := p.Nodes[key("a")], p.Nodes[key("b")], p.Nodes[key("c")], p.Nodes[key("d")]
	if b.Layer != 1 || c.Layer != 1 || d.Layer != 2 {
		t.Fatalf("layers b=%d c=%d d=%d, want 1 1 2", b.Layer, c.Layer, d.Layer)
	}
	lo, hi := min(b.Row, c.Row), max(b.Row, c.Row)
	for name, n := range map[string]layout.Node{"a": a, "d": d} {
		if n.Row < lo || n.Row > hi {
			t.Fatalf("node %s at row %d, want within the middle nodes' rows %d..%d", name, n.Row, lo, hi)
		}
	}
	if a.Row != d.Row {
		t.Fatalf("diamond ends at rows %d and %d, want them level", a.Row, d.Row)
	}
}

func TestPlaceRoutesALongEdgeThroughEveryLayerItSkips(t *testing.T) {
	p := layout.Place(build("a>b", "b>c", "c>d", "a>d"), opts)

	way := p.Edges[edge("a", "d")].Waypoints
	if len(way) != 2 || way[0].Layer != 1 || way[1].Layer != 2 {
		t.Fatalf("waypoints %+v, want one in layer 1 then one in layer 2", way)
	}
	for _, name := range []string{"b", "c"} {
		n := p.Nodes[key(name)]
		for _, w := range way {
			if w.Layer == n.Layer && w.Row >= n.Row && w.Row < n.Row+opts.NodeHeight {
				t.Fatalf("waypoint %+v runs through node %s's box at row %d", w, name, n.Row)
			}
		}
	}
}

func TestPlaceMarksOneFeedbackEdgeInATwoCycleAndRunsItBackwards(t *testing.T) {
	p := layout.Place(build("a>b", "b>a"), opts)

	if p.Edges[edge("a", "b")].Feedback || !p.Edges[edge("b", "a")].Feedback {
		t.Fatalf("feedback flags a>b=%v b>a=%v, want false true",
			p.Edges[edge("a", "b")].Feedback, p.Edges[edge("b", "a")].Feedback)
	}
	if a, b := p.Nodes[key("a")], p.Nodes[key("b")]; a.Layer != 0 || b.Layer != 1 {
		t.Fatalf("layers a=%d b=%d, want 0 1", a.Layer, b.Layer)
	}
}

func TestPlaceOrdersAFeedbackEdgesWaypointsFromItsSource(t *testing.T) {
	p := layout.Place(build("a>b", "b>c", "c>d", "d>a"), opts)

	e := p.Edges[edge("d", "a")]
	if !e.Feedback {
		t.Fatalf("d>a not marked feedback")
	}
	if len(e.Waypoints) != 2 || e.Waypoints[0].Layer != 2 || e.Waypoints[1].Layer != 1 {
		t.Fatalf("waypoints %+v, want layer 2 then layer 1", e.Waypoints)
	}
}

func TestPlaceStacksNodesWithNoEdgesBelowTheRest(t *testing.T) {
	p := layout.Place(build("a>b", "lonely", "alone"), opts)

	lonely, alone := p.Nodes[key("lonely")], p.Nodes[key("alone")]
	if lonely.Layer != 0 || alone.Layer != 0 {
		t.Fatalf("isolated nodes in layers %d and %d, want 0", lonely.Layer, alone.Layer)
	}
	if want := opts.NodeHeight + opts.RowGap; lonely.Row != want || alone.Row != 2*want {
		t.Fatalf("isolated rows %d and %d, want %d and %d", lonely.Row, alone.Row, want, 2*want)
	}
	if p.Rows != alone.Row+opts.NodeHeight {
		t.Fatalf("rows %d, want %d", p.Rows, alone.Row+opts.NodeHeight)
	}
}

func TestPlaceOfOnlyIsolatedNodesStartsAtTheTop(t *testing.T) {
	p := layout.Place(build("one", "two"), opts)

	if p.Nodes[key("one")].Row != 0 || p.Layers != 1 {
		t.Fatalf("placement %+v, want first node at row 0 in a single layer", p)
	}
}

func TestPlaceMakesRoomForATallerBoxAndKeepsEdgesAtItsHeading(t *testing.T) {
	g := build("a>b", "a>c", "b>d", "c>d")
	tall := layout.Options{NodeHeight: 4, RowGap: 1, Heights: map[string]int{key("b"): 9}}
	p := layout.Place(g, tall)

	b, c := p.Nodes[key("b")], p.Nodes[key("c")]
	if upper, lower, height := b, c, 9; b.Row < c.Row {
		if lower.Row < upper.Row+height+tall.RowGap {
			t.Fatalf("c at row %d overlaps the 9-row b at row %d", c.Row, b.Row)
		}
	} else if b.Row < c.Row+4+tall.RowGap {
		t.Fatalf("the 9-row b at row %d overlaps c at row %d", b.Row, c.Row)
	}
	if tall.Port(b) != b.Row+2 {
		t.Fatalf("b's port is row %d, want two below its top row %d", tall.Port(b), b.Row)
	}
	if p.Rows < b.Row+9 {
		t.Fatalf("grid is %d rows, too short for b ending at row %d", p.Rows, b.Row+9)
	}
}

func TestPlaceIsDeterministic(t *testing.T) {
	specs := randomSpecs(rand.New(rand.NewSource(7)), 14, 24)
	want := layout.Place(build(specs...), opts)
	for i := 0; i < 20; i++ {
		if got := layout.Place(build(specs...), opts); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d differs:\n got %+v\nwant %+v", i, got, want)
		}
	}
}

// randomSpecs returns edges between n nodes, cycles included.
func randomSpecs(rng *rand.Rand, n, edges int) []string {
	var specs []string
	for len(specs) < edges {
		a, b := rng.Intn(n), rng.Intn(n)
		if a != b {
			specs = append(specs, fmt.Sprintf("n%d>n%d", a, b))
		}
	}
	return specs
}

func TestPlaceNeverOverlapsBoxesOrRunsEdgesThroughThem(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 300; trial++ {
		specs := randomSpecs(rng, 2+rng.Intn(12), 1+rng.Intn(24))
		g := build(specs...)
		p := layout.Place(g, opts)

		if len(p.Nodes) != len(g.Nodes) || len(p.Edges) != len(g.Edges) {
			t.Fatalf("trial %d %v: placed %d nodes and %d edges of %d and %d",
				trial, specs, len(p.Nodes), len(p.Edges), len(g.Nodes), len(g.Edges))
		}

		// taken[layer][row] names whatever occupies that cell of the layer.
		taken := map[int]map[int]string{}
		claim := func(layer, row int, who string) {
			if taken[layer] == nil {
				taken[layer] = map[int]string{}
			}
			if other, clash := taken[layer][row]; clash {
				t.Fatalf("trial %d %v: %s and %s both occupy layer %d row %d", trial, specs, who, other, layer, row)
			}
			if row < 0 || row >= p.Rows || layer < 0 || layer >= p.Layers {
				t.Fatalf("trial %d %v: %s at layer %d row %d is outside the %dx%d grid", trial, specs, who, layer, row, p.Layers, p.Rows)
			}
			taken[layer][row] = who
		}
		for k, n := range p.Nodes {
			for r := n.Row; r < n.Row+opts.NodeHeight; r++ {
				claim(n.Layer, r, k)
			}
		}

		// Both directions of a two-node cycle share one route, so claim
		// each distinct route once.
		routes := map[[2]string]bool{}
		for ek, e := range p.Edges {
			from, to := p.Nodes[ek[0]], p.Nodes[ek[1]]
			step := 1
			if e.Feedback {
				step = -1
			}
			if (to.Layer-from.Layer)*step < 1 {
				t.Fatalf("trial %d %v: edge %v spans layers %d to %d (feedback=%v)", trial, specs, ek, from.Layer, to.Layer, e.Feedback)
			}
			if len(e.Waypoints) != (to.Layer-from.Layer)*step-1 {
				t.Fatalf("trial %d %v: edge %v has %d waypoints across layers %d to %d", trial, specs, ek, len(e.Waypoints), from.Layer, to.Layer)
			}
			route := ek
			if e.Feedback {
				route = [2]string{ek[1], ek[0]}
			}
			for i, w := range e.Waypoints {
				if w.Layer != from.Layer+step*(i+1) {
					t.Fatalf("trial %d %v: edge %v waypoint %d in layer %d", trial, specs, ek, i, w.Layer)
				}
				if !routes[route] {
					claim(w.Layer, w.Row, fmt.Sprint("edge ", ek))
				}
			}
			routes[route] = true
		}
	}
}
