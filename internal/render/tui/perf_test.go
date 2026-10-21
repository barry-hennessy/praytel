package tui

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

// largeModel fills a model with traces shaped like a busy service mesh:
// each trace a tree several calls deep across a pool of components, every
// span closed and carrying an attribute and a log or two. A handful of
// components call each other densely, the rest form a sparse fringe, so the
// graph has the dense cluster that real large traces do.
//
// In a mesh each component has a few callees of its own, mostly further
// downstream, plus a cluster of clusterSize that all call one another. With
// mesh false every component may call any other, a pathological worst case.
func largeModel(tb testing.TB, traces, spansPerTrace, components int, mesh bool) Model {
	tb.Helper()
	rng := rand.New(rand.NewSource(1))
	m := NewModel(nil, nil)
	m.width, m.height = 160, 50
	m.now = time.Unix(1000, 0)
	clusterSize := min(8, components)
	callees := make([][]int, components)
	for c := range callees {
		for range 3 {
			callee := c + 1 + rng.Intn(max(1, components/4))
			if rng.Intn(20) == 0 || callee >= components {
				callee = rng.Intn(components)
			}
			callees[c] = append(callees[c], callee)
		}
		if c < clusterSize {
			for o := range clusterSize {
				callees[c] = append(callees[c], o)
			}
		}
	}
	pick := func(parent int) int {
		switch {
		case !mesh || parent < 0:
			return rng.Intn(components)
		case rng.Intn(4) == 0:
			return parent
		}
		return callees[parent][rng.Intn(len(callees[parent]))]
	}
	logID := 0
	for t := range traces {
		traceID := fmt.Sprintf("trace-%04d", t)
		comps := make([]int, spansPerTrace)
		for s := range spansPerTrace {
			spanID := fmt.Sprintf("s%04d", s)
			parent := -1
			if s > 0 {
				parent = rng.Intn(s)
			}
			parentComp := -1
			if parent >= 0 {
				parentComp = comps[parent]
			}
			comps[s] = pick(parentComp)
			m.apply(domain.SpanUpserted{
				TraceID: traceID, SpanID: spanID,
				Component:  fmt.Sprintf("svc%02d", comps[s]),
				Name:       fmt.Sprintf("op%d", rng.Intn(3)),
				StartTime:  time.Unix(10, 0),
				EndTime:    time.Unix(10, int64(rng.Intn(1e9))),
				Attributes: []domain.Attr{{Key: "k", Value: domain.StringValue("v")}},
			})
			if parent >= 0 {
				m.apply(domain.SpanParentLinked{TraceID: traceID, SpanID: spanID, ParentSpanID: fmt.Sprintf("s%04d", parent)})
			}
			for range rng.Intn(3) {
				logID++
				m.apply(domain.LogReceived{ID: fmt.Sprint(logID), TraceID: traceID, SpanID: spanID, Body: "handled request ok"})
			}
			m.apply(domain.SpanClosed{TraceID: traceID, SpanID: spanID})
		}
	}
	return m
}

// unlessShort is sizes, less the last of them in a -short run: the largest
// of each benchmark takes long enough to make CI slow, and two sizes are
// still enough to show the cost growing faster than the size does.
func unlessShort[T any](sizes ...T) []T {
	if testing.Short() {
		return sizes[:len(sizes)-1]
	}
	return sizes
}

func BenchmarkTreeView(b *testing.B) {
	for _, traces := range unlessShort(10, 100, 400) {
		m := largeModel(b, traces, 50, 40, true)
		total := len(m.treeLines())
		for _, at := range []string{"top", "bottom"} {
			b.Run(fmt.Sprintf("traces=%d/lines=%d/%s", traces, total, at), func(b *testing.B) {
				mm := m
				if at == "bottom" {
					mm.scroll = total
				}
				for b.Loop() {
					_ = mm.View()
				}
			})
		}
	}
}

// BenchmarkTreeNewData is a log line arriving, a tick passing and the tree
// being redrawn: the unit of work a running program's telemetry repeats.
func BenchmarkTreeNewData(b *testing.B) {
	for _, traces := range unlessShort(25, 100, 400) {
		m := largeModel(b, traces, 50, 40, true)
		b.Run(fmt.Sprintf("traces=%d", traces), func(b *testing.B) {
			n := 0
			for b.Loop() {
				n++
				m.apply(domain.LogReceived{ID: fmt.Sprintf("new-%d", n), TraceID: "trace-0000", SpanID: "s0000", Body: "handled request ok"})
				m.now = m.now.Add(100 * time.Millisecond)
				_ = m.View()
			}
		})
	}
}

// BenchmarkTreeScrollKey is one ↓ key press followed by the redraw it
// causes, the unit of work holding the key down repeats.
func BenchmarkTreeScrollKey(b *testing.B) {
	m := largeModel(b, 100, 50, 40, true)
	var tm tea.Model = m
	down := tea.KeyMsg{Type: tea.KeyDown}
	for b.Loop() {
		tm, _ = tm.Update(down)
		_ = tm.View()
	}
}

// drawWholeGraph builds, lays out and draws every line of the graph view.
func drawWholeGraph(m Model) []string {
	return m.graphLines()
}

// BenchmarkGraphLayoutLarge is building, laying out and drawing the whole
// graph, which new telemetry calls for.
func BenchmarkGraphLayoutLarge(b *testing.B) {
	type shape struct {
		components int
		mesh       bool
	}
	cases := []shape{{20, true}, {60, true}}
	if !testing.Short() {
		cases = append(cases, shape{120, true}, shape{20, false}, shape{60, false})
	}
	for _, tc := range cases {
		m := largeModel(b, 100, 50, tc.components, tc.mesh)
		m.view = viewGraph
		g := m.buildGraph()
		shape := "mesh"
		if !tc.mesh {
			shape = "random"
		}
		b.Run(fmt.Sprintf("%s/nodes=%d/edges=%d", shape, len(g.Nodes), len(g.Edges)), func(b *testing.B) {
			for b.Loop() {
				_ = drawWholeGraph(m)
			}
		})
	}
}

// BenchmarkGraphTick is the graph view redrawing for an animation tick
// with nothing new to show.
func BenchmarkGraphTick(b *testing.B) {
	components := 60
	if testing.Short() {
		components = 20
	}
	m := largeModel(b, 100, 50, components, true)
	m.view = viewGraph
	b.Run(fmt.Sprintf("nodes=%d", len(m.buildGraph().Nodes)), func(b *testing.B) {
		var tm tea.Model = m
		_ = tm.View() // the first redraw lays the graph out; a tick after that shouldn't
		for b.Loop() {
			tm, _ = tm.Update(tickMsg(time.Unix(1001, 0)))
			_ = tm.View()
		}
	})
}
