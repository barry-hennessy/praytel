package stream

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
	"github.com/barry-hennessy/praytel/internal/render/format"
)

type printed struct {
	body      string
	source    domain.Source
	traceID   string
	spanID    string
	spanName  string
	synthetic bool
	emitted   bool
}

// Stream is the append-only renderer. See ARCHITECTURE.md.
type Stream struct {
	lt     observability.LoggingTracer
	events <-chan domain.Event
	filt   *filter.Filter
	out    io.Writer
	color  bool
}

func New(lt observability.LoggingTracer, events <-chan domain.Event, out io.Writer, filt *filter.Filter) Stream {
	if filt == nil {
		filt, _ = filter.New(filter.Query{})
	}

	return Stream{
		lt:     lt.Name("stream"),
		events: events,
		filt:   filt,
		out:    out,
		color:  format.ColorEnabled(out),
	}
}

func (s Stream) Run(ctx context.Context) error {
	_, log, span := s.lt.Start(ctx)
	defer span.End()

	seen := make(map[string]printed)

	for ev, ok := <-s.events; ok; ev, ok = <-s.events {
		if err := s.handle(log, ev, seen); err != nil {
			log.Error("write stream", "err", err)
		}
	}

	return nil
}

func (s Stream) handle(log *slog.Logger, ev domain.Event, seen map[string]printed) error {
	switch ev.(type) {
	case domain.ForestEvicted:
		log.Warn("evicted oldest closed trace")

		return nil
	case domain.ForestShed:
		log.Warn("shed newest inbound data")

		return nil
	}

	var prev printed
	var hadPrev bool

	as, isAssigned := ev.(domain.LogAssigned)
	if isAssigned {
		prev, hadPrev = seen[as.ID]
	}

	if rec, ok := ev.(domain.LogReceived); ok {
		seen[rec.ID] = printed{
			body:      rec.Body,
			source:    rec.Source,
			traceID:   rec.TraceID,
			spanID:    rec.SpanID,
			spanName:  rec.SpanName,
			synthetic: rec.Synthetic,
		}
	}

	if isAssigned {
		if p, ok := seen[as.ID]; ok {
			p.traceID = as.TraceID
			p.spanID = as.SpanID
			p.spanName = as.SpanName
			p.synthetic = as.Synthetic
			seen[as.ID] = p
		}
	}

	admitted := s.filt.Observe(ev)
	just := make(map[string]struct{}, len(admitted))
	for _, id := range admitted {
		just[id] = struct{}{}

		p, ok := seen[id]
		if !ok {
			continue
		}

		if err := s.writeLine(p.source, p.spanName, p.traceID, p.spanID, p.body); err != nil {
			return err
		}

		p.emitted = true
		seen[id] = p
	}

	if !isAssigned || !hadPrev || !prev.emitted {
		return nil
	}

	if _, ok := just[as.ID]; ok {
		return nil
	}

	if !needsHydrate(prev, as) {
		return nil
	}

	return s.writeLine(prev.source, as.SpanName, as.TraceID, as.SpanID, prev.body)
}

func needsHydrate(prev printed, as domain.LogAssigned) bool {
	if prev.spanID == "" && as.SpanID != "" {
		return true
	}

	return prev.traceID != as.TraceID || prev.spanID != as.SpanID || prev.spanName != as.SpanName || prev.synthetic != as.Synthetic
}

func (s Stream) writeLine(source domain.Source, spanName, traceID, spanID, body string) error {
	spanName = format.SanitizeField(spanName)
	traceID = format.SanitizeField(traceID)
	spanID = format.SanitizeField(spanID)
	if source == domain.SourceOTLP {
		body = format.SanitizeField(body)
	}

	prefix := fmt.Sprintf("%s %s %s %s ", source, spanName, traceID, spanID)
	if s.color {
		prefix = format.ANSIWrap(prefix, format.ColorCode(traceID))
	}

	_, err := fmt.Fprint(s.out, prefix, body, "\n")

	return err
}
