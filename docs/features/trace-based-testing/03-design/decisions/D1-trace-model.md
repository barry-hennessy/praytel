# D1: What trace model does a check evaluate against, and which component builds it from the event stream?
Status: decided     Depends on: none     Parent: none
Vision refs: §4 Assertion model, S1 (Ctrl-W cardinality check), S2-S4, S6; §5 Pipeline, Completeness; §8 M2, M4, M5; R4

## Context
- The correlator emits value-copy events; `SpanUpserted` carries the full span, `SpanClosed` and `SpanParentLinked` carry only IDs (`internal/domain/event.go:11-82`). `DrainComplete` means "data done" (`design/ARCHITECTURE.md`, Render).
- The only existing fold of events into a settled forest is `tui.Model.apply` (`internal/render/tui/tui.go:325`). Its `spanView` keeps component, name, status, status message, times and attributes but drops Resource, Scope, Kind, Events and Links (`internal/render/tui/tui.go:53-70`).
- The `--snapshot` completeness rule (drain finished, nothing evicted/shed, no unclosed/declared-unseen/orphaned span, at least one span) lives inside the TUI package: `tui.Snapshot` (`internal/render/tui/snapshot.go:55-95`) and `Model.canonicalise` (`internal/render/tui/snapshot.go:165-198`). `internal/render/snapshot/snapshot.go:33-45` only wraps it. Vision §5 requires exit `3` to reuse this rule exactly.
- "service" is `domain.Span.Component`, taken from resource `service.name` at the receiver (`internal/receiver/traces.go:119,139`); this is the graph's box identity (vision §5 Selectors).
- Synthetic orphan spans exist in the stream (`SpanUpserted.Synthetic`, `internal/domain/event.go:19`).
- Attribute values are typed (`internal/domain/value.go:12-110`); status is an enum (`internal/domain/span.go:5-15`).
- The same model must answer the recorder's Ctrl-W "selector matches 2 spans in this run" check while the child may still be running (S1), and v1.1 evaluates on `SpanClosed` (S6). "One renderer per process" (`design/ARCHITECTURE.md`, Render).
- M4 budget: ≤ 50ms per case at 10k spans × 20 assertions.

## Goals
- One span/trace model shared by file runs, the recorder and (later) watch.
- Exposes exactly the vision's fields: service, name, attrs (typed), status, status_message, duration_ms, parent, trace.
- Completeness verdict identical to `--snapshot` on the same events.
- Deterministic iteration order (offender lists "up to 10 ... and N more" must not vary between runs).

## Non-goals
- Logs, span events and links in the model (vision §7 Later).
- Persisted traces or offline replay (§7 non-goals).
- Changing `--snapshot` output or `SnapshotVersion`.

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| Same completeness verdict as `--snapshot` | 20 | §5 "Completeness and exit 3 reuse the --snapshot rule"; R4 |
| Shareable by run, record and watch without a second renderer | 20 | §5 Pipeline; ARCHITECTURE "one renderer per process"; S1 Ctrl-W check; S6 |
| Deterministic span and offender order | 15 | M2, R7; S4 offender list |
| Evaluation cost | 15 | M4 (≤ 50ms/case), M5 (v1.1) |
| Change surface in `render/tui` and `render/snapshot` | 15 | S5 "golden job stays unchanged"; existing snapshot goldens |
| Exposes exactly the vision's fields with typed values | 15 | §4 assertion table; R9 (surface is public API) |

## Options
Full option texts: `../options/D1-A.md` .. `D1-D.md`. Scores below are the decider's after checking the code, not the explorers' self-scores.

### Option A: Standalone in-memory span recorder (`internal/tracemodel`)
New renderer-independent fold `Recorder.Apply/Spans/Problems`; the completeness rule moves out of `snapshot.go` into it and `tui.Snapshot` calls it. The record TUI runs a second fold beside its `Model`. Cost M, reversibility hard.

