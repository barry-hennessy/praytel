package tui

import (
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/attrspec"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/stage"
	tea "github.com/charmbracelet/bubbletea"
)

func paneModel(panes []Pane) Model {
	m := NewModel(nil, nil).WithPanes(panes)
	m.width, m.height = 100, 30
	m.apply(domain.SpanUpserted{TraceID: "t", SpanID: "r", Name: "GET /", Component: "api",
		Attributes: []domain.Attr{
			{Key: "http.method", Value: domain.StringValue("GET")},
			{Key: "secret", Value: domain.StringValue("hunter2")},
		}})
	m.apply(domain.LogReceived{ID: "1", Source: domain.SourceStdout, TraceID: "t", SpanID: "r", SpanName: "GET /", Body: "hello"})
	m.apply(domain.SpanClosed{TraceID: "t", SpanID: "r"})
	return m
}

func ctrlG(m Model) Model {
	next, _ := m.update(tea.KeyMsg{Type: tea.KeyCtrlG})
	return next
}

func TestPanesCycleInPlanOrder(t *testing.T) {
	m := paneModel([]Pane{
		{Name: "tree", Kind: PaneTree},
		{Name: "graph", Kind: PaneGraph},
		{Name: "logs", Kind: PaneStream},
	})
	want := []viewMode{viewTree, viewGraph, viewStream, viewTree}
	for i, v := range want {
		if m.view != v || m.pane != i%3 {
			t.Fatalf("step %d: view %d pane %d, want view %d pane %d", i, m.view, m.pane, v, i%3)
		}
		m = ctrlG(m)
	}
}

func TestStreamPaneShowsLines(t *testing.T) {
	m := paneModel([]Pane{{Name: "logs", Kind: PaneStream}})
	if m.view != viewStream {
		t.Fatalf("view %d", m.view)
	}
	out := m.View()
	if !strings.Contains(out, "stdout GET / t r hello") {
		t.Fatalf("stream pane:\n%s", out)
	}
	if !strings.Contains(out, "logs") {
		t.Fatalf("footer does not name the pane:\n%s", out)
	}
}

func TestPaneStagesApplyToThatPaneOnly(t *testing.T) {
	spec, err := attrspec.Parse("-secret")
	if err != nil {
		t.Fatal(err)
	}
	m := paneModel([]Pane{
		{Name: "clean", Kind: PaneTree, Stages: []stage.Stage{stage.Attrs(spec)}},
		{Name: "raw", Kind: PaneTree},
	})
	clean := m.View()
	if strings.Contains(clean, "hunter2") || !strings.Contains(clean, "http.method") {
		t.Fatalf("clean pane:\n%s", clean)
	}
	raw := ctrlG(m).View()
	if !strings.Contains(raw, "hunter2") {
		t.Fatalf("raw pane:\n%s", raw)
	}
	if m.forest.Span("t", "r").Attributes[1].Key != "secret" {
		t.Fatal("a pane's stage changed the forest")
	}
}

func TestDefaultPanesAreTreeThenGraph(t *testing.T) {
	m := NewModel(nil, nil)
	if len(m.panes) != 2 || m.panes[0].Kind != PaneTree || m.panes[1].Kind != PaneGraph || m.view != viewTree {
		t.Fatalf("panes %+v view %d", m.panes, m.view)
	}
	m = NewModel(nil, nil).WithPanes(nil)
	if len(m.panes) != 2 {
		t.Fatalf("WithPanes(nil) gave %d panes", len(m.panes))
	}
}
