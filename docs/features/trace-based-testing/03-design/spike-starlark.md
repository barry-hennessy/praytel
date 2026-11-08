# Spike: Starlark as the assertion language (R1 / A3)

Library: `go.starlark.net v0.0.0-20261021141710-4f4267fb140d` (no tagged release; `@latest` resolves to this pseudo-version). Go 1.26.2, darwin/arm64. Needed `go mod tidy` for golang.org/x/sys. Code: `/private/tmp/claude-503/-Users-build-Projects-barry-hennessy-praytel--claude-worktrees-feature-pipeline/1b30bdf7-848d-45ba-a456-720be40780b9/scratchpad/starlark-spike/main.go` (throwaway, repo untouched).

## A3: builtins record a failure and the script continues - YES
A builtin appends to a host-side slice and returns `None`. Only a returned `error` aborts.
```
exists("a")  exists("ok")  def f(): exists("b")  f()  x=[exists("c") for _ in range(1)]
err: <nil>
fail check.star:1:7 missing span a / check.star:4:11 ... b / check.star:6:12 ... c
```
One script reported three failures with no try/except. Implication: R1 is retired. Reserve a returned `error` for misuse (bad args), which aborts the script.

## (c) Call-site position from a builtin - YES, with a gotcha
`thread.CallStack()` is ordered outermost first, and the builtin is the LAST entry (`<builtin>`). Take `cs[len(cs)-2].Pos`. `cs.At(i)` counts from the innermost frame, so `At(len-2)` is wrong. My first attempt did exactly that and reported `<builtin>` for top-level calls.
```
cs[0] <toplevel> @ d.star:3:2 ; cs[1] g @ d.star:2:8 ; cs[2] dbg @ <builtin>
```
`cs.At(1).Pos` is the caller's line:col. The column points at the opening paren of the call (`exists("a")` gives 1:7, which is the `(` after `exists`, not the start of `exists`). Calls made inside helper functions report the helper's line, not the helper's caller. The full stack is available if "called from" chains are wanted.

## (a) Step cap - YES
`th.SetMaxExecutionSteps(100000)` stops both `while True: pass` and a 10M-iteration loop in a function.
- `*starlark.EvalError`; `Msg` is `Starlark computation cancelled: too many steps`. Detect with `errors.As`. The cause is a plain `errors.errorString`, so there is no typed sentinel and you must match the message string.
- `ExecutionSteps()` equals the cap exactly. `CallStack` holds the frames, e.g. `cap.star:5:2 <toplevel>` then `cap.star:4:11 f`. This gives a position for "script too expensive".
- `while` and top-level `for`/`if` are off by default. Enable them with `syntax.FileOptions{While:true, TopLevelControl:true}` through `ExecFileOptions`. A top-level `for` that reassigns a global fails at resolve time with `cannot reassign global n` (resolve.ErrorList, not EvalError). This is a dialect rule, not a bug.

## (b) Thread.Cancel - YES
`go func(){ sleep 50ms; th.Cancel("deadline") }()` while running `while True: pass`. It returned `Starlark computation cancelled: deadline` after 52ms. It is an `EvalError`, the same type as the step cap, so match the message to tell them apart. Cancel only interrupts bytecode execution. A builtin blocked in I/O must honour its own context.

## (d) Latency - YES (fast)
A 52-line script with defs, loops, comprehensions and 24 builtin calls, 1000 runs on a fresh Thread each time:
- Parse, compile and run: about 63 us per eval.
- Compile once with `SourceProgramOptions`, then `prog.Init` per run: about 9 us per eval.

Evaluation cost is negligible next to trace fetching. Compile caching is optional.

## (e) load() restriction and determinism - YES
- `Thread.Load` is the only load path. If it is nil, load fails with `load not implemented by this application`. A host hook can allow `//lib.star` and reject anything else: `cannot load /etc/passwd: load of "/etc/passwd" denied`.
- There is no ambient I/O or time. `time.now()` gives `undefined: time`, so only the predeclared names you pass are visible.
- Re-evaluation is deterministic: 200 runs gave identical results. Dict iteration is insertion order (`["z","a","m"]`). The dialect has no sets by default, so `set()` is a compile error (`does not support sets`) unless enabled.

## Caveats
- Each Exec gets fresh globals, so no state leaks between runs. Predeclared builtins must be re-created per evaluation if they close over a per-run failure slice.
- Failures from a builtin called inside helper functions point at the helper line. Pass an explicit stack depth if "outermost user frame" is preferred.
