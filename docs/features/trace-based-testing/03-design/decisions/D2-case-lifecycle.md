# D2: How does one case run end to end: pipeline lifetime, child execution, drain, completeness, timeout and outcome classification?
Status: decided     Depends on: D1     Parent: none
Vision refs: S2 (headless run, captured output), S3 exit table, S1 (record lifecycle, "still running" warning), S6; §5 Ports and drain, Pipeline; R4, R6, R7; M2, M4

## Context
- Today `run` builds one pipeline per process: `ingest.New` binds the receiver ports (`internal/ingest/pipeline.go:23-35`, called at `cmd/praytel/run.go:44-56`), then runs correlator, renderer, receiver and capture, starts the child, waits, stops the correlator, holds the listener 50ms in drop mode and tears down (`cmd/praytel/run.go:98-202`).
- The child gets the parent environment minus `OTEL_EXPORTER_OTLP_*` plus the receiver's endpoints (`cmd/praytel/run.go:216-221,277-288`). Stdin is passed through except under `--tui` (`cmd/praytel/run.go:142-146`). SIGINT/SIGTERM are forwarded while lifecycle uses a non-cancelled context (`cmd/praytel/run.go:92-96,245-275`).
- `Snapshot.Finish` maps outcomes so a non-zero child code wins (`internal/render/snapshot/snapshot.go:47-72`). Under `praytel test` the child's code is instead compared to the case's `exit_code`, and a mismatch is `4` (S3 table, R6).
- Drain is idle timeout + hard cap (`cmd/praytel/internal/args/args.go:107-108`; correlator `finishDrain`, `internal/correlator/correlator.go:292-302`). Defaults 250ms/2s.
- Cases run sequentially (S3). With default fixed ports (4317/4318) each case rebinds; with `--grpc-port=0` each case gets fresh ports (S5, R11).
- Per-case `timeout` (default `10m`) hit → `INCOMPLETE`, exit `3`; a check over the step cap → `4` (S3, S4). The vision does not say whether `timeout` covers child, drain and check together.
- The vision does not state the child's working directory or stdin under `praytel test`; once files are public (R9) changing either breaks users' commands.
- Record mode runs the child under the TUI exactly as `--tui` does today (S1 step 2) but must also know whether the child is still running when Ctrl-W writes (S1 edge case).
- `shop` batches exports with a 100ms timeout and flushes on shutdown (`demo/workloads/shop/main.go:54-74`); late spans are the main completeness risk (synthesis §3.1).

## Goals
- Every case ends in exactly one of: pass, fail (assertion, cardinality, runtime error, exit mismatch), incomplete (completeness or timeout), with `4` taking precedence over `3`.
- Cases are isolated: no span from case N can be seen by case N+1.
- Child environment identical to today's child; working directory and stdin defined.
- Defined behaviour for Ctrl-C during a multi-case run.
- The same lifecycle serves record mode (child running + TUI) and leaves room for v1.1 watch.

## Non-goals
- Parallel cases (§7 non-goals, R11).
- Retries (§7, R7).
- Assertions on captured stdout/stderr (§7 Later).

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| Determinism: case isolation, late spans, port reuse | 25 | M2 (0 failures in 100 runs), R7 required job, R4 |
| Fidelity to the S3 exit table and precedence | 20 | S3 table; R6 child exiting 3/4 |
| `praytel -- cmd`, `--tui`, `--snapshot` behaviour unchanged | 20 | §5 "No existing invocation changes meaning"; "nothing to relearn" |
| Bounded runtime: no hang in child, drain or check | 15 | §4 `timeout`; S4 "can't hang CI"; R2 |
| Serves record mode (and leaves room for watch) | 10 | S1 still-running warning; S6 |
| Per-case overhead | 10 | M4 ≤ 50ms over `praytel -- cmd` |

## Options
Full texts: `../options/D2-A.md` .. `D2-C.md`. Scores below are the decider's after checking the code, not the explorers' self-scores.

