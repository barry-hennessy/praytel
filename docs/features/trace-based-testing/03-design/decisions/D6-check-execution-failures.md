# D6: How does a check execute and record failures without try/except, including run.one failures, absent attributes, runtime errors, step limit, cancellation and failure positions?
Status: decided     Depends on: D2, D4, D7     Parent: D3
Vision refs: §4 Assertion model (`run.one` skips later expectations; absent attribute is a failure; `expect.*` record and continue), S2 failing/missing-attribute output, S4 (offender list, step limit), §7 v1 item 4; R1, R2; A3; M2, M6

**Holds the vision's early design spike (R1: "Collecting multiple failures this way is untested ... gets a design spike first").** The spike needs only a pinned `go.starlark.net` and should start in wave 1, in parallel with D1/D4, so its evidence is ready when D6's options are explored. The spike validates A3; it does not decide D6.

## Context
- Starlark has no exceptions: an error kills the thread; a script cannot catch and continue (synthesis §4, STRONG). Whether host builtins can record and continue is untested (synthesis §5).
- Vision commitments: every `expect.*` records a failure and continues (R1); each case in its own thread; `run.one(sel)` with 0 or >1 matches fails and "later expectations on it are skipped"; an absent attribute in a comparison is a failure, not a key error; a runtime error in a check is exit `4`; a predicate over the step cap fails with `exceeded step limit` (S4); `Cancel` on `timeout` (R2). Timeout scope and its `3` classification come from D2.
- Output format is fixed by S2/S4: `file:line:col: <kind> <selector description>: <detail> in trace <trace description>`, offenders listed up to 10 then `... and N more`, no raw stack traces (M6: 100% of failures carry file:line:col and a span description).
- Which frame's position to report is open when `expect.*` is called from a helper in a loaded `lib.star` (§4 `load()`), or inside a lambda passed to `always`/`never`.
- The span and trace descriptions in messages (`payments/Gateway.Bank.Authorize`, `web/Orders.Create{order.id="A-1001"}`) come from D7's semantics; how the identifying attribute of a trace is chosen is not stated in the vision.
- Semantics of "skipped" (counted, printed, or silent) after a `run.one` failure are not specified.

## Goals
- All independent failures in a case are reported (A3); dependent ones are not.
- Every failure line has a stable file:line:col and a span description; output is byte-identical across runs of the same trace.
- Runaway checks are bounded by the step cap and `timeout`.

## Non-goals
- Adding try/except or any language extension.
- Retrying or re-running a check.
- Choosing which selectors/comparisons exist (D7).

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| Failure clarity: file:line:col, span description, no stack traces | 25 | M6; §7 v1 item 4; S2/S4 formats |
| Reports all independent failures per case | 20 | R1, A3 |
| No cascades or false failures after `run.one` failure or absent attribute | 15 | §4 assertion table |
| Bounded: step cap and `Cancel` stop runaway checks | 15 | R2, S4 "can't hang CI" |
| Validated by the spike before dependent work | 15 | R1 "design spike first" |
| Deterministic output | 10 | M2 |

## Options
Full texts: `../options/D6-A.md`, `D6-B.md`, `D6-C.md`. All three share the spike-verified core: a fresh thread per check, `expect.*` append to a thread-local list and return `None` (spike A3), a returned `error` aborts, position from `CallStack()[len-2]` (spike (c)), `SetMaxExecutionSteps` and `Cancel` (spike (a), (b)). They differ on what a failed `run.one` does, positions, the step cap and the record type.

### Option A: Soft expects, hard `run.one` (most conventional)
testify `assert`/`require` split. A failed `run.one` records its failure and ends the check silently. Innermost-frame position plus one `called from` hop when the frame is outside the test file. 10M step cap per check with a `--max-steps` flag. Cancel detected with an `atomic.Bool` set before `thread.Cancel`. Cost M.

