package tree

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/praytel/internal/render/format"
)

// Tree is the buffered call-graph renderer. See ARCHITECTURE.md.
type Tree struct {
	lt     observability.LoggingTracer
	events <-chan domain.Event
	filt   *filter.Filter
	out    io.Writer
	color  bool

	spans  map[string]*spanNode
	traces map[string]*traceNode
	logs   map[string]*logLine

	globalOrphanID string
	drainFlushed   bool
	nextSeq        int
}

type spanNode struct {
	traceID      string
	spanID       string
	parentSpanID string
	name         string
	component    string
	synthetic    bool
	closed       bool
	countedOpen  bool
	seq          int
	logIDs       []string
}

type traceNode struct {
	openReal   int
	printCount int
}

type logLine struct {
	id        string
	body      string
	source    domain.Source
	spanID    string
	synthetic bool
}

func New(lt observability.LoggingTracer, events <-chan domain.Event, out io.Writer, filt *filter.Filter) Tree {
	if filt == nil {
		filt, _ = filter.New(filter.Query{})
	}

	return Tree{
		lt:     lt.Name("tree"),
		events: events,
		filt:   filt,
		out:    out,
		color:  format.ColorEnabled(out),
		spans:  make(map[string]*spanNode),
		traces: make(map[string]*traceNode),
		logs:   make(map[string]*logLine),
	}
}

func (t *Tree) Run(ctx context.Context) error {
	_, log, span := t.lt.Start(ctx)
	defer span.End()

	for ev, ok := <-t.events; ok; ev, ok = <-t.events {
		t.filt.Observe(ev)

		switch e := ev.(type) {
		case domain.ForestEvicted:
			log.Warn("evicted oldest closed trace")
		case domain.ForestShed:
			log.Warn("shed newest inbound data")
		case domain.SpanUpserted:
			if err := t.applyUpserted(e); err != nil {
				log.Error("paint trace", "err", err)
			}
		case domain.SpanParentLinked:
			t.applyParentLinked(e)
		case domain.SpanClosed:
			if err := t.applyClosed(e); err != nil {
				log.Error("paint trace", "err", err)
			}
		case domain.LogReceived:
			t.applyLogReceived(e)
		case domain.LogAssigned:
			t.applyLogAssigned(e)
		case domain.DrainComplete:
			if err := t.flushDrain(); err != nil {
				log.Error("flush drain", "err", err)
			}
		}
	}

	if err := t.flushDrain(); err != nil {
		log.Error("flush drain", "err", err)
	}

	return nil
}

func (t *Tree) applyUpserted(e domain.SpanUpserted) error {
	sp, _ := t.ensureSpan(e.SpanID)
	if e.TraceID != "" {
		sp.traceID = e.TraceID
	}
	if e.Name != "" {
		sp.name = e.Name
	}
	if e.Component != "" && !e.Synthetic {
		sp.component = e.Component
	}
	if e.Synthetic {
		sp.synthetic = true
		if e.TraceID == "" && t.globalOrphanID == "" {
			t.globalOrphanID = e.SpanID
		}
	}

	if !e.Synthetic && e.TraceID != "" && !sp.countedOpen {
		sp.countedOpen = true
		tr := t.ensureTrace(e.TraceID)
		tr.openReal++
	}

	return nil
}

func (t *Tree) applyParentLinked(e domain.SpanParentLinked) {
	if e.SpanID == "" || e.ParentSpanID == "" {
		return
	}

	sp, _ := t.ensureSpan(e.SpanID)
	sp.parentSpanID = e.ParentSpanID
	if e.TraceID != "" {
		sp.traceID = e.TraceID
	}
}

