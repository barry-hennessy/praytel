package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/praytel/internal/projection"
	querylang "github.com/barry-hennessy/praytel/internal/query"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

type searchMode uint8

const (
	searchTraces searchMode = iota
	searchLogs
)

var depthColors = []lipgloss.Color{
	lipgloss.Color("39"),
	lipgloss.Color("135"),
	lipgloss.Color("214"),
	lipgloss.Color("42"),
	lipgloss.Color("207"),
}

type spanView struct {
	id         string
	traceID    string
	parentID   string
	name       string
	component  string
	declared   bool
	start      time.Time
	end        time.Time
	observed   time.Time
	status     domain.SpanStatus
	closed     bool
	order      int
	attributes []domain.Attr
	logs       []string
}

type logView struct {
	id      string
	body    string
	traceID string
	spanID  string
}

type eventMsg struct {
	event domain.Event
	ok    bool
}

type tickMsg time.Time

// Model is the live trace-tree Bubble Tea model.
type Model struct {
	events <-chan domain.Event
	filt   *filter.Filter
	fixed  *querylang.Query
	proj   *projection.Projection

	spans     map[string]*spanView
	logs      map[string]*logView
	nextOrder int

	mode     searchMode
	query    string
	width    int
	height   int
	scroll   int
	hscroll  int
	now      time.Time
	done     bool
	warning  string
	quitting bool
}

// hscrollStep is how many columns left/right move the viewport per key
// press.
const hscrollStep = 8

// NewModel constructs a model. It is exported for embedding and deterministic
// tests.
func NewModel(events <-chan domain.Event, filt *filter.Filter) Model {
	return NewQueryModel(events, filt, nil)
}

// NewQueryModel constructs a model with an optional checked query that limits
// the displayed records. The interactive fuzzy search remains available and
// narrows the query-selected view further.
func NewQueryModel(events <-chan domain.Event, filt *filter.Filter, query *querylang.Query) Model {
	if filt == nil {
		filt, _ = filter.New(filter.Query{})
	}

	return Model{
		events: events,
		filt:   filt,
		fixed:  query,
		proj:   projection.New(),
		spans:  make(map[string]*spanView),
		logs:   make(map[string]*logView),
		width:  80,
		height: 24,
		now:    time.Now(),
	}
}

// Init begins waiting for telemetry and animation ticks.
func (m Model) Init() tea.Cmd {
	return tea.Batch(waitEvent(m.events), tick())
}

func waitEvent(events <-chan domain.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		return eventMsg{event: ev, ok: ok}
	}
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update applies one Bubble Tea message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case eventMsg:
		if !msg.ok {
			m.done = true
			return m, nil
		}
		m.apply(msg.event)
		return m, waitEvent(m.events)
	case tickMsg:
		m.now = time.Time(msg)
		if m.done {
			return m, nil
		}
		return m, tick()
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			if m.mode == searchTraces {
				m.mode = searchLogs
			} else {
				m.mode = searchTraces
			}
			m.scroll = 0
			m.hscroll = 0
		case "esc":
			m.query = ""
			m.scroll = 0
			m.hscroll = 0
		case "backspace", "ctrl+h":
			if len(m.query) > 0 {
				r := []rune(m.query)
				m.query = string(r[:len(r)-1])
				m.scroll = 0
			}
		case "up", "ctrl+k":
			if m.scroll > 0 {
				m.scroll--
			}
		case "down", "ctrl+j":
			m.scroll++
		case "pgup":
			m.scroll -= max(1, m.contentHeight()-1)
			if m.scroll < 0 {
				m.scroll = 0
			}
		case "pgdown":
			m.scroll += max(1, m.contentHeight()-1)
		case "left", "ctrl+b":
			m.hscroll -= hscrollStep
			if m.hscroll < 0 {
				m.hscroll = 0
			}
		case "right", "ctrl+f":
			m.hscroll += hscrollStep
		case "home":
			m.hscroll = 0
		default:
			if msg.Type == tea.KeyRunes {
				m.query += string(msg.Runes)
				m.scroll = 0
			}
		}
	}

	return m, nil
}

