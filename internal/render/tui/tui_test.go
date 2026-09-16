package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
)

func TestTreeRendersLiveStateDurationLogsAndNesting(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.now = time.Unix(20, 0)
	m.apply(domain.SpanUpserted{
		TraceID: "trace-1", SpanID: "root", Name: "request", Component: "api",
		StartTime: time.Unix(15, 0),
	})
	m.apply(domain.LogReceived{ID: "1", SpanID: "root", Body: `{"msg":"started"}`})
	m.apply(domain.SpanUpserted{
		TraceID: "trace-1", SpanID: "child", Name: "query", Component: "db",
		StartTime: time.Unix(18, 0), EndTime: time.Unix(18, int64(15*time.Millisecond)), Status: domain.SpanStatusError,
	})
	m.apply(domain.SpanParentLinked{SpanID: "child", ParentSpanID: "root"})
	m.apply(domain.SpanClosed{SpanID: "child"})

	view := m.View()
	for _, want := range []string{"api.request", `{"msg":"started"}`, "db.query", "15ms", "×"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if !strings.ContainsAny(view, "◐◓◑◒") {
		t.Fatalf("running span has no spinner:\n%s", view)
	}
	if !strings.Contains(view, "┆ ┃ db.query") {
		t.Fatalf("nested span does not have depth-coloured bars:\n%s", view)
	}
}

func TestSearchModesChooseTracesOrMatchingLogs(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Name: "orders"})
	m.apply(domain.LogReceived{ID: "1", SpanID: "orders", Body: "payment accepted"})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "health", Name: "health"})
	m.apply(domain.LogReceived{ID: "2", SpanID: "health", Body: "database unavailable"})

	m.query = "ord"
	traceView := m.View()
	if !strings.Contains(traceView, "orders") || strings.Contains(traceView, "health") {
		t.Fatalf("trace fuzzy search selected the wrong trees:\n%s", traceView)
	}

	m.mode = searchLogs
	m.query = "db unav"
	logView := m.View()
	if !strings.Contains(logView, "health") || !strings.Contains(logView, "database unavailable") {
		t.Fatalf("log fuzzy search did not retain the matching span and log:\n%s", logView)
	}
	if strings.Contains(logView, "orders") || strings.Contains(logView, "payment accepted") {
		t.Fatalf("log search retained a non-match:\n%s", logView)
	}
}

func TestDeclaredLogContextCreatesLivePlaceholderThenHydrates(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 8
	m.now = time.Unix(20, 0)
	m.apply(domain.LogReceived{
		ID: "1", Body: "work in progress", Component: "worker",
		DeclaredTraceID: "trace-1", DeclaredSpanID: "1234567890abcdef",
	})

	live := m.View()
	if !strings.Contains(live, "worker.span/12345678") || !strings.ContainsAny(live, "◐◓◑◒") || !strings.Contains(live, "~0s") {
		t.Fatalf("declared context did not create a running placeholder:\n%s", live)
	}

	m.apply(domain.SpanUpserted{
		TraceID: "trace-1", SpanID: "1234567890abcdef", Name: "dispatch", Component: "worker",
		StartTime: time.Unix(19, 0), EndTime: time.Unix(19, int64(20*time.Millisecond)), Status: domain.SpanStatusOK,
	})
	m.apply(domain.SpanClosed{TraceID: "trace-1", SpanID: "1234567890abcdef"})
	m.apply(domain.LogAssigned{ID: "1", TraceID: "trace-1", SpanID: "1234567890abcdef", SpanName: "dispatch"})

	complete := m.View()
	if !strings.Contains(complete, "worker.dispatch") || !strings.Contains(complete, "● 20ms") || strings.Contains(complete, "span/12345678") {
		t.Fatalf("completed span did not hydrate the placeholder:\n%s", complete)
	}
}
