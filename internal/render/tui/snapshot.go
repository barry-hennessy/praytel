package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/barry-hennessy/praytel/internal/attrspec"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/render/tui/internal/graph"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// SnapshotVersion is the first line of every snapshot, after "# praytel-snapshot ".
const SnapshotVersion = "v1"

// snapshotWidth is the fixed width of a snapshot's span rules.
const snapshotWidth = 80

// missingSpan stands in for the name of a span that logs declared but that
// never arrived.
const missingSpan = "<missing span>"

// SnapshotOptions chooses what a snapshot shows.
type SnapshotOptions struct {
	Tree  bool
	Graph bool
	Attrs attrspec.Spec
	Logs  bool
}

// CapturedLine is one line the child wrote to stdout or stderr.
type CapturedLine struct {
	Source domain.Source
	Body   string
}

// SnapshotResult is what Snapshot made of an event stream.
type SnapshotResult struct {
	// Text is the snapshot. It is only meant to be shown if Problems is empty.
	Text string
	// Problems lists why the snapshot cannot be trusted to be complete.
	Problems []string
	// Captured is the child's stdout and stderr, in arrival order.
	Captured []CapturedLine
}

// Snapshot reads events until the channel closes and renders one
// deterministic plain-text view of the settled trace tree and/or component
// graph. It does no I/O: events in, text out. The output depends only on the
// final set of spans, never on the order they arrived in.
func Snapshot(events <-chan domain.Event, opts SnapshotOptions) SnapshotResult {
	m := NewModel(events, nil)
	m.plain = true
	m.width = snapshotWidth

	var (
		res                   SnapshotResult
		drained, evicted, shd bool
	)
	for ev := range events {
		switch ev := ev.(type) {
		case domain.DrainComplete:
			drained = true
		case domain.ForestEvicted:
			evicted = true
		case domain.ForestShed:
			shd = true
		case domain.LogReceived:
			if ev.Source == domain.SourceStdout || ev.Source == domain.SourceStderr {
				res.Captured = append(res.Captured, CapturedLine{Source: ev.Source, Body: ev.Body})
			}
		}
		m.apply(ev)
	}

	var problems []string
	if !drained {
		problems = append(problems, "the drain never completed")
	}
	if evicted {
		problems = append(problems, "a trace was evicted under memory pressure")
	}
	if shd {
		problems = append(problems, "telemetry was shed under memory pressure")
	}
	problems = append(problems, m.canonicalise(opts)...)
	if m.forest.Len() == 0 {
		problems = append(problems, "no spans arrived (uninstrumented code, a cached run, or spans never flushed)")
	}
	sort.Strings(problems)
	res.Problems = problems

	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)

	var b strings.Builder
	b.WriteString("# praytel-snapshot " + SnapshotVersion + "\n")
	if opts.Tree {
		b.WriteString("## tree\n")
		writeLines(&b, m.treeLines())
	}
	if opts.Graph {
		b.WriteString("## graph\n")
		g := m.buildGraph()
		rankGraph(g)
		writeLines(&b, layoutGraphOf(g, nil, "(no components)").lines())
	}
	res.Text = b.String()

	return res
}

// writeLines writes lines with their styling and trailing spaces removed.
func writeLines(b *strings.Builder, lines []string) {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	for _, l := range out {
		b.WriteString(l)
		b.WriteByte('\n')
	}
}

// rankGraph replaces the arrival-order Order of g's nodes and edges with
// ranks from their sorted keys, so the layout depends only on which nodes and
// edges exist.
func rankGraph(g *graph.Graph) {
	nodes := make([]string, 0, len(g.Nodes))
	for k := range g.Nodes {
		nodes = append(nodes, k)
	}
	sort.Strings(nodes)
	for i, k := range nodes {
		g.Nodes[k].Order = i
	}

	edges := make([][2]string, 0, len(g.Edges))
	for k := range g.Edges {
		edges = append(edges, k)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i][0] != edges[j][0] {
			return edges[i][0] < edges[j][0]
		}
		return edges[i][1] < edges[j][1]
	})
	for i, k := range edges {
		g.Edges[k].Order = i
	}
}

