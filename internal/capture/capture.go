package capture

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/charmbracelet/x/ansi"
	"github.com/go-logfmt/logfmt"
)

const defaultWarnEvery = time.Second

// WarnGate rate-limits malformed-JSON warnings across capture streams.
type WarnGate struct {
	every time.Duration

	mu   sync.Mutex
	last time.Time
}

func NewWarnGate(every time.Duration) *WarnGate {
	if every <= 0 {
		every = defaultWarnEvery
	}

	return &WarnGate{every: every}
}

func (g *WarnGate) Allow() bool {
	if g == nil {
		return true
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	if !g.last.IsZero() && now.Sub(g.last) < g.every {
		return false
	}

	g.last = now

	return true
}

// Copy reads lines from r and sends [domain.LogLine]s to out until EOF or ctx cancel.
func Copy(ctx context.Context, lt observability.LoggingTracer, source domain.Source, r io.Reader, out chan<- domain.LogLine, gate *WarnGate) error {
	ctx, log, span := lt.Name("capture").Start(ctx)
	defer span.End()

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		body := sc.Text()
		line := parseLine(source, body)
		if line.malformed && gate.Allow() {
			log.Warn("malformed json log line", "source", string(source))
		}

		select {
		case <-ctx.Done():
			return nil
		case out <- line.LogLine:
		}
	}

	if err := sc.Err(); err != nil {
		log.Warn("capture read", "err", err, "source", string(source))
	}

	return nil
}

type parsed struct {
	domain.LogLine
	malformed bool
}

func parseLine(source domain.Source, body string) parsed {
	// Pretty console writers (zerolog, slog console handlers) colorize
	// key=value tokens with ANSI codes wrapped around them, which both
	// breaks logfmt key matching (e.g. "trace_id" arrives split around
	// escape sequences) and shows up as visible garbage on screen. Strip
	// it once, up front, so both parsing and display work on plain text.
	body = ansi.Strip(body)

	out := parsed{
		LogLine: domain.LogLine{
			Body:   body,
			Source: source,
		},
	}

	trim := strings.TrimSpace(body)
	if trim == "" {
		return out
	}

	if looksLikeJSON(trim) {
		return parseJSON(out, trim)
	}

	return parseLogfmt(out, trim)
}

func parseJSON(out parsed, trim string) parsed {
	if !json.Valid([]byte(trim)) {
		out.malformed = true

		return out
	}

	var meta struct {
		TraceID string `json:"trace_id"`
		SpanID  string `json:"span_id"`
	}
	if err := json.Unmarshal([]byte(trim), &meta); err != nil {
		return out
	}

	out.TraceID = meta.TraceID
	out.SpanID = meta.SpanID

	return out
}

func parseLogfmt(out parsed, trim string) parsed {
	traceID, spanID, ok := logfmtIDs(trim)
	if !ok {
		return out
	}

	out.TraceID = traceID
	out.SpanID = spanID

	return out
}

func logfmtIDs(s string) (traceID, spanID string, ok bool) {
	dec := logfmt.NewDecoder(strings.NewReader(s))
	if !dec.ScanRecord() {
		return "", "", false
	}

	for dec.ScanKeyval() {
		key := string(dec.Key())
		val := string(dec.Value())
		switch key {
		case "trace_id":
			traceID = val
		case "span_id":
			spanID = val
		}
	}

	if traceID == "" && spanID == "" {
		return "", "", false
	}

	return traceID, spanID, true
}

func looksLikeJSON(s string) bool {
	if s == "" {
		return false
	}

	return s[0] == '{' || s[0] == '['
}
