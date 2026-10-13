# Captured lines as an append-only stream

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

Child stdout/stderr become `LogLine`s (source stdout/stderr), enter the correlator, and the stream renderer `Run`s an **append-only** coloured (or monochrome) print on `LogReceived`. Non-JSON is kept, not an error. Untagged lines attach to the **global synthetic orphan** — never a guessed real span. Malformed JSON (not plain text) warns on stderr, rate-limited. Correlator tests: inbound `LogLine` → outbound events; do not inspect forest maps.

## Acceptance criteria

- [x] Unstructured line appears on stdout once; not rewritten
- [x] Correlator seam: no IDs → `LogReceived` assigned to global synthetic orphan
- [x] Plain non-JSON does not warn; broken JSON does (rate-limited)
- [x] No colour → monochrome, still exits 0 if the child did

## Blocked by

- [Transparent prepend](01-transparent-prepend.md)

## Comments

Covered: wrapper `TestHelperChildExitAndStdio`, `TestPlainTextDoesNotWarn`, `TestMalformedJSONWarnsOnStderr`, `TestNoColorStillExitsZero`; correlator `TestUntaggedLogAssignedToGlobalOrphan`; capture rate-limit tests.