### Option B: Export the TUI model's settled forest
Split `tui.Snapshot` into `Collect` + render; add `(*Settled).Spans()/Problems()` in canonical order and `(Model).LiveSpans()` in arrival order for the recorder. One fold, one rule. `status_message` is the TUI display value. Cost S, reversibility easy.

### Option C: Read-only `CheckSpan` view from the TUI model
Extract `collect` from `Snapshot`; `tui.Settle(events)` returns canonically ordered `CheckSpan`s + problems + captured lines; `(*Model).CheckSpans()` for the recorder filters synthetic/declared and sorts by (TraceID, SpanID). Cost S, reversibility easy.

### Option D: Incremental indexed store with change feed and field registry (`internal/forest`)
New store owns full `domain.Span`s, `byService`/`byName` indexes, a `Delta` feed, the completeness rule and a `Fields` registry for a future query language; the TUI forwards `apply` into it. Cost L, reversibility hard.

### Verified claims (path:line)
- `canonicalise` is destructive: deletes synthetic spans and renames declared ones (`internal/render/tui/snapshot.go:168-176,200-204`). Confirms C's point that it cannot run on the live model as is.
- Equal-key siblings keep map-iteration order: `roots`/`children` are built by ranging over `m.spans` (`snapshot.go:210-219`) and sorted with `SliceStable` on the key alone (`snapshot.go:259-265`). Confirms B's tie-break defect; it also applies to A and D, which reuse this scheme.
- The sibling key omits duration, and plain snapshot rendering omits duration too (`tui.go:840-842`). So a duration tie-break among key-equal siblings cannot change `--snapshot` text.
- `status_message` in `spanView` is display text: `clean()`ed, falling back to the last `exception.message` event on error spans (`tui.go:339,971-989,1255-1268`). Confirms B/C's R9 concern. `shop` sets the status message explicitly (`demo/workloads/shop/main.go:85-86`), so raw and display agree for the scenarios. They differ only for apps that record an exception without a status message.
- Problems do not depend on `opts.Attrs` (attribute filtering happens after counting, `snapshot.go:178-204`). They depend only on `opts.Logs`, which defaults to `false` for `--snapshot` (`cmd/praytel/internal/args/args.go:116`, `cmd/praytel/run.go:66-67`).
- C's live path sorts by (TraceID, SpanID). IDs are random per run, so that order is not stable across runs; this is unsafe the moment anything prints offenders from the live view.
- D's extensibility (field registry, query language, `Delta` feed) traces to nothing the vision commits to; D's own unknowns admit this. Discounted.

### Scores (decider; weight × score)
| Criterion (w) | A | B | C | D | Chosen hybrid |
|---|---|---|---|---|---|
| Completeness same as `--snapshot` (20) | 3 | 5 | 5 | 4 | 5 |
| Shareable run/record/watch (20) | 4 | 3 | 3 | 5 | 4 |
| Deterministic order (15) | 4 | 3 | 3 | 4 | 5 |
| Evaluation cost (15) | 4 | 4 | 4 | 4 | 4 |
| Change surface (15) | 2 | 3 | 4 | 1 | 3 |
| Exact vision fields, typed (15) | 5 | 3 | 3 | 4 | 5 |
| **Weighted /100** | **73** | **71** | **74** | **75** | **87** |

A and D lose points on completeness because they move the rule out through a refactor of the code the goldens depend on, and A runs a second fold that can drift (`tui.go:382-383` eviction, `tui.go:359-378` declared hydration). D's share score of 5 is real but bought with the largest change surface and a one-way refactor for speculative needs.

## Decision
Chosen: **Hybrid of C (core) and B (single fold), with three concrete fixes.** One fold (`tui.Model.apply`), one completeness rule (`canonicalise` plus the `Snapshot` checks), and one exported read-only value type that holds exactly the vision's fields. Specification:

