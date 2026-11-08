## Option C: Export a read-only span view from the TUI model   (stance: minimal change)
Summary: Leave the fold and the completeness rule where they are, in `internal/render/tui`. Pull `tui.Snapshot`'s collect loop and problem list out into a shared helper, and add one exported value type plus two functions that hand checks a sorted copy of the settled spans. File runs call `tui.Settle(events)`. The recorder, which already runs the TUI model, calls `(*Model).CheckSpans()`.

How it works:
- New file `internal/render/tui/check.go` with `type CheckSpan struct{ TraceID, SpanID, ParentID, Service, Name, StatusMessage string; Status domain.SpanStatus; Start, End time.Time; Attrs []domain.Attr }`, `type Settled struct{ Spans []CheckSpan; Problems []string; Captured []CapturedLine }`, `func Settle(<-chan domain.Event) Settled` and `func (m *Model) CheckSpans() []CheckSpan`. `Service` comes from `spanView.component`. `duration_ms` is `End-Start`. `parent` and `trace` are resolved by ID in D7's layer.
- `snapshot.go:55-95`: move the loop and the drained/evicted/shed/empty checks into `collect(events) (*Model, []string, []CapturedLine)`. `Snapshot` and `Settle` both call it. `Settle` calls `canonicalise(SnapshotOptions{Attrs: attrspec.All()})` (`attrspec.go:20`), so it drops synthetics just as `--snapshot` does by default (`snapshot.go:169`) and assigns the canonical pre-order (`snapshot.go:267-278`). It then copies spans out in `order`.
- `CheckSpans` does not mutate. `canonicalise` deletes and renames spans (`snapshot.go:168-203`), so it cannot run on the live model. `CheckSpans` therefore filters out synthetic and declared spans and sorts by (TraceID, SpanID).
- Nothing changes in `render/snapshot`, `SnapshotVersion`, `domain` or the correlator. There is no migration. The existing goldens guard the refactor.

Steelman: The `--snapshot` verdict is the same because it comes from the same code: `Settle` and `Snapshot` share one `collect` and one `canonicalise`, so they cannot drift. In record mode the TUI model already holds every span, so nothing else consumes the stream and "one renderer per process" holds without effort. `spanView` already has all the vision fields with typed attrs and status (`tui.go:53-70`), so no new fold is needed. About 150 lines, all additive except one extraction.

Pros: smallest diff. Exit 3 matches `--snapshot` by construction. No new package, event or goroutine. Easy to undo.

Cons / failure modes / hidden costs:
- The check engine imports `render/tui` and pulls in bubbletea and lipgloss. The test runner depends on a UI package, which inverts the layering.
- `status_message` is the TUI's display value: cleaned, and falling back to `exception.message` (`tui.go:971-985`), not the raw OTLP field. That quirk becomes public API (R9).
- There are two ordering paths. `Settle` uses the canonical pre-order and `CheckSpans` uses ID order. Ctrl-W only counts matches, so the split is harmless today. If either path later prints offenders, though, the order will not be stable, because span IDs are random between runs.
- Live completeness for the recorder or watch (S6) is not covered. `canonicalise` is destructive and its problem logic is not split out.
- Every TUI refactor now risks breaking checks, and `spanView` cannot drop a field without breaking them.
- `canonicalise` builds subtree keys recursively, roughly O(n x depth) in string work. It runs once per case, not per assertion, but nobody has measured it at 10k spans.

Criteria scores:
- Same completeness verdict as `--snapshot`: 5 - the same functions, called on the same events.
- Shareable by run, record and watch without a second renderer: 3 - run and record are fine; watch has no non-destructive live verdict.
- Deterministic span and offender order: 4 - canonical order for settled runs; the live path's ID order is not stable across runs.
- Evaluation cost: 4 - one fold and one canonicalise per case, then flat slices; the key building has not been measured.
- Change surface in `render/tui` and `render/snapshot`: 3 - `render/snapshot` is untouched, but the change is inside `render/tui` and widens its exported API.
- Exposes exactly the vision's fields with typed values: 4 - all fields are typed, but `status_message` carries TUI semantics.

Cost: S     Reversibility: easy (unexported core, additive API; a later dedicated package can wrap or replace it)

Unknowns / assumptions:
- This assumes D7 resolves `parent` and `trace` from IDs over the flat slice.
- It assumes the record-mode model is reachable from the Ctrl-W handler; D9 owns that.
- It assumes the TUI's cleaned `status_message` is acceptable as the public value.
- The `canonicalise` cost at 10k spans has not been measured against M4.