### Option B: Structured failure records as the one verdict currency (most extensible)
New pure-Go `internal/verdict` package with typed `Failure`/`Skip`/`CaseVerdict` (kinds, severity, evidence indices) for four consumers. A failed `run.one` returns a frozen poisoned span; `expect.*` given it record a linked `Skip` (`(2 expectations skipped)`). Full `called from` chain. Column normalised to the callee start through the parsed `syntax.File`. Per-assertion step budget by raising the cap mid-run. Cost M-L.

### Option C: Spike mechanism as-is, run.one stops the check (minimal change)
String `Failure` in a thread-local slice. A failed `run.one` returns `errStopCheck`, recognised with `errors.Is` on the `EvalError` cause. Raw `(` column, innermost frame only, no chain. Fixed 1M cap. Cap and Cancel told apart by message suffix. The `eq` subject is the last span read on the thread. About 300 LOC. Cost S.

### Verified and discounted claims
- `go.starlark.net` source is not in the local module cache (`~/go/pkg/mod` has no `go.starlark.net`), so no interpreter internals could be checked. The only evidence is `spike-starlark.md` and the spike code (`scratchpad/starlark-spike/main.go`, which confirms `EvalError.CallStack` and `ExecutionSteps()` are used and compile on the pin).
- **C's `errors.Is` on the `EvalError` cause** is not measured (C's own unknowns). Discounted. The chosen design does not need it: the builtin writes a stop record into thread-local state *before* returning its error, and the host reads that state.
- **C's message-suffix matching** works (spike (a), (b) show the strings), but it is needed for neither case. Cancel uses A's flag set before `Cancel`; the step cap is detected by `ExecutionSteps() >= cap`, which spike (a) measured ("`ExecutionSteps()` equals the cap exactly").
- **B's per-assertion budget** depends on two unmeasured behaviours: raising `SetMaxExecutionSteps` mid-run, and continuing on a thread after a step-cap cancel. B itself says the thread may stay cancelled. Discounted to "global cap with a better position", which the chosen design gets without it.
- **B's poisoned span** is verified only for direct use as an `expect.*` argument (a Go type assertion, like D7's absent marker). Derived uses are not: `"k" in span.attrs`, `span.duration_ms > 200` and `len(span.attrs)` would produce a false `False`, a runtime error, or behaviour that depends on untested interpreter rules for custom types. `expect.eq("card.type" in span.attrs, True)` after a failed `run.one` would be a false failure. Scored down on cascades and validation.
- **B's callee-column lookup** (`CallExpr.Lparen` → callee start) relies on `syntax` package APIs that are long-standing upstream but not exercised by the spike. Adopted with a mandatory first test and a fallback (Decision step 3).
- **C's `lastSpan` subject** is wrong for `eq(a.attrs["x"], b.attrs["y"])` and for any value read earlier into a variable (C's own cons). Rejected per point (5): a subject must be exact.
- **Every option's subject for `eq`**: no option explains how a comparison builtin, which receives a plain `str`/`int`, learns the span exactly. A has `Subject string` with no source; B has `Subject int` with no source. Neither credited on this point.

### Scores (decider; weight × score / 5; explorers' self-scores in brackets where changed)
| Criterion (w) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| Failure clarity (25) | 3 [4]: `(` column contradicts S2/S4; `eq` subject has no source | 4 [5]: same `eq` subject gap | 2 [3]: `(` column, wrong-span subject, no helper context | 5 |
| All independent failures (20) | 3 | 4 [5]: derived use of a poisoned span is a runtime error that ends the check anyway | 3 | 3 |
| No cascades / false failures (15) | 5 | 3 [5]: derived values from a poisoned span can fail falsely | 5 | 5 |
| Bounded (15) | 5 | 3 [4]: budget rests on untested cap behaviour | 5 | 5 |
| Spike-validated (15) | 5 | 2 [3]: poison in operators, mid-run cap, column lookup | 4 [5]: `errors.Is` unwrap unmeasured | 4 |
| Deterministic (10) | 5 | 5 | 5 | 5 |
| **Weighted /100** | **82** | **70** | **74** | **89** |

