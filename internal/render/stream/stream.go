package stream

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/filter"
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
		color:  colorEnabled(out),
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
	spanName = sanitizeField(spanName)
	traceID = sanitizeField(traceID)
	spanID = sanitizeField(spanID)
	if source == domain.SourceOTLP {
		body = sanitizeField(body)
	}

	prefix := fmt.Sprintf("%s %s %s %s ", source, spanName, traceID, spanID)
	if s.color {
		prefix = ansiWrap(prefix, colorCode(traceID))
	}

	_, err := fmt.Fprint(s.out, prefix, body, "\n")

	return err
}

var ctlSeq = regexp.MustCompile(`\x1b\[[?-]*[0-9;]*[A-Za-z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

func sanitizeField(s string) string {
	if s == "" {
		return s
	}

	s = ctlSeq.ReplaceAllString(s, "")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			continue
		case unicode.Is(unicode.Bidi_Control, r):
			continue
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
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