func (m *Model) apply(ev domain.Event) {
	m.proj.Observe(ev, m.now)
	m.filt.Observe(ev)
	switch ev := ev.(type) {
	case domain.SpanUpserted:
		sp := m.ensureSpan(ev.TraceID, ev.SpanID)
		sp.traceID = ev.TraceID
		sp.name = ev.Name
		sp.component = ev.Component
		sp.declared = false
		sp.start = ev.StartTime
		sp.end = ev.EndTime
		sp.status = ev.Status
		sp.attributes = ev.Attributes
	case domain.SpanParentLinked:
		m.ensureSpan(ev.TraceID, ev.SpanID).parentID = ev.ParentSpanID
	case domain.SpanClosed:
		m.ensureSpan(ev.TraceID, ev.SpanID).closed = true
	case domain.LogReceived:
		m.nextOrder++
		spanID := ev.SpanID
		if spanID == "" {
			spanID = ev.DeclaredSpanID
		}
		traceID := ev.TraceID
		if traceID == "" {
			traceID = ev.DeclaredTraceID
		}
		m.logs[ev.ID] = &logView{id: ev.ID, body: ev.Body, traceID: traceID, spanID: spanID}
		if ev.SpanID == "" && ev.DeclaredSpanID != "" {
			sp := m.ensureSpan(ev.DeclaredTraceID, ev.DeclaredSpanID)
			sp.traceID = ev.DeclaredTraceID
			sp.component = ev.Component
			sp.name = "span/" + shortID(ev.DeclaredSpanID)
			sp.declared = true
		}
		m.attachLog(ev.ID, "", "", traceID, spanID)
	case domain.LogAssigned:
		log := m.logs[ev.ID]
		if log != nil {
			oldTrace, old := log.traceID, log.spanID
			log.traceID = ev.TraceID
			log.spanID = ev.SpanID
			m.attachLog(ev.ID, oldTrace, old, ev.TraceID, ev.SpanID)
			sp := m.spans[selectionKey(oldTrace, old)]
			if (oldTrace != ev.TraceID || old != ev.SpanID) && sp != nil &&
				sp.declared && len(sp.logs) == 0 {
				delete(m.spans, selectionKey(oldTrace, old))
			}
		}
	case domain.DrainComplete:
		m.done = true
	case domain.ForestEvicted:
		m.removeTrace(ev.TraceID)
		m.warning = "data pressure: evicted the oldest completed trace"
	case domain.ForestShed:
		m.warning = "data pressure: newest telemetry was dropped"
	}
}

func (m *Model) ensureSpan(traceID, id string) *spanView {
	key := selectionKey(traceID, id)
	if sp := m.spans[key]; sp != nil {
		return sp
	}
	m.nextOrder++
	sp := &spanView{id: id, traceID: traceID, observed: m.now, order: m.nextOrder}
	m.spans[key] = sp
	return sp
}

func (m *Model) attachLog(id, oldTraceID, oldSpanID, traceID, spanID string) {
	if oldSpanID != "" && (oldTraceID != traceID || oldSpanID != spanID) {
		old := m.spans[selectionKey(oldTraceID, oldSpanID)]
		if old != nil {
			for i, logID := range old.logs {
				if logID == id {
					old.logs = append(old.logs[:i], old.logs[i+1:]...)
					break
				}
			}
		}
	}
	if spanID == "" {
		return
	}
	sp := m.ensureSpan(traceID, spanID)
	for _, logID := range sp.logs {
		if logID == id {
			return
		}
	}
	sp.logs = append(sp.logs, id)
}

func (m *Model) removeTrace(traceID string) {
	for id, sp := range m.spans {
		if sp.traceID != traceID {
			continue
		}
		for _, logID := range sp.logs {
			delete(m.logs, logID)
		}
		delete(m.spans, id)
	}
}

// View renders the current tree and interaction panel.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	lines := m.treeLines()
	height := m.contentHeight()
	maxScroll := max(0, len(lines)-height)
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	end := min(len(lines), m.scroll+height)
	visible := lines[min(m.scroll, len(lines)):end]

	width := max(1, m.width)
	maxHScroll := 0
	for _, l := range visible {
		if w := ansi.StringWidth(l) - width; w > maxHScroll {
			maxHScroll = w
		}
	}
	if m.hscroll > maxHScroll {
		m.hscroll = maxHScroll
	}
	cut := make([]string, len(visible))
	for i, l := range visible {
		cut[i] = ansi.Cut(l, m.hscroll, m.hscroll+width)
	}
	visible = cut
	for len(visible) < height {
		visible = append(visible, "")
	}

	return strings.Join(visible, "\n") + "\n" + m.panel()
}