## Decision
Chosen: **Hybrid, A as the base.** A's soft-`expect` / hard-`run.one` rule and its "called from" hop; C's fixed cap with no new flag; B's callee-column lookup and an internal typed `Failure` (no new package, no severity, no budget, no poisoned span); plus two things none of the options had: the comparison subject is the argument's source text, and every early stop is announced on its failure line. Specification, all inside `internal/praytest` (D4):

1. **Entry point.** `func (c *Case) Check(ctx context.Context, v tui.CheckView) CheckResult`, with `CheckResult{Failures []Failure; Stopped bool; Cancelled bool; Steps uint64}`. It needs only a `CheckView` and the loaded `Case`, not a child process, a pipeline or a deadline beyond `ctx`. So it can run against a recorded or live view (D8 dry run, D9 Ctrl-W): see Consequences.
   Internal type, not exported outside the package except through `CheckResult`:
   `Failure{Pos Position; From *Position; Kind Kind; Builtin string; Subject string; Detail string; Offenders []Offender; More int; Stops bool}`, `Offender{Span, Extra, Trace string}`, `Kind` ∈ {`Cardinality`, `ZeroMatch`, `Compare`, `TypeMismatch`, `Absent`, `SpanOpen`, `Tripped`, `Misuse`, `Runtime`, `StepLimit`}. One function, `(Failure).Lines() []string`, renders the text below. `Kind` exists so tests can assert M6 per kind and so D12 can read it; it adds no user syntax.

2. **Thread setup.** A fresh `Thread` per check; `Local("praytest.phase")="check"` (D5); a per-check `*checkState` in `Local("praytest.state")` holding the failure list, a `stop *Failure`, the current offender span for `always`/`never`, and the file's call index (step 3). `SetMaxExecutionSteps(MaxSteps)`. Builtins are created per check (spike caveat). The D2 deadline goroutine sets `state.cancelled` (an `atomic.Bool`) and then calls `thread.Cancel("praytel: timeout")`.

3. **Positions.**
   - The frame is the innermost Starlark frame, `CallStack()[len-2]` (spike (c)). For a runtime error or step-cap hit, it is the innermost frame of `EvalError.CallStack`.
   - **Column = start of the callee expression, not the `(`.** At `Load` (D5), each file and each loaded helper is parsed into a `*syntax.File` and compiled from that same tree (`starlark.FileProgram`), so positions agree. One `syntax.Walk` builds `map[syntax.Position]syntax.Position` from each `CallExpr.Lparen` to the start of `CallExpr.Fn`'s span, and the byte range of each call's arguments. Only the map and the source bytes are kept, not the tree. A frame position found in the map is replaced by the callee start; one not found (a binary operator in a lambda, say) is printed as is. This gives `13:5`/`4:5` as S2/S4 show, with no vision change. A multi-line call reports the line where `expect` starts.
   - **Helpers.** If the frame's file is not the case's `.praytest.star` file, `From` is the innermost frame that *is* in that file, printed as ` (called from shop.praytest.star:12:5)`. One hop only. `expect.*` inside a lambda defined in the test file reports the lambda's line, with no `From`. Offender failures from `always`/`never` are positioned at the `always`/`never` call.
   - **Mandatory first test** (before any other D6 work, in `internal/praytest/position_test.go`): on the pinned interpreter, assert that `CallStack()[len-2].Pos` equals a `CallExpr.Lparen` in the parsed tree for a top-level call, a call inside a `def`, a call inside a `lambda`, a call in a loaded helper and a call split across lines. If it does not hold, keep the raw position everywhere and apply the fallback vision amendment below. The rest of this design does not change.

4. **`run.one` failure stops the check, visibly.** On 0 or >1 matches, `run.one` appends a `Cardinality`/`ZeroMatch` failure with `Stops=true`, sets `state.stop`, and returns an `error`. The interpreter aborts the check. Failures recorded earlier are kept. The host never inspects the error's cause: it reads `state.stop`. The failure line ends with `; rest of check not run`. Nothing is counted or listed for the skipped statements, because the host cannot know them without running them.

