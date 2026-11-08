## Option A: One praytel subprocess per case (re-exec), parent orchestrates   (stance: radical alternative)
Summary: Drop the premise that `praytel test` runs the pipeline in-process. The parent never binds a port. For each case it re-execs its own binary in a hidden mode that runs today's `run()` unchanged, with one extra renderer that writes `{child_code, CheckView}` to fd 3. The parent evaluates checks, owns the timeout and maps outcomes. Isolation comes from the process boundary, not from careful teardown.

How it works:
- `cmd/praytel/run.go`: add one `switch` arm next to `cfg.Snapshot` (`run.go:61-75`): a hidden `--case-result-fd=3` selects `caseresult.New(events)`. Its `Run` calls `tui.Settle` (D1). Its `Finish(code)` writes a JSON record to `os.NewFile(3)` and returns `code`. Child launch, env (`run.go:216-221,277-288`), drain and hold (`run.go:157-173`) are untouched.
- New `internal/caseresult`: encodes and decodes `CheckView`, with typed `domain.Value`s tagged by kind. Parent and sub are always the same binary (`os.Executable()`), so the format is unversioned.
- New runner (D10 places it): `exec.Command(self, "--case-result-fd=3", ports, drain flags, "--", cmd...)` with `ExtraFiles` for the result pipe, `SysProcAttr{Setpgid: true}`, `Dir` = the directory `praytel test` was run from, and `Stdin` = nil (`/dev/null`). Nothing in the repo sets a process group today (grep finds no `Setpgid`/`SysProcAttr`).
- Timeout: one `context.WithTimeout(case.timeout)` covers sub start, child, drain and the Starlark check in the parent. On expiry: SIGTERM to `-pgid` (the sub forwards it and drains, `run.go:245-275`). After a 2s grace: SIGKILL to `-pgid`. Result: `INCOMPLETE` (exit 3). The step cap applies to the check, giving 4.
- Outcomes in the parent: no result record means INCOMPLETE ("case runner died"). If `child_code != exit_code` → 4. If a check fails → 4. If `len(Problems) > 0` → 3. 4 wins.
- After every case, SIGKILL `-pgid` to sweep stragglers before the next case binds.
- Ctrl-C: the sub is in its own process group, so only the parent gets the terminal's SIGINT. The parent forwards it to `-pgid`, waits for the drain, reports the case as INCOMPLETE "interrupted", skips the remaining cases and exits 3 (or 4 if a failure was already reported).
- Record mode stays in-process (`run` with `cfg.TUI`) plus a `childExited` channel closed after `startChild` returns (`run.go:147`).

Steelman: The `praytel -- cmd` code path is the test executor, byte for byte. That is the strongest possible form of "nothing changes meaning". Isolation is enforced by the OS: ports close, goroutines die, and a process-group SIGKILL removes forked daemons that an in-process teardown would leave holding 4317. The timeout is enforced even if the sub deadlocks inside `Settle` or a renderer, because the parent never shares its goroutines.

Pros: hang-proof timeout. Isolation by construction. Zero teardown bugs to find. `run()` is reused instead of being refactored into a reusable per-case function.

Cons / failure modes / hidden costs:
- New internal wire codec for `CheckView`, with typed attributes. It has to round-trip exactly or the checks diverge from `--snapshot`.
- Two levels of signal forwarding, and stderr from two praytel processes interleaves. praytel's own spans (CONTRIBUTING tracing rule) need context passed across the exec.
- Unit tests need a TestMain re-exec trick, because `os.Executable()` is the test binary.
- Record mode does not share the orchestrator, and v1.1 watch would need streaming over fd 3. "Same lifecycle" holds only at the `run()` level.
- `Setpgid` changes terminal semantics: a child that reads the TTY gets SIGTTIN. That is mitigated only by forcing stdin to `/dev/null`.
- Per-case exec plus Go runtime init plus JSON comes on top of the existing 50ms hold (`run.go:166`).

Criteria scores:
- Determinism: 5 - process boundary plus pgid sweep; late spans hit a closed port, never the next case.
- S3 exit table: 4 - clean parent-side precedence; the "runner died" case is an added state outside the table.
- Existing behaviour unchanged: 5 - only a hidden flag and one renderer arm.
- Bounded runtime: 5 - an external deadline and SIGKILL do not depend on the sub cooperating.
- Record/watch: 2 - record bypasses the orchestrator; watch needs a streaming protocol.
- Per-case overhead: 3 - estimated 5-20ms exec and init per case (unmeasured, macOS worst).

Cost: M     Reversibility: easy (hidden flag and runner are internal; the codec is private)

Unknowns / assumptions:
- Exec overhead on macOS CI against M4's 50ms budget.
- Whether D5/D6 would rather run Starlark in the sub, which would ship a verdict instead of a `CheckView` but load the file N+1 times.
- Exit code on Ctrl-C (3 vs 130) is not in the S3 table.
- Children that call `setsid` escape the sweep.
