# D12: Which seams must v1 build so that v1.1 `--watch` can reuse v1's evaluator and format without rework?
Status: decided     Depends on: D1, D6, D8     Parent: none
Vision refs: S6; §5 Pipeline (`--watch` on `SpanClosed`); §7 v1.1; A2; M5; R8

Scope note: this is deliberately thin. It decides only what v1 must contain or avoid so v1.1 is not blocked. It does not design `--watch`'s UX, repro naming or M5 benchmarking; that is v1.1 work, gated on M5.

## Context
- v1.1 evaluates each `always`/`never` from a `.praytest.star` as each span closes, writes one repro per property on first violation, keeps the child running and exits with the child's code (S6).
- `SpanClosed` carries only IDs (`internal/domain/event.go:38-42`); a consumer must fold `SpanUpserted` to know the span. Pipeline channels block rather than drop (`design/ARCHITECTURE.md`, Render), so a slow evaluator stalls ingest; M5 requires no blocked pipeline at 10k spans/s × 10 properties and p99 ≤ 20µs per property per span.
- `always`/`never` predicates are Starlark lambdas (S4), so watch calls into Starlark per closing span from a consumer goroutine; Starlark threads are single-goroutine.
- `span.parent`/`span.trace` may be unresolved when a span closes (parents link late, `internal/domain/event.go:31-37`), unlike a post-drain run (D1).
- v1.1 reuses v1's evaluator (D1, D6, D7), file model (D5) and generator (D8); flag parsing follows record-mode rules (D10).
- A2 is unvalidated: if live repro proves useless, any v1 cost spent on seams is waste.

## Goals
- A short list of concrete seams (interfaces, invariants, things v1 must not do) that v1 tasks must honour.
- Each seam justified by a specific v1.1 need.

## Non-goals
- Implementing any part of `--watch`.
- Bending v1 semantics (post-drain evaluation) to match live evaluation.

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| v1 cost added for v1.1 | 30 | §7 v1 scope; R8 scope creep |
| Rework avoided in v1.1 | 30 | §7 v1.1 "reusing v1's evaluator and format" |
| v1 semantics not constrained by watch | 20 | A2 unvalidated; §5 file runs evaluate after `DrainComplete` |
| Performance headroom for M5 | 10 | M5 |
| Cheap to abandon if A2 fails | 10 | A2 "If wrong" |

## Options
Full text in `../options/D12-A.md`, `D12-B.md`, `D12-C.md`. The weighted scores below are the decider's (x/5 per criterion, scaled to 100), after discounting unsupported claims.

### Option A: Build the watch seams in v1, unexposed (stance: most extensible)
v1 builds a per-span `PropertyEval.Observe`, a `SpanGraph` resolver woven into the v1 span binding, a `(*Case).Properties` collect phase, repro fields with an expect-fail self-check, and a `BenchmarkObserve`. v1's `always`/`never` route through `Observe`. About 600-900 LOC, most of it with no v1 user. Collect mode has D5's data-dependence hole. The interfaces are guessed before any M5 numbers exist.
Scores: cost 2, rework 3 (discounted from 4: its own text says M5 may force a rewrite), semantics 2, M5 4, abandon 2 → **50**.

### Option B: Guardrails only, no new v1 code (stance: minimal change)
No new v1 code. Three prose rules for v1 tasks: keep `toCheckSpan` separate; `internal/selector` stays Starlark-free and `Matches` reads one span; `(*Case).Check(ctx, CheckView)` stays callable in memory, and `always`/`never` keep selector and predicate separate. Plus a non-binding v1.1 plan (headless fold, per-span `toCheckSpan` + `Matches` + reused thread, properties found by a collector `run`).
Verified: the headless-fold assumption holds today. `Snapshot` already folds with `NewModel(events, nil)` + `m.apply(ev)` and no bubbletea program (`internal/render/tui/snapshot.go:55-78`). Weakness confirmed: the rules are prose, and the depguard rule that rule 2 relies on does not exist yet (`.golangci.yml` has no depguard; D4 adds it).
Scores: cost 5, rework 3, semantics 5, M5 2, abandon 5 → **82**.

### Option C: Watch evaluates whole checks per completed trace (stance: radical alternative)
Drop per-span evaluation. v1.1 runs each case's existing `check` against a per-trace `CheckView` once the trace settles (root closed, no open span for quiet period Q, flush at child exit), keeping non-`ZeroMatch` `always`/`never` failures. v1 keeps three invariants: extraction over any span set, a re-entrant `Check`, and `Failure.Kind`/`Builtin`. It needs a vision amendment (S6, §5, P4, M5). Alerts arrive after the trace settles, and for long-lived traces only at child exit. A `run.one` or `expect.eq` in a watched file can stop a check before its properties run.
Scores: cost 5, rework 3 (discounted from 4: the settle detector, the Q heuristic and the watch-file restrictions are new, and the reuse holds only for files that use only always/never), semantics 5, M5 2, abandon 5 → **82**. That is before the product cost of the vision change, which is not a criterion here but is real (see Decision, point 2).

