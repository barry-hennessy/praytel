## Option C: Spike mechanism as-is, run.one stops the check   (stance: minimal change)
Summary: Ship exactly what the spike measured. `expect.*` appends a `Failure` to a per-thread slice and returns `None`. Misuse returns an `error`, which aborts the check and gives exit 4. A failed `run.one` records its failure and then ends the check with a sentinel error, which counts as an ordinary FAIL. Nothing new is built for spans that are absent or skipped.

How it works (all in `internal/praytest`):
- `Failure{Pos Position; Kind, Subject, Detail, Trace string}` is stored in `Local("praytest.failures")`. Builtins are re-created per check (spike caveat). The entry point is `(*Case).Check(ctx, tui.CheckView) CheckResult{Failures []Failure; Stopped bool; RuntimeErr *Failure; Cancelled bool}`.
- Position is `CallStack()[len-2].Pos`, the innermost user frame. That is the helper's line inside a `lib.star` and the lambda's line inside `always`/`never`. Offender failures from `always`/`never` take the `always`/`never` call site. There is no "called from" chain.
- `run.one` with 0 or >1 matches records `one <sel>: 0 spans matched` (or `3 spans matched: a, b, c`) and returns `errStopCheck`. The host checks `errors.Is` on the `EvalError` cause, does not count it as a runtime error, and sets `Stopped`. Every statement after it is skipped and nothing about them is printed.
- The step cap is one constant, `maxSteps = 1_000_000`, applied at load and in checks. `Cancel(cancelReason)` uses a fixed praytel string. The host tells the two apart by matching the `EvalError.Msg` suffixes `too many steps` and `cancelled: <cancelReason>` (D4). A step-cap hit becomes `exceeded step limit` at the innermost frame. A cancel discards the failures (D2).
- Runtime errors print as `pos: runtime error: <Msg>` using the innermost frame of `EvalError.CallStack`, with no backtrace.
- Wording: `attribute k is absent on svc/name`, `span still open: svc/name`, `cannot compare string "2" with int 2`, `0 spans matched <sel>`, `got X, want Y`. Misuse reuses D7's strings. The subject of `eq/lt/...` is the last span whose field was read on the thread (`Local("praytest.lastSpan")`). If no span was read, the subject and `in trace` are left out.
- Failures print in the order they were recorded. Rollout: one PR.

Steelman: The spike shows every mechanism working, so this option has no untested parts. That fits R1 and the 15-point "validated" criterion exactly. Stopping at `run.one` is the simplest rule that cannot cascade. No phantom `None.status` errors appear and no marker semantics need pinning. Most S2-style checks open with `run.one`, so in practice "later expectations on it" are the rest of the check anyway. Execution order is deterministic, so the output is too.

Pros: smallest code, about 300 LOC. Every behaviour is pinned by the spike. Failures cannot cascade. The verdict has no ordering ambiguity.

Cons / failure modes / hidden costs:
- Independent `expect.*` calls after a failed `run.one`, including ones on other spans, are lost silently. This only partly meets A3 and the vision's "on it".
- The column points at `(`, so `expect.eq(` reports `13:14`, while S2 shows `13:5`. Either the vision example changes or the output looks odd.
- Helper and lambda positions point into `lib.star`, not the test file, so a shared helper failing in 5 cases prints 5 identical positions.
- The `lastSpan` subject is wrong for `eq(a.attrs["x"], b.attrs["y"])`. When no span was read, M6's "span description on 100%" is not met.
- Matching on message strings breaks quietly if upstream rewords. Only the D4 surface golden catches it, and only if a test asserts it.

Criteria scores:
- Failure clarity: 3. file:line:col and no stack traces, but the paren column, the heuristic subject and helper-only positions weaken it.
- Reports all independent failures: 3. All are reported up to a failed `run.one`, then none.
- No cascades/false failures: 5. The stop sentinel and D7's marker check rule out both.
- Bounded: 5. Step cap and Cancel are both measured.
- Validated by spike: 5. Nothing goes beyond spike (a), (b), (c) and A3.
- Deterministic: 5. Recording order, CheckView order, fixed strings.

Cost: S     Reversibility: easy. Replacing the stop with a "skipped span" value later only turns some lost failures into reported ones, an additive change to output. No file changes meaning.

Unknowns / assumptions: assumes `EvalError` unwraps to the builtin's returned error (not measured in the spike). Assumes 1M steps is far above real checks; the D4 step corpus should confirm it. Assumes authors accept "check stops at a failed `run.one`" as the documented reading of "skipped".
