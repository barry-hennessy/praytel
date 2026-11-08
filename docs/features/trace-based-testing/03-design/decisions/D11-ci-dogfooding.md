# D11: How does praytel's own CI run trace tests as a required check, and how is determinism proven and guarded?
Status: decided     Depends on: D2, D10     Parent: none
Vision refs: S5; §5 CI; §7 v1 item 6; R7, R11; A5, A7; M2, M7, M8

## Context
- Every `check.yml` job runs a `make` target through the Dagger module so CI matches local runs (`.github/workflows/check.yml:1-12,26-40`; `dagger/check.go`, `dagger/golden.go`). The `golden` job is `continue-on-error` and must stay unchanged (`.github/workflows/check.yml:74-120`; S5).
- `check.yml` runs on PR, push to main and a weekly cron (`.github/workflows/check.yml:15-21`); M2 needs a nightly 20× repeat and 100-run evidence (`GOMAXPROCS=1`, default settings) in the PR that adds the job.
- `make golden`/`golden-check` are the precedent for target shape (`Makefile:183-220`). S5 fixes `make trace-test` as `praytel test --grpc-port=0 --http-port=0 testdata/traces`.
- `shop` needs `-card TYPE` (A5) and must be locatable by the case `command` in CI and locally (PATH vs `env("SHOP_BIN", "shop")`, §4). Flags today: `-pace` only (`demo/workloads/shop/main.go:180-195`); A-1002 slow and A-1003 gift failure at `demo/workloads/shop/main.go:152-163`. Exporter batches with a 100ms timeout and flushes on shutdown (`demo/workloads/shop/main.go:54-74`).
- Timing assertions need ≥ 2× margin (450ms vs 200ms, R7); cases use `-pace 0s`. INCOMPLETE fails the job (R7).
- M8 shell-independence needs a table test across bash, zsh and sh, which the runner image must provide.
- A7: an admin must mark `trace-tests` required in branch protection; a workflow cannot.

## Goals
- `trace-tests` is red exactly when a committed trace fact breaks, and green otherwise.
- Adding `testdata/traces/foo.praytest.star` needs no Makefile or workflow change.
- The same `make trace-test` runs locally and in CI.
- M2 evidence is reproducible and attached to the PR.

## Non-goals
- Retries, in praytel or CI (§7, R7).
- Making the `golden` job failing.
- Testing third-party apps.

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| Determinism and flake resistance | 30 | M2, M7, R7 (required job blocks every PR) |
| Local/CI parity: one command | 20 | `check.yml` header convention; S5 `make trace-test` |
| Diagnosable red runs (S4 output in the log) | 15 | S5 acceptance |
| New test files need no CI edit | 15 | S5 acceptance; R12 |
| CI time and cost (per PR, 100-run evidence, nightly) | 10 | M2; R11 |
| Golden job untouched and isolated | 10 | S5 "golden job stays non-failing and unchanged" |

## Options
Full texts: `../options/D11-A.md` .. `D11-C.md`. Scores are the decider's after checking the code, not the explorers' self-scores.

### Option A: One Dagger function, one copied job, COUNT for repeats (stance: minimal change)
`TraceTest(count, gomaxprocs)` with `+cache="never"` on `goContainer`; `make trace-test COUNT=..`; a `trace-tests` job copied from `lint`; the nightly is a second cron line in `check.yml` with `COUNT=20` chosen by `github.event_name`. The loop stops at the first failure. The red report reaches the log only through Dagger's exec error. M2 evidence by local run or by dispatching `check.yml`.

### Option B: Dagger-backed target, required job in check.yml, separate nightly workflow (stance: most conventional)
Same function and target as A, plus apt `zsh` and the M8 `go test` inside `TraceTest`; `timeout-minutes: 15`; a new `trace-nightly.yml` (daily cron + `workflow_dispatch` with `count`/`gomaxprocs`, log artifact). M2 evidence by dispatching the nightly workflow on the PR branch. Admits exit codes collapse into a Dagger error and the report may be truncated (unverified).

### Option C: Continuous determinism proof in one container (stance: radical alternative)
Every `trace-tests` run repeats the suite N=5 times (odd runs `GOMAXPROCS=1`), with `Expect: ANY` and a per-run cache-bust env; the M8 test inside the same function; separate nightly with `repeat=20`; M2 evidence by dispatch with `repeat=100`.

