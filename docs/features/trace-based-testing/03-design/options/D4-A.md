## Option A: Vendored, trimmed fork behind an interpreter-agnostic seam   (stance: radical alternative)
Summary: Don't depend on `go.starlark.net` through `go.mod` at all. Copy the `syntax`, `starlark`, `starlarkstruct` and `internal/compile` packages at the pinned commit into `internal/starlark/upstream/` and freeze the dialect in code. Put a praytel-owned `internal/script` interface in front of it, so the rest of praytel never sees a Starlark type. The questioned premise is "pin a pseudo-version": in this option we own the bytes instead.

How it works:
- `internal/starlark/upstream/`: an unmodified copy of upstream at `4f4267fb140d`, with an `UPSTREAM` file recording the commit and the licence (BSD-3). A `go:generate` sync script re-copies from a commit hash and shows the diff. Only `internal/starlark` may import it, and a depguard or test check enforces that.
- `internal/starlark`: hardcodes `syntax.FileOptions{While, Recursion, TopLevelControl, GlobalReassign, Set: true}` as a package constant. It has no user knobs. It also builds the predeclared `StringDict` (assertion table plus `env()`).
- `internal/script`: `type Engine interface { Compile(name string, src []byte) (Program, error) }`, `Program.Run(ctx, Inputs) (Result, error)`. `Result`/`Failure{File, Line, Col, Msg, Stack []Frame}` are plain praytel structs. Spans go on `Compile`/`Run` only, not on per-span predicate calls (`CONTRIBUTING.md:16-45`).
- Golden: `internal/starlark/testdata/surface.golden` lists the builtin names, signatures, the `FileOptions` value, the upstream commit and a step-count fingerprint from a fixed corpus script.
- Rollout: first commit adds the copy and seam. Later bumps rerun the sync script, and the PR diff shows upstream changes line by line.

Steelman: R3 exists because upstream breaks without warning and the step unit drifts. With a vendored copy, nothing changes unless someone changes it in a reviewed diff. The step-count fingerprint makes an S4 verdict flip show up in review instead of in users' CI. The `script` seam makes D6/D7 testable with a fake engine. A future move to a helper process (R2 if sandboxing returns) would be a new `Engine` impl, not a rewrite.

Pros: full control over the pace of change. Upgrade diffs can be reviewed exactly. `go.mod` stays unchanged. Dialect and surface are frozen in one constant plus one golden file. Callers are decoupled from the interpreter.
Cons / failure modes / hidden costs: about 15k lines of foreign code in the repo, which lint and coverage must exclude. Dependency-review/dependency-check workflows (and govulncheck) no longer see it, so security fixes are manual. It invites local patches that turn into a real fork. It goes against the letter of the vision's "pin a pseudo-version" mitigation (R3), so the human must accept that. The interface hides Starlark error richness unless `Failure` is designed well.

Criteria scores:
- Containment of upstream breakage: 5 - nothing upstream reaches praytel without an explicit sync diff.
- Control/stability of builtin surface: 5 - frozen constant plus a golden file that includes the commit and step fingerprint.
- Dialect expressiveness: 4 - loops, helpers and recursion enabled. Fixed forever without a format bump.
- Upgrade cost/reviewability: 3 - the diff is very reviewable, but each bump is manual and loses tooling alerts.
- Testability: 5 - fake `Engine` for D6/D7, golden surface and step fingerprint.
- Fit with layout/tracing: 3 - `internal/*` fits, but a vendored tree is unusual in this repo and needs lint carve-outs.

Cost: M     Reversibility: easy (swap the copy for a `go.mod` require; the seam stays)
Unknowns / assumptions: whether the human accepts vendoring instead of a pseudo-version pin. Whether `internal/compile` copies cleanly (upstream's internal-import paths need rewriting). Licence attribution requirements in the distributed binary.
