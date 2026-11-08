# D5: How is a test file evaluated: what runs at load time, how are cases registered, and how do load() and env() resolve?
Status: decided     Depends on: D4     Parent: D3
Vision refs: §4 Assertion model (`test(...)`, `env()`, `load()`), S2, S3 (parse-all-first, usage errors), S6; §7 v1 item 2; R2, R9; A6; M4

## Context
- S3: all arguments are expanded and every file is parsed before any child starts; syntax error, no `test(...)`, a case without `command` are exit `1` with no child started. Detecting "no `test(...)`" and "case without `command`" requires executing top-level code, not only parsing.
- `test(name, command, exit_code=0, timeout="10m", check)` declares a case; the case's `check(run)` runs only after its child and drain (D2). The S1 generated file defines module-level selector constants (`CHARGE = spans(...)`) and a `def check_*` per case.
- `load()` of relative `*.star` helpers; helpers must not be named `*.praytest.star` (§4). Unspecified: whether `load()` may reach outside the test file's directory tree, whether a `*.praytest.star` may be loaded, how a helper shared by many files is cached, and what happens with cycles.
- `env(name, default)` is the only I/O builtin (R2, A6). If it is evaluated at load time, `command = [env("SHOP_BIN", "shop"), ...]` is fixed before any child runs; whether `check` may also call it is unspecified.
- Duplicate case names within one file, and `test()` called from a loaded helper, are not addressed by the vision.
- v1.1 `--watch` reads always/never from a `.praytest.star` (S6), so the evaluation model must let one file's properties be extracted without running its cases.
- M4: input expansion ≤ 50ms for 1,000 files (expansion only, but parse cost adds to every run).

## Goals
- Every exit-`1` condition is found before any child starts, with file:line:col where one exists.
- Files and cases do not leak state into each other.
- A documented, simple rule for "what runs when" in `praytest.5`.

## Non-goals
- Remote or non-`.star` loads.
- Case filtering (`--case` is Later, §7).
- Command override at test time (§7 non-goals).

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| All usage errors detected before any child | 25 | S3 "All-or-nothing usage errors" |
| Determinism and isolation between files and cases | 20 | M2, R7 |
| Authoring ergonomics: helpers, loops generating cases, `env()` | 20 | §3, S2, A6 |
| Simplicity of the documented model | 15 | R9; §7 v1 item 7 (praytest.5) |
| Properties extractable for v1.1 watch | 10 | S6, §7 v1.1 |
| Positions on load/usage errors | 10 | M6 |

## Options
Full text: `03-design/options/D5-A.md`, `D5-B.md`, `D5-C.md`. All three share the same core: execute each file's top level once before any child, `test()` appends a validated case to a host-side registry, `check` callables are kept and called after the child and drain. They differ on the edges: error collection, helper caching, `test()` from helpers, the `load()` boundary, `env()` phase, and whether a watch-extraction phase is designed in now.

### Option A: Two-phase load-then-check, Bazel/Tilt style (most conventional)
`Load(path, getenv) (*File, []Error)` and `(*File).Check(case, run)`. `test()` reads its registry from `Thread.Local`. Helper modules run on a new thread with no registry, so `test()` at helper top level is an error, while helper *functions* called from the test file can register. `load()` is relative, `..` allowed, `.star` but not `.praytest.star`. Frozen modules are cached per process by absolute path, cycles reported at the `load` position. `env()` is allowed in both phases. Zero cases reports `file:1:1: no test(...)`. `expect.*`/`run.*` at load time are errors. Cost M.

### Option B: Phase-gated builtin table, shared module cache, Collect mode (most extensible)
`LoadAll(ctx, paths, Options{Env, Root}) (*Suite, []UsageError)`. Each builtin declares its allowed phases (Load/Check/Collect) in the registration table and `surface.golden`. `test()` is legal from helper functions but not at helper top level, and `Origin` records the call chain. `load()` must resolve under `Root` (= cwd). The environment is snapshotted and every read logged in `EnvReads`. A v1.1 Collect phase runs `check` with a collector `run` to extract `always`/`never` properties. Cost M+, and the phase, root and helper-`test()` rules become v0 public surface.

### Option C: Plain Starlark module exec (minimal change)
`LoadFile(path) (*File, error)`. `test()` uses `UnpackArgs` and returns an error on misuse, so only the first usage error per file is reported. `test()` in a loaded helper registers into the loading file. `load()` is relative, absolute paths rejected, `..` unrestricted, and there is a per-file cache (no sharing across files) with cycle detection. `env()` is a live `os.LookupEnv` in both phases. A fresh thread per case with a step cap. About 200 lines. Cost S.

