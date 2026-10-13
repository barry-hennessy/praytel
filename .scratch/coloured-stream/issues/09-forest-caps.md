# Forest caps

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

`--max-spans` and `--max-log-lines` bound correlator-retained data (pending and orphans included). At cap: evict oldest **closed** traces, then shed **newest** inbound. Emit `ForestEvicted` / `ForestShed`; warn on stderr; do not change child exit code. Pipeline channels still **block** on a slow renderer rather than drop for slowness. No disk spill.

## Acceptance criteria

- [x] Correlator: over cap with closed traces → evict events for oldest closed
- [x] Correlator: still over with all live → shed newest, not hang forever
- [x] Wrapper: shed warns on stderr; child exit code preserved
- [x] No temp files from this slice

## Blocked by

- [Join on IDs, pending, late parents](04-join-ids-pending-late-parents.md)

## Comments

Covered: correlator `TestEvictOldestClosedTrace`, `TestShedNewestWhenAllLive`; wrapper `TestShedWarnsAndPreservesChildExit`.
