package correlator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
)

const globalOrphanName = "orphan"

// Correlator owns the private forest and outbound events. See ARCHITECTURE.md.
type Correlator struct {
	lt    observability.LoggingTracer
	out   chan<- domain.Event
	logs  chan domain.LogLine
	spans chan domain.Span
	stop  chan struct{}

	stopOnce sync.Once
	outOnce  sync.Once

	forest forest
	logSeq uint64
}

type forest struct {
	traces       map[string]*traceNode
	spans        map[string]*forestSpan
	pending      map[string][]domain.LogLine
	globalOrphan *forestSpan
}

type traceNode struct {
	orphan *forestSpan
}

type forestSpan struct {
	traceID      string
	spanID       string
	parentSpanID string
	name         string
	synthetic    bool
	linked       bool
}

func New(lt observability.LoggingTracer, out chan<- domain.Event) *Correlator {
	return &Correlator{
		lt:    lt.Name("correlator"),
		out:   out,
		logs:  make(chan domain.LogLine, 64),
		spans: make(chan domain.Span, 64),
		stop:  make(chan struct{}),
		forest: forest{
			traces:  make(map[string]*traceNode),
			spans:   make(map[string]*forestSpan),
			pending: make(map[string][]domain.LogLine),
		},
	}
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
			c.drainInbound(ctx, logs, spans)
			c.resolvePending(ctx)

			return nil
		case <-c.stop:
			c.drainInbound(ctx, logs, spans)
			c.resolvePending(ctx)

			return nil
		case line, ok := <-logs:
			if !ok {
				c.drainInbound(ctx, nil, spans)
				c.resolvePending(ctx)

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

	if line.SpanID != "" {
		if sp, ok := c.forest.spans[line.SpanID]; ok {
			c.emitReceivedAssigned(ctx, line, sp)
			return
		}

		c.forest.pending[line.SpanID] = append(c.forest.pending[line.SpanID], line)
		c.emit(ctx, domain.LogReceived{
			ID:     id,
			Body:   line.Body,
			Source: line.Source,
		})

		return
	}

	if line.TraceID != "" {
		if _, ok := c.forest.traces[line.TraceID]; ok {
			c.emitReceivedAssigned(ctx, line, c.ensureTraceOrphan(ctx, line.TraceID))
			return
		}

		c.emitReceivedAssigned(ctx, line, c.ensureGlobalOrphan(ctx))

		return
	}

	c.emitReceivedAssigned(ctx, line, c.ensureGlobalOrphan(ctx))
}

func (c *Correlator) handleSpan(ctx context.Context, in domain.Span) {
	if in.SpanID == "" {
		return
	}

	sp, ok := c.forest.spans[in.SpanID]
	if !ok {
		sp = &forestSpan{
			traceID:      in.TraceID,
			spanID:       in.SpanID,
			parentSpanID: in.ParentSpanID,
			name:         in.Name,
		}
		c.forest.spans[in.SpanID] = sp
	} else if !sp.synthetic {
		if in.Name != "" {
			sp.name = in.Name
		}
		if in.TraceID != "" {
			sp.traceID = in.TraceID
		}
		if in.ParentSpanID != "" {
			sp.parentSpanID = in.ParentSpanID
		}
	}

	if in.TraceID != "" {
		c.ensureTrace(in.TraceID)
	}

	c.emit(ctx, domain.SpanUpserted{
		TraceID:   sp.traceID,
		SpanID:    sp.spanID,
		Name:      sp.name,
		Synthetic: sp.synthetic,
	})

	c.flushPending(ctx, sp)
	c.linkParents(ctx, sp)
}

func (c *Correlator) flushPending(ctx context.Context, sp *forestSpan) {
	waiting := c.forest.pending[sp.spanID]
	if len(waiting) == 0 {
		return
	}

	delete(c.forest.pending, sp.spanID)

	for _, line := range waiting {
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
		if _, ok := c.forest.spans[sp.parentSpanID]; ok {
			sp.linked = true
			c.emit(ctx, domain.SpanParentLinked{
				TraceID:      sp.traceID,
				SpanID:       sp.spanID,
				ParentSpanID: sp.parentSpanID,
			})
		}
	}

	for _, child := range c.forest.spans {
		if child.linked || child.parentSpanID != sp.spanID || child.spanID == sp.spanID {
			continue
		}

		child.linked = true
		c.emit(ctx, domain.SpanParentLinked{
			TraceID:      child.traceID,
			SpanID:       child.spanID,
			ParentSpanID: sp.spanID,
		})
	}
}

func (c *Correlator) resolvePending(ctx context.Context) {
	for spanID, waiting := range c.forest.pending {
		delete(c.forest.pending, spanID)

		for _, line := range waiting {
			sp := c.assignmentAtDrain(ctx, line)
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
	if sp, ok := c.forest.spans[line.SpanID]; ok {
		return sp
	}

	if line.TraceID != "" {
		if _, ok := c.forest.traces[line.TraceID]; ok {
			return c.ensureTraceOrphan(ctx, line.TraceID)
		}
	}

	return c.ensureGlobalOrphan(ctx)
}

func (c *Correlator) emitReceivedAssigned(ctx context.Context, line domain.LogLine, sp *forestSpan) {
	c.emit(ctx, domain.LogReceived{
		ID:        line.ID,
		Body:      line.Body,
		Source:    line.Source,
		TraceID:   sp.traceID,
		SpanID:    sp.spanID,
		SpanName:  sp.name,
		Synthetic: sp.synthetic,
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

func (c *Correlator) ensureTrace(traceID string) *traceNode {
	tr, ok := c.forest.traces[traceID]
	if ok {
		return tr
	}

	tr = &traceNode{}
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
	c.forest.spans[sp.spanID] = sp

	return sp
}

func (c *Correlator) emitSpanUpserted(ctx context.Context, sp *forestSpan) {
	c.emit(ctx, domain.SpanUpserted{
		TraceID:   sp.traceID,
		SpanID:    sp.spanID,
		Name:      sp.name,
		Synthetic: sp.synthetic,
	})
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