### Scoring (decider's adjusted scores; explorers' self-scores in brackets where changed)
| Criterion (weight) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| Usage errors before any child (25) | 4 [5]: says `Load` returns `[]Error`, but a builtin that returns an error aborts the exec (spike A3), and A does not say `test()` records and continues, so in practice it gets one error per file like C | 4 | 4 | 5 |
| Determinism/isolation (20) | 4 | 3 [4]: whether a `load` is valid depends on the invocation cwd, so the same file passes from the repo root and fails from a subdirectory; `env()` is still allowed in checks | 4 | 5 |
| Ergonomics (20) | 5 | 5 | 4 | 4 |
| Simplicity of documented model (15) | 4 | 2 | 5 | 4 |
| Watch extractability (10) | 2 | 3 [4]: Collect has the data-dependence hole, and D12 (still open) has not accepted it | 2 | 2 |
| Positions (10) | 5 | 4 | 4 | 5 |
| Weighted /100 | 82 | 72 | 79 | 87 |

## Decision
Chosen: **hybrid: C's minimal core with A's thread-local registry and shared frozen cache, plus three narrowings (record-and-continue `test()`, `env()` at load time only, no `test()` at helper top level). B's Collect phase is not built.** Every edge is set to the narrowest choice that still covers §3/S2, because widening a v0 rule later is additive and narrowing one breaks files (R9).

Concretely, all in `internal/praytest` (the only importer, D4):
1. **Entry points.** `Load(ctx, path string, getenv func(string) (string, bool)) (*File, []UsageError)`. `File{Path string; Cases []*Case}`. `Case{Name string; Command []string; ExitCode int; Timeout time.Duration; Pos Position; check starlark.Callable}` (unexported Starlark field). `UsageError{Pos Position; Msg string}`. D6 owns the method that runs `check`. The runner (D10) calls `Load` on every expanded path, concatenates all `UsageError`s across all files, prints them, and exits `1` before any child starts if there are any.
2. **Load phase.** Each file is compiled with D4's `fileOptions` and executed once on its own `Thread`, with `Local("praytest.phase")="load"`, a per-file registry in `Local("praytest.registry")`, and D6's step cap. Syntax and resolve errors are returned as positioned `UsageError`s, every entry of the `resolve.ErrorList`. After exec the module globals are frozen.
3. **`test()` records and continues.** It uses the spike A3 mechanism: on misuse it appends a `UsageError` at `CallStack()[len-2].Pos` (spike (c)) and returns `None` without registering the case, so one file reports *all* of its bad `test()` calls. Checked conditions: missing or empty `command`, a non-string element, a missing or non-callable `check`, a `check` that is a Starlark function not accepting exactly one positional argument, a bad `timeout`/`exit_code` type or value, and a duplicate `name` (reported at the second call). A top-level runtime error (for example an undefined name) still aborts that file's exec and is reported with its position. Other files are still loaded and reported. After exec, zero registered cases *and* no other error gives `path:1:1: no test(...)`.
4. **Phase gating, two sentences, no phase table.** `test()` and `env()` work only while the load phase runs; `expect.*` and `run.*` work only inside a `check`. A wrong-phase call is an error at the caller's position: exit `1` at load, a runtime error (exit `4`, D6) inside a check. The allowed phase is written in a comment next to each builtin's row in the D4 registration table. It is not a golden column, which keeps D4's golden format unchanged.
5. **`env(name, default)`: load time only.** It reads through the injected `getenv` (the real one is `os.LookupEnv`), so tests are hermetic. To use an environment value in a check, bind it to a top-level constant (`LIMIT = int(env("BANK_MS", "200"))`) and read the constant, which is frozen. A check's verdict therefore depends only on the trace and the frozen module, not on the CI runner's environment at check time.
6. **`test()` from helpers.** A loaded helper module runs on a fresh thread with phase `load` and *no* registry. A `test()` at a helper's top level is the error "test() may only be called while a .praytest.star file loads". A helper *function* called from a test file's top level runs on that file's thread and registers into that file (the case-factory idiom, `for c in CARDS: charge_case(c)`). The case `Pos` is the `test()` call position (spike (c)). There is no `Origin` chain in v0.
7. **`load()`.** The path is relative to the loading file's directory. Absolute paths and module names that do not end in `.star` are rejected, as is any name ending in `.praytest.star`. `..` is allowed (R2: not a sandbox; S6 repros need `../props.star`). Modules are cached per process by cleaned absolute path, executed once, frozen and shared; frozen values are immutable, so sharing cannot leak state between files. A cycle is detected with an in-progress marker and reported at the `load` statement position (`Program.Load(i)`) with the chain `a.star -> b.star -> a.star`. A helper that fails to load is reported once per loading file, at that file's `load` statement, with the helper's own position appended.
8. **`praytest.5` rule (verbatim intent).** "Each file runs top to bottom once, before any command starts; `test()` and `env()` work only then. Each `check` runs after its command exits and its trace drains; `expect.*` and `run.*` work only there. A helper `*.star` may define functions that call `test()`, but must not call it at its top level." Plus D4's late-binding note and the factory idiom.

