# Vision: trace-based testing in praytel
Status: v5: amended from design decisions D2/D6/D8/D11, awaiting human confirmation     Brief: 00-brief.md     Evidence: 01-research/synthesis.md

Revision note: v4 applies the human's second set of binding changes. Tests run through a `praytel test` subcommand, which takes several files, globs or directories. Recording is `praytel test --record=FILE -- cmd`. Test files are named `*.praytest.star`. The v3 decisions still hold: each case owns its command, recording uses the existing trace TUI, and `trace-tests` is a required check from day one.

## 1. Problem

A passing test or a clean exit shows the answer was right. It does not show how the program got there. Praytel already shows the "how" live (`docs/guides/traces.md`: "It's easier to understand the apps you use"). But once you close the TUI, what you learned is gone. You can't say "for this input, this span must exist, that one must not, and no bank call may ever take over 200ms" and have a machine hold you to it.

- **Developers wrapping their own app** see the right behaviour in the TUI, for example "a gift-card order fails at `Gateway.Charge` with `unsupported card type: gift`", and can't keep it as a check. The only durable artifact today is a whole-tree golden (`--snapshot`, `docs/reference/praytel.1.md#snapshots`). It fails on any unrelated change, and its CI job "Never fails the run: a difference in a golden is for a person to read" (`.github/workflows/check.yml`, `golden` job). Nothing in praytel can fail a build on one specific trace fact.
- **Developers chasing an intermittent bug** know what must never happen ("the bank call never takes over 200ms"). Today they find out by staring at a live tree.
- **Evidence elsewhere.** Tracetest and Malabi both exist to assert on spans (synthesis P1, P2). Malabi is archived and Tracetest's status is unclear (synthesis §4). Both need a server or in-process instrumentation. No tool found records a CLI command plus its telemetry and emits a test (synthesis matrix, P3 row).

How much it hurts has not been measured (A1).

## 2. Users and jobs-to-be-done

