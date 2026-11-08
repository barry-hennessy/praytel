# Hypothesis (6.170.15, 2026-11-06), with rapid (Go, v1.3.0) and QuickCheck lineage
Category: adjacent (property-based testing mechanism); rapid is also OSS reference (Go)

## Does it implement the feature(s)?  partial
- Property statements: yes (generative, over inputs, not over observed traces).
- Failure to reproducible example: yes (example DB, `@reproduce_failure`, `@seed`).
- Shrinking: yes. Stateful testing (rule/invariant machines): yes.
- Monitoring a live app for invariant violations: no. Hypothesis generates inputs and drives code under test; it does not observe an externally running process. Nearest analogue is `@invariant` checked after every step of a state machine.

## Mechanism
- Properties are ordinary test functions taking generated args; failure is an exception. [API ref, retrieved 2026-11-08]
- Example DB: default `DirectoryBasedExampleDatabase` at `.hypothesis/examples`, falls back to in-memory if unusable. Failures are saved and replayed in `Phase.reuse`. Docs call it "a cache that you never need to invalidate" and say not to rely on it for correctness; use `@example` for must-run inputs. Pluggable: `GitHubArtifactDatabase`, `ReadOnlyDatabase`, `MultiplexedDatabase`, Redis; custom needs `save/fetch/delete`. [API ref]
- `@reproduce_failure(version, blob)` runs one case, raises `DidNotReproduce` if it passes, errors across Hypothesis versions, meant to be temporary. `print_blob` default False, True on CI. `@seed` and `derandomize` (default True on CI) give determinism. [API ref]
- Shrinking: `Phase.shrink` minimises; `Phase.explain` (best-effort) reports which parts of the minimal case vary freely. `@example` inputs are not shrunk. `report_multiple_bugs` controls reporting of distinct failures. [API ref]
- Stateful: `RuleBasedStateMachine` with `@rule`, `@initialize`, `@precondition`, `@invariant` (runs after every rule), `Bundle`/`consumes`/`multiple` to pass produced values between rules; `stateful_step_count` default 50. [API ref]
- Shrinking design: operates on the underlying random choice stream rather than per-type shrinkers ("internal/integrated shrinking"), contrasted by MacIver with QuickCheck's type-based shrinking. [hypothesis.works via search summary; primary page not read, MODERATE]

## rapid (Go)
- Property: `rapid.Check(t, func(t *rapid.T){ x := rapid.Int().Draw(t,"x"); ... t.Fatalf })`. [pkg.go.dev, retrieved 2026-11-08]
- Automatic minimisation by tracking how bitstream bytes map to value parts; "persistence and automatic re-running of minimized failing test cases" listed as a feature. Mechanism/file location NOT verified. [pkg.go.dev; GitHub README]
- Stateful: `t.Repeat(map[string]func(*T))`; the `""` action runs before/after every action and holds invariant checks; `StateMachineActions(sm)` derives actions by reflection from methods, requires `Check(*T)`. [pkg.go.dev]
- Flags have `RAPID_*` env equivalents. No coverage guidance (unlike `testing.F`). [README]

## User experience
Failure prints minimal example; on rerun the DB replays it first, so a failure reproduces without user action. Blob/seed are the explicit sharing paths. Stateful failure prints the minimal rule sequence (verbose mode shows steps; 6.170.0 changelog).

## Strengths
- Zero-effort persistence/replay loop (DB) plus an explicit, portable pin (`@seed`, blob). [API ref]
- Shrinking is generator-agnostic. [hypothesis.works, MODERATE]
- Invariant-after-every-step model is a clean statement of "this must never hold/hold". [API ref]

## Limitations / known problems
- Blob is version-locked and temporary by design; DB is a cache, not a durable regression test. Durable form is a hand-written `@example`. [API ref]
- DB replay depends on test identity/key; behaviour on refactors UNVERIFIED.
- Tied to deterministic, re-executable code under test; shrinking requires re-running. A real app run (external, nondeterministic, side effects) cannot be shrunk this way. (Inference, not sourced.)
- Slow shrinking triggers warnings (6.151.10 changelog mentions `@seed` printed in it).

## Notable decisions & trade-offs
- Persist the choice sequence, not the value: compact and replayable but version-fragile; hence `@example` for permanence. [API ref]
- Auto-gitignore of `.hypothesis` (6.152.0, 2026-04-14) shows the DB is meant to stay local; sharing via CI artifacts DB. [changelog]

## What transfers to trace properties (INFERENCE, not from sources)
- Invariant-as-predicate checked after each event maps to checking a rule on each correlator event/span end.
- The "failure to minimal example" idea maps to emitting the offending span subtree as a golden/regression artifact; shrinking by re-execution does not apply, but trace slicing (drop unrelated spans) is an analogue.
- Separate "cache" (auto, local, ignorable) from "pinned" (committed code) artifacts: matches the brief's no-leftover-files rule.

## Evolution
Steady 6.x releases through 2026 (database fixes, Bundle filter/map 6.160.0, auto-gitignore 6.152.0, `fuzz_one_input` saves smallest per distinct failure 6.153.4). [changelog, 2026-11-08]

## Gaps
- QuickCheck primary source not fetched (type-based shrinking claim is second-hand).
- rapid failure file location/format unverified.
- Hypothesis internals paper/article not read directly.
- No evidence found on property-based tools applied to live traces/runtime monitoring (runtime verification literature not searched).

## Sources
- https://hypothesis.readthedocs.io/en/latest/reference/api.html - retrieved 2026-11-08 - official docs (first 100k chars only)
- https://hypothesis.readthedocs.io/en/latest/changelog.html - retrieved 2026-11-08 - changelog (first 100k chars)
- https://pkg.go.dev/pgregory.net/rapid - v1.3.0, 2026-03-30 (retrieved 2026-11-08) - official docs
- https://github.com/flyingmutant/rapid - retrieved 2026-11-08 - README
- https://hypothesis.works/articles/details - seen in search results 2026-11-08 - maintainer blog, not fetched
