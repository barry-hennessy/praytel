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

// Layout constants. Sizes are deliberately generous rather than tightly
// packed: the rendered graph flows through the same horizontal/vertical
// scroll-and-crop pipeline the tree view already uses (see Model.View), so
// there's no need to fit everything inside the visible terminal -- panning
// reveals the rest.
const (
	minNodeInnerWidth = 8
	minColumnWidth    = 6
	minColumnGap      = 6
	rowGap            = 1
	minNodeInnerRows  = 2 // label + footer
	gapLead           = 2 // cells between a column's edge and the nearest lane
	laneSpacing       = 2
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
	return fmt.Sprintf("%d span%s", n.SpanCount, plural(n.SpanCount))
}

func nodeBoxWidth(n *graph.Node) int {
	label := n.Label()
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
	styleLabel
	styleLabelMatch
	styleFooter
	styleNeutral
	stylePalette // first of len(componentPalette) styles; see componentStyle
)

var cellStyles = map[cellStyle]lipgloss.Style{
	styleDefault:    lipgloss.NewStyle(),
	styleLabel:      lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
	styleLabelMatch: matchHighlightStyle,
	styleFooter:     lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true),
	styleNeutral:    lipgloss.NewStyle().Foreground(neutralColor),
}

func init() {
	for i, color := range componentPalette {
		cellStyles[stylePalette+cellStyle(i)] = lipgloss.NewStyle().Foreground(color)
	}
}

// componentStyle is a component's own color (see Model.colors), or neutral
// for one that calls nothing. Its box border and every call leaving it are
// drawn in it, the same color the tree view gives its bar and rule.
func componentStyle(n *graph.Node) cellStyle {
	if n.Color == 0 {
		return styleNeutral
	}
	return stylePalette + cellStyle((n.Color-1)%len(componentPalette))
}

type gridCell struct {
	r        rune
	style    cellStyle
	lineDirs uint8 // 0 for non-line cells (text, borders, arrows); otherwise the OR of the dirXxx bits currently occupying this cell
}

// Direction bits for the box-drawing line glyphs a cell can connect to its
// neighbors. Multiple edges' lines that cross at the same cell accumulate
// bits here (see canvas.addLine) so the rendered glyph is the
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

// hop is one drawn step of a routed edge across the gap between two
// adjacent layers. left and right name its endpoints by which side of the
// gap they are on, not by direction: a feedback edge's hops run right to
// left.
type hop struct {
	left, right *positioned
	forward     bool   // the call runs left to right
	arrow       bool   // the edge's last hop: gets the arrowhead and label
	label       string // "(×N)" aggregate count, or ""
	style       cellStyle

	// Set by layout. Every hop has a port row of its own on each endpoint
	// and, unless those rows coincide, a lane column of its own in the gap,
	// so no two hops ever share a line.
	leftRow, rightRow int
	lane              int
}

func (h *hop) bent() bool { return h.leftRow != h.rightRow }

// orderLanes decides which bent hops of one gap take which lane, left to
// right. A hop runs along its left row from the gap's left edge to its
// lane, then along its right row from the lane to the gap's right edge, so
// when one hop's left row is another's right row the first must take the
// lane further left or the two would overlap on that row. Within that
// constraint hops are ordered so that a node's fan-out nests instead of
// crossing itself. Two hops that swap rows constrain each other both ways;
// that pair is left overlapping.
func orderLanes(bent []*hop) []*hop {
	down := func(h *hop) bool { return h.rightRow > h.leftRow }
	sort.SliceStable(bent, func(i, j int) bool {
		a, b := bent[i], bent[j]
		if down(a) != down(b) {
			return !down(a)
		}
		if down(a) {
			return a.leftRow > b.leftRow
		}
		return a.leftRow < b.leftRow
	})

	byLeftRow := make(map[int]*hop, len(bent))
	for _, h := range bent {
		byLeftRow[h.leftRow] = h
	}
	placed := make(map[*hop]bool, len(bent))
	out := make([]*hop, 0, len(bent))
	for len(out) < len(bent) {
		var next *hop
		for _, h := range bent {
			if placed[h] {
				continue
			}
			if next == nil {
				next = h // fallback if every remaining hop is waiting on another
			}
			if before := byLeftRow[h.rightRow]; before == nil || before == h || placed[before] {
				next = h
				break
			}
		}
		placed[next] = true
		out = append(out, next)
	}
	return out
}

