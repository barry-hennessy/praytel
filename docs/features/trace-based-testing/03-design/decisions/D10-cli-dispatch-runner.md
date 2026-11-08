# D10: How is `praytel test` dispatched and parsed, and how are the runner and report composed in cmd/praytel?
Status: decided     Depends on: D2, D5, D6     Parent: none
Vision refs: §5 subcommand rule, "No existing invocation changes meaning", "--" rule; S1 step 1 usage errors; S2 output and conflict; S3 input rules, output, exit table; §7 v1 items 1, 5; R10, R11, R12; A8, A9; M4, M8

## Context
- `main` calls `runProcess` → `args.Parse` → `run` (`cmd/praytel/main.go:63-75`). `args.Parse` splits on the first `--` and requires a command (`cmd/praytel/internal/args/args.go:87-97,165-173`); every malformed invocation returns one `ErrUsage` whose single usage line `ErrMapper` prints (`cmd/praytel/internal/args/args.go:17-19`; `cmd/praytel/main.go:36-61`). The vision needs many specific `praytel: ...` messages, all exit `1`, plus the hint `put flags after "test"` for `praytel --grpc-port=0 test x`.
- Rule: `test` is the subcommand only as the first argument; `praytel -- test -f go.mod` still runs `/usr/bin/test`. Two forms: file mode (`FILE|DIR|GLOB ...`, `--` forbidden) and record mode (`--record=FILE -- cmd`, file args forbidden). `--tui`/`--snapshot` with `test` are exit `1`. v1.1 adds `--watch=FILE --repro-dir=DIR -- cmd` with record-like rules (S6).
- Flags inside `test`: `--record`, `--grpc-port`, `--http-port`, `--drain-idle`, `--drain-timeout` share meaning with the root flags (`cmd/praytel/internal/args/args.go:105-110`).
- Input expansion (S3) is specified precisely: literal/dir/glob, praytel-side `filepath.Glob`, recursive sorted dirs skipping dot-dirs and symlinked dirs, dedupe by cleaned absolute path, nothing-matched error, no implicit cwd. M8 requires identical results quoted vs unquoted under bash/zsh/sh; M4 ≤ 50ms for 1,000 files.
- Report: per-file header, `ok`/`FAIL`/`INCOMPLETE` per case, failure lines from D6, summary line; exit is the worst result (S3 table). Captured child output goes to stderr on exit mismatch (S3). Whether the report streams per case or prints at the end is unstated.
- Where the runner lives (in `cmd/praytel` beside `run.go`, or an `internal` package) decides its testability and whether the tracing rule applies (`CONTRIBUTING.md:16-45`). Existing end-to-end tests run the built binary (`cmd/praytel/snapshot_test.go:10-27`).
- R10: future subcommands must follow the same rule.

## Goals
- Every existing argv keeps its meaning and output.
- Every usage error is precise, on stderr, exit `1`, before any child starts.
- The parser shape admits `--watch` and later subcommands without new ambiguity.
- Runner and report are testable in-process with golden output.

## Non-goals
- `--case` filtering, config files, `**` globs, parallel runs, implicit cwd run (§7 non-goals).
- Changing root flags' parsing or the root usage line.

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| Backward compatibility of every existing argv | 25 | §5 "No existing invocation changes meaning" |
| Precise usage errors, exit 1, before any child | 20 | S1-S3 usage errors; §7 v1 item 5 |
| Testability: golden report, M8 table test | 15 | M6, M8 |
| Clear package boundaries (thin `cmd`, logic in `internal`) | 15 | ARCHITECTURE Shape; CONTRIBUTING tracing rule |
| Extensible to `--watch` and future subcommands | 15 | R10; S6 |
| No new CLI dependencies; consistent with stdlib `flag` | 10 | R3 spirit (dependency risk); `args.go` uses `flag` |

## Options
Full text in `03-design/options/D10-{A,B,C}.md`.

### Option A: One branch in `runProcess`, the whole runner in `cmd/praytel/test.go` (minimal change)
`argv[0]=="test"` → `runTest` in `package main`: its own `flag.FlagSet`, expansion, load-all, loop calling `runCase` directly, streamed report, worst-exit fold. `internal/args` unchanged; a `usageErr` string type in `main`. Cost S, easy to reverse.

