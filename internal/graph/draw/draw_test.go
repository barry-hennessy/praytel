package draw

import (
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/graph"
)

func TestCellGlyphTellsACrossingFromAJunction(t *testing.T) {
	all := dirNorth | dirSouth | dirEast | dirWest

	// Two lines passing straight through each other: dimmed, so neither
	// reads as turning into the other.
	if r, style := (gridCell{lineDirs: all, style: Custom}).glyph(); r != '┼' || style != Crossing {
		t.Fatalf("crossing drew %q in style %d, want a dim cross", r, style)
	}
	// The same four directions where some line turns: a real junction, in
	// the edge's own style.
	if r, style := (gridCell{lineDirs: all, style: Custom, bend: true}).glyph(); r != '┼' || style != Custom {
		t.Fatalf("junction drew %q in style %d, want a cross in the edge's style", r, style)
	}
}

func TestLineKeepsItsKindOnlyWhereEveryLineThroughItShares(t *testing.T) {
	for _, tc := range []struct {
		line Line
		want rune
	}{{Dashed, '╌'}, {Dotted, '┈'}, {Double, '═'}} {
		c := newCanvas(8, 3)
		c.trace([]cellPos{{1, 0}, {1, 7}}, Custom, tc.line)
		c.trace([]cellPos{{1, 4}, {1, 7}}, Custom+1, Solid)

		if r, _ := c.cells[1][2].glyph(); r != tc.want {
			t.Fatalf("a line of kind %d on its own drew %q, want %q", tc.line, r, tc.want)
		}
		if r, _ := c.cells[1][5].glyph(); r != '─' {
			t.Fatalf("a stretch of kind %d shared with a solid line drew %q, want it solid", tc.line, r)
		}
	}
}

func TestDoubleLineBendsDouble(t *testing.T) {
	c := newCanvas(6, 5)
	c.trace([]cellPos{{1, 0}, {1, 3}, {3, 3}, {3, 5}}, Custom, Double)
	got := strings.Join((&Canvas{c}).Lines(nil), "\n")
	for _, r := range "═╗║╚" {
		if !strings.ContainsRune(got, r) {
			t.Fatalf("double line with bends has no %q:\n%s", r, got)
		}
	}
}

func TestCountGoesBesideAVerticalWhenTheLineHasNoLevelStretchOfItsOwn(t *testing.T) {
	c := newCanvas(12, 7)
	line := []cellPos{{1, 0}, {1, 3}, {5, 3}, {5, 9}}
	c.trace(line, Custom, Solid)
	// A second line along both of the first one's level stretches, so
	// neither is the first line's alone.
	c.trace([]cellPos{{1, 0}, {1, 9}}, Custom, Solid)
	c.trace([]cellPos{{5, 0}, {5, 9}}, Custom, Solid)

	if c.labelLine(line, "×3", true) {
		t.Fatalf("label was placed on a stretch another line shares")
	}
	c.labelBeside(line, "×3")

	if got := string([]rune{c.cells[3][4].r, c.cells[3][5].r}); got != "×3" {
		t.Fatalf("cells right of the vertical's middle hold %q, want the count", got)
	}
}

func TestLooksChooseBordersAndArrowheads(t *testing.T) {
	g := graph.New()
	a := g.EnsureNode([]string{"a"}, 0)
	b := g.EnsureNode([]string{"b"}, 1)
	g.EnsureEdge(a.Key, b.Key, 0)

	got := strings.Join(Draw(g, Looks{
		Node: func(n *graph.Node) NodeLook {
			if n.Key == a.Key {
				return NodeLook{Border: DoubleBorder, Label: "+ a"}
			}
			return NodeLook{Border: DottedBorder, Label: "- b"}
		},
		Edge: func(*graph.Edge, bool) EdgeLook { return EdgeLook{Line: Dotted, Head: '×'} },
	}).Lines(nil), "\n")

	for _, want := range []string{"╔", "║ + a", "┌", "┊ - b", "┈×"} {
		if !strings.Contains(got, want) {
			t.Fatalf("no %q in\n%s", want, got)
		}
	}
}