// drawHop draws h across the gap spanning columns gapStart..gapEnd. Every
// non-arrow cell goes through canvas.addLine, so where this hop crosses
// another hop's lane the cell resolves to a cross rather than one line
// erasing the other.
func (c *canvas) drawHop(h *hop, gapStart, gapEnd int) {
	arrowRow, arrowCol, arrowRune := h.rightRow, gapEnd, '▶'
	if !h.forward {
		arrowRow, arrowCol, arrowRune = h.leftRow, gapStart, '◀'
	}
	horizontal := func(row, lo, hi int) {
		for x := lo; x <= hi; x++ {
			if h.arrow && row == arrowRow && x == arrowCol {
				c.set(row, x, arrowRune, h.style)
				continue
			}
			c.addLine(row, x, dirEast|dirWest, h.style)
		}
	}

	if !h.bent() {
		horizontal(h.leftRow, gapStart, gapEnd)
	} else {
		horizontal(h.leftRow, gapStart, h.lane-1)
		horizontal(h.rightRow, h.lane+1, gapEnd)
		lo, hi := min(h.leftRow, h.rightRow), max(h.leftRow, h.rightRow)
		for y := lo + 1; y < hi; y++ {
			c.addLine(y, h.lane, dirNorth|dirSouth, h.style)
		}
		towardRight, towardLeft := dirSouth, dirNorth
		if h.rightRow < h.leftRow {
			towardRight, towardLeft = dirNorth, dirSouth
		}
		c.addLine(h.leftRow, h.lane, dirWest|towardRight, h.style)
		c.addLine(h.rightRow, h.lane, dirEast|towardLeft, h.style)
	}

	if h.arrow && h.label != "" {
		start := gapEnd - utf8.RuneCountInString(h.label) - 1
		if !h.forward {
			start = gapStart + 2
		}
		c.writeText(arrowRow, start, h.label, styleFooter)
	}
}

