## Option B: Export the TUI model's settled forest as the check model   (stance: reuse)
Summary: Don't write a second fold. Split `tui.Snapshot` into a collect step and a render step, and add an exported read-only span view over the canonicalised `tui.Model`. Checks use the same `apply`, the same completeness problems and the same canonical pre-order that `--snapshot` already uses. The recorder's Ctrl-W check reads the live model it is already rendering.

How it works:
- `internal/render/tui/snapshot.go`: move lines 56-95 (`NewModel`, the event loop, the problem list, `canonicalise`) into `func Collect(events <-chan domain.Event, opts SnapshotOptions) *Settled`. `Snapshot` becomes `Collect` + render (lines 97-115), with output unchanged byte for byte.
- New file `internal/render/tui/spans.go`: `type Span struct{ Trace, ID, Parent, Service, Name string; Status domain.SpanStatus; StatusMessage string; DurationMS float64; Attrs []domain.Attr }`, plus `(*Settled).Spans() []Span` (sorted by `spanView.order`, which `canonicalise` renumbers at snapshot.go:267-278) and `(*Settled).Problems() []string`. A test run calls `Collect` with `Attrs` set to match all and `Logs=false`, so synthetic spans are dropped as in snapshot.go:169.
- `(Model).LiveSpans() []Span` gives the recorder the uncanonicalised spans in `order` (arrival) order. This keeps one renderer per process, because the recorder *is* the TUI.
- `internal/render/snapshot` does not change. No migration is needed.
- OTel SDK reuse (`sdktrace.ReadOnlySpan`, `tracetest.SpanStub`) was considered and dropped. Building one needs a conversion from `domain`, it has no `Component` field, and it would replace `domain.Value` typing (value.go) with `attribute.Value`.

Steelman: R4 is met by construction. The verdict is not just "identical" to `--snapshot`, it comes from the same function. Canonical ordering, cycle detection (snapshot.go:279-281), the declared/unclosed/orphaned problems (snapshot.go:178-198) and the merging of `SpanParentLinked`/`SpanClosed` (tui.go:342-347) are already written and covered by the goldens. The new code is roughly 60 lines of accessors.

Pros: one fold and one completeness rule; the existing goldens guard both; the smallest new surface; typed attributes come for free (`spanView.attributes []domain.Attr`, tui.go:68).

Cons / failure modes / hidden costs:
- The assertion model is tied to a display model. `statusMsg` comes from `statusMessage()` (tui.go:971-990): it is `clean()`ed for display and filled in from `exception` events. Users would assert on display text, and that text becomes public API (R9).
- `canonicalise` mutates the model for rendering (it renames declared spans to `<missing span>` and filters attributes, snapshot.go:200-204). Any later change to snapshot rendering silently changes check semantics.
- Sibling order only sorts by component, name, status, message and attrs (snapshot.go:236-243), so siblings with equal keys keep map-iteration order (snapshot.go:259-261). Their `duration_ms` values can then appear in a different order from run to run, which breaks the stable "up to 10 ... and N more" offender list unless a tie-break is added.
- `tui.Model` carries filter, colours and caches (tui.go:94-119). `praytel test` would link bubbletea/lipgloss even when headless. `Snapshot` already has the same problem.
- For S6, the live view needs `apply` per event. That is cheap, but evaluating on `SpanClosed` means scanning `m.spans` because there is no payload (event.go:38-42).

Criteria scores:
- Same completeness verdict: 5 - it is literally the same code path.
- Shareable by run/record/watch: 4 - record and run share it; watch needs the live/settled split, which works but is awkward.
- Deterministic order: 3 - canonical pre-order is reused, but equal-key siblings need a new tie-break.
- Evaluation cost: 4 - one fold plus one sort (as `--snapshot` today); `Spans()` copies once and checks scan a slice.
- Change surface in render/tui and render/snapshot: 2 - the new public API lives inside `render/tui`, and snapshot.go is refactored, though golden output is unchanged.
- Exact vision fields, typed: 3 - all fields are present and typed, but `status_message` carries TUI-specific changes.

Cost: S     Reversibility: easy (the accessors can later be moved behind an interface or replaced by a standalone fold)

Unknowns / assumptions: I assume vision §4 accepts `status_message` taken from exception events, or that a raw field is added to `spanView`. I assume `Collect` with a match-all attrspec gives the full attribute set. The M4 timing at 10k spans is unmeasured.
