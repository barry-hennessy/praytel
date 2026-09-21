package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
	"github.com/charmbracelet/lipgloss"
)

// buildGraph aggregates the currently filtered/visible spans (per
// filteredForest and fuzzyVisible -- the same layers treeLines applies, so
// the graph can never show something the tree wouldn't) into a graph.Graph:
// one node per distinct hierarchy container, one edge per pair of
// containers connected by an observed parent/child span call. The
// aggregation rule itself (same-container calls fold into a function tally
// rather than becoming self-edges) lives in the graph package, which knows
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
		fact := graph.SpanFact{Component: sp.component, Name: sp.name, Order: sp.order}
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

// positioned is a node (real or a routing dummy) placed at a specific
// layer/row in the final layout.
type positioned struct {
	key   string
	layer int
	row   int
	dummy bool
	node  *graph.Node // nil for a dummy waypoint
}

// unitEdge connects two waypoints exactly one layer apart -- the unit of
// both routing and barycenter ordering.
type unitEdge struct {
	from, to string
}

// routedEdge is one original graph.Edge resolved into a chain of waypoints,
// each consecutive pair one layer apart, with dummy waypoints inserted at
// every intermediate layer so long edges don't cut diagonally across
// unrelated columns.
type routedEdge struct {
	edge      *graph.Edge
	waypoints []string
	feedback  bool
}

// routeEdges places every real node at its assigned layer, inserts dummy
// waypoints for edges spanning more than one layer (in either direction --
// a feedback edge can span layers just like a forward one), and returns the
// resulting per-layer node lists, the unit edges used for ordering, and the
// full waypoint chain for every original edge, used for drawing.
func routeEdges(
	g *graph.Graph,
	layer map[string]int,
	feedback map[[2]string]bool,
) (positions map[string]*positioned, layers map[int][]string, unitEdges []unitEdge, routed []routedEdge) {
	positions = map[string]*positioned{}
	layers = map[int][]string{}

	place := func(key string, l int, dummy bool, node *graph.Node) {
		if _, ok := positions[key]; ok {
			return
		}
		positions[key] = &positioned{key: key, layer: l, dummy: dummy, node: node}
		layers[l] = append(layers[l], key)
	}
	for _, key := range graph.OrderedNodeKeys(g) {
		place(key, layer[key], false, g.Nodes[key])
	}

	dummyN := 0
	for _, ek := range graph.SortedEdgeKeys(g) {
		e := g.Edges[ek]
		lf, lt := layer[e.FromKey], layer[e.ToKey]
		isFB := feedback[ek]
		chain := []string{e.FromKey}
		if lf != lt {
			step := 1
			if lt < lf {
				step = -1
			}
			for l := lf + step; l != lt; l += step {
				dummyN++
				dk := fmt.Sprintf("\x00d%d", dummyN)
				place(dk, l, true, nil)
				chain = append(chain, dk)
			}
		}
		chain = append(chain, e.ToKey)
		for i := 0; i+1 < len(chain); i++ {
			if positions[chain[i]].layer == positions[chain[i+1]].layer {
				continue // same-layer hop (only possible for a feedback edge); drawn as a node annotation, not a line
			}
			unitEdges = append(unitEdges, unitEdge{from: chain[i], to: chain[i+1]})
		}
		routed = append(routed, routedEdge{edge: e, waypoints: chain, feedback: isFB})
	}
	return positions, layers, unitEdges, routed
}

func nodeSortKey(p *positioned) string {
	if p.dummy {
		return "\xff" + p.key
	}
	return fmt.Sprintf("%08d:%s", p.node.Order, p.key)
}

