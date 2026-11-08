## Option A: Standalone in-memory span recorder (the OTel `tracetest` / Tracetest shape)   (stance: most conventional)
Summary: A new renderer-independent package folds the event stream into a flat, read-only list of span records. This is the in-memory-exporter pattern (P2: Go OTel `SpanRecorder`/`SpanStub`) feeding a post-hoc select-then-assert layer (P1: Tracetest). The `--snapshot` completeness rule moves into this package, and `tui.Snapshot` calls it, so there is one rule.

How it works:
- New `internal/tracemodel`. `Recorder.Apply(domain.Event)` is an incremental fold. `Recorder.Spans() []Span` returns a frozen, canonically ordered copy. `Recorder.Problems(includeSynthetic bool) []string` gives the completeness verdict.
- `Span{Service, Name string; Attrs []domain.Attr (sorted by key); Status domain.SpanStatus; StatusMessage string; Duration time.Duration; Parent *Span; Trace *Trace}`. `Service` is `SpanUpserted.Component` (`internal/domain/event.go:18`). Resource, Scope, Kind, Events and Links are dropped at the fold.
- The fold also tracks the declared, synthetic, closed and parent state that completeness needs. That means it must handle `LogReceived`/`LogAssigned` the way `tui.Model.apply` does (`internal/render/tui/tui.go:348-379`).
- Canonical order is a pre-order walk with siblings sorted by a content key (service, name, status, message, attrs, child keys). This is the scheme in `internal/render/tui/snapshot.go:221-265`. It never uses IDs or timestamps, which differ between runs.
- `render/tui` change: `Snapshot` feeds a `Recorder` alongside its `Model` and takes its problems from `Recorder.Problems(opts.Logs)`. It then deletes its own counting at `snapshot.go:80-92,178-198,279-281`. Problem strings stay byte-identical. `render/snapshot` is untouched (`internal/render/snapshot/snapshot.go:33-45`).
- Users: the file runner reads `Spans()` after `DrainComplete`. The record TUI holds a `Recorder` next to its `Model` (a second fold, still one renderer) and reads `Spans()` at Ctrl-W. v1.1 watch calls `Apply` and evaluates on `SpanClosed`.

Steelman: This is the shape every comparable tool uses: capture spans into a plain list, then filter and assert. Readers already know it, and so do the people who write the Starlark binding. It gets the trace model out of a UI package, so testing does not depend on `lipgloss` or the layout code. Moving the completeness rule into it makes exit `3` the same rule by construction rather than by a test. An incremental `Apply` serves all three consumers with no extra design work.

Pros: One model with only the vision's fields. The completeness rule lives in one place. Linear scans over a slice are easily within M4 (10k × 20 is 200k predicate calls). The tests need no TUI.

Cons / failure modes / hidden costs:
- Span state is folded twice (the TUI `Model` and the `Recorder`). The two folds can drift, for example on declared-span hydration or eviction (`tui.go:382-383` removes a trace), and `Problems` would then disagree with what the TUI shows.
- The refactor touches the most sensitive code in `snapshot.go`. The synthetic deletion that depends on `opts.Logs` (`snapshot.go:168-176`) must happen before the orphan count, or verdicts change.
- The Recorder needs logs only to compute completeness. That is more than "spans".
- Doubles span memory in the record TUI.
- Content-key sorting costs O(n log n) per `Spans()` call. That is fine after drain, but the cost repeats on every v1.1 `SpanClosed` unless it is cached.

Criteria scores:
- Same completeness verdict: 4 - one shared function, but it is reached through a refactor that could regress.
- Shareable without a second renderer: 5 - a pure fold with no rendering, and incremental by design.
- Deterministic order: 4 - reuses the proven content-key scheme. Identical siblings still tie and need a stable tiebreak.
- Evaluation cost: 4 - flat slice scans. Re-sorting per live evaluation is the risk.
- Change surface in render/tui, render/snapshot: 2 - edits `snapshot.go` internals and makes `Snapshot` drive two folds. Goldens should not change, but that has to be proven.
- Exact vision fields, typed: 5 - the struct is exactly that list, with `domain.Value`-typed attrs.

Cost: M     Reversibility: hard (the public Starlark surface comes from `Span`, and moving the rule is a structural change that is awkward to undo)

Unknowns / assumptions:
- That both folds treat `ForestEvicted` and declared-span hydration the same way. This needs a differential test over existing snapshot fixtures.
- That the cycle check ("unreachable from any root") can be computed outside the numbering pass without changing its count.
- Whether `Duration` is defined for unclosed spans at Ctrl-W time (S1).