func (m Model) contentHeight() int {
	return max(1, m.height-3)
}

// filteredForest applies the glob focus/exclude filter (m.filt.InFocus) and
// the compiled-query selection (m.querySelection) to m.spans -- but NOT the
// live fuzzy search, which fuzzyVisible applies separately since its
// mode-dependent semantics (whole-trace vs. per-branch) differ from simple
// inclusion. treeLines and (a planned) graph view both call this so they
// can never disagree about what's in scope.
func (m Model) filteredForest() (
	children map[string][]*spanView,
	roots []*spanView,
	inScope map[string]*spanView,
	selectedLogs map[string]bool,
) {
	selectedSpans, selectedLogs := m.querySelection()
	children = make(map[string][]*spanView)
	inScope = make(map[string]*spanView)
	for _, sp := range m.spans {
		if !m.filt.InFocus(sp.id) {
			continue
		}
		if selectedSpans != nil && !selectedSpans[selectionKey(sp.traceID, sp.id)] {
			continue
		}
		inScope[selectionKey(sp.traceID, sp.id)] = sp
		parent := m.spans[selectionKey(sp.traceID, sp.parentID)]
		if sp.parentID != "" && parent != nil &&
			m.filt.InFocus(parent.id) &&
			(selectedSpans == nil || selectedSpans[selectionKey(parent.traceID, parent.id)]) {
			children[selectionKey(sp.traceID, sp.parentID)] = append(children[selectionKey(sp.traceID, sp.parentID)], sp)
		} else {
			roots = append(roots, sp)
		}
	}
	byOrder := func(s []*spanView) {
		sort.SliceStable(s, func(i, j int) bool {
			return s[i].order < s[j].order
		})
	}
	byOrder(roots)
	for id := range children {
		byOrder(children[id])
	}
	return children, roots, inScope, selectedLogs
}

// fuzzyVisible reports whether sp should be considered "in view" under the
// current live search (m.mode, m.query): whole-trace in searchTraces mode
// (any span in the trace matched), or per-branch in searchLogs mode (sp or
// a descendant has a matching log). Always true when there is no live
// query.
func (m Model) fuzzyVisible(
	sp *spanView,
	children map[string][]*spanView,
	traceMatches, logMatches map[string]bool,
) bool {
	if m.query == "" {
		return true
	}
	switch m.mode {
	case searchTraces:
		return traceMatches[sp.traceID]
	case searchLogs:
		return m.branchHasLog(sp, children, logMatches, make(map[string]bool))
	}
	return true
}

func (m Model) treeLines() []string {
	children, roots, _, selectedLogs := m.filteredForest()

	traceMatches := m.matchingTraces()
	logMatches := m.matchingLogs()
	highlights := m.spanHighlights()
	var lines []string
	visited := make(map[string]bool)
	for _, root := range roots {
		if !m.fuzzyVisible(root, children, traceMatches, logMatches) {
			continue
		}
		m.renderSpan(&lines, root, 0, children, logMatches, selectedLogs, highlights, visited)
	}
	if len(lines) == 0 {
		if m.query == "" && m.fixed == nil {
			return []string{
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("245")).
					Render("  Waiting for traces and logs…"),
			}
		}
		return []string{
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Render("  No matches"),
		}
	}
	return lines
}

func selectionKey(traceID, spanID string) string { return traceID + "\x00" + spanID }

func (m Model) querySelection() (map[string]bool, map[string]bool) {
	if m.fixed == nil {
		return nil, nil
	}
	spans, logs := map[string]bool{}, map[string]bool{}
	for _, result := range m.proj.Results(m.fixed, m.now) {
		for _, id := range result.Evidence.SelectedSpanIDs {
			spans[selectionKey(result.TraceID, id)] = true
		}
		for _, id := range result.Evidence.SelectedLogIDs {
			logs[id] = true
		}
	}
	return spans, logs
}

