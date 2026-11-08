## Option D: Incremental span store with a field schema and change feed   (stance: most extensible)
Summary: A new renderer-independent package, `internal/forest`, folds the event stream into an indexed, incremental store. It holds the full `domain.Span` values, owns the completeness rule, emits a per-event change feed (`Delta`), and publishes a typed field schema. Run, record and v1.1 watch all read this one store. A future query language compiles against the schema instead of against Starlark glue.

How it works:
- `internal/forest`: `Store.Apply(domain.Event) Delta`. `Delta{Upserted, Closed, Relinked []SpanKey; Evicted []TraceID}`. It handles every event kind that `tui.Model.apply` handles today (`internal/render/tui/tui.go:325-388`), including declared placeholders and their removal on `LogAssigned`, because those affect completeness. Each span is stored as `domain.Span` (`internal/domain/span.go:69-87`) plus `closed`, `declared` and `synthetic` flags. Resource, Kind, Events and Links are kept but not exposed in v1.
- The store keeps two indexes, `byService` and `byName` (span keys), plus children-by-parent maps. They are updated in `Apply` at O(1) per event.
- `Store.Problems(opts) []string` moves the checks in `tui.Snapshot` (`internal/render/tui/snapshot.go:80-95`) and the problem half of `canonicalise` (`snapshot.go:168-198,279-281`) into one function. `tui.Snapshot` calls it, so `--snapshot` text and `SnapshotVersion` stay the same.
- `Store.Freeze() *View` returns an immutable, canonically ordered view: span order is the pre-order from canonicalise's key (`snapshot.go:221-277`). `View.Span(i)` exposes only the vision's fields.
- `forest.Fields`: a registry of `{name, kind, getter func(*Span) domain.Value}` for service (`Component`, `internal/receiver/traces.go:119,139`), name, attrs, status, status_message, duration_ms, parent and trace. The Starlark bridge (D4/D7) and a later query language both bind to this table.
- Record TUI: `tui.Model` holds a `*forest.Store`, and its `apply` forwards to it. That keeps one renderer and makes the Ctrl-W count a `View` query.
- Watch (v1.1): subscribe to `Delta.Closed`. The store already has the span payload that `SpanClosed` lacks (`internal/domain/event.go:38-42`).
- Rollout: (1) extract `Problems` and pin it with a test that runs both paths over the existing snapshot goldens; (2) add `View` and `Fields`; (3) wire the record TUI.

Steelman: Every next requirement is already on the roadmap: watch on `SpanClosed` (S6, M5 ≤20µs per property per span), structural rules through `parent`/`trace` (X1), and span events and links (§7 Later). A batch-only "fold after drain" model would have to be rebuilt for watch. This store is incremental from day one, so watch is a subscriber, not a rewrite. Moving completeness out of the TUI removes the coupling the decision tree names as D1's central tension. After the move there is one rule, not a copy kept in step by hand.

Pros: one owner of completeness; watch needs no new consumer model; indexed selectors keep M4 well under budget; the field registry makes the public surface (R9) explicit and testable; record mode needs no second fold.

Cons / failure modes / hidden costs:
- This is the largest refactor of `render/tui`. Extracting `Problems` from `canonicalise` changes code that the snapshot goldens depend on, and a subtle reordering (for example, synthetic deletion before counting, `snapshot.go:168-176`) could change verdicts.
- `tui.Model` would carry two copies of span state (the `spanView` map and the store), which doubles memory per span in record mode.
- Indexes and the change feed cost something on every event even in file runs, which never use the deltas.
- The field registry is speculative abstraction. If no query language ever arrives, it is indirection that Starlark alone did not need.
- Watch still needs a per-trace "settled" notion that this design does not define.

Criteria scores:
- Same completeness verdict as `--snapshot`: 5 - one function serves both paths.
- Shareable by run, record and watch without a second renderer: 5 - an embedded store plus a change feed.
- Deterministic span and offender order: 4 - canonical pre-order on `Freeze`; live views between freezes are unordered.
- Evaluation cost: 4 - indexed lookups; a small per-event index overhead.
- Change surface in `render/tui` and `render/snapshot`: 2 - touches `apply`, `Snapshot` and `canonicalise`.
- Exposes exactly the vision's fields with typed values: 4 - enforced by the registry, but the store holds more than it exposes, which tempts leaks.

Cost: L     Reversibility: hard (once the TUI and snapshot depend on the store, pulling it out is a second refactor)

Unknowns / assumptions: That the M5 per-span budget tolerates index upkeep in `Apply`. That `Problems` with `Logs=false` matches the `--snapshot` default for test runs. That a query language really is likely; nothing in the vision commits to one.
