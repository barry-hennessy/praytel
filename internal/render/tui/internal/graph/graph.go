// Package graph builds and lays out the TUI's component interconnection
// graph: one node per distinct nested component that spans execute as, one
// aggregated edge per pair of components connected by an observed
// parent/child span call. It knows nothing about Model, live filtering, or
// rendering styles -- callers hand it already-visible span facts and get
// back a graph plus layering/layout primitives.
package graph

import (
	"sort"
	"strings"
)

// ContainerKey returns the stable map key for a component path.
func ContainerKey(path []string) string {
	return strings.Join(path, "\x1f")
}

// Node is one component in the interconnection graph: a distinct path
// through the Hierarchy, down to and including the innermost component a
// span names. Every distinct span name is therefore a node of its own; a
// component that only exists as a shared prefix, with no span of that exact
// name, is not.
type Node struct {
	Key       string
	Path      []string
	SpanCount int
	Order     int // min contributing span order, for deterministic layout
	Color     int // see SpanFact.Color
}

// Label is the component's full name. Path elements below the Component
// carry their own leading separator, so this is plain concatenation.
func (n *Node) Label() string {
	return strings.Join(n.Path, "")
}

// Edge is a directed, aggregated call from one component to another --
// built whenever a visible span's parent resolves to a different path,
// whether that crosses a top-level Component (service) boundary or stays
// within one.
type Edge struct {
	FromKey, ToKey string
	Count          int
	Order          int
}

// Graph is a set of components and the aggregated calls between them.
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
		n = &Node{Key: key, Path: append([]string(nil), path...), Order: order}
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
	Color     int // caller-chosen color tag, opaque here; 0 means none

	HasParent       bool
	ParentComponent string
	ParentName      string
	ParentOrder     int
}

// Build aggregates already-filtered/visible span facts into a Graph: one
// node per distinct component path, one edge per pair of components
// connected by an observed parent/child span call. A span whose parent
// resolves to the same path (recursion, or a retry under the same name)
// only adds to that node's span count rather than becoming a self-edge.
//
// Paths are derived from the names in facts alone (see Hierarchy), so the
// same span can resolve to a differently nested path as the visible set
// grows.
func Build(facts []SpanFact) *Graph {
	h := NewHierarchy()
	for _, f := range facts {
		h.Add(f.Component, f.Name)
		if f.HasParent {
			h.Add(f.ParentComponent, f.ParentName)
		}
	}

	g := New()
	for _, f := range facts {
		node := g.EnsureNode(h.Path(f.Component, f.Name), f.Order)
		if node.Color == 0 {
			node.Color = f.Color
		}
		node.SpanCount++

		if !f.HasParent {
			continue
		}
		parentNode := g.EnsureNode(h.Path(f.ParentComponent, f.ParentName), f.ParentOrder)
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
// component-call graphs can cycle (A calls B in one trace, B calls A in
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

// OrderedNodeKeys exposes orderedNodeKeys to callers (rendering) that need
// the same deterministic sequence.
func OrderedNodeKeys(g *Graph) []string { return orderedNodeKeys(g) }

// SortedEdgeKeys exposes sortedEdgeKeys to callers (rendering) that need the
// same deterministic sequence.
func SortedEdgeKeys(g *Graph) [][2]string { return sortedEdgeKeys(g) }
