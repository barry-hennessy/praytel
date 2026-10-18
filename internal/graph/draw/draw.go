// Package draw turns a graph.Graph into a box-and-arrow picture on a grid
// of cells: layers become columns, layout.Place decides each node's row,
// and calls are routed through the gaps between columns. It knows nothing
// about colors or terminals -- callers say how each node and edge should
// look, as an opaque Style plus a border or line kind, and turn the styled
// cells into text themselves (see Canvas.Lines).
package draw

import (
	"strings"
	"unicode/utf8"

	"github.com/barry-hennessy/praytel/internal/graph"
	"github.com/barry-hennessy/praytel/internal/graph/layout"
)

// Style is a caller-defined cell style. The first few are the drawing's
// own; callers number theirs from Custom up. Where lines of different
// styles share a cell, the higher one wins, so callers order theirs by
// precedence.
type Style uint8

const (
	Default    Style = iota
	Label            // a node's label
	LabelMatch       // the runes of a label picked out by NodeLook.LabelMask
	Footer           // a node's footer and an edge's label
	Crossing         // two unrelated lines passing through each other
	Custom           // the first caller-defined style
)

// Line is how an edge's line is drawn along its straight runs. Bends and
// junctions are drawn solid unless every line through them is Double.
type Line uint8

const (
	Solid  Line = iota
	Dashed      // ╌ ╎
	Double      // ═ ║
	Dotted      // ┈ ┊
)

// Border is how a node's box is drawn.
type Border uint8

const (
	Rounded      Border = iota // ╭─╮
	DoubleBorder               // ╔═╗
	DottedBorder               // ┌┈┐
)

// NodeLook is how one node is drawn.
type NodeLook struct {
	Style     Style
	Border    Border
	Label     string
	LabelMask []bool // runes of Label drawn in LabelMatch
	Footer    string
}

// EdgeLook is how one edge is drawn. Head replaces its arrowhead; zero
// means the usual ▶ (or ◀ for an edge drawn right to left).
type EdgeLook struct {
	Style Style
	Line  Line
	Label string
	Head  rune
}

// Looks says how every node and edge should look. Edge is told whether
// the edge closes a cycle (is drawn right to left, against the layering).
type Looks struct {
	Node func(n *graph.Node) NodeLook
	Edge func(e *graph.Edge, feedback bool) EdgeLook
}

// Layout constants. Sizes are deliberately generous rather than tightly
// packed: callers scroll or print the whole picture, so there's no need to
// fit everything inside a terminal.
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