// canonicalise replaces the model's forest with a copy rewritten so that
// rendering it does not depend on arrival order: it drops what a snapshot
// never shows, filters and sorts attributes, and renumbers span.Order to the
// canonical pre-order. The forest it was given does not change. It returns
// the reasons the settled model is incomplete.
func (m *Model) canonicalise(opts SnapshotOptions) []string {
	var problems []string

	m.forest = m.forest.Clone(func(sp *spanView) bool {
		return opts.Logs || !sp.Synthetic
	})
	if !opts.Logs {
		for sp := range m.forest.Spans() {
			sp.Logs = nil
		}
	}

	var declared, unclosed, orphaned int
	for sp := range m.forest.Spans() {
		switch {
		case sp.Declared:
			declared++
		case !sp.Closed && !sp.Synthetic:
			unclosed++
		}
		if sp.ParentID != "" && m.forest.Span(sp.TraceID, sp.ParentID) == nil {
			orphaned++
		}
	}
	if declared > 0 {
		problems = append(problems, fmt.Sprintf("%d span(s) were declared by logs but never arrived", declared))
	}
	if unclosed > 0 {
		problems = append(problems, fmt.Sprintf("%d span(s) never ended", unclosed))
	}
	if orphaned > 0 {
		problems = append(problems, fmt.Sprintf("%d span(s) name a parent that never arrived", orphaned))
	}

	for sp := range m.forest.Spans() {
		if sp.Declared {
			sp.Name, sp.Component = missingSpan, ""
		}
		sp.Attributes = m.snapshotAttrs(sp.Attributes, opts.Attrs)
		sort.SliceStable(sp.Logs, func(i, j int) bool {
			return m.logBody(sp.Logs[i]) < m.logBody(sp.Logs[j])
		})
	}

	children := make(map[string][]*spanView)
	var roots []*spanView
	for sp := range m.forest.Spans() {
		if sp.ParentID != "" && m.forest.Span(sp.TraceID, sp.ParentID) != nil {
			pk := selectionKey(sp.TraceID, sp.ParentID)
			children[pk] = append(children[pk], sp)
		} else {
			roots = append(roots, sp)
		}
	}

	keys := make(map[*spanView]string, m.forest.Len())
	var keyOf func(sp *spanView) string
	keyOf = func(sp *spanView) string {
		if k, ok := keys[sp]; ok {
			return k
		}
		keys[sp] = "" // a cycle terminates here; see unreachable below
		kids := children[selectionKey(sp.TraceID, sp.ID)]
		kidKeys := make([]string, len(kids))
		for i, c := range kids {
			kidKeys[i] = keyOf(c)
		}
		sort.Strings(kidKeys)

		var b strings.Builder
		for _, f := range []string{sp.Component, sp.Name, strconv.Itoa(int(sp.Status)), clean(sp.StatusMsg)} {
			b.WriteString(strconv.Quote(f))
			b.WriteByte(',')
		}
		for _, a := range sp.Attributes {
			b.WriteString(strconv.Quote(a.Key + "=" + a.Value.String()))
			b.WriteByte(',')
		}
		for _, id := range sp.Logs {
			b.WriteString(strconv.Quote("log:" + m.logBody(id)))
			b.WriteByte(',')
		}
		b.WriteByte('[')
		b.WriteString(strings.Join(kidKeys, ""))
		b.WriteByte(']')
		keys[sp] = b.String()

		return keys[sp]
	}
	for _, r := range roots {
		keyOf(r)
	}

	byKey := func(s []*spanView) {
		sort.SliceStable(s, func(i, j int) bool { return keys[s[i]] < keys[s[j]] })
	}
	byKey(roots)
	for _, kids := range children {
		byKey(kids)
	}

	next := 0
	var number func(sp *spanView)
	number = func(sp *spanView) {
		next++
		sp.Order = next
		for _, c := range children[selectionKey(sp.TraceID, sp.ID)] {
			number(c)
		}
	}
	for _, r := range roots {
		number(r)
	}
	if next < m.forest.Len() {
		problems = append(problems, fmt.Sprintf("%d span(s) are unreachable from any root (parent cycle)", m.forest.Len()-next))
	}

	return problems
}

func (m *Model) logBody(id string) string {
	if l := m.forest.Log(id); l != nil {
		return l.Body
	}
	return ""
}

// snapshotAttrs is attrs filtered by spec and sorted by key, then value.
func (m *Model) snapshotAttrs(attrs []domain.Attr, spec attrspec.Spec) []domain.Attr {
	var out []domain.Attr
	for _, a := range attrs {
		if spec.Match(a.Key) {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Value.String() < out[j].Value.String()
	})
	return out
}