## Decision
Chosen: **Hybrid, B as the base: no new v1 production code. Four seams are enforced by tests or lint and one absence rule is checked at task planning; nothing is left to review-only prose. No vision amendment now. The v1.1 evaluation model (per span as in S6, or C's per trace) stays open and is chosen by a gating spike.** B's and C's v1 seams are the same code paths. They differ only on v1.1's model, and the scope note says D12 is not the place to decide that. So D12 takes the union of the seams that cost nothing and drops the ones that are review-only or unneeded.

### 1. What v1 must honour, and how each rule is enforced
All five rules protect things v1 already builds for its own reasons (D1, D6, D7, D8). The only new v1 work is about 60-100 lines of tests, plus one lint rule that D4 already requires. Each seam test carries the comment `// D12 seam: changing this needs a D12 revisit, not a test edit.`

| # | Rule | Why v1.1 needs it | Enforcement |
|---|---|---|---|
| S1 | `internal/render/tui/check.go` keeps an unexported `toCheckSpan(sp *spanView) CheckSpan` that fills every field except `Parent`/`Trace`, which it sets to `-1`. Extraction (`Settle`, `(*Model).CheckView`) calls it, then sets the indices. | Both per-span and per-trace watch build a `CheckSpan` from one `spanView` without paying for `CheckView()` (D1: O(n log n) per call). | **Test** `TestToCheckSpanMatchesExtraction` in `internal/render/tui/check_test.go`: over every snapshot fixture, for each span, `toCheckSpan` equals `CheckView().Spans[i]` with `Parent`/`Trace` set to `-1`. Inlining or renaming the helper breaks compilation, and drift between the two breaks the test. |
| S2 | `internal/selector` imports neither `go.starlark.net` nor `internal/praytest`, and `Matches` has the signature `func (Selector) Matches(CheckSpan) bool`: one span in, no slice, no view. | Per-span filtering on close with no Starlark call (D7 item 10, M5). | **Lint**: D4's depguard rule (it denies `go.starlark.net` outside `internal/praytest`) covers the first half. It is not in `.golangci.yml` yet, so the D4 task must land before or with `internal/selector`. **Test**: a compile-time assertion in `internal/selector/seam_test.go`: `var _ func(Selector, tui.CheckSpan) bool = Selector.Matches` (or D9's TUI-free span type, if D9 moves it). |
| S3 | `(*Case).Check(ctx, tui.CheckView) CheckResult` (D6's signature) runs with no child, pipeline or `caseResult`, and is re-entrant: a fresh thread per call, and no package-level mutable state reached from a check. | Per-trace watch calls it once per settled trace. Per-span watch runs predicates from a consumer goroutine while the frozen module is shared. D8's self-check already needs the in-memory half. | **Test** `TestCheckReentrant` in `internal/praytest`: load one file, run `Check` from 8 goroutines on distinct in-memory views, and assert each result equals the sequential result. CI does not run `-race` today (no `-race` in `Makefile`, `dagger/` or `.github/workflows/`), so D11 adds `go test -race ./internal/praytest/...`. Without `-race`, the test still catches shared-state corruption that changes results. |
| S4 | `Failure.Kind` and `Failure.Builtin` stay as D6 defines them. `always`/`never` failures carry `Builtin` `"always"`/`"never"`, `Pos` at the call, the offending span in `Offenders`, and a `Kind` that is `ZeroMatch` only for an empty selection (`Tripped` for an offending span, per D6). | Watch filters on these fields (it drops `ZeroMatch`, which D7 makes a post-drain rule) and prints S6's `VIOLATION` line from `Pos`. | **Test**: D6's per-kind M6 tests, extended with one table row per `always`/`never` outcome (tripped, false, zero match) asserting `Builtin`, `Kind` and `len(Offenders)`. |
| S5 | v1 does not build any of these: a per-span evaluator, a collect/property-extraction phase, a span-graph resolver in the Starlark span binding, `Recording` repro fields, an expect-fail `SelfCheck` mode, or a watch benchmark. | Keeps v1 semantics post-drain only, and leaves v1.1 free to pick its model. | **Task plan**: no v1 task may contain these, and the task critic checks every task list against this row. This is the only absence rule. It is enforced at planning time, where scope is set, not in code review. |

B's rule "keep the `always`/`never` selector and predicate as separate values on `checkState`" is **dropped**. It could only be checked in review, it guards internal code that v1.1 can change in one small edit, and the per-trace model does not need it. C's seam "extraction over any span subset" is also **dropped**. Per-trace watch can copy one trace's `spanView`s into a scratch `Model` and call the existing `CheckView()` (D1 item 5 already does this copy), so v1 needs no refactor.

### 2. C and "immediately alerted"
C does not meet the human's goal well enough to adopt now. For short request-scoped traces (`shop`, HTTP handlers) it alerts about Q (around 1-2s) after the root closes, which is close enough to immediate. But the bug-hunter persona's context is "long-running server or repeated test loops" (vision §2). There C fails outright for long-lived, streaming or background traces: the alert can wait until the child exits, which is the opposite of "immediately". It also adds a Q heuristic and stops properties that come after a `run.one`. C stays as the **named fallback for v1.1**. It is chosen only if the gating spike (point 4) shows per-span cannot meet M5, or the live `parent = None` difference proves unusable. **No vision text changes now.** If v1.1 adopts C, these vision lines change (proposed text, for the human at that time):
- S6, first sentence: "evaluates each `always`/`never` from `props.praytest.star` as each span closes." → "runs each case's `check` from `props.praytest.star` against each trace once it completes (its root has closed and no span in it has been open for 1s, or the command has exited), and reports `always`/`never` violations."
- §2 Bug hunter job: "I want to be told immediately" → "I want to be told as soon as the affected request finishes".
- §5 Pipeline: "and `--watch` on `SpanClosed`" → "and `--watch` per completed trace".
- §6 P4 row: "so it evaluates on `SpanClosed`" → "so it evaluates each trace as it completes".
- M5 target: "p99 ≤ 20µs per property per span. No blocked pipeline at 10k spans/s × 10 properties." → "p99 ≤ 20µs per property per span, amortised over each completed trace's check. No blocked pipeline at 10k spans/s × 10 properties."

### 3. v1 interfaces: fixed now vs left open
Safe to fix now (v1 needs them for its own reasons, and both v1.1 models use them unchanged):
- `Failure{Pos, Kind, Builtin, Offenders, ...}` and the `Kind` enum (D6).
- `(*Case).Check(ctx, tui.CheckView) CheckResult` (D6). The `[]Failure` return quoted in D8 and option C is superseded by D6's `CheckResult`.
- `toCheckSpan(*spanView) CheckSpan` with `Parent`/`Trace = -1` (D1). It is unexported, so changing it later is cheap anyway.
- `selector.Selector.Matches(CheckSpan) bool` and `Select` (D7).
- The `CheckSpan`/`CheckView` field set (D1).

Must stay open (v1.1 decides these, with the spike's numbers):
- Per-span vs per-trace evaluation.
- The per-span entry point (an API like A's `Observe`) and how it reaches a predicate.
- Property extraction: a collector `run`, or D5's additive Collect phase.
- Starlark thread reuse across spans, and how D6's cumulative step cap applies to a long-lived thread.
- Live `parent`/`trace` resolution: `None` at close (D7 allows it) vs deferring the span until its parent links.
- Whether to extract `internal/tracemodel` (D1's M5 revisit).
- `Recording` repro fields, the expect-fail self-check and repro naming (D8 item 12).
- M5's exact benchmark definition.

### 4. Gate before any v1.1 code
v1.1 starts only when all three hold:
1. **v1 measures hold**, as the vision requires (§7): v1 has shipped, M1-M4, M6 and M7 are met, and the `trace-tests` job has been required on `main` with no flaky failure for the M7 window.
2. **M5 spike, timeboxed to 2 days, on a throwaway branch.** A headless fold (as `Snapshot` does) of a synthetic 10k spans/s stream, with traces from 1 to 50 spans, and 10 `always`/`never` properties modelled on S4's lambdas. Measure in one harness:
   - (a) per span: `toCheckSpan` + 10 × `Matches` + a predicate call on a reused thread, as p99 per property per span (target ≤ 20µs; `Matches` alone ≤ 2µs, per D7);
   - (b) per trace: scratch-model `CheckView()` + `Check` per completed trace, amortised per span;
   - (c) whether ingest blocks (the receiver's channel never stays full for more than 10ms).
   Also record the step count of a reused thread after 1M spans, against `MaxSteps`.
3. **A2 smoke test**: during the spike, hand-wire one violation of the S4 property in `shop` to a repro written through `Generate`/`SelfCheck`/`WriteNew`. Confirm that `praytel test` on that repro exits `4` before the fix and `0` after it. If that cannot be made to work within the timebox, A2 is in doubt and v1.1 goes back to the PM before any more work.

The v1.1 design picks per-span if (a) meets its target. Otherwise it picks per-trace if (b) meets its target, and the vision amendments in point 2 then go to the human. If neither does, it extracts `internal/tracemodel` first (D1 revisit).

**Why it wins on the criteria** (decider scores):
- **Cost: 5.** Tests only, no production code; the depguard rule is D4's.
- **Rework: 3.** The fold, matcher, evaluator, `Failure` and generator are reused under both models. The per-span entry point and property extraction are v1.1 work, as in B.
- **Semantics: 5.** v1 stays post-drain, and nothing live-aware enters the binding.
- **M5: 3.** No v1 benchmark, but a defined, cheap spike measures both models before anything is committed.
- **Abandon: 5.** If A2 fails, the four seam tests still guard v1 behaviour that D8 and D9 use.

Total **84**. It beats B (82) because its rules are enforced, and C (82) because it does not trade away the vision's "immediately" before anything has been measured.

We give up:
- **Early M5 evidence from v1.** Nobody will know whether per-span watch fits in 20µs until the spike after v1.
- **Rework avoidance on the per-span path.** The per-span entry point, property extraction and thread-reuse rules are all v1.1 work, perhaps 1-2 weeks.
- **A's single predicate path** shared by batch and live. Per-span watch in v1.1 will be a second caller of the predicate, and the two can drift apart.
- **C's parity** between watch and file-run semantics, for now. If per-span wins, `parent`/`trace` may be `None` live where a file run sees a span, and that difference must be documented.
- **A committed v1.1 shape.** The vision's per-span promise stands, but it depends on the spike and is not designed yet.

Rejected:
- A: 600-900 LOC of guessed, unvalidated v1 code with no v1 user (R8), a collect phase with a known hole, and live semantics woven into the v1 span binding before M5 is measured.
- B: the right base, but its rules are prose that a refactor erodes unnoticed, it relies on a depguard rule that does not exist yet, and one rule (selector/predicate on `checkState`) guards internal code that needs no guard.
- C: its v1 seams are kept, but adopting its model now gives up the bug hunter's "immediately" (alerts may wait until child exit on long-lived traces), adds a Q heuristic and needs a vision amendment, all before any number shows that per-span fails.

Revisit if:
- **Spike (a) misses:** per-span p99 > 20µs per property per span, or `Matches` > 2µs p99, or ingest blocks for > 10ms at 10k spans/s. Then v1.1 adopts per-trace (C) with the point-2 amendments, or extracts `internal/tracemodel` first.
- **Seam pressure:** a v1 task needs to edit or delete one of the four seam tests (S1-S4), or a v1 task plan includes an S5 item. Then reopen D12 instead of changing the test.
- **Thread reuse:** the spike shows a reused thread reaching 10% of `MaxSteps` within 1M spans. Then v1.1 must create threads per span or per batch, and that cost goes into the M5 measurement.
- **A2 doubt:** the A2 smoke test fails, or the v1 measures are not all met 3 months after v1 ships. Then v1.1 is parked. The S1-S4 tests stay (they guard v1 behaviour), and nothing else needs deleting.
- **D9 moves `CheckSpan`:** if D9 gives `internal/selector` a TUI-free span type, point the S2 compile assertion at that type (same rule, new type name).

Consequences for other decisions:
- **D1 (decided):** consistent. D12 pins one detail D1 left open: `toCheckSpan` returns `Parent`/`Trace = -1`, and extraction fills them. What a live `-1` means (`None` or deferral) stays open for v1.1.
- **D4 (decided):** the depguard rule D4 specifies must land before or with `internal/selector`, because S2 depends on it. Today `.golangci.yml` has no depguard, so D7's "depguard already enforces this" is true only after that task.
- **D5 (decided):** no Collect phase in v1. D5's revisit trigger ("D12 decides watch needs properties as data") is **not** fired; property extraction is left to v1.1.
- **D6 (decided):** `TestCheckReentrant` (S3) and the `always`/`never` rows in the per-kind tests (S4) join D6's task. The signature `Check(ctx, CheckView) CheckResult` takes precedence over the `[]Failure` form quoted in D8 and option C.
- **D7 (decided):** adds the `Matches` signature assertion (S2). D7's `Matches` > 2µs revisit trigger is measured in the v1.1 spike.
- **D8 (decided):** consistent with item 12. v1 builds no repro fields and no expect-fail mode.
- **D9 (open):** if it moves recorder logic or the span type to break the `selector` → `tui` cycle, `Matches` must still take one span and return a bool (S2).
- **D10 (open):** none.
- **D11 (open):** CI must run `go test -race ./internal/praytest/...` (for S3) and enable depguard (D4).
- **Task planning:** the task critic checks every v1 task list against S5.
- **Vision:** no amendment. The point-2 amendments apply only if v1.1 adopts per-trace evaluation.