// orderLayers assigns a row index within each layer via one top-down
// barycenter pass (order by mean row of predecessors in the previous
// layer) followed by one bottom-up pass (mean row of successors), seeded
// with an order-sorted initial placement and tie-broken by it throughout.
// This is a "good enough for a terminal" heuristic, not full
// crossing-minimization -- overkill at TUI scale.
func orderLayers(layers map[int][]string, positions map[string]*positioned, unitEdges []unitEdge) {
	maxLayer := 0
	for l := range layers {
		if l > maxLayer {
			maxLayer = l
		}
	}

	for l, keys := range layers {
		sorted := append([]string(nil), keys...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return nodeSortKey(positions[sorted[i]]) < nodeSortKey(positions[sorted[j]])
		})
		layers[l] = sorted
		for i, k := range sorted {
			positions[k].row = i
		}
	}

	predecessors := map[string][]string{}
	successors := map[string][]string{}
	for _, ue := range unitEdges {
		a, b := positions[ue.from], positions[ue.to]
		switch {
		case a.layer+1 == b.layer:
			successors[a.key] = append(successors[a.key], b.key)
			predecessors[b.key] = append(predecessors[b.key], a.key)
		case b.layer+1 == a.layer:
			successors[b.key] = append(successors[b.key], a.key)
			predecessors[a.key] = append(predecessors[a.key], b.key)
		}
	}

	reorder := func(l int, neighbors map[string][]string) {
		keys := layers[l]
		type scored struct {
			key   string
			score float64
			has   bool
		}
		sc := make([]scored, len(keys))
		for i, k := range keys {
			ns := neighbors[k]
			if len(ns) == 0 {
				sc[i] = scored{key: k}
				continue
			}
			sum := 0
			for _, n := range ns {
				sum += positions[n].row
			}
			sc[i] = scored{key: k, score: float64(sum) / float64(len(ns)), has: true}
		}
		sort.SliceStable(sc, func(i, j int) bool {
			if sc[i].has != sc[j].has {
				return sc[i].has
			}
			if sc[i].has && sc[i].score != sc[j].score {
				return sc[i].score < sc[j].score
			}
			return nodeSortKey(positions[sc[i].key]) < nodeSortKey(positions[sc[j].key])
		})
		for i, s := range sc {
			layers[l][i] = s.key
			positions[s.key].row = i
		}
	}

	for l := 1; l <= maxLayer; l++ {
		reorder(l, predecessors)
	}
	for l := maxLayer - 1; l >= 0; l-- {
		reorder(l, successors)
	}
}

// graphNodeHighlight mirrors spanHighlight at container granularity: which
// runes of the node's own dotted label were consumed by a fuzzy match.
type graphNodeHighlight struct {
	label []bool
}

// matchGraphNode fuzzy-matches the query against the node's own dotted
// label and every distinct function name folded into it (sorted for
// deterministic results), so a query for a leaf function still highlights
// the section node it collapsed into even though the function name never
// appears in the node's own rendered label. Only the label's portion of the
// match mask is returned -- functions aren't rendered as their own text, so
// there's nothing to highlight them against.
func matchGraphNode(query string, n *graph.Node) (bool, graphNodeHighlight) {
	label := strings.Join(n.Path, ".")
	fnNames := make([]string, 0, len(n.Functions))
	for fn := range n.Functions {
		fnNames = append(fnNames, fn)
	}
	sort.Strings(fnNames)
	segments := append([]string{label}, fnNames...)
	ok, mask := fuzzyMatch(query, strings.Join(segments, " "))
	if !ok {
		return false, graphNodeHighlight{}
	}
	labelLen := utf8.RuneCountInString(label)
	if labelLen > len(mask) {
		labelLen = len(mask)
	}
	return true, graphNodeHighlight{label: mask[:labelLen]}
}

// Layout constants. Sizes are deliberately generous rather than tightly
// packed: the rendered graph flows through the same horizontal/vertical
// scroll-and-crop pipeline the tree view already uses (see Model.View), so
// there's no need to fit everything inside the visible terminal -- panning
// reveals the rest.
const (
	minNodeInnerWidth = 8
	minColumnWidth    = 6
	columnGap         = 6
	rowGap            = 1
	nodeBoxHeight     = 4
	leftMargin        = 1
	topMargin         = 1
)

func truncateLabel(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func nodeFooter(n *graph.Node) string {
	if len(n.Functions) == 0 {
		return fmt.Sprintf("%d span%s", n.SpanCount, plural(n.SpanCount))
	}
	return fmt.Sprintf("%d span%s · %d fn%s", n.SpanCount, plural(n.SpanCount), len(n.Functions), plural(len(n.Functions)))
}

func nodeBoxWidth(n *graph.Node) int {
	label := strings.Join(n.Path, ".")
	footer := nodeFooter(n)
	inner := utf8.RuneCountInString(label)
	if fl := utf8.RuneCountInString(footer); fl > inner {
		inner = fl
	}
	if inner < minNodeInnerWidth {
		inner = minNodeInnerWidth
	}
	return inner + 4 // "│ " + inner + " │"
}

// cellStyle is a small closed set of styles used by the graph canvas,
// stored as an integer per cell so runs of equal style can be
// grouped/rendered cheaply (mirroring styleSegments' run-length approach,
// generalized from a boolean highlight mask to a handful of named styles).
type cellStyle uint8

const (
	styleDefault cellStyle = iota
	styleBorder
	styleLabel
	styleLabelMatch
	styleFooter
	styleEdge
	styleEdgeFeedback
)

var cellStyles = map[cellStyle]lipgloss.Style{
	styleDefault:      lipgloss.NewStyle(),
	styleBorder:       lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	styleLabel:        lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
	styleLabelMatch:   matchHighlightStyle,
	styleFooter:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true),
	styleEdge:         lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
	styleEdgeFeedback: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
}

