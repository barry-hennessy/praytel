package stream

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
)

type printed struct {
	body      string
	source    domain.Source
	traceID   string
	spanID    string
	spanName  string
	synthetic bool
}

// Stream is the append-only renderer. See ARCHITECTURE.md.
type Stream struct {
	lt     observability.LoggingTracer
	events <-chan domain.Event
	out    io.Writer
	color  bool
}

func New(lt observability.LoggingTracer, events <-chan domain.Event, out io.Writer) Stream {
	return Stream{
		lt:     lt.Name("stream"),
		events: events,
		out:    out,
		color:  colorEnabled(out),
	}
}

func (s Stream) Run(ctx context.Context) error {
	ctx, log, span := s.lt.Start(ctx)
	defer span.End()

	seen := make(map[string]printed)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-s.events:
			if !ok {
				return nil
			}

			if err := s.handle(ev, seen); err != nil {
				log.Error("write stream", "err", err)
			}
		}
	}
}

func (s Stream) handle(ev domain.Event, seen map[string]printed) error {
	switch e := ev.(type) {
	case domain.LogReceived:
		if err := s.writeLine(e.Source, e.SpanName, e.TraceID, e.SpanID, e.Body); err != nil {
			return err
		}

		seen[e.ID] = printed{
			body:      e.Body,
			source:    e.Source,
			traceID:   e.TraceID,
			spanID:    e.SpanID,
			spanName:  e.SpanName,
			synthetic: e.Synthetic,
		}

		return nil
	case domain.LogAssigned:
		prev, ok := seen[e.ID]
		if !ok {
			return nil
		}

		if !needsHydrate(prev, e) {
			return nil
		}

		return s.writeLine(prev.source, e.SpanName, e.TraceID, e.SpanID, prev.body)
	default:
		return nil
	}
}

func needsHydrate(prev printed, as domain.LogAssigned) bool {
	if prev.spanID == "" && as.SpanID != "" {
		return true
	}

	return prev.traceID != as.TraceID || prev.spanID != as.SpanID || prev.spanName != as.SpanName || prev.synthetic != as.Synthetic
}

func (s Stream) writeLine(source domain.Source, spanName, traceID, spanID, body string) error {
	prefix := fmt.Sprintf("%s %s %s %s ", source, spanName, traceID, spanID)
	if s.color {
		prefix = ansiWrap(prefix, colorCode(traceID))
	}

	_, err := fmt.Fprint(s.out, prefix, body, "\n")

	return err
}

func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	f, ok := w.(*os.File)
	if !ok {
		return false
	}

	st, err := f.Stat()
	if err != nil {
		return false
	}

	return st.Mode()&os.ModeCharDevice != 0
}

func colorCode(id string) int {
	var h uint32
	for i := 0; i < len(id); i++ {
		h = 31*h + uint32(id[i])
	}

	return int(h%6) + 31
}

func ansiWrap(s string, code int) string {
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", code, s)
}
