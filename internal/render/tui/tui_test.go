package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTreeRendersLiveStateDurationLogsAndNesting(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.now = time.Unix(20, 0)
	m.apply(domain.SpanUpserted{
		TraceID: "trace-1", SpanID: "root", Name: "request", Component: "api",
		StartTime: time.Unix(15, 0),
	})
	m.apply(domain.LogReceived{ID: "1", TraceID: "trace-1", SpanID: "root", Body: `{"msg":"started"}`})
	m.apply(domain.SpanUpserted{
		TraceID: "trace-1", SpanID: "child", Name: "query", Component: "db",
		StartTime: time.Unix(18, 0),
		EndTime:   time.Unix(18, int64(15*time.Millisecond)),
		Status:    domain.SpanStatusError,
	})
	m.apply(domain.SpanParentLinked{TraceID: "trace-1", SpanID: "child", ParentSpanID: "root"})
	m.apply(domain.SpanClosed{TraceID: "trace-1", SpanID: "child"})

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
	if !strings.Contains(view, "┆ × db.query") {
		t.Fatalf("nested span's status is not in its own column inside its parent's bar:\n%s", view)
	}
}

func TestTreeRendersAttributesUnderHeaderNotAsLogLine(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.now = time.Unix(20, 0)
	m.apply(domain.SpanUpserted{
		TraceID: "trace-1", SpanID: "root", Name: "request", Component: "api",
		StartTime:  time.Unix(15, 0),
		Attributes: []domain.Attr{{Key: "http.method", Value: domain.StringValue("GET")}, {Key: "http.status_code", Value: domain.StringValue("200")}},
	})
	m.apply(domain.LogReceived{ID: "1", TraceID: "trace-1", SpanID: "root", Body: "started"})

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

	prefix := bars([]lipgloss.Color{neutralColor}, false)
	if !strings.HasPrefix(lines[attrIdx], prefix+detailIndent) || strings.HasPrefix(lines[attrIdx], prefix+detailIndent+" ") {
		t.Fatalf("attribute line not indented one stop in from the header:\n%q", lines[attrIdx])
	}
	if !strings.HasPrefix(lines[logIdx], prefix+detailIndent) {
		t.Fatalf("log line lost its indent:\n%q", lines[logIdx])
	}
}

// spinnerFrames collapses whichever frame a running span is on to the first.
var spinnerFrames = strings.NewReplacer("◓", "◐", "◑", "◐", "◒", "◐")

func TestTreeDrawsABarAndRuleOnlyForSpansThatCallOthers(t *testing.T) {
	// Colors are otherwise stripped when the tests' output isn't a terminal.
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	m := NewModel(nil, nil)
	m.width = 40
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "root", Component: "api", Name: "request"})
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "child", Component: "db", Name: "query"})
	m.apply(domain.SpanParentLinked{TraceID: "t1", SpanID: "child", ParentSpanID: "root"})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "other", Component: "api", Name: "health"})

	lines := m.treeLines()
	var got []string
	for _, line := range lines {
		got = append(got, spinnerFrames.Replace(strings.TrimRight(ansi.Strip(line), " ")))
	}
	want := []string{
		"◐ api.request", // status in the top cell of its own bar
		"┆ " + strings.Repeat("─", 38),
		"┆ ◐ db.query", // calls nothing: status where its bar would be...
		"┆",            // ...and a blank row for a rule
		"◐ api.health", // nothing follows the last span, so no row at all
	}
	if len(got) != len(want) {
		t.Fatalf("tree =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		// Headers go on to a live duration.
		if !strings.HasPrefix(got[i], want[i]) {
			t.Fatalf("line %d = %q, want prefix %q", i, got[i], want[i])
		}
	}

	// api.request calls db.query, so it is the first component to get a
	// color. The child's blank row still carries its parent's bar in the
	// parent's color.
	paint := func(color lipgloss.Color, s string) string {
		return lipgloss.NewStyle().Foreground(color).Render(s)
	}
	caller := componentPalette[0]
	if want := paint(caller, "┆") + " " + paint(caller, strings.Repeat("─", 38)); lines[1] != want {
		t.Fatalf("caller's rule = %q, want %q", lines[1], want)
	}
	if want := paint(caller, "┆") + "   "; lines[3] != want {
		t.Fatalf("leaf's blank row = %q, want %q", lines[3], want)
	}
}

func TestComponentsThatCallOthersKeepOneColorEach(t *testing.T) {
	m := NewModel(nil, nil)
	span := func(trace, id, parent, name string) *spanView {
		m.apply(domain.SpanUpserted{TraceID: trace, SpanID: id, Component: "svc", Name: name})
		if parent != "" {
			m.apply(domain.SpanParentLinked{TraceID: trace, SpanID: id, ParentSpanID: parent})
		}
		return m.spans[selectionKey(trace, id)]
	}

	// The child arrives before the parent it names.
	leaf := span("t1", "leaf", "mid", "leaf")
	mid := span("t1", "mid", "root", "mid")
	root := span("t1", "root", "", "root")
	if got := m.componentColor(mid); got != componentPalette[0] {
		t.Fatalf("first caller's color = %v, want the palette's first", got)
	}
	if got := m.componentColor(root); got != componentPalette[1] {
		t.Fatalf("second caller's color = %v, want the palette's second", got)
	}
	if got := m.componentColor(leaf); got != neutralColor {
		t.Fatalf("a component that calls nothing has color %v, want neutral", got)
	}

	// The same component in a later trace, this time calling nothing, and a
	// new caller alongside it.
	again := span("t2", "mid", "other", "mid")
	other := span("t2", "other", "", "other")
	if got := m.componentColor(again); got != componentPalette[0] {
		t.Fatalf("component changed color across traces: %v", got)
	}
	if got := m.componentColor(other); got != componentPalette[2] {
		t.Fatalf("third caller's color = %v, want the palette's third", got)
	}
}

