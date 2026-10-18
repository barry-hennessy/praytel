package archmap

import (
	"fmt"
	"io"
	"strings"

	"github.com/barry-hennessy/praytel/internal/graph"
	"github.com/barry-hennessy/praytel/internal/graph/draw"
	"github.com/charmbracelet/lipgloss"
)

// view is what to draw: the nodes and calls to show and what happened to
// each. A single map is a view where everything is Unchanged.
type view struct {
	diff    bool
	entries map[Node]bool
	nodes   map[Node]State
	calls   map[Call]State
	hidden  int // components left out
}

// mapView shows all of m.
func mapView(m Map) view {
	v := view{entries: m.Entries, nodes: map[Node]State{}, calls: map[Call]State{}}
	for n := range m.Nodes {
		v.nodes[n] = Unchanged
	}
	for c := range m.Calls {
		v.calls[c] = Unchanged
	}
	return v
}

// diffView shows every change, plus each changed component's other calls
// and the components at their far ends, for context. Everything else is
// counted rather than shown, so a small change makes a small picture.
func diffView(d Diff) view {
	v := view{diff: true, entries: map[Node]bool{}, nodes: map[Node]State{}, calls: map[Call]State{}}
	for e := range d.Old.Entries {
		v.entries[e] = true
	}
	for e := range d.New.Entries {
		v.entries[e] = true
	}
	for n, s := range d.Nodes {
		if s != Unchanged {
			v.nodes[n] = s
		}
	}
	for c, s := range d.Calls {
		if s != Unchanged || v.nodes[c.From] != Unchanged || v.nodes[c.To] != Unchanged {
			v.calls[c] = s
		}
	}
	for c := range v.calls {
		v.nodes[c.From] = d.Nodes[c.From]
		v.nodes[c.To] = d.Nodes[c.To]
	}
	v.hidden = len(d.Nodes) - len(v.nodes)
	return v
}

// graphOf lays a view out as a graph, ordered by node and call so the
// drawing depends on nothing but the view.
func (v view) graphOf() (*graph.Graph, map[string]Node, map[[2]string]Call) {
	g := graph.New()
	nodes := map[string]Node{}
	for i, n := range sortedKeys(keys(v.nodes), compareNodes) {
		gn := g.EnsureNode([]string{n.Service, n.Name}, i)
		nodes[gn.Key] = n
	}
	calls := map[[2]string]Call{}
	for i, c := range sortedKeys(keys(v.calls), compareCalls) {
		from, to := graph.ContainerKey([]string{c.From.Service, c.From.Name}), graph.ContainerKey([]string{c.To.Service, c.To.Name})
		g.EnsureEdge(from, to, i)
		calls[[2]string{from, to}] = c
	}
	return g, nodes, calls
}

// Styles, in precedence order: where lines share a cell, the later wins,
// so a removal is never hidden under anything else.
const (
	stylePlain   = draw.Custom + iota // a single map
	styleContext                      // unchanged, shown for context
	styleChanged
	styleAdded
	styleRemoved
)

func stateStyle(s State, diff bool) draw.Style {
	switch {
	case !diff:
		return stylePlain
	case s == Added:
		return styleAdded
	case s == Removed:
		return styleRemoved
	case s == Changed:
		return styleChanged
	}
	return styleContext
}

func stateWord(s State) string {
	switch s {
	case Added:
		return "added"
	case Removed:
		return "removed"
	case Changed:
		return "calls changed"
	}
	return ""
}

func (v view) nodeText(n Node) (label, footer string) {
	label = n.Label()
	var words []string
	if v.diff && v.nodes[n] != Unchanged {
		label = v.nodes[n].sign() + label
		words = append(words, stateWord(v.nodes[n]))
	}
	if v.entries[n] {
		words = append(words, "entry point")
	}
	return label, strings.Join(words, ", ")
}