// TruncateLabel shortens s to max runes, ending in … when it had to cut.
func TruncateLabel(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func nodeBoxWidth(look NodeLook) int {
	inner := max(minNodeInnerWidth, utf8.RuneCountInString(look.Label), utf8.RuneCountInString(look.Footer))
	return inner + 4 // "│ " + inner + " │"
}

type gridCell struct {
	r        rune
	style    Style
	lineDirs uint8 // 0 for non-line cells (text, borders, arrows); otherwise the OR of the dirXxx bits currently occupying this cell
	bend     bool  // some line turns or ends here, as opposed to every line passing straight through
	line     Line  // the kind every line here shares, or Solid where they differ
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

var doubleGlyphs = map[uint8]rune{
	dirNorth | dirSouth:                     '║',
	dirEast | dirWest:                       '═',
	dirNorth | dirEast:                      '╚',
	dirNorth | dirWest:                      '╝',
	dirSouth | dirEast:                      '╔',
	dirSouth | dirWest:                      '╗',
	dirNorth | dirSouth | dirEast:           '╠',
	dirNorth | dirSouth | dirWest:           '╣',
	dirEast | dirWest | dirSouth:            '╦',
	dirEast | dirWest | dirNorth:            '╩',
	dirNorth | dirSouth | dirEast | dirWest: '╬',
}

// straightGlyphs are the glyphs of a Dashed or Dotted line's straight runs,
// horizontal then vertical.
var straightGlyphs = map[Line][2]rune{
	Dashed: {'╌', '╎'},
	Dotted: {'┈', '┊'},
}

// glyph resolves what a cell shows. Two things can't be decided until every
// line is down: a four-way cell that nothing bends in is two unrelated
// lines crossing, drawn dim so each reads as continuing straight through
// rather than as a junction; and a line's kind only shows where every line
// through the cell shares it.
func (g gridCell) glyph() (rune, Style) {
	switch {
	case g.lineDirs == 0:
		return g.r, g.style
	case g.lineDirs == dirNorth|dirSouth|dirEast|dirWest && !g.bend:
		if g.line == Double {
			return '╬', Crossing
		}
		return '┼', Crossing
	case g.line == Double:
		return doubleGlyphs[g.lineDirs], g.style
	}
	if s, ok := straightGlyphs[g.line]; ok {
		switch g.lineDirs {
		case dirEast | dirWest:
			return s[0], g.style
		case dirNorth | dirSouth:
			return s[1], g.style
		}
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
			row[j] = gridCell{r: ' ', style: Default}
		}
		cells[i] = row
	}
	return &canvas{cells: cells, w: w, h: h}
}

func (c *canvas) set(row, col int, r rune, style Style) {
	if row < 0 || row >= c.h || col < 0 || col >= c.w {
		return
	}
	c.cells[row][col] = gridCell{r: r, style: style}
}

// addLine merges dirs into whatever line directions already occupy (row,
// col), so two edges whose lines cross or merge at the same cell produce a
// proper junction (a T or a cross) rather than one edge's draw silently
// erasing the other's. A cell keeps a line kind only while every line
// through it has it.
func (c *canvas) addLine(row, col int, dirs uint8, style Style, bend bool, line Line) {
	if row < 0 || row >= c.h || col < 0 || col >= c.w {
		return
	}
	cell := &c.cells[row][col]
	if cell.lineDirs != 0 {
		style = max(style, cell.style)
		if line != cell.line {
			line = Solid
		}
	}
	*cell = gridCell{style: style, lineDirs: cell.lineDirs | dirs, bend: cell.bend || bend, line: line, lines: cell.lines + 1}
}

type cellPos struct {
	row, col int
}

// trace draws one continuous line through pts, each consecutive pair of
// which shares a row or a column. Both end cells are drawn as full line
// segments rather than half stubs, since a line ends against a box or under
// an arrowhead.
func (c *canvas) trace(pts []cellPos, style Style, line Line) {
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
		c.addLine(p.row, p.col, dirs, style, !straight, line)
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

func (c *canvas) writeText(row, col int, text string, style Style) {
	for i, r := range []rune(text) {
		c.set(row, col+i, r, style)
	}
}

func (c *canvas) writeTextMasked(row, col int, text string, mask []bool, base, hl Style) {
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
	c.writeText(row, col, text, Footer)
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
				c.writeText(row, best[0].col+1, text, Footer)
				return
			}
		}
	}
}

// borderGlyphs are a box's corners (top left, top right, bottom left,
// bottom right), then its horizontal and vertical sides.
var borderGlyphs = map[Border][6]rune{
	Rounded:      {'╭', '╮', '╰', '╯', '─', '│'},
	DoubleBorder: {'╔', '╗', '╚', '╝', '═', '║'},
	DottedBorder: {'┌', '┐', '└', '┘', '┈', '┊'},
}

func (c *canvas) drawBox(top, left, width, height int, look NodeLook) {
	g := borderGlyphs[look.Border]
	right := left + width - 1
	bottom := top + height - 1
	c.set(top, left, g[0], look.Style)
	c.set(top, right, g[1], look.Style)
	c.set(bottom, left, g[2], look.Style)
	c.set(bottom, right, g[3], look.Style)
	for x := left + 1; x < right; x++ {
		c.set(top, x, g[4], look.Style)
		c.set(bottom, x, g[4], look.Style)
	}
	for y := top + 1; y < bottom; y++ {
		c.set(y, left, g[5], look.Style)
		c.set(y, right, g[5], look.Style)
	}

	inner := width - 4
	col := left + 2
	label := TruncateLabel(look.Label, inner)
	c.writeTextMasked(top+1, col, label, look.LabelMask, Label, LabelMatch)
	c.writeText(top+2, col, TruncateLabel(look.Footer, inner), Footer)
}

// Canvas is a drawn graph: a grid of styled cells.
type Canvas struct {
	c *canvas
}

// Cell is one drawn cell as it will be shown.
type Cell struct {
	Rune  rune
	Style Style
	Line  bool // part of an edge's line, rather than text, a border or an arrowhead
}

// Cells returns the drawn grid, row by row.
func (cv *Canvas) Cells() [][]Cell {
	out := make([][]Cell, len(cv.c.cells))
	for i, row := range cv.c.cells {
		out[i] = make([]Cell, len(row))
		for j, cell := range row {
			r, style := cell.glyph()
			out[i][j] = Cell{Rune: r, Style: style, Line: cell.lineDirs != 0}
		}
	}
	return out
}

