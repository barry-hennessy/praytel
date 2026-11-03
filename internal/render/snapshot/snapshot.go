// Package snapshot is the renderer behind --snapshot: it collects the whole
// run, then prints one deterministic rendering of it -- but only if the run
// is provably complete.
package snapshot

import (
	"context"
	"fmt"
	"io"

	"github.com/barry-hennessy/praytel/internal/domain"
	"github.com/barry-hennessy/praytel/internal/render/tui"
)

// IncompleteExit is praytel's exit code when the child succeeded but the
// snapshot could not be proved complete.
const IncompleteExit = 3

// Snapshot renders the event stream once, after it closes.
type Snapshot struct {
	events <-chan domain.Event
	opts   tui.SnapshotOptions
	res    tui.SnapshotResult
}

// New constructs a Snapshot over the event stream.
func New(events <-chan domain.Event, opts tui.SnapshotOptions) *Snapshot {
	return &Snapshot{events: events, opts: opts}
}

// Run consumes events until the stream closes or ctx is done. It writes
// nothing; Finish does, once the child's exit code is known.
func (s *Snapshot) Run(ctx context.Context) error {
	done := make(chan tui.SnapshotResult, 1)
	go func() { done <- tui.Snapshot(s.events, s.opts) }()
	select {
	case s.res = <-done:
	case <-ctx.Done():
		// The producer is being torn down; its channel closes shortly and
		// the goroutine ends, but there is nothing worth printing.
		s.res = tui.SnapshotResult{Problems: []string{"interrupted before the run finished"}}
	}

	return nil
}

// Finish writes the outcome and returns praytel's exit code, given the
// child's.
//
//   - Child failed: stdout stays empty, its captured output is replayed to
//     stderr, and its code wins.
//   - Child succeeded, snapshot incomplete: stdout stays empty, the problems
//     go to stderr, and the code is IncompleteExit.
//   - Otherwise the snapshot goes to stdout.
func (s *Snapshot) Finish(childCode int, stdout, stderr io.Writer) int {
	if childCode != 0 {
		for _, l := range s.res.Captured {
			_, _ = fmt.Fprintln(stderr, l.Body)
		}
		s.reportProblems(stderr)

		return childCode
	}
	if len(s.res.Problems) > 0 {
		s.reportProblems(stderr)

		return IncompleteExit
	}
	_, _ = io.WriteString(stdout, s.res.Text)

	return 0
}

func (s *Snapshot) reportProblems(w io.Writer) {
	for _, p := range s.res.Problems {
		_, _ = fmt.Fprintf(w, "praytel: snapshot incomplete: %s\n", p)
	}
}
