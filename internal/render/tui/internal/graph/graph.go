// Package graph builds and lays out the TUI's component/section
// interconnection graph: one node per distinct hierarchy container, one
// aggregated edge per pair of containers connected by an observed
// parent/child span call. It knows nothing about Model, live filtering, or
// rendering styles -- callers hand it already-visible span facts and get
// back a graph plus layering/layout primitives.
package graph

import (
	"sort"
	"strings"
)

// HierarchyPath splits a span's component and name into a container
// hierarchy -- Component, then nested "section" segments -- plus a
// trailing "function" leaf segment. Component.section.section...function
// is the convention this parses; e.g. component="Foo", name="Bar.Qux.Baz"
// yields containerPath=["Foo","Bar","Qux"], function="Baz".
//
// A span with no component and no dots in its name has nothing to derive a
// container from except the name itself, so that name becomes the
// container (a node of its own, no separate function) rather than a
// function with no container -- which would otherwise render as a
// blank-labeled node.
func HierarchyPath(component, name string) (containerPath []string, function string) {
	if component != "" {
		containerPath = append(containerPath, component)
		if name == component {
			return containerPath, ""
		}
		if strings.HasPrefix(name, component+".") {
			name = name[len(component)+1:]
		}
	}
	if name == "" {
		return containerPath, ""
	}
	segments := strings.Split(name, ".")
	if len(segments) == 1 {
		if component == "" {
			return []string{segments[0]}, ""
		}
		return containerPath, segments[0]
	}
	containerPath = append(containerPath, segments[:len(segments)-1]...)
	return containerPath, segments[len(segments)-1]
}

// ContainerKey returns the stable map key for a container path.
func ContainerKey(path []string) string {
	return strings.Join(path, "\x1f")
}

// Node is one container in the interconnection graph: a distinct
// (Component, section...) path. The leaf function segment of any
// contributing span is not itself promoted to a node (that would make the
// graph "call graph of every method" scale); it is tallied in Functions
// instead so the detail isn't lost, just not given node status.
type Node struct {
	Key       string
	Path      []string
	SpanCount int
	Functions map[string]int
	Order     int // min contributing span order, for deterministic layout
}

// Edge is a directed, aggregated call from one container to another --
// built whenever a visible span's parent lives in a different container,
// whether that crosses a Component (service) boundary or just a section
// boundary within one Component.
type Edge struct {
	FromKey, ToKey string
	Count          int
	Order          int
}

// Graph is a set of containers and the aggregated calls between them.
type Graph struct {
	Nodes map[string]*Node
	Edges map[[2]string]*Edge
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{Nodes: map[string]*Node{}, Edges: map[[2]string]*Edge{}}
}

// EnsureNode returns the node at path, creating it if necessary, and lowers
// its recorded order if this contributing span was observed earlier.
func (g *Graph) EnsureNode(path []string, order int) *Node {
	key := ContainerKey(path)
	n, ok := g.Nodes[key]
	if !ok {
		n = &Node{Key: key, Path: append([]string(nil), path...), Functions: map[string]int{}, Order: order}
		g.Nodes[key] = n
		return n
	}
	if order < n.Order {
		n.Order = order
	}
	return n
}

// EnsureEdge returns the edge from fromKey to toKey, creating it if
// necessary, and lowers its recorded order if this contributing call was
// observed earlier.
func (g *Graph) EnsureEdge(fromKey, toKey string, order int) *Edge {
	k := [2]string{fromKey, toKey}
	e, ok := g.Edges[k]
	if !ok {
		e = &Edge{FromKey: fromKey, ToKey: toKey, Order: order}
		g.Edges[k] = e
		return e
	}
	if order < e.Order {
		e.Order = order
	}
	return e
}

// SpanFact is one already-visible span's contribution to the graph: its own
// hierarchy-determining fields, plus its parent's, when the parent is also
// visible. HasParent false means the span has no visible parent (root, or
// its parent was filtered out) and contributes a node but no edge.
type SpanFact struct {
	Component string
	Name      string
	Order     int

	HasParent       bool
	ParentComponent string
	ParentName      string
	ParentOrder     int
}

