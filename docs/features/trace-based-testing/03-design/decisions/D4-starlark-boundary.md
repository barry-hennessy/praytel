# D4: Where does go.starlark.net live, how is it pinned, which dialect is enabled, and how is the builtin surface frozen?
Status: decided     Depends on: none     Parent: D3
Vision refs: §6 Starlark row; §7 v1 item 2, item 7 (praytest.5); R3, R9; M7

## Context
- `go.mod` has no Starlark dependency (`go.mod:1-10`). Upstream has no tagged releases, reserves the right to break the API, and the step unit may change between versions (synthesis §4).
- The vision mitigation is fixed: pin a pseudo-version, isolate it in one package, golden-test the builtin surface (R3); keep the surface to the assertion table plus `env()`, mark the format `v0` until M7 holds, generated files carry a format comment (R9).
- The dialect (`syntax.FileOptions`: `while`, recursion, sets, top-level `if`/`for`, global reassignment) is user-visible: it decides whether §3's "loops and helpers" work and what `praytest.5` documents. Changing it later can break user files.
- Package layout: `design/ARCHITECTURE.md` (Shape) lists `cmd/praytel` for wiring and `internal/*` packages per concern. CONTRIBUTING requires spans on exported functions and none on hot paths (`CONTRIBUTING.md:16-45`); per-span predicate evaluation is a hot path.
- Dependency workflows exist: `.github/workflows/dependency-review.yml`, `.github/workflows/dependency-check.yml`.
- Upstream bumps must be reviewable: a step-unit change could flip an `exceeded step limit` verdict (S4).

## Goals
- Exactly one package imports `go.starlark.net`; everything else sees praytel types.
- A golden test fails on any change to the builtin names, signatures or dialect.
- An explicit, documented upgrade path for the pseudo-version.

## Non-goals
- Sandboxing (R2).
- Choosing the builtin semantics (D7) or failure mechanics (D6).

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| Containment of upstream API/behaviour breakage | 25 | R3 |
| Control and stability of the public builtin surface | 20 | R9, M7 |
| Dialect expressiveness for authors | 20 | §3 "loops and helpers"; S2 multiple cases |
| Upgrade cost and reviewability of a bump | 10 | R3; S4 step-limit verdicts |
| Testability (golden surface, golden failure output) | 15 | R3, M6 |
| Fit with package layout and tracing rules | 10 | ARCHITECTURE Shape; `CONTRIBUTING.md:16-45` |

## Options
Full text: `03-design/options/D4-A.md`, `D4-B.md`, `D4-C.md`.

### Option A: Vendored, trimmed fork behind an interpreter-agnostic seam (radical alternative)
Copy upstream at `4f4267fb140d` into `internal/starlark/upstream/` (no `go.mod` require), hardcode a fully permissive `FileOptions`, put an `internal/script` `Engine`/`Program` interface in front, golden file with names, signatures, `FileOptions`, upstream commit and a step-count fingerprint. Depguard/test enforces the single importer. Cost M.

### Option B: One package, upstream-default dialect, one golden file (minimal change)
`internal/praytest` is the only importer, pinned by `go get` at `v0.0.0-20261021141710-4f4267fb140d`. Zero-value `syntax.FileOptions{}`. `surface_test.go` + `testdata/surface.golden` (sorted predeclared names, param lists from a host-side table that also drives registration, `FileOptions` value). Upgrade path in package doc. Single importer enforced by review only. Cost S.

### Option C: Tilt-style host package, permissive Python-like dialect (most conventional)
Same package and pin as B, `expect` as a `starlarkstruct.Module`, `FileOptions{Set, While, TopLevelControl, GlobalReassign, Recursion}` all on, golden of names (+`Universe`), `FileOptions` and step counts for a fixed corpus; bumps "via the existing dependency workflows". Cost S, dialect one-way.

