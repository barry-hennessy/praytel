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
	id     string
	body   string
	spanID string
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

	spans     map[string]*spanView
	logs      map[string]*logView
	nextOrder int

	mode     searchMode
	query    string
	width    int
	height   int
	scroll   int
	now      time.Time
	done     bool
	warning  string
	quitting bool
}

// NewModel constructs a model. It is exported for embedding and deterministic
// tests.
func NewModel(events <-chan domain.Event, filt *filter.Filter) Model {
	if filt == nil {
		filt, _ = filter.New(filter.Query{})
	}

	return Model{
		events: events,
		filt:   filt,
		spans:  make(map[string]*spanView),
		logs:   make(map[string]*logView),
		width:  80,
		height: 24,
		now:    time.Now(),
	}
}

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
		case "esc":
			m.query = ""
			m.scroll = 0
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
	m.filt.Observe(ev)
	switch ev := ev.(type) {
	case domain.SpanUpserted:
		sp := m.ensureSpan(ev.SpanID)
		sp.traceID = ev.TraceID
		sp.name = ev.Name
		sp.component = ev.Component
		sp.declared = false
		sp.start = ev.StartTime
		sp.end = ev.EndTime
		sp.status = ev.Status
		sp.attributes = ev.Attributes
	case domain.SpanParentLinked:
		m.ensureSpan(ev.SpanID).parentID = ev.ParentSpanID
	case domain.SpanClosed:
		m.ensureSpan(ev.SpanID).closed = true
	case domain.LogReceived:
		m.nextOrder++
		spanID := ev.SpanID
		if spanID == "" {
			spanID = ev.DeclaredSpanID
		}
		m.logs[ev.ID] = &logView{id: ev.ID, body: ev.Body, spanID: spanID}
		if ev.SpanID == "" && ev.DeclaredSpanID != "" {
			sp := m.ensureSpan(ev.DeclaredSpanID)
			sp.traceID = ev.DeclaredTraceID
			sp.component = ev.Component
			sp.name = "span/" + shortID(ev.DeclaredSpanID)
			sp.declared = true
		}
		m.attachLog(ev.ID, "", spanID)
	case domain.LogAssigned:
		log := m.logs[ev.ID]
		if log != nil {
			old := log.spanID
			log.spanID = ev.SpanID
			m.attachLog(ev.ID, old, ev.SpanID)
			sp := m.spans[old]
			if old != ev.SpanID && sp != nil &&
				sp.declared && len(sp.logs) == 0 {
				delete(m.spans, old)
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

func (m *Model) ensureSpan(id string) *spanView {
	if sp := m.spans[id]; sp != nil {
		return sp
	}
	m.nextOrder++
	sp := &spanView{id: id, observed: m.now, order: m.nextOrder}
	m.spans[id] = sp
	return sp
}

func (m *Model) attachLog(id, oldSpanID, spanID string) {
	if oldSpanID != "" && oldSpanID != spanID {
		old := m.spans[oldSpanID]
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
	sp := m.ensureSpan(spanID)
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
	for len(visible) < height {
		visible = append(visible, "")
	}

	return strings.Join(visible, "\n") + "\n" + m.panel()
}

func (m Model) contentHeight() int {
	return max(1, m.height-3)
}

func (m Model) treeLines() []string {
	children := make(map[string][]*spanView)
	var roots []*spanView
	for _, sp := range m.spans {
		if !m.filt.InFocus(sp.id) {
			continue
		}
		parent := m.spans[sp.parentID]
		if sp.parentID != "" && parent != nil &&
			m.filt.InFocus(parent.id) {
			children[sp.parentID] = append(children[sp.parentID], sp)
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

	traceMatches := m.matchingTraces()
	logMatches := m.matchingLogs()
	var lines []string
	visited := make(map[string]bool)
	for _, root := range roots {
		if m.query != "" {
			if m.mode == searchTraces && !traceMatches[root.traceID] {
				continue
			}
			if m.mode == searchLogs && !m.branchHasLog(
				root,
				children,
				logMatches,
				make(map[string]bool),
			) {
				continue
			}
		}
		m.renderSpan(&lines, root, 0, children, logMatches, visited)
	}
	if len(lines) == 0 {
		if m.query == "" {
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

func (m Model) renderSpan(
	lines *[]string,
	sp *spanView,
	depth int,
	children map[string][]*spanView,
	logMatches map[string]bool,
	visited map[string]bool,
) {
	if visited[sp.id] {
		return
	}
	visited[sp.id] = true
	if m.mode == searchLogs && m.query != "" && !m.branchHasLog(
		sp,
		children,
		logMatches,
		make(map[string]bool),
	) {
		return
	}

	prefix := m.bars(depth, sp.closed)
	symbol, style := m.state(sp)
	header := fmt.Sprintf(
		"%s%s %s %s",
		prefix,
		spanLabel(sp),
		style.Render(symbol),
		m.duration(sp),
	)
	*lines = append(*lines, header)
	if len(sp.attributes) > 0 {
		*lines = append(*lines, m.bars(depth, sp.closed)+attributesLine(sp.attributes))
	}
	for _, id := range sp.logs {
		log := m.logs[id]
		if log == nil || (m.mode == searchLogs && m.query != "" && !logMatches[id]) {
			continue
		}
		body := clean(log.body)
		line := m.bars(depth, sp.closed) + "   " +
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Render(body)
		*lines = append(*lines, line)
	}
	for _, child := range children[sp.id] {
		m.renderSpan(lines, child, depth+1, children, logMatches, visited)
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
// entry (which is plain-styled and indented one level further in).
func attributesLine(attrs []domain.Attr) string {
	parts := make([]string, 0, len(attrs))
	for _, a := range attrs {
		parts = append(parts, clean(a.Key)+"="+clean(a.Value))
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true).
		Render(strings.Join(parts, "  "))
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

func (m Model) matchingTraces() map[string]bool {
	out := make(map[string]bool)
	if m.query == "" {
		return out
	}
	for _, sp := range m.spans {
		if fuzzy(m.query, sp.traceID+" "+spanLabel(sp)) {
			out[sp.traceID] = true
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
	if seen[sp.id] {
		return false
	}
	seen[sp.id] = true
	for _, id := range sp.logs {
		if matches[id] {
			return true
		}
	}
	for _, child := range children[sp.id] {
		if m.branchHasLog(child, children, matches, seen) {
			return true
		}
	}
	return false
}

func fuzzy(needle, haystack string) bool {
	needles := strings.Fields(strings.ToLower(needle))
	haystackRunes := []rune(strings.ToLower(haystack))
	for _, word := range needles {
		wordRunes := []rune(word)
		at := 0
		for _, r := range haystackRunes {
			if at < len(wordRunes) && r == wordRunes[at] {
				at++
			}
		}
		if at != len(wordRunes) {
			return false
		}
	}
	return true
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
		" tab mode • ↑/↓ scroll • esc clear • ctrl+q quit • %s",
		state,
	)
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
	in     io.Reader
	out    io.Writer
}

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
	)
	if _, err := p.Run(); err != nil {
		log.Error("run tui", "err", err)
		return err
	}
	return nil
}