func (t *Tree) applyClosed(e domain.SpanClosed) error {
	sp := t.spans[e.SpanID]
	if sp == nil || sp.closed {
		return nil
	}

	sp.closed = true
	if sp.synthetic || sp.traceID == "" || !sp.countedOpen {
		return nil
	}

	tr := t.ensureTrace(sp.traceID)
	tr.openReal--
	if tr.openReal < 0 {
		tr.openReal = 0
	}

	if tr.openReal == 0 {
		return t.paintTrace(sp.traceID, tr.printCount > 0)
	}

	return nil
}

func (t *Tree) applyLogReceived(e domain.LogReceived) {
	ln := t.ensureLog(e.ID)
	ln.body = e.Body
	ln.source = e.Source
	ln.synthetic = e.Synthetic
	if e.SpanID != "" {
		if ln.spanID != "" && ln.spanID != e.SpanID {
			t.detachLog(ln.spanID, e.ID)
		}
		ln.spanID = e.SpanID
		t.attachLog(e.SpanID, e.ID)
	}
}

func (t *Tree) applyLogAssigned(e domain.LogAssigned) {
	ln := t.ensureLog(e.ID)
	ln.synthetic = e.Synthetic
	if e.SpanID != "" {
		if ln.spanID != "" && ln.spanID != e.SpanID {
			t.detachLog(ln.spanID, e.ID)
		}
		ln.spanID = e.SpanID
		t.attachLog(e.SpanID, e.ID)
	}
}

func (t *Tree) flushDrain() error {
	if t.drainFlushed {
		return nil
	}
	t.drainFlushed = true

	for traceID, tr := range t.traces {
		if tr.openReal > 0 || tr.printCount == 0 {
			if err := t.paintTrace(traceID, tr.printCount > 0); err != nil {
				return err
			}
		}
	}

	return t.paintGlobalOrphan()
}

func (t *Tree) paintTrace(traceID string, continued bool) error {
	tr := t.ensureTrace(traceID)
	if !t.traceHasVisibleContent(traceID) {
		tr.printCount++
		return nil
	}

	header := fmt.Sprintf("=== trace %s ===", format.SanitizeField(traceID))
	if continued {
		header = fmt.Sprintf("=== trace %s (continued) ===", format.SanitizeField(traceID))
	}

	if err := t.writeLine(header, traceID, true); err != nil {
		return err
	}

	for _, root := range t.traceRoots(traceID) {
		if err := t.paintSpan(root, traceID, 0); err != nil {
			return err
		}
	}

	tr.printCount++

	return nil
}

func (t *Tree) paintGlobalOrphan() error {
	sp := t.spans[t.globalOrphanID]
	if sp == nil || len(sp.logIDs) == 0 {
		return nil
	}

	if !t.filt.PassAll() && !t.filt.InFocus(t.globalOrphanID) {
		return nil
	}

	if err := t.writeLine("=== untagged ===", "", true); err != nil {
		return err
	}

	return t.paintSpanLogs(sp, 0)
}

func (t *Tree) traceHasVisibleContent(traceID string) bool {
	for _, sp := range t.spans {
		if sp.traceID != traceID || sp.synthetic {
			continue
		}
		if t.spanVisible(sp) {
			return true
		}
	}

	return false
}

func (t *Tree) spanVisible(sp *spanNode) bool {
	if t.filt.PassAll() {
		return len(sp.logIDs) > 0 || t.spanHasVisibleDescendant(sp)
	}

	if !t.filt.InFocus(sp.spanID) {
		return false
	}

	return len(sp.logIDs) > 0 || t.spanHasVisibleDescendant(sp)
}

func (t *Tree) spanHasVisibleDescendant(sp *spanNode) bool {
	for _, other := range t.spans {
		if other.traceID != sp.traceID || other.parentSpanID != sp.spanID {
			continue
		}
		if t.spanVisible(other) {
			return true
		}
	}

	return false
}

