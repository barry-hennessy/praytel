package correlator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
)

const (
	globalOrphanName = "orphan"

	DefaultMaxSpans    = 1_000_000
	DefaultMaxLogLines = 1_000_000
)

// Drain is the post-child wait: idle with no inbound, bounded by Timeout.
// Zero Idle skips the wait. Timeout is a hard cap from Stop; if unset, Idle is used.
type Drain struct {
	Idle    time.Duration
	Timeout time.Duration
}

// Limits bound correlator-retained spans and log lines (pending and orphans included).
type Limits struct {
	MaxSpans    int
	MaxLogLines int
}

// Correlator owns the private forest and outbound events. See docs/guides/architecture.md.
type Correlator struct {
	lt    observability.LoggingTracer
	out   chan<- domain.Event
	logs  chan domain.LogLine
	spans chan domain.Span
	stop  chan struct{}

	stopOnce sync.Once
	outOnce  sync.Once

	drain      Drain
	limits     Limits
	dropIngest func()

	forest   forest
	logSeq   uint64
	traceSeq uint64
}

type forest struct {
	traces       map[string]*traceNode
	spans        map[domain.SpanKey]*forestSpan
	pending      map[domain.SpanKey][]domain.LogLine
	globalOrphan *forestSpan

	// logLines counts the lines in pending and in every span's logs, so
	// checking the limit doesn't mean counting them all on every arrival.
	logLines int
	// unlinked holds each span not yet linked to its parent, by the parent
	// span ID it names, so a span arriving can find the children waiting
	// for it without going through every span.
	unlinked map[string]map[*forestSpan]struct{}
}

func (f *forest) awaitParent(sp *forestSpan) {
	if sp.linked || sp.parentSpanID == "" {
		return
	}
	waiting := f.unlinked[sp.parentSpanID]
	if waiting == nil {
		waiting = make(map[*forestSpan]struct{})
		f.unlinked[sp.parentSpanID] = waiting
	}
	waiting[sp] = struct{}{}
}

func (f *forest) stopAwaitingParent(sp *forestSpan) {
	waiting := f.unlinked[sp.parentSpanID]
	delete(waiting, sp)
	if len(waiting) == 0 {
		delete(f.unlinked, sp.parentSpanID)
	}
}

type traceNode struct {
	orphan   *forestSpan
	seq      uint64
	closed   bool
	openReal int
}

type forestSpan struct {
	traceID       string
	spanID        string
	parentSpanID  string
	name          string
	component     string
	synthetic     bool
	linked        bool
	ended         bool
	startTime     time.Time
	endTime       time.Time
	status        domain.SpanStatus
	kind          domain.SpanKind
	statusMessage string
	resource      domain.Resource
	scope         domain.Scope
	attributes    []domain.Attr
	events        []domain.SpanEvent
	links         []domain.SpanLink
	logs          []domain.LogLine
}

func New(lt observability.LoggingTracer, out chan<- domain.Event, drain Drain, limits Limits) *Correlator {
	if limits.MaxSpans <= 0 {
		limits.MaxSpans = DefaultMaxSpans
	}
	if limits.MaxLogLines <= 0 {
		limits.MaxLogLines = DefaultMaxLogLines
	}

	return &Correlator{
		lt:     lt.Name("correlator"),
		out:    out,
		logs:   make(chan domain.LogLine, 64),
		spans:  make(chan domain.Span, 64),
		stop:   make(chan struct{}),
		drain:  drain,
		limits: limits,
		forest: forest{
			traces:   make(map[string]*traceNode),
			spans:    make(map[domain.SpanKey]*forestSpan),
			pending:  make(map[domain.SpanKey][]domain.LogLine),
			unlinked: make(map[string]map[*forestSpan]struct{}),
		},
	}
}

// DropIngest is called once drain finishes, before Run returns, so late OTLP is not blocked on unread channels.
func (c *Correlator) DropIngest(fn func()) {
	c.dropIngest = fn
}

func (c *Correlator) Logs() chan<- domain.LogLine {
	return c.logs
}

func (c *Correlator) Spans() chan<- domain.Span {
	return c.spans
}

func (c *Correlator) Run(ctx context.Context) error {
	ctx, _, span := c.lt.Start(ctx)
	defer span.End()

	defer c.closeOut()

	logs := c.logs
	spans := c.spans

	for {
		select {
		case <-ctx.Done():
			c.finishDrain(ctx, logs, spans)

			return nil
		case <-c.stop:
			c.waitIdleOrCap(ctx, logs, spans)

			return nil
		case line, ok := <-logs:
			if !ok {
				c.waitIdleOrCap(ctx, nil, spans)

				return nil
			}

			c.handleLog(ctx, line)
		case sp, ok := <-spans:
			if !ok {
				spans = nil
				continue
			}

			c.handleSpan(ctx, sp)
		}
	}
}