type gridCell struct {
	r        rune
	style    cellStyle
	lineDirs uint8 // 0 for non-line cells (text, borders, arrows); otherwise the OR of the dirXxx bits currently occupying this cell
}

// Direction bits for the box-drawing line glyphs a cell can connect to its
// neighbors. Multiple edges' lines that cross or merge at the same cell
// accumulate bits here (see canvas.addLine) so the rendered glyph is the
// correct junction (a T or a cross) instead of whichever edge happened to
// draw last blindly overwriting the others.
const (
	dirNorth uint8 = 1 << iota
	dirSouth
	dirEast
	dirWest
)

var lineGlyphs = map[uint8]rune{
	dirNorth | dirSouth:                     '│',
	dirEast | dirWest:                       '─',
	dirNorth | dirEast:                      '└',
	dirNorth | dirWest:                      '┘',
	dirSouth | dirEast:                      '┌',
	dirSouth | dirWest:                      '┐',
	dirNorth | dirSouth | dirEast:           '├',
	dirNorth | dirSouth | dirWest:           '┤',
	dirEast | dirWest | dirSouth:            '┬',
	dirEast | dirWest | dirNorth:            '┴',
	dirNorth | dirSouth | dirEast | dirWest: '┼',
	dirNorth:                                '╵',
	dirSouth:                                '╷',
	dirEast:                                 '╶',
	dirWest:                                 '╴',
}

type canvas struct {
	cells [][]gridCell
	w, h  int
}

func newCanvas(w, h int) *canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	cells := make([][]gridCell, h)
	for i := range cells {
		row := make([]gridCell, w)
		for j := range row {
			row[j] = gridCell{r: ' ', style: styleDefault}
		}
		cells[i] = row
	}
	return &canvas{cells: cells, w: w, h: h}
}

func (c *canvas) set(row, col int, r rune, style cellStyle) {
	if row < 0 || row >= c.h || col < 0 || col >= c.w {
		return
	}
	c.cells[row][col] = gridCell{r: r, style: style}
}

// addLine merges dirs into whatever line directions already occupy (row,
// col) and re-resolves the glyph from the combined set, so two edges whose
// lines cross or merge at the same cell produce a proper junction (a T or a
// cross) rather than one edge's draw silently erasing the other's.
func (c *canvas) addLine(row, col int, dirs uint8, style cellStyle) {
	if row < 0 || row >= c.h || col < 0 || col >= c.w {
		return
	}
	merged := c.cells[row][col].lineDirs | dirs
	c.cells[row][col] = gridCell{r: lineGlyphs[merged], style: style, lineDirs: merged}
}

func (c *canvas) writeText(row, col int, text string, style cellStyle) {
	for i, r := range []rune(text) {
		c.set(row, col+i, r, style)
	}
}

func (c *canvas) writeTextMasked(row, col int, text string, mask []bool, base, hl cellStyle) {
	for i, r := range []rune(text) {
		st := base
		if i < len(mask) && mask[i] {
			st = hl
		}
		c.set(row, col+i, r, st)
	}
}

