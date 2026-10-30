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

// viewMode is orthogonal to searchMode: it picks the full-screen display
// (tree vs. component graph), independent of which fuzzy search
// mode narrows it.
type viewMode uint8

const (
	viewTree viewMode = iota
	viewGraph
)

// componentPalette is handed out, in order and then round again, to each
// component that calls another (see Model.colors). A component that calls
// nothing is drawn in neutralColor.
var componentPalette = []lipgloss.Color{
	lipgloss.Color("39"),
	lipgloss.Color("135"),
	lipgloss.Color("214"),
	lipgloss.Color("42"),
	lipgloss.Color("207"),
}

var neutralColor = lipgloss.Color("240")

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

// eventMsg carries every event that was waiting when the model last asked
// for one, so a burst costs one update and one redraw rather than one per
// event. ok is false once the stream has closed, after events.
type eventMsg struct {
	events []domain.Event
	ok     bool
}

// maxEventBatch caps one eventMsg, so a stream that never pauses still
// gets redrawn now and then.
const maxEventBatch = 4096

type tickMsg time.Time

// Model is the live trace-tree Bubble Tea model.
type Model struct {
	events <-chan domain.Event
	filt   *filter.Filter

	spans     map[string]*spanView
	logs      map[string]*logView
	nextOrder int

	// version counts changes to the telemetry above, so cache can tell
	// whether what it holds was built from what is here now.
	version int
	cache   *viewCache

	// callers holds the selectionKey of every span named as another's
	// parent, whether or not it has arrived yet. colors gives each component
	// (by spanLabel) that has such a span a 1-based index into
	// componentPalette, in order of first appearance; once given it is
	// never changed, so a component looks the same in the tree and the
	// graph, across traces, and however the view is filtered.
	callers map[string]bool
	colors  map[string]int

	mode     searchMode
	view     viewMode
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

// wheelStep is how many rows one notch of the mouse wheel scrolls.
const wheelStep = 3

// NewModel constructs a model. It is exported for embedding and deterministic
// tests.
func NewModel(events <-chan domain.Event, filt *filter.Filter) Model {
	if filt == nil {
		filt, _ = filter.New(filter.Query{})
	}

	return Model{
		events:  events,
		filt:    filt,
		spans:   make(map[string]*spanView),
		logs:    make(map[string]*logView),
		callers: make(map[string]bool),
		colors:  make(map[string]int),
		cache:   &viewCache{},
		width:   80,
		height:  24,
		now:     time.Now(),
	}
}

// Init begins waiting for telemetry and animation ticks.
func (m Model) Init() tea.Cmd {
	return tea.Batch(waitEvent(m.events), tick())
}

func waitEvent(events <-chan domain.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return eventMsg{}
		}
		batch := []domain.Event{ev}
		for len(batch) < maxEventBatch {
			select {
			case ev, ok := <-events:
				if !ok {
					return eventMsg{events: batch}
				}
				batch = append(batch, ev)
			default:
				return eventMsg{events: batch, ok: true}
			}
		}
		return eventMsg{events: batch, ok: true}
	}
}

// tickInterval is how often running spans' spinners and durations move on.
const tickInterval = 100 * time.Millisecond

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update applies one Bubble Tea message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if layout := next.relayoutGraph(); layout != nil {
		cmd = tea.Batch(cmd, layout)
	}
	return next, cmd
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case graphLaidOutMsg:
		g := &m.cache.graph
		g.key, g.ok, g.at, g.cost, g.val = msg.key, true, m.now, msg.cost, msg.layout
		g.pending = false
	case eventMsg:
		for _, ev := range msg.events {
			m.apply(ev)
		}
		if !msg.ok {
			m.done = true
			return m, nil
		}
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
		m.clampScroll()
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
		case "ctrl+g":
			if m.view == viewTree {
				m.view = viewGraph
			} else {
				m.view = viewTree
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
			m.scrollBy(-1)
		case "down", "ctrl+j":
			m.scrollBy(1)
		case "pgup":
			m.scrollBy(-max(1, m.contentHeight()-1))
		case "pgdown":
			m.scrollBy(max(1, m.contentHeight()-1))
		case "left", "ctrl+b":
			m.panBy(-hscrollStep)
		case "right", "ctrl+f":
			m.panBy(hscrollStep)
		case "home":
			m.hscroll = 0
		default:
			if msg.Type == tea.KeyRunes {
				m.query += string(msg.Runes)
				m.scroll = 0
			}
		}
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			break
		}
		// Shift turns the vertical wheel sideways, for mice and terminals
		// that have no horizontal wheel of their own.
		switch {
		case msg.Button == tea.MouseButtonWheelUp && msg.Shift,
			msg.Button == tea.MouseButtonWheelLeft:
			m.panBy(-hscrollStep)
		case msg.Button == tea.MouseButtonWheelDown && msg.Shift,
			msg.Button == tea.MouseButtonWheelRight:
			m.panBy(hscrollStep)
		case msg.Button == tea.MouseButtonWheelUp:
			m.scrollBy(-wheelStep)
		case msg.Button == tea.MouseButtonWheelDown:
			m.scrollBy(wheelStep)
		}
	}

	return m, nil
}

