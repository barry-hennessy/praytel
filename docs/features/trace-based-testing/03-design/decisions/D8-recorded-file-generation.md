# D8: How does --record turn picks into a Starlark file, and what does the generator guarantee about it?
Status: decided     Depends on: D5, D7     Parent: none
Vision refs: S1 (example file, acceptance, edge cases), S6 (repro files), §6 P3 row; R5, R9, R12; M1, M3

## Context
- S1 fixes the output shape: a header comment (recorded command, "Starlark ... Edit freely", run hint), module-level selector constants named from the span (`CHARGE`), one `def check_<name>(run)` per case, one `test(...)` call with `command` equal to the argv after `--`, unchanged.
- Acceptance: the file parses, contains no trace/span IDs, timestamps, durations or ports, and passes `praytel test` on the unchanged command. M3 tests this over every `shop` span.
- Attribute values can themselves be volatile (ports, temp paths, ids); `--snapshot` leaves this to the user via `--snapshot-attrs="-key"` (`docs/reference/praytel.1.md:116-117`, `internal/attrspec/attrspec.go:12-46`). The recorder makes duration, timestamps and IDs un-tickable (S1 step 3), but an attribute like `server.port` is tickable unless the generator or recorder classifies it. Whether this is a structural guarantee or a heuristic is open.
- Value formatting must round-trip D7's type rules (int vs float vs string attribute values; status as a string).
- The `# WARNING: command was still running ...` line appears iff the child was running at write time (S1).
- Refuse to overwrite (S1 step 1) is checked before the child starts, but the file is written much later at Ctrl-W; another process may create it in between. A partial write must not leave a broken file.
- Absence picks emit a selector "for the user to edit" (S1 step 4).
- Format marker: generated files carry a format comment; the format is `v0` (R9).
- v1.1 writes repro files with the watched command and the violated property (S6), so the generator may be called without a TUI.
- Recording cannot append a case to an existing file (overwrite is refused); multi-case files are produced by editing (S2).

## Goals
- Same picks produce the same bytes.
- The written file is idiomatic, readable and editable like the S1 example.
- The written file is guaranteed (not merely expected) to satisfy S1's acceptance, or the write is refused with a reason.

## Non-goals
- Whole-trace capture, a bless/`--update` flag (§6 P3 row, §7).
- Appending to existing files.
- YAML/JSON export (§7).

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| Zero volatile values in output | 25 | M3; R5; S1 acceptance |
| Generated file passes on the unchanged command | 20 | S1 acceptance |
| Readable, idiomatic, editable | 20 | §3 "code you own"; M1 |
| Safe write: no overwrite race, no partial file | 15 | S1 step 1, step 5 |
| Byte-stable output, golden-testable | 10 | M3 generator test; R9 |
| Reusable for v1.1 repro files without a TUI | 10 | S6 |

## Options
Full texts: `../options/D8-A.md`, `D8-B.md`, `D8-C.md`. All three share the same skeleton: a pure function from a `Recording` value to bytes, typed literals, selectors via D7's `Selector.Source()`, a heuristic attribute denylist, an in-process load + dry run before writing, and a temp file published with `os.Link`. They differ on where the code lives, how text is produced (own quoter / interpreter quoter / syntax tree + printer), how wide the denylist is, and the repro seam. Scores below are the decider's; explorers' self-scores in brackets where changed.

### Option A: Codegen emitter in a Starlark-free package, wide deny-list, live preview, link write (most conventional)
`internal/recordgen.Render(Recording)`, own literal quoter, a separate `# praytel-format: v0` line, deny-list by key (`*.id`, `*.port`, `process.pid`, ...) and value pattern, enforced in the recorder and again in `Render`; parse + replay check; temp + `os.Link`; live source preview in the pick panel. Cost M.

### Option B: Fprintf generator in `internal/praytest`, narrow keyword refusal, self-load check, link write with specified fallback (minimal change)
`internal/praytest/gen.go`, `Generate(Recording)`, strings via `starlark.String(s).String()`, header line 1 `# praytel test format v0. Recorded with: ...`, narrow key/value denylist, `LoadFile` + check against the current `CheckView`, temp + `os.Link` with an `O_EXCL` fallback that removes the target on write error. Cost S.

