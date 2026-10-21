// Package layout decides where a graph.Graph's nodes and edges go on a
// character grid: which layer (column) each node sits in, which row its box
// starts on, and which rows a multi-layer edge passes through on its way.
//
// It knows nothing about glyphs, colors, or column widths -- callers turn a
// Placement into cells.
//
// The method is the usual layered ("Sugiyama") one, in four steps: reverse
// enough edges to leave no cycles, give every node a layer so that all
// edges point the same way, order each layer to cross as few edges as
// possible, then pick rows so that connected things line up. The last step
// is the one that has to be exact on a coarse grid: a box is several rows
// tall, a passing edge is one, and an edge that ends up a single row off
// its target reads as a kink rather than as a line.
package layout

import (
	"math"
	"slices"
	"sort"

	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
)

// Options are the cell dimensions placement has to respect.
type Options struct {
	NodeHeight int            // rows a node's box occupies, unless Heights says otherwise
	Heights    map[string]int // taller boxes, by node key
	RowGap     int            // minimum blank rows around a box within its layer
}

func (o Options) height(key string) int {
	return max(o.NodeHeight, o.Heights[key])
}

// Point is one cell-grid position an edge passes through: the row its line
// runs along while crossing the given layer's column.
type Point struct {
	Layer, Row int
}

// Node is a placed node. Row is the top row of its box and Height the rows
// it spans, which is more than was asked for when the box needs extra rows
// to give each incoming edge one of its own.
type Node struct {
	Layer, Row, Height int
}

// Edge is a placed edge. Waypoints lists the layers strictly between the
// edge's endpoints, ordered from its From node to its To node; an edge
// between adjacent layers has none. A Feedback edge is one that closes a
// cycle, so it runs against the layering (To sits in an earlier layer).
//
// FromRow and ToRow are the rows it meets its two nodes on. Every edge
// leaving a node's right side shares one row, so they can fan out from a
// single trunk; every edge arriving at its left side gets a row to itself,
// so each ends in its own arrowhead and none can be mistaken for another.
type Edge struct {
	Waypoints      []Point
	Feedback       bool
	FromRow, ToRow int
}

// Placement is the full result. Layers and Rows are the grid's extent.
type Placement struct {
	Nodes  map[string]Node
	Edges  map[[2]string]Edge
	Layers int
	Rows   int
}

// Place lays g out.
func Place(g *graph.Graph, o Options) Placement {
	_, feedback := graph.DetectFeedbackEdges(g)

	// Everything below wants a DAG, so feedback edges are taken reversed. A
	// two-node cycle then collapses onto one pair, which both of its edges
	// share a route along.
	var pairs [][2]string
	seen := map[[2]string]bool{}
	forward := map[[2]string]bool{} // pairs some edge really runs along, not just back along
	connected := map[string]bool{}
	for _, ek := range graph.SortedEdgeKeys(g) {
		pair := dagPair(ek, feedback[ek])
		if !seen[pair] {
			seen[pair] = true
			pairs = append(pairs, pair)
		}
		if !feedback[ek] {
			forward[pair] = true
		}
		connected[ek[0]], connected[ek[1]] = true, true
	}

	p := Placement{Nodes: map[string]Node{}, Edges: map[[2]string]Edge{}}
	bends := map[[2]string][]Point{}
	ends := map[[2]string][2]int{} // the rows each pair leaves its first node and arrives at its second on
	if len(pairs) > 0 {
		layers, nodes, bendItems := buildLayers(g, pairs, forward, assignLayers(pairs), o)
		reduceCrossings(layers)
		assignPorts(layers, o)
		assignRows(layers, o)

		minTop := math.MaxInt
		for _, items := range layers {
			minTop = min(minTop, items[0].top)
		}
		for l, items := range layers {
			for _, it := range items {
				row := it.top - minTop
				p.Rows = max(p.Rows, row+it.height)
				if it.node != "" {
					p.Nodes[it.node] = Node{Layer: l, Row: row, Height: it.height}
				}
			}
		}
		for _, pair := range pairs {
			first, last := nodes[pair[0]], nodes[pair[1]]
			before := first
			for _, it := range bendItems[pair] {
				bends[pair] = append(bends[pair], Point{Layer: it.layer, Row: it.top - minTop})
				before = it
			}
			ends[pair] = [2]int{first.portRow() - minTop, last.arriveRow(before) - minTop}
		}
		p.Layers = len(layers)
	}

	// Nodes with no edges have nothing to line up with; stack them under
	// the first layer.
	for _, key := range graph.OrderedNodeKeys(g) {
		if connected[key] {
			continue
		}
		row := p.Rows
		if row > 0 {
			row += o.RowGap
		}
		p.Nodes[key] = Node{Layer: 0, Row: row, Height: o.height(key)}
		p.Rows = row + o.height(key)
		p.Layers = max(p.Layers, 1)
	}

	for ek := range g.Edges {
		fb := feedback[ek]
		way := slices.Clone(bends[dagPair(ek, fb)])
		rows := ends[dagPair(ek, fb)]
		if fb {
			slices.Reverse(way)
			rows[0], rows[1] = rows[1], rows[0]
		}
		p.Edges[ek] = Edge{Waypoints: way, Feedback: fb, FromRow: rows[0], ToRow: rows[1]}
	}
	return p
}