func (c *Correlator) Stop() {
	c.stopOnce.Do(func() {
		close(c.stop)
	})
}

func (c *Correlator) closeOut() {
	c.outOnce.Do(func() {
		close(c.out)
	})
}

func (c *Correlator) waitIdleOrCap(ctx context.Context, logs <-chan domain.LogLine, spans <-chan domain.Span) {
	if c.drain.Idle <= 0 {
		c.finishDrain(ctx, logs, spans)
		return
	}

	idle := c.drain.Idle
	hard := c.drain.Timeout
	if hard <= 0 {
		hard = idle
	}

	hardTimer := time.NewTimer(hard)
	defer hardTimer.Stop()

	idleTimer := time.NewTimer(idle)
	defer idleTimer.Stop()

	resetIdle := func() {
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(idle)
	}

	for {
		select {
		case <-ctx.Done():
			c.finishDrain(ctx, logs, spans)
			return
		case <-hardTimer.C:
			c.finishDrain(ctx, logs, spans)
			return
		case <-idleTimer.C:
			c.finishDrain(ctx, logs, spans)
			return
		case line, ok := <-logs:
			if !ok {
				logs = nil
				if spans == nil {
					c.finishDrain(ctx, logs, spans)
					return
				}
				continue
			}

			c.handleLog(ctx, line)
			resetIdle()
		case sp, ok := <-spans:
			if !ok {
				spans = nil
				if logs == nil {
					c.finishDrain(ctx, logs, spans)
					return
				}
				continue
			}

			c.handleSpan(ctx, sp)
			resetIdle()
		}
	}
}

func (c *Correlator) finishDrain(ctx context.Context, logs <-chan domain.LogLine, spans <-chan domain.Span) {
	c.drainInbound(ctx, logs, spans)
	if c.dropIngest != nil {
		c.dropIngest()
	}
	c.drainInbound(ctx, logs, spans)
	c.resolvePending(ctx)
	c.emit(ctx, domain.DrainComplete{})
}

func (c *Correlator) drainInbound(ctx context.Context, logs <-chan domain.LogLine, spans <-chan domain.Span) {
	for {
		select {
		case sp, ok := <-spans:
			if !ok {
				spans = nil
				if logs == nil {
					return
				}

				continue
			}

			c.handleSpan(ctx, sp)
		case line, ok := <-logs:
			if !ok {
				logs = nil
				if spans == nil {
					return
				}

				continue
			}

			c.handleLog(ctx, line)
		default:
			return
		}
	}
}

func (c *Correlator) handleLog(ctx context.Context, line domain.LogLine) {
	id := line.ID
	if id == "" {
		c.logSeq++
		id = strconv.FormatUint(c.logSeq, 10)
	}

	line.ID = id

	needSpans := 0
	if line.SpanID == "" {
		if line.TraceID != "" {
			if tr, ok := c.forest.traces[line.TraceID]; ok {
				if tr.orphan == nil {
					needSpans = 1
				}
			} else if c.forest.globalOrphan == nil {
				needSpans = 1
			}
		} else if c.forest.globalOrphan == nil {
			needSpans = 1
		}
	}

	if !c.admit(ctx, needSpans, 1) {
		return
	}
	c.forest.logLines++ // every way on from here keeps the line, on a span or pending

	if line.SpanID != "" {
		if sp := c.findSpan(line.TraceID, line.SpanID); sp != nil {
			sp.logs = append(sp.logs, line)
			c.emitReceivedAssigned(ctx, line, sp)
			return
		}

		key := domain.SpanKey{TraceID: line.TraceID, SpanID: line.SpanID}
		c.forest.pending[key] = append(c.forest.pending[key], line)
		c.emit(ctx, domain.LogReceived{
			ID:                id,
			Body:              line.Body.String(),
			Source:            line.Source,
			Component:         line.Component,
			DeclaredTraceID:   line.TraceID,
			DeclaredSpanID:    line.SpanID,
			Timestamp:         line.Timestamp,
			ObservedTimestamp: line.ObservedTimestamp,
			SeverityNumber:    line.SeverityNumber,
			SeverityText:      line.SeverityText,
			BodyValue:         line.Body,
			Resource:          line.Resource,
			Scope:             line.Scope,
			Attributes:        line.Attributes,
		})

		return
	}

	if line.TraceID != "" {
		if _, ok := c.forest.traces[line.TraceID]; ok {
			sp := c.ensureTraceOrphan(ctx, line.TraceID)
			sp.logs = append(sp.logs, line)
			c.emitReceivedAssigned(ctx, line, sp)
			return
		}

		sp := c.ensureGlobalOrphan(ctx)
		sp.logs = append(sp.logs, line)
		c.emitReceivedAssigned(ctx, line, sp)

		return
	}

	sp := c.ensureGlobalOrphan(ctx)
	sp.logs = append(sp.logs, line)
	c.emitReceivedAssigned(ctx, line, sp)
}