Why: all three options agree on the core, and it is the only shape that can find "no `test()`" and "no `command`" before any child (25-weight criterion). The hybrid takes the 5 on usage errors by making `test()` record and continue, a mechanism the spike already verified (A3), so a CI user fixing a 20-case file sees every bad case in one run instead of one per run. It takes the 5 on determinism by forbidding `env()` at check time. With `trace-tests` a required check on `main` from day one (S5) and M7 requiring zero flaky failures, an assertion that silently reads the runner's environment is a real flake source. Binding a top-level constant costs authors one line and loses nothing that A6 asked for (`command = [env(...)]` is load time). Simplicity stays at 4: the rule is two sentences, not B's page. Every narrowing (env load-only, no helper-top-level `test()`, no Collect) can be relaxed later without breaking an existing file.

We give up:
- Reading the environment inside a check or a predicate directly. It must go through a top-level constant.
- `test()` at a helper's top level ("include this file and get its cases"). Shared case libraries must expose a function.
- B's designed-in watch Collect phase and `EnvReads` log. Watch extraction is left entirely to D12 and v1.1.
- B's load root. A test file can `load("../../anything.star")` anywhere on disk (R2 accepts this).
- Reporting more than the first error when a file's top level hits a *runtime* error. Only `test()` misuse and syntax/resolve errors are reported in full.
- Per-file fresh helper modules (C). A helper is shared and frozen across files, so a helper that relied on mutating its own globals at load fails the same way for every loader.
- An `Origin` call chain for cases registered by helper functions. The position is the `test()` call inside the helper.

Rejected:
- A: the right core, but it allows `env()` at check time (a determinism leak against S5/M7) and does not specify that `test()` records and continues, so it in practice reports one error per file. Its cache and registry mechanics are kept.
- B: its phase table, cwd load root and helper-`test()`/`Origin` rules make a page of v0 public surface (R9) that is hard to narrow later. The cwd root makes a file's validity depend on the invocation directory, and the Collect phase it pays for has a known hole and an unaccepted consumer (D12 is open).
- C: closest to the chosen core, but it reports one usage error per file, allows `env()` in checks, and lets `test()` at a helper's top level register into every loader, which is a surprising, wide rule that would be hard to withdraw.

Revisit if:
- Two or more dogfooding or user test files (M7 corpus or issues) bind a top-level constant *only* to pass an environment value into a check, and an issue asks for `env()` in checks. Then allow `env()` in the check phase as an additive change, with the value snapshotted once per process.
- D12 decides watch needs properties as data (not by probing `check`), and that cannot be done by running the existing `check` against a collector `run`. Then add a Collect phase. It is additive: test-mode files are unaffected.
- A `load()` outside the repository causes a reported incident or a CI security review objects. Then restrict `..` to the enclosing VCS root (not cwd), as a v0 narrowing done before M7 removes the `v0` label.
- Load time (exec plus helper loads, excluding expansion) exceeds 50ms for the 1,000-file M4 fixture. Then switch to compile-once/`Init` caching (spike (d): about 9us vs 63us).
- Users ask for every top-level runtime error per file (two or more issues). Then consider per-statement recovery. This is internal only.

Consequences for other decisions:
- **D6**: check threads set `Local("praytest.phase")="check"`. `env()` and `test()` inside a check are runtime errors (exit `4`) with a position. The step cap D6 chooses also bounds load-phase exec. A cap hit at load is a `UsageError` (exit `1`), not exit `4`, because no child has started. D6 owns the `Case` check method and the `run` object.
- **D7**: each builtin row in the registration table carries a one-word phase comment (`load`/`check`). `spans(...)` is phase-free, because selectors are built at top level as constants (S1).
- **D8**: generated files must call `test()` only at their top level, put any environment reads at top level, and use no `load()`. The S1 shape already satisfies this. Repros (S6) may `load("../props.star")`, which is allowed.
- **D10**: the runner calls `Load` for every expanded file first, aggregates every `UsageError`, prints them as `file:line:col: msg` on stderr and exits `1` if there are any, before starting a child. `getenv` is injected from `cmd/praytel`, which allows in-process golden tests.
- **D12**: v1 gives no property-extraction phase. Its seams are the retained `check` callables, frozen modules and the `env()`-free check phase. Whether watch probes `check` with a collector `run` or needs more is D12's question. Any such phase can be added without breaking a v1 file.
- **D4 (decided)**: consistent. It uses `fileOptions`, `internal/praytest` stays the only importer, `Thread.Load` is owned here as D4 assigned it, and the golden format is unchanged.
- **D1 (decided)**: no interaction. Checks see D1's model through D6's `run`.