func dagPair(ek [2]string, feedback bool) [2]string {
	if feedback {
		return [2]string{ek[1], ek[0]}
	}
	return ek
}

// assignLayers gives every node in the DAG a layer such that each edge goes
// to a strictly later one.
//
// It starts from the longest-path layering (each node one past its deepest
// predecessor), which is as narrow as possible but drags every source to
// layer 0 however late it is first used, leaving long edges. So nodes are
// then pulled forward, last first, up against their nearest successor
// wherever that shortens more edges than it lengthens -- which a source
// always does.
func assignLayers(pairs [][2]string) map[string]int {
	succ, pred := map[string][]string{}, map[string][]string{}
	var keys []string
	note := func(key string) {
		if _, ok := succ[key]; !ok {
			succ[key] = nil
			keys = append(keys, key)
		}
	}
	for _, pair := range pairs {
		note(pair[0])
		note(pair[1])
		succ[pair[0]] = append(succ[pair[0]], pair[1])
		pred[pair[1]] = append(pred[pair[1]], pair[0])
	}

	// Topological order, first-seen order among nodes that are ready.
	waiting := map[string]int{}
	var order []string
	for _, key := range keys {
		waiting[key] = len(pred[key])
		if waiting[key] == 0 {
			order = append(order, key)
		}
	}
	layer := map[string]int{}
	for i := 0; i < len(order); i++ {
		key := order[i]
		for _, s := range succ[key] {
			layer[s] = max(layer[s], layer[key]+1)
			if waiting[s]--; waiting[s] == 0 {
				order = append(order, s)
			}
		}
	}

	for _, key := range slices.Backward(order) {
		if len(succ[key]) <= len(pred[key]) {
			continue
		}
		nearest := math.MaxInt
		for _, s := range succ[key] {
			nearest = min(nearest, layer[s])
		}
		layer[key] = nearest - 1
	}

	// Pulling sources forward can leave a disconnected part of the graph
	// starting past layer 0; shift each part back to the start.
	group := map[string]string{}
	var find func(key string) string
	find = func(key string) string {
		if group[key] == "" || group[key] == key {
			return key
		}
		group[key] = find(group[key])
		return group[key]
	}
	for _, pair := range pairs {
		group[find(pair[0])] = find(pair[1])
	}
	first := map[string]int{}
	for _, key := range keys {
		root := find(key)
		if l, ok := first[root]; !ok || layer[key] < l {
			first[root] = layer[key]
		}
	}
	for _, key := range keys {
		layer[key] -= first[find(key)]
	}
	return layer
}

// item is one thing occupying rows in a layer: a node's box, or the single
// row where a long edge passes through.
type item struct {
	node  string // "" for a bend
	layer int
	pos   int // index within the layer

	height int     // rows occupied
	port   int     // offset from top of the row edges leave on
	weight float64 // how hard it holds its row against the layer's other items
	top    int

	left, right []*item       // what it is connected to in the adjacent layers
	arrive      map[*item]int // offset from top of the row the edge from each of left arrives on; a bend has none, and takes them all on its one row

	leftPos, rightPos []int // untangle's scratch space: left's and right's positions, in order
}

// portRow is the row every edge to it.right leaves on.
func (it *item) portRow() int { return it.top + it.port }

// arriveRow is the row the edge from the given one of it.left arrives on.
func (it *item) arriveRow(from *item) int { return it.top + it.arrive[from] }