// scrollBy moves the view n rows down, or up for a negative n, stopping at
// either end.
func (m *Model) scrollBy(n int) {
	m.scroll += n
	m.clampScroll()
}

// panBy moves the view n columns right, or left for a negative n. Only the
// left edge is enforced here; View stops it at the widest line.
func (m *Model) panBy(n int) {
	m.hscroll = max(0, m.hscroll+n)
}

func (m *Model) apply(ev domain.Event) {
	m.version++
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
		m.colorCallers(sp)
	case domain.SpanParentLinked:
		sp := m.ensureSpan(ev.TraceID, ev.SpanID)
		sp.parentID = ev.ParentSpanID
		m.colorCallers(sp)
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

// colorCallers gives a color to sp's component if sp is known to be some
// span's parent, and to its own parent's component, now that sp names it as
// one. A declared placeholder is skipped until it hydrates with its real
// name, so the placeholder name doesn't use up a color.
func (m *Model) colorCallers(sp *spanView) {
	assign := func(caller *spanView) {
		if caller == nil || caller.declared {
			return
		}
		if label := spanLabel(caller); m.colors[label] == 0 {
			m.colors[label] = len(m.colors)%len(componentPalette) + 1
		}
	}
	if m.callers[selectionKey(sp.traceID, sp.id)] {
		assign(sp)
	}
	if sp.parentID != "" {
		parentKey := selectionKey(sp.traceID, sp.parentID)
		m.callers[parentKey] = true
		assign(m.spans[parentKey])
	}
}

// componentColor is the color everything belonging to sp's component is
// drawn in.
func (m Model) componentColor(sp *spanView) lipgloss.Color {
	if i := m.colors[spanLabel(sp)]; i > 0 {
		return componentPalette[(i-1)%len(componentPalette)]
	}
	return neutralColor
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
		delete(m.callers, id)
	}
}

// View renders the current tree and interaction panel.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	// Only the rows on screen are rendered: building every line of a large
	// trace just to show a screenful of it is what made each redraw cost
	// more the more there was to scroll through.
	height := m.contentHeight()
	width := max(1, m.width)
	m.clampScroll()
	var visible []string
	if m.view == viewGraph {
		// Every row of the graph is as wide as the graph, and it is cut to
		// the screen as it is drawn.
		graph := m.cachedGraph()
		m.hscroll = max(0, min(m.hscroll, graph.columns()-width))
		visible = graph.window(m.scroll, m.scroll+height, m.hscroll, width)
	} else {
		visible = m.cachedTree().rows(m, m.scroll, m.scroll+height)
		maxHScroll := 0
		for _, l := range visible {
			if w := ansi.StringWidth(l) - width; w > maxHScroll {
				maxHScroll = w
			}
		}
		if m.hscroll > maxHScroll {
			m.hscroll = maxHScroll
		}
		for i, l := range visible {
			visible[i] = ansi.Cut(l, m.hscroll, m.hscroll+width)
		}
	}
	for len(visible) < height {
		visible = append(visible, "")
	}

	return strings.Join(visible, "\n") + "\n" + m.panel()
}

func (m Model) contentHeight() int {
	return max(1, m.height-3)
}

