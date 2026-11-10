// Package stage holds the record-local stages of a plan. Each stage has two
// forms with the same result: OnEvent changes events before a view sees them
// (for outputs with the trigger "each", which cannot take output back), and
// Lens changes what a view reads from the projection. No stage removes a
// control event. See design/PIPELINE.md, "Stages".
package stage

import (
	"fmt"
	"iter"

	"github.com/barry-hennessy/praytel/internal/attrspec"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/plan"
	"github.com/barry-hennessy/praytel/internal/projection"
)

// Stage is one compiled stage.
type Stage interface {
	// OnEvent passes ev, changed or not, to emit zero or more times.
	OnEvent(ev domain.Event, emit func(domain.Event))
	// Lens wraps r so that reads see what OnEvent would have let through.
	Lens(r projection.Reader) projection.Reader
}

// Build compiles one plan stage.
func Build(s plan.Stage) (Stage, error) {
	switch s.Kind {
	case plan.StageAttrs:
		spec, err := attrspec.Parse(s.Globs)
		if err != nil {
			return nil, fmt.Errorf("stage attrs: %w", err)
		}
		return Attrs(spec), nil
	case plan.StageNoAttrs:
		return Attrs(attrspec.Spec{}), nil
	default:
		return nil, fmt.Errorf("unknown stage %q", s.Kind)
	}
}

// BuildAll compiles a list of plan stages, in order.
func BuildAll(ss []plan.Stage) ([]Stage, error) {
	out := make([]Stage, 0, len(ss))
	for _, s := range ss {
		st, err := Build(s)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// Chain runs ev through stages in order and passes the result to emit.
func Chain(stages []Stage, ev domain.Event, emit func(domain.Event)) {
	if len(stages) == 0 {
		emit(ev)
		return
	}
	stages[0].OnEvent(ev, func(next domain.Event) {
		Chain(stages[1:], next, emit)
	})
}

// LensAll wraps r in each stage's lens, in order.
func LensAll(stages []Stage, r projection.Reader) projection.Reader {
	for _, s := range stages {
		r = s.Lens(r)
	}
	return r
}

// Attrs keeps only the span attributes that spec matches, in their order.
func Attrs(spec attrspec.Spec) Stage { return attrs{spec: spec} }

type attrs struct{ spec attrspec.Spec }

func (a attrs) OnEvent(ev domain.Event, emit func(domain.Event)) {
	if up, ok := ev.(domain.SpanUpserted); ok {
		up.Attributes = a.filter(up.Attributes)
		ev = up
	}
	emit(ev)
}

func (a attrs) filter(in []domain.Attr) []domain.Attr {
	var out []domain.Attr
	for _, at := range in {
		if a.spec.Match(at.Key) {
			out = append(out, at)
		}
	}
	return out
}

func (a attrs) Lens(r projection.Reader) projection.Reader {
	return attrsReader{Reader: r, a: a}
}

// attrsReader gives a copy of each span with its attributes filtered.
type attrsReader struct {
	projection.Reader
	a attrs
}

func (r attrsReader) view(sp *projection.Span) *projection.Span {
	if sp == nil {
		return nil
	}
	cp := *sp
	cp.Attributes = r.a.filter(sp.Attributes)
	return &cp
}

func (r attrsReader) Span(traceID, spanID string) *projection.Span {
	return r.view(r.Reader.Span(traceID, spanID))
}

func (r attrsReader) Spans() iter.Seq[*projection.Span] {
	return func(yield func(*projection.Span) bool) {
		for sp := range r.Reader.Spans() {
			if !yield(r.view(sp)) {
				return
			}
		}
	}
}
