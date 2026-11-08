## Option C: Plain Starlark module exec: top level runs once at load, checks run later   (stance: minimal change)
Summary: Each file is executed once with `starlark.ExecFileOptions` (D4's `fileOptions`) before any child starts. `test(...)` is a predeclared builtin that validates its arguments and appends a `Case` to a host-side slice. The `check` callables are kept from the frozen globals and called after the child exits and the drain finishes. `load()` and `env()` are the obvious host hooks, with nothing added on top.

How it works:
- `internal/praytest/load.go`: `func LoadFile(path string) (*File, error)`; `File{Path string; Cases []Case}`; `Case{Name string; Command []string; ExitCode int; Timeout time.Duration; Check starlark.Callable; Pos syntax.Position}` (unexported fields where Starlark types leak). The runner (D10) calls `LoadFile` on every expanded path, collects every error and starts no child unless all files load.
- `test()`: `UnpackArgs` with `command` required. Non-string elements, an empty command, a non-callable `check`, a bad `timeout` or a duplicate name in the file all return an error positioned at `CallStack()[len-2].Pos`. After exec, zero cases gives `path: no test(...)`. `test` checks `thread.Local("phase")=="load"`, so calling it inside a check is an error. A `test()` call in a loaded helper registers into the loading file.
- `Thread.Load`: resolves relative to the loading file's directory, accepts `*.star` only, rejects `*.praytest.star` and absolute paths, and does not restrict `..`. Its cache is a per-file map with an in-progress sentinel, so a cycle reports `load cycle: a.star -> b.star -> a.star`. There is no cross-file cache: each file gets fresh helpers, which costs about 63us per helper.
- `env(name, default)`: a live `os.LookupEnv`, the same builtin at load and check time.
- Check time: a fresh `Thread` per case with a step cap. `expect.*` builtins record into a per-case failure sink held in `thread.Local`, so builtins are built once per file. Module values are already frozen after exec.
- `praytest.5` rule: "The file runs top to bottom once, before any command starts. `check` functions run after their command exits."

Steelman: This is how Starlark itself works (exec the module, then freeze it), so there is nothing new to invent or document beyond one sentence. Every S3 condition is a concrete error raised during load, with a position, across all files, before any child starts. Freezing gives isolation between cases for free, and each file has its own `Thread`, so files cannot see each other. Top-level `for` loops that generate cases work under D4's dialect.

Pros: about 200 lines; the model takes one sentence to document; uses only upstream mechanisms (`Load`, `Local`, freeze); positions come from the spike's verified frame rule.

Cons / failure modes / hidden costs:
- Only the first usage error per file is reported, because a returned error aborts the exec. Other files are still reported.
- Top-level code runs under the step cap with no way to override it; slow helper code at the top level slows down every run.
- `env()` at check time makes a verdict depend on the environment. The rule does not forbid this, so determinism relies on convention.
- Watch (S6) still has to exec the file and then pick properties out of a check by running it against streaming spans. Nothing here makes that extraction possible, so the burden moves to D12.
- Lambdas defined in a loop that capture the loop variable late are only covered by a warning in the docs.
- Not restricting `..` lets a test file load any `.star` file in the repo.

Criteria scores:
- All usage errors before any child: 4 - every S3 case is caught at load time; only the first error per file is reported.
- Determinism/isolation: 4 - frozen modules and one thread per file/case; `env()` in checks is the leak.
- Authoring ergonomics: 4 - loops, helpers and `env()` work anywhere; the late-binding trap remains.
- Simplicity of model: 5 - one sentence, and it matches Starlark's own semantics.
- Watch extractability: 2 - properties exist only as code inside checks.
- Positions: 4 - call-site positions everywhere except "no test(...)", which has a file only.

Cost: S     Reversibility: easy (tightening `env()`/`load()` later is a narrowing, done before the v0 label is removed)

Unknowns / assumptions:
- That an `UnpackArgs` error for a missing `command` surfaces with a call position (the spike verified frames, not this exact path).
- That D12 accepts executing checks against streaming spans instead of extracting properties statically.
