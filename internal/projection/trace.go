package projection

import (
	"iter"
	"maps"
	"slices"
	"time"
)

// TraceState is where a trace is in its life. OTLP has no "trace complete"
// record, so a trace is settled, not complete: see design/PIPELINE.md,
// "Trace state".
type TraceState uint8

const (
	// Live: the trace has an open span, or an event touched it recently.
	Live TraceState = iota
	// Settled: every known real span ended and no event touched the trace
	// for the settle time. A later event makes it live again.
	Settled
	// Final: the run drained; no more events arrive.
	Final
)

func (s TraceState) String() string {
	switch s {
	case Settled:
		return "settled"
	case Final:
		return "final"
	default:
		return "live"
	}
}

type traceInfo struct {
	state   TraceState
	touched time.Time
}

// State is the trace's state; Live for a trace the forest does not hold.
func (f *Forest) State(traceID string) TraceState {
	if t := f.traces[traceID]; t != nil {
		return t.state
	}
	return Live
}

// TraceIDs yields the ID of every trace the forest holds, in no order.
func (f *Forest) TraceIDs() iter.Seq[string] {
	return maps.Keys(f.traces)
}

// Settle marks settled every live trace whose real spans have all ended and
// that no event has touched for after, as of the time given to SetNow. It
// returns their IDs, sorted.
func (f *Forest) Settle(after time.Duration) []string {
	open := map[string]bool{}
	for _, sp := range f.spans {
		if !sp.Synthetic && !sp.Declared && !sp.Closed {
			open[sp.TraceID] = true
		}
	}
	var ids []string
	for id, t := range f.traces {
		if t.state == Live && !open[id] && f.now.Sub(t.touched) >= after {
			t.state = Settled
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// Finalize marks every trace final and returns their IDs, sorted.
func (f *Forest) Finalize() []string {
	ids := slices.Sorted(maps.Keys(f.traces))
	for _, t := range f.traces {
		t.state = Final
	}
	f.final = true
	return ids
}

// Incomplete lists why the trace cannot be trusted to be whole, sorted:
//
//   - "missing_span": logs named a span that never arrived.
//   - "unknown_parent": a span names a parent that never arrived.
//   - "open_span": a real span never ended (only once the run is final).
//   - "shed": telemetry was dropped under memory pressure during the run.
func (f *Forest) Incomplete(traceID string) []string {
	reasons := map[string]bool{}
	for _, sp := range f.spans {
		if sp.TraceID != traceID {
			continue
		}
		switch {
		case sp.Declared:
			reasons["missing_span"] = true
		case f.final && !sp.Synthetic && !sp.Closed:
			reasons["open_span"] = true
		}
		if sp.ParentID != "" && f.Span(traceID, sp.ParentID) == nil {
			reasons["unknown_parent"] = true
		}
	}
	if f.shed {
		reasons["shed"] = true
	}
	return slices.Sorted(maps.Keys(reasons))
}

// OneTrace is r showing only the trace traceID.
func OneTrace(r Reader, traceID string) Reader {
	return oneTrace{Reader: r, id: traceID}
}

type oneTrace struct {
	Reader
	id string
}

func (o oneTrace) Span(traceID, spanID string) *Span {
	if traceID != o.id {
		return nil
	}
	return o.Reader.Span(traceID, spanID)
}

func (o oneTrace) Log(id string) *Log {
	if l := o.Reader.Log(id); l != nil && l.TraceID == o.id {
		return l
	}
	return nil
}

func (o oneTrace) Spans() iter.Seq[*Span] {
	return func(yield func(*Span) bool) {
		for sp := range o.Reader.Spans() {
			if sp.TraceID == o.id && !yield(sp) {
				return
			}
		}
	}
}

func (o oneTrace) Logs() iter.Seq[*Log] {
	return func(yield func(*Log) bool) {
		for l := range o.Reader.Logs() {
			if l.TraceID == o.id && !yield(l) {
				return
			}
		}
	}
}

func (o oneTrace) Len() int {
	n := 0
	for range o.Spans() {
		n++
	}
	return n
}

func (o oneTrace) TraceIDs() iter.Seq[string] {
	return func(yield func(string) bool) {
		for id := range o.Reader.TraceIDs() {
			if id == o.id {
				yield(id)
				return
			}
		}
	}
}
