## Option A: Codegen emitter with a volatile deny-list, live preview and link-based no-clobber write   (stance: most conventional)
Summary: This follows Playwright codegen, Cypress Studio and goldie. A pure-Go emitter turns a `Recording` value into idiomatic Starlark source. A pick panel shows the source live before Ctrl-W. Volatile values are stopped by a known-keys/value-pattern deny-list, the same normalisation goldie and Jest use. The file goes to a temp file and is published with a no-clobber `link`.

How it works:
- New `internal/recordgen` with no Starlark import (D4). `Render(Recording) ([]byte, error)`. `Recording{Argv []string; File string; StillRunning bool; Cases []Case}`, `Case{Name; Sel selector.Selector; Asserts []Assert{Field string; Value any}; Absent []selector.Selector}`.
- Fixed emission order: the header, then `# praytel-format: v0`, then the WARNING iff `StillRunning`, then the relative-path note (D2). Constants are sorted by name and named from the last span-name segment (`CHARGE`, collisions get `_2`). Then one `def check_<case>` and one `test(...)`, with `command` taken verbatim from `Argv`. Selectors come from `Selector.Source()` (D7). Literals use their own quoter: ints with `strconv.FormatInt`, floats with `'g'` plus a forced `.0` (NaN/Inf refused), strings with an escape set limited to `\" \\ \n \t \uXXXX`. Arrays, maps and bytes are refused (D7).
- Volatile guard with two layers. (1) In the recorder, duration, timestamps and IDs are untickable (S1). Attributes are also untickable if their key is in a table (`server.port`, `client.port`, `net.*.port`, `*.id`, `process.pid`, `thread.id`, ...) or their value matches a pattern (hex/UUID IDs, RFC3339, `host:port`, `/tmp/`, `$TMPDIR`). (2) `Render` re-checks the same table and refuses with `card.foo looks volatile (matches port)`.
- Verification before the write. `praytest.Load(bytes)` proves the file parses and fits the dialect. Then the check runs in-process against the recorded run's `CheckView` spans. A failure refuses Ctrl-W with the reason.
- Safe write: `CreateTemp` in the target dir, write, `fsync`, then `os.Link(tmp, FILE)`, which is atomic and fails with `EEXIST` if someone created FILE since startup. The temp file is removed in every case.
- v1.1 repro: `--watch` builds a `Recording` with no TUI. One extra `Case.CheckSource` field holds the violated property.
- Tests: a golden test per `shop` span (M3), plus a regex scan of the output for IDs, timestamps and ports.

Steelman: every tool users know works this way. The output is plain code, shown before it is written, never overwrites, and normalises the usual suspects. Because a pure function produces the bytes, a golden test can check them, and the repro path reuses the same function. The replay check turns "passes on the unchanged command" from a hope into a property of this run.

Pros: idiomatic output, deterministic, small, and reuses D7's `Source()`. The race-free no-clobber write uses only the stdlib.

Cons / failure modes / hidden costs:
- The deny-list is a heuristic. An app-specific volatile value (`order.ref = "A93F"`) gets through, so M3's "0" holds only for patterns the list knows. The list grows forever and trips false positives (`card.id`).
- Replaying against the recorded run doesn't prove the test passes on a second run, because nondeterministic child output is not caught.
- `os.Link` fails on some filesystems (FAT, some network mounts). That needs a fallback to `O_EXCL` + write, which reopens the partial-file risk.
- The custom quoter must be fuzz-tested against the Starlark parser.

Criteria scores:
- Zero volatile values: 3 - IDs, times and durations are blocked by structure, but ports and paths only by heuristic.
- Passes on unchanged command: 4 - parse and replay checks against this run, but not a fresh run.
- Readable/idiomatic: 5 - the S1 shape exactly, previewed.
- Safe write: 4 - link is atomic and no-clobber, with a filesystem fallback gap.
- Byte-stable: 5 - a pure function with fixed ordering and golden tests.
- v1.1 reuse: 4 - headless `Render`, which needs one extra field.

Cost: M     Reversibility: easy (the deny-list and emitter are internal; only the `v0` file shape is public).

Unknowns / assumptions: that `Selector.Source()` is canonical (sorted attr keys); how many false positives the deny-list gives on `shop`; how `os.Link` behaves on the CI and macOS temp filesystems.
