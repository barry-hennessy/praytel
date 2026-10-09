package tui

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph/layout"
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
	rowGap            = 1
	nodeBoxHeight     = 4
	leftMargin        = 1
	topMargin         = 1

	// The gap between two columns is gapLead cells of line out of the left
	// column, one vertical track per bus spaced trackPitch apart, then
	// gapTail cells into the right column, the last of which holds the
	// arrowhead. Count labels widen the lead or tail they sit in.
	gapLead    = 2
	gapTail    = 3
	trackPitch = 2
)

var placeOptions = layout.Options{NodeHeight: nodeBoxHeight, RowGap: rowGap}

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
	styleLabelMatch
	styleFooter
	styleCrossing

	// Edge styles are in precedence order: where lines of different styles
	// share a cell, the later one wins.
	styleEdgeFeedback
	styleEdgeLight
	styleEdge
	styleEdgeHeavy

	// styleComponent+i draws a box in depthColors[i].
	styleComponent
)

var cellStyles = func() []lipgloss.Style {
	styles := []lipgloss.Style{
		styleDefault:      lipgloss.NewStyle(),
		styleLabelMatch:   matchHighlightStyle,
		styleFooter:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true),
		styleCrossing:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		styleEdgeFeedback: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		styleEdgeLight:    lipgloss.NewStyle().Foreground(lipgloss.Color("31")),
		styleEdge:         lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		styleEdgeHeavy:    lipgloss.NewStyle().Foreground(lipgloss.Color("87")).Bold(true),
	}
	for _, color := range depthColors {
		styles = append(styles, lipgloss.NewStyle().Foreground(color))
	}
	return styles
}()

// componentStyle picks a box color from the component name alone, so a
// component keeps its color as other components come and go and all of one
// component's sections read as a group.
func componentStyle(component string) cellStyle {
	h := fnv.New32a()
	h.Write([]byte(component))
	return styleComponent + cellStyle(h.Sum32()%uint32(len(depthColors)))
}

// edgeWeightStyle buckets an edge's call count against the graph's range,
// so the busiest calls stand out. Weight is carried by color rather than by
// heavier glyphs because box-drawing has no heavy rounded corners.
func edgeWeightStyle(g *graph.Graph) func(count int) cellStyle {
	lo, hi := 0, 0
	for _, e := range g.Edges {
		if lo == 0 || e.Count < lo {
			lo = e.Count
		}
		hi = max(hi, e.Count)
	}
	return func(count int) cellStyle {
		if hi == lo {
			return styleEdge
		}
		return styleEdgeLight + cellStyle((count-lo)*2/(hi-lo))
	}
}

type gridCell struct {
	r        rune
	style    cellStyle
	lineDirs uint8 // 0 for non-line cells (text, borders, arrows); otherwise the OR of the dirXxx bits currently occupying this cell
	bend     bool  // some line turns or ends here, as opposed to every line passing straight through
	lines    uint8 // how many traced lines cover this cell
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
	dirNorth | dirEast:                      '╰',
	dirNorth | dirWest:                      '╯',
	dirSouth | dirEast:                      '╭',
	dirSouth | dirWest:                      '╮',
	dirNorth | dirSouth | dirEast:           '├',
	dirNorth | dirSouth | dirWest:           '┤',
	dirEast | dirWest | dirSouth:            '┬',
	dirEast | dirWest | dirNorth:            '┴',
	dirNorth | dirSouth | dirEast | dirWest: '┼',
}