### Option A: One praytel subprocess per case (re-exec), parent orchestrates
The parent never binds ports. It re-execs its own binary per case with a hidden `--case-result-fd=3`, which runs today's `run()` plus a renderer that writes `{child_code, CheckView}` as JSON to fd 3. The sub runs in its own process group; the parent owns timeout (SIGTERM then SIGKILL to `-pgid`), classification and a post-case group sweep. cwd = invocation dir, stdin `/dev/null`. Ctrl-C → exit 3. Record mode stays in-process. Cost M, reversibility easy.

### Option B: Today's `run` body, once per case
Extract `run.go:44-201` into `runCase` with an `onStart` hook; a sequential loop calls it per case with a `settleView` (D1 `tui.Settle`). cwd inherited, stdin `/dev/null`. `time.AfterFunc(timeout)` armed at child start kills the direct child, closes the read pipes and cancels Starlark. Timeout → 3 with the child code ignored. Ctrl-C forwarded as today, remaining cases `INCOMPLETE (interrupted)`, no new exit code. Record reads an `atomic.Bool childRunning`. Cost S, reversibility easy.

### Option C: One session per case, `go test`-style process rules
Extract `runSession(ctx, lt, sessionCfg)`; per case: fresh pipeline, `cmd.Dir = filepath.Dir(file)`, stdin null, `Setpgid`, one deadline from child start covering child, drain and check (SIGTERM group, 5s, SIGKILL). Exit mismatch → 4 and the check still runs; Ctrl-C → current case `INTERRUPTED`, exit 130; second Ctrl-C SIGKILLs the group. Cost M, reversibility hard (cwd).

### Verified claims (path:line)
- **Ports and env.** `receiver.Listen` binds `127.0.0.1:<port>` with `net.Listen`, so port `0` yields a fresh ephemeral port per call (`internal/receiver/receiver.go:71-97,166-168`). The child learns the actual ports only through `ChildEnv()`: `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:<grpc>` + `PROTOCOL=grpc` for traces, `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:<http>/v1/logs` for logs (`receiver.go:194-207`), merged after stripping inherited `OTEL_EXPORTER_OTLP_*` (`cmd/praytel/run.go:220,277-288`). Defaults are fixed via `receiver.DefaultGRPCPort/DefaultHTTPPort` (`cmd/praytel/internal/args/args.go:105-106`). So per-case ephemeral ports already work with `--grpc-port=0 --http-port=0` and no new code; S5's `make trace-test` already passes them.
- **Rebind.** `Serve` closes both listeners on `runCtx` cancel (`receiver.go:223-236`), which `run` awaits (`run.go:172-173`) before returning, so a sequential rebind of 4317/4318 does not race praytel itself. Only a foreign process (or a straggler from the last case, on fixed ports) can interfere. `TestOccupiedDefaultPortDoesNotExecChild` (`cmd/praytel/wrapper_test.go:243-271`) pins today's exit-1-before-exec on a bind failure.
- **Pipe holders block the drain.** `capWG.Wait()` (`run.go:159`) runs *before* `corr.Stop()` (`run.go:160`), and `capture.Copy` returns only on EOF or ctx cancel (`internal/capture/capture.go:62-75`); `runCtx` is not cancelled until after the hold (`run.go:172`). A grandchild that inherits stdout and outlives the child therefore blocks the drain indefinitely, today and in every option that reuses `run` unchanged (A's "byte for byte" reuse inherits it inside the sub; only A's parent deadline bounds it). B's "close outR/errR on timeout" and the hybrid below bound it.
- **Event channel close.** The correlator closes `out` on `Run` exit (`internal/correlator/correlator.go:173,216-219`) after emitting `DrainComplete` (`correlator.go:302`), satisfying D1's requirement for `Settle`.
- **Process groups.** No `Setpgid`/`SysProcAttr` anywhere in the repo; `forwardSignals` signals only the direct child (`run.go:248-275`). `go 1.26.0` (`go.mod:3`), so `exec.Cmd.Cancel`/`WaitDelay` exist, though the hybrid signals the group directly.
- **Default-path guards.** ~25 end-to-end tests run the built binary over `run()`: signal forwarding (`wrapper_test.go:529`), drain/late flush/hard cap (`:406,442,462,485`), port flags and env (`:202,225,243`), snapshot completeness (`snapshot_test.go:10-44`). The `golden` CI job never fails, so it is *not* a guard; these tests are.
- **Discounted:** C's `go test`/pytest/Bazel conventions are self-declared from memory (C's Unknowns) and carry no weight. A's 5-20ms exec estimate is unmeasured. B's "same code path, therefore unchanged" holds only if the extraction is behaviour-neutral, which is exactly what must be guarded (point 6 below).

