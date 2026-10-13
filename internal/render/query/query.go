package query

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/projection"
	querylang "github.com/barry-hennessy/praytel/internal/query"
)

// Format identifies a query result encoding.
type Format string

const (
	// Text writes one compact, human-readable line for each matching trace.
	Text Format = "text"
	// JSON writes one JSON document after the run becomes final.
	JSON Format = "json"
	// NDJSON writes one JSON envelope per settled/final update.
	NDJSON Format = "ndjson"
)

// Emit identifies which trace lifecycle state produces incremental output.
type Emit string

const (
	// EmitFinal waits for the run to drain before returning matches.
	EmitFinal Emit = "final"
	// EmitSettled also emits quiet, closed trace revisions while the child runs.
	EmitSettled Emit = "settled"
)

// Options configure a Renderer.
type Options struct {
	Format      Format
	Emit        Emit
	SettleAfter time.Duration
	Limit       int
}

// Renderer evaluates a checked query against a live event projection.
type Renderer struct {
	events <-chan domain.Event
	out    io.Writer
	query  *querylang.Query
	opts   Options
	proj   *projection.Projection

	mu        sync.RWMutex
	childCode int
	started   bool
}

// New constructs a query renderer. The query must have been checked before a
// child is started. A non-positive limit is replaced with 100.
func New(events <-chan domain.Event, out io.Writer, q *querylang.Query, opts Options) *Renderer {
	if opts.Format == "" {
		opts.Format = Text
	}
	if opts.Emit == "" {
		opts.Emit = EmitFinal
	}
	if opts.SettleAfter <= 0 {
		opts.SettleAfter = 250 * time.Millisecond
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	return &Renderer{events: events, out: out, query: q, opts: opts, proj: projection.New()}
}

// Projection returns the renderer's read model for progressive detail APIs.
func (r *Renderer) Projection() *projection.Projection { return r.proj }

// SetChildResult records the child result included in the terminal envelope.
// It is safe to call while Run is consuming events.
func (r *Renderer) SetChildResult(code int, started bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.childCode, r.started = code, started
}

// Run consumes events until the channel closes or ctx is cancelled.
func (r *Renderer) Run(ctx context.Context) error {
	interval := r.opts.SettleAfter / 4
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	if interval > 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	finalized := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if r.opts.Emit == EmitSettled && r.opts.Format == NDJSON {
				results := r.proj.ReadySettled(r.query, now, r.opts.SettleAfter)
				if len(results) > 0 {
					if err := r.writeResults("trace_settled", projection.Settled, results); err != nil {
						return err
					}
				}
			}
		case ev, ok := <-r.events:
			now := time.Now()
			if !ok {
				if !finalized {
					return r.writeFinal(now)
				}
				return nil
			}
			r.proj.Observe(ev, now)
			if _, ok := ev.(domain.DrainComplete); ok {
				if err := r.writeFinal(now); err != nil {
					return err
				}
				finalized = true
			}
		}
	}
}

type resultDTO struct {
	TraceID           string                        `json:"trace_id"`
	Revision          uint64                        `json:"revision"`
	State             projection.State              `json:"state"`
	MatchedAt         time.Time                     `json:"matched_at"`
	Evidence          projection.Evidence           `json:"evidence"`
	IncompleteReasons []projection.IncompleteReason `json:"incomplete_reasons,omitempty"`
}

type envelope struct {
	SchemaVersion     int                           `json:"schema_version"`
	Type              string                        `json:"type"`
	RunState          projection.State              `json:"run_state"`
	ChildExitCode     int                           `json:"child_exit_code,omitempty"`
	ChildStarted      bool                          `json:"child_started"`
	Returned          int                           `json:"returned"`
	Matched           int                           `json:"matched"`
	Truncated         bool                          `json:"truncated"`
	IncompleteReasons []projection.IncompleteReason `json:"incomplete_reasons,omitempty"`
	Items             []resultDTO                   `json:"items"`
}

func (r *Renderer) writeFinal(now time.Time) error {
	results := r.proj.FinalResults(r.query, now)
	if r.opts.Format == Text {
		for _, item := range results[:min(len(results), r.opts.Limit)] {
			if _, err := fmt.Fprintf(r.out, "%s revision=%d state=final spans=%v logs=%v incomplete=%v\n", item.TraceID, item.Revision, item.Evidence.SpanIDs, item.Evidence.LogIDs, item.IncompleteReasons); err != nil {
				return err
			}
		}
		return nil
	}
	return r.writeResults("run_finished", projection.Final, results)
}

func (r *Renderer) writeResults(kind string, state projection.State, results []projection.Result) error {
	matched := len(results)
	if len(results) > r.opts.Limit {
		results = results[:r.opts.Limit]
	}
	items := make([]resultDTO, len(results))
	for i, item := range results {
		items[i] = resultDTO{item.TraceID, item.Revision, item.State, item.MatchedAt, item.Evidence, item.IncompleteReasons}
	}
	summary := r.proj.Summary()
	r.mu.RLock()
	code, started := r.childCode, r.started
	r.mu.RUnlock()
	reasons := append([]projection.IncompleteReason(nil), summary.IncompleteReasons...)
	if matched > len(items) {
		reasons = append(reasons, projection.IncompleteReason("max_results"))
	}
	sort.Slice(reasons, func(i, j int) bool { return reasons[i] < reasons[j] })
	e := envelope{1, kind, state, code, started, len(items), matched, matched > len(items), reasons, items}
	return json.NewEncoder(r.out).Encode(e)
}