// clampScroll keeps the vertical scroll within what there is to show, so
// holding ↓ at the bottom doesn't run up a count that ↑ then has to work
// back through before anything moves.
func (m *Model) clampScroll() {
	var lines int
	if m.view == viewGraph {
		lines = m.cachedGraph().lineCount()
	} else {
		lines = m.cachedTree().lines
	}
	m.scroll = max(0, min(m.scroll, lines-m.contentHeight()))
}

// viewCache keeps the expensive part of each view -- the tree's layout,
// the graph's whole canvas -- between redraws, which happen on every key
// press and animation tick whether or not anything they show has changed.
// It is shared by every copy of a Model, and each entry knows which state
// it was built from.
type viewCache struct {
	tree  cached[*treeLayout]
	graph cached[*graphLayout]
}

type cacheKey struct {
	version int
	query   string
	mode    searchMode
}

type cached[T any] struct {
	key  cacheKey
	ok   bool
	at   time.Time     // the model's clock when it was built
	cost time.Duration // how long building it took
	val  T

	pending bool // a rebuild is under way in the background
}

func (m Model) cacheKey() cacheKey {
	return cacheKey{version: m.version, query: m.query, mode: m.mode}
}

// slowBuild is how long a build has to take before the result is reused
// for a while after the telemetry it shows has moved on.
const slowBuild = 10 * time.Millisecond

// get returns the value built from m's current state, building it if
// needed. While telemetry is still streaming in, a build that was slow is
// reused, out of date, until a tick or so has passed -- longer still, the
// slower it was -- so that a large view keeps up with the keyboard instead
// of rebuilding itself for every few events. A new search, or the end of
// the stream, always rebuilds at once.
func (c *cached[T]) get(m Model, build func() T) T {
	key := m.cacheKey()
	if c.ok && c.key != key {
		sameSearch := c.key.query == key.query && c.key.mode == key.mode
		if sameSearch && !m.done && c.cost >= slowBuild &&
			m.now.Sub(c.at) < max(tickInterval, 4*c.cost) {
			return c.val
		}
	}
	if !c.ok || c.key != key {
		started := time.Now()
		c.val = build()
		c.key, c.ok, c.at, c.cost = key, true, m.now, time.Since(started)
	}
	return c.val
}

func (m Model) cachedTree() *treeLayout {
	if m.cache == nil {
		return m.layoutTree()
	}
	return m.cache.tree.get(m, m.layoutTree)
}

// cachedGraph is the graph to draw: the latest layout, unless laying it
// out is slow enough to be done in the background (see relayoutGraph), in
// which case it is the last one that finished.
func (m Model) cachedGraph() *graphLayout {
	if m.cache == nil {
		return m.layoutGraphView()
	}
	g := &m.cache.graph
	switch {
	case g.ok && (g.pending || g.cost >= slowBuild):
		return g.val
	case g.pending:
		return &graphLayout{placeholder: lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Render("  Laying out the graph…")}
	}
	return g.get(m, m.layoutGraphView)
}

// graphLaidOutMsg is a layout relayoutGraph started, finished.
type graphLaidOutMsg struct {
	key    cacheKey
	layout *graphLayout
	cost   time.Duration
}

// relayoutGraph starts laying the graph out again in the background, if
// it is on show, out of date, and either too slow to lay out in between
// key presses or not laid out yet -- in which case how slow it will be
// isn't known. A layout of a large graph takes long enough to stall the
// whole interface; this way the graph on show is a little behind instead.
func (m Model) relayoutGraph() tea.Cmd {
	if m.cache == nil || m.view != viewGraph {
		return nil
	}
	g := &m.cache.graph
	key := m.cacheKey()
	if g.pending || (g.ok && (g.key == key || g.cost < slowBuild)) {
		return nil
	}
	g.pending = true
	// The graph is put together from the model here, while nothing else is
	// changing it; only laying it out is left to the background.
	graph, highlights := m.graphToLayOut()
	placeholder := m.treePlaceholder()
	return func() tea.Msg {
		started := time.Now()
		layout := layoutGraphOf(graph, highlights, placeholder)
		return graphLaidOutMsg{key: key, layout: layout, cost: time.Since(started)}
	}
}