1. **Extract `collect`.** Move `snapshot.go:56-95` unchanged into unexported `collect(events <-chan domain.Event, opts SnapshotOptions) (*Model, []string, []CapturedLine)`. `Snapshot` becomes `collect` + the existing render (`snapshot.go:97-115`). Snapshot output must stay byte-identical, and the existing goldens guard it.
2. **Raw status message.** Add `rawStatusMsg string` to `spanView`, set to `ev.StatusMessage` in `apply` (next to `tui.go:339`). Display (`statusMsg`) is unchanged. Use it as the last component of the tie-break (fix 3), never in the snapshot key.
3. **Content tie-break in `canonicalise`.** Extend `byKey` (`snapshot.go:259-261`) to break key ties by: duration (`end-start`), then start offset from the earliest start in the same trace, then `rawStatusMsg`, then span ID as a last resort. Key-equal siblings render identically in plain mode (`tui.go:840-842`), so `--snapshot` text cannot change. The order of anything a check can observe then depends only on content.
4. **New file `internal/render/tui/check.go`, the only exported surface:**
   ```go
   type CheckSpan struct {
       Service, Name  string            // Service = spanView.component
       Status         domain.SpanStatus
   	StatusMessage  string            // raw OTLP status message (rawStatusMsg), not display text
       Attrs          []domain.Attr     // all attributes, sorted by key then value (snapshotAttrs with attrspec.All())
       Start, End     time.Time         // End zero while unclosed; duration_ms is derived by D7
       Closed         bool
       Parent         int               // index into the same slice; -1 for a root
       Trace          int               // index of this span's root in the same slice
   }
   type CheckView struct {
       Spans    []CheckSpan     // canonical pre-order (after fix 3)
       Problems []string        // exactly the --snapshot problem list for Logs=false
       Captured []CapturedLine
   }
   func Settle(events <-chan domain.Event) CheckView     // collect(events, SnapshotOptions{Attrs: attrspec.All(), Logs: false}) + extract
   func (m *Model) CheckView() CheckView                // live, non-mutating: see 5
   ```
   No trace or span IDs are exported (R5, M3). `parent` and `trace` are slice indices.
5. **Live view without mutation.** `(*Model).CheckView()` copies every `*spanView` by value (shallow struct copy) into a scratch `Model` and runs the same `canonicalise` + extraction on the copy. That is safe because `canonicalise` replaces `sp.logs` and `sp.attributes` with fresh slices and edits only fields of the copied struct (`snapshot.go:173-175,202-204`). The live model is untouched. Record mode and file runs therefore share one ordering and one problem function. Drain/evict/shed problems are not meaningful live, so the live `Problems` holds only `canonicalise`'s list.

Why it wins on the criteria:
- **Completeness (20):** the verdict comes from the same `collect` + `canonicalise` code that `--snapshot` uses, called with the default `Logs=false`. Nothing is moved or copied.
- **Shareable (20):** run (`Settle`), record (`Model.CheckView` on the model the TUI already renders) and v1.1 watch all read one fold, so there is no second renderer and no second fold. It falls short of 5 only because watch needs a per-span path (see D12).
- **Determinism (15):** this is the only option that closes the verified tie-break hole (`snapshot.go:259-261`) and drops C's ID-ordered live path.
- **Fields (15):** exactly the vision's fields, typed, with raw `status_message` rather than TUI display text, so the public surface (R9) does not inherit display quirks.
- **Change surface (15):** additive apart from the `collect` extraction, one new `spanView` field and a tie-break that provably leaves snapshot text unchanged. `render/snapshot` and `SnapshotVersion` are untouched.
- **Reversibility:** easy. `CheckSpan`/`CheckView`/`Settle` are the only contract, and they can later be re-homed in a standalone package (A's or D's shape) without touching D7's evaluator or the Starlark surface.