| Role | Job | Context | Priority |
|---|---|---|---|
| **App developer using praytel locally** | "When I've just seen the right trace for some input, I want to pin it as a test without writing span-query code from scratch." | Already runs `praytel --tui -- ./thing`. Has OTel instrumentation. | **Primary** |
| **CI maintainer** (including praytel's own) | "When a PR changes what a command calls, I want a red build on the facts we agreed matter and nothing else." | Runs `make`/shell in CI. Cares about exit codes and no flakes. Knows `--snapshot`. | **Primary** |
| **Bug hunter** | "When a rare bad thing happens, I want to be told immediately and keep a failing test that shows it." | Long-running server or repeated test loops. | Secondary (v1.1) |
| **Reviewer of agent-written code** | "I want to state architectural rules (who may call whom) and have them checked." | Uses the graph view for this today (`docs/guides/overview.md`). | Secondary (stretch) |

## 3. Vision

A trace test is **inputs plus assertions**. The inputs are a command with its arguments and data. The assertions say what the resulting trace must contain, must not contain, and what must always or never hold. You run `praytel test --record=gift.praytest.star -- shop -card gift`. Your command runs exactly as it does under `--tui` today, and you watch its trace in the same TUI. You search for the spans you care about, mark which attributes identify them and which values to assert, and finish. Praytel writes one small Starlark file holding the command and the assertions. `praytel test testdata/traces` finds every `*.praytest.star` under that directory, reruns each case headless, checks each trace, reports per file and per case, and exits 0 or non-zero. That drops into `make` and CI the way `--snapshot` does. The file is code you own: add cases with other inputs, loops and helpers. Praytel's own CI requires these tests to pass on `main`. There's still no daemon, no server, no YAML and no embedded terminal.

## 4. Scenarios

All scenarios use the `shop` demo (`demo/workloads/shop/main.go`). By default it places orders A-1001..A-1004. In A-1002 (`amex`), `Gateway.Bank.Authorize` takes 450ms. In A-1003 (`gift`), `Gateway.Bank.Authorize` fails and `Gateway.Charge` (attribute `card.type`) ends with status error, message `unsupported card type: gift`. Its only flag today is `-pace`. **v1 adds `-card TYPE`**, which places one order (A-1001) with that card, so cases can vary input data (A5). Key bindings and builtin names are proposals. The behaviour is the commitment.

### Assertion model (core of v1)

A file declares one or more `test(...)` cases. Each case has inputs (`command`, required; `exit_code`, default 0; `timeout`, default `10m`) and a `check` function. Spans are chosen with a **selector**, `spans(service=, name=, attrs={})`. All fields are optional and all must match.

| Kind | Builtin | Fails when |
|---|---|---|
| Existence | `expect.exists(run, sel, count=None)` | 0 spans match (or not exactly `count`) |
| Absence | `expect.absent(run, sel)` | ≥1 span matches |
| Single span | `run.one(sel)` | 0 or >1 match. The rest of that check does not run, and the failure line says so; failures recorded before it are kept. |
| Comparison | `expect.eq/ne/lt/le/gt/ge(actual, want)` on `span.attrs[k]`, `status`, `status_message`, `duration_ms`, `service`, `name` | comparison false. An absent attribute is a failure, not a key error. |
| Always / never | `expect.always(run, sel, pred)`, `expect.never(run, sel, pred)` | some matching span has `pred` false (always) or true (never). Lists up to 10 offenders, then `... and N more`. |

`span.parent` and `span.trace` support structural rules (X1). `env(name, default)` reads the environment, so a case can say `command = [env("SHOP_BIN", "shop"), ...]` when CI uses a different binary path. Every `expect.*` records a failure and continues, without aborting (R1). Files that `load()` helpers name the helpers `*.star`, not `*.praytest.star`, so directory runs don't treat them as tests.

`command` runs in the directory `praytel test` was started from, with stdin closed. `command[0]` and any relative path argument are resolved from that directory, or through `PATH` if `command[0]` contains no `/`. A recorded file therefore replays from the directory it was recorded in; to run it from elsewhere, use a `PATH` name or `env()`.

### S1. Record a test from the trace TUI (v1)

1. `praytel test --record=gift.praytest.star -- shop -pace 0s -card gift`
   - If `gift.praytest.star` exists, praytel prints `praytel: gift.praytest.star exists; refusing to overwrite` to stderr, exits `1` and starts nothing.
   - If FILE doesn't end in `.praytest.star` (for example `--record=gift.star`), praytel prints `praytel: gift.star: recorded tests must be named *.praytest.star so directory runs find them`, exits `1` and starts nothing. It does not rename the file silently.
   - `praytel test --record=gift.praytest.star` with no `--` exits `1` (`--record needs a command after --`). Giving file arguments as well as `--record` also exits `1`.
2. The command runs as under `praytel --tui -- ...` today, with the same environment and the same trace TUI. The status line adds `RECORD gift.praytest.star · Ctrl-T pick · Ctrl-W write`.
3. The user types `Charge` to search (today's type-to-search), then presses **Ctrl-T** (pick mode) and moves to `Gateway.Charge`, then presses Enter. A panel lists `status = error`, `status_message = "unsupported card type: gift"` and `card.type = "gift"`. Duration, timestamps and IDs are shown but cannot be ticked, nor can attributes on praytel's volatile list (ports, PIDs, and ID-, time- or temp-path-shaped values; listed in `praytel-test.1`).
4. Space ticks `status` and `status_message` as **assertions**. `s` marks `card.type` as part of the **selector**. `a` on another span adds a commented-out **absence** assertion with that span's selector, for the user to edit and uncomment (the span exists in this run, so the assertion would fail as recorded).
5. Ctrl-W writes the file and the status line reads `wrote gift.praytest.star (1 case, 2 assertions)`. Quitting without Ctrl-W writes nothing.

```python
# praytel test format v0. Recorded with: praytel test --record=gift.praytest.star -- shop -pace 0s -card gift
# Starlark (Python-like). Edit freely. Run: praytel test gift.praytest.star
CHARGE = spans(service = "payments", name = "Gateway.Charge")

def check_gift(run):
    span = run.one(CHARGE.where(attrs = {"card.type": "gift"}))
    expect.eq(span.status, "error")
    expect.eq(span.status_message, "unsupported card type: gift")

test(name = "gift", command = ["shop", "-pace", "0s", "-card", "gift"], check = check_gift)
```

Acceptance: `command` equals the argv after `--`, unchanged. The file parses. It contains no trace/span IDs, timestamps or durations, and no attribute value on the volatile list. It passes `praytel test gift.praytest.star` on unchanged `shop`.

**Recording edge cases (v1):**
- **Selector matches many spans.** Ctrl-W refuses with `selector matches 2 spans in this run; mark an attribute with s to pick one`.
- **Command never exits.** Picking and Ctrl-W work while the child runs. The file gets `# WARNING: command was still running when recorded; praytel test waits for it to exit (timeout 10m).` Acceptance: the warning appears if and only if the child was running at write time.
- **Missing attribute.** Absence can't be ticked from an attribute list. To assert that an attribute is absent, the user edits the file; the guide shows how.
- **Refuse to overwrite, wrong suffix**: step 1.

### S2. Several inputs, existence and absence, one file (v1)

The user adds a second case to `gift.praytest.star`:

```python
def check_visa(run):
    expect.exists(run, CHARGE.where(attrs = {"card.type": "visa"}), count = 1)
    expect.absent(run, spans(service = "payments", status = "error"))

test(name = "visa", command = ["shop", "-pace", "0s", "-card", "visa"], check = check_visa)
```

`praytel test gift.praytest.star` runs each case's command in turn, headless, waits for its drain and runs its check. Child stdout/stderr are captured, as with `--snapshot`.

**Passing.** stdout shows the following, and praytel exits `0`:
```
gift.praytest.star
  ok   gift
  ok   visa
praytel: 2 cases in 1 file passed
```

**Failing.** If `shop` is edited so visa charges also call the failing `Gateway.Bank.Authorize`, praytel exits `4`:
```
gift.praytest.star
  ok   gift
  FAIL visa
    gift.praytest.star:13:5: absent payments{status="error"}: 1 span matched: payments/Gateway.Bank.Authorize in trace web/Orders.Create{order.id="A-1001"}
praytel: 1 of 2 cases in 1 file failed
```

**Missing attribute.** `expect.eq(span.attrs["payment.result"], "ok")` on a span without it fails as `attribute payment.result is absent on payments/Gateway.Charge` at file:line:col. Exit `4`, not `1`.

**Conflict.** `praytel test gift.praytest.star -- ./bin/shop` exits `1` without starting anything: `praytel: test files take no command; the cases in gift.praytest.star declare their own`.

### S3. Many files: globs, directories, de-duplication (v1)

Input rules for `praytel test ARG...` (without `--record`):

- **Literal file.** Used as given. It must exist and end in `.praytest.star`, or exit `1`.
- **Directory.** Every regular file ending in `.praytest.star` under it, recursively. Directories whose names start with `.` are skipped, and symlinked directories are not followed. Sorted by path, bytewise.
- **Glob.** An argument containing `*`, `?` or `[` that is not an existing path is expanded by praytel with Go `filepath.Glob` rules (no `**`; pass a directory to recurse). Matched files must end in `.praytest.star`, and matched directories are expanded as above. Results are sorted. Quoted `'testdata/traces/*.praytest.star'` therefore gives the same set in bash, zsh, `sh -c` and a CI YAML `run:` line as the unquoted, shell-expanded form.
- **Order.** Arguments are expanded left to right, each in sorted order. **Duplicates** (same cleaned absolute path) run once, at their first position.
- **Nothing matched.** A glob or directory that yields no `.praytest.star` file exits `1`: `praytel: 'testdata/none/*.praytest.star' matched no .praytest.star files`. So does `praytel test` with no arguments: there is no implicit current-directory run.
- **All-or-nothing usage errors.** All arguments are expanded and every file is parsed before any child starts. Any exit-`1` condition in any file means no child runs.
- **Execution.** Files run one after another in that order, and cases in file order. No parallelism.

Given `testdata/traces/shop.praytest.star` (S2 cases) and `testdata/traces/perf/props.praytest.star` (S4), plus a helper `testdata/traces/lib.star`:

`praytel test testdata/traces testdata/traces/shop.praytest.star` runs `perf/props.praytest.star` then `shop.praytest.star`, each once, and does not run `lib.star`. If `props` fails as in S4, praytel exits `4`:
```
testdata/traces/perf/props.praytest.star
  FAIL all_cards
    testdata/traces/perf/props.praytest.star:4:5: never Gateway.Bank.Authorize: tripped by ...
testdata/traces/shop.praytest.star
  ok   gift
  ok   visa
praytel: 1 of 3 cases in 2 files failed
```

**Overall exit is the worst result across all files and cases**, using one table:

| Case | Exit |
|---|---|
| Every case in every file: child exit equals `exit_code`, trace complete, all assertions pass | `0` |
| **(Pending human confirmation; fallback: exit 3 with INTERRUPTED in the report.)** praytel received SIGINT or SIGTERM during a file run. The current case is drained and printed as `INTERRUPTED`, later cases are printed as `not run`, and the summary prints. Takes precedence over `4` and `3`. | `130` (SIGINT) or `143` (SIGTERM) |
| Any assertion or selector cardinality fails, a Starlark runtime error occurs in a check, or a child exited before its `timeout` with a code different from `exit_code` (captured output on stderr). Takes precedence over `3`. | `4` |
| No case failed but some case's trace was incomplete under the `--snapshot` rule, its `timeout` was hit (timeout covers the child, drain and check), or its OTLP ports could not be bound after earlier cases had run. That check is not run and is printed as `INCOMPLETE`. | `3` |
| Missing file, wrong suffix, glob/directory matched nothing, no arguments, syntax error, no `test(...)`, a case without `command`, `--` given with file arguments, or combined with `--tui`/`--snapshot` | `1`, no child started |

### S4. Always and never across matching spans (v1)

```python
BANK = spans(name = "Gateway.Bank.Authorize")

def check_props(run):
    expect.never(run, BANK, lambda s: s.duration_ms > 200)
    expect.always(run, CHARGE, lambda s: "card.type" in s.attrs)

test(name = "all_cards", command = ["shop", "-pace", "0s"], check = check_props)
```

`praytel test props.praytest.star` exits `4`:
```
props.praytest.star
  FAIL all_cards
    props.praytest.star:4:5: never Gateway.Bank.Authorize: tripped by payments/Gateway.Bank.Authorize (452ms) in trace web/Orders.Create{order.id="A-1002"}
praytel: 1 of 1 cases in 1 file failed
```
The `always` line passes and prints nothing. A predicate over the Starlark step cap fails with `exceeded step limit` (exit `4`) and can't hang CI. Acceptance: once A-1002 is fast, exit `0`.

### S5. Praytel's own CI requires `praytel test` (v1)

`testdata/traces/shop.praytest.star` holds the S1, S2 and S4 cases, with the expectations flipped to match today's `shop`. `make trace-test` runs `praytel test --grpc-port=0 --http-port=0 testdata/traces` once, inside the Dagger module's pinned Go container with `praytel` and `shop` on `PATH`, like every other check target. `make trace-test COUNT=N` repeats it N times for M2 and the nightly run. A new `trace-tests` job in `.github/workflows/check.yml` runs it and is a **required status check on `main` from the day it merges**. Acceptance:
- A PR that makes A-1002's bank call fast turns `trace-tests` red at the `never` assertion, with the S4 output in the log, and can't merge.
- A PR that adds `testdata/traces/foo.praytest.star` gets it run with no Makefile or workflow change.
- The PR that adds the `trace-tests` job links M2's 100-run evidence: two `trace-nightly` runs on `main` of the same code (count 100 at default settings, and count 100 with `GOMAXPROCS=1`), made after the target and the nightly workflow merged in an earlier PR.
- The `golden` job stays non-failing and unchanged.

### S6. Live property alert with repro test (v1.1, the unvalidated bet)

`praytel test --watch=props.praytest.star --repro-dir=testdata/repros -- shop` evaluates each `always`/`never` from `props.praytest.star` as each span closes. On the first violation per property, stderr shows:
```
praytel: VIOLATION never Gateway.Bank.Authorize (props.praytest.star:4) payments/Gateway.Bank.Authorize 452ms
praytel: wrote testdata/repros/bank_never_1.praytest.star
```
Later violations are counted, not written. The child keeps running, and the exit code is the child's. The repro is a `test(...)` with the watched command, and `praytel test testdata/repros` exits `4` today and `0` once fixed. `--watch` follows `--record`'s rules: it needs `--`, takes no file arguments, and its FILE must end in `.praytest.star`.

### Stretch (not committed)
- **X1.** Architecture rules: `expect.never(run, spans(service = "payments"), lambda s: s.parent != None and s.parent.service == "web")`.
- **X2.** Tighten a `--snapshot` diff into one check for the line you care about.

## 5. Fit with the existing product

- **`test` is praytel's first subcommand.** The synopsis (`praytel.1.md#synopsis`) gains two lines next to `praytel [flags] -- command [args ...]`:
  ```
  praytel test [flags] FILE|DIR|GLOB ...
  praytel test [flags] --record=FILE -- command [args ...]
  ```
  **Rule:** `test` is the subcommand only when it is the first argument. Flags for `test` (`--record`, `--grpc-port`, `--http-port`, `--drain-idle`, `--drain-timeout`) come after it. `praytel --grpc-port=0 test x.praytest.star` exits `1` with the hint `put flags after "test"`.
- **No existing invocation changes meaning.** Today the `--` separator "is required" (`praytel.1.md#description`), so `praytel test ...` without `--` is already a usage error. Anything after `--` is the user's command, so `praytel -- test -f go.mod` still runs `/usr/bin/test` exactly as today.
- **The `--` rule is kept inside `test`.** `--record` (and v1.1 `--watch`) run a command and need `--`. File runs don't, because the cases own their commands, so `--` with file arguments is a usage error. The man page gets one sentence on this.
- **Recording is today's TUI** (`praytel.1.md#tui-keys`) with a status-line addition, Ctrl-T pick mode layered on type-to-search, and Ctrl-W. There is no new TUI surface beyond the pick panel. A command that works under `--tui` today records the same way.
- **Completeness and exit `3`** reuse the `--snapshot` rule (`praytel.1.md#snapshots`). The only new code is `4`.
- **Ports and drain.** `--grpc-port=0`, `--drain-idle` and `--drain-timeout` mean the same inside `test`. Each case's child gets the same environment as today's child. Each case binds its own receiver. Use `--grpc-port=0 --http-port=0` for isolation in CI.
- **Pipeline.** File runs evaluate after `DrainComplete`, and `--watch` on `SpanClosed` (`design/ARCHITECTURE.md#join`). The record TUI is one renderer, respecting "one renderer per process".
- **Selectors** use service + span name, the graph's box identity (`docs/guides/overview.md#where-the-boxes-come-from`).
- **CI.** A job next to `golden` in `check.yml` and a `make` target, like `make golden-check`.

Where it could feel inconsistent: the exit code is the verdict, not the child's code. `--snapshot` set this precedent with `3`. A child exit mismatch is a test failure (`4`), because a run can start many children. Having one subcommand next to a flags-only surface is a new shape, but `praytel [flags] -- cmd` stays the default and doesn't change.

What an existing user must relearn: nothing. `test` is opt-in, and no existing flag, key, output or exit code changes.

## 6. Informed by prior art

| Pattern (synthesis §1) | Decision | Why, for praytel's users |
|---|---|---|
| P1 selector + assertion list (Tracetest) | **Adopt the shape, reject the medium** | Select spans, then assert: existence, absence, always and never all hang off one selector. We reject YAML and Tracetest's expression DSL (synthesis matrix). Starlark gives operators, lambdas and loops. Praytel owns the receiver, so there's no backend polling (synthesis §3.1). |
| P2 in-process assertions (Malabi, Go `tracetest`) | **Adopt "tests are code", reject in-process** | Our users wrap arbitrary commands in any language. Malabi's abandonment shows the cost of tying a tool to one runtime. |
| P3 record-and-generate (Playwright codegen, Cypress Studio, Jest) | **Adopt explicit pick, reject whole-output capture and `-u`** | `--snapshot` already covers whole output. Playwright's assert mode, where you pick what to assert, is the analogue, and the trace TUI plays the inspector's role. The generator never emits IDs, timestamps or durations, and refuses attribute values on its documented volatile list; app-specific per-run values are left to the author. There's no bless flag (synthesis §3.3-3.4). |
| `go test ./...`-style path arguments | **Adopt file/dir/glob arguments and a fixed suffix, reject the rest of a runner** | A suffix plus a directory argument means a new test file needs no CI edit (S5). Expanding globs ourselves gives one result in every shell. Parallel scheduling, filters and caching are runner features we don't need for a handful of files. |
| Tracetest inputs/outputs | **Adopt inputs, defer outputs** | Inputs are per-case commands. Chaining outputs between cases needs a scheduler, which is a non-goal. |
| P4 periodic query-and-alert (Honeycomb, Tempo) | **Reject the mechanism** | Praytel is live and in-memory, so it evaluates on `SpanClosed`. Grafana steers alerting away from traces (synthesis §4), so watch is v1.1, gated on M5. |
| P5 property/invariant (Hypothesis, rapid) | **Adopt always/never and failure-to-artifact, reject shrinking** | `always`/`never` correspond to invariants, and a repro file to an `@example` pin. Inputs to an external process can't be shrunk. |
| Starlark (mandated) | **Adopt, design around gaps** | Step cap and `Cancel` bound runaway checks. `expect.*` record and continue, because there's no try/except (R1-R3). |

## 7. Scope

**v1 (must)**: record, run many files, edit, required in CI.
1. `praytel test FILE|DIR|GLOB ...`: the S3 input rules (suffix, recursive sorted directories, praytel-side glob expansion, de-duplication, nothing-matched error, parse-all-first), sequential execution, per-file and per-case report, and the single worst-result exit table. (S2, S3)
2. The file format: per-case `test(name, command, exit_code, timeout, check)`, selectors, `run.one`, `expect.exists/absent/eq/ne/lt/le/gt/ge/always/never`, `span.parent`/`trace`, `env()`, `load()` of relative `.star` files. (S2, S4)
3. `praytel test --record=FILE -- cmd...`: today's trace TUI plus pick mode (assert / selector / absent), Ctrl-W writes the command from `--` and the picks, refusal to overwrite, refusal of a FILE not ending in `.praytest.star`, and the S1 edge cases. (S1)
4. Failures carry file:line:col and a span description. Absent attributes and cardinality problems are test failures. (S2)
5. The subcommand rule (`test` only as the first argument, flags after it) and the usage errors in S1-S3. (S1-S3)
6. `testdata/traces/*.praytest.star`, `make trace-test`, a `trace-tests` job **required on `main`**, and `shop -card`. (S5)
7. Docs: `docs/reference/praytel-test.1.md` for the subcommand and `docs/reference/praytest.5.md` for the file format (CONTRIBUTING.md: "one file per command or configuration surface"), and `docs/guides/trace-tests.md` (S1-S4 on `shop`).

**v1.1 (committed)**: `praytel test --watch=FILE --repro-dir=DIR -- cmd` (S6), gated on M5 and reusing v1's evaluator and format.

**Later (could)**: assertions on logs and captured stdout/stderr; selecting cases with `--case=NAME`; span events and links in selectors.

**Explicit NON-GOALS**
- **`praytel test` is not a full test runner.** It runs the files it was given, in a fixed order, one case at a time. v1 has no parallel scheduler, no filtering by case or tag (`--case` is Later), no rerun-on-save, no config file listing test paths, no `**` globs, and no implicit run of the current directory.
- **Praytel does not embed a terminal.** You give the command on the command line, after `--`. Recording doesn't capture shell input or host an interactive shell.
- **No command override at test time.** A file's cases own their commands. Use `env()` or `PATH` in the file for per-environment differences.
- **No YAML, JSON or TOML test definitions**, not even as export.
- **No `--update` / bless flag.** Re-record or edit. `make golden` serves "accept everything".
- **No request driving.** Praytel won't send HTTP or gRPC. The case's `command` is the driver.
- **No retries of failed cases**, in praytel or in CI (R7).
- **No persisted traces or offline replay.** `praytel test` always reruns the inputs (`ARCHITECTURE.md`: "No spill files").
- **No sandbox for test files** (trusted like a Makefile), no metrics assertions, and no input generation or shrinking.

## 8. Success measures

| # | Measure | Target | How measured |
|---|---|---|---|
| M1 | New user, following the guide, from `praytel test --record` to a passing `praytel test` | ≤ 2 min. The VHS recording (`make docs`) fits S1 in ≤ 60s. | Timed guide walkthrough |
| M2 | Determinism (**day-one requirement**, since the job is required) | 0 failures in 100 consecutive `make trace-test` runs, with default settings and `GOMAXPROCS=1`, in the PR that adds the job. A nightly scheduled run repeats 20× at default settings and 20× with `GOMAXPROCS=1` and must stay green. A failing nightly opens or updates a `trace-nightly` issue. | CI loop and nightly history |
| M3 | Volatile values in generated files | 0 trace/span IDs, timestamps or durations (structural: the generator has no input for them); 0 attribute values on the documented volatile list | Generator test over every `shop` span, plus one test per volatile-list entry |
| M4 | `praytel test` overhead over `praytel -- cmd` | ≤ 50ms per case at 10k spans × 20 assertions. Input expansion ≤ 50ms for a tree of 1,000 files. | `make bench-diff` |
| M5 (v1.1 gate) | Live property cost | p99 ≤ 20µs per property per span. No blocked pipeline at 10k spans/s × 10 properties. | Correlator benchmark |
| M6 | Failure clarity | 100% of failures have file:line:col. Every failure about spans names its selector or span (`svc/name`); comparison failures name the compared expression as written. 0 raw stack traces. | Golden tests of failure output |
| M7 | Dogfooding | 0 flaky `trace-tests` failures on `main` in the first month. ≥ 1 real regression caught in the first quarter. | CI history |
| M8 | Shell independence | The same file list and order for quoted and unquoted globs under bash, zsh and `sh` | Table test in CI |

## 9. Risks

| # | Risk | Mitigation / acceptance |
|---|---|---|
| R1 | **Starlark has no try/except.** | `expect.*` record and continue, with each case in its own thread. Collecting multiple failures this way is untested (synthesis §5), so it gets a **design spike** first. |
| R2 | **Starlark is not a sandbox.** | Non-goal. Step cap and `Cancel` on `timeout` cover CPU. The only I/O builtin is the read-only `env()`. Stated in the reference. |
| R3 | **go.starlark.net has no tagged releases.** | Pin a pseudo-version, isolate it in one package and golden-test the builtin surface. |
| R4 | **Late spans or sampling** (synthesis §3.1-3.2). | Reuse `--snapshot` completeness and exit `3`. The guide says to flush on exit and not sample. |
| R5 | **Brittleness from renames and pinned values.** | Selectors never use IDs. Failures name the selector, so a rename reads as "0 spans matched". |
| R6 | **A child exiting `3`/`4`.** | Not ambiguous: the child's code is compared to `exit_code` and reported in the verdict. |
| R7 | **A required job that flakes blocks every praytel PR.** | M2's 100-run evidence before merge, plus the nightly repeat. Cases use `shop -pace 0s`. Timing assertions need ≥ 2× margin (450ms vs 200ms in S4). INCOMPLETE (exit `3`) fails the job, so a slow drain shows up rather than hiding. No retries: a flaky case is fixed or deleted in a PR that unblocks `main`, and the nightly run catches drift before it reaches contributors. |
| R8 | **Scope creep** toward "Tracetest in a terminal" or a general test runner. | The §7 non-goals. Accepting many paths is the whole of the runner. |
| R9 | **Builtins become a public API.** | Keep the v1 surface to the assertion-model table plus `env()`. Mark the format `v0` until M7 holds. Generated files carry a format comment. |
| R10 | **A first subcommand invites more, and argument parsing gets ambiguous.** | The fixed rule: `test` only as the first argument, flags after it. Anything after `--` is never parsed. Future subcommands must follow the same rule. |
| R11 | **Sequential runs get slow as files grow.** | Accepted for v1: praytel's own suite is a few cases. Parallelism is a non-goal until M4 shows real suites hurting. Each case's ports are already independent (`--grpc-port=0`), so adding it later isn't blocked. |
| R12 | **A file without the suffix is silently skipped by a directory run.** | `--record` refuses other names, and a literal non-suffix file is an error, not a skip. The guide states the suffix rule up front. |

## 10. Assumptions and open questions

- `ASSUMPTION A1:` Users want narrow, failing checks in addition to goldens. **If wrong**, the feature duplicates `--snapshot` plus `diff`, and we'd stop after S2.
- `ASSUMPTION A2 (unvalidated, no prior art):` Live violation → generated repro (S6) is useful (synthesis §5). **If wrong**, v1.1 costs an event consumer for nothing. It's sequenced after v1 and gated on M5.
- `ASSUMPTION A3:` Host builtins that record and continue give "report all failures" up to the first failed `run.one`, runtime error or step-limit hit in a case. **If wrong**, only the first failure per case is reported. Worse, but acceptable.
- `ASSUMPTION A4:` OTel SDKs under praytel export every span, with no sampling. **If wrong**, tests flake. We'd add the hint `0 spans matched; is the app sampling?`.
- `ASSUMPTION A5:` Adding `-card TYPE` to `shop` is acceptable. The default four orders don't change, so existing goldens are unaffected. **If wrong**, the scenarios need another workload that varies its input.
- `ASSUMPTION A6:` A small read-only `env(name, default)` builtin is an acceptable way for cases to vary per environment, as the human suggested. **If wrong**, cases rely on `PATH` alone and `env()` is dropped from v1.
- `ASSUMPTION A7:` Someone with admin rights on the repository will mark `trace-tests`, and the new `test` job that runs `go test -race ./...`, as required in branch protection when the job merges. A workflow file can't do this. **If wrong**, the job runs but doesn't gate, and S5's "can't merge" acceptance fails.
- `ASSUMPTION A8:` Go `filepath.Glob` rules (no `**`) are enough, because a directory argument already recurses. **If wrong**, users who write `'**/*.praytest.star'` get a nothing-matched error, and we'd add `**` support.
- `ASSUMPTION A9:` Requiring flags after `test` (not before it) won't confuse users who are used to `praytel [flags] -- cmd`. **If wrong**, users hit the hint error, and we'd accept global flags in either position.

- `QUESTION Q1:` The human wrote `record=FILE`. This draft reads it as the `--record=FILE` flag of the `test` subcommand: `praytel test --record=FILE -- cmd`. Is that right, or did you mean a separate `praytel record FILE -- cmd` subcommand?
- `QUESTION Q2:` The human wrote `*_.praytest.star`. This draft treats the underscore as a typo and uses `*.praytest.star` (for example `gift.praytest.star`). Is that right, or must names have the form `name_.praytest.star`?

## Changelog (v5)

| # | Location | Before -> after | Source |
|---|---|---|---|
| 1 | Header, Status | "revised v4, awaiting human confirmation" -> "v5: amended from design decisions D2/D6/D8/D11, awaiting human confirmation" | D2/D6/D8/D11 |
| 2 | §4 Assertion model, `run.one` row, "Fails when" | "Later expectations on it are skipped." -> "The rest of that check does not run, and the failure line says so; failures recorded before it are kept." | D6 |
| 3 | §4 Assertion model, new paragraph after the `env()`/`load()` paragraph | (none) -> cwd/stdin rule: `command` runs in the invocation directory with stdin closed; relative paths resolve from there or via `PATH` | D2 |
| 4 | S1 step 3, last sentence | "Duration, timestamps and IDs ... cannot be ticked." -> adds "nor can attributes on praytel's volatile list (...; listed in `praytel-test.1`)" | D8 |
| 5 | S1 step 4, last sentence | "`a` ... adds an absence assertion ..., for the user to edit." -> "adds a commented-out absence assertion ..., for the user to edit and uncomment (...would fail as recorded)" | D8 |
| 6 | S1 example, line 1 | "# praytel test. Recorded with: ..." -> "# praytel test format v0. Recorded with: ..." | D8 |
| 7 | S1 Acceptance | "no trace/span IDs, timestamps, durations or ports" -> "no trace/span IDs, timestamps or durations, and no attribute value on the volatile list" | D8 |
| 8 | S3 exit table, new row above `4` (**pending human confirmation; fallback: exit 3 with INTERRUPTED in the report**) | (none) -> SIGINT/SIGTERM during a file run: current case `INTERRUPTED`, later cases `not run`, summary prints; exit `130`/`143`, precedence over `4` and `3` | D2 |
| 9 | S3 exit table, row `4` | "or a child exit differs from `exit_code`" -> "or a child exited before its `timeout` with a code different from `exit_code`" | D2 |
| 10 | S3 exit table, row `3` | "or its `timeout` was hit" -> "its `timeout` was hit (timeout covers the child, drain and check), or its OTLP ports could not be bound after earlier cases had run" | D2 |
| 11 | S5, first paragraph | "`make trace-test` runs ...: one invocation, no shell loop." -> "... once, inside the Dagger module's pinned Go container ...; `make trace-test COUNT=N` repeats it N times for M2 and the nightly run." | D11 |
| 12 | S5 acceptance, third bullet | "includes M2's 100-run evidence" -> "links M2's 100-run evidence: two `trace-nightly` runs on `main` (default and `GOMAXPROCS=1`, count 100 each), after the target and nightly workflow merged earlier" | D11 |
| 13 | §5 "Ports and drain" | (none) -> appended "Each case binds its own receiver. Use `--grpc-port=0 --http-port=0` for isolation in CI." | D2 |
| 14 | §6 P3 row | "The generator never emits volatile values." -> "never emits IDs, timestamps or durations, and refuses attribute values on its documented volatile list; app-specific per-run values are left to the author." | D8 |
| 15 | §8 M2 target | "A nightly scheduled run repeats 20× and must stay green." -> "... 20× at default settings and 20× with `GOMAXPROCS=1` ...; a failing nightly opens or updates a `trace-nightly` issue." | D11 |
| 16 | §8 M3 row | target "0 IDs, timestamps, durations or ports" / measure "Generator test over every `shop` span" -> target "0 trace/span IDs, timestamps or durations (structural ...); 0 attribute values on the documented volatile list" / measure "... plus one test per volatile-list entry" | D8 |
| 17 | §8 M6 target | "100% of failures have file:line:col and a span description." -> "100% of failures have file:line:col. Every failure about spans names its selector or span; comparison failures name the compared expression as written." | D6 |
| 18 | §10 A3 | "... give \"report all failures\"." -> "... give \"report all failures\" up to the first failed `run.one`, runtime error or step-limit hit in a case." | D6 |
| 19 | §10 A7 | "will mark `trace-tests` as required" -> "will mark `trace-tests`, and the new `test` job that runs `go test -race ./...`, as required" (D11 marks this "proposed, the human's call") | D11 |