func (c *Correlator) handleSpan(ctx context.Context, in domain.Span) {
	if in.SpanID == "" {
		return
	}

	key := domain.SpanKey{TraceID: in.TraceID, SpanID: in.SpanID}
	sp, ok := c.forest.spans[key]
	if ok {
		c.updateSpan(ctx, sp, in)
		return
	}

	if !c.admit(ctx, 1, 0) {
		return
	}

	sp = &forestSpan{
		traceID:       in.TraceID,
		spanID:        in.SpanID,
		parentSpanID:  in.ParentSpanID,
		name:          in.Name,
		component:     in.Component,
		startTime:     in.StartTime,
		endTime:       in.EndTime,
		status:        in.Status,
		kind:          in.Kind,
		statusMessage: in.StatusMessage,
		resource:      in.Resource,
		scope:         in.Scope,
		attributes:    in.Attributes,
		events:        in.Events,
		links:         in.Links,
	}
	c.forest.spans[key] = sp
	c.forest.awaitParent(sp)

	if in.TraceID != "" {
		c.ensureTrace(in.TraceID)
		c.noteRealSpanAdded(sp)
	}

	c.emitSpanUpserted(ctx, sp)

	c.flushPending(ctx, sp)
	c.linkParents(ctx, sp)

	if in.Ended {
		c.closeSpan(ctx, sp)
	}
}

func (c *Correlator) updateSpan(ctx context.Context, sp *forestSpan, in domain.Span) {
	if !sp.synthetic {
		if in.Name != "" {
			sp.name = in.Name
		}
		if in.TraceID != "" {
			hadTrace := sp.traceID != ""
			sp.traceID = in.TraceID
			c.ensureTrace(in.TraceID)
			if !hadTrace {
				c.noteRealSpanAdded(sp)
			}
		}
		if in.ParentSpanID != "" {
			c.forest.stopAwaitingParent(sp)
			sp.parentSpanID = in.ParentSpanID
			c.forest.awaitParent(sp)
		}
		if in.Component != "" {
			sp.component = in.Component
		}
		if !in.StartTime.IsZero() {
			sp.startTime = in.StartTime
		}
		if !in.EndTime.IsZero() {
			sp.endTime = in.EndTime
		}
		if in.Status != domain.SpanStatusUnset {
			sp.status = in.Status
		}
		if in.Kind != domain.SpanKindUnspecified {
			sp.kind = in.Kind
		}
		if in.StatusMessage != "" {
			sp.statusMessage = in.StatusMessage
		}
		if len(in.Resource.Attributes) > 0 {
			sp.resource = in.Resource
		}
		if in.Scope.Name != "" || in.Scope.Version != "" || len(in.Scope.Attributes) > 0 {
			sp.scope = in.Scope
		}
		if len(in.Attributes) > 0 {
			sp.attributes = in.Attributes
		}
		if len(in.Events) > 0 {
			sp.events = in.Events
		}
		if len(in.Links) > 0 {
			sp.links = in.Links
		}
	}

	c.emitSpanUpserted(ctx, sp)

	c.flushPending(ctx, sp)
	c.linkParents(ctx, sp)

	if in.Ended {
		c.closeSpan(ctx, sp)
	}
}

func (c *Correlator) flushPending(ctx context.Context, sp *forestSpan) {
	key := domain.SpanKey{TraceID: sp.traceID, SpanID: sp.spanID}
	waiting := c.forest.pending[key]
	if unscoped := c.forest.pending[domain.SpanKey{SpanID: sp.spanID}]; len(unscoped) > 0 {
		waiting = append(waiting, unscoped...)
		delete(c.forest.pending, domain.SpanKey{SpanID: sp.spanID})
	}
	if len(waiting) == 0 {
		return
	}

	delete(c.forest.pending, key)

	for _, line := range waiting {
		sp.logs = append(sp.logs, line)
		c.emit(ctx, domain.LogAssigned{
			ID:        line.ID,
			TraceID:   sp.traceID,
			SpanID:    sp.spanID,
			SpanName:  sp.name,
			Synthetic: sp.synthetic,
		})
	}
}