func (c *canvas) drawBox(top, left, width, height int, n *graph.Node, hl graphNodeHighlight) {
	right := left + width - 1
	bottom := top + height - 1
	styleBorder := componentStyle(n)
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
	label := truncateLabel(n.Label(), inner)
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
//
// Separate calls are never drawn on top of each other: each one leaves and
// enters a box at a row of its own (boxes grow to fit) and bends in a lane
// of its own (gaps between columns widen to fit), so any two can be told
// apart along their whole length.
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

	// Every hop occupies one port on the facing side of each endpoint.
	// gaps[l] holds the hops between layers l and l+1.
	gaps := make([][]*hop, maxLayer+1)
	leftPorts := map[string][]*hop{}  // hops attached to a waypoint's left side
	rightPorts := map[string][]*hop{} // ...and to its right side
	for _, re := range routed {
		for i := 0; i+1 < len(re.waypoints); i++ {
			a, b := positions[re.waypoints[i]], positions[re.waypoints[i+1]]
			if a.layer == b.layer {
				continue
			}
			h := &hop{left: a, right: b, forward: true, style: componentStyle(g.Nodes[re.edge.FromKey])}
			if b.layer < a.layer {
				h.left, h.right, h.forward = b, a, false
			}
			if h.arrow = i+2 == len(re.waypoints); h.arrow && re.edge.Count > 1 {
				h.label = fmt.Sprintf("(×%d)", re.edge.Count)
			}
			gaps[h.left.layer] = append(gaps[h.left.layer], h)
			rightPorts[h.left.key] = append(rightPorts[h.left.key], h)
			leftPorts[h.right.key] = append(leftPorts[h.right.key], h)
		}
	}
	// Ports run top to bottom in the order of what they connect to, so a
	// node's own hops fan out without crossing each other.
	for _, hops := range rightPorts {
		sort.SliceStable(hops, func(i, j int) bool { return hops[i].right.row < hops[j].right.row })
	}
	for _, hops := range leftPorts {
		sort.SliceStable(hops, func(i, j int) bool { return hops[i].left.row < hops[j].left.row })
	}

	// Rows: each layer stacks its own waypoints. A dummy is a single row; a
	// box is tall enough for its busier side's ports.
	innerRows := func(p *positioned) int {
		return max(minNodeInnerRows, len(leftPorts[p.key]), len(rightPorts[p.key]))
	}
	top := map[string]int{}
	totalHeight := 0
	for l := 0; l <= maxLayer; l++ {
		y := topMargin
		for _, key := range layers[l] {
			p := positions[key]
			top[key] = y
			if p.dummy {
				y += 1 + rowGap
			} else {
				y += innerRows(p) + 2 + rowGap
			}
		}
		totalHeight = max(totalHeight, y)
	}
	portRow := func(p *positioned, ports []*hop, h *hop) int {
		if p.dummy {
			return top[p.key]
		}
		i := 0
		for ports[i] != h {
			i++
		}
		return top[p.key] + 1 + (innerRows(p)-len(ports))/2 + i
	}

	// Columns: each layer is as wide as its widest box, and each gap as
	// wide as its lanes plus room for the longest count label at either end.
	colWidth := make([]int, maxLayer+1)
	colX := make([]int, maxLayer+1)
	x := leftMargin
	for l := 0; l <= maxLayer; l++ {
		w := minColumnWidth
		for _, key := range layers[l] {
			if p := positions[key]; !p.dummy {
				w = max(w, nodeBoxWidth(p.node))
			}
		}
		colWidth[l] = w
		colX[l] = x
		x += w

		lead, tail := gapLead, gapLead
		var bent []*hop
		for _, h := range gaps[l] {
			h.leftRow = portRow(h.left, rightPorts[h.left.key], h)
			h.rightRow = portRow(h.right, leftPorts[h.right.key], h)
			if h.bent() {
				bent = append(bent, h)
			}
			if room := utf8.RuneCountInString(h.label) + 3; h.label != "" && h.forward {
				tail = max(tail, room)
			} else if h.label != "" {
				lead = max(lead, room)
			}
		}
		lanes := 0
		for i, h := range orderLanes(bent) {
			h.lane = x + lead + i*laneSpacing
			lanes = (i+1)*laneSpacing - 1
		}
		x += max(minColumnGap, lead+lanes+tail)
	}

	cv := newCanvas(x, totalHeight)

	for l, hops := range gaps {
		if len(hops) == 0 {
			continue
		}
		gapStart, gapEnd := colX[l]+colWidth[l], colX[l+1]-1
		for _, h := range hops {
			cv.drawHop(h, gapStart, gapEnd)
		}
	}

	for _, keys := range layers {
		for _, key := range keys {
			p := positions[key]
			if !p.dummy {
				cv.drawBox(top[key], colX[p.layer], colWidth[p.layer], innerRows(p)+2, p.node, highlights[p.node.Key])
				continue
			}
			// A dummy has no box, so the edge passing through it needs a
			// straight bridge across its column joining its two hops.
			for _, h := range leftPorts[key] {
				for dx := 0; dx < colWidth[p.layer]; dx++ {
					cv.addLine(top[key], colX[p.layer]+dx, dirEast|dirWest, h.style)
				}
			}
		}
	}

	return &graphLayout{c: cv}
}

// graphLines renders the current component interconnection graph
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
