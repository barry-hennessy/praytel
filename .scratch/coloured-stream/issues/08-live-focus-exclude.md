# Live `--focus` / `--exclude`

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

`--focus GLOB` (repeatable, union) matches component or span name and admits the **path** (match ∪ ancestors ∪ descendants). `--exclude GLOB` after expansion. Stream **live admit**: first time a line is in the set, including late admit when a parent link lands; never un-print. Filter has its own projection; not a single drop-pipe that starves later renderers of span structure.

## Acceptance criteria

- [x] Focus path includes parent and child spans’ logs; siblings off the path stay out
- [x] Two `--focus` flags union; `--exclude` drops matching names after expansion
- [x] Late `SpanParentLinked` can admit a previously held line; stdout does not rewrite earlier lines
- [x] Filter tests via events → admitted log IDs (or correlator+filter driver)

## Blocked by

- [Join on IDs, pending, late parents](04-join-ids-pending-late-parents.md)

## Comments

Covered: `internal/filter` path/union/exclude/late-admit tests; `TestCorrelatorEventsDriveFocusAdmit`; wrapper `TestFocusHidesUntaggedLines`, `TestFocusOrphanStillStreams`.