func (c *Correlator) linkParents(ctx context.Context, sp *forestSpan) {
	if sp.parentSpanID != "" && sp.parentSpanID != sp.spanID && !sp.linked {
		if _, ok := c.forest.spans[domain.SpanKey{TraceID: sp.traceID, SpanID: sp.parentSpanID}]; ok {
			c.forest.stopAwaitingParent(sp)
			sp.linked = true
			c.emit(ctx, domain.SpanParentLinked{
				TraceID:      sp.traceID,
				SpanID:       sp.spanID,
				ParentSpanID: sp.parentSpanID,
			})
		}
	}

	for child := range c.forest.unlinked[sp.spanID] {
		if child.linked || child.parentSpanID != sp.spanID || child.spanID == sp.spanID {
			continue
		}

		c.forest.stopAwaitingParent(child)
		child.linked = true
		c.emit(ctx, domain.SpanParentLinked{
			TraceID:      child.traceID,
			SpanID:       child.spanID,
			ParentSpanID: sp.spanID,
		})
	}
}

func (c *Correlator) resolvePending(ctx context.Context) {
	for key, waiting := range c.forest.pending {
		delete(c.forest.pending, key)

		for _, line := range waiting {
			sp := c.assignmentAtDrain(ctx, line)
			if sp == nil {
				c.forest.logLines--
				continue
			}

			sp.logs = append(sp.logs, line)
			c.emit(ctx, domain.LogAssigned{
				ID:        line.ID,
				TraceID:   sp.traceID,
				SpanID:    sp.spanID,
				SpanName:  sp.name,
				Synthetic: sp.synthetic,
			})
		}
	}
}

func (c *Correlator) assignmentAtDrain(ctx context.Context, line domain.LogLine) *forestSpan {
	if sp := c.findSpan(line.TraceID, line.SpanID); sp != nil {
		return sp
	}

	if line.TraceID != "" {
		if tr, ok := c.forest.traces[line.TraceID]; ok {
			if tr.orphan != nil {
				return tr.orphan
			}
			if !c.admit(ctx, 1, 0) {
				return nil
			}

			return c.ensureTraceOrphan(ctx, line.TraceID)
		}
	}

	if c.forest.globalOrphan != nil {
		return c.forest.globalOrphan
	}
	if !c.admit(ctx, 1, 0) {
		return nil
	}

	return c.ensureGlobalOrphan(ctx)
}

func (c *Correlator) emitReceivedAssigned(ctx context.Context, line domain.LogLine, sp *forestSpan) {
	c.emit(ctx, domain.LogReceived{
		ID:                line.ID,
		Body:              line.Body.String(),
		Source:            line.Source,
		Component:         line.Component,
		TraceID:           sp.traceID,
		SpanID:            sp.spanID,
		SpanName:          sp.name,
		Synthetic:         sp.synthetic,
		DeclaredTraceID:   line.TraceID,
		DeclaredSpanID:    line.SpanID,
		Timestamp:         line.Timestamp,
		ObservedTimestamp: line.ObservedTimestamp,
		SeverityNumber:    line.SeverityNumber,
		SeverityText:      line.SeverityText,
		BodyValue:         line.Body,
		Resource:          line.Resource,
		Scope:             line.Scope,
		Attributes:        line.Attributes,
	})
	c.emit(ctx, domain.LogAssigned{
		ID:        line.ID,
		TraceID:   sp.traceID,
		SpanID:    sp.spanID,
		SpanName:  sp.name,
		Synthetic: sp.synthetic,
	})
}

func (c *Correlator) emit(_ context.Context, ev domain.Event) {
	c.out <- ev
}

func (c *Correlator) admit(ctx context.Context, needSpans, needLogs int) bool {
	c.evictUntilFit(ctx, needSpans, needLogs)
	if c.spanCount()+needSpans <= c.limits.MaxSpans && c.logCount()+needLogs <= c.limits.MaxLogLines {
		return true
	}

	c.emit(ctx, domain.ForestShed{})

	return false
}

func (c *Correlator) evictUntilFit(ctx context.Context, needSpans, needLogs int) {
	for c.spanCount()+needSpans > c.limits.MaxSpans || c.logCount()+needLogs > c.limits.MaxLogLines {
		id := c.oldestClosedTrace()
		if id == "" {
			return
		}

		c.evictTrace(ctx, id)
	}
}