// draw renders the view as box drawing, coloured by r.
func (v view) draw(r *lipgloss.Renderer) []string {
	g, nodes, calls := v.graphOf()
	canvas := draw.Draw(g, draw.Looks{
		Node: func(gn *graph.Node) draw.NodeLook {
			n := nodes[gn.Key]
			label, footer := v.nodeText(n)
			look := draw.NodeLook{Style: stateStyle(v.nodes[n], v.diff), Label: label, Footer: footer}
			switch {
			case !v.diff:
			case v.nodes[n] == Added:
				look.Border = draw.DoubleBorder
			case v.nodes[n] == Removed:
				look.Border = draw.DottedBorder
			}
			return look
		},
		Edge: func(e *graph.Edge, feedback bool) draw.EdgeLook {
			s := v.calls[calls[[2]string{e.FromKey, e.ToKey}]]
			look := draw.EdgeLook{Style: stateStyle(s, v.diff)}
			switch {
			case v.diff && s == Added:
				look.Line = draw.Double
			case v.diff && s == Removed:
				look.Line = draw.Dotted
			case feedback:
				look.Line = draw.Dashed
			}
			return look
		},
	})

	styles := map[draw.Style]lipgloss.Style{
		draw.Label:    r.NewStyle().Bold(true),
		draw.Footer:   r.NewStyle().Foreground(lipgloss.Color("244")).Italic(true),
		draw.Crossing: r.NewStyle().Foreground(lipgloss.Color("240")),
		stylePlain:    r.NewStyle().Foreground(lipgloss.Color("75")),
		styleContext:  r.NewStyle().Foreground(lipgloss.Color("244")),
		styleChanged:  r.NewStyle().Foreground(lipgloss.Color("178")),
		styleAdded:    r.NewStyle().Foreground(lipgloss.Color("71")),
		styleRemoved:  r.NewStyle().Foreground(lipgloss.Color("167")),
	}
	lines := canvas.Lines(func(s draw.Style, text string) string {
		if st, ok := styles[s]; ok {
			return st.Render(text)
		}
		return text
	})
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Output is how praytel map prints.
type Output string

const (
	Text    Output = "text"
	Mermaid Output = "mermaid"
)

// RenderMap prints one map.
func RenderMap(w io.Writer, r *lipgloss.Renderer, f Output, m Map) error {
	v := mapView(m)
	if f == Mermaid {
		return v.mermaid(w, nil)
	}
	if len(v.nodes) == 0 {
		_, err := fmt.Fprintln(w, "empty map")
		return err
	}
	_, err := io.WriteString(w, strings.Join(v.draw(r), "\n")+"\n")
	return err
}

// RenderDiff prints what changed from d.Old to d.New: in text, the list
// of changes and then the drawing; in Mermaid, the drawing with the list
// as comments.
func RenderDiff(w io.Writer, r *lipgloss.Renderer, f Output, d Diff) error {
	v := diffView(d)
	lines := d.Lines()
	if f == Mermaid {
		return v.mermaid(w, lines)
	}
	var b strings.Builder
	if !d.Changed() {
		b.WriteString("no changes\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n" + strings.Join(v.draw(r), "\n") + "\n")
	if v.hidden > 0 {
		fmt.Fprintf(&b, "\n%d unchanged component%s not shown.\n", v.hidden, plural(v.hidden))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// mermaid prints the view as a Mermaid flowchart, with comments first.
func (v view) mermaid(w io.Writer, comments []string) error {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, c := range comments {
		b.WriteString("  %% " + c + "\n")
	}
	ids := map[Node]string{}
	for i, n := range sortedKeys(keys(v.nodes), compareNodes) {
		ids[n] = fmt.Sprintf("n%d", i)
		label, footer := v.nodeText(n)
		label = mermaidEscape(label)
		if footer != "" {
			label += "<br/><i>" + mermaidEscape(footer) + "</i>"
		}
		fmt.Fprintf(&b, "  %s[\"%s\"]", ids[n], label)
		if class := mermaidClass(v.nodes[n], v.diff); class != "" {
			b.WriteString(":::" + class)
		}
		b.WriteString("\n")
	}
	var linkStyles []string
	for i, c := range sortedKeys(keys(v.calls), compareCalls) {
		arrow := "-->"
		switch s := v.calls[c]; {
		case v.diff && s == Added:
			arrow = "==>"
			linkStyles = append(linkStyles, fmt.Sprintf("  linkStyle %d stroke:#2da44e,stroke-width:3px", i))
		case v.diff && s == Removed:
			arrow = "-.->"
			linkStyles = append(linkStyles, fmt.Sprintf("  linkStyle %d stroke:#cf222e", i))
		}
		fmt.Fprintf(&b, "  %s %s %s\n", ids[c.From], arrow, ids[c.To])
	}
	for _, l := range linkStyles {
		b.WriteString(l + "\n")
	}
	if v.diff {
		b.WriteString("  classDef added fill:#dafbe1,stroke:#2da44e,stroke-width:3px\n")
		b.WriteString("  classDef removed fill:#ffebe9,stroke:#cf222e,stroke-dasharray:4 3\n")
		b.WriteString("  classDef changed fill:#fff8c5,stroke:#bf8700\n")
		b.WriteString("  classDef context fill:#f6f8fa,stroke:#8c959f,color:#57606a\n")
	}
	if v.hidden > 0 {
		fmt.Fprintf(&b, "  %%%% %d unchanged component%s not shown\n", v.hidden, plural(v.hidden))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func mermaidClass(s State, diff bool) string {
	switch {
	case !diff:
		return ""
	case s == Added:
		return "added"
	case s == Removed:
		return "removed"
	case s == Changed:
		return "changed"
	}
	return "context"
}

func mermaidEscape(s string) string {
	return strings.NewReplacer(`"`, "#quot;", "<", "#lt;", ">", "#gt;").Replace(s)
}