// level returns the port row it would need for its connection to n, one of
// its left neighbors if fromLeft and of its right neighbors otherwise, to
// run level.
func (it *item) level(n *item, fromLeft bool) int {
	if fromLeft {
		return n.portRow() - it.arrive[n] + it.port
	}
	return n.arriveRow(it)
}

// buildLayers lays the DAG out as items: a box per node, plus a bend in
// every layer an edge has to pass through on the way to a non-adjacent one,
// so that every connection is between neighboring layers. Each layer starts
// in the order its nodes were first seen, bends after boxes.
func buildLayers(g *graph.Graph, pairs [][2]string, forward map[[2]string]bool, layer map[string]int, o Options) (layers [][]*item, nodes map[string]*item, bends map[[2]string][]*item) {
	add := func(it *item) *item {
		for len(layers) <= it.layer {
			layers = append(layers, nil)
		}
		it.pos = len(layers[it.layer])
		layers[it.layer] = append(layers[it.layer], it)
		return it
	}
	nodes = map[string]*item{}
	for _, key := range graph.OrderedNodeKeys(g) {
		if l, ok := layer[key]; ok {
			nodes[key] = add(&item{node: key, layer: l, height: o.height(key), port: o.NodeHeight / 2, weight: boxWeight})
		}
	}
	bends = map[[2]string][]*item{}
	for _, pair := range pairs {
		prev, last := nodes[pair[0]], nodes[pair[1]]
		link := func(next *item) {
			prev.right = append(prev.right, next)
			next.left = append(next.left, prev)
			prev = next
		}
		weight := bendWeight
		if !forward[pair] {
			weight = feedbackBendWeight
		}
		for l := prev.layer + 1; l < last.layer; l++ {
			bend := add(&item{layer: l, height: 1, weight: weight})
			bends[pair] = append(bends[pair], bend)
			link(bend)
		}
		link(last)
	}
	return layers, nodes, bends
}

// reduceCrossings reorders each layer to cross as few edges as it can find.
//
// Finding the true minimum is NP-hard, so this is the standard heuristic:
// sweep across the layers sorting each by the average position of its
// neighbors in the layer just done, alternating direction, and after each
// sweep swap any adjacent pair whose swap removes crossings. The sweeps
// don't improve monotonically, so the best order seen is the one kept.
func reduceCrossings(layers [][]*item) {
	leftOf := func(it *item) []*item { return it.left }
	rightOf := func(it *item) []*item { return it.right }

	var best [][]*item
	bestCount := math.MaxInt
	keepIfBest := func() {
		if count := countCrossings(layers); count < bestCount {
			bestCount = count
			best = best[:0]
			for _, items := range layers {
				best = append(best, slices.Clone(items))
			}
		}
	}
	keepIfBest()
	for sweep := 0; sweep < 24 && bestCount > 0; sweep++ {
		if sweep%2 == 0 {
			for l := 1; l < len(layers); l++ {
				sortByNeighbors(layers[l], leftOf)
			}
		} else {
			for l := len(layers) - 2; l >= 0; l-- {
				sortByNeighbors(layers[l], rightOf)
			}
		}
		untangle(layers)
		keepIfBest()
	}
	for l, items := range best {
		copy(layers[l], items)
		renumber(layers[l])
	}
}

func renumber(items []*item) {
	for i, it := range items {
		it.pos = i
	}
}

// sortByNeighbors orders a layer by the mean position of each item's
// neighbors. An item with none has no opinion, so it holds its place.
func sortByNeighbors(items []*item, neighbors func(*item) []*item) {
	type wanted struct {
		it   *item
		want float64
	}
	order := make([]wanted, len(items))
	for i, it := range items {
		order[i] = wanted{it, float64(it.pos)}
		if ns := neighbors(it); len(ns) > 0 {
			sum := 0
			for _, n := range ns {
				sum += n.pos
			}
			order[i].want = float64(sum) / float64(len(ns))
		}
	}
	slices.SortStableFunc(order, func(a, b wanted) int {
		switch {
		case a.want < b.want:
			return -1
		case a.want > b.want:
			return 1
		}
		return 0
	})
	for i, o := range order {
		items[i] = o.it
	}
	renumber(items)
}