func (m Model) renderSpan(
	lines *[]string,
	sp *spanView,
	depth int,
	children map[string][]*spanView,
	logMatches map[string]bool,
	selectedLogs map[string]bool,
	highlights map[string]spanHighlight,
	visited map[string]bool,
) {
	key := selectionKey(sp.traceID, sp.id)
	if visited[key] {
		return
	}
	visited[key] = true
	if m.mode == searchLogs && !m.fuzzyVisible(sp, children, nil, logMatches) {
		return
	}

	h := highlights[key]
	prefix := m.bars(depth, sp.closed)
	symbol, style := m.state(sp)
	header := fmt.Sprintf(
		"%s%s %s %s",
		prefix,
		styleSegments(spanLabel(sp), h.label, lipgloss.NewStyle(), matchHighlightStyle),
		style.Render(symbol),
		m.duration(sp),
	)
	*lines = append(*lines, header)
	if len(sp.attributes) > 0 {
		*lines = append(
			*lines,
			m.bars(depth, sp.closed)+attributesLine(sp.attributes, h.attrs),
		)
	}
	for _, id := range sp.logs {
		log := m.logs[id]
		if log == nil || (selectedLogs != nil && !selectedLogs[id]) ||
			(m.mode == searchLogs && m.query != "" && !logMatches[id]) {
			continue
		}
		body := clean(log.body)
		line := m.bars(depth, sp.closed) + "   " +
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Render(body)
		*lines = append(*lines, line)
	}
	for _, child := range children[key] {
		m.renderSpan(lines, child, depth+1, children, logMatches, selectedLogs, highlights, visited)
	}
}

func (m Model) bars(depth int, closed bool) string {
	var b strings.Builder
	for i := 0; i <= depth; i++ {
		bar := "┆"
		if closed && i == depth {
			bar = "┃"
		}
		b.WriteString(
			lipgloss.NewStyle().
				Foreground(depthColors[i%len(depthColors)]).
				Render(bar),
		)
		b.WriteByte(' ')
	}
	return b.String()
}

// attributesLine renders a span's attributes on one faint, italic line so
// they read as metadata attached to the header above rather than as a log
// entry (which is plain-styled and indented one level further in). masks,
// when non-nil, highlights the runes of the matching attribute(s) that made
// the span match the current trace search (see matchSpan); it is nil
// outside of a search or for spans not shown for their own sake.
func attributesLine(attrs []domain.Attr, masks [][]bool) string {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	parts := make([]string, 0, len(attrs))
	for i, a := range attrs {
		text := clean(a.Key) + "=" + clean(a.Value.String())
		var mask []bool
		if i < len(masks) {
			mask = masks[i]
		}
		parts = append(parts, styleSegments(text, mask, base, matchHighlightStyle.Italic(true)))
	}
	return strings.Join(parts, base.Render("  "))
}

func (m Model) state(sp *spanView) (string, lipgloss.Style) {
	if sp.closed {
		if sp.status == domain.SpanStatusError {
			return "×", lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		}
		return "●", lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	}
	spinner := []string{"◐", "◓", "◑", "◒"}
	i := int(m.now.UnixMilli()/150) % len(spinner)
	return spinner[i], lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Bold(true)
}

