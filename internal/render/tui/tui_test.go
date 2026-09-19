package tui

import (
	"reflect"
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
		StartTime: time.Unix(18, 0),
		EndTime:   time.Unix(18, int64(15*time.Millisecond)),
		Status:    domain.SpanStatusError,
	})
	m.apply(domain.SpanParentLinked{SpanID: "child", ParentSpanID: "root"})
	m.apply(domain.SpanClosed{SpanID: "child"})

	view := m.View()
	wants := []string{
		"api.request",
		`{"msg":"started"}`,
		"db.query",
		"15ms",
		"×",
	}
	for _, want := range wants {
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

func TestTreeRendersAttributesUnderHeaderNotAsLogLine(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.now = time.Unix(20, 0)
	m.apply(domain.SpanUpserted{
		TraceID: "trace-1", SpanID: "root", Name: "request", Component: "api",
		StartTime:  time.Unix(15, 0),
		Attributes: []domain.Attr{{Key: "http.method", Value: "GET"}, {Key: "http.status_code", Value: "200"}},
	})
	m.apply(domain.LogReceived{ID: "1", SpanID: "root", Body: "started"})

	lines := m.treeLines()

	headerIdx := -1
	attrIdx := -1
	logIdx := -1
	for i, line := range lines {
		switch {
		case strings.Contains(line, "api.request"):
			headerIdx = i
		case strings.Contains(line, "http.method=GET"):
			attrIdx = i
		case strings.Contains(line, "started"):
			logIdx = i
		}
	}
	if headerIdx == -1 || attrIdx == -1 || logIdx == -1 {
		t.Fatalf("missing header/attr/log line:\n%s", strings.Join(lines, "\n"))
	}
	if attrIdx != headerIdx+1 {
		t.Fatalf("attribute line not directly under header: header=%d attr=%d", headerIdx, attrIdx)
	}
	if attrIdx >= logIdx {
		t.Fatalf("attribute line should render before log lines: attr=%d log=%d", attrIdx, logIdx)
	}
	if !strings.Contains(lines[attrIdx], "http.status_code=200") {
		t.Fatalf("second attribute missing from attribute line:\n%s", lines[attrIdx])
	}

	prefix := m.bars(0, false)
	if !strings.HasPrefix(lines[attrIdx], prefix+"http.method") {
		t.Fatalf("attribute line not flush under header (indented like a log line instead):\n%q", lines[attrIdx])
	}
	if !strings.HasPrefix(lines[logIdx], prefix+"   ") {
		t.Fatalf("log line lost its extra indent:\n%q", lines[logIdx])
	}
}

func TestSearchModesChooseTracesOrMatchingLogs(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Name: "orders"})
	m.apply(domain.LogReceived{
		ID: "1", SpanID: "orders", Body: "payment accepted",
	})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "health", Name: "health"})
	m.apply(domain.LogReceived{
		ID: "2", SpanID: "health", Body: "database unavailable",
	})

	m.query = "ord"
	traceView := m.View()
	if !strings.Contains(traceView, "orders") ||
		strings.Contains(traceView, "health") {
		t.Fatalf("trace fuzzy search selected the wrong trees:\n%s", traceView)
	}

	m.mode = searchLogs
	m.query = "db unav"
	logView := m.View()
	if !strings.Contains(logView, "health") ||
		!strings.Contains(logView, "database unavailable") {
		t.Fatalf(
			"log fuzzy search did not retain the matching span and log:\n%s",
			logView,
		)
	}
	if strings.Contains(logView, "orders") ||
		strings.Contains(logView, "payment accepted") {
		t.Fatalf("log search retained a non-match:\n%s", logView)
	}
}

func TestTraceSearchMatchesAttributeKeyOrValue(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.apply(domain.SpanUpserted{
		TraceID: "t1", SpanID: "orders", Name: "orders",
		Attributes: []domain.Attr{{Key: "http.status_code", Value: "500"}},
	})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "health", Name: "health"})

	m.query = "status_code"
	byKey := m.View()
	if !strings.Contains(byKey, "orders") || strings.Contains(byKey, "health") {
		t.Fatalf("attribute key search selected the wrong trees:\n%s", byKey)
	}

	m.query = "500"
	byValue := m.View()
	if !strings.Contains(byValue, "orders") || strings.Contains(byValue, "health") {
		t.Fatalf("attribute value search selected the wrong trees:\n%s", byValue)
	}
}

func TestSpanHighlightsMarkOnlyTheMatchedCharacters(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.apply(domain.SpanUpserted{
		TraceID: "t1", SpanID: "root", Name: "orders",
		Attributes: []domain.Attr{{Key: "ids", Value: "[12345]"}},
	})
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "child", Name: "unrelated"})
	m.apply(domain.SpanParentLinked{SpanID: "child", ParentSpanID: "root"})

	m.mode = searchTraces
	m.query = "ids12345"

	if !m.matchingTraces()["t1"] {
		t.Fatalf("expected trace t1 to match query %q", m.query)
	}

	highlights := m.spanHighlights()
	root, ok := highlights["root"]
	if !ok {
		t.Fatalf("expected span %q to have its own highlight, got none", "root")
	}
	if len(root.attrs) != 1 {
		t.Fatalf("expected exactly one attribute mask, got %d", len(root.attrs))
	}
	want := []bool{true, true, true, false, false, true, true, true, true, true, false}
	if !reflect.DeepEqual(root.attrs[0], want) {
		t.Fatalf("attribute mask for %q = %v, want %v", "ids=[12345]", root.attrs[0], want)
	}

	if _, ok := highlights["child"]; ok {
		t.Fatalf("child span does not itself match %q, so it should have no highlight, but it does", m.query)
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
	if !strings.Contains(live, "worker.span/12345678") ||
		!strings.ContainsAny(live, "◐◓◑◒") ||
		!strings.Contains(live, "~0s") {
		t.Fatalf("declared context did not create a running placeholder:\n%s", live)
	}

	m.apply(domain.SpanUpserted{
		TraceID:   "trace-1",
		SpanID:    "1234567890abcdef",
		Name:      "dispatch",
		Component: "worker",
		StartTime: time.Unix(19, 0),
		EndTime:   time.Unix(19, int64(20*time.Millisecond)),
		Status:    domain.SpanStatusOK,
	})
	m.apply(domain.SpanClosed{TraceID: "trace-1", SpanID: "1234567890abcdef"})
	m.apply(domain.LogAssigned{
		ID: "1", TraceID: "trace-1", SpanID: "1234567890abcdef",
		SpanName: "dispatch",
	})

	complete := m.View()
	if !strings.Contains(complete, "worker.dispatch") ||
		!strings.Contains(complete, "● 20ms") ||
		strings.Contains(complete, "span/12345678") {
		t.Fatalf("completed span did not hydrate the placeholder:\n%s", complete)
	}
}