### Scoring (decider's adjusted scores; explorers' self-scores in brackets where changed)
| Criterion (weight) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| Containment (25) | 4 [5]: invisible to dependency-review/govulncheck/Dependabot, so security fixes depend on a human noticing | 3 | 4 | 4 |
| Surface control (20) | 5 | 4 | 3 | 5 |
| Dialect expressiveness (20) | 4 | 2 | 5 | 4 |
| Upgrade cost/review (10) | 2 [3]: manual sync plus rewriting `internal/compile` import paths each bump | 3 | 3 [4]: `dependency-check.yml` only opens a weekly reminder issue (`.github/workflows/dependency-check.yml:1-4`), it does not bump | 4 |
| Testability (15) | 4 [5]: the fake `Engine` serves D6/D7, not this decision | 4 | 4 | 5 |
| Fit with layout/tracing (10) | 2 [3]: 15k foreign lines need lint/coverage carve-outs; vendoring is new to this repo | 4 | 4 [5]: `make golden` already means tracing goldens (`Makefile:183-198`), name collides | 5 |
| Weighted /100 | 76 | 65 | 78 | 87 |

Tilt-related claims in C (flags, layout) are from memory and were not credited.

## Decision
Chosen: **hybrid**: B/C's package and pin, A's enforced boundary and step fingerprint, and a narrow dialect with only `TopLevelControl` turned on. Concretely:
1. **Pin**: `go get go.starlark.net@v0.0.0-20261021141710-4f4267fb140d` (packages `starlark`, `syntax`; `starlarkstruct` allowed). The pin goes in `go.mod` like any module, so dependency-review and govulncheck see it. This follows the vision's R3 wording, with no vendoring.
2. **Package**: `internal/praytest` is the only importer of any `go.starlark.net/...` package. It exports only praytel types (entry points belong to D5/D6). Enforcement: enable `depguard` in `.golangci.yml` with a rule that denies `go.starlark.net` for all files except `**/internal/praytest/**`. Single-importer is then a lint failure, not a review convention.
3. **Dialect**: one unexported `var fileOptions = &syntax.FileOptions{TopLevelControl: true}`. Everything else stays at upstream defaults: no `While`, `Recursion`, `Set` or `GlobalReassign`. `praytest.5` documents it as: "Starlark; top-level `if`/`for` allowed; no `while`, recursion, sets or global reassignment".
4. **Surface golden**: `internal/praytest/surface_test.go` compares against `internal/praytest/testdata/surface.golden` and regenerates with `go test ./internal/praytest -run TestSurface -update`. Do not reuse `make golden`, which means tracing goldens. The golden holds: (a) every predeclared name, sorted, including `starlark.Universe` names and `expect` module members, (b) each praytel builtin's parameter list with defaults and keyword-only markers, taken from the host-side table that also does registration (B), (c) `fmt.Sprintf("%+v", *fileOptions)`, (d) the `go.starlark.net` module version read from `debug.ReadBuildInfo` or `go list -m`, (e) `ExecutionSteps()` for a fixed corpus of 3-5 scripts in `testdata/steps/` (A/C), so a step-unit change shows as a diff.
5. **Upgrade path**: a package doc paragraph, linked from CONTRIBUTING, says: `go get go.starlark.net@<pseudo>`, `go test ./internal/praytest/... -run TestSurface -update`, review the golden diff, and paste any step-count or `Universe` change into the PR description. Add a `go.starlark.net` line to `.github/dependency-check.md` so the weekly reminder covers it.
6. **Tracing**: spans only on `internal/praytest`'s exported entry points. No spans on builtins or predicates, which run per span (`CONTRIBUTING.md:16-45`).

Why: this keeps the vision's R3 mitigation exactly (pseudo-version, one package, golden), and A's best idea makes it stronger: a mechanically enforced importer rule plus a step-count fingerprint, which answers S4's "a bump could flip a step-limit verdict". `TopLevelControl` covers §3 "loops" and S2's table-driven cases (`for card in CARDS: test(...)`) without a function wrapper. The spike confirmed top-level `for` works under this flag (`spike-starlark.md` (a)). Helpers (`def`, lambdas, comprehensions, in-function `for`) already work under the defaults. Leaving `While`, `Recursion`, `Set` and `GlobalReassign` off keeps the v0 surface small (R9), and turning any of them on later is additive: no existing file breaks. Turning one off after release would break files. `GlobalReassign` off also keeps the recorded files' module constants (`CHARGE = spans(...)`, S1) fixed for the whole file. The step cap and `Cancel` (spike (a), (b)) bound every remaining loop.