We give up:
- **Layering purity.** The test runner imports `internal/render/tui`, and with it bubbletea and lipgloss. `--snapshot` already does the same, and it is one binary.
- **Coupling to TUI work.** Changes to `spanView`, `apply` or `canonicalise` now also affect checks. `check.go` needs its own tests over the snapshot fixtures to catch this.
- **Cheap per-event live evaluation.** `CheckView()` costs O(n log n) per call (copy + canonicalise). That is fine for Ctrl-W and post-drain runs, but wrong for evaluating on every `SpanClosed`.
- **D's change feed, indexes and field registry**, and A's "completeness by construction in a neutral package". Selection is a linear scan over a slice.
- **Raw-vs-display divergence.** For apps that record an exception without setting a status message, the TUI shows a message that `span.status_message` does not have (it is `""`). This is deliberate, and the guide must say so.

Rejected:
- A: second fold beside the TUI model can drift from it, and it moves the completeness rule out of the golden-guarded code for no gain the hybrid lacks. Hard to reverse.
- B: exposes TUI display `status_message` as public API and leaves the verified equal-key tie-break and arrival-order live view unfixed.
- C: closest to the choice, but its live path orders by random span IDs, it has no live problem list, and it exposes display `status_message` and raw IDs.
- D: largest one-way refactor, built for a query language and change feed the vision does not commit to; it doubles span state in the TUI.

Revisit if:
- **M4:** `make bench-diff` shows `Settle` (collect + canonicalise + extract) above **25ms** at 10k spans (half the 50ms case budget) on CI hardware. Then cache subtree keys, or move to A's standalone fold.
- **M5 (v1.1):** the watch benchmark cannot reach p99 ≤ 20µs per property per span with per-span lookups on the TUI model, or headless watch needs the model without bubbletea. Then extract an `internal/tracemodel` store (A's or D's shape) behind the same `CheckSpan` type.
- **Goldens:** any snapshot golden changes byte-wise from steps 1-3. Then revert the tie-break into `CheckView` extraction only (sort a copy), leaving `canonicalise` untouched.
- **Coupling:** check tests break twice from TUI-only changes to `spanView`/`apply` within one quarter. Then extract the fold.
- **Users:** a user report shows `status_message` empty where the TUI shows text, and users want the display value. Then add `exception_message`; do not change `status_message`.

Consequences for other decisions:
- **D2:** a case's verdict input is `tui.Settle(events)`, fed from that case's own pipeline and a fresh `Model`, which gives isolation. INCOMPLETE (exit `3`) iff `len(CheckView.Problems) > 0`. `Settle` reads the channel until it closes, so D2 must close the event channel after `DrainComplete`. Captured child output comes from `CheckView.Captured`. Test runs always use `Logs=false`: synthetic spans are dropped and do not count.
- **D7:** selectors and assertions run over `[]CheckSpan` in slice order, and offender lists use that order. `duration_ms` is `End-Start` in milliseconds and is undefined when `!Closed` (live record view only); D7 decides how that is represented. `span.parent` and `span.trace` resolve through the `Parent`/`Trace` indices. `status_message` is the raw OTLP value. Attributes are all attributes, sorted. The Starlark bridge (D4/D6) binds `CheckSpan` and never `spanView`.
- **D9:** the Ctrl-W cardinality check and the pick panel read `(*Model).CheckView()` on the live record model. It excludes synthetic and declared spans and may contain unclosed spans (`Closed=false`). Whether the child is still running comes from D2's lifecycle, not from this view.
- **D12:** v1.1 must not call `CheckView()` per `SpanClosed`. The seam v1 should keep is an unexported `toCheckSpan(*spanView)` helper used by the extraction, so watch can build one `CheckSpan` from `m.spans[selectionKey(trace, span)]` on close. A live `Parent`/`Trace` may be unresolved at close time (`-1`), which D12 must define.
- **D10/D11:** none directly. D11's 100-run determinism evidence should include a test that the `CheckSpan` order is identical across repeated runs of `shop`.
