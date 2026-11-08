## Option A: Two-phase load-then-check, Bazel/Tilt style   (stance: most conventional)
Summary: Each `.praytest.star` is executed once, top to bottom, during a load phase. `test(...)` only appends a frozen `Case` to a registry for that file. Every file is loaded before any child starts. Later, each case's `check` callable runs on a fresh thread against its `run`. `load()` works as it does in Bazel and Tilt: each module is executed once, frozen, cached per process, and cycles are an error.

How it works:
- `internal/praytest` (the only importer, per D4) exports `Load(path string, getenv func(string) (string, bool)) (*File, []Error)` and `(*File).Check(c Case, run Run) Result`. `Case{Name, Command []string, ExitCode int, Timeout time.Duration, Pos Position, check starlark.Callable}`.
- Load thread: the predeclared names are `test`, `spans`, `env`, `expect` and `Universe`. `test()` validates its arguments in place and reports errors at the call position from `CallStack()[len-2]` (spike (c)): missing `command`, empty or non-string `command`, bad `timeout`, `check` not callable, duplicate `name`. If the file registers zero cases, `Load` reports `file:1:1: no test(...)`. `expect.*` and `run.*` called at load time are errors ("only inside check").
- `test()` reads its registry through `Thread.Local`. Loaded modules get a new thread without that local (starlark-go.md:13), so a `test()` call in a helper is an error: "test() may only be called from a .praytest.star file".
- `load()`: the path is relative to the loading file, and `..` is allowed. The target must end in `.star` and must not end in `.praytest.star`. The cache is keyed by the cleaned absolute path. A cycle is detected with an in-progress marker and reported at the `load` statement's position (`Program.Load(i)`). A frozen module is shared across files safely because frozen values are immutable.
- `env()` works in both phases through the injected `getenv`.
- `cmd/praytel test` expands the inputs, calls `Load` on every file, collects every error, and exits `1` if there are any. Only then does it run the cases sequentially.
- `praytest.5` rule: "Top level runs once at load; `check` runs after its command exits."

Steelman: Every mature Starlark host uses this model: Bazel BUILD files, Tilt's Tiltfile and Buck. Authors and reviewers already know it. Top-level `for card in CARDS: test(...)` (D4) works with nothing extra. Running top-level code once is the only way to detect "no test()" and "no command" before any child starts, and this model does that as a side effect. Freezing gives isolation for free.

Pros: S3 errors are all found up front, with positions. Starlark's freeze gives isolation. The model fits in one sentence. Helper caching is cheap.

Cons / failure modes / hidden costs:
- Late binding: a `lambda` defined in a top-level loop sees the last loop value. This model invites exactly that pattern, so it must be documented.
- If `env()` is read at check time, the same file can behave differently across runs. Allowing it in both phases weakens determinism.
- Watch (S6) must extract `always`/`never` by calling `check` with a probe `run`, because the properties live inside `check` bodies, not in data.
- A shared helper cache across files means a helper's load error is reported once but blocks every file that loads it.
- Allowing `..` lets a test load files anywhere on disk.

Criteria scores:
- All usage errors before child: 5. Every exit-`1` condition comes out of `Load`.
- Determinism/isolation: 4. Freeze plus a fresh check thread; `env()` at check time is the leak.
- Authoring ergonomics: 5. Loops, helpers and `env()` work everywhere.
- Simplicity of model: 4. Two phases, a familiar rule, plus a late-binding footnote.
- Watch extractability: 2. Properties are code inside `check`, so they can only be found by probing.
- Positions: 5. Call-site and `load` positions are available.

Cost: M     Reversibility: hard. The phase semantics and `load` rules become the user format (R9).

Unknowns / assumptions: That Bazel, Buck and Tilt work this way is from memory and was only partly verified (starlark-go.md:20, :35). How large the per-process cache can get with 1,000 files has not been measured (M4).