// untangle swaps adjacent items within a layer wherever that leaves their
// own edges crossing each other less, until no swap helps.
func untangle(layers [][]*item) {
	// Swaps only move items within the layer being worked on, so while it
	// is, every neighbor's position holds still and can be sorted once.
	positions := func(ns []*item, buf []int) []int {
		buf = buf[:0]
		for _, n := range ns {
			buf = append(buf, n.pos)
		}
		if len(buf) > 1 {
			slices.Sort(buf)
		}
		return buf
	}
	// above counts the pairs of a from as and b from bs, both sorted, where
	// a > b: how often an edge to as crosses one to bs, given as's item sits
	// above bs's.
	above := func(as, bs []int) int {
		count, j := 0, 0
		for _, a := range as {
			for j < len(bs) && bs[j] < a {
				j++
			}
			count += j
		}
		return count
	}
	crossings := func(upper, lower *item) int {
		return above(upper.leftPos, lower.leftPos) + above(upper.rightPos, lower.rightPos)
	}
	for improved, rounds := true, 0; improved && rounds < 20; rounds++ {
		improved = false
		for _, items := range layers {
			for _, it := range items {
				it.leftPos = positions(it.left, it.leftPos)
				it.rightPos = positions(it.right, it.rightPos)
			}
			for i := 0; i+1 < len(items); i++ {
				if crossings(items[i+1], items[i]) < crossings(items[i], items[i+1]) {
					items[i], items[i+1] = items[i+1], items[i]
					items[i].pos, items[i+1].pos = i, i+1
					improved = true
				}
			}
		}
	}
}

// countCrossings counts the pairs of edges that cross, between every pair
// of neighboring layers.
func countCrossings(layers [][]*item) int {
	count := 0
	var ends [][2]int // each edge into a layer: position it leaves from, position it arrives at
	var tree []int    // a Fenwick tree over arrival positions
	for _, items := range layers {
		ends = ends[:0]
		for _, it := range items {
			for _, n := range it.left {
				ends = append(ends, [2]int{n.pos, it.pos})
			}
		}
		// Two edges cross when one leaves strictly before the other and
		// arrives strictly after it. Going through them by where they
		// leave, each crosses every edge already seen -- from an earlier
		// position -- that arrives later than it does.
		slices.SortFunc(ends, func(a, b [2]int) int { return a[0] - b[0] })
		tree = slices.Grow(tree[:0], len(items)+1)[:len(items)+1]
		clear(tree)
		seen := 0
		for i := 0; i < len(ends); {
			j := i
			for j < len(ends) && ends[j][0] == ends[i][0] {
				atOrBefore := 0
				for k := ends[j][1] + 1; k > 0; k -= k & -k {
					atOrBefore += tree[k]
				}
				count += seen - atOrBefore
				j++
			}
			for ; i < j; i++ {
				for k := ends[i][1] + 1; k < len(tree); k += k & -k {
					tree[k]++
				}
				seen++
			}
		}
	}
	return count
}

// assignPorts decides, now that each layer's order is settled, where on its
// box each node's edges attach.
//
// Incoming edges take consecutive rows, in the order their sources sit in
// the layer before, so they arrive without crossing. Outgoing edges all
// leave from the middle one of those rows, which is also the row the middle
// incoming edge arrives on: a node with one way in and one way out, or
// lined up with the middle of a fan-in, still gets a straight line through
// it. The box is made taller if it hasn't a row for each.
func assignPorts(layers [][]*item, o Options) {
	for _, items := range layers {
		for _, it := range items {
			if it.node == "" {
				continue
			}
			above := max(len(it.left)-1, 0) / 2 // incoming edges arriving above the port row
			it.port = max(o.NodeHeight/2, 1+above)
			from := slices.Clone(it.left)
			sort.SliceStable(from, func(i, j int) bool { return from[i].pos < from[j].pos })
			it.arrive = map[*item]int{}
			for i, n := range from {
				it.arrive[n] = it.port - above + i
			}
			it.height = max(it.height, it.port-above+len(from)+1)
		}
	}
}

// How hard each kind of item holds its row when several compete for it. A
// box moving a row costs a kink at its own edges; a bend moving costs a kink
// in the middle of a long edge that would otherwise run straight past,
// which is the worse look, so bends win. Except the bends of an edge that
// only closes a cycle: the calls the graph is mostly about run forward, and
// those should get the straight lines.
const (
	boxWeight          = 1.0
	bendWeight         = 4.0
	feedbackBendWeight = 0.5
)

