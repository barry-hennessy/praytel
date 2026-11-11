package tui

import (
	"strings"
	"testing"

	"github.com/barry-hennessy/praytel/internal/domain"
)

func queryModel() Model {
	m := NewModel(nil, nil)
	m.width, m.height = 100, 30
	for _, ev := range []domain.Event{
		domain.SpanUpserted{TraceID: "t1", SpanID: "a", Name: "checkout", Component: "api"},
		domain.SpanUpserted{TraceID: "t1", SpanID: "b", Name: "insert", Component: "db", Status: domain.SpanStatusError},
		domain.SpanParentLinked{TraceID: "t1", SpanID: "b", ParentSpanID: "a"},
		domain.SpanUpserted{TraceID: "t2", SpanID: "c", Name: "healthcheck", Component: "api"},
	} {
		m.apply(ev)
	}
	return m
}

func TestQueryModeShowsThePath(t *testing.T) {
	m := queryModel()
	m.query = `/span{ span:status = error }`
	out := m.View()
	if !strings.Contains(out, "insert") || !strings.Contains(out, "checkout") {
		t.Fatalf("the match and its parent should show:\n%s", out)
	}
	if strings.Contains(out, "healthcheck") {
		t.Fatalf("the other trace should not show:\n%s", out)
	}

	m.query = `/span{ span:name = "healthcheck" }`
	out = m.View()
	if !strings.Contains(out, "healthcheck") || strings.Contains(out, "insert") {
		t.Fatalf("a new query should replace the old one:\n%s", out)
	}
}

func TestQueryModeErrorShowsInTheFooter(t *testing.T) {
	m := queryModel()
	m.query = `/span{ span:nope = 1 }`
	out := m.View()
	if !strings.Contains(out, "unknown field span:nope") {
		t.Fatalf("no error in the footer:\n%s", out)
	}
	if !strings.Contains(out, "healthcheck") || !strings.Contains(out, "insert") {
		t.Fatalf("a bad query should not hide anything:\n%s", out)
	}
}

func TestFuzzySearchStillWorks(t *testing.T) {
	m := queryModel()
	m.query = "health"
	out := m.View()
	if !strings.Contains(out, "healthcheck") || strings.Contains(out, "insert") {
		t.Fatalf("fuzzy search:\n%s", out)
	}
}
