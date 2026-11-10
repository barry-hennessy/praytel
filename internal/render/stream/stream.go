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
	"github.com/barry-hennessy/praytel/internal/projection"
	"github.com/barry-hennessy/praytel/internal/view"
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

// Stream is the append-only renderer. See docs/guides/architecture.md.
type Stream struct {
	lt     observability.LoggingTracer
	events <-chan domain.Event
	filt   *filter.Filter
	out    io.Writer
	color  bool
}

func New(lt observability.LoggingTracer, events <-chan domain.Event, out io.Writer, filt *filter.Filter) Stream {
	_, _, span := lt.Name("stream.New").Start(context.Background())
	defer span.End()

	if filt == nil {
		filt, _ = filter.New(filter.Query{})
	}

	return Stream{
		lt:     lt,
		events: events,
		filt:   filt,
		out:    out,
		color:  ColorEnabled(out),
	}
}

func (s Stream) Run(ctx context.Context) error {
	_, log, span := s.lt.Name("stream.Stream.Run").Start(ctx)
	defer span.End()

	v := NewView(log, s.color, s.filt)
	for ev, ok := <-s.events; ok; ev, ok = <-s.events {
		v.Observe(ev, nil)
		for _, line := range v.Render(nil, view.Viewport{}) {
			if _, err := io.WriteString(s.out, line+"\n"); err != nil {
				log.Error("write stream", "err", err)
			}
		}
	}

	return nil
}

// View is the append-only stream as a pipeline view: each event may add
// lines, and Render hands them over once. It reads only events, never the
// projection.
type View struct {
	log     *slog.Logger
	color   bool
	filt    *filter.Filter
	seen    map[string]printed
	pending []string
}

var _ view.View = (*View)(nil)

// NewView makes a stream view. Warnings about memory pressure go to log, not
// to the stream. color wraps each line's prefix in an ANSI color per trace.
func NewView(log *slog.Logger, color bool, filt *filter.Filter) *View {
	if filt == nil {
		filt, _ = filter.New(filter.Query{})
	}
	return &View{log: log, color: color, filt: filt, seen: make(map[string]printed)}
}

// Observe adds the lines that ev makes.
func (v *View) Observe(ev domain.Event, _ projection.Reader) {
	switch ev.(type) {
	case domain.ForestEvicted:
		v.log.Warn("evicted oldest closed trace")

		return
	case domain.ForestShed:
		v.log.Warn("shed newest inbound data")

		return
	}

	seen := v.seen
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

	admitted := v.filt.Observe(ev)
	just := make(map[string]struct{}, len(admitted))
	for _, id := range admitted {
		just[id] = struct{}{}

		p, ok := seen[id]
		if !ok {
			continue
		}

		v.line(p.source, p.spanName, p.traceID, p.spanID, p.body)
		p.emitted = true
		seen[id] = p
	}

	if !isAssigned || !hadPrev || !prev.emitted {
		return
	}

	if _, ok := just[as.ID]; ok {
		return
	}

	if !needsHydrate(prev, as) {
		return
	}

	v.line(prev.source, as.SpanName, as.TraceID, as.SpanID, prev.body)
}

// Render hands over the lines made since the last call.
func (v *View) Render(_ projection.Reader, _ view.Viewport) []string {
	out := v.pending
	v.pending = nil
	return out
}

func needsHydrate(prev printed, as domain.LogAssigned) bool {
	if prev.spanID == "" && as.SpanID != "" {
		return true
	}

	return prev.traceID != as.TraceID || prev.spanID != as.SpanID || prev.spanName != as.SpanName || prev.synthetic != as.Synthetic
}

func (v *View) line(source domain.Source, spanName, traceID, spanID, body string) {
	spanName = sanitizeField(spanName)
	traceID = sanitizeField(traceID)
	spanID = sanitizeField(spanID)
	if source == domain.SourceOTLP {
		body = sanitizeField(body)
	}

	prefix := fmt.Sprintf("%s %s %s %s ", source, spanName, traceID, spanID)
	if v.color {
		prefix = ansiWrap(prefix, colorCode(traceID))
	}

	v.pending = append(v.pending, prefix+body)
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

// ColorEnabled reports whether w is a terminal that should get color.
func ColorEnabled(w io.Writer) bool {
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