### Option B: Subcommand registry, typed usage errors, event-driven Reporter (most extensible)
`registry[argv[0]]` before root parsing; new packages `internal/usage`, `internal/testargs`, `internal/testrun`; `Run(ctx, files, Executor, Reporter)` with a four-method `Reporter` interface and one text implementation; a reserved `Watch` mode. Cost M, easy to reverse.

### Option C: Go-tool dispatch, `internal/testrun`, streaming reporter (most conventional)
`argv[0]=="test"` switch; `args.ParseTest` with a shared `registerRun(fs)`; `args.UsageError{Msg}`; `internal/testrun` with `Expand`, `Run(..., RunCaseFunc, Reporter)` and a `Reporter` interface with one `TextReporter`; `Summary.Code()`. Cost M, easy to reverse.

### Verified claims
- **(1) No existing argv changes meaning: confirmed.** If `argv[0]=="test"`: with no `--`, `splitDashDash` fails and `Parse` returns `ErrUsage` (`args.go:90-93`). With a `--`, the flag part starts with the non-flag `test`, so `fs.Parse` stops immediately, `NArg()==1`, and `Parse` returns `ErrUsage` (`args.go:118-124`). Every argv beginning with `test` exits 1 today, so claiming it changes no working invocation. `praytel -- test -f go.mod` has `argv[0]=="--"` and never reaches the branch. All three options are equally safe on this point. B's and C's hint triggers are imprecise, though. C's "no `--` and `test` appears anywhere" fires on `praytel --snapshot test x`, where `test` is the flag's value. B's "registered name before `--`" fires on `praytel --snapshot test -- cmd`, which today reports `unknown --snapshot view "test"`. B's trigger would therefore change the message of an argv that already fails, and the new message would be wrong.
- **Tracing rule:** `CONTRIBUTING.md:42-44` lists `cmd/praytel` as following the span rule too. So moving logic into `package main` does not escape the rule, and moving it into `internal` costs no extra spans. A's "trace-golden churn" con and B's and C's "tracing extends to a new package" con roughly cancel out. This criterion therefore turns on the package boundaries themselves, not on tracing.
- **Reuse claims:** D8 assigns D10 nothing beyond wiring `--record` to D9. D8's generator lives in `internal/praytest`. D12's watch writes `VIOLATION` lines, not the file report (vision S6). So "D8/D9/D12 need the report" (A's con, and the case for B's and C's Reporter) is weak. Only D11 consumes the report, through its bytes and exit code. The real reason to move the loop out of `main` is in-process golden testing with a fake case runner. Reuse is not a reason.
- **Existing wording:** root sub-errors print unprefixed, followed by the usage line (`args.go:132-141`). The only `praytel:`-prefixed runtime message is `internal/render/snapshot/snapshot.go:76`. Every new message the vision quotes is `praytel: <msg>`, with no usage line after it.

### Scores (weighted /100; self-scores discounted where noted)
| Criterion (w) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| Backward compat (25) | 5 | 4 (hint trigger rewrites an existing error) | 5 | 5 |
| Precise usage errors (20) | 3 (flags after files, raw `flag` text) | 3 (wording in 4 sites) | 3 (raw `flag` text, flags after files) | 5 |
| Testability (15) | 3 (report only through real children) | 5 | 5 | 5 |
| Package boundaries (15) | 2 | 3 (three new packages + Executor) | 4 | 4 |
| Extensible (15) | 3 | 4 (speculative registry, guessed Reporter that B admits may not fit watch) | 4 | 4 |
| No new deps (10) | 5 | 5 | 5 | 5 |
| **Weighted** | **69** | **77** | **86** | **94** |

## Decision
Chosen: **Hybrid, C as the base.** It takes C's dispatch, `args.ParseTest` and `internal/testrun` with an injected case runner. It drops C's `Reporter` interface and B's registry. It adds a precise hint trigger, uniform `praytel:` usage wording, and a fully specified report and exit fold. Specification:

1. **Dispatch** (`cmd/praytel/main.go:63`). `if len(argv) > 0 && argv[0] == "test" { return runTest(ctx, lt, mapErr, argv[1:], os.LookupEnv, stdout, stderr) }`. Otherwise today's path runs, byte for byte. There is no registry. A second subcommand adds one `case` to a `switch argv[0]` and must follow R10.

2. **Hint (root path; only an argv that already fails changes, and only by gaining one line).** In `args.Parse`, at the two `ErrUsage` returns that can involve a misplaced `test` (`args.go:92` with no `--`, where the flags are all of `args`; `args.go:123`), call `hintMisplacedTest(flags, errOut)`. It parses `flags` with a throwaway root FlagSet (the same definitions, output `io.Discard`). If that parse succeeds and `fs.Arg(0) == "test"`, it prints `praytel: put flags after "test"`. The existing usage line then follows from `ErrMapper`, as today. `test` is therefore detected exactly where a subcommand would sit after valid root flags. Examples: `--grpc-port=0 test x` gets the hint. `--tui test -- cmd` gets it too, which is correct. `--snapshot test -- cmd` does not: its message stays `unknown --snapshot view "test"`. Root flag definitions move into `newRootFlagSet()` (registration order unchanged), which `Parse` and the hint both use. `args_test.go` and every `cmd/praytel` e2e test must pass unmodified.

3. **Parsing:** `args.ParseTest(argv []string) (TestConfig, error)` in `cmd/praytel/internal/args`.
   - `flag.NewFlagSet("praytel test", ContinueOnError)`, output `io.Discard`. A shared `registerRun(fs)` defines `--grpc-port`, `--http-port`, `--drain-idle` and `--drain-timeout`, and the root FlagSet uses the same function, so names and defaults (`args.go:105-108`) cannot drift. Also `--record` (string). `--tui` and `--snapshot` are registered only so they can be rejected.
   - `TestConfig{Mode Mode; Paths []string; Record string; Command []string; Run RunFlags}`, with `Mode` ∈ {`Files`, `Record`}. `Watch` is added in v1.1 together with its flag. It is not reserved now.
   - The argv is split at the first `--`. Every error is `args.UsageError{Msg}`, a distinct type, not `ErrUsage`. `ErrMapper` gets an `errors.As` branch, placed before the `ErrUsage` branch, that prints `praytel: <Msg>` and returns 1, with no usage line after it. Rules, first match wins:
     - `flag` parse error → Msg = the `flag` package's text prefixed with `test: ` (`praytel: test: flag provided but not defined: -max-spans`). `-h`/`--help` prints `usage: praytel test [flags] FILE|DIR|GLOB ...` and `       praytel test --record=FILE [flags] -- command [args ...]` to stderr and returns `flag.ErrHelp` (exit 0, like root).
     - `--tui` set → `--tui cannot be used with test`. `--snapshot` set → `--snapshot cannot be used with test`.
     - A positional before `--` that starts with `-` and is not `-` → `put flags before file arguments: <arg>`. This is needed because `flag` stops at the first positional.
     - `--record` with positionals → `--record takes no file arguments`. `--record` without `--` or with an empty command → `--record needs a command after --` (S1). `--record` value not ending in `.praytest.star` → S1's exact text. The FILE-exists check (S1) runs in the record entry before any child starts (D9), with `WriteNew` as the backstop (D8).
     - File mode with `--` and ≥1 positional → `test files take no command; the cases in <first positional as given> declare their own` (S2). `--` with no positional and no `--record` → `test runs files; to record one use --record=FILE -- command`. No positional at all → `no test files given; there is no implicit current-directory run` (S3).

4. **`internal/testrun`.** It imports `internal/praytest`, `internal/render/tui` (for `CheckView`) and `observability`. The exported surface is kept small, so the span rule costs three spans: `testrun.Prepare`, `testrun.Expand` and `testrun.Run`.
   - `Expand(ctx, lt, args []string) ([]string, []string)` returns the paths and the problem lines, following the S3 rules exactly as the decision-tree defaults state. Problems are worded `praytel: '<arg>' matched no .praytest.star files`, `praytel: <arg>: no such file`, `praytel: <arg>: not a .praytest.star file`. Every argument is expanded, and all problems are collected. It is a pure function of the filesystem, which makes it the M8 and M4 table and benchmark target.
   - `Prepare(ctx, lt, args, getenv) ([]*praytest.File, []string)` runs `Expand`, then `praytest.Load` on every path that expanded, even when other arguments failed. It concatenates the expansion problems and every `UsageError` (D5, printed `file:line:col: msg` with no prefix, in path order). If any problem exists, `runTest` writes the lines to stderr and returns 1. No child has started at that point (S3 all-or-nothing).
   - `type RunCaseFunc func(ctx context.Context, f *praytest.File, c *praytest.Case) CaseRun`. `CaseRun{ChildCode int; Exited bool; TimedOut bool; Phase string /* "child running"|"draining"|"checking" */; Interrupted os.Signal; BindErr error; View tui.CheckView; Stdout, Stderr []byte}`. `cmd/praytel` backs it with D2's `runCase(..., caseOpts{Isolate: true, Deadline: c.Timeout, ...})`. Signal identity (130 vs 143) comes from `caseResult.Interrupted`, which `runCase` and `runTest` capture per D2 step 7. A function type is used, not an interface: one seam, one method.
   - `Run(ctx, lt, files, rc RunCaseFunc, stdout, stderr io.Writer) int` owns the loop, the D2 step-6 classifier (calling `(*Case).Check` from D6 under steps 6.2 and 6.5), the report and the exit fold. Once a case is interrupted, every later case is not called and is printed `not run`. Once a case returns `BindErr`, that case and every later case are printed `INCOMPLETE` with the note and are not called (D2 point 1). A `BindErr` on the very first case is returned up as today's startup error (exit 1 via `mapErr`, `run.go:54-55`), which `Run` signals by returning code 1 with nothing printed to stdout.
   - The report is an unexported concrete type inside `testrun`. There is no `Reporter` interface.

5. **Report (stdout, streamed per case, flushed after each line).** CI shows progress during long cases, and stdout and stderr are each deterministic, so goldens compare them separately.
   - File header: the path exactly as expanded (relative arguments stay relative).
   - Case line: `fmt.Sprintf("  %-4s %s", label, name)`, label ∈ `ok`, `FAIL`, `INCOMPLETE`, `INTERRUPTED`, `not run`. This reproduces S2/S3 `  ok   gift` and `  FAIL visa`. The header and the case line are printed before the case starts only if streaming needs it. Specifically, the header prints when the file's first case starts, and the case line prints when the case ends.
   - Detail lines under `FAIL`, in this order, each indented 4 spaces with continuations at 6 (D6 step 9):
     1. on exit mismatch: `<file>:<line>:<col>: exit code <got>, want <want>` at the case's `Pos`;
     2. `Failure.Lines()` in recording order;
     3. D2's note, if any (`trace incomplete: <problems>` / `timeout <d> exceeded while <phase>`).
   - Under `INCOMPLETE`: one note line, either `trace incomplete: <problems joined "; ">`, `timeout <d> exceeded while <phase>`, or `could not bind OTLP ports: <err>`. `INTERRUPTED` and `not run` have no detail lines.
   - Captured output, on exit mismatch only, goes to stderr right after the case's stdout lines are written: `praytel: <file>: <case>: child stdout:` followed by the bytes, then the same for `child stderr:`. Each part prints only if it is non-empty, and a newline is appended if the bytes lack one.
   - Summary (always printed when `Run` was entered; stdout): with T cases, F files, counts f failed, i incomplete, n interrupted, r not run:
     - all ok: `praytel: T case(s) in F file(s) passed` (S2: `2 cases in 1 file passed`);
     - otherwise: `praytel: ` + comma-join of the non-zero items in the order failed, incomplete, interrupted, not run. The first item is `<k> of T cases in F file(s) <word>`, and the rest are `<k> <word>`. Examples: `praytel: 1 of 3 cases in 2 files failed` (S3), `praytel: 1 of 1 cases in 1 file failed` (S4), `praytel: 1 of 3 cases in 2 files failed, 1 incomplete`, `praytel: 1 of 5 cases in 2 files interrupted, 3 not run`. `cases` stays plural in the `of` form, matching S4.

6. **Exit fold** (computed in `Run`, never in the report): `code = 0`; any `INCOMPLETE` → `max(code, 3)`; any `FAIL` → `4`. If any case is `INTERRUPTED`, the final code is `128 + signo` (130 SIGINT, 143 SIGTERM) and overrides both 4 and 3 (D2 step 7). **Fallback, if the human refuses 130/143 (D2):** `INTERRUPTED` and `not run` count as `INCOMPLETE` in the fold (so 4 still beats them), and the labels and summary words stay. Only this line of code changes. Usage errors (exit 1) and a first-case bind failure (exit 1) never reach the fold.

7. **Record mode.** `runTest` with `Mode == Record` builds an `args.Config` from `TestConfig` (TUI on, root defaults for `MaxSpans`/`MaxLogLines`) and calls D9's record entry. D10 owns nothing after parsing.

8. **Tests.**
   - `args_test.go`: a `ParseTest` table covering every rule in item 3, plus a hint table: `--grpc-port=0 test x` → hint; `--tui test -- c` → hint; `--snapshot test -- c` → no hint; `-- test -f go.mod` → parses unchanged.
   - `testrun`: M8 `Expand` table (quoted glob, shell-expanded list, dir, dot-dir, symlinked dir, dedupe, order) and an M4 benchmark at 1,000 files.
   - `Run` goldens with a fake `RunCaseFunc` and files from `praytest.LoadSource` (D8), each giving stdout, stderr and the exit code. They cover every D6 wording row, an exit mismatch with captured output, each INCOMPLETE note, a later-case bind failure, interruption with both signals, and the S2/S3/S4 examples byte for byte.
   - One e2e `praytel test` in `cmd/praytel` against `testdata/child`.

Why it wins on the criteria:
- **Backward compat (25):** only argvs that fail today are touched (verified at `args.go:90-93,118-124`). The hint fires exactly where a subcommand would sit after valid root flags, so no existing error is reworded. It only gains a line above the usage line. B's trigger reworded one.
- **Precise usage errors (20):** one typed error, one printing site (`ErrMapper`), and one `praytel: <msg>` form for every new message. Raw `flag` text is prefixed. Flags placed after files get their own message. All expansion and load problems are reported together before any child starts. Every option left at least one of these gaps.
- **Testability (15):** the report, the classifier wiring and the fold are tested with synthetic `CaseRun`s, with no child process. `Expand` is pure for M8 and M4.
- **Boundaries (15):** `cmd/praytel` keeps dispatch, `runCase` (D2) and signal capture. The rest goes in one internal package, not three (B). Not 5, because the lifecycle spans the `RunCaseFunc` seam.
- **Extensible (15):** `--watch` is one flag, one `Mode` and one rule block in `ParseTest`. A new subcommand is one `case`. Not 5, because nothing is pre-built for formats or subcommands, deliberately.
- **Deps (10):** stdlib `flag` only.

We give up:
- **A pluggable report.** JUnit, JSON or GitHub annotations need a refactor of `testrun`'s unexported report into an interface when they are first asked for. B and C would have had the interface in place.
- **A subcommand registry.** Each new subcommand edits `runProcess`'s switch and adds a `Parse<Sub>` in `args`.
- **A single-package lifecycle.** `runCase` lives in `main` and is reached through `RunCaseFunc`, so reading one case's full path crosses two packages. A is the only option that avoided this.
- **Wording consistency with the root path.** Root sub-errors stay unprefixed and are followed by the usage line (`args.go:132-141`, a non-goal to change). `test` errors are `praytel: <msg>` with no usage line. Two styles coexist.
- **Files whose names start with `-`** cannot be given as bare positionals (`./-x.praytest.star` works).
- **Ordering between stdout and stderr** in a merged CI log: captured output can interleave with the streamed report. Each stream alone is deterministic.
- **A small refactor of root flag registration** (`newRootFlagSet`, `registerRun`) that changes no behaviour, guarded by the unmodified existing tests.

Rejected:
- A: the smallest diff, but the report, classifier and fold can only be tested through real children, and `package main` grows to about 400 lines of logic. It leaves the flags-after-files and raw-`flag` messages imprecise. Its dispatch shape is kept.
- B: a one-entry registry, a reserved `Watch` enum and a four-method `Reporter` whose shape B itself doubts fits watch. That is speculative structure across three new packages, and its hint trigger rewrites an existing error message (`--snapshot test -- cmd`).
- C: the chosen base. Its single-implementation `Reporter` interface is dropped, and its loose hint trigger and unprefixed `flag` errors are fixed.

Revisit if:
- **Second output consumer:** an issue or D11 needs machine-readable results (JUnit, JSON, annotations), or a second report shape appears in-tree. Then extract the unexported report into a `Reporter` interface inside `testrun`. This is internal only.
- **Second subcommand:** a second subcommand is accepted into the vision. Then reconsider a dispatch table if the switch exceeds 3 cases.
- **Seam friction:** `CaseRun` gains a fourth field purely to carry lifecycle state that `testrun` re-derives, or a bug is traced to the classifier and `runCase` disagreeing across the seam. Then move `runCase` into an internal package (D2 permits it as a mechanical move) and drop `RunCaseFunc`.
- **M8/M4:** the M8 table differs between shells in CI, or `Expand` exceeds 50ms at 1,000 files in `make bench-diff`. Then fix `Expand` only.
- **Wording complaints:** 2 or more issues about mixed root and `test` error styles. Then prefix root sub-errors with `praytel:`. That is a root output change and needs a vision note.
- **Interleaving:** an issue reports an unreadable CI log because captured output interleaves with the report. Then buffer captured output and print it after the summary.

Consequences for other decisions:
- **D2 (decided):** consistent. `runCase` stays in `cmd/praytel`, adapted to `RunCaseFunc`. `caseResult` must expose captured stdout and stderr bytes and the timeout phase. The classifier logic of step 6 runs in `testrun.Run`, over `CaseRun` plus `(*Case).Check`. The 130/143 precedence is implemented in item 6, with the fallback specified, pending human confirmation.
- **D5 (decided):** uses `Load` for every expanded path and `LoadSource` (D8) in goldens. `getenv` is injected from `runTest` (`os.LookupEnv`). `UsageError`s print as `file:line:col: msg`.
- **D6 (decided):** `Failure.Lines()` is printed under `FAIL` with D6's indents. `CheckResult.Cancelled` maps to INCOMPLETE per D2 step 6.3.
- **D8 (decided):** no change; `LoadSource` is reused by goldens.
- **D9 (open):** receives a parsed record-mode `TestConfig` and owns the FILE-exists pre-check before any child, the TUI, and Ctrl-W. The suffix check is done in `ParseTest`.
- **D11 (open):** the command shape is `praytel test --grpc-port=0 --http-port=0 testdata/traces`, the report and summary format are item 5, and the exit codes are item 6. CI should treat 130/143 as a cancelled job, not a test failure.
- **D12 (open):** `--watch` adds `Mode Watch` and its rules to `ParseTest` (needs `--`, no positionals, `.praytest.star` suffix, `--repro-dir`). Watch reuses `runCase`, `Load` and D6, not `testrun.Run` or its report.

**Vision amendments required:** none. Item 5 fills in what the vision leaves unstated, all consistent with S2/S3/S4 as written: streaming, labels for INCOMPLETE/INTERRUPTED/not run, mixed-outcome summaries, and the captured-output framing. The new usage messages in item 3 are not quoted in the vision. They follow its `praytel: <msg>` form, and `praytel-test.1` should list them. D2's pending S3 exit-table amendment (130/143) is the only open vision change this decision depends on.

## Amendments after Phase 4 critique
- **Item 4 contract (CaseRun):** `CaseRun` gains a `Started time.Time` field (the child start instant, `cmd.Start`) so the check phase run in `testrun` can honour D2 item 5's single deadline. The "seam friction" revisit trigger does **not** fire: `testrun` cannot re-derive the start time, and the field carries no new lifecycle behaviour. Logged by the orchestrator, no decision reopened.
- **Item 4 contract (Run signature, Interrupted type):** `testrun.Run(ctx, lt, files, rc, stdout, stderr, exitFor)` gains an `exitFor func(os.Signal) int` parameter, and `testrun.Interrupted{Sig}` (the context cancel-cause type) is exported. Reasons: one source for the interrupt exit code (`interruptExit` in `cmd/praytel`, so the 130/143 fallback is a one-function change), `testrun` cannot import `package main`, and the cause type must be visible to both packages. Logged by the orchestrator, no decision reopened.
- **Item 5 report wording (new):** when an interrupt arrives while no case is running (between cases, or after `Prepare` before the first case), the summary gains an `interrupted between cases` item and the exit is `exitFor(sig)`. This wording was added by the task planner (T21a/T21b); D10 item 5 did not cover this case. Flagged for human confirmation with the other pending items.