### Option C: Syntax tree + own printer, verify-before-write gate with taint scan (radical alternative)
`Build(Recording) *syntax.File` + a hand-written deterministic printer (go.starlark.net has none; buildtools is a new dependency), `Verify` = parse + load + dry run + literal taint scan (exact match against the run's IDs/timestamps/ports plus a key list reusing `attrspec`); repros reuse `Verify` with the dry run expected to fail. Cost M.

### Verified claims and corrections
- **The run's IDs are not available to the generator.** D1's `CheckSpan` has no trace or span ID fields, by design ("No trace or span IDs are exported (R5, M3)", D1 decision item 4). C's exact-equality taint against "a recorded trace or span ID" therefore needs a D1 change. Not credited. The flip side helps every option: IDs cannot reach the generator structurally, because its input type is built from `CheckSpan`.
- **A's `*.id` key rule refuses stable values.** `shop`'s root carries `order.id` (`demo/workloads/shop/main.go:95`), the attribute D7's trace description is built on and S2/S4 print. A also lists `card.id` as a false positive. Discounted on volatility (a refusal rule that blocks the most useful stable key is wrong, not merely conservative).
- **`shop` has no volatile attributes.** Its span attributes are `order.id`, `customer`, `items`, `card.type` (`demo/workloads/shop/main.go:95,124,143`). So M3 "over every `shop` span" is passed by any option's structural rule alone and does not exercise the denylist. The denylist needs its own per-entry tests.
- **`starlark.String.String()` re-parses (B).** Upstream implements it as `syntax.Quote`, but there is no `go.starlark.net` in the local module cache (also noted in D7), so not verified here. Not relied on: see the quoter decision below.
- **No option handles absence picks correctly.** An absence pick (`a`) is made on a span that exists in the run being recorded, so `expect.absent(run, <that span's selector>)` fails on the recorded run by construction. With A, B or C as written, the dry run would refuse every file that contains an absence pick; without the dry run the file would fail S1's acceptance ("passes on unchanged `shop`"). This is a contradiction inside S1 (step 4 vs acceptance), resolved below with a vision amendment.
- **Link reasoning is correct.** `rename(2)` replaces an existing target atomically, so it would overwrite a file created between the start-up check and Ctrl-W. `link(2)` fails with `EEXIST` if the new name exists, and the name appears atomically pointing at an already-written, fsynced inode, so no reader ever sees a partial file under the target name. Same directory, so no `EXDEV`. C's "no partial file" holds only on the link path; its fallback (`O_EXCL` + write) has the same crash caveat as B's and A's.
- **Live preview (A)** is a pick-panel feature, i.e. D9's question, not D8's. Not scored here.

### Scores (weight x score / 5)
| Criterion (w) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| Zero volatile (25) | 3: structural for IDs/time/duration; wide list refuses `order.id` | 3 | 3 [4]: exact ID taint needs IDs D1 does not export | 3 |
| Passes unchanged (20) | 3 [4]: dry run refuses every absence pick | 3 [4]: same | 3 [4]: same | 4 |
| Readable (20) | 5 | 5 | 4 | 5 |
| Safe write (15) | 4 | 5 [4]: fallback fully specified with cleanup | 4 [5]: fallback has the crash caveat it does not admit | 5 |
| Byte-stable (10) | 5 | 5 | 5 | 5 |
| Repro reuse (10) | 4 | 3 | 4 | 4 |
| **Weighted /100** | **77** | **78** | **73** | **84** |

## Decision
Chosen: **hybrid, B as the base**: B's string-built generator inside `internal/praytest` with its self-check and link write; D7's single pure-Go literal quoter instead of B's interpreter quoter or A's second quoter; a narrower, specified volatility classifier shared with the recorder (A's two layers, without `*.id`); absence picks emitted commented out; C's "verify the bytes" stance kept as the self-check, without C's syntax tree, printer or ID taint. Specification:

1. **Package and entry points.** `internal/praytest/gen.go` (D4: the self-check needs `Load`, and only this package touches the interpreter). TUI-free: inputs are plain Go values plus D1's `CheckView`.
   ```go
   type Recording struct {
       File         string           // FILE as given to --record
       Argv         []string         // argv after --, unchanged
       StillRunning bool             // from D2's childRunning at Ctrl-W
       ExitCode     *int             // child's code if it has exited, else nil
       Spans        []PickedSpan
   }
   type PickedSpan struct {
       Index     int                 // index into CheckView.Spans (canonical order)
       Selector  []string            // attr keys marked with s
       SelStatus bool                // status marked with s
       Asserts   []string            // "status", "status_message", or "attrs.<key>" ticked with Space
       Absent    bool                // marked with a
   }
   func Generate(rec Recording, view tui.CheckView) ([]byte, error)          // pure, no I/O
   func SelfCheck(ctx context.Context, src []byte, rec Recording, view tui.CheckView) error
   func WriteNew(path string, src []byte) error
   func Volatile(key string, v domain.Value) (reason string, volatile bool)
   ```
   Values come from `view` at the picked index, not from the TUI's display strings, so the file holds exactly what D7 compares (int stays int, status as `"unset"/"ok"/"error"`, raw `status_message`). There is no field through which a timestamp, duration or ID can be requested: that is the structural half of the volatility guarantee.
2. **Text production: plain string building, one quoter.** A `strings.Builder` with fixed templates. No syntax tree, no printer, no new dependency: the generator emits one fixed shape and a syntax tree buys nothing a re-parse does not already check. All literals go through one pure-Go function in `internal/selector` (D7's package, which already needs it for `Source()`): `selector.Literal(domain.Value) (string, error)`.
   - Strings: double-quoted; `\\`, `\"`, `\n`, `\r`, `\t` escaped; other non-printable runes as `\uXXXX`/`\UXXXXXXXX`; printable runes verbatim. Invalid UTF-8 is refused (`card.foo: value is not valid UTF-8`), because Starlark string literals cannot carry raw high `\x` bytes.
   - Ints: `strconv.FormatInt`. Floats: `FormatFloat(f, 'g', -1, 64)`, `.0` appended when the result has no `.`, `e` or `n`; NaN/±Inf refused. Bools: `True`/`False`.
   - Arrays, maps, bytes: not tickable and refused by `Generate` (D7).
   - Guard: a fuzz test in `internal/praytest` parses `x = <Literal(v)>`, evaluates it, and asserts it equals `v` under D7's equality. The self-check (item 6) also catches any residual quoter bug before a byte is written.
3. **Exact layout** (S1's shape; every line below is fixed text except `<...>`):
   - Line 1: `# praytel test format v0. Recorded with: praytel test --record=<FILE> -- <argv>` (see items 8, 9).
   - Line 2: `# Starlark (Python-like). Edit freely. Run: praytel test <FILE>`
   - Iff `StillRunning`: `# WARNING: command was still running when recorded; praytel test waits for it to exit (timeout 10m).`
   - Iff `Argv[0]` contains `/` and is not absolute (D2): `# command paths are relative to the directory praytel test is run from`
   - One constant per distinct picked span box, in first-use order over the sorted picks: `<NAME> = spans(service = "...", name = "...")`, from `Selector.Source()` of the box-only selector. `<NAME>` is the last `.`-segment of the span name, upper-snake (`Gateway.Charge` -> `CHARGE`); characters outside `[A-Za-z0-9]` become `_`, runs collapse, a leading digit or empty result gets `SPAN_`; a collision with a different box gets `_2`, `_3`.
   - A blank line, then `def check_<id>(run):` where `<id>` is the FILE stem (basename minus `.praytest.star`) sanitized the same way, lower-case. The case `name` is the stem verbatim as a string literal, so no FILE is refused for its name.
   - Body, picks sorted by `Index` (canonical, content-ordered per D1), fields in fixed order (status, status_message, attrs by key), so the same picks in any pick order give the same bytes:
     - Span with assertions: `span = run.one(<NAME>[.where(status = ..., attrs = {...})])` then one `expect.eq(span.<field>, <literal>)` per tick (`span.attrs["k"]` for attributes). With more than one asserted span the variable is the lower-case constant name (`charge`, `bank_authorize`) instead of `span`.
     - Span with selector marks only: `run.one(<NAME>.where(...))` as a statement (it fails on 0 or >1 matches).
     - Absence pick: two comment lines, `# Edit before enabling: this span exists in the recorded run, so the line below would fail as recorded.` and `# expect.absent(run, <NAME>[.where(...)])`. See item 7.
   - A blank line, then `test(name = "<stem>", command = [<argv literals>], check = check_<id>)`. No `exit_code` or `timeout`, except that if the child has already exited with a non-zero code, `exit_code = <code>` is emitted so the file passes on the unchanged command.
   - Trailing newline; LF line endings; 4-space indent; S1's `k = v` keyword spacing.
   This satisfies D5 (top-level `test()`, no `env()`, no `load()`) and D4 (no global reassignment).
4. **Volatility: what is structural and what is heuristic.**
   - *Structural (guaranteed):* trace/span IDs, timestamps and durations cannot appear, because `CheckSpan` carries no IDs (D1) and `PickedSpan` has no way to request `Start`, `End` or `duration_ms`. The generator never emits `duration_ms`.
   - *Heuristic (best effort, documented):* `Volatile(key, value)` classifies attribute values. Key globs (`path.Match`, as `attrspec`): `port`, `*.port`, `*_port`, `pid`, `*.pid`, `*_pid`, `thread.id`, `*.time`, `*_time`, `*timestamp*`, `*_unix_nano`, `*duration*`, `*elapsed*`. String value shapes: exactly 16 or 32 lower-case hex digits (span/trace ID shape), a UUID, an RFC 3339 date-time prefix (`dddd-dd-ddTdd:dd`), `host:port` or a URL with an explicit port, and a prefix of `os.TempDir()`, `/tmp/`, `/var/folders/` or `/private/var/folders/`. Deliberately **not** `*.id`: it would refuse `order.id`, which is stable and is what D7's trace description uses.
   - Enforcement in two places: the recorder (D9) shows such attributes but makes them untickable with the reason (`looks volatile: port`), like duration and IDs today; `Generate` re-checks and returns an error naming the key, so a recorder bug cannot write one.
   - The list lives in `praytel-test.1` (it is generator behaviour, not file format), so changing it is not a format change. App-specific volatile values (`order.ref = "A93F"` generated per run) are not detected; the guide tells authors to delete such lines.
5. **M3 wording.** "Zero volatile values" cannot be guaranteed for arbitrary attribute values; any claim otherwise would be false. The vision is amended (below) to state the structural guarantee and the documented heuristic separately. M3's test becomes: generate a file for every `shop` span with every tickable field ticked, assert the self-check passes and a regex scan finds no ID-, timestamp- or duration-shaped text; plus one unit test per volatile-list entry (positive, and a near-miss negative such as `order.id`, `report`).
6. **Self-check before every write (cheap: one ~63us load plus one check over the live view).** `SelfCheck` runs `praytest.LoadSource(ctx, rec.File, src, emptyGetenv)` (a bytes variant of D5's `Load`), requires zero `UsageError`s, exactly 1 case, `Command` equal to `rec.Argv` element for element, then runs that case's check against `view` through D6's check method and requires no failures. Any failure refuses the write; the status line reads `not written: <reason>` and the picks are kept. This proves "parses" and "passes" against the recorded run. It does not prove the file passes on a rerun: a value that differs between runs and is not on the volatile list still yields a flaky file. Additionally, ticking `status` or `status_message` on a span with `Closed=false` is refused (`span still open; its status is not final`), because the value can change when the span ends.
7. **Absence picks.** Emitted commented out (item 3), so the file passes as recorded and the user still gets the selector to edit, which is what S1 step 4 already intends ("for the user to edit"). The status line counts them: `wrote gift.praytest.star (1 case, 2 assertions, 1 absence to edit)`. Vision amendment below.
8. **Header argv quoting.** In line 1, `<FILE>` and each argv element print bare if they match `^[A-Za-z0-9_@%+=:,./-]+$`; otherwise single-quoted with `'` written as `'\''`; an empty element prints `''`; an element containing any control byte (including newline) prints as its `selector.Literal` string form, so nothing can end the comment. The comment is informational; `command` is authoritative.
9. **Format marker.** The first line of every generated file is exactly `# praytel test format v0. Recorded with: ...`. It is S1's existing first line with `format v0` inserted, and is greppable as `^# praytel test format (v[0-9]+)`. In v0 the loader ignores it (hand-written files need no marker). When a later format exists, the loader may use it to warn; that is additive.
10. **Write (`WriteNew`).**
    1. `os.CreateTemp(filepath.Dir(path), ".praytel-*.tmp")`; write; `fsync`; close. The `.tmp` suffix means a stray temp file is never picked up by a directory run.
    2. `os.Link(tmp, path)`. `EEXIST` -> refuse with `gift.praytest.star exists; refusing to overwrite`, picks kept (the user can move the other file away and press Ctrl-W again).
    3. Remove the temp file in every case; `fsync` the directory, best effort.
    4. **Fallback** when `Link` fails with `EPERM`, `ENOTSUP`/`EOPNOTSUPP` or `ENOSYS` (FAT/exFAT, some SMB/NFS/FUSE mounts): `os.OpenFile(path, O_WRONLY|O_CREATE|O_EXCL, 0o644)`, one `Write` of the whole buffer, `fsync`, close; on any write/sync error, remove `path`. Still never overwrites (`O_EXCL`). Caveat: a crash or SIGKILL between create and the end of the write leaves a truncated file. **Accepted**: the file is about 1 KB written in one call, the window is microseconds on a rarely used path, and the failure is loud, not silent: a truncated file fails `praytel test` at load with exit `1` and a position, and a re-record is refused until the user deletes it. `renameat2(RENAME_NOREPLACE)`/`renamex_np(RENAME_EXCL)` would close the window but are per-OS; not worth it for v1.
11. **Tests.** Goldens in `internal/praytest/testdata/gen/*.golden` (S1 example, WARNING variant, relative-path variant, non-zero exit, multi-span, absence, collisions, quoting edge cases), regenerated with `-update` like D4's surface golden; the M3 test (item 5); the quoter fuzz test (item 2); `WriteNew` tests for `EEXIST` after a racing create and for the fallback path (injected link error).
12. **v1.1 repro seam.** v1 builds no repro fields (A2 is unvalidated, D12). The seams are: `Generate`/`SelfCheck`/`WriteNew` are TUI-free and take plain values; `Recording` is internal, so v1.1 adds fields (for example a header origin and a property reference that emits `load("../props.star", ...)`, which D5 allows) and a `SelfCheck` mode that expects the check to fail. Both are additive to internal code and change no v0 file.

Why, against the criteria:
- **Zero volatile (25), 3:** the structural half (IDs, timestamps, durations) is a real guarantee because the input type cannot carry them. The attribute half is honest best effort with a narrow, tested list that does not refuse `order.id`. No option can do better without knowing the app; C's exact ID taint needed IDs that D1 deliberately withholds.
- **Passes unchanged (20), 4:** the only design whose self-check does not reject every absence pick, plus the open-span status refusal and `exit_code` emission. Not 5: a rerun can differ.
- **Readable (20), 5:** S1's exact shape, with one added word pair on line 1.
- **Safe write (15), 5:** no-overwrite on every path; no-partial on the link path; the fallback's crash window is stated and fails loudly.
- **Byte-stable (10), 5:** pure function of (picks, view), sorted by canonical index, goldens.
- **Repro reuse (10), 4:** TUI-free functions; the repro fields are deferred, not designed.
- **Reversibility:** everything except line 1 and the S1 shape is internal. The volatile list is documented behaviour, not format.

We give up:
- **A guarantee about attribute values.** An app-specific per-run value (order references, request IDs under unusual keys, a port under `addr`) can still be ticked and written; the file then flakes on rerun. Only the author can catch it.
- **A guarantee about reruns.** The self-check proves the file passes against the recorded run only.
- **Live absence assertions from the TUI.** Absence picks arrive commented out; the user must edit and uncomment them.
- **Ticking attributes on the volatile list,** including false positives such as a stable `checkout.time = "noon"` (matches `*.time`) or a fixed `db.port = 5432`. The author edits the file to add such an assertion.
- **Status assertions on spans that are still open at Ctrl-W.**
- **A's live source preview** (left to D9), **C's syntax tree** and printer (the self-check gives the same safety for less code), and **B's use of the interpreter's quoter** (one pure-Go quoter shared with `Source()` instead).
- **No-partial-file on filesystems without hard links,** under a crash during the write.

Rejected:
- A: its `*.id` rule refuses `order.id`, the key D7's descriptions rely on; a second quoter outside D7's package; the preview is D9 scope; its dry run would refuse every absence pick.
- B: the chosen base, but it relies on the unverified `starlark.String.String()` quoting (a second quoter beside D7's `Source()`), and its self-check would refuse every absence pick.
- C: a syntax tree and hand-written printer (or a new buildtools dependency) for a fixed shape that a re-parse already validates; its exact ID taint needs IDs D1 does not export; it claims "no partial file" while its fallback has the crash window; its dry run would refuse every absence pick.

Revisit if:
- **Volatile escapes:** 1 issue, or 2 M7 dogfooding files, where a recorded file flaked on rerun because of a ticked attribute value. Then add that key/shape to the list, or (if 3 or more such reports) add a "record twice and diff" option that marks values differing between two runs as untickable.
- **False positives:** 2 or more issues asking to tick an attribute the list blocks. Then narrow the offending entry; consider a `--record-attrs` spec reusing `attrspec` syntax.
- **Absence UX:** the M1 guide walkthrough exceeds 2 min because of the commented-out absence line, or 2 issues report it. Then design a pick that records an absence selector the user can narrow in the TUI.
- **Self-check refusals:** any self-check refusal in tests or a bug report that is not a cardinality failure. That is a generator or quoter bug; fix it and add a golden.
- **Fallback hit:** any report of a truncated recorded file, or the fallback path used on a CI or macOS default filesystem. Then use `renameat2(RENAME_NOREPLACE)` (Linux) and `renamex_np(RENAME_EXCL)` (macOS) before the `O_EXCL` fallback.
- **Format change:** the first change to the generated shape after release. Bump the marker to `v1` and have the loader warn on unknown markers.

Consequences for other decisions:
- **D1 (decided):** consistent; relies on `CheckSpan` exporting no IDs. No change requested.
- **D2 (decided):** consistent; uses `childRunning` for the WARNING, emits D2's relative-path comment, and stores argv unchanged per the cwd/stdin rule. The child's exit code (when it has exited) must reach the recorder so a non-zero code is emitted as `exit_code`.
- **D4 (decided):** consistent; code lives in `internal/praytest`, generated files stay within the `TopLevelControl`-only dialect.
- **D5 (decided):** needs an additive bytes entry point, `LoadSource(ctx, name string, src []byte, getenv) (*File, []UsageError)`, sharing `Load`'s code. Generated files use top-level `test()`, no `env()`, no `load()`.
- **D7 (decided):** `internal/selector` exports `Literal(domain.Value) (string, error)` with the rules in item 2, and `Source()` uses it. `Source()` must order fields deterministically (status, then attrs by key, as D7 item 9 already does for `String()`).
- **D6 (open), requirement:** the check method must be callable on an in-memory `tui.CheckView` with no child process and no `caseResult` (for example `(*Case).Check(ctx, view CheckView) []Failure`), and must accept a live view whose spans may have `Closed=false`. D2 already runs the check in-process after `Settle`, so this should cost nothing.
- **D9 (open):** the recorder calls `praytest.Volatile` to make attributes untickable with a reason; refuses status ticks on open spans; builds `Recording`; at Ctrl-W calls `Generate`, `SelfCheck`, `WriteNew` in order and shows `wrote FILE (N case, M assertions[, K absence to edit])` or `not written: <reason>`, keeping the picks on refusal. The S1 cardinality message (`selector matches 2 spans ...`) may come from D9's own `Select` check before `Generate`; the self-check is the backstop. Live preview, if wanted, is D9's choice and can call `Generate`.
- **D10 (open):** none beyond wiring `--record` to D9.
- **D12 (open):** repro generation reuses `Generate`/`SelfCheck`/`WriteNew`, adding internal `Recording` fields and an expect-fail self-check mode in v1.1. v1 builds neither.

**Vision amendments required** (for the orchestrator to put in front of the human):
1. **S1 example, line 1:** replace `# praytel test. Recorded with: praytel test --record=gift.praytest.star -- shop -pace 0s -card gift` with `# praytel test format v0. Recorded with: praytel test --record=gift.praytest.star -- shop -pace 0s -card gift`.
2. **S1 step 3, last sentence:** replace "Duration, timestamps and IDs are shown but cannot be ticked." with "Duration, timestamps and IDs are shown but cannot be ticked, nor can attributes on praytel's volatile list (ports, PIDs, and ID-, time- or temp-path-shaped values; listed in `praytel-test.1`)."
3. **S1 step 4, last sentence:** replace "`a` on another span adds an **absence** assertion with that span's selector, for the user to edit." with "`a` on another span adds a commented-out **absence** assertion with that span's selector, for the user to edit and uncomment (the span exists in this run, so the assertion would fail as recorded)."
4. **S1 Acceptance:** replace "It contains no trace/span IDs, timestamps, durations or ports." with "It contains no trace/span IDs, timestamps or durations, and no attribute value on the volatile list."
5. **M3 row:** replace target "0 IDs, timestamps, durations or ports" and measure "Generator test over every `shop` span" with target "0 trace/span IDs, timestamps or durations (structural: the generator has no input for them); 0 attribute values on the documented volatile list" and measure "Generator test over every `shop` span, plus one test per volatile-list entry".
6. **§6 P3 row:** replace "The generator never emits volatile values." with "The generator never emits IDs, timestamps or durations, and refuses attribute values on its documented volatile list; app-specific per-run values are left to the author."
