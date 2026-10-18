package archmap

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/barry-hennessy/praytel/internal/domain"
)

// Recorder builds maps from a run's event stream. It keeps only what a map
// needs -- each span's component, name and parent -- plus anything that
// says the run's data is incomplete.
type Recorder struct {
	traces   map[string]map[string]*recSpan // trace ID -> span ID -> span
	evicted  bool
	shed     bool
	timedOut bool
	drained  bool
}

type recSpan struct {
	node   Node
	parent string
	seen   bool // a real (non-synthetic) SpanUpserted arrived for it
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder {
	return &Recorder{traces: map[string]map[string]*recSpan{}}
}

func (r *Recorder) span(traceID, spanID string) *recSpan {
	t := r.traces[traceID]
	if t == nil {
		t = map[string]*recSpan{}
		r.traces[traceID] = t
	}
	s := t[spanID]
	if s == nil {
		s = &recSpan{}
		t[spanID] = s
	}
	return s
}

// Observe incorporates one event.
func (r *Recorder) Observe(ev domain.Event) {
	switch e := ev.(type) {
	case domain.SpanUpserted:
		// A synthetic span is the correlator's stand-in for logs with no
		// real span; it is not part of the architecture.
		if e.Synthetic {
			return
		}
		s := r.span(e.TraceID, e.SpanID)
		s.node = Node{e.Component, e.Name}
		s.parent = e.ParentSpanID
		s.seen = true
	case domain.SpanParentLinked:
		r.span(e.TraceID, e.SpanID).parent = e.ParentSpanID
	case domain.ForestEvicted:
		r.evicted = true
	case domain.ForestShed:
		r.shed = true
	case domain.DrainComplete:
		r.drained = true
		r.timedOut = r.timedOut || e.TimedOut
	}
}

// IncompleteError says why a run's maps can't be trusted. Each reason is
// one sentence a person can act on.
type IncompleteError struct {
	Reasons []string
}

func (e *IncompleteError) Error() string {
	return "map not written: " + strings.Join(e.Reasons, "; ")
}

// Fragments returns one map per entry point the run reached, or an
// *IncompleteError if any part of the run's data is known to be missing.
func (r *Recorder) Fragments() (Fragments, error) {
	var reasons []string
	if !r.drained {
		reasons = append(reasons, "the run did not finish draining")
	}
	if r.timedOut {
		reasons = append(reasons, "draining reached --drain-timeout, so spans may still have been on their way")
	}
	if r.shed || r.evicted {
		reasons = append(reasons, "data was shed or evicted under --max-spans/--max-log-lines")
	}

	frags := Fragments{}
	unknown := 0
	for _, traceID := range sortedTraceIDs(r.traces) {
		spans := r.traces[traceID]
		root := func(s *recSpan) (*recSpan, bool) {
			for range len(spans) + 1 {
				if s.parent == "" {
					return s, true
				}
				p := spans[s.parent]
				if p == nil || !p.seen {
					return nil, false
				}
				s = p
			}
			return nil, false // a parent cycle
		}
		for _, s := range spans {
			if !s.seen {
				continue
			}
			rt, ok := root(s)
			if !ok {
				unknown++
				continue
			}
			frag, ok := frags[rt.node]
			if !ok {
				frag = New()
				frag.Entries[rt.node] = true
				frags[rt.node] = frag
			}
			frag.Nodes[s.node] = true
			if s.parent != "" {
				if from := spans[s.parent].node; from != s.node {
					frag.Calls[Call{from, s.node}] = true
				}
			}
		}
	}
	if unknown > 0 {
		reasons = append(reasons, fmt.Sprintf("%d span%s name%s a parent that never arrived (an exporter that did not flush, or trace context from outside the run)", unknown, plural(unknown), singularVerb(unknown)))
	}
	if len(frags) == 0 && len(reasons) == 0 {
		reasons = append(reasons, "no traces arrived (cached test results? try -count=1)")
	}
	if len(reasons) > 0 {
		return nil, &IncompleteError{Reasons: reasons}
	}
	return frags, nil
}

func sortedTraceIDs(m map[string]map[string]*recSpan) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func singularVerb(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}

// IsIncomplete reports whether err is an *IncompleteError.
func IsIncomplete(err error) bool {
	var ie *IncompleteError
	return errors.As(err, &ie)
}
