# Join on IDs, pending, late parents

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

Correlator join: JSON `trace_id`/`span_id` attach to real spans; trace-only → per-trace synthetic orphan; named unseen `span_id` stays **pending** until the span or drain, then `LogAssigned` (sticky). Child span before parent: `SpanUpserted` then `SpanParentLinked` — no invented parent. Stream may print a **new** hydrate line on `LogAssigned`, never edit history. Single correlator goroutine; events are value copies.

## Acceptance criteria

- [x] Correlator: IDs join; trace-only → per-trace orphan; pending then `LogAssigned` with stable log ID
- [x] Correlator: child-before-parent emits upsert then parent link; assignment never reparented
- [x] Stream: context appears without rewriting the original line
- [x] Tests do not read correlator maps

## Blocked by

- [Captured lines as an append-only stream](03-captured-lines-append-only-stream.md)

## Comments

Covered: correlator join/pending/parent-late/sticky tests; stream `TestLogAssignedHydratesWithoutRewriting`.