// Build aggregates already-filtered/visible span facts into a Graph: one
// node per distinct hierarchy container, one edge per pair of containers
// connected by an observed parent/child span call. Same-container calls (a
// function calling another function in the same section) are folded into
// the node's own function tally rather than becoming self-edges.
func Build(facts []SpanFact) *Graph {
	g := New()
	for _, f := range facts {
		path, fn := HierarchyPath(f.Component, f.Name)
		node := g.EnsureNode(path, f.Order)
		node.SpanCount++
		if fn != "" {
			node.Functions[fn]++
		}

		if !f.HasParent {
			continue
		}
		parentPath, _ := HierarchyPath(f.ParentComponent, f.ParentName)
		parentNode := g.EnsureNode(parentPath, f.ParentOrder)
		if parentNode.Key != node.Key {
			g.EnsureEdge(parentNode.Key, node.Key, min(f.Order, f.ParentOrder)).Count++
		}
	}
	return g
}

// orderedNodeKeys returns g's node keys sorted by first-observed order (span
// order), tie-broken by key, so every downstream algorithm that iterates
// nodes gets a deterministic sequence regardless of Go's randomized map
// iteration.
func orderedNodeKeys(g *Graph) []string {
	keys := make([]string, 0, len(g.Nodes))
	for k := range g.Nodes {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		ni, nj := g.Nodes[keys[i]], g.Nodes[keys[j]]
		if ni.Order != nj.Order {
			return ni.Order < nj.Order
		}
		return keys[i] < keys[j]
	})
	return keys
}

// sortedEdgeKeys returns g's edge keys similarly ordered by first-observed
// order, tie-broken by endpoints.
func sortedEdgeKeys(g *Graph) [][2]string {
	keys := make([][2]string, 0, len(g.Edges))
	for k := range g.Edges {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		ei, ej := g.Edges[keys[i]], g.Edges[keys[j]]
		if ei.Order != ej.Order {
			return ei.Order < ej.Order
		}
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	return keys
}

// DetectFeedbackEdges splits g's edges into a forward DAG (adjacency list,
// feedback edges excluded) plus the set of "back" edges that would close a
// cycle in a DFS starting from nodes in order-sorted sequence. Aggregated
// container-call graphs can cycle (A calls B in one trace, B calls A in
// another) even though per-trace parent pointers can't, so layering must
// tolerate this rather than assume a DAG.
func DetectFeedbackEdges(g *Graph) (forward map[string][]string, feedback map[[2]string]bool) {
	forward = make(map[string][]string)
	feedback = make(map[[2]string]bool)

	adj := make(map[string][]*Edge)
	for _, k := range sortedEdgeKeys(g) {
		e := g.Edges[k]
		adj[e.FromKey] = append(adj[e.FromKey], e)
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(g.Nodes))

	var visit func(key string)
	visit = func(key string) {
		color[key] = gray
		for _, e := range adj[key] {
			switch color[e.ToKey] {
			case white:
				forward[key] = append(forward[key], e.ToKey)
				visit(e.ToKey)
			case gray:
				feedback[[2]string{e.FromKey, e.ToKey}] = true
			case black:
				forward[key] = append(forward[key], e.ToKey)
			}
		}
		color[key] = black
	}

	for _, key := range orderedNodeKeys(g) {
		if color[key] == white {
			visit(key)
		}
	}
	return forward, feedback
}

// AssignLayers computes a longest-path layer (rank/column) for every node
// using only forward edges -- feedback edges are ignored here, though they
// are still routed and drawn later. Nodes with no forward incoming edges
// (true roots, and nodes whose only edges were fed back) start at layer 0;
// every other node's layer is one more than its deepest forward
// predecessor. This is Kahn's algorithm: a node is only dequeued once every
// forward predecessor has already been finalized, so the result is
// deterministic regardless of queue/map iteration order.
func AssignLayers(g *Graph, forward map[string][]string) map[string]int {
	indegree := make(map[string]int, len(g.Nodes))
	for k := range g.Nodes {
		indegree[k] = 0
	}
	for _, tos := range forward {
		for _, to := range tos {
			indegree[to]++
		}
	}

	layer := make(map[string]int, len(g.Nodes))
	var queue []string
	for _, key := range orderedNodeKeys(g) {
		if indegree[key] == 0 {
			queue = append(queue, key)
			layer[key] = 0
		}
	}

	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		for _, to := range forward[key] {
			if layer[key]+1 > layer[to] {
				layer[to] = layer[key] + 1
			}
			indegree[to]--
			if indegree[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	return layer
}

// OrderedNodeKeys exposes orderedNodeKeys to callers (rendering) that need
// the same deterministic sequence.
func OrderedNodeKeys(g *Graph) []string { return orderedNodeKeys(g) }

// SortedEdgeKeys exposes sortedEdgeKeys to callers (rendering) that need the
// same deterministic sequence.
func SortedEdgeKeys(g *Graph) [][2]string { return sortedEdgeKeys(g) }