func (m Model) duration(sp *spanView) string {
	var d time.Duration
	if !sp.start.IsZero() {
		end := sp.end
		if end.IsZero() {
			end = m.now
		}
		d = end.Sub(sp.start)
	} else if !sp.closed {
		d = m.now.Sub(sp.observed)
		return "~" + formatDuration(d)
	} else {
		return "?"
	}
	return formatDuration(d)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Millisecond:
		return d.Round(time.Microsecond).String()
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < 10*time.Second:
		return d.Round(10 * time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

func spanLabel(sp *spanView) string {
	if sp.component == "" || strings.HasPrefix(sp.name, sp.component+".") {
		return sp.name
	}
	if sp.name == "" {
		return sp.component
	}
	return sp.component + "." + sp.name
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// matchingTraces returns the set of trace IDs to keep when searching in
// searchTraces mode. A trace matches if ANY single span in it fuzzy-matches
// on its own trace ID, label, or attributes — the match is per-span, but the
// result is per-trace: treeLines renders every span, attribute, and log in a
// matched trace unconditionally, not just the span that actually matched. So
// a query can surface an entire tree of unrelated calls off the back of one
// incidental hit deep inside it. See fuzzy for why "misses" are rarer than
// they look, and spanHighlights for how the actually-matching span is
// picked out visually.
func (m Model) matchingTraces() map[string]bool {
	out := make(map[string]bool)
	if m.query == "" {
		return out
	}
	for _, sp := range m.spans {
		if ok, _ := matchSpan(m.query, sp); ok {
			out[sp.traceID] = true
		}
	}
	return out
}

// spanHighlight marks which runes of a span's label and of each attribute's
// "key=value" text were consumed by a fuzzy trace-search match, so the
// renderer can highlight only the characters that actually matched.
type spanHighlight struct {
	label []bool
	attrs [][]bool
}

// matchSpan reports whether a span, judged solely on its own trace ID,
// label, and attributes, satisfies a fuzzy trace-search query — the same
// per-span test matchingTraces uses to decide whether to pull in the whole
// trace — and, when it does, which runes of the label and of each attribute
// were part of the match.
func matchSpan(query string, sp *spanView) (bool, spanHighlight) {
	segments := make([]string, 0, len(sp.attributes)+2)
	segments = append(segments, sp.traceID, spanLabel(sp))
	for _, a := range sp.attributes {
		segments = append(segments, a.Key+"="+a.Value.String())
	}
	ok, mask := fuzzyMatch(query, strings.Join(segments, " "))
	if !ok {
		return false, spanHighlight{}
	}

	pos := 0
	next := func(seg string) []bool {
		n := utf8.RuneCountInString(seg)
		m := append([]bool(nil), mask[pos:pos+n]...)
		pos += n + 1 // +1 for the space joining this segment to the next
		return m
	}
	next(segments[0]) // trace ID isn't rendered in the tree; discard its mask
	h := spanHighlight{label: next(segments[1])}
	for _, seg := range segments[2:] {
		h.attrs = append(h.attrs, next(seg))
	}
	return true, h
}

// spanHighlights computes matchSpan for every span, keyed by trace and span ID, for
// use while rendering a trace search. It is empty outside of a non-empty
// searchTraces query, and only contains entries for spans that individually
// matched — spans merely along for the ride in a matched trace get no
// highlight.
func (m Model) spanHighlights() map[string]spanHighlight {
	out := make(map[string]spanHighlight)
	if m.query == "" || m.mode != searchTraces {
		return out
	}
	for id, sp := range m.spans {
		if ok, h := matchSpan(m.query, sp); ok {
			out[id] = h
		}
	}
	return out
}

func (m Model) matchingLogs() map[string]bool {
	out := make(map[string]bool)
	if m.query == "" {
		return out
	}
	for id, log := range m.logs {
		if fuzzy(m.query, log.body) {
			out[id] = true
		}
	}
	return out
}

func (m Model) branchHasLog(
	sp *spanView,
	children map[string][]*spanView,
	matches map[string]bool,
	seen map[string]bool,
) bool {
	key := selectionKey(sp.traceID, sp.id)
	if seen[key] {
		return false
	}
	seen[key] = true
	for _, id := range sp.logs {
		if matches[id] {
			return true
		}
	}
	for _, child := range children[key] {
		if m.branchHasLog(child, children, matches, seen) {
			return true
		}
	}
	return false
}

// fuzzy is a per-character SUBSEQUENCE match, not a substring match: each
// whitespace-separated word in needle must have its characters appear
// somewhere in haystack in the same relative order, but not necessarily
// adjacent, and case-insensitively. It does not know about "key=value"
// structure — "=" and digits are just characters like any other. That makes
// it forgiving of typos and word order, but also means a query can match by
// stitching characters together across unrelated attributes (e.g. "rand"
// from a span's label, "s" from the tail of "ids=", "=12345" from that same
// attribute's value) even when the literal query string appears nowhere in
// the haystack.
func fuzzy(needle, haystack string) bool {
	ok, _ := fuzzyMatch(needle, haystack)
	return ok
}

// fuzzyMatch is fuzzy plus, on a match, a mask over haystack's runes (same
// length and indexing as []rune(strings.ToLower(haystack))) marking which
// ones were consumed by the match, so callers can highlight exactly the
// characters that made the query hit.
func fuzzyMatch(needle, haystack string) (bool, []bool) {
	needles := strings.Fields(strings.ToLower(needle))
	haystackRunes := []rune(strings.ToLower(haystack))
	mask := make([]bool, len(haystackRunes))
	for _, word := range needles {
		wordRunes := []rune(word)
		at := 0
		for i, r := range haystackRunes {
			if at < len(wordRunes) && r == wordRunes[at] {
				mask[i] = true
				at++
			}
		}
		if at != len(wordRunes) {
			return false, nil
		}
	}
	return true, mask
}

// matchHighlightStyle marks the runes a fuzzy trace search actually matched,
// distinct from the depth, status, and duration colors used elsewhere.
var matchHighlightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)

// styleSegments renders s rune-by-rune, applying hl to the runs where mask
// is true and base everywhere else. A nil mask (no match for this piece)
// renders the whole string in base.
func styleSegments(s string, mask []bool, base, hl lipgloss.Style) string {
	if mask == nil {
		return base.Render(s)
	}
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); {
		matched := i < len(mask) && mask[i]
		j := i
		for j < len(runes) && (j < len(mask) && mask[j]) == matched {
			j++
		}
		seg := string(runes[i:j])
		if matched {
			b.WriteString(hl.Render(seg))
		} else {
			b.WriteString(base.Render(seg))
		}
		i = j
	}
	return b.String()
}

func clean(s string) string {
	// capture already strips ANSI from stdout/stderr, but OTLP log bodies
	// bypass that path, so strip here too rather than leaving stray CSI
	// text (e.g. "[2m") behind when only the ESC byte gets removed.
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func (m Model) panel() string {
	traceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	logStyle := traceStyle
	active := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	if m.mode == searchTraces {
		traceStyle = active
	} else {
		logStyle = active
	}
	state := "running"
	if m.done {
		state = "complete"
	}
	line := fmt.Sprintf(
		" %s  %s   › %s",
		traceStyle.Render("traces"),
		logStyle.Render("logs"),
		m.query,
	)
	help := fmt.Sprintf(
		" tab mode • ↑/↓ scroll • ←/→ pan • esc clear • ctrl+q quit • %s",
		state,
	)
	if m.hscroll > 0 {
		help = fmt.Sprintf(" ◂ panned %d cols • home reset • %s", m.hscroll, help)
	}
	helpColor := lipgloss.Color("241")
	if m.warning != "" {
		help = " " + m.warning + " • " + state
		helpColor = lipgloss.Color("214")
	}
	help = lipgloss.NewStyle().Foreground(helpColor).Render(help)

	return lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(max(1, m.width)).
		Render(line + "\n" + help)
}

// TUI renders the event stream as an interactive live tree.
type TUI struct {
	lt     observability.LoggingTracer
	events <-chan domain.Event
	filt   *filter.Filter
	query  *querylang.Query
	in     io.Reader
	out    io.Writer
}

// New constructs an unconstrained live TUI.
func New(
	lt observability.LoggingTracer,
	events <-chan domain.Event,
	in io.Reader,
	out io.Writer,
	filt *filter.Filter,
) TUI {
	return NewWithQuery(lt, events, in, out, filt, nil)
}

// NewWithQuery constructs a TUI whose tree is constrained by query. Query must
// have been checked before the child starts; nil preserves the unfiltered view.
func NewWithQuery(
	lt observability.LoggingTracer,
	events <-chan domain.Event,
	in io.Reader,
	out io.Writer,
	filt *filter.Filter,
	query *querylang.Query,
) TUI {
	return TUI{lt: lt.Name("tui"), events: events, filt: filt, query: query, in: in, out: out}
}

func (t TUI) Run(ctx context.Context) error {
	_, log, span := t.lt.Start(ctx)
	defer span.End()
	if os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		lipgloss.SetColorProfile(termenv.ANSI256)
	}

	p := tea.NewProgram(
		NewQueryModel(t.events, t.filt, t.query),
		tea.WithContext(ctx),
		tea.WithInput(t.in),
		tea.WithOutput(t.out),
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		log.Error("run tui", "err", err)
		return err
	}
	return nil
}