// Lines renders the canvas one row at a time, handing each run of
// equally styled cells to render. A nil render returns the plain text.
func (cv *Canvas) Lines(render func(Style, string) string) []string {
	c := cv.c
	if render == nil {
		render = func(_ Style, s string) string { return s }
	}
	out := make([]string, c.h)
	for i, row := range c.cells {
		var b strings.Builder
		var seg []rune
		segStyle := Default
		flush := func() {
			if len(seg) > 0 {
				b.WriteString(render(segStyle, string(seg)))
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
	first  int       // leftmost layer
	rows   []int     // the row the line holds while crossing each layer, from first onward
	tracks []int     // per hop to the next layer: which of the gap's vertical tracks it turns on (unused where the line stays level)
	style  Style     // the forward edge's style where the line carries one, else the backward edge's
	line   Line      // likewise
	pts    []cellPos // the cells the line turns at, once drawn

	forward, backward *graph.Edge // the edge pointing right and the one pointing left along the line; either may be nil
	fwd, back         EdgeLook
}

// buildRoutes turns placed edges into routes, in the graph's deterministic
// edge order.
func buildRoutes(g *graph.Graph, p layout.Placement, looks Looks) []*route {
	byPair := map[[2]string]*route{}
	var routes []*route
	for _, ek := range graph.SortedEdgeKeys(g) {
		placed := p.Edges[ek]
		left, right := ek[0], ek[1]
		if placed.Feedback {
			left, right = right, left
		}
		look := looks.Edge(g.Edges[ek], placed.Feedback)
		r, ok := byPair[[2]string{left, right}]
		if !ok {
			r = &route{first: p.Nodes[left].Layer, style: look.Style, line: look.Line}
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
			r.backward, r.back = g.Edges[ek], look
		} else {
			r.forward, r.fwd = g.Edges[ek], look
			r.style, r.line = look.Style, look.Line
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

// Draw lays g out left-to-right: layers become columns, and layout.Place
// decides each node's row. It builds the full-size canvas at whatever
// width/height the graph naturally needs.
func Draw(g *graph.Graph, looks Looks) *Canvas {
	p := layout.Place(g, placeOptions)
	routes := buildRoutes(g, p, looks)
	nodeLooks := make(map[string]NodeLook, len(g.Nodes))
	for key, n := range g.Nodes {
		nodeLooks[key] = looks.Node(n)
	}

	// A gap is the space between layer i and layer i+1, where every bend
	// lives: lines only ever cross a column level, on a row no box in that
	// column occupies.
	type gap struct {
		hops                 []hop
		tracks               int
		leadLabel, tailLabel int // widest label sitting in the lead and in the tail
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
		head.leadLabel = max(head.leadLabel, utf8.RuneCountInString(r.back.Label))
		tail.tailLabel = max(tail.tailLabel, utf8.RuneCountInString(r.fwd.Label))
	}

	colWidth := make([]int, p.Layers)
	for i := range colWidth {
		colWidth[i] = minColumnWidth
	}
	for key, n := range p.Nodes {
		colWidth[n.Layer] = max(colWidth[n.Layer], nodeBoxWidth(nodeLooks[key]))
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
		cv.trace(pts, r.style, r.line)
		r.pts = pts
	}

	// Labels need every line down first, to know which stretches are one
	// edge's alone; arrowheads go on after that so they can't be mistaken
	// for line. Each label sits as near its own arrowhead as it can.
	type pending struct {
		pts   []cellPos
		label string
	}
	var beside []pending
	for _, r := range routes {
		if r.forward != nil && r.fwd.Label != "" && !cv.labelLine(r.pts, r.fwd.Label, true) {
			beside = append(beside, pending{r.pts, r.fwd.Label})
		}
		if r.backward != nil && r.back.Label != "" && !cv.labelLine(r.pts, r.back.Label, false) {
			beside = append(beside, pending{r.pts, r.back.Label})
		}
	}
	for _, l := range beside {
		cv.labelBeside(l.pts, l.label)
	}
	for _, r := range routes {
		last := len(r.rows) - 1
		if r.forward != nil {
			cv.set(topMargin+r.rows[last], gaps[r.first+last-1].end, orRune(r.fwd.Head, '▶'), r.fwd.Style)
		}
		if r.backward != nil {
			cv.set(topMargin+r.rows[0], gaps[r.first].start, orRune(r.back.Head, '◀'), r.back.Style)
		}
	}

	for _, key := range graph.OrderedNodeKeys(g) {
		n := p.Nodes[key]
		cv.drawBox(topMargin+n.Row, colX[n.Layer], colWidth[n.Layer], n.Height, nodeLooks[key])
	}

	return &Canvas{c: cv}
}

func orRune(r, fallback rune) rune {
	if r == 0 {
		return fallback
	}
	return r
}