### Scores (decider; weight × score)
| Criterion (w) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| Determinism (25) | 4 | 3 | 4 | 4 |
| S3 fidelity (20) | 4 | 3 | 3 | 5 |
| Existing modes unchanged (20) | 5 | 4 | 3 | 4 |
| Bounded runtime (15) | 5 | 3 | 5 | 5 |
| Record / watch (10) | 2 | 4 | 3 | 4 |
| Overhead (10) | 3 | 4 | 4 | 4 |
| **Weighted /100** | **81** | **68** | **73** | **87** |

Notes on scores: A's determinism is 4, not 5: the process boundary isolates no better than a fresh in-process pipeline + fresh `Model` (D1), and a `setsid` daemon on fixed ports escapes its sweep just as it escapes the hybrid's. B's S3 score is 3 because its classifier hides a known exit mismatch behind a timeout and its own text is contradictory on whether the check runs on an incomplete trace; bounded runtime 3 because `proc.Kill()` leaves grandchildren alive. C's record score is 3 because file-dir cwd breaks S1's acceptance ("passes `praytel test gift.praytest.star`") for any recording written outside the invocation directory with a relative `command`.

## Decision
Chosen: **Hybrid: B's in-process `runCase` extraction, with C's process-group kill and single deadline, a stricter classifier, and test-only isolation gated off the default path.** Specification:

