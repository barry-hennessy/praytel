# starlark-go, go.starlark.net/starlark (pseudo-version v0.0.0-20261021141710-4f4267fb140d, checked 2026-11-08)
Category: OSS reference / platform (the mandated embedding language)

## Does it implement the feature(s)?  partial
It is the language runtime the brief mandates, not a trace-testing tool. It provides: embedding, host-controlled builtins, step cap, cancellation, load hooks, positioned errors. It provides nothing for trace querying or assertions; all of that is host-defined. Memory limiting and wall-clock limits are not built in.

## Mechanism
- Determinism: core language has no randomness, clocks or unspecified iteration order, so a program gives the same result on every run. Side effects occur only via functions the host predeclares. [spec](https://github.com/google/starlark-go/blob/master/doc/spec.md) (retrieved 2026-11-08, STRONG, official)
- Sandboxing: the spec does not claim a sandbox. README warns that running untrusted Starlark in-process trusts your app's health to that code; recommends a separate process with OS limits against DoS. Memory cannot be bounded per thread, and one step may be slow or allocate heavily. [README](https://raw.githubusercontent.com/google/starlark-go/master/README.md) (2026-11-08, STRONG)
- Language limits: recursion and `while` are off unless enabled. `syntax.FileOptions` has Set, While, TopLevelControl, GlobalReassign, LoadBindsGlobally (deprecated) and Recursion. [options.go](https://raw.githubusercontent.com/google/starlark-go/master/syntax/options.go) (2026-11-08, STRONG). Repeat ops are capped at 1<<30 ("excessive repeat"). [eval.go](https://raw.githubusercontent.com/google/starlark-go/master/starlark/eval.go) (2026-11-08)
- Step cap and cancel: `Thread.SetMaxExecutionSteps(uint64)`; the step unit is "unspecified and may change", but the count is deterministic and reproducible (not CPU time). On exceeding it, `Thread.OnMaxSteps` runs; the default cancels with "too many steps". `Thread.Cancel(reason)` is goroutine-safe and makes execution fail promptly with an EvalError. A running builtin is not interrupted, so a long builtin must watch for cancellation itself. `Uncancel` resets the flag. `ExecutionSteps()` reads the count. Cancellation polls an atomic, not a context. [eval.go](https://raw.githubusercontent.com/google/starlark-go/master/starlark/eval.go), [pkg.go.dev](https://pkg.go.dev/go.starlark.net/starlark) (2026-11-08, STRONG)
- Exposing Go values: predeclared `StringDict` passed to `ExecFileOptions(opts, thread, filename, src, predeclared)`; predeclared is not modified by execution, though its values may be mutated. `starlarkstruct.Struct` (immutable, `FromStringDict(constructor, d)`, `Make` as the `struct` builtin) and `starlarkstruct.Module` (named members, not printing fields) are the standard patterns. Custom types implement the Starlark `Value`/`HasAttrs` interfaces (HasAttrs not confirmed on the fetched page, UNVERIFIED here). [starlarkstruct](https://pkg.go.dev/go.starlark.net/starlarkstruct) (2026-11-08, STRONG)
- Host state: `Thread.SetLocal/Local(key)` attaches host state to a thread. Thread-local state is not passed to modules loaded by `load`, since each module gets a new thread. [spec](https://github.com/google/starlark-go/blob/master/doc/spec.md)
- Errors: `*EvalError{Msg, CallStack, cause}`; `Backtrace()` gives a traceback. `CallFrame{Name, Pos syntax.Position}` carries file:line:col. `Program.Load(i)` returns the module name and position of each load. Non-EvalErrors from builtins are wrapped with an accurate frame. [eval.go](https://raw.githubusercontent.com/google/starlark-go/master/starlark/eval.go), [pkg.go.dev](https://pkg.go.dev/go.starlark.net/starlark) (2026-11-08, STRONG)
- load(): top-level only; the first argument is a string and the host decides how to resolve and run it via `Thread.Load`. Repeated loads of one module must return the same environment or error (the host must cache). Names starting `_` are not exported. A module's top-level values are frozen after successful execution, so shared modules are race-free across threads. [spec](https://github.com/google/starlark-go/blob/master/doc/spec.md), [eval.go](https://raw.githubusercontent.com/google/starlark-go/master/starlark/eval.go) (STRONG)
- Output: `Thread.Print` hook; nil sends output to stderr.
- Performance: tree-walking evaluator (bytecode compiler is used, but the authors say bytecode experiments were blocked by Go compiler limits); ints are `big.Int` ("relatively poor"); dicts use a custom insertion-ordered hash table. The interpreter starts very quickly, and independent threads run in parallel. A compiled `Program` can be serialized (`Write`/`CompiledProgram`; include `CompilerVersion` in cache keys). [impl.md](https://raw.githubusercontent.com/google/starlark-go/master/doc/impl.md), [README](https://raw.githubusercontent.com/google/starlark-go/master/README.md) (2026-11-08). No per-evaluation timing numbers were found: UNVERIFIED.

## User experience
Users write Python-like syntax. The host decides what is predeclared. Tilt as an example host: `load(path, *names)` runs each file once, `ext://` loads extensions, `load_dynamic` allows computed paths, `fail(msg)` raises a load error, and `local()` runs shell commands. Tilt exposes `os`, `config` and `struct` modules, so host side effects are a deliberate, large surface. [Tilt API](https://docs.tilt.dev/api.html) (2026-11-08, STRONG for Tilt's docs)

## Strengths
- Deterministic by design, with a deterministic step cap suited to reproducible test failure ("exceeded N steps").
- Host owns the entire API surface; the language has no I/O unless granted.
- Positioned errors and call stacks suit user-facing test diagnostics.
- Frozen modules allow sharing helper libraries across parallel test threads.
- Small, fast start-up. Wide adoption: Tilt, Drone CI, Kurtosis, Skycfg, Isopod and others (community list, ANECDOTE/MODERATE): [Starlark Resources](https://albertocavalcante.github.io/sky/starlark/resources) (2026-11-08). Copybara adopted Skylark as a config language per Alan Donovan's 2017 announcement: [golang-nuts](https://groups.google.com/g/golang-nuts/c/vHwG8HZMz1M) (STALE, via search summary).

## Limitations / known problems
- Not a security sandbox against hostile code: no memory cap, and a builtin that runs long is not interruptible. Wall-clock timeout needs a host goroutine calling `Cancel`. (README, STRONG)
- Step unit is unspecified and may change, so step budgets are not stable across upgrades (eval.go).
- Weak integer performance; tree-walking interpreter.
- API stability: the project reserves the right to make breaking changes. The module has no tagged release, only pseudo-versions (previous listed one Feb 2023, then Oct 2026). [README](https://raw.githubusercontent.com/google/starlark-go/master/README.md), [versions](https://pkg.go.dev/go.starlark.net/starlark?tab=versions)
- `ExecFile` is deprecated, so use `ExecFileOptions`.
- Bazel and Buck2 are not Go users of this library: Bazel uses a Java implementation, and Buck2 uses starlark-rust (the latter UNVERIFIED in this pass; not fetched).
- Python-looking but not Python: no classes, no exceptions (errors are fatal to the thread, so a test script cannot catch an assertion failure and continue), and no `while`/recursion by default.

## Notable decisions & trade-offs
Determinism and hermeticity are chosen over expressiveness. Resource control is limited to a step counter and cancellation; memory is left to the OS. Module loading is delegated entirely to the host.

## Evolution
`ExecFile` was superseded by `ExecFileOptions` and `FileOptions`, replacing global flags with per-file options (eval.go, deprecation note). `LoadBindsGlobally` is deprecated.

## Gaps
- Tilt-style per-evaluation latency numbers and benchmarks (not found).
- Whether the Starlark `fail`-style assertion failures can be collected (no try/except) was not tested.
- Buck2, Bazel and Drone usage was not verified from primary sources.
- Whether `starlarktest` helpers exist for a test-assertion pattern (not checked).

## Sources
- https://github.com/google/starlark-go/blob/master/doc/spec.md - retrieved 2026-11-08 - spec
- https://raw.githubusercontent.com/google/starlark-go/master/README.md - 2026-11-08 - docs
- https://raw.githubusercontent.com/google/starlark-go/master/doc/impl.md - 2026-11-08 - design doc
- https://raw.githubusercontent.com/google/starlark-go/master/starlark/eval.go - 2026-11-08 - source
- https://raw.githubusercontent.com/google/starlark-go/master/syntax/options.go - 2026-11-08 - source
- https://pkg.go.dev/go.starlark.net/starlark - 2026-11-08 - API docs
- https://pkg.go.dev/go.starlark.net/starlarkstruct - 2026-11-08 - API docs
- https://pkg.go.dev/go.starlark.net/starlark?tab=versions - 2026-11-08 - versions
- https://docs.tilt.dev/api.html - 2026-11-08 - official docs (Tilt)
- https://albertocavalcante.github.io/sky/starlark/resources - 2026-11-08 - community list
- https://groups.google.com/g/golang-nuts/c/vHwG8HZMz1M - 2017 - announcement (via search summary)
