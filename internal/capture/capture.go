package capture

import (
	"bufio"
	"context"
	"io"
	"sync"
	"time"

	"github.com/barry-hennessy/bones/observability"
	"github.com/barry-hennessy/praytel/internal/domain"
)

const defaultWarnEvery = time.Second

// WarnGate rate-limits malformed-JSON warnings across capture streams.
type WarnGate struct {
	every time.Duration

	mu   sync.Mutex
	last time.Time
}

// NewWarnGate returns a warning gate with the supplied minimum interval.
func NewWarnGate(every time.Duration) *WarnGate {
	if every <= 0 {
		every = defaultWarnEvery
	}

	return &WarnGate{every: every}
}

// Allow reports whether a warning should be emitted now.
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