// glyph resolves what a cell shows. Two things can't be decided until every
// line is down: a four-way cell that nothing bends in is two unrelated
// lines crossing, drawn dim so each reads as continuing straight through
// rather than as a junction; and a feedback line's straight runs are dashed.
func (g gridCell) glyph() (rune, cellStyle) {
	switch {
	case g.lineDirs == 0:
		return g.r, g.style
	case g.lineDirs == dirNorth|dirSouth|dirEast|dirWest && !g.bend:
		return '┼', styleCrossing
	case g.style == styleEdgeFeedback && g.lineDirs == dirEast|dirWest:
		return '╌', g.style
	case g.style == styleEdgeFeedback && g.lineDirs == dirNorth|dirSouth:
		return '╎', g.style
	}
	return lineGlyphs[g.lineDirs], g.style
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
// col), so two edges whose lines cross or merge at the same cell produce a
// proper junction (a T or a cross) rather than one edge's draw silently
// erasing the other's.
func (c *canvas) addLine(row, col int, dirs uint8, style cellStyle, bend bool) {
	if row < 0 || row >= c.h || col < 0 || col >= c.w {
		return
	}
	cell := &c.cells[row][col]
	if cell.lineDirs != 0 {
		style = max(style, cell.style)
	}
	*cell = gridCell{style: style, lineDirs: cell.lineDirs | dirs, bend: cell.bend || bend, lines: cell.lines + 1}
}

type cellPos struct {
	row, col int
}

// trace draws one continuous line through pts, each consecutive pair of
// which shares a row or a column. Both end cells are drawn as full line
// segments rather than half stubs, since a line ends against a box or under
// an arrowhead.
func (c *canvas) trace(pts []cellPos, style cellStyle) {
	cells := []cellPos{pts[0]}
	for _, p := range pts[1:] {
		for q := cells[len(cells)-1]; q != p; {
			q.row += sign(p.row - q.row)
			q.col += sign(p.col - q.col)
			cells = append(cells, q)
		}
	}
	toward := func(from, to cellPos) uint8 {
		switch {
		case to.row < from.row:
			return dirNorth
		case to.row > from.row:
			return dirSouth
		case to.col > from.col:
			return dirEast
		}
		return dirWest
	}
	for i, p := range cells {
		var dirs uint8
		if i > 0 {
			dirs |= toward(p, cells[i-1])
		}
		if i+1 < len(cells) {
			dirs |= toward(p, cells[i+1])
		}
		straight := dirs == dirNorth|dirSouth || dirs == dirEast|dirWest
		if i == 0 || i+1 == len(cells) {
			dirs = dirEast | dirWest
		}
		c.addLine(p.row, p.col, dirs, style, !straight)
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
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

// writeLabel puts text over a stretch of line, but only where that stretch
// (and a cell either side, so the label reads as sitting on the line) is
// one edge's plain horizontal run -- never over a bend, a crossing, or a
// run several edges share, where a count would be attributed to the wrong
// edge. It reports whether the label fit.
func (c *canvas) writeLabel(row, col int, text string) bool {
	n := utf8.RuneCountInString(text)
	if row < 0 || row >= c.h || col < 1 || col+n >= c.w {
		return false
	}
	for _, cell := range c.cells[row][col-1 : col+n+1] {
		if cell.lineDirs != dirEast|dirWest || cell.lines != 1 || cell.bend {
			return false
		}
	}
	c.writeText(row, col, text, styleFooter)
	return true
}

// labelLine writes text on the first free stretch of the line through pts,
// searching from its last point back toward its first if fromEnd, so the
// label sits as near that end as it can. It is dropped if the line has no
// stretch to itself that is long enough.
func (c *canvas) labelLine(pts []cellPos, text string, fromEnd bool) {
	n := utf8.RuneCountInString(text)
	for i := range len(pts) - 1 {
		a, b := pts[i], pts[i+1]
		if fromEnd {
			a, b = pts[len(pts)-2-i], pts[len(pts)-1-i]
		}
		if a.row != b.row {
			continue
		}
		for col := b.col - n; col >= a.col; col-- {
			at := col
			if !fromEnd {
				at = a.col + b.col - n - col
			}
			if c.writeLabel(a.row, at, text) {
				return
			}
		}
	}
}

func (c *canvas) drawBox(top, left, width int, n *graph.Node, hl graphNodeHighlight) {
	style := componentStyle(n.Path[0])
	right := left + width - 1
	bottom := top + nodeBoxHeight - 1
	c.set(top, left, '╭', style)
	c.set(top, right, '╮', style)
	c.set(bottom, left, '╰', style)
	c.set(bottom, right, '╯', style)
	for x := left + 1; x < right; x++ {
		c.set(top, x, '─', style)
		c.set(bottom, x, '─', style)
	}
	for y := top + 1; y < bottom; y++ {
		c.set(y, left, '│', style)
		c.set(y, right, '│', style)
	}

	inner := width - 4
	labelCol := left + 2
	label := truncateLabel(strings.Join(n.Path, "."), inner)
	c.writeTextMasked(top+1, labelCol, label, hl.label, style, styleLabelMatch)
	c.writeText(top+2, labelCol, truncateLabel(nodeFooter(n), inner), styleFooter)
}

func (c *canvas) lines() []string {
	out := make([]string, c.h)
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
		out[i] = b.String()
	}
	return out
}

// route is one drawn line running left to right through the layers: an
// edge, or both edges of a two-node cycle, which share a path and get an
// arrowhead at each end.
type route struct {
	first  int   // leftmost layer
	rows   []int // the row the line holds while crossing each layer, from first onward
	tracks []int // per hop to the next layer: which of the gap's vertical tracks it turns on (unused where the line stays level)
	style  cellStyle
	pts    []cellPos // the cells the line turns at, once drawn

	forward, backward *graph.Edge // the edge pointing right and the one pointing left along the line; either may be nil
}

// buildRoutes turns placed edges into routes, in the graph's deterministic
// edge order.
func buildRoutes(g *graph.Graph, p layout.Placement) []*route {
	weight := edgeWeightStyle(g)
	byPair := map[[2]string]*route{}
	var routes []*route
	for _, ek := range graph.SortedEdgeKeys(g) {
		placed := p.Edges[ek]
		left, right := ek[0], ek[1]
		if placed.Feedback {
			left, right = right, left
		}
		r, ok := byPair[[2]string{left, right}]
		if !ok {
			r = &route{first: p.Nodes[left].Layer, style: styleEdgeFeedback}
			r.rows = append(r.rows, placeOptions.Port(p.Nodes[left]))
			for i := range placed.Waypoints {
				w := placed.Waypoints[i]
				if placed.Feedback {
					w = placed.Waypoints[len(placed.Waypoints)-1-i]
				}
				r.rows = append(r.rows, w.Row)
			}
			r.rows = append(r.rows, placeOptions.Port(p.Nodes[right]))
			r.tracks = make([]int, len(r.rows)-1)
			byPair[[2]string{left, right}] = r
			routes = append(routes, r)
		}
		if placed.Feedback {
			r.backward = g.Edges[ek]
		} else {
			r.forward = g.Edges[ek]
			r.style = weight(r.forward.Count)
		}
	}
	return routes
}

// hop is the part of a route that changes row between two adjacent layers,
// and so needs a vertical somewhere in the gap between them.
type hop struct {
	from, to int // rows on the left and right side of the gap
	route    *route
	index    int // which of the route's hops this is
}

// assignTracks gives every hop in one gap a vertical track, returning how
// many tracks the gap needs.
//
// Hops are grouped into buses that share a track: all the hops leaving one
// node, or all the hops arriving at one, whichever group is larger -- so a
// fan-out or fan-in draws as one trunk with branches, and lines that have
// nothing to do with each other never run along the same vertical. Buses
// are then ordered left to right to keep one bus's horizontal from landing
// on top of another's, and after that to cross as little as possible.
func assignTracks(hops []hop) int {
	leaving, arriving := map[int]int{}, map[int]int{}
	for _, h := range hops {
		leaving[h.from]++
		arriving[h.to]++
	}
	type busKey struct {
		arriving bool
		row      int
	}
	index := map[busKey]int{}
	var buses [][]hop
	for _, h := range hops {
		key := busKey{row: h.from}
		if arriving[h.to] > leaving[h.from] {
			key = busKey{arriving: true, row: h.to}
		}
		i, ok := index[key]
		if !ok {
			i = len(buses)
			index[key] = i
			buses = append(buses, nil)
		}
		buses[i] = append(buses[i], h)
	}

	// Each swap of neighbors strictly lowers the total cost, so this ends.
	for swapped := true; swapped; {
		swapped = false
		for i := 0; i+1 < len(buses); i++ {
			if trackCost(buses[i+1], buses[i]) < trackCost(buses[i], buses[i+1]) {
				buses[i], buses[i+1] = buses[i+1], buses[i]
				swapped = true
			}
		}
	}
	for i, bus := range buses {
		for _, h := range bus {
			h.route.tracks[h.index] = i
		}
	}
	return len(buses)
}

// trackCost scores putting one bus's track to the left of another's. A hop
// runs level from the left column to its track, vertically along it, then
// level to the right column. So with a left of b: a's run into the right
// column would overlap b's run out of the left column if they are on the
// same row (heavily penalized -- two unrelated lines would merge), and each
// place one hop's level run passes through the other's vertical is a
// crossing.
func trackCost(left, right []hop) int {
	between := func(row, a, b int) bool {
		return row > min(a, b) && row < max(a, b)
	}
	cost := 0
	for _, a := range left {
		for _, b := range right {
			if a.to == b.from {
				cost += 100
			}
			if between(b.from, a.from, a.to) {
				cost++
			}
			if between(a.to, b.from, b.to) {
				cost++
			}
		}
	}
	return cost
}

func countLabel(e *graph.Edge) string {
	if e == nil || e.Count < 2 {
		return ""
	}
	return fmt.Sprintf("×%d", e.Count)
}

type graphLayout struct {
	c *canvas
}

func (l *graphLayout) lines() []string {
	return l.c.lines()
}

// layoutGraph lays g out left-to-right: layers become columns, and
// layout.Place decides each node's row. It builds the full-size canvas at
// whatever width/height the graph naturally needs -- no attempt is made to
// fit the current terminal size, since the result flows through the same
// scroll/crop viewport the tree view already uses.
func layoutGraph(g *graph.Graph, highlights map[string]graphNodeHighlight) *graphLayout {
	p := layout.Place(g, placeOptions)
	routes := buildRoutes(g, p)

	// A gap is the space between layer i and layer i+1, where every bend
	// lives: lines only ever cross a column level, on a row no box in that
	// column occupies.
	type gap struct {
		hops                 []hop
		tracks               int
		leadLabel, tailLabel int // widest count label sitting in the lead and in the tail
		start, end           int // first and last column
	}
	gaps := make([]gap, max(0, p.Layers-1))
	for _, r := range routes {
		for i := 0; i+1 < len(r.rows); i++ {
			if r.rows[i] != r.rows[i+1] {
				gp := &gaps[r.first+i]
				gp.hops = append(gp.hops, hop{from: r.rows[i], to: r.rows[i+1], route: r, index: i})
			}
		}
		head, tail := &gaps[r.first], &gaps[r.first+len(r.rows)-2]
		head.leadLabel = max(head.leadLabel, utf8.RuneCountInString(countLabel(r.backward)))
		tail.tailLabel = max(tail.tailLabel, utf8.RuneCountInString(countLabel(r.forward)))
	}

	colWidth := make([]int, p.Layers)
	for i := range colWidth {
		colWidth[i] = minColumnWidth
	}
	for key, n := range p.Nodes {
		colWidth[n.Layer] = max(colWidth[n.Layer], nodeBoxWidth(g.Nodes[key]))
	}
	colX := make([]int, p.Layers)
	x := leftMargin
	for l := range colX {
		colX[l] = x
		x += colWidth[l]
		if l == len(gaps) {
			break
		}
		gp := &gaps[l]
		gp.tracks = assignTracks(gp.hops)
		gp.start = x
		if gp.leadLabel > 0 {
			gp.leadLabel++ // a cell of line between the arrowhead and its label
		}
		x += gapLead + gp.leadLabel + max(0, gp.tracks-1)*trackPitch + min(gp.tracks, 1) + gapTail + gp.tailLabel
		gp.end = x - 1
	}

	cv := newCanvas(x+1, topMargin+p.Rows+1)

	for _, r := range routes {
		last := len(r.rows) - 1
		head, tail := gaps[r.first], gaps[r.first+last-1]
		pts := []cellPos{{topMargin + r.rows[0], head.start}}
		for i := 0; i < last; i++ {
			if r.rows[i] == r.rows[i+1] {
				continue
			}
			gp := gaps[r.first+i]
			track := gp.start + gapLead + gp.leadLabel + r.tracks[i]*trackPitch
			pts = append(pts, cellPos{topMargin + r.rows[i], track}, cellPos{topMargin + r.rows[i+1], track})
		}
		pts = append(pts, cellPos{topMargin + r.rows[last], tail.end})
		cv.trace(pts, r.style)
		r.pts = pts
	}

	// Labels need every line down first, to know which stretches are one
	// edge's alone; arrowheads go on after that so they can't be mistaken
	// for line. Each count sits as near its own arrowhead as it can.
	for _, r := range routes {
		if label := countLabel(r.forward); label != "" {
			cv.labelLine(r.pts, label, true)
		}
		if label := countLabel(r.backward); label != "" {
			cv.labelLine(r.pts, label, false)
		}
	}
	for _, r := range routes {
		last := len(r.rows) - 1
		if r.forward != nil {
			cv.set(topMargin+r.rows[last], gaps[r.first+last-1].end, '▶', r.style)
		}
		if r.backward != nil {
			cv.set(topMargin+r.rows[0], gaps[r.first].start, '◀', styleEdgeFeedback)
		}
	}

	for _, key := range graph.OrderedNodeKeys(g) {
		n := p.Nodes[key]
		cv.drawBox(topMargin+n.Row, colX[n.Layer], colWidth[n.Layer], g.Nodes[key], highlights[key])
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
