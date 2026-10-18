package tui

import (
	"fmt"

	"github.com/barry-hennessy/praytel/internal/graph"
	"github.com/barry-hennessy/praytel/internal/graph/draw"
	"github.com/charmbracelet/lipgloss"
)

// buildGraph aggregates the currently filtered/visible spans (per
// filteredForest and fuzzyVisible -- the same layers treeLines applies, so
// the graph can never show something the tree wouldn't) into a graph.Graph:
// one node per distinct nested component, one edge per pair of components
// connected by an observed parent/child span call. The aggregation rule
// itself (how names nest, and that same-component calls never become
// self-edges) lives in the graph package, which knows
// nothing about Model or live filtering; buildGraph's own job is only to
// decide which spans are visible right now and hand their facts over.
func (m Model) buildGraph() *graph.Graph {
	children, _, inScope, _ := m.filteredForest()
	traceMatches := m.matchingTraces()
	logMatches := m.matchingLogs()

	facts := make([]graph.SpanFact, 0, len(inScope))
	for _, sp := range inScope {
		if !m.fuzzyVisible(sp, children, traceMatches, logMatches) {
			continue
		}
		fact := graph.SpanFact{Component: sp.component, Name: sp.name, Order: sp.order, Color: m.colors[spanLabel(sp)]}
		if sp.parentID != "" {
			if parent, ok := inScope[selectionKey(sp.traceID, sp.parentID)]; ok && m.fuzzyVisible(parent, children, traceMatches, logMatches) {
				fact.HasParent = true
				fact.ParentComponent = parent.component
				fact.ParentName = parent.name
				fact.ParentOrder = parent.order
			}
		}
		facts = append(facts, fact)
	}
	return graph.Build(facts)
}

// graphNodeHighlight mirrors spanHighlight at component granularity: which
// runes of the node's own label were consumed by a fuzzy match.
type graphNodeHighlight struct {
	label []bool
}

// matchGraphNode fuzzy-matches the query against the node's label and
// returns which of its runes the match consumed.
func matchGraphNode(query string, n *graph.Node) (bool, graphNodeHighlight) {
	ok, mask := fuzzyMatch(query, n.Label())
	if !ok {
		return false, graphNodeHighlight{}
	}
	return true, graphNodeHighlight{label: mask}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func nodeFooter(n *graph.Node) string {
	return fmt.Sprintf("%d span%s", n.SpanCount, plural(n.SpanCount))
}

// cellStyle is a small closed set of styles used by the graph canvas,
// stored as an integer per cell so runs of equal style can be
// grouped/rendered cheaply (mirroring styleSegments' run-length approach,
// generalized from a boolean highlight mask to a handful of named styles).
// The first few are the drawing's own (see draw.Style).
type cellStyle = draw.Style

const (
	styleDefault    = draw.Default
	styleLabel      = draw.Label
	styleLabelMatch = draw.LabelMatch
	styleFooter     = draw.Footer
	styleCrossing   = draw.Crossing

	// Component styles are in precedence order: where lines of different
	// styles share a cell, the later one wins.
	styleNeutral = draw.Custom
	stylePalette = draw.Custom + 1 // first of len(componentPalette) styles; see componentStyle
)

var cellStyles = func() []lipgloss.Style {
	styles := []lipgloss.Style{
		styleDefault:    lipgloss.NewStyle(),
		styleLabel:      lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true),
		styleLabelMatch: matchHighlightStyle,
		styleFooter:     lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true),
		styleCrossing:   lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		styleNeutral:    lipgloss.NewStyle().Foreground(neutralColor),
	}
	for _, color := range componentPalette {
		styles = append(styles, lipgloss.NewStyle().Foreground(color))
	}
	return styles
}()

// componentStyle is a component's own color (see Model.colors), or neutral
// for one that calls nothing. Its box border and every call leaving it are
// drawn in it, the same color the tree view gives its bar and rule.
func componentStyle(n *graph.Node) cellStyle {
	if n.Color == 0 {
		return styleNeutral
	}
	return stylePalette + cellStyle((n.Color-1)%len(componentPalette))
}

func countLabel(e *graph.Edge) string {
	if e == nil || e.Count < 2 {
		return ""
	}
	return fmt.Sprintf("×%d", e.Count)
}

type graphLayout struct {
	c *draw.Canvas
}

func (l *graphLayout) lines() []string {
	return l.c.Lines(func(s draw.Style, text string) string { return cellStyles[s].Render(text) })
}

// layoutGraph draws g the TUI's way: each component in its own color with
// its span count, each call in its caller's color with its count, and
// calls that close a cycle dashed. The result is drawn at whatever size
// the graph naturally needs; it flows through the same scroll/crop
// viewport the tree view already uses (see Model.View).
func layoutGraph(g *graph.Graph, highlights map[string]graphNodeHighlight) *graphLayout {
	return &graphLayout{c: draw.Draw(g, draw.Looks{
		Node: func(n *graph.Node) draw.NodeLook {
			return draw.NodeLook{Style: componentStyle(n), Label: n.Label(), LabelMask: highlights[n.Key].label, Footer: nodeFooter(n)}
		},
		Edge: func(e *graph.Edge, feedback bool) draw.EdgeLook {
			line := draw.Solid
			if feedback {
				line = draw.Dashed
			}
			return draw.EdgeLook{Style: componentStyle(g.Nodes[e.FromKey]), Line: line, Label: countLabel(e)}
		},
	})}
}

// graphLines renders the current component/section interconnection graph
// as a full ASCII node-link diagram, following exactly the same
// focus/exclude, compiled-query, and live fuzzy search narrowing as
// treeLines (see filteredForest and fuzzyVisible).
func (m Model) graphLines() []string {
	g := m.buildGraph()
	if len(g.Nodes) == 0 {
		if m.query == "" && m.fixed == nil {
			return []string{
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("245")).
					Render("  Waiting for traces and logs…"),
			}
		}
		return []string{
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Render("  No matches"),
		}
	}

	highlights := map[string]graphNodeHighlight{}
	if m.query != "" && m.mode == searchTraces {
		for key, n := range g.Nodes {
			if ok, h := matchGraphNode(m.query, n); ok {
				highlights[key] = h
			}
		}
	}

	return layoutGraph(g, highlights).lines()
}
