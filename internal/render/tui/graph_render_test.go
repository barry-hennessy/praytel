package tui

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
	"github.com/charmbracelet/x/ansi"
)

var updateGolden = flag.Bool("update", false, "rewrite the graph snapshots in testdata/graph")

// graphOf builds a graph from "from>to" call specs, each optionally
// suffixed "*N" for a call count, in the order given. Names are container
// paths, so "api.auth" is the auth section of the api component, and may
// name the function making or receiving the call after a colon:
// "api.auth:login".
func graphOf(specs ...string) *graph.Graph {
	g := graph.New()
	for i, spec := range specs {
		count := 1
		if call, n, ok := strings.Cut(spec, "*"); ok {
			spec = call
			fmt.Sscan(n, &count)
		}
		node := func(name string) *graph.Node {
			path, fn, _ := strings.Cut(name, ":")
			n := g.EnsureNode(strings.Split(path, "."), i)
			n.SpanCount += count
			if fn != "" {
				n.Functions[fn] += count
			}
			return n
		}
		from, to, _ := strings.Cut(spec, ">")
		a, b := node(from), node(to)
		g.EnsureEdge(a.Key, b.Key, i).Count += count
	}
	return g
}

// renderPlain renders g with styling stripped and trailing blanks trimmed.
func renderPlain(g *graph.Graph) string {
	lines := layoutGraph(g, nil).lines()
	for i, line := range lines {
		lines[i] = strings.TrimRight(ansi.Strip(line), " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// The snapshots in testdata/graph are the graph view's whole point -- what
// a graph of a given shape looks like -- so they are compared verbatim.
// After a deliberate change to layout or glyphs, regenerate them with
// `go test ./internal/render/tui -run TestGraphSnapshots -update` and read
// the diff as a picture.
func TestGraphSnapshots(t *testing.T) {
	cases := map[string][]string{
		// Everything on one row, every edge a straight line.
		"chain": {"api>auth", "auth>db"},
		// One trunk out of the source, branching to each target; the
		// source level with the middle one.
		"fanout": {"api>auth", "api>orders", "api>search*3"},
		// The mirror image: one trunk into the target.
		"fanin": {"web>db", "worker>db", "cron>db"},
		// An edge skipping a layer passes under the boxes it skips.
		"diamond": {"api>auth", "api>orders", "auth>db", "orders>db", "api>db*4"},
		// Calls that close a cycle run back dashed, out of the way of the
		// forward path; the two-node cycle shares one line, an arrowhead
		// at each end.
		"cycle": {"api>orders", "orders>billing", "billing>api", "orders>api"},
		// Boxes list their functions, busiest first, and grow to fit;
		// edges still meet each box at its heading.
		"functions": {
			"web:render>api.orders:list*3", "web:submit>api.orders:create", "api.orders:create>db:insert",
			"api.orders:list>db:select*3", "api.orders:create>mail:send",
		},
		"service-map": {
			"web>api.auth*2", "web>api.orders*5", "api.auth>db.users", "api.orders>db.orders*5",
			"api.orders>queue*3", "queue>worker*3", "worker>db.orders*3", "worker>mail", "api.orders>api.auth*2",
			"worker>api.orders",
		},
	}
	for name, specs := range cases {
		t.Run(name, func(t *testing.T) {
			got := renderPlain(graphOf(specs...))
			path := filepath.Join("testdata", "graph", name+".txt")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("graph differs from %s\n--- got\n%s--- want\n%s", path, got, want)
			}
		})
	}
}

func TestGraphCellGlyphTellsACrossingFromAJunction(t *testing.T) {
	all := dirNorth | dirSouth | dirEast | dirWest

	// Two lines passing straight through each other: dimmed, so neither
	// reads as turning into the other.
	if r, style := (gridCell{lineDirs: all, style: styleEdge}).glyph(); r != '┼' || style != styleCrossing {
		t.Fatalf("crossing drew %q in style %d, want a dim cross", r, style)
	}
	// The same four directions where some line turns: a real junction, in
	// the edge's own style.
	if r, style := (gridCell{lineDirs: all, style: styleEdge, bend: true}).glyph(); r != '┼' || style != styleEdge {
		t.Fatalf("junction drew %q in style %d, want a cross in the edge's style", r, style)
	}
}

func TestGraphBoxListsAtMostSixFunctionsThenCountsTheRest(t *testing.T) {
	g := graph.New()
	n := g.EnsureNode([]string{"api"}, 0)
	for i, fn := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		n.Functions[fn] = 10 - i
	}

	got := renderPlain(g)
	for _, want := range []string{"│ a ×10", "│ f ×5", "│ +2 more"} {
		if !strings.Contains(got, want) {
			t.Fatalf("box missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "│ g ") {
		t.Fatalf("box lists a seventh function:\n%s", got)
	}
}

func TestGraphBoxBorderTakesTheTreeViewsColorForItsDepth(t *testing.T) {
	g := graphOf("api>db")
	deep := g.Nodes[graph.ContainerKey([]string{"db"})]
	deep.ObserveDepth(2)

	c := layoutGraph(g, nil).c
	var corners []cellStyle // each box's top-left corner, left to right
	for col := range c.w {
		for _, row := range c.cells {
			if cell := row[col]; cell.r == '╭' && cell.lineDirs == 0 {
				corners = append(corners, cell.style)
			}
		}
	}
	if want := []cellStyle{depthStyle(0), depthStyle(2)}; !slices.Equal(corners, want) {
		t.Fatalf("box corner styles %v, want %v (api at depth 0, db at depth 2)", corners, want)
	}
	if depthStyle(0) == depthStyle(2) {
		t.Fatalf("depths 0 and 2 share a style")
	}
}

func TestGraphUnrelatedEdgesNeverShareAVertical(t *testing.T) {
	// a>y and b>x have to cross; each must keep its own track rather than
	// merging into a junction that would read as a>x and b>y as well.
	got := renderPlain(graphOf("a>x", "b>y", "a>y", "b>x"))
	if strings.ContainsAny(got, "├┤") {
		t.Fatalf("unrelated edges merged into a junction:\n%s", got)
	}
}

func TestGraphRendersEveryNodeOfArbitraryGraphs(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for trial := range 300 {
		n := 2 + rng.Intn(20)
		var specs []string
		for range 1 + rng.Intn(2*n) {
			a, b := rng.Intn(n), rng.Intn(n)
			if a != b {
				specs = append(specs, fmt.Sprintf("c%d.s%d>c%d.s%d", a%4, a, b%4, b))
			}
		}
		g := graphOf(specs...)
		got := renderPlain(g)
		for _, node := range g.Nodes {
			if label := "│ " + strings.Join(node.Path, ".") + " "; !strings.Contains(got, label) {
				t.Fatalf("trial %d %v: no box labelled %q in\n%s", trial, specs, label, got)
			}
		}
	}
}

func BenchmarkGraphLayout(b *testing.B) {
	rng := rand.New(rand.NewSource(5))
	var specs []string
	for len(specs) < 80 {
		x, y := rng.Intn(50), rng.Intn(50)
		if x != y {
			specs = append(specs, fmt.Sprintf("c%d.s%d>c%d.s%d", x%6, x, y%6, y))
		}
	}
	g := graphOf(specs...)
	for b.Loop() {
		layoutGraph(g, nil).lines()
	}
}
