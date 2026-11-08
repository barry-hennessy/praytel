## Option C: One session per case, `go test`-style process rules   (stance: most conventional)
Summary: Each case is a full, fresh copy of today's `run` pipeline (bind, child, drain, teardown), the same way `go test` gives each package its own process and Tracetest gives each run its own trace. The runner is a sequential loop over a single `runSession` function. Child conventions follow `go test`/CI runners: the working directory is the test file's directory, stdin is `/dev/null`, the child runs in its own process group, and one wall-clock deadline covers the child, the drain and the check.

How it works:
- **Extract a session.** Move `cmd/praytel/run.go:44-202` into `cmd/praytel/session.go`: `runSession(ctx, lt, sessionCfg) (sessionResult, error)`. `sessionCfg` holds `{Command, Dir, Stdin, Deadline, OwnPgrp, View func(events) renderer}`. `sessionResult` holds `{ChildCode, Started, TimedOut, Interrupted, Events}`. `praytel -- cmd` calls it with `Dir=""`, stdin passed through, no deadline and `OwnPgrp=false`, which is exactly today's behaviour.
- **Per case:** `ingest.New` runs again for every case (`internal/ingest/pipeline.go:23-35`), so each case gets a new channel, correlator and receiver. The view is `tui.Settle(events)` (D1), which returns when the correlator closes `out` after `DrainComplete` (`internal/correlator/correlator.go:173,302`). With fixed ports, `Serve` has already returned (`internal/receiver/receiver.go:209-238`), so the next bind succeeds.
- **Child:** `exec.CommandContext` with `cmd.Dir = filepath.Dir(file)` (as `go test` does), `cmd.Stdin = nil` (the null device), `SysProcAttr{Setpgid:true}`. `Cancel` sends SIGTERM to `-pgid`. `WaitDelay` is 5s, after which SIGKILL. The environment is unchanged (`run.go:216-221,277-288`).
- **Deadline:** `context.WithTimeout(timeout)` starts at child start. On expiry the child is killed, the drain still runs to its hard cap, and Starlark gets `Cancel` (D4/D6). Worst case is `timeout + 5s + drain cap + 50ms`.
- **Classification, in order:**
  1. Interrupted: the case is not classified.
  2. Timed out: INCOMPLETE (3).
  3. Exit code differs from `exit_code`: FAIL (4). The check still runs, so every failure is reported, as test runners do. If the trace is also incomplete, the report says so and the case stays 4.
  4. `Problems` is non-empty: INCOMPLETE, and the check is skipped.
  5. Otherwise the check runs: pass, or 4.
  Captured output is printed only for non-pass cases (`go test` without `-v`).
- **Ctrl-C:** the first SIGINT is forwarded (`run.go:245-275`). The current case drains and is printed as `INTERRUPTED`, later cases are skipped, and the summary prints. Exit 130. A second SIGINT sends SIGKILL to the group and exits at once (the Bazel/compose double-Ctrl-C pattern).
- **Record:** `runSession` uses the TUI view, invocation cwd and nil stdin, as `--tui` does today (`run.go:142-146`). An `atomic.Bool childRunning` is set after `Start` and cleared after `Wait`; the S1 warning reads it.

Steelman: there are no new concepts. Isolation comes from construction, not from filtering: a new correlator per case means no span from case N can reach case N+1's `Model`, and a process-group kill removes the grandchildren that might export late. `praytel -- cmd` becomes the one-case special case of the same code, so the test path and the default path cannot drift. Cwd, stdin and timeout semantics are the ones Go and CI users already expect, which matters once files are public (R9).

Pros: the strongest isolation available; one code path; a bounded runtime that is easy to state; watch (S6) is the same session with a different view.

Cons / failure modes / hidden costs:
- Using the file's directory as cwd conflicts with S1. A recording to `testdata/traces/x.praytest.star` of `-- ./bin/shop` breaks, because `command` must be stored unchanged. The recorder would have to warn about this.
- Rebinding fixed ports races any non-Go process holding 4317. A daemon that called `setsid` escapes the pgrp kill and can export into the next case.
- Extracting `run` touches the golden-guarded default path (criterion 3 risk). `Setpgid` must stay off there, or Ctrl-C from the terminal stops reaching the child.
- Each case pays the bind, the 250ms idle drain and the 50ms hold. That is the same as `praytel -- cmd`, but N cases cost N times that.
- Exit 130 is not in the S3 table.

Criteria scores:
- Determinism: 5 - a fresh pipeline per case plus a pgrp kill; ports rebind cleanly.
- S3 fidelity: 4 - precedence is explicit; 130 is an addition the table lacks.
- `praytel -- cmd` unchanged: 3 - default path refactored, guarded only by tests.
- Bounded runtime: 5 - one deadline, kill escalation, capped drain.
- Record/watch: 4 - same session; the still-running flag is trivial.
- Overhead: 4 - zero over `praytel -- cmd` per case, by construction.

Cost: M     Reversibility: hard (cwd/stdin become public behaviour once files ship)

Unknowns / assumptions:
- The conventions cited here (`go test` cwd and stdin, pytest stdin capture, exit 130, double Ctrl-C) are from memory. None were checked against the research files or primary docs.
- I assume `GracefulStop` cannot block past the drop-mode hold.
- Exit 130 needs vision sign-off.