// filteredForest applies the glob focus/exclude filter (m.filt.InFocus) to
// m.spans -- but NOT the live fuzzy search, which fuzzyVisible applies separately since its
// mode-dependent semantics (whole-trace vs. per-branch) differ from simple
// inclusion. layoutTree and buildGraph both call this so they can never
// disagree about what's in scope.
func (m Model) filteredForest() (
	children map[string][]*spanView,
	roots []*spanView,
	inScope map[string]*spanView,
) {
	children = make(map[string][]*spanView)
	inScope = make(map[string]*spanView)
	for _, sp := range m.spans {
		if !m.filt.InFocus(sp.id) {
			continue
		}
		inScope[selectionKey(sp.traceID, sp.id)] = sp
		parent := m.spans[selectionKey(sp.traceID, sp.parentID)]
		if sp.parentID != "" && parent != nil && m.filt.InFocus(parent.id) {
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
	return children, roots, inScope
}

// fuzzyVisible reports whether sp should be considered "in view" under the
// current live search (m.mode, m.query): whole-trace in searchTraces mode
// (any span in the trace matched), or per-branch in searchLogs mode (sp or
// a descendant has a matching log; see logBranches). Always true when there
// is no live query.
func (m Model) fuzzyVisible(sp *spanView, traceMatches, logBranches map[string]bool) bool {
	if m.query == "" {
		return true
	}
	switch m.mode {
	case searchTraces:
		return traceMatches[sp.traceID]
	case searchLogs:
		return logBranches[selectionKey(sp.traceID, sp.id)]
	}
	return true
}

// treeLayout is the tree view worked out as far as which line each span
// starts on, but not styled. Styling is most of what drawing the tree
// costs, and only a screenful of it is ever on show (see rows).
type treeLayout struct {
	spans      []treeSpan
	lines      int
	highlights map[string]spanHighlight
}

// treeSpan is one span's place in the tree view.
type treeSpan struct {
	sp        *spanView
	ancestors []lipgloss.Color // each enclosing span's component color, outermost first
	logs      []*logView       // those of its logs on show
	line      int              // the line its header is on
}

// height is how many lines s takes up: its header, one per attribute, its
// logs and the rule under it.
func (s treeSpan) height() int {
	return 2 + len(s.sp.attributes) + len(s.logs)
}

func (m Model) layoutTree() *treeLayout {
	children, roots, inScope := m.filteredForest()
	traceMatches := m.matchingTraces()
	logMatches := m.matchingLogs()
	logBranches := m.logBranches(children, inScope, logMatches)
	t := &treeLayout{highlights: m.spanHighlights()}

	visited := make(map[string]bool)
	var walk func(sp *spanView, ancestors []lipgloss.Color)
	walk = func(sp *spanView, ancestors []lipgloss.Color) {
		key := selectionKey(sp.traceID, sp.id)
		if visited[key] {
			return
		}
		visited[key] = true
		if m.mode == searchLogs && !m.fuzzyVisible(sp, nil, logBranches) {
			return
		}
		s := treeSpan{sp: sp, ancestors: ancestors, line: t.lines}
		for _, id := range sp.logs {
			log := m.logs[id]
			if log == nil || (m.mode == searchLogs && m.query != "" && !logMatches[id]) {
				continue
			}
			s.logs = append(s.logs, log)
		}
		t.spans = append(t.spans, s)
		t.lines += s.height()
		trail := append(ancestors[:len(ancestors):len(ancestors)], m.componentColor(sp))
		for _, child := range children[key] {
			walk(child, trail)
		}
	}
	for _, root := range roots {
		if m.fuzzyVisible(root, traceMatches, logBranches) {
			walk(root, nil)
		}
	}
	if t.lines > 0 {
		t.lines-- // nothing follows the last span's rule
	} else {
		t.lines = 1 // the placeholder
	}
	return t
}

// rows renders lines [from, to) of the tree, styling only the spans that
// have a line in that range.
func (t *treeLayout) rows(m Model, from, to int) []string {
	to = min(to, t.lines)
	from = max(0, min(from, to))
	if len(t.spans) == 0 {
		return []string{m.treePlaceholder()}[from:to]
	}
	i := sort.Search(len(t.spans), func(i int) bool {
		return t.spans[i].line+t.spans[i].height() > from
	})
	out := make([]string, 0, to-from)
	for ; i < len(t.spans) && t.spans[i].line < to; i++ {
		s := t.spans[i]
		lines := m.renderSpan(s, t.highlights[selectionKey(s.sp.traceID, s.sp.id)])
		out = append(out, lines[max(0, from-s.line):min(len(lines), to-s.line)]...)
	}
	return out
}

func (m Model) treePlaceholder() string {
	text := "  Waiting for traces and logs…"
	if m.query != "" {
		text = "  No matches"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(text)
}

// treeLines renders the whole tree.
func (m Model) treeLines() []string {
	t := m.layoutTree()
	return t.rows(m, 0, t.lines)
}

func selectionKey(traceID, spanID string) string { return traceID + "\x00" + spanID }

// renderSpan draws one span's lines: its header, attributes, logs and rule.
func (m Model) renderSpan(s treeSpan, h spanHighlight) []string {
	sp, ancestors := s.sp, s.ancestors
	trail := append(ancestors[:len(ancestors):len(ancestors)], m.componentColor(sp))
	prefix := bars(trail, sp.closed)
	// The status symbol takes the span's own column on its header row: the
	// top cell of its bar, or the cell left blank for a span with no bar.
	symbol, style := m.state(sp)
	header := fmt.Sprintf(
		"%s%s %s %s",
		bars(ancestors, false),
		style.Render(symbol),
		styleSegments(spanLabel(sp), h.label, lipgloss.NewStyle(), matchHighlightStyle),
		m.duration(sp),
	)
	lines := make([]string, 0, s.height())
	lines = append(lines, header)
	for _, row := range attributeRows(sp.attributes, h.attrs) {
		lines = append(lines, prefix+detailIndent+row)
	}
	for _, log := range s.logs {
		body := clean(log.body)
		line := prefix + detailIndent +
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Render(body)
		lines = append(lines, line)
	}
	return append(lines, m.spanRule(trail, sp.closed))
}

// detailIndent sets a span's attributes and logs one stop in from its
// header, so the header is the only thing on the span's own bar.
const detailIndent = "   "

// minSpanRuleWidth keeps the rule visible before the first window size
// message arrives and at depths that would otherwise run it off-screen.
const minSpanRuleWidth = 8

// bars draws one vertical bar per span on the path from the root down to
// the span being rendered -- trail holds their component colors, the span's
// own last -- so each ancestor's color runs unbroken down the left of
// everything it encloses. A span whose component calls nothing encloses
// nothing, so its column is left blank rather than drawn in neutralColor.
func bars(trail []lipgloss.Color, closed bool) string {
	var b strings.Builder
	for i, color := range trail {
		bar := "┆"
		if closed && i == len(trail)-1 {
			bar = "┃"
		}
		if color == neutralColor {
			b.WriteString("  ")
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(color).Render(bar))
		b.WriteByte(' ')
	}
	return b.String()
}

// spanRule is the line drawn under a span -- its header, attributes and
// logs -- to set it apart from whatever follows. It is in the span's own
// color and sits inside its bars rather than interrupting them, so the
// span's bar and its rule read as one bracket around what the span owns. A
// span whose component calls nothing owns nothing, and gets only the blank
// row, still crossed by its ancestors' bars.
func (m Model) spanRule(trail []lipgloss.Color, closed bool) string {
	own := trail[len(trail)-1]
	if own == neutralColor {
		return bars(trail, closed)
	}
	width := max(minSpanRuleWidth, m.width-2*len(trail))
	return bars(trail, closed) +
		lipgloss.NewStyle().
			Foreground(own).
			Render(strings.Repeat("─", width))
}

// maxAttributeKeyWidth caps the key column of a span's attribute table, so
// one long key doesn't push every value off to the right; a key longer than
// this runs into its own value's column alone.
const maxAttributeKeyWidth = 32

// attributeRows renders a span's attributes as a two-column table, one
// attribute per row with the keys padded to a shared width, so many or long
// attributes stay readable. Keys are faint and italic so the table reads as
// metadata attached to the header above rather than as a log entry; values
// are left in the terminal's own color. masks, when non-nil, highlights the
// runes of the matching attribute(s) that made the span match the current
// trace search (see matchSpan, whose masks cover "key=value"); it is nil
// outside of a search or for spans not shown for their own sake.
//
// Render is costly next to everything else here, and only a screenful of
// rows is ever drawn per frame, so each row costs one: the key, its padding
// and the column separator share a style, and the value needs none unless
// part of it matched.
func attributeRows(attrs []domain.Attr, masks [][]bool) []string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)

	keys := make([]string, len(attrs))
	widths := make([]int, len(attrs))
	keyWidth := 0
	for i, a := range attrs {
		keys[i] = clean(a.Key)
		widths[i] = ansi.StringWidth(keys[i])
		keyWidth = max(keyWidth, min(maxAttributeKeyWidth, widths[i]))
	}
	rows := make([]string, 0, len(attrs))
	for i, a := range attrs {
		key := keys[i] + strings.Repeat(" ", max(0, keyWidth-widths[i])) + " │ "
		value := clean(a.Value.String())
		if i >= len(masks) || masks[i] == nil {
			rows = append(rows, keyStyle.Render(key)+value)
			continue
		}
		n := min(utf8.RuneCountInString(a.Key), len(masks[i]))
		rows = append(rows,
			styleSegments(key, masks[i][:n], keyStyle, matchHighlightStyle.Italic(true))+
				styleSegments(value, masks[i][min(n+1, len(masks[i])):], lipgloss.NewStyle(), matchHighlightStyle)) // skip the "="
	}
	return rows
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

// logBranches returns, for a log search, the key of every span with a
// matching log of its own or anywhere beneath it, and nil otherwise.
func (m Model) logBranches(
	children map[string][]*spanView,
	inScope map[string]*spanView,
	logMatches map[string]bool,
) map[string]bool {
	if m.query == "" || m.mode != searchLogs {
		return nil
	}
	out := make(map[string]bool)
	seen := make(map[string]bool)
	var has func(sp *spanView) bool
	has = func(sp *spanView) bool {
		key := selectionKey(sp.traceID, sp.id)
		if seen[key] {
			return out[key]
		}
		seen[key] = true
		for _, id := range sp.logs {
			if logMatches[id] {
				out[key] = true
			}
		}
		for _, child := range children[key] {
			if has(child) {
				out[key] = true
			}
		}
		return out[key]
	}
	for _, sp := range inScope {
		has(sp)
	}
	return out
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
	viewLabel := "tree"
	if m.view == viewGraph {
		viewLabel = "graph"
	}
	viewStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("135")).Bold(true)
	state := "running"
	if m.done {
		state = "complete"
	}
	line := fmt.Sprintf(
		" %s  %s   %s   › %s",
		traceStyle.Render("traces"),
		logStyle.Render("logs"),
		viewStyle.Render(viewLabel),
		m.query,
	)
	help := fmt.Sprintf(
		" tab mode • ctrl+g graph • ↑/↓ scroll • ←/→ pan • esc clear • ctrl+q quit • %s",
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

	// Each line is cut to the screen rather than left to wrap: View leaves
	// the panel exactly three rows, and a fourth would push the top of the
	// tree off the screen.
	width := max(1, m.width)
	return lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(width).
		Render(ansi.Truncate(line, width, "…") + "\n" + ansi.Truncate(help, width, "…"))
}

// TUI renders the event stream as an interactive live tree.
type TUI struct {
	lt     observability.LoggingTracer
	events <-chan domain.Event
	filt   *filter.Filter
	in     io.Reader
	out    io.Writer
}

// New constructs a TUI over the event stream. A nil filt shows everything.
func New(
	lt observability.LoggingTracer,
	events <-chan domain.Event,
	in io.Reader,
	out io.Writer,
	filt *filter.Filter,
) TUI {
	return TUI{lt: lt.Name("tui"), events: events, filt: filt, in: in, out: out}
}

func (t TUI) Run(ctx context.Context) error {
	_, log, span := t.lt.Start(ctx)
	defer span.End()
	if os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		lipgloss.SetColorProfile(termenv.ANSI256)
	}

	p := tea.NewProgram(
		NewModel(t.events, t.filt),
		tea.WithContext(ctx),
		tea.WithInput(t.in),
		tea.WithOutput(t.out),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		log.Error("run tui", "err", err)
		return err
	}
	return nil
}
