## Option B: One package, upstream-default dialect, one golden file   (stance: minimal change)
Summary: Add one package, `internal/praytest`, as the only importer of `go.starlark.net`, pinned at `v0.0.0-20261021141710-4f4267fb140d`. Use the zero-value `syntax.FileOptions{}` (upstream defaults: no `while`, recursion, sets, top-level `if`/`for` or global reassignment). Freeze the surface with a single golden file.

How it works:
- `go.mod`: one `require go.starlark.net <pseudo-version>`, added by `go get`. The existing `dependency-review.yml` and `dependency-check.yml` cover it, so CI needs no changes.
- `internal/praytest/`: the whole Starlark host lives here: predeclared `StringDict`, builtins, `load` and conversion. Its exported API uses only praytel types (`domain.*` and its own `Case`, `Failure` and `Result`). The exact entry points belong to D5 and D6. The package has no adapter or sub-package.
- Dialect: `var fileOptions = &syntax.FileOptions{}` in one place. `def` helpers, lambdas, comprehensions and `for` inside functions all work.
- `surface_test.go` compares against `testdata/surface.golden`, with `-update` to regenerate it. The file holds the sorted predeclared names, each builtin's parameter list (taken from a host-side table that also drives registration) and `fmt.Sprintf("%+v", *fileOptions)`. Any change to names, signatures or dialect fails the test.
- Upgrade path: a package doc paragraph says to run `go get go.starlark.net@<new>`, then `go test ./internal/praytest/...` (surface golden, failure-output goldens, a step-limit test), and to note any step-count change in the PR.
- Tracing: spans on exported entry points only. Builtins and predicates run per span and stay untraced (`CONTRIBUTING.md:37-40`).

Steelman: This is the smallest change that meets all three goals. The narrowest dialect is the safest starting point: adding `while` or top-level `for` later breaks no existing file, but removing either would. That suits a format marked `v0` (R9). Helpers and loops inside `def` already cover §3. The narrow dialect also leaves fewer behaviours for an upstream bump to change.

Pros: about 3 files plus a `go.mod` line. No new abstractions. Dialect changes are additive. One golden file to review on each bump.

Cons / failure modes / hidden costs:
- S2-style "several cases from a table" cannot use a top-level `for`. Authors must use a comprehension or wrap the loop in a function, which is awkward and must be documented in `praytest.5`.
- "Only one importer" is enforced only by review. No test or lint stops a second package from importing `go.starlark.net`.
- With no internal seam, an upstream API break touches every builtin file in the package, though nothing outside it.
- The golden shows parameter names but not semantics such as defaults or keyword-only behaviour unless the table records them.
- Step-unit drift is caught only if a test asserts exact step counts.

Criteria scores:
- Containment of upstream breakage: 3 - one package, but no enforced boundary and no internal seam.
- Surface control/stability: 4 - names, signatures and dialect are golden-tested, and the narrow dialect keeps the surface small.
- Dialect expressiveness: 2 - helpers and in-function loops work, but there is no top-level `for`/`if`, `while` or recursion.
- Upgrade cost/reviewability: 3 - the steps are documented and the golden diff is small, but step drift is only partly covered.
- Testability: 4 - one surface golden, and failure-output goldens fit beside it.
- Fit with layout/tracing: 4 - a plain `internal/*` package with spans only at entry points.

Cost: S     Reversibility: easy (moving the package and bumping the pin are mechanical, and widening the dialect is additive. Narrowing it later would be one-way, but this option starts narrow.)

Unknowns / assumptions: Assumes D5/D6 accept that the host code is not split into sub-packages. Assumes a comprehension-based multi-case idiom is acceptable to authors. The R1 spike may show that recording failures needs host features that push for a wider dialect.
