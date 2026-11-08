## Option B: Today's `run` body, once per case   (stance: minimal change)

Summary: Turn the body of `run` (`cmd/praytel/run.go:44-201`) into `runCase` with almost no edits, and make `praytel test` a sequential loop that calls it once per case. Each case gets its own `ingest.New` pipeline, receiver, correlator and event channel. The renderer slot runs D1's `tui.Settle`. A single per-case timer, started when the child starts, covers the child, the drain and the check.

How it works:
- `cmd/praytel/run.go`: extract `runCase(ctx, lt, cfg, argv, view renderer, childIn io.Reader, onStart func(*os.Process)) (code int, started bool, err error)`. `run` becomes `runCase` plus the existing `snap.Finish` (`run.go:197-199`), so `praytel -- cmd`, `--tui` and `--snapshot` follow the same code path as today. `startChild` (`run.go:204-243`) gains an `onStart` hook that fires after `cmd.Start`.
- New `cmd/praytel/test.go`: `for _, c := range cases { r := runCase(...) }`. The view is a 10-line `settleView` whose `Run` calls `tui.Settle(events)` and stores the `CheckView`. The correlator closes `events` on exit (`correlator.go:173,216-219`), so `Settle` ends after `DrainComplete`, which is what D1 requires.
- cwd: inherit praytel's cwd (`exec.Cmd.Dir` unset, as at `run.go:216`). Stdin: `nil` (`/dev/null`), using the branch `--tui` already takes (`run.go:142-146`). Environment: unchanged, `rec.ChildEnv()` + `withOTelExporterEnv` (`run.go:151,220,277-288`).
- Timeout: `time.AfterFunc(c.Timeout)` armed in `onStart`. When it fires it sets `timedOut`, sends `proc.Kill()`, closes `outR`/`errR` so `capWG.Wait` (`run.go:159`) cannot block on a grandchild that still holds the pipe, and calls `Cancel` on the check's Starlark thread (D6). The drain is already bounded by its hard cap (`correlator.go:237-262`).
- Classification, in this order: `timedOut` gives INCOMPLETE (3) and the child code is ignored. Otherwise, if the child code differs from `exit_code`, or the check fails or raises an error, the case is FAIL (4). Otherwise, if `len(Problems) > 0`, the case is INCOMPLETE (3) and the check is not run. Otherwise it is ok. The overall exit is the maximum over all cases.
- Ctrl-C: unchanged per case. The signal is forwarded to the child (`run.go:229`) and the drain finishes on the non-cancelled `runCtx` (`run.go:95`). The loop checks `ctx.Err()` between cases, prints the remaining cases as `INCOMPLETE (interrupted)` and stops. No new exit code is added.
- Record: `runCase` with `cfg.TUI`. `onStart` and the return set and clear an `atomic.Bool childRunning`, which D9 reads at Ctrl-W.
- Rollout: one PR. Existing goldens guard the `run` path.

Steelman: The lifecycle that `--snapshot` users and the golden job already rely on, including signal forwarding, the 50ms drop hold, drain and teardown, runs unchanged for every case. Isolation comes from tearing down the whole pipeline and building a fresh `Model` per case, not from new filtering code. The only new logic is the timer and a four-line classifier.

Pros: the smallest diff. Existing modes are untouched by construction. Each case can be tested by reusing the existing run tests. The `onStart` hook is also the seam that watch needs.

Cons / failure modes / hidden costs:
- With the default fixed ports, a grandchild that outlives case N can export into case N+1's fresh receiver on the same port, which breaks isolation. Only `--grpc-port=0` closes this.
- Rebinding 4317/4318 each case can fail if another process takes the port between cases. The case then exits `1` partway through the run.
- Every case pays the idle drain (250ms) plus the 50ms hold (`run.go:166`) plus bind and teardown. For 50 cases that is about 15s of fixed wait.
- `proc.Kill()` kills only the direct child. Grandchildren are leaked, not reaped.
- A timeout that fires during the drain or the check discards a possible FAIL, so `3` can hide a real `4`.
- Ctrl-C forwards only one signal. A child that ignores SIGINT runs until its `timeout`.

Criteria scores:
- Determinism: 3. Each case gets a fresh pipeline, but the fixed-port grandchild leak and the rebind race remain.
- S3 fidelity: 4. Precedence is explicit, but a timeout masks a `4`.
- Existing modes unchanged: 5. They run the same code path.
- Bounded runtime: 4. One timer covers the whole case, and closing the pipes defeats pipe-holders. Leaked grandchildren survive.
- Record/watch: 4. `childRunning` and `onStart` are cheap seams.
- Overhead: 3. There is about 300ms of fixed wait per case, though per-case overhead relative to `praytel -- cmd` is roughly zero.

Cost: S     Reversibility: easy (internal refactor; only cwd = praytel's cwd and stdin = /dev/null become public contract under R9)

Unknowns / assumptions: Go listeners set SO_REUSEADDR, so an immediate rebind succeeds. D6's `Cancel` is reachable from the timer goroutine. Ignoring the child code on timeout is acceptable under S3.