// drawSegment draws one waypoint-to-waypoint hop as a single elbowed line
// (straight if the row is unchanged), bending at a column midway between
// the two endpoints. arrowAtEnd places an arrowhead at (rowTo, colTo);
// forward picks a rightward (▶) vs. leftward (◀) arrowhead. When arrowAtEnd
// and label is non-empty, the label is written just before the arrowhead if
// there's room on the line -- used for a multi-edge's "(×N)" aggregate
// count -- and silently dropped otherwise. Every non-arrow cell is drawn via
// canvas.addLine rather than a direct rune, so a cell where two edges'
// lines cross or merge resolves to the correct T/cross junction instead of
// one edge's draw overwriting the other's.
func (c *canvas) drawSegment(rowFrom, colFrom, rowTo, colTo int, style cellStyle, arrowAtEnd, forward bool, label string) {
	arrowRune := '▶' // ▶
	if !forward {
		arrowRune = '◀' // ◀
	}

	var finalRow, finalLo, finalHi int

	if rowFrom == rowTo {
		lo, hi := colFrom, colTo
		if lo > hi {
			lo, hi = hi, lo
		}
		for x := lo; x <= hi; x++ {
			if arrowAtEnd && x == colTo {
				c.set(rowFrom, x, arrowRune, style)
				continue
			}
			c.addLine(rowFrom, x, dirEast|dirWest, style)
		}
		finalRow, finalLo, finalHi = rowFrom, lo, hi
	} else {
		mid := colFrom + (colTo-colFrom)/2
		if mid == colFrom {
			if colTo > colFrom {
				mid++
			} else {
				mid--
			}
		}

		loH, hiH := colFrom, mid
		if loH > hiH {
			loH, hiH = hiH, loH
		}
		for x := loH; x <= hiH; x++ {
			if x == mid {
				continue // the bend cell is set below, combined with the vertical run
			}
			c.addLine(rowFrom, x, dirEast|dirWest, style)
		}

		loV, hiV := rowFrom, rowTo
		if loV > hiV {
			loV, hiV = hiV, loV
		}
		for y := loV; y <= hiV; y++ {
			if y == rowFrom || y == rowTo {
				continue // the two bend cells are set below
			}
			c.addLine(y, mid, dirNorth|dirSouth, style)
		}

		fromHoriz, fromVert := dirWest, dirSouth
		if colFrom > mid {
			fromHoriz = dirEast
		}
		if rowTo < rowFrom {
			fromVert = dirNorth
		}
		c.addLine(rowFrom, mid, fromHoriz|fromVert, style)

		toHoriz, toVert := dirEast, dirNorth
		if mid > colTo {
			toHoriz = dirWest
		}
		if rowFrom > rowTo {
			toVert = dirSouth
		}
		c.addLine(rowTo, mid, toHoriz|toVert, style)

		loH2, hiH2 := mid, colTo
		if loH2 > hiH2 {
			loH2, hiH2 = hiH2, loH2
		}
		for x := loH2; x <= hiH2; x++ {
			if x == mid {
				continue
			}
			if arrowAtEnd && x == colTo {
				c.set(rowTo, x, arrowRune, style)
				continue
			}
			c.addLine(rowTo, x, dirEast|dirWest, style)
		}
		finalRow, finalLo, finalHi = rowTo, loH2, hiH2
	}

	if arrowAtEnd && label != "" && colTo >= finalLo && colTo <= finalHi {
		labelRunes := utf8.RuneCountInString(label)
		start := colTo - labelRunes - 1
		if !forward {
			start = colTo + 2
		}
		if start >= finalLo && start+labelRunes-1 <= finalHi {
			c.writeText(finalRow, start, label, styleFooter)
		}
	}
}

func (c *canvas) drawBox(top, left, width int, n *graph.Node, hl graphNodeHighlight) {
	right := left + width - 1
	bottom := top + nodeBoxHeight - 1
	c.set(top, left, '┌', styleBorder)
	c.set(top, right, '┐', styleBorder)
	c.set(bottom, left, '└', styleBorder)
	c.set(bottom, right, '┘', styleBorder)
	for x := left + 1; x < right; x++ {
		c.set(top, x, '─', styleBorder)
		c.set(bottom, x, '─', styleBorder)
	}
	for y := top + 1; y < bottom; y++ {
		c.set(y, left, '│', styleBorder)
		c.set(y, right, '│', styleBorder)
	}

	inner := width - 4
	labelCol := left + 2
	label := truncateLabel(strings.Join(n.Path, "."), inner)
	if hl.label != nil {
		c.writeTextMasked(top+1, labelCol, label, hl.label, styleLabel, styleLabelMatch)
	} else {
		c.writeText(top+1, labelCol, label, styleLabel)
	}
	c.writeText(top+2, labelCol, truncateLabel(nodeFooter(n), inner), styleFooter)
}

func (c *canvas) lines() []string {
	out := make([]string, c.h)
	for i, row := range c.cells {
		var b strings.Builder
		j := 0
		for j < len(row) {
			k := j
			for k < len(row) && row[k].style == row[j].style {
				k++
			}
			seg := make([]rune, 0, k-j)
			for _, cell := range row[j:k] {
				seg = append(seg, cell.r)
			}
			b.WriteString(cellStyles[row[j].style].Render(string(seg)))
			j = k
		}
		out[i] = b.String()
	}
	return out
}