1. **Extraction.** Move `run.go:44-201` into `runCase(ctx, lt, cfg args.Config, rc caseOpts) (caseResult, error)`. `run` = `runCase(..., caseOpts{})` + today's `snap.Finish` (`run.go:197-199`). The zero `caseOpts` must reproduce today's behaviour exactly: stdin passthrough (nil under `--tui`), no pgid, `proc.Signal` forwarding, unbounded `capWG.Wait`, no deadline. Every test-mode behaviour below is behind `caseOpts.Isolate bool` or a non-zero field. `caseOpts` also carries `View func(<-chan domain.Event) renderer`, `Deadline time.Duration`, `OnStart/OnExit func()` (record and watch hooks). `caseResult` = `{ChildCode int; Exited bool (exited before the deadline); TimedOut bool; TimeoutPhase string (child|drain|check, added after D10); Interrupted os.Signal; Stdout, Stderr []byte (captured child output for the per-case report, added after D10); View tui.CheckView}`.
2. **Ports (point 1).** Each case calls `ingest.New` with the `test` flags' ports. Defaults stay 4317/4318, the same as the root flags (decision-tree defaults; §5 "mean the same inside `test`"), because an app that hardcodes 4317 and works under `--tui`/`--record` must also replay. Per-case ephemeral ports are `--grpc-port=0 --http-port=0`; the child learns them only through `ChildEnv()`'s `OTEL_EXPORTER_OTLP_ENDPOINT`/`_LOGS_ENDPOINT` (`receiver.go:194-207`), as today. Residual risk accepted: on fixed ports a straggler that escaped the group sweep (step 6, `setsid`) can export into the next case. The guide and `praytel-test.1` say: "use `--grpc-port=0 --http-port=0` in CI". A bind failure before the first case exits `1` with no child started (today's behaviour, `run.go:54-55`); a bind failure on a later case marks that case and all remaining cases `INCOMPLETE (could not bind OTLP ports: ...)` and the run continues to the summary.
3. **Child (point 2).** Environment exactly as today (`rec.ChildEnv()` + `withOTelExporterEnv`). **cwd is inherited from praytel** (`cmd.Dir` unset, as `run.go:216`). **stdin is `/dev/null`** (`cmd.Stdin = nil`, the branch `--tui` already takes at `run.go:142-146`). Under `Isolate`, `SysProcAttr{Setpgid: true}`. The rule `praytest.5` states and generated files must satisfy: *"`command` runs in the directory `praytel test` was started from, with stdin closed. `command[0]` and any relative path argument are resolved from that directory, or through `PATH` if `command[0]` contains no `/`. A recorded file therefore replays from the directory it was recorded in; to run it from elsewhere, use a `PATH` name or `env()`."* A per-case `dir=` parameter can be added later without breaking any file; switching the default cannot, which is why the default is the conservative one.
4. **Post-exit sequence under `Isolate`** (fixes the pipe-holder hang verified above): child `Wait` returns → close write ends (today) → wait for capture EOF **at most `--drain-timeout`**; on expiry `SIGKILL` to `-pgid` and close `outR`/`errR` so `capture.Copy` returns → `corr.Stop()` and the normal idle/hard-cap drain → **group sweep: `SIGKILL` to `-pgid`** (ESRCH ignored) → `rec.Drop()`, 50ms hold, teardown as today. The sweep follows the drain so a grandchild that is still flushing gets the idle window first.
5. **Deadline (point 3).** One wall-clock deadline of `timeout`, started at `cmd.Start`. Expiry while the child runs: `SIGTERM` to `-pgid`, after **2s** `SIGKILL` to `-pgid`; then step 4 runs (bounded by `--drain-timeout`) so captured output is still collected. Expiry during the drain: the drain finishes (it is already bounded) and the check is not started. Expiry during the check: Starlark `thread.Cancel` (D6). Worst-case wall time per case: `timeout + 2s + 2×drain-timeout + 50ms`.
6. **Classification, first match wins; exactly one outcome per case:**
   1. Interrupted (point 4) → `INTERRUPTED`.
   2. The child exited on its own **before** the deadline with a code ≠ `exit_code` → `FAIL` (4), even if the deadline later expired or the trace is incomplete. If `Problems` is empty and there was no timeout, the check still runs and its failures are added to the report; otherwise the report adds `trace incomplete: <problems>` or `timeout during drain/check` as a note. Captured output goes to stderr (S3).
   3. Deadline expired (any phase) → `INCOMPLETE` (3): `timeout 10m exceeded while <child running|draining|checking>`. The child's code is not compared, because the child was killed. Failures recorded by a check cancelled mid-run are discarded, so the outcome never depends on how far a cancelled check got.
   4. `len(CheckView.Problems) > 0` → `INCOMPLETE` (3); the check is not run (S3 row 3, D1).
   5. Run the check: any failure, runtime error or step-cap hit → `FAIL` (4); else `ok`.
   This is how a timeout can and cannot hide a 4: a known exit mismatch is never hidden; an assertion failure that would only have been found by a check that never ran or was cancelled is reported as 3, which is honest, since the verdict is unknown. The step cap is deterministic and fires before the deadline in any non-pathological check, so S4's "exceeded step limit → 4" holds.
7. **Ctrl-C (point 4).** Under `Isolate` the child is in its own group, so the terminal's SIGINT reaches only praytel. The first SIGINT/SIGTERM is forwarded to `-pgid` (not just the pid); the current case drains normally (bounded by step 4) and prints as `INTERRUPTED`; remaining cases print as `not run`; the summary prints; exit **128+signo: 130 for SIGINT, 143 for SIGTERM**, which takes precedence over 4 and 3. A second signal sends `SIGKILL` to `-pgid` and exits immediately with the same code. Why not 3: an interrupted run is not a verdict. Exit 3 would make `git bisect run praytel test ...` mark a commit bad on Ctrl-C, whereas a code above 127 aborts the bisect. Exit 3 would also be indistinguishable from a slow drain in CI logs.
8. **Drain cost (point 5).** Test mode keeps the drain defaults (250ms idle / 2s cap) and the 50ms hold. About 0.3s of fixed wait per case is accepted: praytel's own suite has about 5 cases (S5), so about 1.5s per `make trace-test` and about 2.5 min over M2's 100 runs. M4 measures overhead *over* `praytel -- cmd`, which pays the same drain, so the marginal cost is about 0. Shortening idle in test mode would trade CI seconds for late-span INCOMPLETEs (R4, M2) and is rejected.
9. **Default-path risk (point 6).** Mitigations, all required in the extraction PR: (a) the extraction PR contains no `praytel test` code and must pass every existing `cmd/praytel` end-to-end test unmodified (`wrapper_test.go`, `snapshot_test.go`); (b) `Isolate=false` code paths are line-for-line today's (no pgid, `proc.Signal`, unbounded capture wait); (c) add one test asserting that under the default path a terminal-style SIGINT still reaches the child once (extends `wrapper_test.go:529`).
10. **Record mode.** `runCase` with `cfg.TUI`, `Isolate=false` (exactly `--tui`: inherited cwd, stdin nil, no pgid), and `OnStart`/`OnExit` setting and clearing an `atomic.Bool childRunning` that D9 reads at Ctrl-W. Watch (v1.1) is the same call with its own view.

Why it wins on the criteria:
- **Determinism (25):** a fresh pipeline, channel and `Model` per case (D1) plus a group sweep after every case, and the pipe-holder hang closed. Ephemeral ports per case for CI need no new code. It matches A without a second process or codec.
- **S3 fidelity (20):** the only option with a total, ordered classifier in which a known exit mismatch always wins over 3 and a cancelled check cannot produce a timing-dependent verdict. Interruption is made explicit instead of overloading 3.
- **Unchanged modes (20):** test behaviour is gated off the default path and the extraction must pass the existing e2e suite unmodified. It scores 4 rather than A's 5 because it is still a refactor of `run`.
- **Bounded (15):** one deadline, TERM→KILL to the group, bounded capture wait, bounded drain.
- **Record/watch (10) and overhead (10):** one in-process code path with hooks, and no exec or codec per case.

We give up:
- **A's hang-proofing against praytel's own bugs.** A deadlock inside `Settle`, a renderer or the correlator hangs the in-process runner; only CI's job timeout catches it. A's external deadline would not depend on praytel's goroutines cooperating.
- **Full isolation on default ports.** A `setsid`/double-forked daemon escapes the group sweep and can export into the next case on 4317. Only `--grpc-port=0` closes this, and that is opt-in.
- **Location-independent test files.** A file with a relative `command` passes only when run from its recording directory. C's file-dir cwd would have fixed that, at the cost of breaking recordings and being irreversible.
- **Stdin-driven cases.** A case cannot feed input on stdin in v1. That is additive later, for example a `stdin=` parameter.
- **Children that read the TTY.** In their own process group they get SIGTTIN and stop until the deadline. With stdin at `/dev/null`, only programs that open `/dev/tty` themselves are affected.
- **Partial results of a timed-out check,** and about 0.3s of fixed drain wait per case.
- **A clean vision table.** The S3 table needs the amendment below.

Rejected:
- A: buys hang-proofing with a private `CheckView` wire codec, two-level signal forwarding, a re-exec test harness and a record path that bypasses the orchestrator. The fixes it relies on (pgid, sweep, deadline) are taken into the hybrid in-process.
- B: kills only the direct child, lets a timeout hide a known exit mismatch, leaves the pipe-holder drain hang unbounded on the success path, and is ambiguous on whether the check runs on an incomplete trace.
- C: the test file's directory as cwd breaks S1 recordings made elsewhere and is a one-way public commitment justified only by conventions cited from memory. Its lifecycle mechanics are otherwise adopted.

**Vision amendment required (S3 exit table)**:
- **Add a row above `4`:** "praytel received SIGINT or SIGTERM during a file run. The current case is drained and printed as `INTERRUPTED`, later cases are printed as `not run`, and the summary prints. Exit `130` (SIGINT) or `143` (SIGTERM). Takes precedence over `4` and `3`."
- **Row `4`:** change "or a child exit differs from `exit_code`" to "or a child exited before its `timeout` with a code different from `exit_code`".
- **Row `3`:** change "or its `timeout` was hit" to "its `timeout` was hit (timeout covers the child, drain and check), or its OTLP ports could not be bound after earlier cases had run".
- **§5 "Ports and drain":** add "Each case binds its own receiver. Use `--grpc-port=0 --http-port=0` for isolation in CI."
- **§4:** state the cwd/stdin rule from step 3.

The orchestrator should put these in front of the human. If 130/143 is refused, the fallback is `3` with `INTERRUPTED` in the report. That changes only step 7 and D10's aggregation.

Revisit if:
- **Port bleed:** any M2, nightly or M7 `trace-tests` failure is traced to a span from an earlier case or to `EADDRINUSE` on a rebind. Then make `praytel test` default to ephemeral ports. This is a vision change to §5, but it breaks no file.
- **Late spans:** ≥ 1 of M2's 100 runs, or ≥ 2 nightly runs in a month, ends INCOMPLETE with problems caused by spans arriving after the idle drain. Then raise the test-mode `--drain-idle` default. Never lower it.
- **Self-hang:** a CI `trace-tests` run exceeds its worst-case bound (`timeout + 2s + 2×drain-timeout + 50ms` per case) without a child being the cause. Then move to A's re-exec isolation behind the same `caseResult`.
- **Overhead:** `make bench-diff` shows `praytel test` per-case wall time > 50ms above `praytel -- cmd` (M4). Then profile the extraction before adding any reuse of receivers across cases.
- **Default-path regression:** any existing `cmd/praytel` e2e test must be edited to make the extraction pass, or a `praytel -- cmd` bug is bisected to it within the first release. Then revert to a duplicated `runCase` that leaves `run` untouched.
- **cwd:** ≥ 2 issues or dogfooding files that need a different working directory. Then add an optional `dir=` to `test(...)` (additive). Do not change the default.
- **TTY:** a report of a case stopped by SIGTTIN. Then document it, or offer an opt-out from `Setpgid` per case.

Consequences for other decisions:
- **D1:** consistent. Each case passes its own `pipe.Events` to `tui.Settle`. The correlator closes the channel after `DrainComplete` (`correlator.go:173,302`), and INCOMPLETE holds iff `Problems` is non-empty.
- **D4:** no conflict. Cancellation via `thread.Cancel` stays inside `internal/praytest`.
- **D5:** `timeout` parses as a Go duration (`10m`). `praytest.5` carries the step 3 cwd/stdin rule. `load()` resolution (relative to the file) is independent of the child's cwd.
- **D6:** the check runs in-process after `Settle` returns, only under classification steps 2 (complete, no timeout) and 5. The deadline goroutine calls `thread.Cancel`. A cancelled check maps to INCOMPLETE and its partial failures are discarded. A step-limit error maps to FAIL. D6 must distinguish the two by message (D4).
- **D8:** generated files store argv unchanged (S1). If `command[0]` is a relative path containing `/`, the generator emits `# command paths are relative to the directory praytel test is run from`.
- **D9:** reads `childRunning` (step 10) for the S1 warning. Record mode is `Isolate=false`, identical to `--tui`.
- **D10:** the runner owns the loop, signal capture (it must know which signal arrived, for 130 vs 143), streaming the per-case report, the aggregation `max(0, 3, 4)` with interruption overriding to 128+signo, the first-case bind-failure exit 1, and the `INTERRUPTED`/`not run`/bind-failure report lines. `test` flags keep the root defaults (4317/4318, 250ms/2s).
- **D11:** `make trace-test` keeps `--grpc-port=0 --http-port=0` (S5). Budget about 0.3s fixed wait per case. M2's evidence should log the INCOMPLETE reasons, to feed the late-span trigger. The CI job needs a timeout (for example 15 min) as the backstop for the self-hang risk accepted above.
- **D12:** watch is `runCase` with a streaming view and the same `OnStart`/`OnExit` hooks, with `Isolate=false` to match `--` semantics (the child's exit code is returned, S6).