### Verified claims (path:line)
1. **Dagger is the CI runner for every check.** Each `check.yml` job is checkout + `setup-dagger` + `make <target>` (`.github/workflows/check.yml:27-40,42-55,57-72,77-111`); the targets are `$(DAGGER) call ...` (`Makefile:171-178,201-207`). No workflow installs Go natively. So all three options, and the hybrid, need Docker for `make trace-test` locally, like every other check target. No option gains or loses here.
2. **There is no unit-test job in CI at all.** No `go test` appears in `.github/` or in a Dagger function except the benchmark build (`dagger/bench.go:129`) and the never-failing golden stamp (`scripts/golden:27`, run by `dagger/golden.go:17-41`). So D4's "CI runs `TestSurface` as part of the normal `go test`", D12's `-race` and seam tests, D2's "e2e tests are the guards", and M8's "table test in CI" all have nowhere to run today. None of the options noticed this. B and C put the M8 test inside `TraceTest`, which would make `trace-tests` red for a non-trace reason (against Goal 1).
3. **Exit codes across the Dagger boundary.** A `WithExec` that exits non-zero fails the call and the `dagger` CLI exits 1 with the output inside its error rendering (B's con is right in effect). The repository already has the fix: `goldenScript` runs the work with `|| rc=$?`, writes `stamp.log` and `stamp.rc` into `/out`, and always `exit 0` (`dagger/golden.go:17-32`); `golden-check` exports the directory and reads it on the host (`Makefile:201-207`). Separately, `make` itself exits 2 on any recipe failure, but prints `Error <n>` with the recipe's code. So the process exit code that reaches Actions is never 3 or 4 in any option; the class must be printed, and only a non-zero/zero distinction matters for the job. A relies on Dagger's error text and is weakest here; C's `Expect: ANY` keeps the code but still returns it through a string.
4. **Cache.** `+cache="never"` is the precedent for uncached calls (`dagger/check.go:39,57`; `dagger/bench.go:34,55`), and `Record` busts the exec cache with a timestamp env (`dagger/main.go:113-115`). Both are needed: the function marker alone does not stop identical `WithExec` layers being reused inside one call.
5. **Nightly placement.** The only cron in `check.yml` is weekly (`:19-20`), and it triggers all four jobs including `golden`. A daily line there (A) runs lint, vulncheck, zizmor and golden every night. A separate workflow (B, C) keeps `check.yml` and `golden` untouched. The repo already has a scheduled workflow that opens or nudges an issue for its owner (`.github/workflows/dependency-check.yml:15-48`, `issues: write` scoped to that job), which is the notification precedent; artifact upload precedent is `bench.yml:86-90`.
6. **Shells in the image.** `bash` exists in `golang:1.26` (`dagger/golden.go:39` runs `bash -c`), `sh` is Debian's dash. `zsh` is not in the image (all three explorers assume this; not contradicted). It must be apt-installed in whatever container runs M8.
7. **`shop`.** It is its own module (`demo/workloads/shop/go.mod`), so it is built with `cd demo/workloads/shop && go build`. The Dagger source excludes docs and Markdown but not `testdata/` or `demo/workloads` (`dagger/main.go:36-38`). Margins for today's cases: amex 450ms vs 200ms (2.25×), visa 60ms vs 200ms (3.3×) (`demo/workloads/shop/main.go:157-168`).
8. **Discounted:** C's "1% flake blocks ~5% of PRs" is arithmetic, correct (1 − 0.99⁵ ≈ 4.9%), and counts against C. B's and C's "dispatch the new nightly workflow on the PR branch" is not possible as written: GitHub only dispatches a workflow whose file exists on the default branch, and in their plans `trace-nightly.yml` first appears in that same PR. A's dispatch of `check.yml` avoids that but has no inputs to set the count.

### Scores (decider; weight × score / 5)
| Criterion (w) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| Determinism (30) | 3 | 4 | 2 | 4 |
| Parity (20) | 5 | 5 | 5 | 5 |
| Diagnosable (15) | 2 | 2 | 3 | 5 |
| No CI edit (15) | 5 | 5 | 5 | 5 |
| Cost (10) | 2 | 4 | 2 | 4 |
| Golden untouched (10) | 3 | 5 | 5 | 5 |
| **Weighted /100** | **69** | **83** | **70** | **92** |

A's determinism is 3: stop-at-first gives no flake rate, and the evidence path is local. C's is 2: per-PR repeats multiply the chance a flake blocks an unrelated PR, which is the exact risk R7 names, and folding M8 into the gate makes it red for non-trace reasons. B's diagnosability is 2 because, as written, it returns the report through a Dagger error.

## Decision
Chosen: **Hybrid, B as the base.** B's Dagger function, Makefile target, `check.yml` job and separate nightly workflow, with: golden's export-and-read pattern so the report and exit class survive the Dagger boundary; per-PR count fixed at 1 (C's per-PR repeat rejected); all N runs executed with a flake count (not stop-at-first); a separate `test` job for `go test -race`, M8, `TestSurface` and the D12 seams (moved out of `TraceTest`); an issue on nightly failure; and a two-PR rollout so the 100-run evidence comes from CI runners. Specification:

1. **Dagger function** `dagger/tracetest.go`: `func (m *Demos) TraceTest(ctx, count int /* +default=1 */, gomaxprocs string /* +optional */) *dagger.Directory`, marked `+cache="never"`. From `goContainer(m.Source)` (`dagger/main.go:78-85`): `CGO_ENABLED=0 go build -o /usr/local/bin/praytel ./cmd/praytel`; `cd demo/workloads/shop && go build -o /usr/local/bin/shop .`; `WithEnvVariable("TRACE_TEST_AT", time.Now().Format(time.RFC3339Nano))` (as `dagger/main.go:113-115`); `GOMAXPROCS` set only if given. It runs one `sh -c` script that never fails (as `dagger/golden.go:17-32`):
   - for `i` in 1..count: `praytel test --grpc-port=0 --http-port=0 testdata/traces >/out/run-$i.log 2>&1 || rc=$?`; append `run i/N: exit <rc>` to `/out/runs.txt`. **All N runs execute**; there is no early stop, so the nightly and evidence runs yield a flake count.
   - `/out/report.txt`: the full log of the first failing run (or of run 1 if all passed), then `runs.txt`, then `praytel: <k> of N runs failed` (or `N of N runs passed`).
   - `/out/rc`: the first failing run's exit code, else `0`. `exit 0`.
   Cases name `shop` by PATH; no case needs `SHOP_BIN`. No `go test` runs in this function.
2. **Makefile** (next to the golden targets; header comment gains the lines):
   ```
   TRACE ?= trace-out
   trace-test:
   	@set -e; rm -rf $(TRACE); \
   	$(DAGGER) call trace-test $(if $(COUNT),--count $(COUNT),) $(if $(GOMAXPROCS),--gomaxprocs $(GOMAXPROCS),) export --path $(TRACE); \
   	cat $(TRACE)/report.txt; \
   	rc=$$(cat $(TRACE)/rc); \
   	case $$rc in 0) ;; 1) c='usage error, no case ran';; 3) c='INCOMPLETE';; 4) c='FAIL';; 130|143) c='interrupted';; *) c='praytel crashed';; esac; \
   	[ "$$rc" = 0 ] || { echo "make trace-test: praytel test exited $$rc ($$c)"; exit $$rc; }
   ```
   The report is printed by the host's `cat`, so it is never truncated or wrapped by Dagger's error output. `make` exits 2 on any failure and prints `Error <rc>`; the class line names it. If `dagger call` itself fails (a build error), there is no `praytel test exited` line, which tells an infrastructure or build failure apart from a verdict. `trace-out/` goes in `.gitignore` beside `golden-out/`. `COUNT` keeps its bench meaning, "more rounds" (`Makefile:42`).
3. **Per-PR job** in `check.yml`: `trace-tests`, the `lint` steps (`:27-40`) running `make trace-test` (count 1, so S5's "one invocation" holds for the gate), `timeout-minutes: 15` (D2), no `continue-on-error`, no path filter (a path-filtered required check stays pending). A final step `if: always()` appends `trace-out/report.txt` to `$GITHUB_STEP_SUMMARY` in a code block, or "the trace tests could not run; see the log" if absent (shape of `check.yml:113-120`). Every non-zero code, including 3, fails the job; there is no retry. A 130/143 can occur only on cancellation, where Actions already shows the job as cancelled (D10's "treat as cancelled" needs no code). `golden` (`:74-120`) is not edited, and `check.yml`'s triggers are not edited.
4. **`test` job** in `check.yml` (new, failing): `make test` → `$(DAGGER) call test` → `goContainer` + `apt-get install -y --no-install-recommends zsh` + `PRAYTEL_REQUIRE_SHELLS=bash,zsh,sh` + `go test -race -count=1 ./...`, `+cache="never"` not needed (pure function of source). It runs D4's `TestSurface`, D12's S1-S4 seam tests (`TestCheckReentrant` under `-race`), D10's M8 `Expand` table, D1's CheckSpan-order test and the existing `cmd/praytel` e2e tests that D2 relies on as guards. The M8 test skips a missing shell unless `PRAYTEL_REQUIRE_SHELLS` names it, so it runs on any laptop and is strict in CI. Fallback, decided now: if pre-existing packages fail under `-race` when this job first lands, the job runs `go test -count=1 ./...` plus `go test -race -count=1 ./internal/praytest/... ./internal/selector/... ./internal/testrun/...`, and an issue is opened for the pre-existing failures. Kept separate from `trace-tests` so that `trace-tests` is red only for trace facts (Goal 1).
5. **Lint**: D4's depguard rule goes into `.golangci.yml`; the existing `lint` job (`check.yml:27-40`) enforces it with no workflow change. It lands in the same PR as, or before, `internal/selector` (D12).
6. **Nightly** `.github/workflows/trace-nightly.yml` (separate; `check.yml` is not touched): `schedule` daily (for example `"23 5 * * *"`) and `workflow_dispatch` with inputs `count` (default 20) and `gomaxprocs` (default empty). On schedule it runs two steps, `make trace-test COUNT=20` and `make trace-test COUNT=20 GOMAXPROCS=1` (the second `if: always()`, with `TRACE=trace-out-1`); on dispatch it runs one step with the inputs, passed through `env` (no `${{ }}` in `run:`, for zizmor). `timeout-minutes: 120`. Logs are uploaded with the pinned `actions/upload-artifact` (`bench.yml:86-90`) `if: always()`, and the step summary gets both `report.txt` files. A final job, `if: failure() && github.event_name == 'schedule'`, with `issues: write` on that job only, opens an issue labelled `trace-nightly` assigned to the owner with the run URL and the `runs.txt` lines, or comments on the open one (copying `dependency-check.yml:29-48`). This is the M7 record; email is not relied on.
7. **Rollout, two PRs** (because a new workflow cannot be dispatched before it is on `main`):
   - **PR 1**: `shop -card` (A5), `testdata/traces/shop.praytest.star`, `dagger/tracetest.go`, `make trace-test`, `trace-nightly.yml`, the `test` job and depguard if not already in. No `trace-tests` job yet.
   - After merge, the author dispatches `trace-nightly` on `main` twice: `count=100` and `count=100, gomaxprocs=1`. Both must show `0 of 100 runs failed`. Any INCOMPLETE reasons in the logs are recorded (D2's late-span trigger).
   - **PR 2**: adds the `trace-tests` job to `check.yml` and links both runs and the nightly history since PR 1. It changes no code that the evidence ran. Then an admin marks `trace-tests` required (A7), recorded as a CONTRIBUTING step.
8. **Local use.** `make trace-test` needs Docker and Dagger, like `make lint`. For a fast loop without Docker, CONTRIBUTING documents `go build -o bin/ ./cmd/praytel && (cd demo/workloads/shop && go build -o ../../../bin/ .) && PATH=$PWD/bin:$PATH praytel test testdata/traces`. That is a documented command, not a second target, so there is one CI path.

Why it wins on the criteria:
- **Determinism (30):** no retries, `-pace 0s`, a pinned image, cache-busted runs, INCOMPLETE fails, and a flake count from every repeat run. The 100-run evidence is from the same runners and code path that gate PRs, at both GOMAXPROCS settings. The nightly runs both settings. The per-PR job does not multiply flake exposure. Not 5, because nested-container CPU noise on shared runners remains.
- **Parity (20):** one `make trace-test`, one Dagger function, locally and in CI.
- **Diagnosable (15):** the S4 report is printed in full by the host's `cat` and repeated in the step summary, with a class line, using a pattern the repo already runs (`golden-check`).
- **No CI edit (15):** directory argument (D10).
- **Cost (10):** one run per PR, in parallel with the other jobs; the nightly costs about 40 suite runs a day; the evidence is two one-off dispatches.
- **Golden (10):** separate job, separate nightly workflow, `check.yml` triggers unchanged.

We give up:
- **A single PR.** S5 says the PR that adds the job includes the evidence; here PR 2 links evidence produced on `main` after PR 1. Trace tests are on `main` ungated for the days between the two PRs.
- **Docker-free `make trace-test`.** Local runs of the target need Docker, unlike `make golden` (`Makefile:197-198`). The native command is documentation only.
- **Exit codes as process codes.** `make` collapses every failure to 2. The class is a printed line (and `make`'s `Error <n>`), not the process status. Anything that needs 3 vs 4 as a status must read `trace-out/rc`.
- **C's continuous per-PR proof.** A case that flakes 1% can merge and be caught only by the nightly, up to a day later.
- **Wall-clock and CPU realism.** All timing evidence comes from a nested container on a shared 2-vCPU runner, not a laptop or macOS.
- **A's 40-line diff.** This adds a `test` job, a nightly workflow and an issue step, roughly 150 lines of YAML and Go.
- **Fail-fast on repeats.** A broken suite still runs all N repeats in the nightly and evidence runs. That is bounded by `timeout-minutes: 120`.

Rejected:
- A: the daily cron in `check.yml` reruns lint, vulncheck, zizmor and golden every night, the report survives only inside Dagger's error output, stop-at-first gives no flake rate, and no one is told when the nightly fails.
- B: the chosen base. Its report path through a Dagger error, its M8 test inside the gate, and its evidence-by-dispatch on a workflow not yet on `main` are fixed.
- C: repeating the suite in every required run multiplies the chance a flake blocks an unrelated PR (R7), and adds about N times the CI time per PR, to buy a proof the nightly already gives within a day.

Revisit if:
- **Flake:** any `trace-tests` failure on `main` or a PR, or any nightly issue, is traced to nondeterminism and not a code change. Apply R7 (fix or delete the case in a PR that unblocks `main`). If ≥ 2 such events in 30 days, reopen D11 and consider C's per-PR repeat or wider margins, and check D2's late-span trigger.
- **Evidence:** either 100-run dispatch shows ≥ 1 failure. Then PR 2 does not open until the cause is fixed and both dispatches pass again.
- **Diagnosability:** a red `trace-tests` run has no `report.txt` in its summary while `praytel test` ran. Then fix the export path before anything else.
- **Per-PR time:** the `trace-tests` job's median wall time over 20 PRs is more than 3 minutes above the slowest other `check.yml` job, or above 8 minutes. Then cache the built binaries across runs, or move this one job to a native `setup-go` runner (a parity trade the human must accept).
- **-race:** the `test` job's median exceeds 10 minutes, or `-race` causes ≥ 2 failures in a month not reproducible locally. Then narrow `-race` to the item 4 fallback package list.
- **Dispatch rule:** if GitHub allows dispatching a workflow that is not yet on the default branch, collapse the rollout into one PR (cheap).
- **Nightly silenced:** GitHub disables the schedule (60 days without repository activity) or no nightly ran in the last 7 days. Re-enable it and note it in the M7 record.
- **130/143 refused** by the human (D2): no change here; item 2's class line maps 3 instead.

Consequences for other decisions:
- **D2 (decided):** consistent. `--grpc-port=0 --http-port=0` in the target; `timeout-minutes: 15` per PR as the self-hang backstop; INCOMPLETE reasons are kept in each run log and the evidence runs. D2's "e2e tests are the guards" is now true in CI only through the new `test` job.
- **D4 (decided):** depguard rides the existing `lint` job; `TestSurface` runs in the new `test` job. D4 assumed a "normal `go test`" in CI that did not exist; item 4 creates it.
- **D10 (decided):** CI consumes only the report bytes and the exit code. No machine-readable output is needed, so D10's "second output consumer" trigger is not fired. 130/143 need no CI handling.
- **D12 (decided):** `-race` and the S1-S4 seam tests run in the `test` job; depguard lands before or with `internal/selector`.
- **D1 (decided):** the CheckSpan-order-across-runs test runs in the `test` job, not in `trace-tests`.
- **D9:** the VHS S1 recording (M1) is built by `demos.yml` (`make docs`), not by D11.
- **D5, D6, D7, D8 (decided):** no conflict; their unit tests now run in CI via the `test` job.
- **Task planning:** the `test` job and depguard land before or with the first `internal/praytest` package. PR 1 and PR 2 in item 7 are separate tasks, with the evidence dispatch and the A7 admin step as explicit checklist items.

**Vision amendments required** (for the human):
- **S5, first paragraph:** "`make trace-test` runs `praytel test --grpc-port=0 --http-port=0 testdata/traces`: one invocation, no shell loop." → "`make trace-test` runs `praytel test --grpc-port=0 --http-port=0 testdata/traces` once, inside the Dagger module's pinned Go container with `praytel` and `shop` on `PATH`, like every other check target. `make trace-test COUNT=N` repeats it N times for M2 and the nightly run."
- **S5 acceptance:** "The PR that adds the job includes M2's 100-run evidence." → "The PR that adds the `trace-tests` job links M2's 100-run evidence: two `trace-nightly` runs on `main` of the same code (count 100 at default settings, and count 100 with `GOMAXPROCS=1`), made after the target and the nightly workflow merged in an earlier PR."
- **M2 target:** "A nightly scheduled run repeats 20× and must stay green." → "A nightly scheduled run repeats 20× at default settings and 20× with `GOMAXPROCS=1` and must stay green. A failing nightly opens or updates a `trace-nightly` issue."
- **A7 (proposed, the human's call):** "...will mark `trace-tests` as required..." → "...will mark `trace-tests`, and the new `test` job that runs `go test -race ./...`, as required..."