func (t *Tree) traceRoots(traceID string) []*spanNode {
	inTrace := make(map[string]*spanNode)
	for _, sp := range t.spans {
		if sp.traceID == traceID && !sp.synthetic {
			inTrace[sp.spanID] = sp
		}
	}

	var roots []*spanNode
	for _, sp := range inTrace {
		if _, ok := inTrace[sp.parentSpanID]; !ok {
			roots = append(roots, sp)
		}
	}

	sort.Slice(roots, func(i, j int) bool {
		return roots[i].seq < roots[j].seq
	})

	return roots
}

func (t *Tree) paintSpan(sp *spanNode, traceID string, depth int) error {
	if !t.spanVisible(sp) {
		return nil
	}

	label := t.spanLabel(sp)
	if err := t.writeLine(strings.Repeat("  ", depth)+label, traceID, true); err != nil {
		return err
	}

	if err := t.paintSpanLogs(sp, depth+1); err != nil {
		return err
	}

	for _, child := range t.spanChildren(sp, traceID) {
		if err := t.paintSpan(child, traceID, depth+1); err != nil {
			return err
		}
	}

	return nil
}

func (t *Tree) spanChildren(sp *spanNode, traceID string) []*spanNode {
	var out []*spanNode
	for _, other := range t.spans {
		if other.traceID == traceID && other.parentSpanID == sp.spanID {
			out = append(out, other)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].seq < out[j].seq
	})

	return out
}

func (t *Tree) paintSpanLogs(sp *spanNode, depth int) error {
	if !t.filt.PassAll() && !t.filt.InFocus(sp.spanID) {
		return nil
	}

	prefix := strings.Repeat("  ", depth)
	for _, id := range sp.logIDs {
		ln := t.logs[id]
		if ln == nil {
			continue
		}

		body := ln.body
		if ln.source == domain.SourceOTLP {
			body = format.SanitizeField(body)
		}

		line := fmt.Sprintf("%s%s %s", prefix, ln.source, body)
		if err := t.writeLine(line, sp.traceID, false); err != nil {
			return err
		}
	}

	return nil
}

func (t *Tree) spanLabel(sp *spanNode) string {
	name := format.SanitizeField(sp.name)
	if name == "" {
		name = format.SanitizeField(sp.spanID)
	}

	if sp.synthetic {
		return fmt.Sprintf("%s [synthetic]", name)
	}

	component := format.SanitizeField(sp.component)
	if component == "" {
		return name
	}

	return fmt.Sprintf("%s [%s]", name, component)
}

func (t *Tree) writeLine(line, traceID string, header bool) error {
	if t.color && header && traceID != "" {
		line = format.ANSIWrap(line, format.ColorCode(traceID))
	}

	_, err := fmt.Fprintln(t.out, line)

	return err
}

func (t *Tree) ensureSpan(spanID string) (*spanNode, bool) {
	sp, ok := t.spans[spanID]
	if ok {
		return sp, false
	}

	t.nextSeq++
	sp = &spanNode{spanID: spanID, seq: t.nextSeq}
	t.spans[spanID] = sp

	return sp, true
}

func (t *Tree) ensureTrace(traceID string) *traceNode {
	tr, ok := t.traces[traceID]
	if ok {
		return tr
	}

	tr = &traceNode{}
	t.traces[traceID] = tr

	return tr
}

func (t *Tree) ensureLog(id string) *logLine {
	ln, ok := t.logs[id]
	if ok {
		return ln
	}

	ln = &logLine{id: id}
	t.logs[id] = ln

	return ln
}

func (t *Tree) attachLog(spanID, logID string) {
	sp, _ := t.ensureSpan(spanID)
	for _, id := range sp.logIDs {
		if id == logID {
			return
		}
	}

	sp.logIDs = append(sp.logIDs, logID)
}

func (t *Tree) detachLog(spanID, logID string) {
	sp := t.spans[spanID]
	if sp == nil {
		return
	}

	out := sp.logIDs[:0]
	for _, id := range sp.logIDs {
		if id != logID {
			out = append(out, id)
		}
	}
	sp.logIDs = out
}
