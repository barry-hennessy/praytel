## Option B: Structured failure records as the one verdict currency   (stance: most extensible)
Summary: Every builtin records a typed `Failure` value (kind, positions, selector, evidence spans) instead of a string. All renderers derive their text from that value: the CLI report, D9's recorder view, the v1.1 watch alert and the D8/S6 repro generator. Severity (soft/hard) and a per-assertion step budget are fields on the record and in the thread mechanics, but v1 gives users no new syntax for them.

How it works:
- **New pure-Go package `internal/verdict`** (no Starlark import, so it passes depguard): `Failure{Kind; Severity Soft|Hard; Pos Position; Chain []Position; Builtin string; Selector selector.Selector; Subject int; Evidence []int; Total int; Detail{AttrKey, Want, Got domain.Value; Steps uint64}}`. Kinds are `Cardinality`, `ZeroMatch`, `Compare`, `TypeMismatch`, `Absent`, `SpanOpen`, `Tripped`, `StepLimit`, `Runtime`, `Misuse`. `Subject` and `Evidence` are indices into `CheckView.Spans` (D1), and evidence is stored untruncated. Alongside it: `Skip{Pos; Cause int}` and `CaseVerdict{Failures; Skips; Aborted *Failure; Cancelled bool; Steps}`.
- **`internal/praytest`**: `(*Case).Check(ctx, view) CaseVerdict`. It uses a fresh thread with `Local("praytest.phase")="check"`, a per-case `*verdict.Builder` in `Thread.Local`, `SetMaxExecutionSteps(cap)`, and a `ctx` watcher that calls `Cancel`. Builtins append through the builder and return `None` (spike A3). Only misuse returns an `error`.
- **`run.one` failure** records a Hard `Cardinality` and returns a frozen *poisoned span*. Each `expect.*` checks every argument with a Go type assertion for the poisoned span (as D7 does for the absent marker) and records a `Skip` linked to the cause instead of a `Failure`. The report prints `(2 expectations skipped)` under the cause.
- **Positions**: `Pos` is the innermost user frame (`cs[len-2]`, spike (c)). `Chain` keeps the outer user frames, so a helper in `lib.star` prints `called from shop.praytest.star:12:5`. Lambdas inside `always`/`never` report at the `expect` call, and a runtime error inside the lambda reports at the lambda's frame. Columns are normalised to the start of the callee expression, using the parsed `syntax.File` to look up which `CallExpr` has that `Lparen`, so the output matches S4's `4:5`.
- **Step cap and Cancel**: an `EvalError` is classified by its message (D4). A step-cap hit becomes `Aborted = StepLimit` with its frame, and the case FAILs (exit 4). A cancel sets `Cancelled`, and D2 discards the failures. **Per-assertion budget**: before calling a predicate, `always`/`never` raise the cap to `ExecutionSteps()+budget` and afterwards restore it. Hitting the budget records `StepLimit` with the offending span as the subject.
- **Rendering**: `verdict.Text(v, view)` produces the S2/S4 lines, with 10 offenders and then `... and N more`. D12 builds the S6 `VIOLATION` line from the same `Failure`. D8 gets `Selector.Source()` and `Kind` to emit a repro. A `--format=json` output later is only a serializer.
- **Rollout**: the types and the text renderer go into the first D6 PR, with golden tests on the text. Severity and budget have no user syntax in v1.

Steelman: the vision already names four consumers of one failure (the report, the recorder, the watch alert and the repro file). If failures are strings, each consumer re-parses text or re-derives facts. With a typed record, the report can't drift from the repro or the alert, M6 is checked as "every Failure has a Pos and a description" by a unit test instead of by regex, and determinism can be tested per field.

Pros: the dependency between failures is explicit, so there are no cascades and the skips stay visible. Evidence survives into watch and repro. Positions are richer than the S2 format without changing it. JSON, soft expectations or `require.*` become additive later.

Cons / failure modes / hidden costs:
- About 3x the code of a string-collecting design, and a new package. Four consumers are designed for, but only the CLI exists in v1. A2 may make the watch consumer moot (R8).
- Per-assertion budget: the go.starlark.net code is not in the local module cache, so it is unverified. Once the thread has been cancelled for "too many steps", it may stay cancelled (that would need `Uncancel` on this pin). In that case the case cannot continue after an overrun, and the budget reduces to the global cap with a better position.
- The poisoned span behaves outside `expect.*` the way D7's absent marker does, which is unspecified, so `if span.attrs[...]` after a failed `one` silently does nothing.
- Callee-column normalisation needs syntax trees to be kept per file, including loaded helpers.
- Index-based evidence ties the record to its `CheckView`, so watch must supply an equivalent view.
- Exposing severity or budgets later is public surface (R9), which is one-way.

Criteria scores:
- Failure clarity: 5 - positions, chains and descriptions all come from structured data, and the renderer is golden-tested.
- All independent failures: 5 - record-and-continue (A3), and only misuse aborts.
- No cascades: 5 - skips are linked explicitly to their cause, and absent and poisoned values are caught by type assertion.
- Bounded: 4 - the global cap and Cancel are validated. Per-assertion budgets depend on unverified cap/uncancel behaviour.
- Spike-validated: 3 - A3, call-site positions, the cap and Cancel were validated. The poisoned span, raising the cap mid-run and the column lookup were not.
- Deterministic: 5 - failures are kept in execution order, evidence in `CheckView` order, and nothing depends on time.

Cost: M-L     Reversibility: easy for the internal types. Exposing severity or budgets syntax would be one-way.

Unknowns / assumptions:
- Whether `SetMaxExecutionSteps` can be raised mid-execution, and whether a thread can continue after a step-cap cancel on pin `4f4267fb140d`.
- That S4's `4:5` column (the start of the callee) is intended, not the `(` that the spike observed.
- That D9 and D12 will actually consume `Failure`.