5. **Classification of `Exec`'s error, first match wins** (no message matching anywhere):
   1. `state.cancelled` → `Cancelled=true`; failures are discarded (D2 step 6.3).
   2. `state.stop != nil` → `run.one` stop or misuse (step 7). `Stopped=true`.
   3. `thread.ExecutionSteps() >= MaxSteps` → append `StepLimit` at the innermost `EvalError` frame. `Stopped=true`.
   4. Otherwise → append `Runtime` with `EvalError.Msg` (never `Backtrace()`) at the innermost frame. `Stopped=true`.
   For 3 and 4, if `state` holds a current `always`/`never` offender (set before each predicate call, cleared after), the line names that span and its trace. Any failure, runtime error or step-limit hit is `FAIL` (exit 4, D2 step 6.5).

6. **Step cap: one fixed constant, `MaxSteps = 10_000_000` per check thread and per file load (D5).** No `--max-steps` flag and no per-assertion budget in v1. 10M rather than 1M because one `always`/`never` lambda costs tens of steps per span, and M4's 10k spans × several predicates already reaches hundreds of thousands. D4's step-corpus fingerprint is the guard against an upstream step-unit change.

7. **Misuse** (D7's bad `status`, bad attr value type, conflicting `where`, `count=0`; D5's `env()`/`test()` in a check; wrong argument count or type) is recorded as `Misuse` with `Stops=true` and `state.stop` set, then the builtin returns an error, as in step 4. Exit 4 (D7). Phase errors at *load* stay D5's `UsageError`s (exit 1).

8. **Comparison subject: exact, from source.** For `eq/ne/lt/le/gt/ge`, `Subject` is the source text of the first argument, taken from the call's byte range in step 3. Whitespace runs are collapsed to one space and the text is cut at 60 bytes with `...`. The span is named **only when it is known exactly**: when an argument is D7's absent marker or the open marker (Consequences: D7), both of which carry their span and trace. No heuristic such as "last span read" is used. If the call index has no entry (fallback), the subject is omitted.

