# Tasks: trace-based testing (`praytel test`)
Vision: 02-vision.md (v5)   Decisions: D1, D2, D3 (split into D4/D5/D6), D4, D5, D6, D7, D8, D9, D10, D11, D12   Spike: 03-design/spike-starlark.md   Revision: 3 (critic rounds 1 and 2 addressed; see "Revision log")

## Summary
- **32 tasks**: 14 S, 18 M, 0 L. 31 can be finished by an implementer in one worktree. T30 also has a human release gate (M2 evidence and branch protection) that no implementer can complete.
- **Critical path: 14 tasks**: T1 → T7 → T11 → T15 → T18a → T18b → T19 → T21a → T21b → T24 → T25 → T28 → T29 → T27.
- **Max parallel width: 9** (antichain T2, T4, T5, T6, T8, T9, T10, T11, T12; Owns verified disjoint, including trace goldens). Wave 1 has 6 tasks with no dependencies.
- **v1.1 watch is out of scope.** No task builds any D12 S5 item (see "D12 S5 do-not-build check").

## Rules that apply to every task
- **Standard gate [G]**, part of every DoD below: `go build ./...` and `go vet ./...` exit 0; `go test -race -count=1` passes for every package the task owns; `make lint` exits 0 (Dagger). Tasks that only touch YAML or Markdown use `make zizmor` / the checks they list instead.
- **Tracing (CONTRIBUTING.md "Tracing").** Spans are named `package.Type.Func`. They start in every exported function or method, and in every unexported function that does real work (I/O, decoding, forwarding). Hot paths have none: anything that runs per span, per event, per frame, per Starlark builtin call, or inside the TUI model's `Update`/`View`. `internal/praytest`, `internal/testrun` and `internal/record` take an `lt observability.LoggingTracer`. Where a decided signature has no `lt`, add `lt` right after `ctx`, and add `ctx` where the signature has none; nothing else changes.
  - **Exempt, with no tracer:** `internal/selector` (pure, per span); `praytest.Volatile` and `praytest.Summarize` (called from the TUI model); every Starlark builtin; `Failure.Lines`; the `tui.Recorder` methods `Header`/`Badge`/`Rows`/`Panel`/`Key`/`Write` (called from the TUI model; the I/O happens in `Write`'s returned `work` func, which is traced); `testrun`'s unexported classifier and report writer (pure, per case/per line); and `tui.Settle`/`(*Model).CheckView`, following the precedent of `tui.Snapshot`, which takes no tracer.
  - `(*praytest.Case).Check` keeps the D6/D12 seam signature and uses the `lt` captured by `Load`.
- **Trace goldens and who owns them.** `make golden PKGS=./internal/<pkg>` runs `scripts/golden`, which stamps `testdata/<TestName>.golden` for **every** test in the package that emits spans. Tests that emit none get no golden.
  - Each task owns only the goldens of the tests it adds, identified by a **test-name prefix** listed in its Owns. Prefixes are chosen so no two tasks' prefixes match the same test name.
  - Each task's tracing DoD line follows one procedure:
    1. Run `make golden PKGS=./internal/<pkg>`.
    2. `git add` only `internal/<pkg>/testdata/<own prefixes>*.golden`.
    3. `git checkout -- internal/<pkg>/testdata && git clean -f 'internal/<pkg>/testdata/Test*.golden'`, which drops every other re-stamped or new golden (the task's own are already staged).
    4. `grep` its own committed goldens for the named spans.
  - A new instrumented package (`internal/praytest`, `internal/testrun`, `internal/record`) gets `main_test.go` with `func TestMain(m *testing.M) { testtrace.Main(m) }`. The CI `golden` job never fails, so goldens are not a merge gate; the per-task DoD line is.
- **D12 seam tests** carry the comment `// D12 seam: changing this needs a D12 revisit, not a test edit.` Adding a fixture file that a seam test globs is not a test edit.
- **Default-path guard (D2 item 9).** Whenever a task touches `cmd/praytel`, `git diff --exit-code main -- cmd/praytel/wrapper_test.go cmd/praytel/snapshot_test.go cmd/praytel/internal/args/args_test.go` must exit 0.

## Hard ordering constraints (from the decisions) and where they are enforced
| Constraint | Source | Enforced by |
|---|---|---|
| The CI `test` job (`go test -race ./...`) and the depguard rule land before or with the first `internal/praytest` and `internal/selector` code | D4 item 2, D11 items 4, 5, 7, D12 S2/S3 | T1 → T7 (first praytest code), T1 → T8 (first selector code) |
| The run() → runCase extraction keeps the default path byte-identical, has no `praytel test` code and passes `wrapper_test.go`/`snapshot_test.go` unmodified | D2 item 9 | T9 is its own task. Isolation (T13, T16) comes after it |
| The callee-column position test runs first, on the pinned interpreter, with the documented fallback | D6 item 3 | T7 is the first praytest task. T11, T15 and T19 read its outcome constant |
| depguard lands before or with `internal/selector` | D12 S2 | T1 → T8 |
| Rollout in two PRs: PR 1 has the target, nightly workflow, shop `-card`, `test` job and dogfood file; PR 2 adds the `trace-tests` job and links the 100-run evidence | D11 item 7 | PR 1 = T1, T2, T6, T26 (with everything they depend on). PR 2 = T30, with a human gate |
| The record entry `record.Run` lives in `internal/record` (D9 item 10), but `runCase` lives in `package main` (D10 item 4) | D9 item 10, D10 item 4 | Injection: `record.Run(ctx, lt, cfg record.Config, run record.RunFunc)`. `cmd/praytel` passes a closure over `runCase` (T23 owns `Run`, T25 wires it) |
| A second SIGINT/SIGTERM kills the child's process group before praytel exits | D2 item 7, M2 | Exactly one handler exits: T16's while a group is live, runTest's (T24) otherwise (T24 Contract) |

## DAG
Edges (A -> B means B depends on A):
```
T1 -> T7, T1 -> T8, T1 -> T9, T1 -> T26, T1 -> T30
T2 -> T26, T2 -> T28, T2 -> T29
T3 -> T8, T3 -> T10, T3 -> T13, T3 -> T15, T3 -> T21a
T4 -> T24
T5 -> T21a
T6 -> T30
T7 -> T11, T7 -> T12
T8 -> T18a, T8 -> T20, T8 -> T23
T9 -> T13, T9 -> T25
T10 -> T14, T10 -> T23
T11 -> T15
T12 -> T20, T12 -> T23, T12 -> T27
T13 -> T16
T14 -> T25
T15 -> T17, T15 -> T18a
T16 -> T24
T17 -> T22
T18a -> T18b
T18b -> T19
T19 -> T20, T19 -> T21a, T19 -> T22
T20 -> T23
T21a -> T21b
T21b -> T24
T22 -> T27
T23 -> T25
T24 -> T25, T24 -> T26
T25 -> T27, T25 -> T28, T25 -> T29
T26 -> T30
T28 -> T29
T29 -> T27
```
The DAG is acyclic: every edge goes from a lower level to a higher one in the table below.

Parallel groups (level = longest dependency chain from a root). Tasks in one level have disjoint Owns and no edge between them. Tasks in different levels may also run in parallel; see each task's "Parallel-safe with".
| Level | Tasks |
|---|---|
| 1 | T1, T2, T3, T4, T5, T6 |
| 2 | T7, T8, T9, T10 |
| 3 | T11, T12, T13, T14 |
| 4 | T15, T16 |
| 5 | T17, T18a |
| 6 | T18b |
| 7 | T19 |
| 8 | T20, T21a, T22 |
| 9 | T21b, T23 |
| 10 | T24 |
| 11 | T25, T26 |
| 12 | T28, T30 |
| 13 | T29 |
| 14 | T27 |

Owns checks for tasks that share a package and may run in parallel:
- **`internal/praytest` source files.** T12 (`volatile.go`, `write.go`) and T22 (`docexamples_test.go`) share no file with T11, T15, T17, T18a, T18b, T19 or T20. `registry.go` and `surface.golden` are owned only by the chain T11 → T18a → T18b → T19.
- **`internal/praytest` trace goldens**, by test-name prefix: T11 `TestLoad*`, `TestSurface*`; T12 `TestWriteNew*` (and `TestVolatile*`, which emits no spans); T15 `TestCheck*`; T17 `TestHelper*`; T18a `TestBind*`; T18b `TestExist*`, plus re-stamping `TestCheckReentrant`; T19 `TestCompare*`, `TestProps*`, `TestKinds*`, plus re-stamping `TestCheckReentrant`; T20 `TestGenerate*`, `TestSelfCheck*`, `TestM3*`; T22 `TestDoc*`. No prefix matches another task's test names. `TestCheckReentrant` is shared only along the chain T15 → T18a → T18b → T19, never by parallel tasks.
- **`internal/testrun` trace goldens**: T5 `TestExpand*`; T21a `TestPrepare*`, `TestRun*`; T21b `TestReportGoldens*`. T21b also owns the `testdata/run/**` output goldens.
- **`cmd/praytel`.** T4 owns `main.go`'s ErrMapper branch, `internal/args` and `errmapper_test.go`. T9, T13 and T16 own `run.go`, `runcase.go`, `isolate.go`, their tests and the test child's flags. T4 and T9/T13/T16 are disjoint. T24 and T25 come after both.

## Coverage
### Decision -> tasks
| Decision | Tasks |
|---|---|
| D1 trace model | T3 (core), T14 (`checkViewIndexed`), T26 (CheckSpan order across shop runs), T29 (raw `status_message` note) |
| D2 case lifecycle | T9 (extraction), T13 (isolate, deadline, sweep), T16 (signals, `interruptExit`, bind failure), T21a (classifier, fold, interrupt at any point), T24 (file-mode wiring, runTest signal capture, single exit), T25 (record lifecycle), T22/T27 (cwd/stdin rule, port advice) |
| D3 Starlark host (split) | covered by its children: T7, T11, T15, T17, T18a, T18b, T19 |
| D4 boundary/pin/dialect | T1 (depguard), T7 (pin, dialect, upgrade path), T11 (surface golden, step corpus), T22 (dialect text) |
| D5 file evaluation | T11 (Load, `test()`, `env()`, phases), T17 (`load()`), T22 (rule text) |
| D6 check execution | T7 (position test), T11 (call index), T15 (Check, Failure, classification), T17 (`called from`), T18a (misuse), T18b (`run.one` stop), T19 (subjects, offenders), T21b (indentation in the report) |
| D7 selector semantics | T8 (matcher, descriptions), T18a (binding, span values, markers), T18b (existence), T19 (comparisons, always/never), T22, T29 |
| D8 recorded file generation | T8 (`Literal`), T12 (`Volatile`, `WriteNew`), T20 (`Generate`, `SelfCheck`, `Summarize`, goldens, M3), T23 (recorder calls), T27 (volatile list in docs) |
| D9 record TUI | T1 (tui depguard rule), T10 (interface, cursor), T14 (panel, Ctrl-W), T23 (`internal/record`, `record.Run`), T25 (wiring, S1 headless flow), T27 (keys doc), T28 (VHS) |
| D10 CLI dispatch/runner | T4 (`ParseTest`, hint, `UsageError`), T5 (`Expand`), T21a (`Prepare`/`Run`/classify/fold), T21b (report), T24 (dispatch, `RunCaseFunc`), T25 (record branch, D10 item 7 config), T27 (messages doc) |
| D11 CI dogfooding | T1 (`test` job, depguard), T2 (`shop -card`), T6 (nightly), T26 (target, Dagger fn, dogfood file), T30 (PR 2, evidence, A7) |
| D12 watch seams | T1 (`-race`, depguard), T3 (S1), T8 (S2), T15 (S3, plus fixtures from T18b and T19), T19 (S4), every task (S5 absence rule) |

### Vision v1 scenario / measure -> tasks
| Item | Tasks |
|---|---|
| S1 record from the TUI | T2, T4 (usage refusals), T8, T10, T12, T14, T20, T23, T25, T26 (`gift` case), T27, T28, T29 |
| S2 several inputs, exists/absent, one file | T11, T15, T18a, T18b, T19, T21a, T21b, T24, T26, T29 |
| S3 many files, globs, dirs, exit table | T4, T5, T11 (parse-all-first), T13, T16, T21a, T21b, T24 |
| S4 always/never | T15 (step limit), T19, T21b, T26 |
| S5 required CI | T1, T2, T6, T26, T30 |
| S6 watch (v1.1) | none, by design (D12 S5) |
| X1 (stretch) `span.parent`/`span.trace` | T18a (fields are v1 scope item 2) |
| §7 item 7 docs | T22 (praytest.5), T27 (praytel-test.1, praytel.1, index), T29 (guide, README), T28 (recording) |
| M1 | T28 (VHS ≤ 60 s), T29 (timed walkthrough ≤ 120 s) |
| M2 | T26, T6, T30 (human evidence), T24 (second signal leaves no grandchild) |
| M3 | T12, T20 |
| M4 | T3 (Settle), T5 (Expand), T8 (Select), T14 (Ctrl-W view), T21b (per case), T11 (load, D5 revisit) |
| M5 | none in v1 (D12 S5: no watch benchmark) |
| M6 | T15, T18b, T19, T21b |
| M7 | T30 (observed after merge) |
| M8 | T1 (shells in the CI container), T5 |

## D12 S5 do-not-build check
No v1 task contains a per-span evaluator, a collect/property-extraction phase, a span-graph resolver in the Starlark span binding, `Recording` repro fields, an expect-fail `SelfCheck` mode, or a watch benchmark. No task parses `--watch` or `--repro-dir`. T20's DoD includes a grep that `gen.go` has no repro/property/expect-fail identifiers.

## Pending human items that change task output
- **Exit 130/143 (D2 amendment, pending).** One function, `interruptExit(sig os.Signal) int` in `cmd/praytel/isolate.go` (T16), gives the exit code for an interruption. T16's second-signal exit and T24's runTest call it directly. `testrun.Run` receives it as its `exitFor` parameter (T21a), because testrun cannot import `package main`. **The fallback changes `interruptExit` only**: it returns 3 instead of 128+signo, and T21a's fold already lets 4 beat any `exitFor` value below 128. T21b's goldens `interrupted_*` and T27's exit table follow whichever applies at merge.
- **Columns (D6).** T7 records the outcome in `calleeColumns`. If it is `false`, the expected S2/S4 columns in the T19/T21b goldens are `13:18` / `4:17`, and the orchestrator puts D6's fallback amendment in front of the human.
- **A7.** T30's human gate: an admin marks `trace-tests` and `test` as required.
- **D10 item 4 contract amendments (orchestrator logs).** All three are in T21a, with reasons in its Notes:
  - `CaseRun` gains `Started time.Time`.
  - `testrun.Run(ctx, lt, files, rc, stdout, stderr) int` gains an `exitFor func(os.Signal) int` parameter: `Run(ctx, lt, files, rc, exitFor, stdout, stderr) int`.
  - A new exported type `testrun.Interrupted{Sig os.Signal}` (an `error`) joins the contract as the context cancel cause.

---

## T1: Add the CI `test` job and the depguard rules
Implements: D11 (items 4, 5), D4 (item 2), D9 (item 1 enforcement), D12 (S2 lint half, S3 `-race`)   Serves: S5, M8, M2 (guards)   Size: S
Depends on: none   Parallel-safe with: T2, T3, T4, T5, T6, T10, T14
Owns: `.golangci.yml`; `dagger/test.go` (new); `Makefile` (new `test` target and header-comment lines only); `.github/workflows/check.yml` (new `test` job only)
Contract: provides `make test` → `$(DAGGER) call test`. Inside the container it sets `PRAYTEL_REQUIRE_SHELLS=bash,zsh,sh` (consumed by T5). depguard rules: `starlark-single-importer` denies `go.starlark.net` in every file except `**/internal/praytest/**`; `tui-no-test-code` denies `github.com/barry-hennessy/praytel/internal/praytest`, `.../internal/selector` and `.../internal/record` in `**/internal/render/tui/**`.
Goal: Give praytel a failing unit-test job in CI, and the lint boundaries the Starlark and record work depend on, before any of that code exists. This makes the existing `cmd/praytel` e2e tests the guards that D2 counts on.
Definition of done:
  - [ ] `.golangci.yml` enables `depguard` with exactly the two rules above, and `make lint` exits 0.
  - [ ] `dagger/test.go` defines `func (m *Demos) Test(ctx context.Context) (string, error)`. On `goContainer(m.Source)` it runs `apt-get install -y --no-install-recommends zsh`, sets `CGO_ENABLED=1` and `PRAYTEL_REQUIRE_SHELLS=bash,zsh,sh`, runs `go test -race -count=1 ./...`, then runs `go test -count=1 ./...` in `demo/workloads/shop`.
  - [ ] `make test` exits 0, and its output contains `ok  	github.com/barry-hennessy/praytel/cmd/praytel`.
  - [ ] `check.yml` has a job `test` with the same checkout and `setup-dagger` steps as `lint`, `run: make test`, no `continue-on-error` and no path filter. `git diff main -- .github/workflows/check.yml` shows only added lines under `jobs:`.
  - [ ] `make zizmor` exits 0.
  - [ ] If pre-existing packages fail under `-race`: D11 item 4's fallback is applied. `Test` then runs `go test -count=1 ./...` plus `go test -race -count=1 ./internal/praytest/... ./internal/selector/... ./internal/testrun/... ./internal/record/...`, and the PR description links an issue that lists the failing packages.
Test plan: none of its own (CI and lint config). It is verified by `make lint`, `make test` and `make zizmor`. Whether the depguard rule fires is checked in T7, once `go.starlark.net` is in `go.mod`.
Benchmark: n/a
Notes: `goContainer` (`dagger/main.go:78`) uses `golang:1.26` (Debian, which has gcc for `-race`). `Praytel()` sets `CGO_ENABLED=0`, so set `1` explicitly here. Fallback patterns that match no package yet print a warning; check that the exit status is still 0. The `golden` job and the `on:` triggers stay untouched (S5). Marking `test` as required is part of T30's human gate (A7).

## T2: Add `-card TYPE` to the shop workload
Implements: D11 (item 7, PR 1; A5)   Serves: S1-S5, A5   Size: S
Depends on: none   Parallel-safe with: T1, T3-T17, T18a, T18b, T19, T20, T21a, T21b, T22-T25
Owns: `demo/workloads/shop/main.go`; `demo/workloads/shop/main_test.go` (new)
Contract: `shop -card TYPE` places exactly one order, A-1001 (customer `ada`, 2 items) with card TYPE. With no `-card`, the four default orders are unchanged. Internally: `func ordersFor(card string) []order`.
Goal: Let cases vary input data (A5) without changing the default orders that existing recordings and goldens use.
Definition of done:
  - [ ] `cd demo/workloads/shop && go test -count=1 -run TestOrdersFor .` passes. The test asserts that `ordersFor("")` equals today's 4-order list element for element, and that `ordersFor("gift")` equals `[{A-1001 ada gift 2}]`.
  - [ ] `cd demo/workloads/shop && go run . -pace 0s -card gift` prints `shop: taking orders` and exits 0 in under 2 s with no OTLP endpoint set.
  - [ ] `go run . -h` lists `-card`.
Test plan: unit (`TestOrdersFor`). It catches a changed default order list, which would break existing goldens and recordings.
Benchmark: n/a
Notes: An unknown card type takes `authorize`'s `default` branch (60 ms). `COMMON_PATHS` in the `Makefile` includes `demo/workloads`, so every recording's stamp goes stale. Recordings are refreshed before a release (`demo/README.md`), not in this task.

## T3: Export the check trace model from the TUI fold
Implements: D1 (items 1-5), D12 (S1)   Serves: S1 (Ctrl-W data), S2-S4, M2, M4   Size: M
Depends on: none   Parallel-safe with: T1, T2, T4, T5, T6, T7, T9, T11, T12
Owns: `internal/render/tui/snapshot.go`; `internal/render/tui/tui.go` (only the `rawStatusMsg` field on `spanView` and its assignment in `apply`); `internal/render/tui/check.go` (new); `internal/render/tui/check_test.go` (new); `internal/render/tui/perf_test.go` (add `BenchmarkSettle`)
Contract (D1 item 4, verbatim):
```go
type CheckSpan struct {
    Service, Name string; Status domain.SpanStatus; StatusMessage string // raw OTLP
    Attrs []domain.Attr; Start, End time.Time; Closed bool; Parent, Trace int // indices, -1 = none
}
type CheckView struct { Spans []CheckSpan; Problems []string; Captured []CapturedLine }
func Settle(events <-chan domain.Event) CheckView
func (m *Model) CheckView() CheckView
func collect(events <-chan domain.Event, opts SnapshotOptions) (*Model, []string, []CapturedLine) // unexported
func toCheckSpan(sp *spanView) CheckSpan // unexported; Parent/Trace = -1 (D12 S1)
```
Goal: One fold, one completeness rule and one read-only span type for file runs, the recorder and (later) watch. The content tie-break makes check-visible order depend only on content, and `--snapshot` output stays byte-identical.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/render/tui/... ./cmd/praytel/...` passes, and `git diff --exit-code main -- internal/render/tui/testdata internal/render/tui/snapshot_test.go cmd/praytel` exits 0 (no snapshot golden or test changed).
  - [ ] `check_test.go` passes with: `TestSettleProblemsEqualSnapshotProblems` (for every event fixture in `snapshot_test.go`, `Settle(ev).Problems` equals `Snapshot(ev, SnapshotOptions{Logs: false}).Problems`); `TestSettleOrderIgnoresArrivalOrderAndIDs` (100 shuffled arrival orders and random IDs over a forest with key-equal siblings that differ only in duration, start offset or raw status message give identical `Spans` once `Start`/`End` are zeroed); `TestCheckViewDoesNotMutateModel` (the model's `View()` string is identical before and after `CheckView()`); `TestCheckViewExcludesSyntheticAndDeclared`; `TestCheckSpanStatusMessageIsRaw` (an error span with an `exception.message` event and an empty status message gives `StatusMessage == ""`); `TestCheckSpanHasNoIDField` (reflection over `CheckSpan` finds no field name containing `ID` or `Id`); `TestToCheckSpanMatchesExtraction` (D12 seam comment: over every snapshot fixture, `toCheckSpan` equals `CheckView().Spans[i]` with `Parent`/`Trace` set to -1).
  - [ ] `BenchmarkSettle` runs under `-short`, and its ns/op is ≤ 25,000,000 on the implementer's machine (value pasted in the PR description).
Test plan: unit and golden. The existing snapshot goldens catch any change in snapshot text from the tie-break. The order test catches nondeterminism (M2). The mutation test catches live-model corruption in record mode.
Benchmark: ns/op of `tui.Settle` · 10,000 spans (1,000 traces × 10 spans, 4 attributes each, all closed) pre-loaded into a buffered channel that ends with `DrainComplete` and is closed · ≤ 25 ms/op (D1 revisit trigger: half of M4's 50 ms) · `go test ./internal/render/tui -run '^$' -bench BenchmarkSettle -short -benchmem`, and `make bench-diff` for CI hardware. Above the threshold, the task is not done: apply D1's revisit (cache subtree keys) inside this task.
Notes: Move `snapshot.go:56-95` into `collect` unchanged. The tie-break order is duration, start offset from the trace's earliest start, `rawStatusMsg`, span ID. It must never enter the snapshot key. `Settle` = `collect(events, SnapshotOptions{Attrs: attrspec.All(), Logs: false})` + extraction. The live `Problems` holds only `canonicalise`'s list. `CheckView()` copies each `*spanView` by value into a scratch `Model` (D1 item 5). No tracer (same precedent as `tui.Snapshot`).

## T4: Parse `praytel test` arguments
Implements: D10 (items 2, 3, 8: args tests)   Serves: S1 step 1, S2 conflict, S3 usage rows, §5 subcommand rule, R10, A9   Size: M
Depends on: none   Parallel-safe with: T1-T3, T5-T17, T18a, T18b, T19, T20, T21a, T21b, T22, T23
Owns: `cmd/praytel/internal/args/args.go`; `cmd/praytel/internal/args/parsetest_test.go` (new); `cmd/praytel/main.go` (only the `UsageError` branch in `newErrMapper`); `cmd/praytel/errmapper_test.go` (new)
Contract:
```go
type Mode int
const ( Files Mode = iota; Record )
type RunFlags struct { GRPCPort, HTTPPort int; DrainIdle, DrainTimeout time.Duration }
type TestConfig struct { Mode Mode; Paths []string; Record string; Command []string; Run RunFlags }
type UsageError struct { Msg string } // Error() == Msg
func ParseTest(argv []string, errOut io.Writer) (TestConfig, error) // errOut only for -h
func registerRun(fs *flag.FlagSet) *RunFlags  // shared by root and test flag sets
func newRootFlagSet(errOut io.Writer) (...)   // root definitions, registration order unchanged
```
Goal: Parse the two `test` forms with precise `praytel: <msg>` usage errors, and add the misplaced-flag hint, without changing any existing argv's meaning or output.
Definition of done:
  - [ ] [G]. `go test -count=1 ./cmd/praytel/...` passes, and `git diff --exit-code main -- cmd/praytel/internal/args/args_test.go cmd/praytel/wrapper_test.go cmd/praytel/snapshot_test.go` exits 0.
  - [ ] `TestParseTest` (table) returns `UsageError` with exactly these `Msg` values: `test: flag provided but not defined: -max-spans`; `--tui cannot be used with test`; `--snapshot cannot be used with test`; `put flags before file arguments: -x`; `--record takes no file arguments`; `--record needs a command after --` (both for no `--` and for an empty command); `gift.star: recorded tests must be named *.praytest.star so directory runs find them`; `test files take no command; the cases in gift.praytest.star declare their own`; `test runs files; to record one use --record=FILE -- command`; `no test files given; there is no implicit current-directory run`. Valid inputs give the expected `TestConfig` (Files with 2 paths; Record with command `[shop -pace 0s -card gift]`).
  - [ ] `TestParseTestHelp`: `-h` writes `usage: praytel test [flags] FILE|DIR|GLOB ...` and `       praytel test --record=FILE [flags] -- command [args ...]` to `errOut` and returns `flag.ErrHelp`.
  - [ ] `TestParseTestDefaultsMatchRoot`: `ParseTest([]string{"x.praytest.star"}, io.Discard)` gives `Run == RunFlags{4317, 4318, 250ms, 2s}`.
  - [ ] `TestHintMisplacedTest`: `Parse([--grpc-port=0 test x])` writes `praytel: put flags after "test"` and returns `ErrUsage`. `--tui test -- c` writes the hint. `--snapshot test -- c` does not, and writes `unknown --snapshot view "test"`. `-- test -f go.mod` returns `Command == [test -f go.mod]`.
  - [ ] `TestErrMapperUsageError` (`errmapper_test.go`): `newErrMapper(lt, &buf)(args.UsageError{Msg: "x"})` returns 1 and `buf.String() == "praytel: x\n"`, with no usage line.
Test plan: unit tables. They catch wording drift from the vision's quoted messages, and any reinterpretation of today's argvs.
Benchmark: n/a
Notes: No dispatch to `runTest` here (T24). The hint runs only at the two `ErrUsage` returns D10 item 2 names (`args.go:92`, `args.go:123`). It uses a throwaway root FlagSet with output `io.Discard`. `ParseTest` takes `errOut` (D10 writes only `argv`) because `-h` must print to stderr. `args` takes no tracer today; parsing stays untraced, as now.

## T5: Expand test file arguments
Implements: D10 (item 4 `Expand`; decision-tree default "input expansion")   Serves: S3, M8, M4   Size: M
Depends on: none   Parallel-safe with: T1-T4, T6-T17, T18a, T18b, T19, T20, T22, T23
Owns: `internal/testrun/doc.go`, `expand.go`, `expand_test.go`, `expand_shell_test.go`, `bench_test.go`, `main_test.go` (all new); trace goldens `internal/testrun/testdata/TestExpand*.golden` (new)
Contract: `func Expand(ctx context.Context, lt observability.LoggingTracer, args []string) (paths []string, problems []string)`
Goal: Implement S3's input rules as a pure function of the filesystem. It is the M8 and M4 test target.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/testrun -run TestExpand` passes. The table covers: a literal suffix file; a literal non-suffix file (`praytel: x.star: not a .praytest.star file`); a missing file (`praytel: x.praytest.star: no such file`); a directory, recursive and sorted bytewise; a dot-directory skipped; a symlinked directory not followed; a symlink to a regular file included; globs with `*`, `?` and `[`; a glob matching a directory expands that directory; an existing path containing `*` is used literally; nothing matched (`praytel: 'testdata/none/*.praytest.star' matched no .praytest.star files`); dedupe on the cleaned absolute path, first position wins; S3's example (`testdata/traces testdata/traces/shop.praytest.star` gives `perf/props.praytest.star` then `shop.praytest.star`, without `lib.star`); two bad arguments give two problem lines.
  - [ ] `TestExpandShellIndependence` (M8): for each of `bash`, `zsh`, `sh` with `LC_ALL=C`, `Expand` of the quoted glob equals `Expand` of the shell's unquoted expansion (`<shell> -c 'printf "%s\n" <glob>'`), in the same order. A missing shell is skipped unless `PRAYTEL_REQUIRE_SHELLS` names it, in which case the test fails.
  - [ ] `PRAYTEL_REQUIRE_SHELLS=bash,sh go test -count=1 ./internal/testrun -run TestExpandShellIndependence` passes on the implementer's machine.
  - [ ] `BenchmarkExpand` ns/op ≤ 50,000,000 (pasted in the PR description).
  - [ ] Tracing: follow the golden procedure (Rules). The committed `internal/testrun/testdata/TestExpand*.golden` are the only goldens in the commit, and at least one contains `testrun.Expand` (`grep -l 'testrun.Expand' internal/testrun/testdata/TestExpand*.golden` prints a path).
Test plan: unit table plus a shell-comparison test. They catch order and dedupe bugs and shell-dependent results (M8).
Benchmark: ns/op of `Expand` · 1,000 `.praytest.star` files in 100 directories 3 levels deep, plus 100 non-suffix files and a `.git` dot-directory with 100 files, under `b.TempDir()`; args `[root, root+"/*/*.praytest.star"]` · ≤ 50 ms/op (M4) · `go test ./internal/testrun -run '^$' -bench BenchmarkExpand -short`, and `make bench-diff`.
Notes: No `**` (A8). Use `filepath.WalkDir`, `filepath.Glob` and `filepath.Abs`+`Clean`.

## T6: Add the nightly trace-test workflow
Implements: D11 (item 6)   Serves: M2, M7, S5   Size: S
Depends on: none (consumes T26's `make trace-test COUNT= GOMAXPROCS= TRACE=` contract, fixed by D11 item 2)   Parallel-safe with: every task except T30
Owns: `.github/workflows/trace-nightly.yml` (new)
Contract: consumes `make trace-test [COUNT=N] [GOMAXPROCS=1] [TRACE=dir]` → `<dir>/report.txt`, `<dir>/runs.txt`.
Goal: Repeat the trace suite daily at both GOMAXPROCS settings, and open or update an issue when it fails, so drift is caught before contributors hit it.
Definition of done:
  - [ ] The file has top-level `permissions: contents: read`; `on.schedule` with cron `"23 5 * * *"`; and `workflow_dispatch` with inputs `count` (default `"20"`) and `gomaxprocs` (default `""`).
  - [ ] On a schedule it runs two steps: `make trace-test COUNT=20`, then `make trace-test COUNT=20 GOMAXPROCS=1 TRACE=trace-out-1` with `if: always()`. On dispatch it runs one step, with the inputs passed through `env:` and no `${{ inputs.* }}` inside any `run:`.
  - [ ] The job has `timeout-minutes: 120`. `actions/upload-artifact` is pinned to the SHA used in `.github/workflows/demos.yml`, with `if: always()`, and uploads `trace-out*`. A summary step appends each existing `report.txt` in a code block to `$GITHUB_STEP_SUMMARY`.
  - [ ] A second job with `if: failure() && github.event_name == 'schedule'` and `permissions: issues: write` on that job only opens an issue labelled `trace-nightly`, or comments on the open one (the shape of `.github/workflows/dependency-check.yml:29-48`). The body contains the run URL and the `runs.txt` lines.
  - [ ] `make zizmor` exits 0.
Test plan: none (workflow config; `make zizmor` is the static check). Its behaviour is exercised by T30's human dispatches, which can only happen after PR 1 is on `main`.
Benchmark: n/a
Notes: Ships in PR 1 with T26 (D11 item 7). GitHub cannot dispatch it before it is on the default branch. It must not edit `check.yml`.

## T7: Pin go.starlark.net and prove the call-site column
Implements: D4 (items 1, 3, 5), D6 (item 3: mandatory first test)   Serves: S2/S4 positions, M6, R3   Size: S
Depends on: T1   Parallel-safe with: T2, T3, T4, T5, T6, T8, T9, T10, T13, T14, T16
Owns: `go.mod`; `go.sum`; `internal/praytest/doc.go`, `dialect.go`, `position.go`, `position_test.go`, `main_test.go` (all new); `.github/dependency-check.md`; `CONTRIBUTING.md` (one line linking the upgrade path)
Contract: `var fileOptions = &syntax.FileOptions{TopLevelControl: true}` (unexported). `const calleeColumns bool` in `position.go` records this test's outcome (`true`: columns are the callee start; `false`: the raw `(` column). T11, T15 and T19 read it.
Goal: Pin the interpreter and establish, before any position-dependent code exists, whether S2/S4's `13:5`/`4:5` columns can be produced. If not, take D6's fallback.
Definition of done:
  - [ ] `go list -m go.starlark.net` prints `go.starlark.net v0.0.0-20261021141710-4f4267fb140d`, and `go mod tidy` leaves `go.mod`/`go.sum` unchanged (`git diff --exit-code go.mod go.sum` after tidy).
  - [ ] `go test -count=1 ./internal/praytest -run TestCallStackPosIsLparen` passes with 5 subtests: a top-level call, a call inside a `def`, a call inside a `lambda`, a call in a helper loaded through a test `Thread.Load` hook, and a call split across lines. Exactly one of these holds: (a) each subtest asserts `CallStack()[len-2].Pos` equals a `CallExpr.Lparen` found by `syntax.Walk` over the same parsed file, and `calleeColumns = true`; or (b) the subtests assert the observed mismatch, `calleeColumns = false`, and the PR description quotes D6's fallback amendment (S2 `13:18`, S4 `4:17`) for the orchestrator.
  - [ ] Depguard probe: with a scratch file `internal/domain/zz_probe.go` importing `go.starlark.net/starlark`, `make lint` reports a depguard finding naming that file. The scratch file is deleted before commit, and the lint line is pasted in the PR description.
  - [ ] The `doc.go` package doc states the upgrade path: `go get go.starlark.net@<pseudo>`, `go test ./internal/praytest/... -run TestSurface -update`, review the golden diff, and paste step-count/Universe changes into the PR. `CONTRIBUTING.md` links it, and `.github/dependency-check.md` has a `go.starlark.net` line.
  - [ ] [G].
Test plan: unit (the pin test). It catches the one unmeasured interpreter behaviour that D6's column design rests on, before anything is built on it.
Benchmark: n/a
Notes: The spike's gotcha: `CallStack()` is outermost first, so take `cs[len(cs)-2]`; `cs.At(i)` counts from the innermost frame. The spike needed `go mod tidy` for `golang.org/x/sys`. With no exported symbols yet, the `unused` linter may flag `fileOptions`/`calleeColumns`. Reference them from the test, or use `//nolint:unused // used by T11`. `make lint` must pass either way. No exported functions yet, so no spans and no trace golden (`TestCallStackPosIsLparen` emits none).

## T8: Build the selector matcher, descriptions and literal quoter
Implements: D7 (items 1-4, 9, 10), D8 (item 2 `Literal`), D12 (S2)   Serves: S1-S4, R5, M4   Size: M
Depends on: T1, T3   Parallel-safe with: T2, T4, T5, T6, T7, T9, T10, T11, T12, T13, T14, T15, T16, T17
Owns: `internal/selector/` (all new): `selector.go`, `literal.go`, `describe.go`, `selector_test.go`, `literal_test.go`, `describe_test.go`, `seam_test.go`, `bench_test.go`
Contract:
```go
package selector // imports tui (CheckSpan) and domain; never go.starlark.net
type Fields struct { Service, Name string; Status *domain.SpanStatus; Attrs []domain.Attr }
type Selector struct { /* unexported */ }
func New(f Fields) (Selector, error)               // attr values: string, int64, float64, bool only
func (s Selector) Where(f Fields) (Selector, error) // `where: name already set to "X"`
func (s Selector) Box() Selector                    // service+name only
func (s Selector) Matches(sp tui.CheckSpan) bool
func (s Selector) Select(spans []tui.CheckSpan) []int
func (s Selector) String() string      // payments{status="error",card.type="gift"}
func (s Selector) Source() string      // spans(service = "payments", name = "Gateway.Charge")
func (s Selector) WhereSource() string // status = "error", attrs = {"card.type": "gift"}   ("" if none)
func Equal(a, b domain.Value) bool     // D7 item 4 equality
func Literal(v domain.Value) (string, error)
func DescribeSpan(sp tui.CheckSpan) string               // payments/Gateway.Charge
func DescribeTrace(spans []tui.CheckSpan, i int) string  // web/Orders.Create{order.id="A-1001"}
```
Goal: A pure-Go, exact, typed matcher, plus the deterministic descriptions and the single literal quoter that file runs, the recorder, the generator and v1.1 watch all share.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/selector` passes with: `TestMatchesExact` (`Gateway.*` does not match `Gateway.Charge`; no case folding or trimming); `TestMatchesTypedAttrs` (int attr 2 matches selector values 2 and 2.0; bool `true` does not match int 1; `"2"` does not match 2); `TestNewRejectsBadAttrValues` (bytes, array, map); `TestWhereConflicts` (a different value gives `where: name already set to "Gateway.Charge"`; the same value is a no-op; a new attr key is ANDed); `TestSelectOrderIsSliceOrder`; `TestString` (`payments{status="error"}`, `Gateway.Bank.Authorize`, status first, then attrs by key); `TestSourceAndWhereSource`; `TestDescribeTrace` (id-like keys first, so `order.id` beats `customer`; otherwise the first 2 attributes by key; a root with no attributes prints bare; values cut at 40 characters; S2's text `web/Orders.Create{order.id="A-1001"}`); `TestLiteral` (escapes `\\ \" \n \r \t`; other non-printables as `\uXXXX`/`\UXXXXXXXX`; invalid UTF-8 refused; ints; floats get `.0` when needed; NaN/±Inf refused; `True`/`False`; arrays, maps and bytes refused).
  - [ ] `seam_test.go` contains `var _ func(Selector, tui.CheckSpan) bool = Selector.Matches` with the D12 seam comment.
  - [ ] `go list -deps ./internal/selector | grep -c starlark` prints `0`.
  - [ ] `BenchmarkSelect` ns/op ≤ 10,000,000 (pasted in the PR description).
Test plan: unit tables. They catch coercion and glob semantics (R5, predictability), description drift from S2/S4 text, and quoter escapes that would break generated files.
Benchmark: ns/op of 20 `Select` calls · 10,000 shop-like `CheckSpan`s (4 services, 7 names, 2-4 attrs) and 20 selectors mixing box-only, status and 1-2 attrs · ≤ 10 ms/op (D7 revisit: a fifth of M4) · `go test ./internal/selector -run '^$' -bench BenchmarkSelect -short`, and `make bench-diff`.
Notes: `CheckSpan.Attrs` is sorted by key, then value, so look up keys by binary search; keys may repeat. The `Literal` round-trip fuzz test needs Starlark and is in T20. The package takes no tracer (per-span hot path). T26 adds it to CONTRIBUTING's list.

## T9: Extract `runCase` from `run`, default behaviour unchanged
Implements: D2 (item 1 with zero `caseOpts`, item 9, item 10 hooks)   Serves: S1/S2 lifecycle; §5 "No existing invocation changes meaning"   Size: M
Depends on: T1   Parallel-safe with: T2-T8, T10, T11, T12, T14, T15, T17, T18a, T18b, T19, T20, T21a, T21b, T22, T23
Owns: `cmd/praytel/run.go`; `cmd/praytel/runcase.go` (new); `cmd/praytel/runcase_test.go` (new)
Contract:
```go
type caseOpts struct {
    Isolate  bool                                      // T13; false = today's behaviour
    Deadline time.Duration                             // T13
    View     func(events <-chan domain.Event) renderer // nil = today's snapshot/tui/stream switch
    OnStart  func()                                    // after cmd.Start succeeds
    OnExit   func(code int)                            // after cmd.Wait returns (code needed by D8)
}
type caseResult struct { ChildCode int; Started bool; snap *snapshot.Snapshot } // extended by T13/T16
func runCase(ctx context.Context, lt observability.LoggingTracer, cfg args.Config,
    stdin io.Reader, stdout, stderr io.Writer, rc caseOpts) (caseResult, error)
// run(...) == runCase(..., caseOpts{}) + today's snap.Finish
```
Goal: Make today's per-process lifecycle reusable per case, with hooks for record mode. The behaviour-neutral refactor is isolated in its own reviewable change, with no `praytel test` code.
Definition of done:
  - [ ] [G] and the default-path guard. `go test -race -count=1 ./cmd/praytel/...` passes.
  - [ ] `git diff --name-only main` lists only the three owned files, and none of them contains `praytest`, `testrun` or `ParseTest`.
  - [ ] `TestDefaultPathForwardsSIGINTOnce`: `praytel -- <child> -wait-signal` plus one SIGINT to praytel → the child prints its signal JSON line exactly once, and praytel exits with the child's code (D2 item 9c).
  - [ ] `TestRunCaseHooksFireAroundChild`: `OnStart` is called once, before `OnExit`; `OnExit` gets 7 for a child run with `-exit 7`. With zero `caseOpts`, no hook is called.
  - [ ] `TestRunCaseCustomView`: a `View` func gets a channel that is closed after a `domain.DrainComplete` arrives.
  - [ ] Tracing: `grep -n 'lt.Name("main.runCase")' cmd/praytel/runcase.go` matches, and `grep -n 'lt.Name("main.run")' cmd/praytel/run.go` still matches.
Test plan: e2e. The existing ~25 `cmd/praytel` binary tests are the guard for byte-identical behaviour, plus the three tests above. Together they catch signal-forwarding, drain or exit-code regressions from the refactor.
Benchmark: n/a
Notes: With zero `caseOpts`, the code must be line for line today's paths: stdin passthrough (nil under `--tui`), `proc.Signal` forwarding, unbounded `capWG.Wait`, no pgid and no deadline. D2 revisit: if any existing e2e test needs an edit, stop and revert to a duplicated `runCase` that leaves `run` untouched.

## T10: Record mode in the TUI, part 1: the Recorder interface and pick cursor
Implements: D9 (items 1-5; item 7 footer while picking)   Serves: S1 steps 2-3, M1   Size: M
Depends on: T3   Parallel-safe with: T1, T2, T4-T9, T11, T12, T13, T15, T16, T17, T18a, T18b, T19, T20, T21a, T21b, T22, T24, T26, T30
Owns: `internal/render/tui/record.go` (new); `internal/render/tui/tui.go`; `internal/render/tui/record_test.go` (new)
Contract (D9 item 2):
```go
type SpanRef string
type Recorder interface {
    Header() string
    Badge(ref SpanRef) string
    Rows(sp CheckSpan) int
    Panel(sp CheckSpan, ref SpanRef, row, width int) []string
    Key(sp CheckSpan, ref SpanRef, row int, key string) string
    Write(v CheckView, idx map[SpanRef]int) (status string, work func() string)
}
func (t TUI) WithRecorder(r Recorder) TUI
func (m Model) WithRecorder(r Recorder) Model
```
Goal: Add the record-mode header, Ctrl-T pick mode and a span cursor to the existing TUI, all behind `rec != nil`, so `--tui` is unchanged.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/render/tui/...` passes, and `git diff --exit-code main -- internal/render/tui/tui_test.go internal/render/tui/snapshot_test.go internal/render/tui/graph_test.go internal/render/tui/graph_render_test.go` exits 0.
  - [ ] `record_test.go` (fake `Recorder`) passes with: `TestRecordHeaderPrefixesHelpLine` (the footer starts with `RECORD gift.praytest.star · Ctrl-T pick · Ctrl-W write`); `TestCtrlTEntersPickOnFirstHighlightedSpan` (query `Charge`, then `tea.KeyCtrlT`, puts the cursor on `Gateway.Charge` with zero moves); `TestCtrlTWithNoSpansSaysNoSpansYet`; `TestCtrlTFromGraphSwitchesToTree`; `TestPickCursorMovesWithArrowsAndCtrlJK`; `TestPickModeLocksSearch` (runes go to `rec.Key`, the query is unchanged, Backspace/←/→/Tab do nothing); `TestEscLeavesPickModeKeepingPicks`; `TestCursorSnapsAfterForestEvicted`; `TestBadgeDrawnAfterPickedSpanHeader`; `TestPickFooterText` (`PICK ↑/↓ span • enter fields • a absent • esc back • ctrl+w write`); `TestNilRecorderIgnoresRecordKeys` (without a recorder, Ctrl-T and Enter leave `View()` byte-identical).
  - [ ] Record-mode code in `render/tui` is at most 400 lines (D9 revisit): `wc -l < internal/render/tui/record.go` plus the added lines in `git diff --numstat main -- internal/render/tui/tui.go` sum to ≤ 400.
Test plan: unit (model tests in the existing `tui_test.go` style). They catch any change to non-record behaviour and cursor or search-lock regressions.
Benchmark: n/a
Notes: `a` in the cursor stage calls `rec.Key(..., "a")`. `panelKey` gains `picking`, `panelOpen`, `recStatus`, `confirming`, `writing` and `rec.Header()`. The cursor is a `selectionKey` with a `cursorLine` fallback. Draw it in reverse video in `renderSpan`, which does not touch the layout cache (D9 verified claims). No spans in model code (TUI model hot path). Panel and Ctrl-W are T14.

## T11: Load test files: `test()`, `env()`, phases, call index, surface golden
Implements: D5 (items 1-6; item 8 in the package doc), D4 (items 3, 4, 6), D6 (item 3 call index; item 6 `MaxSteps` at load)   Serves: S2, S3 (parse all first; usage errors before any child), A6, M6, R9   Size: M
Depends on: T7   Parallel-safe with: T2-T6, T8, T9, T10, T12, T13, T14, T16
Owns: `internal/praytest/file.go`, `load.go`, `builtins_load.go`, `registry.go`, `callindex.go`, `phase.go`, `loader.go` (stub: `load()` returns `load() is not supported yet`; T17 replaces it), `load_test.go`, `callindex_test.go`, `phase_test.go`, `surface_test.go`, `testdata/surface.golden`, `testdata/steps/*.star` (3-5 files), `testdata/load/**` (all new); trace goldens `internal/praytest/testdata/TestLoad*.golden` and `TestSurface*.golden` (new)
Contract:
```go
type Position struct { File string; Line, Col int }  // String(): "file:line:col"
type UsageError struct { Pos Position; Msg string }   // String(): "file:line:col: msg"
type File struct { Path string; Cases []*Case }
type Case struct { Name string; Command []string; ExitCode int; Timeout time.Duration; Pos Position /* + unexported check, module, lt */ }
const MaxSteps = 10_000_000
func Load(ctx context.Context, lt observability.LoggingTracer, path string, getenv func(string) (string, bool)) (*File, []UsageError)
func LoadSource(ctx context.Context, lt observability.LoggingTracer, name string, src []byte, getenv func(string) (string, bool)) (*File, []UsageError)
// package-internal: requirePhase(thread *starlark.Thread, phase string) error; registry row {Name, Params, Phase /* comment */, fn}
```
Goal: Execute each file's top level once, before any child. Collect every usage error with its position, register cases, and freeze the builtin surface in a golden.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/praytest -run 'TestLoad|TestSurface'` passes with: `TestLoadReportsEveryBadTestCall` (one file with a missing `command`, a non-string element, a non-callable `check`, a `check` taking 2 parameters and a duplicate `name` gives 5 `UsageError`s, each at its `test` call position; the duplicate is reported at the second call); `TestLoadSyntaxErrorsArePositioned` (every `resolve.ErrorList` entry is returned); `TestLoadTopLevelRuntimeErrorStopsFile`; `TestLoadNoTestCall` (`path:1:1: no test(...)`); `TestLoadDefaults` (`exit_code` 0, `timeout` 10m, `timeout = "1s"` parsed, `timeout = "x"` is an error); `TestLoadTopLevelForRegistersCases`; `TestLoadGlobalReassignIsResolveError`; `TestLoadWhileIsRejected`; `TestLoadEnvReadsInjectedGetenvOnly` (a default is used when unset; a variable set only in the real environment is not seen); `TestLoadRequirePhase` (`requirePhase(thread, "load")` returns an error on a thread whose phase is `check`, and the reverse); `TestLoadStepCapIsUsageError`; `TestLoadSourceEqualsLoad`; `TestLoadUsageErrorColumnIsCallee` (when `calleeColumns` is true, the column is the start of `test`; otherwise the `(`).
  - [ ] `TestSurface` compares against `testdata/surface.golden`, which holds: sorted predeclared names including `Universe`; the `test` and `env` parameter lists from the registry; `fmt.Sprintf("%+v", *fileOptions)`; the `go.starlark.net` module version; and `ExecutionSteps()` for each `testdata/steps/*.star`. `go test ./internal/praytest -run TestSurface -update` followed by `go test ./internal/praytest -run TestSurface` passes.
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestLoad*.golden` and `testdata/TestSurface*.golden`. `grep -l 'praytest.Load' internal/praytest/testdata/TestLoad*.golden` and `grep -l 'praytest.LoadSource' internal/praytest/testdata/TestLoad*.golden` each print a path. `grep -n '\.Start(' internal/praytest/builtins_*.go` prints nothing.
  - [ ] `BenchmarkLoad1000`'s ns/op is pasted in the PR description. If it is above 50,000,000, the PR links an issue titled `D5 revisit: compile-once caching`.
Test plan: unit and golden. They catch usage errors that would otherwise start a child (S3 all-or-nothing), environment leakage (M2), dialect widening and builtin-surface drift (R3, R9).
Benchmark: total ns to `Load` 1,000 S2-sized files (2 cases, 3 selector constants each) from `b.TempDir()` · threshold 50 ms (D5 revisit trigger, not a v1 measure, so not a merge gate) · `go test ./internal/praytest -run '^$' -bench BenchmarkLoad1000 -short`.
Notes: All of this task's tests start with `TestLoad` or `TestSurface`, so its golden prefixes never match T17's `TestHelper*`. `test()` records and continues: it appends a `UsageError` at the caller position and returns `None` (spike A3). Thread locals: `praytest.phase` = `load`, `praytest.registry`. Build the call index (`Lparen` → callee start, plus argument byte ranges) with one `syntax.Walk` over the same `*syntax.File` that is compiled with `starlark.FileProgram`. Keep only the map and the source bytes. Freeze module globals after exec. `env()`/`test()` called inside a check are tested in T15, which owns the check thread.

## T12: Classify volatile attributes and write files without overwriting
Implements: D8 (items 4, 5 per-entry tests, 10, 11 `WriteNew` tests)   Serves: S1 steps 1, 3, 5; M3   Size: S
Depends on: T7   Parallel-safe with: T2-T6, T8-T11, T13-T17, T18a, T18b, T19, T21a, T21b, T22, T24, T26, T30
Owns: `internal/praytest/volatile.go`, `volatile_test.go`, `write.go`, `write_test.go` (all new); trace goldens `internal/praytest/testdata/TestWriteNew*.golden` (new; `TestVolatile*` emits no spans)
Contract:
```go
func Volatile(key string, v domain.Value) (reason string, volatile bool) // reason e.g. "looks volatile: port"
var ErrExists = errors.New("refusing to overwrite")
// WriteNew's refusal: fmt.Errorf("%s exists; %w", path, ErrExists)  →  "<path> exists; refusing to overwrite"
func WriteNew(ctx context.Context, lt observability.LoggingTracer, path string, src []byte) error
```
Goal: The heuristic half of D8's volatility guarantee, and a writer that never overwrites and never leaves a partial file under the target name on the link path.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/praytest -run 'TestVolatile|TestWriteNew'` passes.
  - [ ] `TestVolatile` has one positive subtest per key glob (`port`, `server.port`, `db_port`, `pid`, `process.pid`, `x_pid`, `thread.id`, `start.time`, `start_time`, `event_timestamp`, `x_unix_nano`, `request.duration`, `elapsed_ms`) and per value shape (16 and 32 lower-case hex digits, UUID, `2026-11-08T10:11`, `localhost:8080`, `http://h:8080/x`, `os.TempDir()+"/x"`, `/tmp/x`, `/var/folders/x`, `/private/var/folders/x`). Each positive has a near-miss negative (`order.id`, `report`, `card.type`, 15 hex digits, upper-case 16 hex digits, `http://h/x`, `/tmpfile`).
  - [ ] `TestWriteNewRefusesExisting`: `err.Error() == "x.praytest.star exists; refusing to overwrite"`, `errors.Is(err, ErrExists)` holds, and the existing content is unchanged.
  - [ ] `TestWriteNewRacingCreate`: a file created between the temp write and the link (injected hook) gives the same refusal, and no `.praytel-*.tmp` file remains.
  - [ ] `TestWriteNewFallbackWhenLinkUnsupported`: injected `EPERM`, `ENOTSUP` and `ENOSYS` from link each take the `O_EXCL` path and write the full content. An injected write error removes the target.
  - [ ] `TestWriteNewLeavesNoTempFile`: after a successful write, the directory holds only the target.
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestWriteNew*.golden`. `grep -l 'praytest.WriteNew' internal/praytest/testdata/TestWriteNew*.golden` prints a path.
Test plan: unit. One test per list entry plus near misses catches false positives on `order.id` (which D7 relies on). The injected link/write errors catch the overwrite race and the partial-file fallback.
Benchmark: n/a
Notes: `Volatile` takes no tracer and has no span: it is called per panel row. Inject `os.Link` and the write step through unexported package variables for tests. The file is about 1 KB and is written in one call (D8 item 10).

## T13: Isolate mode: process group, deadline, bounded capture, sweep
Implements: D2 (items 2, 3, 4, 5, 8; `caseResult` fields)   Serves: S2/S3 exit table rows 3 and 4, R7, M2   Size: M
Depends on: T9, T3   Parallel-safe with: T2, T4-T8, T10, T11, T12, T14, T15, T17, T18a, T18b, T19, T20, T21a, T21b, T22, T23
Owns: `cmd/praytel/runcase.go`; `cmd/praytel/isolate.go` (new); `cmd/praytel/isolate_test.go` (new); `cmd/praytel/testdata/child/main.go` (new flags only)
Contract: `caseResult` gains `Exited bool; TimedOut bool; TimeoutPhase string /* "child running" | "draining" */; Stdout, Stderr []byte; View tui.CheckView; StartedAt time.Time`. `caseOpts.Isolate`/`Deadline` take effect. `func settleView(dst *tui.CheckView) func(<-chan domain.Event) renderer` is in `isolate.go`.
Goal: Bound every case in time and in process lifetime: own process group, stdin `/dev/null`, inherited cwd, one deadline, a bounded capture wait and a group sweep. All of it sits behind `Isolate`, so the default path is unchanged.
Definition of done:
  - [ ] [G] and the default-path guard. `go test -race -count=1 ./cmd/praytel/...` passes.
  - [ ] `go test -race -count=1 ./cmd/praytel -run TestIsolate` passes with: `TestIsolateStdinIsDevNull` (a child reading stdin gets EOF at once); `TestIsolateInheritsCwd`; `TestIsolateChildEnvMatchesDefault` (`OTEL_EXPORTER_OTLP_ENDPOINT` is the receiver's; inherited `OTEL_EXPORTER_OTLP_*` are stripped); `TestIsolatePutsChildInOwnGroup` (the child's pgid differs from the test process's); `TestIsolatePipeHolderBoundedByDrainTimeout` (a child that leaves a grandchild holding stdout for 30 s returns within drain-timeout 500 ms + 3 s, and the grandchild is dead); `TestIsolateSweepKillsGroupAfterDrain`; `TestIsolateDeadlineTermThenKill` (a child ignoring SIGTERM, with a 500 ms deadline, gives `TimedOut`, phase `child running`, and returns within 500 ms + 2 s + 2×drain-timeout + 1 s); `TestIsolateDeadlineDuringDrain` (phase `draining`); `TestIsolateCapturesStdoutStderr` (`-stdout x -stderr y` gives `Stdout == "x"`, `Stderr == "y"`); `TestIsolateViewIsSettled` (`-export-grpc-span` gives `len(View.Spans) == 1` and empty `Problems`); `TestIsolateExitedBeforeDeadline` (`-exit 3` gives `Exited`, `ChildCode == 3`, not `TimedOut`).
  - [ ] Tracing: the post-exit sweep is an unexported function doing I/O, so `grep -n 'lt.Name("main.sweepGroup")' cmd/praytel/isolate.go` matches.
Test plan: in-process tests of `runCase` against `testdata/child`. They catch the pipe-holder drain hang, grandchild leaks across cases (M2 isolation) and an unbounded runtime (R7).
Benchmark: n/a
Notes: `SysProcAttr{Setpgid: true}` applies only under `Isolate`. Ignore ESRCH. The post-exit order is exactly D2 item 4: wait for capture EOF at most `--drain-timeout`, then SIGKILL the group and close `outR`/`errR`; then `corr.Stop` and the normal drain; then the group sweep; then `rec.Drop`, the 50 ms hold and teardown. Possible child flags: `-hold-stdout-child DURATION` (leaves a grandchild that writes its pid to a file), `-ignore-sigterm`, `-read-stdin`, `-print-cwd`. The `checking` timeout phase belongs to `testrun` (T21a). D2's self-hang revisit applies.

## T14: Record mode in the TUI, part 2: pick panel and Ctrl-W
Implements: D9 (items 6, 7, 8 TUI side, 9), D1 (`checkViewIndexed` on the same extraction)   Serves: S1 steps 3-5 and edge cases, M1   Size: M
Depends on: T10   Parallel-safe with: T1, T2, T4-T9, T11, T12, T13, T15, T16, T17, T18a, T18b, T19, T20, T21a, T21b, T22, T23, T24, T26, T30
Owns: `internal/render/tui/record.go`; `internal/render/tui/tui.go`; `internal/render/tui/record_panel_test.go` (new); `internal/render/tui/perf_test.go` (add `BenchmarkCheckViewIndexed`)
Contract: unexported `func (m *Model) checkViewIndexed() (CheckView, map[SpanRef]int)` and `type recordedMsg struct{ status string }`. How `Recorder.Write` results are used (planner's reading of D9 item 8, no signature change; T23 pins the recorder side in `TestWriteStatusIsSummary`): `work == nil` means `status` is a refusal or a "nothing picked" line, shown as is. `work != nil` means `status` is the write summary (`gift.praytest.star (1 case, 2 assertions)`). In pick mode the TUI shows `writing…` and runs `work` as a `tea.Cmd`. Outside pick mode it shows `write <status>? enter writes · any other key cancels` and runs `work` on Enter.
Goal: The panel stage and both Ctrl-W paths, with the full `CheckView` built only at Ctrl-W.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/render/tui/...` passes, with the same unmodified-test guard as T10.
  - [ ] `record_panel_test.go` passes with: `TestEnterOpensPanelLine1FromSpanView` (line 1 starts `payments/Gateway.Charge` and ends `(duration, times and IDs cannot be ticked)`); `TestPanelLeavesTenTreeRowsAt80x24`; `TestPanelKeysRouteToRecorder` (Space/`s`/`a` reach `rec.Key` with the panel row); `TestPanelEscReturnsToCursor`; `TestPanelFooterText` (`space assert • s selector • a absent • esc back • ctrl+w write`); `TestCtrlWInPickModeRunsWorkAtOnce`; `TestCtrlWOutsidePickModeAsksThenEnterWrites` (the prompt is `write gift.praytest.star (1 case, 2 assertions)? enter writes · any other key cancels` when the fake returns that summary); `TestCtrlWConfirmCancelledByOtherKeyConsumesIt`; `TestRepeatedCtrlWCancels`; `TestCtrlWWhileWritingShowsWritingAndIsIgnored`; `TestRefusalStatusShownAndPicksKept`; `TestRecordedMsgSetsStatus`; `TestCtrlWAfterChildExitIsUnchanged` (`m.done` true gives the same outcome); `TestKeyCtrlWString` (`tea.KeyMsg{Type: tea.KeyCtrlW}.String() == "ctrl+w"`); `TestStatusClearsOnNextKey`.
  - [ ] The T10 line budget still holds: record-mode lines in `render/tui` are ≤ 400.
  - [ ] `BenchmarkCheckViewIndexed` ns/op ≤ 25,000,000 (pasted in the PR description).
Test plan: unit model tests driving `tea.KeyMsg` sequences. They catch the D9 key semantics (immediate vs confirmed write, delete-word protection) and a stale footer cache.
Benchmark: ns/op of `checkViewIndexed` · the 10,000-span live model from T3's benchmark workload · ≤ 25 ms/op (D1's 25 ms bound for the same extraction; with T8's 10 ms for `Select`, it stays within D9's 50 ms Ctrl-W revisit) · `go test ./internal/render/tui -run '^$' -bench BenchmarkCheckViewIndexed -short`.
Notes: The panel height is `min(rows+2, contentHeight/2)`, and `contentHeight` subtracts it. Line 1 is drawn by `tui` from `spanView`, so IDs and times never reach the recorder. The panel uses per-span `toCheckSpan`, never the full view per key press.

## T15: Run a check and record failures
Implements: D6 (items 1, 2, 3 positions and `From` computation, 5, 6, 9 `Lines`, 10), D12 (S3)   Serves: S2/S4 failure lines, M6, R1, R2   Size: M
Depends on: T11, T3   Parallel-safe with: T2, T4, T5, T6, T8, T9, T10, T12, T13, T14, T16
Owns: `internal/praytest/check.go`, `failure.go`, `run.go` (the `run` Starlark value without `one`), `check_test.go`, `failure_test.go`, `reentrant_test.go`, `testdata/reentrant/pure.praytest.star` (all new); trace goldens `internal/praytest/testdata/TestCheck*.golden` (new; `TestFailureLines` emits no spans)
Contract:
```go
type CheckResult struct { Failures []Failure; Stopped, Cancelled bool; Steps uint64 }
func (c *Case) Check(ctx context.Context, v tui.CheckView) CheckResult // ctx.Done(): set cancelled, then thread.Cancel
type Kind int // Cardinality, ZeroMatch, Compare, TypeMismatch, Absent, SpanOpen, Tripped, Misuse, Runtime, StepLimit
type Offender struct { Span, Extra, Trace string }
type Failure struct { Pos Position; From *Position; Kind Kind; Builtin, Subject, Detail string; Offenders []Offender; More int; Stops bool }
func (f Failure) Lines() []string // [0] = "file:l:c: <text>[ (called from f:l:c)]"; [1:] = offender lines, unindented; the report indents 4/6
// package-internal for T18a/T18b/T19: checkState, stateOf(thread), (*checkState).record(Failure), (*checkState).stop(Failure) error, callerPos(thread) (Position, *Position), current offender slot
```
Goal: A fresh-thread check run with record-and-continue failures, a deterministic classification of every early stop (no message matching), and the single renderer of failure text.
Definition of done:
  - [ ] [G]. `go test -race -count=1 ./internal/praytest -run 'TestCheck|TestFailureLines'` passes with: `TestCheckRuntimeErrorIsPositioned` (`1/0` on line 5 gives one `Runtime` failure at `file:5:<col>`; `Lines()[0]` contains `runtime error: ` and ends `; rest of check not run`; no line contains `Traceback`); `TestCheckStepLimit` (a `for` over `range(10**9)` gives `exceeded step limit (10000000 steps); rest of check not run`, and `Steps >= MaxSteps`); `TestCheckCancelDiscardsFailures` (cancelling ctx mid-loop gives `Cancelled`, no `Failures`, and returns within 100 ms of the cancel); `TestCheckPhaseErrors` (`env()` and `test()` inside a check each give a positioned failure, not a Go error); `TestCheckNeedsNoChild` (`Check` on a `tui.CheckView` literal returns).
  - [ ] `TestFailureLines` (table): one row per D6 item-9 wording row (14 rows) built from constructed `Failure` values, with exact strings, including `... and 3 more` after 10 offenders and ` (called from shop.praytest.star:12:5)` when `From` is set.
  - [ ] `TestCheckReentrant` (D12 seam comment) loads every file matching `testdata/reentrant/*.praytest.star`. For each, it runs `Check` from 8 goroutines on 8 distinct in-memory views generated by the test, and each result must equal the sequential result for the same view, under `-race`. This task adds `pure.praytest.star` (checks using only loops, comprehensions, helper `def`s and a runtime error). T18b and T19 add fixtures to the same glob, so the test code itself is never edited.
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestCheck*.golden`. `grep -l 'praytest.Case.Check' internal/praytest/testdata/TestCheck*.golden` prints a path.
Test plan: unit. Pure-Starlark runtime errors, step limits and cancellation exercise every classification branch without builtins. The `Lines` table pins all M6 wording. The reentrancy test protects the v1.1 seam, and later fixtures extend it to builtins.
Benchmark: n/a (covered by T21b's per-case benchmark)
Notes: Classification order (D6 item 5): cancelled flag; `state.stop`; `ExecutionSteps() >= MaxSteps`; otherwise `Runtime` with `EvalError.Msg`, never `Backtrace()`. Thread local `praytest.phase` = `check`. Create builtins per check (spike caveat). `From` is the innermost frame in the case's file when the failing frame is not in it. It is computed here and tested with a loaded helper in T17. Durations print as `math.Round` milliseconds.

## T16: Isolate mode: interruption, the interrupt exit code, later-case bind failures
Implements: D2 (item 2 bind failure, item 7)   Serves: S3 exit table (INTERRUPTED row), R7, M2   Size: S
Depends on: T13   Parallel-safe with: T2, T4-T8, T10, T11, T12, T14, T15, T17, T18a, T18b, T19, T20, T21a, T21b, T22, T23
Owns: `cmd/praytel/runcase.go`; `cmd/praytel/isolate.go`; `cmd/praytel/isolate_signal_test.go` (new)
Contract:
```go
func interruptExit(sig os.Signal) int // the single source of the interruption exit code: 128+signo today; 3 under the D2 fallback
// caseResult.Interrupted os.Signal
// caseOpts.Exit func(code int)  (nil = os.Exit); on a second signal: SIGKILL -pgid, then Exit(interruptExit(sig))
// caseOpts.OnGroupDone func()   called after the group sweep (T13's step 4), i.e. once no process in the group can remain
var errBindOTLP = errors.New("bind otlp") // wraps ingest.New failures: errors.Is(err, errBindOTLP)
```
Under `Isolate`, `runCase` registers its SIGINT/SIGTERM handler **before** `cmd.Start` and stops it only **after** the group sweep. While a process group can exist, this handler is always the one that kills it (T24 relies on this).
Goal: Under `Isolate`, a first SIGINT/SIGTERM goes to the child's group and marks the case interrupted. A second one kills the group and then exits with `interruptExit(sig)`. A bind failure is identifiable, so the runner can tell a first-case failure from a later one.
Definition of done:
  - [ ] [G] and the default-path guard. `TestDefaultPathForwardsSIGINTOnce` (T9) passes.
  - [ ] `TestInterruptExit`: `interruptExit(syscall.SIGINT) == 130` and `interruptExit(syscall.SIGTERM) == 143` (under the fallback: both 3; this test is the only place the literals appear).
  - [ ] `TestIsolateFirstSignalInterruptsCase`: SIGINT delivered to the process during a `-wait-signal` child reaches the child's group, the case drains, and `Interrupted == syscall.SIGINT`. The same holds for SIGTERM.
  - [ ] `TestIsolateSecondSignalKillsGroupAndExits`: a second signal sends SIGKILL to `-pgid` and then calls the injected `Exit` with `interruptExit(sig)` (asserted against the function, not a literal), for both signals. The grandchild left by `-hold-stdout-child` is dead (its pid gives ESRCH) at the moment `Exit` is called.
  - [ ] `TestIsolateOnGroupDoneAfterSweep`: `OnGroupDone` is called once, after the grandchild is dead.
  - [ ] `TestRunCaseBindFailureIsErrBindOTLP`: with an occupied gRPC port, the error satisfies `errors.Is(err, errBindOTLP)`, and the child's `-marker` file does not exist.
Test plan: in-process signal tests with `signal.Notify` registered by `runCase`. They catch signals that would only reach the direct child (grandchildren survive), an exit that runs before the kill, and a misclassified bind failure.
Benchmark: n/a
Notes: 130/143 is pending human confirmation; the fallback edits `interruptExit` only. Signals that arrive while no group exists (during `Prepare`, a check, or between cases) are caught by `runTest` (T24), not here.

## T17: Resolve `load()` of helper files
Implements: D5 (item 7; item 6 helper top-level `test()`), D6 (item 3 `called from` test)   Serves: §4 `load()`, S3 (`lib.star` is not a test)   Size: S
Depends on: T15   Parallel-safe with: T2, T4, T5, T6, T8, T9, T10, T12, T13, T14, T16, T18a, T18b, T19, T20, T21a, T21b, T23, T24, T25, T26, T28, T29, T30
Owns: `internal/praytest/loader.go` (replaces T11's stub); `internal/praytest/loader_test.go` (new); `internal/praytest/testdata/load/helpers/**` (new); trace goldens `internal/praytest/testdata/TestHelper*.golden` (new)
Contract: no new exported API. `Load`/`LoadSource` resolve `load()`. The unexported `loadHelper(ctx, lt, ...)` reads and executes one helper module, starting span `praytest.loadHelper`. It runs once per helper per process, because helpers are cached, so it is not a hot path.
Goal: Relative, cached, cycle-checked helper loading, with the narrow rules D5 set for v0.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/praytest -run TestHelper` passes with: `TestHelperLoadRelativeToLoadingFile` (`load("lib.star", "f")` in `a/b.praytest.star` resolves `a/lib.star` with the process cwd set to `/`); `TestHelperLoadRejectsAbsoluteNonStarAndPraytest` (three messages, each at the `load` statement position); `TestHelperLoadAllowsDotDot`; `TestHelperLoadCachedOncePerProcess` (two files loading one helper execute it once, observed through a test-only exec counter); `TestHelperLoadCycleReported` (`a.star -> b.star -> a.star` at the `load` position); `TestHelperTopLevelTestCallIsError` (`test() may only be called while a .praytest.star file loads`); `TestHelperFunctionRegistersIntoCallingFile`; `TestHelperLoadFailureReportedPerLoadingFile` (each loader gets an error at its own `load` statement with the helper's position appended); `TestHelperRuntimeErrorShowsCalledFrom` (a check calling a helper that does `1/0` gives `Pos` in `lib.star`, `From` in the test file, and `Lines()[0]` containing `(called from x.praytest.star:`); `TestHelperCallIndexUsedForColumns`.
  - [ ] This task does not modify `internal/praytest/registry.go` or `internal/praytest/testdata/surface.golden`: neither appears in `git diff --name-only $(git merge-base HEAD main)` (`load` is a statement, not a registry builtin).
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestHelper*.golden`. `grep -c 'praytest.loadHelper' internal/praytest/testdata/TestHelperLoadRelativeToLoadingFile.golden` prints a number ≥ 1.
Test plan: unit. It catches path escapes from the wrong base directory, cycles that hang, and state leaking across files through shared helpers.
Benchmark: n/a
Notes: Every test here starts with `TestHelper`, so its golden prefix never matches T11's `TestLoad*`. A helper module runs on a fresh thread with phase `load` and no registry. Modules are cached by cleaned absolute path, frozen and shared. Use an in-progress marker for cycles.

## T18a: Bind selectors and span values
Implements: D7 (items 1, 2, 5, 6), D6 (item 7 misuse for selectors)   Serves: S1 (`.where`, selector constants), S2 (missing attribute marker), X1 (`parent`, `trace`)   Size: M
Depends on: T8, T15   Parallel-safe with: T2, T4, T5, T6, T9, T10, T12, T13, T14, T16, T17
Owns: `internal/praytest/bind_selector.go`, `bind_span.go`, `markers.go`, `bind_test.go`, `markers_test.go` (new); `internal/praytest/registry.go`; `internal/praytest/testdata/surface.golden`; trace goldens `internal/praytest/testdata/TestBind*.golden` (new)
Contract: Starlark `spans(service=, name=, status=, attrs={})` (phase-free) and `<selector>.where(...)`. A frozen span value with `service, name, status, status_message, duration_ms, attrs, parent, trace`, and a read-only `attrs` mapping. Package-internal for T18b/T19: `spanValue(view, i)`, `selectorOf(v starlark.Value) (selector.Selector, error)`, `absentMarker{key, span, trace}`, `openMarker{span, trace}`, `asMarker(v starlark.Value) (Failure, bool)`.
Goal: Expose D7's exact selectors and frozen span values to Starlark, with markers that `expect.*` will detect by Go type assertion.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/praytest -run 'TestBind|TestSurface'` passes with: `TestBindSpansStatusValidation` (`status = "bad"` is a `Misuse`); `TestBindAttrValueTypes` (`None`, list and dict are `Misuse`); `TestBindWhereConflictMisuse`; `TestBindSpanFields` (`duration_ms` is an unrounded float; `status` is a string; `status_message` is raw; `parent` is `None` for a root; `trace` is the root span; `s.parent.service` reads); `TestBindAttrsMapping` (`in`, `.get(k, d)`, `len`, sorted key iteration, `[k]` when present); `TestBindSelectorsAreFrozenConstants` (a module constant is reused unchanged across two cases); `TestBindAbsentMarkerPin` (pins what `==`, `<` and truth testing do with `<absent k>` on the pinned interpreter, and that it prints `<absent payment.result>`); `TestBindOpenMarkerOnUnclosedSpan` (`duration_ms` of a `Closed=false` span is the open marker).
  - [ ] `TestSurface`, with `testdata/surface.golden` regenerated, lists `spans`, `where` and the span/attrs attribute names.
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestBind*.golden`. Each test that runs a check gives a golden containing `praytest.Case.Check` (`grep -l 'praytest.Case.Check' internal/praytest/testdata/TestBind*.golden` prints at least one path).
Test plan: unit. It catches selector misuse that would silently match, and marker behaviour changes on an interpreter bump.
Benchmark: n/a
Notes: Selector literals are converted to `domain.Value` once, when the selector is created (D7 item 4). No new spans (builtins and span values are per-span paths). Descriptions come from `selector.String`, `DescribeSpan` and `DescribeTrace`.

## T18b: Add the existence assertions and `run.one`
Implements: D7 (item 7), D6 (items 4, 7)   Serves: S1 (`run.one`), S2 (exists, absent), M6   Size: S
Depends on: T18a   Parallel-safe with: T2, T4, T5, T6, T9, T10, T12, T13, T14, T16, T17
Owns: `internal/praytest/builtins_exist.go`, `exist_test.go`, `testdata/reentrant/exist.praytest.star` (new); `internal/praytest/run.go` (adds `one`); `internal/praytest/registry.go`; `internal/praytest/testdata/surface.golden`; trace goldens `internal/praytest/testdata/TestExist*.golden` (new) and `TestCheckReentrant.golden` (re-stamp; T15 → T18a → T18b is a chain, so no parallel overlap)
Contract: Starlark `run.one(sel)`, `expect.exists(run, sel, count=None)`, `expect.absent(run, sel)`. `expect` is a `starlarkstruct.Module`.
Goal: Existence and absence over D7 selectors, with `run.one`'s visible stop and record-and-continue for everything else.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/praytest -run 'TestExist|TestCheckReentrant|TestSurface'` passes with: `TestExistZeroMatches` (`exists <sel>: 0 spans matched`); `TestExistWrongCountListsOffenders` (`exists <sel>: 3 spans matched, want 1:` plus offender lines); `TestExistCountZeroIsMisuse` (`expect.exists: count must be >= 1; use expect.absent; rest of check not run`); `TestExistAbsentMatchedS2Text` (`absent payments{status="error"}: 1 span matched: payments/Gateway.Bank.Authorize in trace web/Orders.Create{order.id="A-1001"}`); `TestExistRunOneStopsCheck` (0 matches gives `one <sel>: 0 spans matched; rest of check not run`; a failing `expect.absent` before it is kept; one after it is not recorded); `TestExistRunOneManyListsOffenders`; `TestExistRecordAndContinue` (3 failing `expect.*` calls give 3 failures, A3); `TestExistAtLoadTimeIsUsageError`.
  - [ ] `testdata/reentrant/exist.praytest.star` (using `spans`, `where`, `run.one`, `expect.exists`, `expect.absent`) is picked up by `TestCheckReentrant`, which passes under `-race` with `reentrant_test.go` unchanged (`git diff --exit-code $(git merge-base HEAD main) -- internal/praytest/reentrant_test.go`).
  - [ ] `TestSurface`, with `testdata/surface.golden` regenerated, lists `run.one`, `expect.exists` and `expect.absent`.
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestExist*.golden` and `testdata/TestCheckReentrant.golden`. `grep -l 'praytest.Case.Check' internal/praytest/testdata/TestExist*.golden` prints at least one path.
Test plan: unit. It catches false failures after `run.one` (no cascades), lost independent failures (A3), and `count=0` ambiguity.
Benchmark: n/a
Notes: No span per builtin.

## T19: Add comparison and always/never assertions
Implements: D7 (items 4, 8), D6 (items 5 offender naming, 8 subjects, 9 rows), D12 (S4)   Serves: S2 missing-attribute line, S4, X1, M6   Size: M
Depends on: T18b   Parallel-safe with: T2, T4, T5, T6, T9, T10, T12, T13, T14, T16, T17
Owns: `internal/praytest/builtins_compare.go`, `builtins_props.go`, `compare_test.go`, `props_test.go`, `kinds_test.go`, `testdata/reentrant/props.praytest.star` (new); `internal/praytest/registry.go`; `internal/praytest/testdata/surface.golden`; trace goldens `internal/praytest/testdata/TestCompare*.golden`, `TestProps*.golden`, `TestKinds*.golden` (new) and `TestCheckReentrant.golden` (re-stamp; chain, no parallel overlap)
Contract: Starlark `expect.eq/ne/lt/le/gt/ge(actual, want)`, `expect.always(run, sel, pred)`, `expect.never(run, sel, pred)`.
Goal: The rest of §4's assertion table. Exact subjects come from source text, cross-type comparisons record a failure instead of raising, and always/never list offenders deterministically.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/praytest -run 'TestCompare|TestProps|TestKinds|TestCheckReentrant|TestSurface'` passes, with:
    - `TestCompareEqNeWording` (`eq span.status: got "ok", want "error"`; `ne span.name: got "X", want anything but "X"`); `TestCompareOrderWording` (`lt span.duration_ms: got 452.3, want < 200`); `TestCompareCrossTypeOrderRecordsFailure` (`lt span.attrs["n"]: cannot compare string "2" with int 2`, and a later failing `expect` in the same check is also recorded); `TestCompareEqMixedTypes` (int 2 eq float 2.0 passes; `"2"` eq 2 fails; `True` eq 1 fails); `TestCompareAbsentAttrS2` (`eq span.attrs["payment.result"]: attribute payment.result is absent on payments/Gateway.Charge in trace <trace>`); `TestCompareOpenSpanDuration` (`lt span.duration_ms: span still open: payments/Gateway.Charge in trace <trace>`); `TestCompareSubjectIsSourceText` (whitespace runs collapsed, cut at 60 bytes with `...`, a multi-line call reports the line where `expect` starts).
    - `TestPropsNeverS4Text`: on a fixture view where A-1002's bank span lasts 452 ms, the S4 file gives `props.praytest.star:4:5: never Gateway.Bank.Authorize: tripped by payments/Gateway.Bank.Authorize (452ms) in trace web/Orders.Create{order.id="A-1002"}` (`:4:17:` when `calleeColumns` is false). Also `TestPropsAlwaysFalseWording`, `TestPropsZeroMatchFails` (`never <sel>: 0 spans matched`), `TestPropsOffendersCapTenAndMore` (13 offenders give 10 lines plus `... and 3 more`), `TestPropsPredicateRuntimeErrorNamesSpan` (`runtime error: ... on payments/X in trace ...; rest of check not run`), `TestPropsPredicateStepLimitNamesSpan`, `TestPropsPredicateCrossTypeIsRuntimeError`, and `TestPropsS4AlwaysCardTypeInAttrsPasses`.
    - `TestKinds` (M6; D12 seam comment): one row per `Kind` asserting `Kind`, `Builtin` and a non-zero `Pos`, and that `Lines()[0]` matches `^[^:]+:\d+:\d+: ` and contains no `Traceback`. The always/never rows (tripped, false, zero match) assert `Builtin` ∈ {`always`, `never`}, `Kind` = `Tripped`/`Tripped`/`ZeroMatch`, and `len(Offenders)` = 1/1/0.
    - `TestSurface`, with `testdata/surface.golden` regenerated, lists all §4 builtins plus `env` and `test`.
  - [ ] `testdata/reentrant/props.praytest.star` (using comparisons and always/never lambdas) is picked up by `TestCheckReentrant`, which passes under `-race` with `reentrant_test.go` unchanged since its merge-base.
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestCompare*.golden`, `TestProps*.golden`, `TestKinds*.golden` and `TestCheckReentrant.golden`. `grep -l 'praytest.Case.Check' internal/praytest/testdata/TestProps*.golden` prints at least one path.
Test plan: unit. The per-kind table is M6's golden. The S4 text test pins the vision's exact line. The cross-type and marker tests catch the CEL-style "no such key" pain D7 designed against. The reentrancy fixture covers predicates on a shared frozen module.
Benchmark: n/a (T21b measures the whole check)
Notes: The subject is the source text of the first argument, from the call index's byte ranges (T11). The span is named only for markers. Set the current offender before each predicate call and clear it after (D6 item 5).

## T20: Generate recorded files and self-check them
Implements: D8 (items 1, 2 fuzz, 3, 5, 6, 7, 8, 9, 11), D12 (no repro fields)   Serves: S1 (file shape, acceptance), M3   Size: M
Depends on: T19, T12, T8   Parallel-safe with: T2, T4, T5, T6, T9, T10, T13, T14, T16, T17, T21a, T21b, T22, T24, T26, T30
Owns: `internal/praytest/gen.go`, `selfcheck.go`, `gen_test.go`, `selfcheck_test.go`, `literal_fuzz_test.go`, `m3_test.go` (new); `internal/praytest/testdata/gen/**` (output goldens and a shop-shaped `CheckView` fixture; new); trace goldens `internal/praytest/testdata/TestGenerate*.golden`, `TestSelfCheck*.golden`, `TestM3*.golden` (new; `TestSummarize` emits no spans)
Contract (D8 item 1, with `ctx`/`lt` added per the tracing rule):
```go
type Recording struct { File string; Argv []string; StillRunning bool; ExitCode *int; Spans []PickedSpan }
type PickedSpan struct { Index int; Selector []string; SelStatus bool; Asserts []string; Absent bool }
type Summary struct { Cases, Assertions, Absences int }
func Summarize(rec Recording) Summary // pure; no tracer (called from the TUI model via Recorder.Write)
func Generate(ctx context.Context, lt observability.LoggingTracer, rec Recording, view tui.CheckView) ([]byte, Summary, error)
func SelfCheck(ctx context.Context, lt observability.LoggingTracer, src []byte, rec Recording, view tui.CheckView) error
```
Goal: Turn picks into S1's exact file shape as a pure function, and refuse any file that does not load, has the wrong command, or fails against the recorded run.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/praytest -run 'TestGenerate|TestSelfCheck|TestSummarize|TestM3'` passes, with output goldens in `testdata/gen/`: `s1_gift` (the vision's S1 example byte for byte, line 1 `# praytel test format v0. Recorded with: praytel test --record=gift.praytest.star -- shop -pace 0s -card gift`), `still_running` (WARNING line), `relative_path` (`./bin/shop` adds `# command paths are relative to the directory praytel test is run from`), `nonzero_exit` (`exit_code = 3`), `multi_span` (variables `charge`, `bank_authorize`), `absence` (the two comment lines), `name_collision` (`_2`), `quoting` (header argv with a space, a quote, an empty element and a newline).
  - [ ] `TestSummarize`: for every golden's `Recording`, `Summarize(rec)` equals the `Summary` returned by `Generate` (S1: `{1, 2, 0}`).
  - [ ] `TestGenerateIsPickOrderIndependent`: permuted picks give identical bytes.
  - [ ] `TestGenerateRefuses`: array, map and bytes attributes; a volatile attribute (the error names the key); invalid UTF-8; a status tick on a `Closed=false` span (`span still open; its status is not final`).
  - [ ] `TestSelfCheckRefuses`: a selector matching 2 spans; a command that differs from `rec.Argv`. `TestSelfCheckPassesS1`.
  - [ ] `TestM3NoVolatileText`: for every span in the shop-shaped fixture, with every tickable field ticked, `SelfCheck` returns nil, and the output matches none of `[0-9a-f]{16}`, an RFC 3339 date-time, `\d+ms` or `duration`.
  - [ ] `go test ./internal/praytest -run '^$' -fuzz FuzzLiteralRoundTrip -fuzztime 30s` reports no failure (`x = <Literal(v)>` evaluates equal to `v` under `selector.Equal`). The seed corpus is committed.
  - [ ] Generated goldens contain no `load(`, no `env(`, and `test(` only at column 1.
  - [ ] D12 S5: `grep -nE 'Repro|Property|ExpectFail' internal/praytest/gen.go internal/praytest/selfcheck.go` prints nothing.
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestGenerate*.golden`, `TestSelfCheck*.golden` and `TestM3*.golden`. `grep -l 'praytest.Generate' internal/praytest/testdata/TestGenerate*.golden` and `grep -l 'praytest.SelfCheck' internal/praytest/testdata/TestSelfCheck*.golden` each print a path.
Test plan: golden plus unit plus fuzz. The goldens pin the public v0 file shape (R9). The fuzz test catches quoter bugs. The M3 test is the vision's measure.
Benchmark: n/a
Notes: Values come from `view` at `PickedSpan.Index`, never from TUI display strings. `SelfCheck` = `LoadSource` (zero `UsageError`s, exactly 1 case, `Command == rec.Argv`) plus `(*Case).Check(ctx, view)` with no failures. Absence picks are emitted commented out, so they do not fail the self-check (D8 item 7).

## T21a: Run cases: prepare, classify, fold exit codes
Implements: D10 (items 4 `Prepare`/`Run`, 6), D2 (item 6 classifier; item 7 interruption at any point)   Serves: S2, S3 exit table, M4   Size: M
Depends on: T5, T19, T3   Parallel-safe with: T2, T4, T6, T9, T10, T12, T13, T14, T16, T17, T20, T22, T23
Owns: `internal/testrun/prepare.go`, `run.go`, `classify.go`, `report.go` (minimal: file header, case line, summary), `prepare_test.go`, `run_test.go` (new); trace goldens `internal/testrun/testdata/TestPrepare*.golden`, `TestRun*.golden` (new; `TestClassify`/`TestFold` emit no spans)
Contract (D10 item 4, with three amendments; see Notes):
```go
type RunCaseFunc func(ctx context.Context, f *praytest.File, c *praytest.Case) CaseRun
type CaseRun struct { ChildCode int; Exited, TimedOut bool; Phase string; Interrupted os.Signal; BindErr error
                      View tui.CheckView; Stdout, Stderr []byte; Started time.Time }
type Interrupted struct { Sig os.Signal } // error; the ctx cancel cause runTest uses (T24)
func Prepare(ctx context.Context, lt observability.LoggingTracer, args []string, getenv func(string) (string, bool)) ([]*praytest.File, []string)
func Run(ctx context.Context, lt observability.LoggingTracer, files []*praytest.File, rc RunCaseFunc,
         exitFor func(os.Signal) int, stdout, stderr io.Writer) int
```
- **Context rules.** `rc` receives `context.WithoutCancel(ctx)` (the lifecycle handles its own signals, T16). `(*Case).Check` receives `ctx` bounded by `Started + c.Timeout`. Before each case, `Run` checks `context.Cause(ctx)`; if it is an `Interrupted`, that case and every later case are `not run`, and `rc` is not called.
- **Interrupt during a check.** When `Check` returns `Cancelled` and `context.Cause(ctx)` is an `Interrupted`, the case is INTERRUPTED (not INCOMPLETE), and every later case is `not run`.
- **Fold.** First, `code = max over cases (0; 3 for INCOMPLETE; 4 for FAIL)`. The run is **interrupted** if any case is INTERRUPTED **or** `context.Cause(ctx)` is an `Interrupted` when the loop ends (this covers a signal between cases, and one after `Prepare` before the first case). If interrupted, `c := exitFor(sig)`; `code = c` when `c >= 128`, else `code = max(code, c)`. That one rule covers both 130/143 and the D2 fallback.
- **Summary.** When the run is interrupted but no case is INTERRUPTED, the summary's items end with `interrupted between cases`, for example `praytel: 2 of 3 cases in 1 file not run, interrupted between cases`. D10 item 5 leaves this case unstated; this is the planner's wording, consistent with its comma-joined items.
Goal: Own the case loop, D2's first-match classifier, interruption at any point and the worst-result exit fold. All of it is testable with a fake case runner and no child process.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/testrun -run 'TestClassify|TestFold|TestPrepare|TestRun'` passes with:
    - `TestClassify` (table, one row per D2 step-6 branch): interrupted → INTERRUPTED; exited early with a code ≠ `exit_code` → FAIL (check run only if complete and not timed out); deadline in each phase → INCOMPLETE; non-empty `Problems` → INCOMPLETE with the check not called; check failure, runtime error or step limit → FAIL; else ok.
    - `TestFold` (table): {ok, ok} → 0; {INCOMPLETE} → 3; {INCOMPLETE, FAIL} → 4; {FAIL, INTERRUPTED} with `exitFor` returning 130 → 130; with `exitFor` returning 3 (fallback) → 4; {ok, INTERRUPTED} with fallback → 3; {ok, not run} with cause `Interrupted` and `exitFor` 130 → 130; the same with fallback → 3.
    - `TestPrepareCollectsAllProblems`: a missing argument, a file with a syntax error and a valid file give every problem line (expansion problems first, then `file:line:col: msg` in path order) and no `File`s to run.
    - `TestRunStopsAfterInterruptedCase` (`rc` returns `Interrupted`; later `rc` calls count 0; later outcomes are `not run`).
    - `TestRunInterruptDuringCheck` (ctx cancelled with cause `Interrupted{SIGINT}` while a check loops under the step cap: the outcome is INTERRUPTED, later cases are `not run`, and the code is `exitFor(SIGINT)`).
    - `TestRunInterruptBeforeCase` (cause `Interrupted{SIGINT}` set after case 1 returns ok and before case 2: case 2 is `not run`, `rc` is not called for it, the summary ends `interrupted between cases`, and the code is `exitFor(SIGINT)`: 130 with the 128+signo `exitFor`, 3 with the D10 item 6 fallback `exitFor`).
    - `TestRunInterruptBeforeFirstCase` (cause set before `Run` is entered: every case is `not run`, `rc` is never called, the summary is `praytel: 3 of 3 cases in 1 file not run, interrupted between cases`, and the code is `exitFor(SIGINT)`).
    - `TestRunDeadlineDuringCheckIsIncomplete`; `TestRunLaterBindFailure` (that case and the rest are INCOMPLETE and not called); `TestRunFirstBindFailureReturns1` (stdout empty); `TestRunStreamsPerCase` (the fake `rc` for case 2 observes case 1's line already on stdout).
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestPrepare*.golden` and `testdata/TestRun*.golden`. `grep -l 'testrun.Prepare' internal/testrun/testdata/TestPrepare*.golden` and `grep -l 'testrun.Run' internal/testrun/testdata/TestRun*.golden` each print a path.
Test plan: unit tables on outcome and exit code. They catch a timeout hiding an exit mismatch, a cancelled check producing a timing-dependent verdict, signals lost outside the child or between cases, and the wrong precedence.
Benchmark: n/a (T21b)
Notes: **Three D10 item 4 contract amendments (orchestrator logs them; listed under "Pending human items").**
- *`CaseRun.Started`.* It is consistent with D2 item 5 (one deadline from `cmd.Start` covering child, drain and check). D10's "seam friction" revisit fires when `CaseRun` gains a field carrying lifecycle state that `testrun` re-derives; it does not fire here, because `testrun` cannot re-derive the child's start time (only `runCase` knows when `cmd.Start` returned).
- *`exitFor func(os.Signal) int` on `Run`.* This keeps one source for the interrupt exit code (`interruptExit`, T16), so the D2 fallback is a one-function change. It must be passed in because `testrun` cannot import `package main`.
- *The exported `Interrupted` type.* `runTest` (`package main`) needs a cause type that `testrun` recognises with `errors.As`, and that type must live where both can see it.

Durations print without trailing zero units (`10m`, `1s`), not Go's `10m0s`.

## T21b: Report text and output goldens
Implements: D10 (items 5, 8 goldens), D6 (item 9 indentation, item 10), D2 (item 7 report lines)   Serves: S2, S3, S4 outputs; M4; M6   Size: M
Depends on: T21a   Parallel-safe with: T2, T4, T6, T9, T10, T12, T13, T14, T16, T17, T20, T22, T23
Owns: `internal/testrun/report.go`; `internal/testrun/report_test.go`, `case_bench_test.go` (new); `internal/testrun/testdata/run/**` (output goldens and `.praytest.star` inputs; new); trace goldens `internal/testrun/testdata/TestReportGoldens*.golden` (new)
Contract: the report format of D10 item 5. Case line `fmt.Sprintf("  %-4s %s", label, name)`; failure lines indented 4, continuations 6; captured output on stderr framed `praytel: <file>: <case>: child stdout:` / `child stderr:`; summary wording as D10 item 5, plus T21a's `interrupted between cases` item.
Goal: Pin every user-visible line of a file run byte for byte against the vision's examples.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/testrun -run TestReportGoldens` passes. Each golden in `testdata/run/` compares stdout, stderr and the exit code from `Run` with a fake `RunCaseFunc` over files built with `praytest.LoadSource`:
    - `s2_pass` (S2's passing block, exit 0); `s2_fail` (S2's failing block byte for byte, exit 4); `s2_missing_attr` (exit 4); `s3_two_files` (S3's block, exit 4); `s4_never` (S4's block, exit 4).
    - `exit_mismatch` (a `<file>:<l>:<c>: exit code 2, want 0` line at the case `Pos`; the stderr framing, with a newline appended when missing); `exit_mismatch_incomplete_note`; `incomplete_problems` (`trace incomplete: a; b`, exit 3).
    - `timeout_child`, `timeout_draining`, `timeout_checking` (`timeout 10m exceeded while <phase>`, exit 3; the cancelled check's failures are not printed).
    - `later_bind_failure` (`could not bind OTLP ports: ...`); `first_bind_failure` (exit 1, stdout empty).
    - `interrupted_sigint` (`INTERRUPTED` then `not run` lines, summary `praytel: 1 of 5 cases in 2 files interrupted, 3 not run`, exit 130 via the test's `exitFor`); `interrupted_sigterm` (143); `interrupted_during_check` (a check looping under the step cap is interrupted: `INTERRUPTED`, the later case `not run`, exit 130); `interrupted_between_cases` (case 1 `ok`, case 2 `not run`, summary ending `interrupted between cases`, exit 130).
    - `mixed_summary` (`praytel: 1 of 3 cases in 2 files failed, 1 incomplete`); `step_limit` (exit 4); `fail_beats_incomplete` (exit 4).
  - [ ] `BenchmarkCaseOverhead` ns/op ≤ 50,000,000 (pasted in the PR description).
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestReportGoldens*.golden`. `grep -l 'testrun.Run' internal/testrun/testdata/TestReportGoldens*.golden` prints a path.
Test plan: golden. Every S2/S3/S4 example and every exit-table row is pinned byte for byte, and with D6's `Lines` table (T15) that covers M6's report format.
Benchmark: ns/op of `tui.Settle` over a 10,000-span event stream plus `(*Case).Check` with 20 assertions (5 `exists`, 5 `absent`, 5 `eq` after one `run.one`, 5 `always`/`never` lambdas) on the result · ≤ 50 ms/op (M4) · `go test ./internal/testrun -run '^$' -bench BenchmarkCaseOverhead -short`, and `make bench-diff`.
Notes: The report is an unexported concrete type, with no `Reporter` interface (D10). If 130/143 is refused, the `interrupted_*` goldens' exit codes follow the fallback `exitFor`.

## T22: Write the praytest(5) file-format reference
Implements: D4 (item 3 dialect text), D5 (item 8 rule, late-binding note), D7 (semantics), D6 (positions, stop rule), D2 (item 3 cwd/stdin rule), D8 (item 9 format marker)   Serves: §7 v1 item 7, R2, R9   Size: S
Depends on: T19, T17   Parallel-safe with: T2, T4, T5, T6, T9, T10, T12, T13, T14, T16, T20, T21a, T21b, T23, T24, T25, T26, T28, T29, T30
Owns: `docs/reference/praytest.5.md` (new); `internal/praytest/docexamples_test.go` (new); trace goldens `internal/praytest/testdata/TestDoc*.golden` (new)
Contract: provides `docs/reference/praytest.5.md`, which T27's index links.
Goal: Document the v0 file format exactly as built, so users can write and edit files without reading code.
Definition of done:
  - [ ] Frontmatter has `title`, `summary` and `weight` (CONTRIBUTING.md).
  - [ ] Sections: Synopsis; Cases (`test(...)` parameters and defaults); Selectors; Spans (fields, `attrs` mapping, `None` parent); Assertions (a table with every praytel builtin in `testdata/surface.golden`); Failures (record-and-continue, `run.one` stop, positions, `called from`); Phases (D5 item 8 rule); `env()`; `load()`; Command execution (D2 item 3 rule, verbatim); Dialect (D4 item 3 text, verbatim, plus the late-binding factory idiom); Step limit (10,000,000); Format version (`# praytel test format v0`); Absent and open markers (behaviour outside `expect.*` unspecified; use `in`/`.get`); Not a sandbox (R2).
  - [ ] `TestDocExamples`: every fenced `python` block in `docs/reference/praytest.5.md` that contains `test(` loads through `LoadSource` with zero `UsageError`s. `go test -count=1 ./internal/praytest -run TestDocExamples` passes.
  - [ ] `TestDocListsEveryBuiltin` (same file): every praytel-defined name in `testdata/surface.golden` appears in the doc.
  - [ ] Tracing: follow the golden procedure (Rules), committing only `testdata/TestDoc*.golden`. `grep -l 'praytest.LoadSource' internal/praytest/testdata/TestDocExamples.golden` prints the path.
Test plan: unit. These two doc tests catch examples that do not load and builtins missing from the reference.
Benchmark: n/a
Notes: The volatile list belongs in praytel-test(1) (T27), not here (D8 item 4: generator behaviour, not file format).

## T23: Implement recorder logic and the record entry in `internal/record`
Implements: D9 (item 6 tick rules, item 8 write-path steps 2-6, item 9 record tests, item 10 record entry), D8 (consequences: `Volatile`, `Recording`, `Generate` → `SelfCheck` → `WriteNew`)   Serves: S1 steps 1 (exists refusal), 3-5 and edge cases, M3   Size: M
Depends on: T10, T20, T12, T8   Parallel-safe with: T2, T4, T5, T6, T9, T13, T14, T16, T17, T21a, T21b, T22, T24, T26, T30
Owns: `internal/record/` (all new): `record.go`, `run.go`, `child.go`, `picks.go`, `write.go`, `record_test.go`, `run_test.go`, `write_test.go`, `main_test.go`, trace goldens `testdata/Test*.golden` (this task is the package's only owner)
Contract:
```go
type Config struct { File string; Argv []string; Stdin io.Reader; Stdout io.Writer }
type ViewFunc = func(events <-chan domain.Event) interface{ Run(context.Context) error }
type RunFunc func(ctx context.Context, view ViewFunc, onStart func(), onExit func(code int)) (int, error)
// D9 item 10's entry, with the D10-style injection of the lifecycle:
func Run(ctx context.Context, lt observability.LoggingTracer, cfg Config, run RunFunc) (int, error)
//   1. checkTarget(ctx, lt, cfg.File): if the file exists, return (1, err) with err wrapping praytest.ErrExists,
//      text "<file> exists; refusing to overwrite"; run is not called.
//   2. child := &Child{}; r := New(ctx, lt, cfg.File, cfg.Argv, child)
//   3. return run(ctx, func(ev) { return tui.New(lt, ev, cfg.Stdin, cfg.Stdout, nil).WithRecorder(r) }, child.Started, child.Exited)
type Child struct { /* atomic running flag, exit code */ }
func (c *Child) Started(); func (c *Child) Exited(code int); func (c *Child) State() (running bool, code *int)
func New(ctx context.Context, lt observability.LoggingTracer, file string, argv []string, child *Child) *Recorder // implements tui.Recorder
func checkTarget(ctx context.Context, lt observability.LoggingTracer, file string) error // unexported, does I/O, traced
```
Goal: All of the recorder's rules (picks, tick refusals, Ctrl-W checks, D8 calls) and D9's record entry in a TUI-free package that is table-tested without a terminal.
Definition of done:
  - [ ] [G]. `go test -count=1 ./internal/record` passes, and `var _ tui.Recorder = (*Recorder)(nil)` compiles.
  - [ ] Run tests: `TestRunRefusesExistingFileBeforeRun` (the fake `RunFunc` is not called; code 1; `errors.Is(err, praytest.ErrExists)`; `err.Error() == "gift.praytest.star exists; refusing to overwrite"`); `TestRunWiresHooksAndCode` (the fake `RunFunc` calls `onStart` then `onExit(5)`; `Child.State()` reports running between them; `Run` returns 5); `TestRunViewCarriesRecorder` (the `ViewFunc` handed to `RunFunc` returns a value whose type is `tui.TUI`).
  - [ ] Tick-rule tests, one per refusal, each returning its status text: `looks volatile: port`; `not recordable in v0` (array, map, bytes); `span still open; its status is not final`; `not a selector field` (`s` on `status_message`); `clear the absence pick first` (both directions).
  - [ ] `TestPanelRows` (`status`, `status_message`, then attributes by key, formatted `[✓][s] card.type = "gift"`) and `TestBadge` (`[2✓ 1s]`, `[absent]`).
  - [ ] `TestWriteStatusIsSummary`: with 2 assert ticks and 1 selector mark on one span, `Write` returns `status == "gift.praytest.star (1 case, 2 assertions)"` and a non-nil `work`. With an extra absence pick it returns `"gift.praytest.star (1 case, 2 assertions, 1 absence to edit)"`. `Write` does not call `Generate`; it uses `praytest.Summarize`, and `Generate` runs inside `work` (D9 item 8.6). This is checked through a package-level `generate` func variable that the test replaces with one that fails the test if called before `work()`.
  - [ ] Write tests: zero picks gives `("nothing picked; Ctrl-T to pick", nil)`; an evicted pick gives `not written: 1 picked span was evicted under data pressure and has been unpicked (payments/Gateway.Charge); Ctrl-W again writes the rest`, and the pick is removed; a selector matching 2 spans gives `selector matches 2 spans in this run; mark an attribute with s to pick one`; the WARNING line is present if and only if `Child.State()` reports running when `Write` is called; an exited non-zero child emits `exit_code = <n>`; on success `work()` returns `wrote gift.praytest.star (1 case, 2 assertions)`, or `(1 case, 2 assertions, 1 absence to edit)` with an absence pick; a second write returns `not written: gift.praytest.star exists; refusing to overwrite`; picks are kept after every refusal.
  - [ ] Tracing: follow the golden procedure (Rules) for `./internal/record`. The committed goldens contain spans named `record.Run`, `record.New`, `record.checkTarget` and `record.write` (the span started inside `Write`'s `work` func); each `grep -l '<span>' internal/record/testdata/Test*.golden` prints a path.
Test plan: unit tables over fabricated `CheckSpan`/`CheckView` values plus a fake `RunFunc`. They cover every S1 edge case and the record entry without a terminal or a child.
Benchmark: n/a (the Ctrl-W latency budget is covered by T14's `checkViewIndexed` benchmark and T8's `Select` benchmark)
Notes: Imports `tui`, `selector`, `praytest` and `domain`. Only `cmd/praytel` imports this package. `Config` is a record-specific config, not `args.Config`: Go's internal-package rule forbids `internal/record` from importing `cmd/praytel/internal/args`. D10 item 7's `args.Config` is built in `cmd/praytel` and captured by the `RunFunc` closure (T25). Read `Child.State()` inside `Write`, at the moment of Ctrl-W (D9 item 8.2). `work` runs off the update loop as a `tea.Cmd`. The `tui.Recorder` methods take no span (TUI model path).

## T24: Wire `praytel test FILE|DIR|GLOB ...`
Implements: D10 (items 1, 4 `RunCaseFunc` backing), D2 (item 2 first-case bind failure exits 1; item 7 signal capture and the single exit)   Serves: S2, S3, S4, §5 subcommand rule, R10, M2   Size: M
Depends on: T16, T4, T21b   Parallel-safe with: T2, T6, T10, T12, T14, T17, T20, T22, T23
Owns: `cmd/praytel/main.go` (dispatch in `runProcess`); `cmd/praytel/test.go` (new); `cmd/praytel/test_test.go` (new); `cmd/praytel/testdata/praytest/**` (new); `cmd/praytel/testdata/child/main.go` (new flags only, if `TestPraytelTestSecondSignalKillsGroup` needs one, e.g. `-ignore-sigint`)
Contract: `func runTest(ctx context.Context, lt observability.LoggingTracer, mapErr bones.ErrMapper[int], argv []string, getenv func(string) (string, bool), stdout, stderr io.Writer) int`.
  - **Setup.** It starts span `main.runTest`, parses with `args.ParseTest`, and registers `signal.Notify(ch, SIGINT, SIGTERM)` for its whole remaining duration, covering `Prepare`, every case, every check and the gaps between them. It derives `ctx, cancel := context.WithCancelCause(ctx)`.
  - **First signal.** It calls `cancel(testrun.Interrupted{Sig: sig})`. `testrun.Run` turns that into INTERRUPTED/`not run` and the exit code, whenever it arrives (T21a). A signal before `Run` is entered makes every case `not run`.
  - **Second signal: exactly one handler exits.** `runTest` keeps `groupLive atomic.Bool`, set to `true` by the case's `OnStart` hook and back to `false` by `caseOpts.OnGroupDone` (T16, after the group sweep).
    - While `groupLive` is true, `runTest`'s handler does nothing on the second signal. T16's handler, which is registered before `cmd.Start` and stopped after the sweep, SIGKILLs `-pgid` and then exits with `interruptExit(sig)`.
    - While `groupLive` is false (no process group can exist), `runTest`'s handler exits at once with `interruptExit(sig)`.
    - So praytel never exits while a group it started can still be alive.
  - **Case runner.** Its `RunCaseFunc` calls `runCase(ctx, lt, cfgFor(tc, c), nil, ..., caseOpts{Isolate: true, Deadline: c.Timeout, View: settleView(&v), OnStart: ..., OnGroupDone: ...})` and maps `errBindOTLP` to `CaseRun.BindErr`. It calls `testrun.Run(ctx, lt, files, rc, interruptExit, stdout, stderr)`.
Goal: Make file mode a working subcommand end to end. It dispatches only on `argv[0] == "test"`, reports all usage errors before any child, runs real children under isolation, catches signals at any point of the run, and never leaves a process group behind on exit.
Definition of done:
  - [ ] [G] and the default-path guard. `go test -race -count=1 ./cmd/praytel/...` passes.
  - [ ] `test_test.go`, using the built binary with `testdata/child` named through `env("CHILD_BIN", ...)`, passes with:
    - `TestPraytelTestPasses` (exit 0; stdout ends `praytel: 2 cases in 1 file passed`); `TestPraytelTestAssertionFailureExits4`; `TestPraytelTestExitMismatchExits4WithCapturedOutput` (the captured output is on stderr); `TestPraytelTestNoSpansIsIncomplete` (exit 3, `trace incomplete: no spans arrived`); `TestPraytelTestTimeoutIsIncomplete` (`timeout = "1s"` with a `-wait-signal` child gives exit 3 and `timeout 1s exceeded while child running`).
    - `TestPraytelTestUsageErrorsStartNoChild`: one subtest per exit-1 row of S1-S3, including the S2 conflict `praytel: test files take no command; the cases in gift.praytest.star declare their own`, a wrong suffix, a missing file, nothing matched, no arguments, `--tui`, and a syntax error in one of two files. Each exits 1 with its message on stderr, and the child's `-marker` file does not exist.
    - `TestPraytelTestInterruptExits130` (SIGINT while case 1 of 2's child runs gives `INTERRUPTED`, then `not run`, and exit `interruptExit(SIGINT)`); `TestPraytelTestInterruptDuringCheckExits130` (case 1's child exits at once and its check loops under the step cap; SIGINT sent after the child's `-marker` file appears plus the drain idle gives `INTERRUPTED` for case 1, `not run` for case 2, and exit `interruptExit(SIGINT)`).
    - `TestPraytelTestSecondSignalKillsGroup`: case 1's child ignores SIGINT and leaves a grandchild that writes its pid to a file. Send SIGINT, then SIGINT again. Praytel exits with `interruptExit(SIGINT)`, and when the test observes the exit, signal 0 to the grandchild's pid returns ESRCH.
    - `TestPraytelTestFirstCaseBindFailureExits1`; `TestDashDashTestStillRunsTestBinary` (`praytel -- test -f go.mod` exits 0); `TestFlagsBeforeTestHint` (`praytel --grpc-port=0 test x.praytest.star` exits 1, stderr has `praytel: put flags after "test"`).
  - [ ] Tracing: `grep -n 'lt.Name("main.runTest")' cmd/praytel/test.go` matches.
Test plan: e2e with the built binary. It is the only place where real process groups, signals, ports and the report meet.
Benchmark: n/a (M4 is measured by T21b; overhead over `praytel -- cmd` is the same drain, per D2 item 8)
Notes: `getenv` is `os.LookupEnv`. The test flags keep the root defaults (4317/4318); tests use `--grpc-port=0 --http-port=0`. Record mode is T25. Expected exit codes in tests come from `interruptExit`, so the D2 fallback only changes that function. Tune the step-cap loop in the interrupt-during-check fixture so the check runs at least 2 s on the implementer's machine.

## T25: Wire `praytel test --record=FILE -- cmd`
Implements: D9 (item 10 wiring), D10 (item 7), D2 (item 10)   Serves: S1   Size: S
Depends on: T24, T14, T23, T9   Parallel-safe with: T2, T6, T17, T22, T26, T30
Owns: `cmd/praytel/record.go` (new); `cmd/praytel/test.go` (record branch); `cmd/praytel/record_test.go` (new)
Contract: `func runRecord(ctx context.Context, lt observability.LoggingTracer, tc args.TestConfig, stdin io.Reader, stdout, stderr io.Writer) int`.
  - It starts span `main.runRecord` and builds `cfg := args.Config{Command: tc.Command, TUI: true, GRPCPort: tc.Run.GRPCPort, HTTPPort: tc.Run.HTTPPort, DrainIdle: tc.Run.DrainIdle, DrainTimeout: tc.Run.DrainTimeout, MaxSpans: correlator.DefaultMaxSpans, MaxLogLines: correlator.DefaultMaxLogLines}` (D10 item 7).
  - It calls `record.Run(ctx, lt, record.Config{File: tc.Record, Argv: tc.Command, Stdin: stdin, Stdout: stdout}, run)`, where `run` is a closure calling `runCase(ctx, lt, cfg, stdin, stdout, stderr, caseOpts{Isolate: false, View: view, OnStart: onStart, OnExit: onExit})` and returning the child's code.
  - On an error wrapping `praytest.ErrExists` it prints `praytel: <err>` to stderr and returns 1. Otherwise it returns the child's code, as `--tui` does. Record mode is `Isolate=false` (D2 item 10), so signals behave exactly as under `--tui`.
Goal: Connect parsing, the lifecycle and D9's record entry for S1, with the FILE-exists refusal before any child starts.
Definition of done:
  - [ ] [G] and the default-path guard.
  - [ ] `TestRecordConfigFromTestConfig`: the `args.Config` passed to `runCase` has `TUI == true`, `MaxSpans == correlator.DefaultMaxSpans`, `MaxLogLines == correlator.DefaultMaxLogLines`, and ports and drain from `tc.Run`.
  - [ ] `TestRecordRefusesExistingFile`: exit 1, stderr `praytel: gift.praytest.star exists; refusing to overwrite`, and the child's `-marker` file does not exist.
  - [ ] `TestRecordRefusesWrongSuffix`, `TestRecordNeedsCommand`, `TestRecordRejectsFileArgs`: exit 1 with T4's messages, and no child started.
  - [ ] `TestRecordReturnsChildCode` (in-process, a child run with `-exit 5` and a fake `View` substituted via the closure): `runRecord` returns 5.
  - [ ] `TestRecordS1FlowHeadless`: a `tui.Model` built with `WithRecorder(record.New(...))` over a shop-shaped event fixture, driven through the S1 key sequence (type `Charge`, Ctrl-T, Enter, Space on `status`, Space on `status_message`, `s` on `card.type`, Ctrl-W) and running the returned `tea.Cmd`, writes a file byte-equal to `internal/praytest/testdata/gen/s1_gift.golden`.
  - [ ] Tracing: `grep -n 'lt.Name("main.runRecord")' cmd/praytel/record.go` matches.
Test plan: e2e for the refusals (built binary); in-process for the config, the exit code and the full S1 key flow, which no terminal-less e2e can drive.
Benchmark: n/a
Notes: The entry is D9's `record.Run` in `internal/record` (T23), with the lifecycle injected as `RunFunc`, the same shape D10 uses for `RunCaseFunc`. This task only wires it.

## T26: Dogfood: shop trace tests and `make trace-test` (PR 1)
Implements: D11 (items 1, 2, 7 PR 1, 8), D1 (consequence: CheckSpan order across runs)   Serves: S5, S1/S2/S4 on the real shop, M2, M7   Size: M
Depends on: T24, T2, T1   Parallel-safe with: T6, T10, T12, T14, T17, T20, T22, T23, T25, T27, T28, T29
Owns: `testdata/traces/shop.praytest.star` (new); `dagger/tracetest.go` (new); `Makefile` (`trace-test` target and header lines); `.gitignore` (`trace-out*/`); `CONTRIBUTING.md` ("Trace tests" section: the native fast loop; the tracing list gains `internal/praytest`, `internal/testrun`, `internal/record`, and `internal/selector` under "take no tracer"); `cmd/praytel/shop_order_test.go` (new)
Contract: provides `make trace-test [COUNT=N] [GOMAXPROCS=1] [TRACE=dir]` → `<dir>/report.txt`, `<dir>/runs.txt`, `<dir>/rc`, `<dir>/run-<i>.log`. On failure it prints the class line `make trace-test: praytel test exited <rc> (<class>)` (D11 item 2). `func (m *Demos) TraceTest(ctx context.Context, count int /* +default=1 */, gomaxprocs string /* +optional */) *dagger.Directory`, marked `+cache="never"`.
Goal: praytel's own trace facts, run the same way locally and in CI, with the report surviving the Dagger boundary. The `trace-tests` job is not added yet (that is PR 2, T30).
Definition of done:
  - [ ] `testdata/traces/shop.praytest.star` holds the cases `gift` (S1 shape), `visa` (S2) and `all_cards` (S4 flipped to today's shop: `expect.never(run, BANK, lambda s: s.parent.attrs.get("card.type") == "amex" and s.duration_ms < 200)` and `expect.always(run, CHARGE, lambda s: "card.type" in s.attrs)`). Every command starts `["shop", "-pace", "0s"` and names `shop` by `PATH`.
  - [ ] Native: `go build -o bin/ ./cmd/praytel && (cd demo/workloads/shop && go build -o ../../../bin/ .) && PATH=$PWD/bin:$PATH praytel test --grpc-port=0 --http-port=0 testdata/traces` exits 0, and its last line is `praytel: 3 cases in 1 file passed`.
  - [ ] Mutation (not committed): with shop's amex `work(450 * time.Millisecond)` changed to `work(50 * time.Millisecond)`, the same command exits 4 and stdout contains `never Gateway.Bank.Authorize: tripped by payments/Gateway.Bank.Authorize`. The transcript is in the PR description.
  - [ ] `make trace-test COUNT=3` exits 0, prints `3 of 3 runs passed`, and `trace-out/rc` contains `0`. With the mutation, `make trace-test` exits non-zero and its output ends with `make trace-test: praytel test exited 4 (FAIL)`, after the full report.
  - [ ] `TestCheckViewOrderIsStableAcrossShopRuns`: builds shop into `t.TempDir()`, runs `runCase` (Isolate, `settleView`) 5 times with `-pace 0s`, and finds `CheckView.Spans` identical across runs once `Start`/`End` are zeroed. `go test -race -count=1 ./cmd/praytel -run TestCheckViewOrderIsStableAcrossShopRuns` passes.
  - [ ] `git diff --exit-code main -- .github/workflows/check.yml` exits 0 (no `trace-tests` job in PR 1). `cd dagger && go build ./...` exits 0, and `make lint` exits 0.
Test plan: e2e (the dogfood file against the real shop, with a mutation check that S5's acceptance turns red) plus an integration test for D1's cross-run order.
Benchmark: n/a
Notes: The script runs all N runs with no early stop, writes `report.txt` as the first failing run's log plus `runs.txt` plus the `k of N` line, and always `exit 0` (shape of `dagger/golden.go:17-32`). Bust the exec cache with `TRACE_TEST_AT` (shape of `dagger/main.go:113-115`). Build shop in its own module directory. The admin and evidence steps are T30.

## T27: Write praytel-test(1) and update praytel(1) and the docs index
Implements: D10 (messages, report, exit), D9 (keys, confirm, browser caveat), D8 (item 4 volatile list), D2 (port advice, worst-case bound)   Serves: §7 v1 item 7, S1-S3   Size: S
Depends on: T25, T12, T22, T29   Parallel-safe with: T6, T26, T30
Owns: `docs/reference/praytel-test.1.md` (new); `docs/reference/praytel.1.md`; `docs/index.md`
Contract: links `reference/praytest.5.md` (T22) and `guides/trace-tests.md` (T29).
Goal: Document the subcommand surface (CONTRIBUTING: one reference file per command) and link it from the existing manual and the index.
Definition of done:
  - [ ] `praytel-test.1.md` has frontmatter and the sections Synopsis (the two §5 lines), Description, Flags (5 flags with the defaults `praytel test -h` prints), Input files (S3 rules), Recording (Ctrl-T, ↑/↓, Ctrl-K/Ctrl-J, Enter, Space, `s`, `a`, Esc, Ctrl-W, the Enter confirm outside pick mode, graph→tree, the search lock, the browser/IDE terminal caveat), Volatile attributes, Output (report format and labels, including `interrupted between cases`), Exit status (0/1/3/4 and 130/143, or the fallback if the human refused it), Usage errors, Examples, CI (`--grpc-port=0 --http-port=0`).
  - [ ] Each key glob and value shape in `internal/praytest/volatile.go` appears in the Volatile attributes section; each `Msg` string in T4's `TestParseTest` table and each problem format in `internal/testrun/expand.go` appears in the Usage errors section. The PR description shows the grep loop and its empty output for missing items.
  - [ ] `praytel.1.md`: Synopsis gains exactly the two lines; Description gains one sentence on `--` inside `test` and a link to praytel-test(1); `git diff main -- docs/reference/praytel.1.md | grep -c '^-[^-]'` prints `0`.
  - [ ] `docs/index.md`: Reference gains praytel-test(1) and praytest(5); Guides gains an item linking `guides/trace-tests.md`.
  - [ ] Every relative link in the three files resolves to an existing file, including `reference/praytest.5.md` and `guides/trace-tests.md` (the PR description shows the check's output).
Test plan: none (documentation). The grep checks above stand in for tests.
Benchmark: n/a
Notes: The pick keys and the confirm are user-visible behaviour the vision leaves open, so this page is where they are committed (D9 Consequences: Docs).

## T28: Record the S1 VHS demo
Implements: D9 (item 9 VHS), D11 (consequence: VHS is built by `demos.yml`)   Serves: M1, S1   Size: S
Depends on: T25, T2   Parallel-safe with: T6, T17, T22, T26, T30
Owns: `demo/scenarios/trace-tests/scenario.toml`, `scenario.tape`, `captions.txt` (new); `docs/assets/trace-tests.gif`, `docs/assets/trace-tests.mp4`, `docs/assets/trace-tests.inputs` (generated)
Contract: provides the asset paths `docs/assets/trace-tests.{gif,mp4}` that T29's guide links.
Goal: The repository convention gives every guide a recording (`demo/README.md`). This one is also M1's ≤ 60 s check, and it proves Ctrl-T/Ctrl-W reach praytel through a terminal.
Definition of done:
  - [ ] `scenario.toml` has `guide = "trace-tests"`, `workload = "shop"`, and the shop build stage copied from `demo/scenarios/traces/scenario.toml`.
  - [ ] The tape sources `/demo/common.tape`, runs `praytel test --record=gift.praytest.star -- shop -pace 0s -card gift`, types `Charge`, presses Ctrl-T, Enter, Space on `status` and `status_message`, `s` on `card.type`, then Ctrl-W, and waits with `Wait+Screen /wrote gift.praytest.star/`. It quits, runs `praytel test gift.praytest.star`, and waits with `Wait+Screen /1 case in 1 file passed/`.
  - [ ] `make docs/assets/trace-tests` exits 0 (needs Docker and Dagger). `ffprobe -v error -show_entries format=duration -of csv=p=0 docs/assets/trace-tests.mp4` prints a value ≤ 60.
  - [ ] `make docs-list` lists `docs/assets/trace-tests.gif`.
Test plan: e2e (the recording is a scripted run of S1 plus its acceptance). If the waits fail, the recording fails.
Benchmark: n/a (M1's 60 s is a DoD item)
Notes: If the implementer's environment has no Docker, item 3 becomes a human step before release (recordings are committed by hand before a release, `demo/README.md`). If VHS cannot deliver Ctrl-T/Ctrl-W, that triggers D9's "key interception" revisit (alias keys); do not work around it in the tape.

## T29: Write the trace-tests guide and mention it in the README
Implements: D1 (raw `status_message` note), D7 (exact not glob; `in`/`.get`; no vacuous always/never), D8 (delete per-run values; absence picks commented out), D2 (flush on exit; CI ports), D4 (dialect pointer)   Serves: S1-S4 walkthrough, M1, §7 v1 item 7, R4, R12   Size: S
Depends on: T25, T2, T28   Parallel-safe with: T6, T17, T22, T26, T30
Owns: `docs/guides/trace-tests.md` (new); `README.md`
Contract: consumes `docs/assets/trace-tests.{gif,mp4}` (T28); provides `docs/guides/trace-tests.md`, which T27's index links.
Goal: Take a new user from recording to a passing `praytel test` on shop in under 2 minutes (M1), and state up front the rules that otherwise surprise people.
Definition of done:
  - [ ] Frontmatter; the recording link `[![...](../assets/trace-tests.gif)](../assets/trace-tests.mp4)`, both targets existing; the `*.praytest.star` suffix rule in the first section (R12); sections for S1 (record), S2 (add the visa case), S3 (run a directory), S4 (always/never), and CI.
  - [ ] Each command's output block in the guide is byte-equal to what the command prints at HEAD against shop. The PR description has one transcript per block.
  - [ ] M1: a timed walkthrough of the guide on shop, from `praytel test --record=...` to a passing `praytel test`, by someone following only the guide. The PR description has the transcript with wall-clock start and end times, and the elapsed time is ≤ 120 s.
  - [ ] The guide states each of these: flush on exit and do not sample (R4, A4); `status_message` is the raw OTLP value, which can be empty where the TUI shows exception text (D1); selectors are exact, not globs as in `--filter` (D7); use `in`/`.get` in predicates (D7); assert an absent attribute with `expect.eq("k" in span.attrs, False)` (S1 edge case); always/never fail on zero matches (D7); delete per-run values from recorded files (D8); absence picks arrive commented out (D8); use `--grpc-port=0 --http-port=0` in CI (D2).
  - [ ] `README.md`'s Tl;dr gains bullets for `praytel test --record=FILE -- command` and `praytel test DIR`, linking the guide.
Test plan: none (documentation). The transcript checks stand in for tests, and the timed walkthrough is M1's measurement.
Benchmark: n/a (M1's 2 minutes is a DoD item, not a benchmark)
Notes: Keep the walkthrough's S1 steps identical to T28's tape, so the recording and the text match.

## T30: Make `trace-tests` a required check (PR 2) with the M2 evidence
Implements: D11 (items 3, 7 PR 2), A7   Serves: S5, M2, M7   Size: S
Depends on: T26, T6, T1   Parallel-safe with: T10, T12, T14, T17, T20, T22, T23, T25, T27, T28, T29
Owns: `.github/workflows/check.yml` (`trace-tests` job only); `CONTRIBUTING.md` (section "Required checks and trace-test evidence")
Contract: consumes `make trace-test` (T26) and `trace-nightly.yml` dispatch inputs (T6).
Goal: Gate `main` on the trace facts, but only after determinism has been shown on CI runners. Write down the steps a human must take, because an implementer cannot produce the evidence from a worktree.
Definition of done (implementer):
  - [ ] `check.yml` has a job `trace-tests` with `lint`'s steps plus `run: make trace-test`, `timeout-minutes: 15`, no `continue-on-error` and no path filter. A final `if: always()` step appends `trace-out/report.txt` in a code block to `$GITHUB_STEP_SUMMARY`, or `The trace tests could not run; see the log.` when the file is absent. `git diff main -- .github/workflows/check.yml` shows only added lines under `jobs:`, and the `golden` job is byte-identical.
  - [ ] `make zizmor` exits 0.
  - [ ] The `CONTRIBUTING.md` section lists, as commands: (1) after PR 1 merges, `gh workflow run trace-nightly.yml --ref main -f count=100` and `gh workflow run trace-nightly.yml --ref main -f count=100 -f gomaxprocs=1`; (2) both runs' `report.txt` must end `100 of 100 runs passed`, and any INCOMPLETE reasons are copied into the PR (D2 late-span trigger); (3) open PR 2 linking both run URLs and the nightly history since PR 1; (4) an admin marks `trace-tests` and `test` as required on `main` (A7); (5) if any evidence run failed, PR 2 stays unmerged until the cause is fixed and both runs pass again (D11 revisit).
Human release gate (not completable by an implementer; PR 2 must not merge until all hold):
  - [ ] PR 2's description links two `trace-nightly` dispatch runs on `main` of the same code, each ending `100 of 100 runs passed` (default, and `GOMAXPROCS=1`).
  - [ ] `gh api repos/{owner}/{repo}/branches/main/protection/required_status_checks` lists the contexts `trace-tests` and `test`.
Test plan: none (workflow config, checked by `make zizmor`). Its behaviour is proven by the human gate's 200 runs.
Benchmark: n/a (M2 is a count of runs, not a benchmark)
Notes: D11 item 7 words the pass line as `0 of 100 runs failed`, but D11 item 1's report format prints `N of N runs passed` when all pass. This plan uses the item 1 format. The PR changes no code that the evidence ran. A 130/143 can only occur on cancellation, which Actions already shows as cancelled.

---

## Revision log

### Round 1
Round 1 critic findings → changes. Task IDs T1-T17, T19, T20 and T22-T30 are unchanged. T18 is split into T18a/T18b and T21 into T21a/T21b. Tasks not named below changed only in their "Parallel-safe with" lists, which were recomputed from the new DAG.

| # | Finding | Change made |
|---|---|---|
| 1 | T27 links T22's and T29's files; T29 links T28's assets | Added edges T22 → T27, T29 → T27 and T28 → T29. T27 dropped from T22's and T29's parallel lists; T28 dropped from T29's. T27's DoD names both link targets; T22 and T29 Contracts name what T27 links. |
| 2 | No signal handling outside `runCase` (Prepare, checks, gaps) | **T24 Contract:** `runTest` holds `signal.Notify` for its whole duration and cancels with `context.WithCancelCause` and `testrun.Interrupted{Sig}`. **T21a Contract:** `rc` gets `context.WithoutCancel(ctx)`; a cancelled check whose cause is `Interrupted` is INTERRUPTED, later cases `not run`. Added T21a `TestRunInterruptDuringCheck`/`TestRunInterruptBeforeCase`, T21b golden `interrupted_during_check`, and T24 `TestPraytelTestInterruptDuringCheckExits130`. |
| 3 | The second-signal exit hard-codes 130/143 | Added `interruptExit(sig os.Signal) int` in T16's `isolate.go` as the single source. T16's `Exit`, `runTest` and `testrun.Run` (via a new `exitFor` parameter) all use it. T21a's fold rule makes the fallback a one-function change. T16's test asserts against `interruptExit(sig)`; `TestInterruptExit` is the only place with literals. The pending note now reads "The fallback changes `interruptExit` only". |
| 4 | The D12 S3 seam test lands before the builtins exist | `TestCheckReentrant` (T15) globs `testdata/reentrant/*.praytest.star`; T15 adds `pure.praytest.star`. `exist.praytest.star` is in T18b's Owns and DoD, and `props.praytest.star` in T19's. Both DoDs assert `reentrant_test.go` is unchanged since its merge-base. A rule line says that adding a fixture is not a test edit. |
| 5 | T23 never pins the Write summary that T14 relies on | Added T23 `TestWriteStatusIsSummary` with the two exact strings and the check that `Write` does not call `Generate`. T20 gains a pure `praytest.Summarize(rec) Summary` (with `TestSummarize`, which ties it to `Generate`'s Summary). T14's confirm-prompt test now asserts the exact prompt text. |
| 6 | The record entry in `cmd/praytel` contradicts D9 item 10 | `record.Run` is back in `internal/record` (T23), with the lifecycle injected as `record.RunFunc`; T25 only wires it. **One necessary adaptation:** `cfg args.Config` cannot compile, because Go's internal-package rule forbids `internal/record` from importing `cmd/praytel/internal/args`. So `record.Run` takes `record.Config{File, Argv, Stdin, Stdout}`, and D10 item 7's `args.Config` (TUI: true, root `MaxSpans`/`MaxLogLines` defaults) is built in T25 and captured by the closure. T25 DoD gains `TestRecordConfigFromTestConfig`. |
| 7 | `CaseRun.Started` and D10's seam-friction trigger | T21a Notes record why the trigger does not fire (testrun cannot re-derive the child's start time). The orchestrator logs it as a D10 item 4 contract amendment. |
| 8 | The tracing rule is checked only in T11 | Added a named-span DoD line to T5, T9, T12, T13, T15, T20, T21a, T21b, T23, T24 and T25. The rules section states the unexported-real-work clause and the exemption list. |
| 9 | Exported `record.CheckTarget` does I/O with no ctx/lt | Removed it. Replaced by unexported `checkTarget(ctx, lt, file)` (span `record.checkTarget`), called inside `record.Run`. |
| 10 | T18 too large | Split into T18a "Bind selectors and span values" (M) and T18b "Add the existence assertions and `run.one`" (S), with T18a → T18b → T19. |
| 11 | T21 borderline; M1 unchecked | Split into T21a "prepare, classify, fold" (M) and T21b "Report text and output goldens" (M), with T21a → T21b → T24. T29 DoD gains the timed walkthrough (≤ 120 s). |

### Round 2
Round 2 critic findings → changes. No task IDs, sizes or DAG edges changed.

| # | Finding | Change made |
|---|---|---|
| 1 | [T21a] An interrupt between cases could exit 0 | The fold now treats the run as interrupted when **any case is INTERRUPTED or `context.Cause(ctx)` is an `Interrupted` at the end of the loop**, then applies `exitFor(sig)` with the ≥ 128/max rule. `TestRunInterruptBeforeCase` asserts code `exitFor(SIGINT)` for both the 130 `exitFor` and the fallback `exitFor`. Added `TestRunInterruptBeforeFirstCase` (cause set before `Run`: every case `not run`, code `exitFor(SIGINT)`) and two `TestFold` rows. The summary gains an `interrupted between cases` item when no case was mid-run (planner wording; D10 item 5 leaves this unstated). T21b gains golden `interrupted_between_cases`; T27's Output section mentions the item. |
| 2 | [T24, T16] The second signal could exit before the group is killed | Option chosen: **exactly one handler exits**. T16's handler is registered before `cmd.Start` and stopped after the group sweep, and a new `caseOpts.OnGroupDone` fires after the sweep. `runTest` keeps `groupLive` (set by `OnStart`, cleared by `OnGroupDone`). While it is true, runTest's handler ignores the second signal and T16 kills `-pgid` and then exits. While it is false, runTest exits. Added T16 tests (`TestIsolateSecondSignalKillsGroupAndExits` now checks the grandchild is dead before `Exit`; `TestIsolateOnGroupDoneAfterSweep`) and T24 `TestPraytelTestSecondSignalKillsGroup`. T24 Owns gains child flags if needed. A new hard-ordering row records the rule. |
| 3 | Trace-golden Owns overlapped and were incomplete | Every task now owns only the goldens of its own tests, by test-name prefix, and prefixes no longer collide. T11's tests were renamed to `TestLoad*`/`TestSurface`, T17's to `TestHelper*`, T18a's to `TestBind*`, T18b's to `TestExist*`, and T19's to `TestCompare*`/`TestProps*`/`TestKinds`. Owns gained goldens for T5 (`TestExpand*`), T11, T12 (`TestWriteNew*`), T15 (`TestCheck*`), T17, T18a, T18b, T19, T20 (`TestGenerate*`/`TestSelfCheck*`/`TestM3*`), T21a (`TestPrepare*`/`TestRun*`), T21b (`TestReportGoldens*`), T22 (`TestDoc*`) and T23 (whole package). `TestCheckReentrant.golden` is re-stamped only along the chain T15 → T18b → T19. One procedure in "Rules" (stamp, `git add` own prefixes, `git checkout`/`git clean` the rest, grep own goldens) replaces every per-task golden instruction, and each tracing DoD greps only the task's own goldens. The "Owns checks" section lists the prefixes per package. |
| 4 | [T17] The helper loader does I/O with no span | T17 Contract adds unexported `loadHelper(ctx, lt, ...)` with span `praytest.loadHelper`. Its DoD greps `testdata/TestHelperLoadRelativeToLoadingFile.golden` for it, and Owns gains `testdata/TestHelper*.golden`. The test was renamed from `TestLoadRelativeToLoadingFile` so its prefix does not collide with T11's `TestLoad*`. |
| 5 | [T21a, plan] Unlogged changes to D10 item 4 | "Pending human items" now lists three D10 item 4 contract amendments for the orchestrator to log: `CaseRun.Started`, the `exitFor` parameter on `Run`, and the exported `testrun.Interrupted` type. T21a Notes give each reason (one source for the interrupt exit code; testrun cannot import `package main`; a cause type both packages can see). |

Re-verification after revision 3:
- **DAG:** acyclic, with the same edges and levels as revision 2.
- **Owns:** disjoint within every level and for every pair listed as parallel-safe, now including trace goldens. Prefixes are listed under "DAG → Owns checks". In the widest set {T2, T4, T5, T6, T8, T9, T10, T11, T12}, the only shared directories are `internal/praytest/testdata/` (T11 `TestLoad*`/`TestSurface*` vs T12 `TestWriteNew*`) and `cmd/praytel/` (T4's args/ErrMapper files vs T9's `run.go`/`runcase*`), with no shared file.
- **Coverage:** every decision D1-D12 and every v1 scenario S1-S5 still maps to at least one task.
- **Numbers:** critical path 14 tasks (unchanged); max parallel width 9 (unchanged); task count 32 (14 S, 18 M).
