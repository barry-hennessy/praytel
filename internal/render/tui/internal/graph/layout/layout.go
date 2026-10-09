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

// Node is a placed node. Row is the top row of its box.
type Node struct {
	Layer, Row int
}

// Edge is a placed edge. Waypoints lists the layers strictly between the
// edge's endpoints, ordered from its From node to its To node; an edge
// between adjacent layers has none. A Feedback edge is one that closes a
// cycle, so it runs against the layering (To sits in an earlier layer).
type Edge struct {
	Waypoints []Point
	Feedback  bool
}

// Placement is the full result. Layers and Rows are the grid's extent.
type Placement struct {
	Nodes  map[string]Node
	Edges  map[[2]string]Edge
	Layers int
	Rows   int
}

// Port returns the row an edge attaches to n on: the vertical middle of a
// box of the standard height, and the same distance from the top of a
// taller one, so that edges meet every box at its heading.
func (o Options) Port(n Node) int {
	return n.Row + o.NodeHeight/2
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
	if len(pairs) > 0 {
		layers, bendItems := buildLayers(g, pairs, forward, assignLayers(pairs), o)
		reduceCrossings(layers)
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
					p.Nodes[it.node] = Node{Layer: l, Row: row}
				}
			}
		}
		for pair, its := range bendItems {
			for _, it := range its {
				bends[pair] = append(bends[pair], Point{Layer: it.layer, Row: it.top - minTop})
			}
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
		p.Nodes[key] = Node{Layer: 0, Row: row}
		p.Rows = row + o.height(key)
		p.Layers = max(p.Layers, 1)
	}

	for ek := range g.Edges {
		fb := feedback[ek]
		way := slices.Clone(bends[dagPair(ek, fb)])
		if fb {
			slices.Reverse(way)
		}
		p.Edges[ek] = Edge{Waypoints: way, Feedback: fb}
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
	port   int     // offset from top of the row edges attach on
	weight float64 // how hard it holds its row against the layer's other items
	top    int

	left, right []*item // what it is connected to in the adjacent layers
}

func (it *item) portRow() int { return it.top + it.port }

// buildLayers lays the DAG out as items: a box per node, plus a bend in
// every layer an edge has to pass through on the way to a non-adjacent one,
// so that every connection is between neighboring layers. Each layer starts
// in the order its nodes were first seen, bends after boxes.
func buildLayers(g *graph.Graph, pairs [][2]string, forward map[[2]string]bool, layer map[string]int, o Options) (layers [][]*item, bends map[[2]string][]*item) {
	add := func(it *item) *item {
		for len(layers) <= it.layer {
			layers = append(layers, nil)
		}
		it.pos = len(layers[it.layer])
		layers[it.layer] = append(layers[it.layer], it)
		return it
	}
	nodes := map[string]*item{}
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
	return layers, bends
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
	want := map[*item]float64{}
	for _, it := range items {
		want[it] = float64(it.pos)
		if ns := neighbors(it); len(ns) > 0 {
			sum := 0
			for _, n := range ns {
				sum += n.pos
			}
			want[it] = float64(sum) / float64(len(ns))
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return want[items[i]] < want[items[j]] })
	renumber(items)
}

