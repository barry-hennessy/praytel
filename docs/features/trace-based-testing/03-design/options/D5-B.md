## Option B: Phased plan with a phase-gated builtin table, a shared module cache, and a property-collect mode   (stance: most extensible)
Summary: Running a file is split into named phases, `Load`, `Check` and `Collect`. Each builtin in D4's registration table declares the phases it may run in. A load pass over every file builds an immutable `Suite` plan before any child starts. Watch extraction and later features become new phases or new plan readers, not new evaluators.

How it works:
- `internal/praytest`: `LoadAll(ctx, paths, Options{Env map[string]string, Root string}) (*Suite, []UsageError)`. The result is `Suite{Files []*File}`, where `File{Path, Cases []*Case, Loads []string, EnvReads []EnvRead{Name, Default, Value, Pos}}` and `Case{Name, Command, ExitCode, Timeout, Pos, Origin []Pos, check starlark.Callable}`. `(*Case).Check(ctx, Run) Result` is consumed by D6. `(*Case).Properties(ctx) ([]Property, error)` is consumed by D12.
- Registration table: each builtin gets a `phases` column, recorded in `surface.golden`. `test` runs in Load only. `expect.*` and `run.*` run in Check and Collect. `env` runs in all phases. A builtin called in the wrong phase is a usage error with the caller's file:line:col. For example, `expect.never` at top level fails at load time with exit `1`.
- `test()` writes to the registry of the *calling test file's* thread. A `test()` call at a helper's module top level is an error. A `test()` call from a helper *function* that a test file invokes during Load is allowed, which gives shared case libraries and parametrisation (`for c in CARDS: charge_case(c)`). `Origin` records the call chain. Duplicate names within a file are an error.
- `load()`: the path is relative to the loading file, must end in `.star` and not `.praytest.star`, and must resolve under `Root` (the cwd). `../` is therefore allowed, so `testdata/repros/x.praytest.star` can load `../props.star`. Modules are cached by canonical path, frozen, and shared across files. Cycles are detected with an in-progress marker and reported with the full chain.
- `env()`: the process environment is snapshotted once, and every read is logged in `EnvReads`.
- `Collect`: runs `check` with a collector `run`. `always`/`never` record `Property{Selector, Pred, Pos}` and the other `expect.*` do nothing. Watch files and repros keep properties in a shared `*.star` helper.
- Rollout: Load and Check ship in v1. Collect ships in v1.1 with no format change.

Steelman: the next three requirements arrive as additions: v1.1 watch is a phase, `--case` is a filter over `Suite`, and shared libraries already work. D8's repro generator can emit `load("../props.star", "bank_props")` and reuse the props instead of copying them. `EnvReads` lets reports and repros say which environment a run depended on. The phase table makes "what runs when" a column in the golden file, which is something you can check rather than prose.

Pros: all structural errors are found before children start. Shared frozen modules are fast (M4) and deterministic. Helper-built cases are allowed.

Cons / failure modes / hidden costs:
- Collect cannot see properties behind data-dependent code (`if run.one(...)`). Watch has to reject such files or warn that it skipped properties.
- More rules for `praytest.5` to document: phases, the root boundary, and which `test()`-from-helper calls are legal.
- Freezing shared modules means that mutating helper data fails at runtime, which surprises authors.
- Every v0 rule here is public surface (R9). Narrowing it later breaks user files.

Criteria scores:
- All usage errors before any child: 4 - Load-phase and phase-misuse errors are caught up front. Misuse of arguments inside `check` still surfaces at check time.
- Determinism/isolation: 4 - per-file registries, frozen shared modules and an env snapshot. Sharing a cache is a small coupling between files.
- Authoring ergonomics: 5 - loops, case factories, shared libraries and `env()` all work in both phases.
- Simplicity of documented model: 2 - phases, the root rule and the helper-`test()` rule add up to a page, not a sentence.
- Watch extractability: 4 - Collect mode is designed in, but has the data-dependence hole.
- Positions: 4 - call-chain `Origin`, and spike (c) positions for every phase error.

Cost: M (the upper end).     Reversibility: hard (internal seams are easy to change, but the load-root, helper-`test()` and phase rules are v0 public surface).

Unknowns / assumptions:
- That the cwd is an acceptable load root, and that CI runs from the repo root.
- That D12 accepts Collect over a separate watch evaluator.
- That D6 can share `run` between real and collector implementations.