// assignRows gives every item a top row, keeping each layer's order.
//
// It is the classic sweep: walk the layers left to right, pulling each item
// level with the median of what it connects to in the layer before, then
// right to left against the layer after, and repeat. The median rather than
// the mean is what makes edges come out dead straight: an item with an odd
// number of neighbors lands exactly on one of them.
func assignRows(layers [][]*item, o Options) {
	for _, items := range layers {
		settle(items, o, againstNothing)
	}

	// The sweeps don't converge on a fixed point -- each undoes a little of
	// the one before -- so keep whichever state looked best on the way.
	var best []int
	bestCost := math.Inf(1)
	keepIfBest := func() {
		cost := 0.0
		for _, items := range layers {
			for _, it := range items {
				for _, n := range it.left {
					cost += kinkCost(it.arriveRow(n) - n.portRow())
				}
			}
		}
		if cost < bestCost {
			bestCost = cost
			best = best[:0]
			for _, items := range layers {
				for _, it := range items {
					best = append(best, it.top)
				}
			}
		}
	}
	for range 6 {
		for l := 1; l < len(layers); l++ {
			settle(layers[l], o, againstLeft)
		}
		keepIfBest()
		for l := len(layers) - 2; l >= 0; l-- {
			settle(layers[l], o, againstRight)
		}
		keepIfBest()
	}
	i := 0
	for _, items := range layers {
		for _, it := range items {
			it.top = best[i]
			i++
		}
	}
	polish(layers, o)
}

// polish nudges single items a row or two wherever there is room and it
// leaves their own edges straighter. The sweeps place whole layers at a
// time against one neighboring layer; this catches what that misses, mostly
// an item left one row off a neighbor on its other side.
func polish(layers [][]*item, o Options) {
	cost := func(it *item, top int) float64 {
		sum := 0.0
		for _, n := range it.left {
			sum += kinkCost(top + it.port - it.level(n, true))
		}
		for _, n := range it.right {
			sum += kinkCost(top + it.port - it.level(n, false))
		}
		return sum
	}
	for improved, rounds := true, 0; improved && rounds < 10; rounds++ {
		improved = false
		for _, items := range layers {
			for i, it := range items {
				lo, hi := math.MinInt, math.MaxInt
				if i > 0 {
					lo = items[i-1].top + items[i-1].height + rowGap(items[i-1], it, o)
				}
				if i+1 < len(items) {
					hi = items[i+1].top - it.height - rowGap(it, items[i+1], o)
				}
				for _, step := range []int{-1, 1, -2, 2} {
					top := it.top + step
					if top >= lo && top <= hi && cost(it, top) < cost(it, it.top) {
						it.top = top
						improved = true
						break
					}
				}
			}
		}
	}
}

// rowGap is the blank rows required between two vertically adjacent items:
// none between two passing edges, RowGap wherever a box is involved.
func rowGap(upper, lower *item, o Options) int {
	if upper.node == "" && lower.node == "" {
		return 0
	}
	return o.RowGap
}

// kinkCost scores one edge segment by how many rows it has to climb between
// adjacent layers. Level is free. A single row is the worst case -- two
// corners with nothing between them, which reads as a glitch -- and beyond
// that a bend is a bend, only slightly worse for being longer.
func kinkCost(rows int) float64 {
	switch rows = max(rows, -rows); rows {
	case 0:
		return 0
	case 1:
		return 4
	}
	return 2 + float64(rows)/16
}

