# Transparent prepend

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

`praytel -- cmd …` runs one child to completion, copies the child’s stdout and stderr to praytel’s stdout/stderr as a passthrough, and exits with the **child’s** code. Exec failure is praytel’s non-zero exit (child never started). Bones `RootCtx` + `errgroup`. Praytel diagnostics go through `LoggingTracer` on **stderr** with a noop tracer — never mixed in as captured child data. Flags-only, no config file, no leftover files. Stream `Run` may still be a passthrough; correlator can be a stub.

## Acceptance criteria

- [x] `praytel -- true` exits 0; `praytel -- false` exits 1; missing binary exits non-zero without claiming a child ran
- [x] Child stdout appears on praytel stdout; praytel diagnostics only on stderr
- [x] Wrapper test: helper child with a chosen exit code; no stray files after the run
- [x] No GitHub / no OTLP required for this slice

## Blocked by

None - can start immediately

## Comments

Implemented on `main` (`cmd/praytel` + wrapper tests). `go test ./cmd/praytel/ -run 'TestTrueExitsZero|TestFalseExitsOne|TestMissingBinaryDoesNotClaimChildRan|TestHelperChildExitAndStdio'` passes. Later slices already sit on top of this (OTLP bind, stream, join); this ticket did not need a new commit.
