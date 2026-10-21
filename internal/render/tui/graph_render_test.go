package tui

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

var updateGolden = flag.Bool("update", false, "rewrite the graph snapshots in testdata/graph")

// graphOf builds a graph from "from>to" call specs, each optionally
// suffixed "*N" for a call count, in the order given. Names are component
// paths, so "api.auth" is the auth component nested in api.
func graphOf(specs ...string) *graph.Graph {
	g := graph.New()
	for i, spec := range specs {
		count := 1
		if call, n, ok := strings.Cut(spec, "*"); ok {
			spec = call
			fmt.Sscan(n, &count)
		}
		node := func(name string) *graph.Node {
			// Path elements below the first carry their own separator.
			path := strings.SplitAfter(name, ".")
			for j := len(path) - 1; j > 0; j-- {
				path[j-1] = strings.TrimSuffix(path[j-1], ".")
				path[j] = "." + path[j]
			}
			n := g.EnsureNode(path, i)
			n.SpanCount += count
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
	if r, style := (gridCell{lineDirs: all, style: stylePalette}).glyph(); r != '┼' || style != styleCrossing {
		t.Fatalf("crossing drew %q in style %d, want a dim cross", r, style)
	}
	// The same four directions where some line turns: a real junction, in
	// the edge's own style.
	if r, style := (gridCell{lineDirs: all, style: stylePalette, bend: true}).glyph(); r != '┼' || style != stylePalette {
		t.Fatalf("junction drew %q in style %d, want a cross in the edge's style", r, style)
	}
}

func TestGraphLineStaysDashedOnlyWhereEveryLineThroughItClosesACycle(t *testing.T) {
	c := newCanvas(8, 3)
	c.trace([]cellPos{{1, 0}, {1, 7}}, stylePalette, true)
	c.trace([]cellPos{{1, 4}, {1, 7}}, stylePalette+1, false)

	if r, _ := c.cells[1][2].glyph(); r != '╌' {
		t.Fatalf("a cycle-closing line on its own drew %q, want it dashed", r)
	}
	if r, _ := c.cells[1][5].glyph(); r != '─' {
		t.Fatalf("a stretch shared with a forward call drew %q, want it solid", r)
	}
}

func TestGraphEdgesIntoOneNodeEachEndInTheirOwnArrowhead(t *testing.T) {
	got := renderPlain(graphOf("web>db*2", "worker>db*3", "cron>db*4"))

	if n := strings.Count(got, "▶"); n != 3 {
		t.Fatalf("%d arrowheads into db, want one per edge:\n%s", n, got)
	}
	for _, count := range []string{"×2─▶", "×3─▶", "×4─▶"} {
		if !strings.Contains(got, count) {
			t.Fatalf("no arrowhead carrying %q:\n%s", count, got)
		}
	}
}

func TestGraphCountGoesBesideAVerticalWhenTheLineHasNoLevelStretchOfItsOwn(t *testing.T) {
	c := newCanvas(12, 7)
	line := []cellPos{{1, 0}, {1, 3}, {5, 3}, {5, 9}}
	c.trace(line, stylePalette, false)
	// A second line along both of the first one's level stretches, so
	// neither is the first line's alone.
	c.trace([]cellPos{{1, 0}, {1, 9}}, stylePalette, false)
	c.trace([]cellPos{{5, 0}, {5, 9}}, stylePalette, false)

	if c.labelLine(line, "×3", true) {
		t.Fatalf("label was placed on a stretch another line shares")
	}
	c.labelBeside(line, "×3")

	if got := string([]rune{c.cells[3][4].r, c.cells[3][5].r}); got != "×3" {
		t.Fatalf("cells right of the vertical's middle hold %q, want the count", got)
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
			if label := "│ " + node.Label() + " "; !strings.Contains(got, label) {
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

// TestCanvasRowsMatchLipglossRendering checks the canvas's shortcut of
// writing each style's escape codes itself against rendering every run of
// cells through lipgloss.
func TestCanvasRowsMatchLipglossRendering(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	g := graphOf("api.request>db.query*3", "api.request>cache.get", "cache.get>db.query", "db.query>api.request")
	for key, n := range g.Nodes {
		n.Color = len(key)%len(componentPalette) + 1
	}
	c := layoutGraph(g, map[string]graphNodeHighlight{}).c
	got := c.lines()
	for i, row := range c.cells {
		var b strings.Builder
		var seg []rune
		segStyle := styleDefault
		flush := func() {
			if len(seg) > 0 {
				b.WriteString(cellStyles[segStyle].Render(string(seg)))
				seg = seg[:0]
			}
		}
		for _, cell := range row {
			r, style := cell.glyph()
			if style != segStyle {
				flush()
				segStyle = style
			}
			seg = append(seg, r)
		}
		flush()
		if got[i] != b.String() {
			t.Fatalf("row %d:\n got %q\nwant %q", i, got[i], b.String())
		}
	}

	// A window onto the canvas shows what cutting its whole rows would.
	for _, col := range []int{0, 1, 7, c.w - 5, c.w} {
		for _, width := range []int{1, 10, c.w} {
			window := c.window(2, c.h, col, width)
			for i, line := range window {
				want := ansi.Strip(ansi.Cut(got[2+i], col, col+width))
				if have := ansi.Strip(line); have != want {
					t.Fatalf("window at column %d, %d wide, row %d:\n got %q\nwant %q", col, width, 2+i, have, want)
				}
			}
		}
	}
}