We give up:
- `while` loops and recursion. Authors use `for x in range(n)` with a bound, or a builtin. Recursive ancestor walks over `span.parent` (X1) need a bounded `for` or a D7 builtin.
- `set()`. Authors use dict keys.
- Reassigning top-level names (`n += 1` at top level is a resolve error, spike (a)). Accumulation must happen inside a function or a comprehension.
- A's byte-level control over upstream. A bump is a `go.mod` change that the golden test reviews, not a line-by-line diff of interpreter source.
- A's interpreter-agnostic `Engine` seam. If praytel ever left Starlark, it would rewrite `internal/praytest`, not swap an implementation. D6/D7 test against the real interpreter, which costs about 63us per eval (spike (d)).
- Tilt/Python familiarity for users who expect `while`. This is accepted as fine print in `praytest.5`.

Rejected:
- A: it contradicts the vision's "pin a pseudo-version" (R3) and would need the human to sign off. It hides a security-relevant dependency from dependency-review, govulncheck and Dependabot. Its two real advantages, enforced isolation and the step fingerprint, are taken into the hybrid without vendoring.
- B: the zero-value dialect blocks top-level `for`, so S2's multi-case files need a wrapper function or comprehension trick. It enforces the single importer by review only, and it has no step-count fingerprint for S4.
- C: it turns on `While`, `Recursion`, `Set` and `GlobalReassign` all at once. That is a one-way widening of a v0 public surface that §3/S2 do not need. It also relies on `dependency-check.yml` doing bumps, which it does not do.

Revisit if:
- Two or more user or dogfooding test files (M7 corpus or issues) use a wrapper function only to get `while`/recursion semantics, or an issue asks for one of them. Then enable that one flag; it is additive and needs a golden update plus a `praytest.5` note.
- A `go.starlark.net` bump changes any step count in `testdata/steps/` by more than 10%, or changes `Universe` names. Then decide on that PR whether to adjust the default step cap (D6) or hold the pin.
- Upstream publishes a tagged release (`go list -m -versions go.starlark.net` returns a non-pseudo version). Then switch the pin to the tag.
- A govulncheck/OSV advisory names `go.starlark.net` and the fix needs an API change that touches more than `internal/praytest`. Then reconsider the boundary, for example A's seam.
- `internal/praytest` grows past about 3k non-test lines or D5/D6 need a second package with direct interpreter access. Then split it into `internal/praytest/host` (the only importer) and praytel-typed callers, and update the depguard rule.

Consequences for other decisions:
- **D5**: the dialect is fixed. Top-level `for`/`if` are available, so cases may be generated by a top-level loop calling `test(...)`. Globals cannot be reassigned, and module state is frozen after load. Late-binding warning for D5/`praytest.5`: a `lambda`/`def` defined in a top-level loop that reads the loop variable sees its final value. Document the factory idiom (`def check_for(card): return lambda run: ...`). D5 owns `Thread.Load` (spike (e)) inside `internal/praytest`.
- **D6**: failure mechanics live inside `internal/praytest`. There is no engine seam, so tests use the real interpreter. Step-limit and `Cancel` are both `*starlark.EvalError` and must be told apart by message (spike (a), (b)). The surface golden's step corpus is the S4 regression guard. D6 sets the cap value.
- **D7**: every builtin name and parameter list D7 defines must be entered in the registration table so it appears in `surface.golden`. `Universe` names are part of the frozen v0 surface. Structural rules over `span.parent` cannot rely on `while` or recursion.
- **D8**: recorded files must stay within this dialect, for example no global reassignment. They carry the `v0` format comment (R9).
- **D11**: `.golangci.yml` gains `depguard`, and CI runs `TestSurface` as part of the normal `go test`.
- No conflict with decided work: D3 is `split` and has no decided content that this contradicts.
