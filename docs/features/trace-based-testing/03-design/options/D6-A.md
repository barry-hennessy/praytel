## Option A: Soft expects, hard `run.one` (testify assert/require)   (stance: most conventional)
Summary: `expect.*` are soft assertions. They append a failure to a per-check list and return `None`, like testify `assert`, JUnit `assertAll` and Playwright `expect.soft`. `run.one` is the hard assertion, like testify `require`, `t.Fatal` or a Playwright strict-locator throw. On 0 or >1 matches it records its failure and returns an `error`, which ends the check, and everything recorded before it is kept. The case fails at the end if the list is non-empty. A global step cap with a flag override and the case `timeout` bound execution, which is the usual "default limit plus override" convention.

How it works (all in `internal/praytest`, per D4):
- `func (c *Case) Check(ctx context.Context, v tui.CheckView) CheckResult`. `CheckResult{Failures []Failure; Cancelled bool}`. `Failure{Pos Position; From *Position; Kind, Subject, Detail, Trace string; Offenders []string; More int}`. One renderer produces the S2/S4 line: `file:line:col: <kind> <subject>: <detail> in trace <trace>`.
- A fresh `Thread` per check gets `Local("praytest.phase")="check"`, a `*[]Failure` in `Local("praytest.failures")`, and `SetMaxExecutionSteps(cap)`. Builtins are recreated per evaluation (spike caveat).
- Position: the innermost Starlark frame, `cs[len(cs)-2]` (spike (c)). This is the assertion line, which is what every mainstream runner reports. If that frame's file is not the `.praytest.star` file (a `lib.star` helper), `From` is set to the outermost frame in the test file and printed as ` (called from f:l:c)`. This works like Go's `t.Helper`, but needs no new builtin. `expect.*` inside a lambda reports the lambda's line.
- After `run.one` fails, the check stops. The output has no "skipped" line, as testify `require` prints none. D7's misuse errors also abort.
- Runtime `EvalError`: the failures already recorded are kept, and one more is appended, `runtime error: <msg>`, at the innermost user frame. Exit 4, with no stack trace.
- Step cap: the default is 10,000,000 per check, including predicates, and it is shared with load per D5. `praytel test --max-steps=N` overrides it. A cap hit appends `exceeded step limit (N)` at the innermost frame. Inside always/never it adds the span being evaluated.
- Cancel: the D2 deadline goroutine sets an `atomic.Bool` before calling `thread.Cancel`. That avoids message matching (D4). The result is `Cancelled=true`, and the failures are discarded (D2).
- Failure order is recording order. Offenders are in `CheckView.Spans` order, 10 then `... and N more` (D7).

Steelman: most authors already know this split. "Check everything you can, and stop when a precondition fails" is how testify, JUnit 5, AssertJ `SoftAssertions` and Playwright all work, so `praytest.5` can say it in one line. `run.one` aborting makes cascades impossible by construction. Nothing downstream of a missing span can fail, print noise or produce an odd value, and no poisoned-span type has to be designed. Every mechanism used is already demonstrated in the spike: record and continue, a returned error aborts, CallStack, the cap, and Cancel.

Pros: one rule; no sentinel values; zero cascades; failure order is deterministic; small code.

Cons / failure modes / hidden costs:
- If `run.one` fails, independent expectations after it do not run. This partly breaks A3. In a check like `span = run.one(...)` followed by `expect.absent(...)`, the second failure surfaces only after the first is fixed. The workaround is ordering by convention ("put `run.one` last" or split cases). That is guidance, not a guarantee.
- A runtime error in a lambda also hides later expects.
- Silent skipping leaves the author unaware of how much went unchecked.
- `--max-steps` is new CLI surface, and a single global cap is coarse for heavy predicates.
- "Called from" shows only one hop. Deep helper chains lose the middle frames.

Criteria scores:
- Failure clarity (25): 4. Positions are always present, helpers get "called from", and there are no stack traces. Skipped work is invisible.
- All independent failures (20): 3. Soft expects report everything, but `run.one` and runtime errors truncate the list.
- No cascades (15): 5. The abort removes dependents by construction, and absent attributes go through D7's marker.
- Bounded (15): 5. The cap plus Cancel, both spike-verified.
- Spike-validated (15): 5. Every mechanism appears in spike-starlark.md.
- Deterministic (10): 5. Recording order, deterministic steps, and Cancel results discarded.

Cost: M     Reversibility: easy. Making `run.one` soft later only adds failure lines. The verdict stays at 4, so it changes goldens but no file.

Unknowns / assumptions: 10M steps is assumed to clear M5's per-span budget at 10k spans. That needs a benchmark. The "called from" rule assumes users accept one hop.