// untangle swaps adjacent items within a layer wherever that leaves their
// own edges crossing each other less, until no swap helps.
func untangle(layers [][]*item) {
	// crossings counts how often an edge of upper crosses one of lower,
	// given upper sits above lower.
	crossings := func(upper, lower *item) int {
		count := 0
		for _, side := range [][2][]*item{{upper.left, lower.left}, {upper.right, lower.right}} {
			for _, a := range side[0] {
				for _, b := range side[1] {
					if a.pos > b.pos {
						count++
					}
				}
			}
		}
		return count
	}
	for improved, rounds := true, 0; improved && rounds < 20; rounds++ {
		improved = false
		for _, items := range layers {
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

func countCrossings(layers [][]*item) int {
	count := 0
	for _, items := range layers {
		var ends [][2]int // each edge into this layer: position it leaves from, position it arrives at
		for _, it := range items {
			for _, n := range it.left {
				ends = append(ends, [2]int{n.pos, it.pos})
			}
		}
		for i, a := range ends {
			for _, b := range ends[i+1:] {
				if (a[0]-b[0])*(a[1]-b[1]) < 0 {
					count++
				}
			}
		}
	}
	return count
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
		settle(items, o, func(*item) []*item { return nil })
	}
	leftOf := func(it *item) []*item { return it.left }
	rightOf := func(it *item) []*item { return it.right }

	// The sweeps don't converge on a fixed point -- each undoes a little of
	// the one before -- so keep whichever state looked best on the way.
	var best []int
	bestCost := math.Inf(1)
	keepIfBest := func() {
		cost := 0.0
		for _, items := range layers {
			for _, it := range items {
				for _, n := range it.left {
					cost += kinkCost(it.portRow() - n.portRow())
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
			settle(layers[l], o, leftOf)
		}
		keepIfBest()
		for l := len(layers) - 2; l >= 0; l-- {
			settle(layers[l], o, rightOf)
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
		for _, side := range [][]*item{it.left, it.right} {
			for _, n := range side {
				sum += kinkCost(top + it.port - n.portRow())
			}
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
func settle(items []*item, o Options, neighbors func(*item) []*item) {
	type block struct {
		members []member
		value   float64
	}
	var blocks []block
	offsets := make([]int, len(items))
	for i, it := range items {
		if i > 0 {
			offsets[i] = offsets[i-1] + items[i-1].height + rowGap(items[i-1], it, o)
		}

		// An item with nothing to line up with stays where it is, but
		// gives way to anything that does have somewhere to be.
		m := member{target: float64(it.portRow()), weight: 0.001}
		if ns := neighbors(it); len(ns) > 0 {
			m = member{target: aim(ns, o), weight: it.weight}
		}
		m.target -= float64(it.port + offsets[i])

		blocks = append(blocks, block{members: []member{m}, value: m.target})
		for n := len(blocks); n > 1 && blocks[n-2].value > blocks[n-1].value; n-- {
			merged := append(blocks[n-2].members, blocks[n-1].members...)
			blocks = append(blocks[:n-2], block{members: merged, value: weightedMedian(merged)})
		}
	}

	i, floor := 0, math.MinInt
	for _, b := range blocks {
		// Box pitch is usually odd, so a fork can't be centered exactly on
		// its parent and something has to round. Rounding a lone item's
		// half up and a pooled block's half down makes the two agree: the
		// block settles half a row high, and the parent that then aims
		// between its middle items lands back where it was. Rounding both
		// the same way walks the pair a row further on every sweep.
		base := int(math.Floor(b.value + 0.5))
		if len(b.members) > 1 {
			base = int(math.Ceil(b.value - 0.5))
		}
		base = max(base, floor) // the two roundings must not reorder blocks
		floor = base
		for range b.members {
			items[i].top = base + offsets[i]
			i++
		}
	}
}

// aim is the port row an item would take if nothing were in its way: the
// median of its neighbors' port rows. With an even number of neighbors that
// is halfway between the middle two, a symmetric fork -- unless those two
// are too close for a fork to be more than a one-row wiggle on each side, in
// which case it lines up with whichever of them holds its row harder.
func aim(neighbors []*item, o Options) float64 {
	ns := slices.Clone(neighbors)
	sort.SliceStable(ns, func(i, j int) bool { return ns[i].portRow() < ns[j].portRow() })
	lo, hi := ns[(len(ns)-1)/2], ns[len(ns)/2]
	switch {
	case hi.portRow()-lo.portRow() >= o.NodeHeight:
		return float64(lo.portRow()+hi.portRow()) / 2
	case hi.weight > lo.weight:
		return float64(hi.portRow())
	}
	return float64(lo.portRow())
}

type member struct {
	target, weight float64
}

// weightedMedian returns the middle of the range of values that minimize
// the members' total weighted distance to their targets.
func weightedMedian(members []member) float64 {
	sorted := slices.Clone(members)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].target < sorted[j].target })
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