// settle re-rows one layer's items, in their fixed order, so that each sits
// as close as it can to where its neighbors want it -- as close as the
// layer's other items allow, since a box needs RowGap around it.
//
// Subtracting from each item the rows the items above it must take up turns
// "in order, not overlapping" into simply "non-decreasing", and the closest
// non-decreasing sequence to a set of targets is isotonic regression,
// solved exactly by pooling adjacent violators: whenever an item would have
// to go above its predecessor, the two are merged into a block that moves
// as one. A block goes to the weighted median of its members' targets, not
// their mean, so that some member lands exactly on its target and keeps a
// straight edge, rather than every member missing by a little.
func settle(items []*item, o Options, against side) {
	// Blocks are consecutive runs of members, each run in order of target
	// rather than of item, so a merge is a linear one rather than a sort.
	// Which item a member was doesn't matter: a block moves as one.
	type block struct {
		start int // its first member; it runs to the next block's start
		value float64
	}
	var blocks []block
	members := make([]member, 0, len(items))
	var scratch []member
	offsets := make([]int, len(items))
	for i, it := range items {
		if i > 0 {
			offsets[i] = offsets[i-1] + items[i-1].height + rowGap(items[i-1], it, o)
		}

		// An item with nothing to line up with stays where it is, but
		// gives way to anything that does have somewhere to be.
		m := member{target: float64(it.portRow()), weight: 0.001}
		if ns := against.of(it); len(ns) > 0 {
			m = member{target: aim(it, ns, against == againstLeft, o), weight: it.weight}
		}
		m.target -= float64(it.port + offsets[i])

		members = append(members, m)
		blocks = append(blocks, block{start: len(members) - 1, value: m.target})
		for n := len(blocks); n > 1 && blocks[n-2].value > blocks[n-1].value; n-- {
			start := blocks[n-2].start
			scratch = mergeRuns(members[start:], blocks[n-1].start-start, scratch)
			blocks = append(blocks[:n-2], block{start: start, value: weightedMedian(members[start:])})
		}
	}

	i, floor := 0, math.MinInt
	for k, b := range blocks {
		size := len(members) - b.start
		if k+1 < len(blocks) {
			size = blocks[k+1].start - b.start
		}
		// Box pitch is usually odd, so a fork can't be centered exactly on
		// its parent and something has to round. Rounding a lone item's
		// half up and a pooled block's half down makes the two agree: the
		// block settles half a row high, and the parent that then aims
		// between its middle items lands back where it was. Rounding both
		// the same way walks the pair a row further on every sweep.
		base := int(math.Floor(b.value + 0.5))
		if size > 1 {
			base = int(math.Ceil(b.value - 0.5))
		}
		base = max(base, floor) // the two roundings must not reorder blocks
		floor = base
		for range size {
			items[i].top = base + offsets[i]
			i++
		}
	}
}

// side says which of a layer's neighboring layers it is being settled
// against.
type side int

const (
	againstNothing side = iota
	againstLeft
	againstRight
)

func (s side) of(it *item) []*item {
	switch s {
	case againstLeft:
		return it.left
	case againstRight:
		return it.right
	}
	return nil
}

// aim is the port row an item would take if nothing were in its way: the
// median of the rows that would make each of its connections level. With an
// even number of neighbors that is halfway between the middle two, a
// symmetric fork -- unless those two are too close for a fork to be more
// than a one-row wiggle on each side, in which case it lines up with
// whichever of them holds its row harder.
func aim(it *item, neighbors []*item, fromLeft bool, o Options) float64 {
	ns := slices.Clone(neighbors)
	row := func(n *item) int { return it.level(n, fromLeft) }
	sort.SliceStable(ns, func(i, j int) bool { return row(ns[i]) < row(ns[j]) })
	lo, hi := ns[(len(ns)-1)/2], ns[len(ns)/2]
	switch {
	case row(hi)-row(lo) >= o.NodeHeight:
		return float64(row(lo)+row(hi)) / 2
	case hi.weight > lo.weight:
		return float64(row(hi))
	}
	return float64(row(lo))
}

type member struct {
	target, weight float64
}

// mergeRuns puts members, two runs split at split each in order of target,
// in order of target as a whole. It returns scratch, the space it used,
// for next time.
func mergeRuns(members []member, split int, scratch []member) []member {
	scratch = append(scratch[:0], members[:split]...)
	a, b := scratch, members[split:]
	out := members[:0]
	for len(a) > 0 && len(b) > 0 {
		// out never catches up with b: it is len(a) behind.
		if b[0].target < a[0].target {
			out, b = append(out, b[0]), b[1:]
		} else {
			out, a = append(out, a[0]), a[1:]
		}
	}
	copy(members[len(out):], a) // whatever is left of b is already in place
	return scratch
}

// weightedMedian returns the middle of the range of values that minimize
// the members' total weighted distance to their targets. sorted is in order
// of target.
func weightedMedian(sorted []member) float64 {
	half := 0.0
	for _, m := range sorted {
		half += m.weight / 2
	}
	lower, found := 0.0, false
	sum := 0.0
	for _, m := range sorted {
		sum += m.weight
		if !found && sum >= half {
			lower, found = m.target, true
		}
		if sum > half {
			return (lower + m.target) / 2
		}
	}
	return lower
}