func (c *Correlator) oldestClosedTrace() string {
	var (
		bestID  string
		bestSeq uint64
		found   bool
	)

	for id, tr := range c.forest.traces {
		if !tr.closed {
			continue
		}
		if !found || tr.seq < bestSeq {
			bestID = id
			bestSeq = tr.seq
			found = true
		}
	}

	return bestID
}

func (c *Correlator) evictTrace(ctx context.Context, traceID string) {
	for key, sp := range c.forest.spans {
		if sp.traceID != traceID {
			continue
		}

		c.forest.logLines -= len(c.forest.pending[key]) + len(sp.logs)
		c.forest.stopAwaitingParent(sp)
		delete(c.forest.pending, key)
		delete(c.forest.spans, key)
	}

	delete(c.forest.traces, traceID)
	c.emit(ctx, domain.ForestEvicted{TraceID: traceID})
}

func (c *Correlator) spanCount() int {
	return len(c.forest.spans)
}

func (c *Correlator) logCount() int {
	return c.forest.logLines
}

func (c *Correlator) noteRealSpanAdded(sp *forestSpan) {
	if sp.synthetic || sp.traceID == "" {
		return
	}

	tr := c.ensureTrace(sp.traceID)
	tr.openReal++
	tr.closed = false
}

func (c *Correlator) closeSpan(ctx context.Context, sp *forestSpan) {
	if sp.ended || sp.synthetic {
		return
	}

	sp.ended = true
	c.emit(ctx, domain.SpanClosed{
		TraceID: sp.traceID,
		SpanID:  sp.spanID,
	})

	if sp.traceID == "" {
		return
	}

	tr := c.ensureTrace(sp.traceID)
	if tr.openReal > 0 {
		tr.openReal--
	}
	if tr.openReal == 0 {
		tr.closed = true
	}
}

func (c *Correlator) ensureTrace(traceID string) *traceNode {
	tr, ok := c.forest.traces[traceID]
	if ok {
		return tr
	}

	c.traceSeq++
	tr = &traceNode{seq: c.traceSeq}
	c.forest.traces[traceID] = tr

	return tr
}

func (c *Correlator) ensureGlobalOrphan(ctx context.Context) *forestSpan {
	if c.forest.globalOrphan != nil {
		return c.forest.globalOrphan
	}

	sp := c.newSynthSpan(randomHex(16), globalOrphanName)
	c.forest.globalOrphan = sp
	c.emitSpanUpserted(ctx, sp)

	return sp
}

func (c *Correlator) ensureTraceOrphan(ctx context.Context, traceID string) *forestSpan {
	tr := c.ensureTrace(traceID)
	if tr.orphan != nil {
		return tr.orphan
	}

	sp := c.newSynthSpan(traceID, globalOrphanName)
	tr.orphan = sp
	c.emitSpanUpserted(ctx, sp)

	return sp
}

func (c *Correlator) newSynthSpan(traceID, name string) *forestSpan {
	sp := &forestSpan{
		traceID:   traceID,
		spanID:    randomHex(8),
		name:      name,
		synthetic: true,
	}
	c.forest.spans[domain.SpanKey{TraceID: sp.traceID, SpanID: sp.spanID}] = sp

	return sp
}

func (c *Correlator) emitSpanUpserted(ctx context.Context, sp *forestSpan) {
	c.emit(ctx, domain.SpanUpserted{
		TraceID:       sp.traceID,
		SpanID:        sp.spanID,
		ParentSpanID:  sp.parentSpanID,
		Name:          sp.name,
		Component:     sp.component,
		Synthetic:     sp.synthetic,
		StartTime:     sp.startTime,
		EndTime:       sp.endTime,
		Status:        sp.status,
		Kind:          sp.kind,
		StatusMessage: sp.statusMessage,
		Resource:      sp.resource,
		Scope:         sp.scope,
		Attributes:    sp.attributes,
		Events:        sp.events,
		Links:         sp.links,
	})
}

func (c *Correlator) findSpan(traceID, spanID string) *forestSpan {
	if spanID == "" {
		return nil
	}
	if traceID != "" {
		return c.forest.spans[domain.SpanKey{TraceID: traceID, SpanID: spanID}]
	}
	var found *forestSpan
	for key, span := range c.forest.spans {
		if key.SpanID != spanID {
			continue
		}
		if found != nil {
			return nil
		}
		found = span
	}
	return found
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(i)
		}
	}

	return hex.EncodeToString(b)
}
