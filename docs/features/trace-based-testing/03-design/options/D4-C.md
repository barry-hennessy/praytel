## Option C: Tilt-style host package, permissive Python-like dialect   (stance: most conventional)
Summary: Do what Go hosts of starlark-go usually do. One host package owns the interpreter: the predeclared `StringDict`, `starlarkstruct.Module` namespaces (`expect`) and `ExecFileOptions`. Enable the full "Python-like" dialect. Pin with a plain `go get` pseudo-version that the existing dependency workflows bump.

How it works:
- New `internal/praytest` (the counterpart of Tilt's `internal/tiltfile`/`starkit`) is the only importer of `go.starlark.net/{starlark,syntax,starlarkstruct}`. It exports praytel types only: `Load(ctx, path) (*File, error)`, `File.Cases []Case`, and `Case.Check(ctx, run) []Failure` (failures carry `file:line:col`, a message and offenders). D5/D6 own the inner shapes. Builtins (`test`, `spans`, `expect.*`, `env`, `struct`) live in `builtins.go`. `env` and `struct` are values in one `predeclared()` func, and `expect` is a `starlarkstruct.Module`. `starlark.Universe` stays as upstream has it.
- Dialect: `syntax.FileOptions{Set, While, TopLevelControl, GlobalReassign, Recursion: true}` in one exported-to-tests var. Unbounded `while`/recursion are acceptable because the step cap and `Cancel` (R2) bound them.
- Pin: `go.starlark.net v0.0.0-20261021141710-4f4267fb140d` in `go.mod`. Bumps arrive via the existing `dependency-check.yml`/`dependency-review.yml` like any module.
- Goldens (`testdata/surface.golden`): (1) names in `predeclared()` + `Universe` + module members, each with arity/param names from `Builtin` docs, (2) the `FileOptions` value, (3) step counts for a fixed corpus of programs, so a step-unit change shows as a diff (S4). `make golden` regenerates.
- Upgrade path: a CONTRIBUTING paragraph says to bump, run `make golden`, and put the step-count diff in the PR description.
- Tracing: spans on `Load` and `Check` only. No spans on builtins, which run per span (`CONTRIBUTING.md:37-40`).
- Rollout: a new package, so nothing to migrate. `praytest.5` documents the dialect as "Starlark with sets, while, recursion, top-level if/for", and the format is marked `v0`.

Steelman: This is the well-trodden route. Authors coming from Tilt, Skycfg or Bazel `.bzl` get the behaviour they expect. A top-level `for` that calls `test()` is the obvious table-driven way to write S2's cases, and §3 promises "loops and helpers" with no fine print. Starting permissive is the only dialect choice that never breaks user files: you can always keep a feature, but removing one breaks files. Keeping builtins and interpreter in one package satisfies "exactly one importer" without inventing an adapter layer.

Pros: least novel code; the documented dialect matches what people expect; contributors can follow upstream docs and Tilt as references; the step-count golden makes S4 regressions reviewable.

Cons / failure modes / hidden costs:
- The dialect is one-way. `GlobalReassign` and `Recursion` can never be withdrawn once v0 files use them. Also, `GlobalReassign` lets a top-level `CHARGE` be reassigned between cases, which hurts how readable recorded files are.
- Real Tilt does not keep starlark in one package: its plugins import it directly. "Conventional" here means following that practice loosely, and builtins will grow in this package until it is large.
- `Universe` is upstream's to change. A bump can add a builtin (e.g. a new string method) and expand praytel's public surface. The golden catches that, but praytel inherits it unless it filters.
- Signature goldens from `Builtin` docs are weak because Go builtins don't declare params. The golden needs a hand-kept table.

Criteria scores:
- Containment of upstream breakage: 4 - one importer and praytel types at the edge, but `Universe` passes through unfiltered.
- Control/stability of builtin surface: 3 - golden-tested, but the permissive dialect and the inherited `Universe` widen what must stay stable.
- Dialect expressiveness: 5 - everything §3/S2 implies works, including top-level loops over cases.
- Upgrade cost/reviewability: 4 - the existing workflows do the bump, and the step and surface goldens show the diff.
- Testability: 4 - surface, dialect and step-count goldens. Signature fidelity relies on a hand table.
- Fit with layout/tracing: 5 - `internal/*` per concern, spans only on `Load`/`Check`.

Cost: S     Reversibility: hard (package/pin easy; enabled dialect features one-way once users depend on them)

Unknowns / assumptions:
- Tilt's exact `FileOptions`/resolve flags and its package layout are UNVERIFIED (from memory, not research).
- Whether `GlobalReassign` affects freezing of loaded helper modules (D5) is not checked.
- Whether dependency-check.yml opens bump PRs or only reports is not checked.
