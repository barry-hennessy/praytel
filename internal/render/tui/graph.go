package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph/layout"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// buildGraph aggregates the currently filtered/visible spans (per
// filteredForest and fuzzyVisible -- the same layers layoutTree applies, so
// the graph can never show something the tree wouldn't) into a graph.Graph:
// one node per distinct nested component, one edge per pair of components
// connected by an observed parent/child span call. The aggregation rule
// itself (how names nest, and that same-component calls never become
// self-edges) lives in the graph package, which knows
// nothing about Model or live filtering; buildGraph's own job is only to
// decide which spans are visible right now and hand their facts over.
func (m Model) buildGraph() *graph.Graph {
	children, _, inScope := m.filteredForest()
	traceMatches := m.matchingTraces()
	logBranches := m.logBranches(children, inScope, m.matchingLogs())

	facts := make([]graph.SpanFact, 0, len(inScope))
	for _, sp := range inScope {
		if !m.fuzzyVisible(sp, traceMatches, logBranches) {
			continue
		}
		fact := graph.SpanFact{Component: sp.component, Name: sp.name, Order: sp.order, Color: m.colors[spanLabel(sp)]}
		if sp.parentID != "" {
			if parent, ok := inScope[selectionKey(sp.traceID, sp.parentID)]; ok && m.fuzzyVisible(parent, traceMatches, logBranches) {
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
	return fmt.Sprintf("%d span%s", n.SpanCount, plural(n.SpanCount))
}

func nodeBoxWidth(n *graph.Node) int {
	inner := max(minNodeInnerWidth, utf8.RuneCountInString(n.Label()), utf8.RuneCountInString(nodeFooter(n)))
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
	styleCrossing

	// Component styles are in precedence order: where lines of different
	// styles share a cell, the later one wins.
	styleNeutral
	stylePalette // first of len(componentPalette) styles; see componentStyle
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
	return stylePalette + cellStyle((n.Color-1)%len(componentPalette)) //nolint:gosec // Color is 1..len(componentPalette); see Model.colors
}

type gridCell struct {
	r        rune
	style    cellStyle
	lineDirs uint8 // 0 for non-line cells (text, borders, arrows); otherwise the OR of the dirXxx bits currently occupying this cell
	bend     bool  // some line turns or ends here, as opposed to every line passing straight through
	dashed   bool  // every line here closes a cycle
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
	case g.dashed && g.lineDirs == dirEast|dirWest:
		return '╌', g.style
	case g.dashed && g.lineDirs == dirNorth|dirSouth:
		return '╎', g.style
	}
	return lineGlyphs[g.lineDirs], g.style
}

type canvas struct {
	cells [][]gridCell
	w, h  int

	traced []cellPos // trace's scratch space, kept between lines
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
// erasing the other's. A cell stays dashed only while every line through it
// is.
func (c *canvas) addLine(row, col int, dirs uint8, style cellStyle, bend, dashed bool) {
	if row < 0 || row >= c.h || col < 0 || col >= c.w {
		return
	}
	cell := &c.cells[row][col]
	if cell.lineDirs != 0 {
		style = max(style, cell.style)
		dashed = dashed && cell.dashed
	}
	*cell = gridCell{style: style, lineDirs: cell.lineDirs | dirs, bend: cell.bend || bend, dashed: dashed, lines: cell.lines + 1}
}

type cellPos struct {
	row, col int
}

// trace draws one continuous line through pts, each consecutive pair of
// which shares a row or a column. Both end cells are drawn as full line
// segments rather than half stubs, since a line ends against a box or under
// an arrowhead.
func (c *canvas) trace(pts []cellPos, style cellStyle, dashed bool) {
	cells := append(c.traced[:0], pts[0])
	defer func() { c.traced = cells }()
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
		c.addLine(p.row, p.col, dirs, style, !straight, dashed)
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
// label sits as near that end as it can. It reports false if the line has
// no level stretch to itself that is long enough.
func (c *canvas) labelLine(pts []cellPos, text string, fromEnd bool) bool {
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
				return true
			}
		}
	}
	return false
}

// labelBeside is the fallback for a line with no level stretch of its own:
// it writes text in the blank cells just right of one of the line's
// verticals, as near the middle of the longest one as there is room. Only a
// vertical that is this line's alone will do; beside a shared trunk the
// count would seem to be the trunk's.
func (c *canvas) labelBeside(pts []cellPos, text string) {
	n := utf8.RuneCountInString(text)
	blank := func(row, col int) bool {
		if col+n+1 > c.w {
			return false
		}
		for _, cell := range c.cells[row][col : col+n+1] { // the text and a cell of clearance
			if cell.r != ' ' || cell.lineDirs != 0 {
				return false
			}
		}
		return true
	}
	var best []cellPos // the two ends of the longest vertical that is the line's own
	for i := range len(pts) - 1 {
		a, b := pts[i], pts[i+1]
		if a.col != b.col || a.row == b.row {
			continue
		}
		lo, hi := min(a.row, b.row), max(a.row, b.row)
		own := true
		for row := lo + 1; row < hi; row++ {
			if cell := c.cells[row][a.col]; cell.lines != 1 && cell.lineDirs == dirNorth|dirSouth {
				own = false
			}
		}
		if own && hi-lo >= 2 && (best == nil || hi-lo > best[1].row-best[0].row) {
			best = []cellPos{{lo, a.col}, {hi, a.col}}
		}
	}
	if best == nil {
		return
	}
	mid := (best[0].row + best[1].row) / 2
	for off := 0; mid-off > best[0].row || mid+off < best[1].row; off++ {
		for _, row := range []int{mid - off, mid + off} {
			if row > best[0].row && row < best[1].row && blank(row, best[0].col+1) {
				c.writeText(row, best[0].col+1, text, styleFooter)
				return
			}
		}
	}
}

func (c *canvas) drawBox(top, left, width, height int, n *graph.Node, hl graphNodeHighlight) {
	style := componentStyle(n)
	right := left + width - 1
	bottom := top + height - 1
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
	col := left + 2
	label := truncateLabel(n.Label(), inner)
	c.writeTextMasked(top+1, col, label, hl.label, styleLabel, styleLabelMatch)
	c.writeText(top+2, col, truncateLabel(nodeFooter(n), inner), styleFooter)
}

func (c *canvas) lines() []string {
	return c.window(0, c.h, 0, c.w)
}

// window renders rows [from, to) of the canvas, each cut to the width
// columns starting at col. Cutting here, cell by cell, is far cheaper than
// rendering whole rows of a wide graph and cutting the styled text after.
//
// Each style's escape codes are worked out once up front, rather than
// rendering every run of cells through lipgloss: a run never holds a
// newline or tab, the only things Render does more than wrap its text for.
func (c *canvas) window(from, to, col, width int) []string {
	from, to = max(0, from), min(to, c.h)
	if from >= to {
		return nil
	}
	col = max(0, min(col, c.w))
	end := min(c.w, col+max(0, width))
	type wrap struct{ open, close string }
	wraps := make([]wrap, len(cellStyles))
	for i, style := range cellStyles {
		open, close, _ := strings.Cut(style.Render("#"), "#")
		wraps[i] = wrap{open, close}
	}
	out := make([]string, 0, to-from)
	var b strings.Builder
	for _, row := range c.cells[from:to] {
		b.Reset()
		segStyle := styleDefault
		open := false
		for _, cell := range row[col:end] {
			r, style := cell.glyph()
			if style != segStyle || !open {
				if open {
					b.WriteString(wraps[segStyle].close)
				}
				segStyle, open = style, true
				b.WriteString(wraps[segStyle].open)
			}
			b.WriteRune(r)
		}
		if open {
			b.WriteString(wraps[segStyle].close)
		}
		out = append(out, b.String())
	}
	return out
}

// route is one drawn line running left to right through the layers: an
// edge, or both edges of a two-node cycle, which share a path and get an
// arrowhead at each end.
type route struct {
	first  int       // leftmost layer
	rows   []int     // the row the line holds while crossing each layer, from first onward
	tracks []int     // per hop to the next layer: which of the gap's vertical tracks it turns on (unused where the line stays level)
	style  cellStyle // the calling component's color; the forward edge's caller where the line carries both
	dashed bool      // the line carries only an edge that closes a cycle
	pts    []cellPos // the cells the line turns at, once drawn

	forward, backward *graph.Edge // the edge pointing right and the one pointing left along the line; either may be nil
}

// buildRoutes turns placed edges into routes, in the graph's deterministic
// edge order.
func buildRoutes(g *graph.Graph, p layout.Placement) []*route {
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
			r = &route{first: p.Nodes[left].Layer, style: componentStyle(g.Nodes[ek[0]]), dashed: true}
			// The line runs from where the edge leaves its first node to
			// where it arrives at its second, or for a feedback edge the
			// other way about.
			leave, arrive := placed.FromRow, placed.ToRow
			if placed.Feedback {
				leave, arrive = arrive, leave
			}
			r.rows = append(r.rows, leave)
			for i := range placed.Waypoints {
				w := placed.Waypoints[i]
				if placed.Feedback {
					w = placed.Waypoints[len(placed.Waypoints)-1-i]
				}
				r.rows = append(r.rows, w.Row)
			}
			r.rows = append(r.rows, arrive)
			r.tracks = make([]int, len(r.rows)-1)
			byPair[[2]string{left, right}] = r
			routes = append(routes, r)
		}
		if placed.Feedback {
			r.backward = g.Edges[ek]
		} else {
			r.forward = g.Edges[ek]
			r.style, r.dashed = componentStyle(g.Nodes[ek[0]]), false
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

	// Neighboring buses whose verticals cover different rows, with a row to
	// spare between them, can stand on the same track: a fan-in's branches
	// from above and from below then share a column instead of each
	// widening the gap.
	type span struct{ lo, hi int }
	track := -1
	var onTrack []span
	for _, bus := range buses {
		s := span{bus[0].from, bus[0].from}
		for _, h := range bus {
			s.lo, s.hi = min(s.lo, h.from, h.to), max(s.hi, h.from, h.to)
		}
		fits := track >= 0
		for _, other := range onTrack {
			if s.lo <= other.hi+1 && other.lo <= s.hi+1 {
				fits = false
			}
		}
		if !fits {
			track++
			onTrack = onTrack[:0]
		}
		onTrack = append(onTrack, s)
		for _, h := range bus {
			h.route.tracks[h.index] = track
		}
	}
	return track + 1
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

// graphLayout is a laid-out graph, or, when c is nil, the placeholder shown
// in place of one.
type graphLayout struct {
	c           *canvas
	placeholder string
}

func (l *graphLayout) lines() []string {
	if l.c == nil {
		return []string{l.placeholder}
	}
	return l.c.lines()
}

func (l *graphLayout) lineCount() int {
	if l.c == nil {
		return 1
	}
	return l.c.h
}

// columns is how wide the graph is.
func (l *graphLayout) columns() int {
	if l.c == nil {
		return ansi.StringWidth(l.placeholder)
	}
	return l.c.w
}

// window renders lines [from, to) of the graph, cut to the width columns
// starting at col.
func (l *graphLayout) window(from, to, col, width int) []string {
	if l.c == nil {
		lines := []string{l.placeholder}[max(0, min(from, 1)):max(0, min(to, 1))]
		for i, line := range lines {
			lines[i] = ansi.Cut(line, col, col+width)
		}
		return lines
	}
	return l.c.window(from, to, col, width)
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
		cv.trace(pts, r.style, r.dashed)
		r.pts = pts
	}

	// Labels need every line down first, to know which stretches are one
	// edge's alone; arrowheads go on after that so they can't be mistaken
	// for line. Each count sits as near its own arrowhead as it can.
	type pending struct {
		pts   []cellPos
		label string
	}
	var beside []pending
	for _, r := range routes {
		if label := countLabel(r.forward); label != "" && !cv.labelLine(r.pts, label, true) {
			beside = append(beside, pending{r.pts, label})
		}
		if label := countLabel(r.backward); label != "" && !cv.labelLine(r.pts, label, false) {
			beside = append(beside, pending{r.pts, label})
		}
	}
	for _, l := range beside {
		cv.labelBeside(l.pts, l.label)
	}
	for _, r := range routes {
		last := len(r.rows) - 1
		if r.forward != nil {
			cv.set(topMargin+r.rows[last], gaps[r.first+last-1].end, '▶', r.style)
		}
		if r.backward != nil {
			cv.set(topMargin+r.rows[0], gaps[r.first].start, '◀', componentStyle(g.Nodes[r.backward.FromKey]))
		}
	}

	for _, key := range graph.OrderedNodeKeys(g) {
		n := p.Nodes[key]
		cv.drawBox(topMargin+n.Row, colX[n.Layer], colWidth[n.Layer], n.Height, g.Nodes[key], highlights[key])
	}

	return &graphLayout{c: cv}
}

// layoutGraphView lays out the current component/section interconnection
// graph as a full ASCII node-link diagram, following exactly the same
// focus/exclude and live fuzzy search narrowing as the tree (see
// filteredForest and fuzzyVisible).
func (m Model) layoutGraphView() *graphLayout {
	g, highlights := m.graphToLayOut()
	return layoutGraphOf(g, highlights, m.treePlaceholder())
}

// graphToLayOut is the graph on show and its nodes' search highlights.
func (m Model) graphToLayOut() (*graph.Graph, map[string]graphNodeHighlight) {
	g := m.buildGraph()
	highlights := map[string]graphNodeHighlight{}
	if m.query != "" && m.mode == searchTraces {
		for key, n := range g.Nodes {
			if ok, h := matchGraphNode(m.query, n); ok {
				highlights[key] = h
			}
		}
	}
	return g, highlights
}

// layoutGraphOf lays g out, or shows placeholder if it is empty. It
// touches nothing but its arguments, so it can run in the background.
func layoutGraphOf(g *graph.Graph, highlights map[string]graphNodeHighlight, placeholder string) *graphLayout {
	if len(g.Nodes) == 0 {
		return &graphLayout{placeholder: placeholder}
	}
	return layoutGraph(g, highlights)
}
