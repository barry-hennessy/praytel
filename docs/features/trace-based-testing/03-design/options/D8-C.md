## Option C: Verify, don't trust: AST-built file, re-loaded and dry-run against the recording before an atomic no-clobber link   (stance: radical alternative)
Summary: The premise questioned is that a careful template makes the file correct "by construction". Here the generator is treated as untrusted. It builds a syntax tree, prints it, and then a gate re-parses the bytes, loads them through D5's `Load`, runs each `check` against the in-memory recording, and scans every literal for volatile values. The file is written only if every step passes. Otherwise Ctrl-W reports the reason and writes nothing.

How it works:
- `internal/praytest/gen`: `type Recording struct{ Argv []string; Running bool; Cases []PickedCase }` holds the selectors, assertions, absences and attribute values typed by D7 (int, float, string, status). `Build(Recording) *syntax.File` builds the S1 shape: header, constants, `def check_*`, `test(...)`. `Print(*syntax.File) []byte` is a small deterministic printer for only the node kinds the generator emits. go.starlark.net has no printer. bazelbuild/buildtools `build.Format` could be used instead (see Unknowns).
- `Verify(bytes, rec, trace) error` runs four steps. (1) `syntax.Parse`. (2) `praytest.Load` with an empty `getenv`, which must give exactly one case whose `Command` equals `Argv`. (3) The case's `check` runs against a D6 `run` built from the recorded trace, and it must pass. (4) A taint scan walks every `Literal` and rejects any value that equals a recorded trace or span ID, a timestamp, a duration or a port, or any attribute value under a key in a volatile list (`*.port`, `*.id`, temp-path prefixes). The `--snapshot-attrs` key spec is reused for that list.
- Write: create a temp file in the target directory, write it, fsync it, then `link(tmp, target)`, which fails with EEXIST and never overwrites. Unlink the temp file. Where `link` is unsupported, use `O_CREAT|O_EXCL` on the target and write to it.
- v1.1 repro files call `Build` with a different `Recording` (the property is the check, and `load("../props.star")` is one more node). They go through the same `Verify`, with step 3 expecting the check to *fail*.

Steelman: The weighted criteria ask for a *guarantee*, and only checking the actual output bytes gives one. Template bugs, D7 escaping mistakes and int/float round-trip errors are all caught before anything reaches disk. M3's test reduces to "Verify never rejects over every `shop` span". The answer to the "store data, not code" objection is that the code is checked like data.

Pros: The 25- and 20-weight criteria are checked on every write, not argued. Formatting is impossible to get syntactically wrong. Repros share the whole pipeline.

Cons / failure modes / hidden costs: Step 3 needs D6's `run` to be constructible from an in-memory trace, which couples D8 to D6/D1 internals. The taint scan is still a heuristic for attribute values: a port stored under `addr` gets through. A dry run proves the check passes on *this* run, not on a rerun, so a nondeterministic attribute such as an order number still passes. Rejections at Ctrl-W after the user has picked are frustrating. A hand-written printer is extra code to maintain, and buildtools is a new dependency with its own style.

Criteria scores:
- Zero volatile values: 4. IDs, times and ports are checked exactly, but attribute values are only heuristic.
- Passes unchanged command: 4. It is verified against the recording but not against a rerun.
- Readable/idiomatic: 4. Output is the S1 shape. It falls to 3 if the buildtools style differs.
- Safe write: 5. The link is no-clobber and atomic, with no partial file.
- Byte-stable/golden: 5. Deterministic printer, plus a re-parse round trip in the tests.
- v1.1 reuse: 5. The same Build/Verify, with the expectation inverted.

Cost: M     Reversibility: easy (internal package. Only the file shape is public, and it is S1's either way.)

Unknowns / assumptions: Whether buildtools `build.Format` emits `k = v` call spacing matching S1. Whether D6's `run` can wrap a recorded trace without a child. That `link(2)` is available on the supported filesystems.