type graphLayout struct {
	c *canvas
}

func (l *graphLayout) lines() []string {
	return l.c.lines()
}

// layoutGraph lays g out left-to-right: layers become columns, ordering
// within a layer becomes rows. It builds the full-size canvas at whatever
// width/height the graph naturally needs -- no attempt is made to fit the
// current terminal size, since the result flows through the same
// scroll/crop viewport the tree view already uses.
func layoutGraph(g *graph.Graph, highlights map[string]graphNodeHighlight) *graphLayout {
	forward, feedback := graph.DetectFeedbackEdges(g)
	layer := graph.AssignLayers(g, forward)
	positions, layers, unitEdges, routed := routeEdges(g, layer, feedback)
	orderLayers(layers, positions, unitEdges)

	maxLayer := 0
	for l := range layers {
		if l > maxLayer {
			maxLayer = l
		}
	}
	maxRow := 0
	for _, keys := range layers {
		if len(keys) > maxRow {
			maxRow = len(keys)
		}
	}

	colWidth := make([]int, maxLayer+1)
	colX := make([]int, maxLayer+1)
	for l := 0; l <= maxLayer; l++ {
		w := minColumnWidth
		for _, key := range layers[l] {
			p := positions[key]
			if p.dummy {
				continue
			}
			if bw := nodeBoxWidth(p.node); bw > w {
				w = bw
			}
		}
		colWidth[l] = w
	}
	x := leftMargin
	for l := 0; l <= maxLayer; l++ {
		colX[l] = x
		x += colWidth[l] + columnGap
	}
	totalWidth := x

	rowSlot := nodeBoxHeight + rowGap
	totalHeight := topMargin + max(1, maxRow)*rowSlot + rowGap

	cv := newCanvas(totalWidth, totalHeight)

	// rightPoint/leftPoint anchor every hop to the boundary of its layer's
	// column -- for a real node that's just outside its box border, and a
	// dummy waypoint (which has no box) is treated identically, as a
	// zero-width placeholder at that same boundary. This keeps every
	// bend/vertical strictly inside the gap between columns, never inside a
	// column's own span -- which matters because a dummy shares its layer's
	// column with whatever real nodes are also in that layer, and those
	// nodes' boxes fill the column's full width at their own rows.
	centerRow := func(p *positioned) int { return topMargin + p.row*rowSlot + nodeBoxHeight/2 }
	boxLeft := func(p *positioned) int { return colX[p.layer] }
	rightPoint := func(p *positioned) (int, int) { return centerRow(p), boxLeft(p) + colWidth[p.layer] }
	leftPoint := func(p *positioned) (int, int) { return centerRow(p), boxLeft(p) - 1 }

	for _, re := range routed {
		style := styleEdge
		if re.feedback {
			style = styleEdgeFeedback
		}
		for i := 0; i+1 < len(re.waypoints); i++ {
			a := positions[re.waypoints[i]]
			b := positions[re.waypoints[i+1]]
			if a.layer == b.layer {
				continue
			}
			forward := b.layer > a.layer
			var fr, fc, tr, tc int
			if forward {
				fr, fc = rightPoint(a)
				tr, tc = leftPoint(b)
			} else {
				fr, fc = leftPoint(a)
				tr, tc = rightPoint(b)
			}
			arrow := i+2 == len(re.waypoints)
			label := ""
			if arrow && re.edge.Count > 1 {
				label = fmt.Sprintf("(×%d)", re.edge.Count)
			}
			cv.drawSegment(fr, fc, tr, tc, style, arrow, forward, label)
		}

		// Each interior dummy waypoint now anchors its incoming and outgoing
		// hops to opposite boundaries of its own column (whichever boundary
		// faces that neighbor), so it needs a straight bridge across its own
		// row connecting those two boundaries -- safe because a dummy's row
		// is never shared with a real box in that same layer.
		for i := 1; i < len(re.waypoints)-1; i++ {
			p := positions[re.waypoints[i]]
			row, lc := leftPoint(p)
			_, rc := rightPoint(p)
			cv.drawSegment(row, lc, row, rc, style, false, true, "")
		}
	}

	for _, keys := range layers {
		for _, key := range keys {
			p := positions[key]
			if p.dummy {
				continue
			}
			top := topMargin + p.row*rowSlot
			cv.drawBox(top, boxLeft(p), colWidth[p.layer], p.node, highlights[p.node.Key])
		}
	}

	return &graphLayout{c: cv}
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