func TestSearchModesChooseTracesOrMatchingLogs(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "orders", Name: "orders"})
	m.apply(domain.LogReceived{
		ID: "1", TraceID: "t1", SpanID: "orders", Body: "payment accepted",
	})
	m.apply(domain.SpanUpserted{TraceID: "t2", SpanID: "health", Name: "health"})
	m.apply(domain.LogReceived{
		ID: "2", TraceID: "t2", SpanID: "health", Body: "database unavailable",
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

func TestTreeScopesSpanIDsByTrace(t *testing.T) {
	m := NewModel(nil, nil)
	m.apply(domain.SpanUpserted{TraceID: "one", SpanID: "same", Name: "first"})
	m.apply(domain.SpanUpserted{TraceID: "two", SpanID: "same", Name: "second"})
	view := strings.Join(m.treeLines(), "\n")
	if !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Fatalf("one trace overwrote the other:\n%s", view)
	}
}

func TestTraceSearchMatchesAttributeKeyOrValue(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 12
	m.apply(domain.SpanUpserted{
		TraceID: "t1", SpanID: "orders", Name: "orders",
		Attributes: []domain.Attr{{Key: "http.status_code", Value: domain.StringValue("500")}},
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
		Attributes: []domain.Attr{{Key: "ids", Value: domain.StringValue("[12345]")}},
	})
	m.apply(domain.SpanUpserted{TraceID: "t1", SpanID: "child", Name: "unrelated"})
	m.apply(domain.SpanParentLinked{TraceID: "t1", SpanID: "child", ParentSpanID: "root"})

	m.mode = searchTraces
	m.query = "ids12345"

	if !m.matchingTraces()["t1"] {
		t.Fatalf("expected trace t1 to match query %q", m.query)
	}

	highlights := m.spanHighlights()
	root, ok := highlights[selectionKey("t1", "root")]
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

	if _, ok := highlights[selectionKey("t1", "child")]; ok {
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
		!strings.Contains(complete, "● worker.dispatch 20ms") ||
		strings.Contains(complete, "span/12345678") {
		t.Fatalf("completed span did not hydrate the placeholder:\n%s", complete)
	}
}

func TestHorizontalScrollPansLongLinesIntoView(t *testing.T) {
	m := NewModel(nil, nil)
	m.height = 8
	m.width = 20
	m.now = time.Unix(20, 0)
	m.apply(domain.SpanUpserted{
		TraceID: "trace-1", SpanID: "root", Name: "request", Component: "api",
		StartTime: time.Unix(15, 0),
	})
	longBody := "this line is much longer than the visible width and runs off screen"
	m.apply(domain.LogReceived{ID: "1", TraceID: "trace-1", SpanID: "root", Body: longBody})

	initial := m.View()
	if strings.Contains(initial, "off screen") {
		t.Fatalf("expected the tail of a long line to be off-screen before panning:\n%s", initial)
	}

	var model tea.Model = m
	for range 10 {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	m = model.(Model)
	if m.hscroll == 0 {
		t.Fatalf("right arrow did not advance horizontal scroll")
	}
	panned := m.View()
	if !strings.Contains(panned, "off screen") {
		t.Fatalf("panning right did not reveal the tail of the long line:\n%s", panned)
	}
	if strings.Contains(panned, "this line") {
		t.Fatalf("panning right did not scroll the head of the line out of view:\n%s", panned)
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = model.(Model)
	if m.hscroll != 0 {
		t.Fatalf("home did not reset horizontal scroll: %d", m.hscroll)
	}
	reset := m.View()
	if !strings.Contains(reset, "this line") {
		t.Fatalf("home did not restore the start of the line:\n%s", reset)
	}
}

func TestTreeWindowMatchesTheWholeTree(t *testing.T) {
	m := largeModel(t, 3, 12, 6, true)
	for _, mode := range []searchMode{searchTraces, searchLogs} {
		m.mode = mode
		for _, query := range []string{"", "op1", "handled", "zzz"} {
			m.query = query
			whole := m.treeLines()
			layout := m.layoutTree()
			if layout.lines != len(whole) {
				t.Fatalf("mode %d query %q: layout counts %d lines, rendering gives %d", mode, query, layout.lines, len(whole))
			}
			for from := range len(whole) {
				for _, n := range []int{1, 7, 40} {
					to := min(len(whole), from+n)
					if got := layout.rows(m, from, from+n); !reflect.DeepEqual(got, whole[from:to]) {
						t.Fatalf("mode %d query %q: rows(%d, %d) differ from the whole tree's", mode, query, from, from+n)
					}
				}
			}
		}
	}
}

func TestScrollStopsAtTheBottom(t *testing.T) {
	m := largeModel(t, 2, 10, 4, true)
	var model tea.Model = m
	bottom := len(m.treeLines()) - m.contentHeight()
	for range bottom + 50 {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if got := model.(Model).scroll; got != bottom {
		t.Fatalf("scroll = %d after holding down past the end, want %d", got, bottom)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := model.(Model).scroll; got != bottom-1 {
		t.Fatalf("one up from the bottom scrolled to %d, want %d", got, bottom-1)
	}
}