9. **Wording** (D6 owns it, per D7). Values are printed as Starlark literals (`String()`), cut at 80 bytes. A span is `svc/name`, a selector and a trace follow D7 item 9. The report indents a failure line by 4 spaces (S2), and continuation lines by 6 spaces.

   | Situation | Line after `file:line:col: ` |
   |---|---|
   | `exists`, 0 matches | `exists <sel>: 0 spans matched` |
   | `exists`, wrong count | `exists <sel>: 3 spans matched, want 1:` + offenders |
   | `absent`, ≥ 1 match | `absent <sel>: 1 span matched: payments/Gateway.Bank.Authorize in trace web/Orders.Create{order.id="A-1001"}` (S2) |
   | `run.one`, 0 / > 1 | `one <sel>: 0 spans matched; rest of check not run` / `one <sel>: 2 spans matched; rest of check not run:` + offenders |
   | `never` tripped | `never <sel>: tripped by payments/Gateway.Bank.Authorize (452ms) in trace web/Orders.Create{order.id="A-1002"}` (S4) |
   | `always` false | `always <sel>: false for <span> (<ms>ms) in trace <trace>` |
   | `always`/`never`, 0 matches | `never <sel>: 0 spans matched` (no A4 hint in v1) |
   | `eq` / `ne` false | `eq span.status: got "ok", want "error"` / `ne span.name: got "X", want anything but "X"` |
   | `lt/le/gt/ge` false | `lt span.duration_ms: got 452.3, want < 200` |
   | cross-type order | `lt span.attrs["n"]: cannot compare string "2" with int 2` |
   | absent attribute | `eq span.attrs["payment.result"]: attribute payment.result is absent on payments/Gateway.Charge in trace <trace>` (contains S2's text) |
   | open span (live views only) | `lt span.duration_ms: span still open: payments/Gateway.Charge in trace <trace>` |
   | misuse | `expect.exists: count must be >= 1; use expect.absent; rest of check not run` |
   | runtime error | `runtime error: <EvalError.Msg>[ on <span> in trace <trace>]; rest of check not run` |
   | step cap | `exceeded step limit (10000000 steps)[ on <span> in trace <trace>]; rest of check not run` |

   Offenders: exactly one → inline, as S2/S4. Two or more → the header ends with `:` and each offender is a continuation line `<span> [(<ms>ms) ]in trace <trace>`, at most 10, then `... and N more`, in `CheckView.Spans` order (D7). Durations are `math.Round` milliseconds. If both arguments of a comparison are markers, the first is reported. Failures print in recording order.

10. **Cancel** produces no failure line; D2's `timeout 10m exceeded while checking` is the report.

Why it wins on the criteria:
- **Clarity (25):** callee columns match S2/S4 as written; comparison subjects are exact by construction; helper failures show both the helper line and the test-file line; runtime errors and step-cap hits inside predicates name the span; never a backtrace. The only gap is that a comparison on a present attribute names the expression, not the span (amendment below).
- **Independent failures (20):** every `expect.*` records and continues (spike A3). A failed `run.one`, misuse, runtime error or step-cap hit ends the check, and the line says so. This is A3's documented "worse but acceptable" shape, applied only after a precondition failure, and authors can see where checking stopped.
- **No cascades (15):** nothing after a missing span runs, so nothing can fail falsely. No poisoned value exists.
- **Bounded (15):** fixed cap and `Cancel`, both spike-measured, told apart without strings.
- **Validated (15):** recording, aborting with an error, `CallStack`, the cap, `ExecutionSteps` and `Cancel` are all in the spike. The one unmeasured piece, the `Lparen` lookup, has a mandatory first test and a fallback that changes only columns.
- **Deterministic (10):** recording order, `CheckView` order, step counts are deterministic, a cancelled check's failures are discarded, and no output depends on time except span durations, which are fixed for a given trace.

We give up:
- **Independent failures after a failed `run.one`.** In `span = run.one(A)` followed by `expect.absent(run, B)`, B's failure shows only after A is fixed. Recorded files with several `run.one` calls (D8) inherit this. Workaround: put `run.one` after selector-based expectations, or split cases. The `; rest of check not run` suffix makes the cut visible but does not count what was skipped.
- **B's skip accounting** (`(2 expectations skipped)`) and its explicit cause links.
- **Span names on ordinary comparison failures.** `eq span.status: got "ok", want "error"` names the expression, not `payments/Gateway.Charge`. Exact provenance would need typed wrappers for field values (which break Starlark `==`) or interpreter frame inspection (untested).
- **A tunable cap.** A check that legitimately needs more than 10M steps cannot run in v1.
- **Middle frames** of deep helper chains: one `called from` hop.
- **B's `internal/verdict` package and evidence indices** for the recorder, watch alert and repro consumers. `Failure` is internal and can be moved later.
- **Raw `(` columns' zero-cost simplicity.** Positions need a per-file call index built at load.

Rejected:
- A: the chosen base, but it reports `(` columns that contradict S2/S4, leaves the `eq` subject undefined, skips silently, and adds a `--max-steps` flag v1 does not need.
- B: its poisoned span can fail falsely on derived values, and its per-assertion budget and mid-run cap rest on untested interpreter behaviour; about 3x the code for consumers that do not exist in v1.
- C: its `lastSpan` subject can name the wrong span, its stop detection relies on an unmeasured `EvalError` unwrap, it prints `(` columns, and 1M steps is tight for M4's 10k-span checks.

Revisit if:
- **Lost failures:** 2 or more dogfooding/M7 files or issues where a fix of a `run.one` failure revealed another failure in the same check, or D8 ships recorded checks with 2 or more `run.one` calls. Then add skip-on-poison for direct `expect.*` arguments only, after a pin test of the poison value under `in`, `==`, `<` and truth. It is additive: more lines, same exit code.
- **Step cap:** any dogfooding check uses more than 1M steps (10% of the cap; log `CheckResult.Steps` in `-v` or tests), or a D4 step-corpus diff exceeds 10% on a bump. Then reconsider the value, or add `--max-steps` (additive).
- **Columns:** the mandatory position test fails on the pin or after a bump. Then apply the fallback amendment.
- **Span names:** 2 or more issues ask which span a comparison failure was about. Then evaluate `DebugFrame` locals or typed field values, behind a pin test.
- **Helpers:** 2 or more issues ask for the full call chain. Then print all frames (text-only change).

Consequences for other decisions:
- **D2 (decided):** consistent. The deadline goroutine sets the cancel flag before `thread.Cancel`; a cancelled check's failures are discarded; a step-cap hit is FAIL. D2's "distinguish by message" is replaced by flag and step count, which is stricter, not a conflict.
- **D4 (decided):** consistent. Everything stays in `internal/praytest`. D4 said step cap and Cancel "must be told apart by message"; this design needs no message matching, which only removes a dependency on upstream wording. `MaxSteps = 10_000_000` is the cap D4 left to D6.
- **D5 (decided):** `Load` must parse to a `*syntax.File`, compile from that tree, and build the `Lparen → callee` index for the file and for each loaded helper (cached with the frozen module). D5's `UsageError` positions (from `test()` misuse) use the same normalisation, so load-time and check-time columns agree. The load step cap is `MaxSteps`; a hit is a `UsageError` (exit 1), as D5 says.
- **D7 (decided), one amendment requested:** D7 item 5 says `duration_ms` is `None` on an unclosed span and that `expect.*` then records `span still open: svc/name`. A plain `None` does not carry its span, so that promise cannot be kept exactly. Request: `duration_ms` on an unclosed span returns an **open marker** built like the absent marker (opaque, carries the span, falsy, Go type assertion in `expect.*`, behaviour elsewhere unspecified). File runs never see it (unclosed spans are INCOMPLETE). If D7 keeps `None`, the line becomes `<expr>: span still open (duration_ms is None)` without a span name. All of D7's other wording is taken over as specified above.
- **D8 (open):** yes, D6's check is buildable as a dry run against a recorded trace without a child. `(*Case).Check(ctx, view)` needs only the loaded `Case` and a `CheckView`, so D8-C can call it on `(*Model).CheckView()`. Two conditions: (a) D8 must load the generated bytes through D5's `Load`, which today takes a path (write a temp file, or D5 adds a `LoadSource(name, src)` sibling with the same semantics); (b) the live view may contain unclosed spans, so the open-marker amendment to D7 matters for it. Recorded checks with several `run.one` calls stop at the first failure; D8 should emit selector-based expectations before `run.one` blocks where it can.
- **D10 (open):** the runner prints `Failure.Lines()` under the `FAIL` line with the indents in step 9, and maps `CheckResult` to an outcome per D2 step 6. Report goldens should cover each row of the wording table.
- **D12 (open):** `Failure.Kind` and `Pos` are available for the S6 `VIOLATION` line (`props.praytest.star:4` is `Pos` without the column). `Check` runs a whole check; per-span evaluation of `always`/`never` is D12's question and is not built here.

**Vision amendments required:**
- **§4 assertion table, `run.one` row, "Fails when":** replace "0 or >1 match. Later expectations on it are skipped." with "0 or >1 match. The rest of that check does not run, and the failure line says so; failures recorded before it are kept."
- **M6, Target:** replace "100% of failures have file:line:col and a span description. 0 raw stack traces." with "100% of failures have file:line:col. Every failure about spans names its selector or span (`svc/name`); comparison failures name the compared expression as written. 0 raw stack traces."
- **A3:** replace "Host builtins that record and continue give \"report all failures\"." with "Host builtins that record and continue give \"report all failures\" up to the first failed `run.one`, runtime error or step-limit hit in a case."
- **No change to S2's `13:5` or S4's `4:5`.** Fallback only if the mandatory position test fails: S2 `gift.praytest.star:13:5:` becomes `gift.praytest.star:13:18:`, S3 and S4 `props.praytest.star:4:5:` becomes `props.praytest.star:4:17:`, and §7 item 4 adds "(the column is the call's opening parenthesis)".
