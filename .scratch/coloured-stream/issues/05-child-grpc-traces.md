# Child gRPC traces

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

OTLP/gRPC trace export from the **child** reaches the receiver, becomes domain spans, and joins the same stream as stdio logs. Demo/wrapper test: tiny child that starts a span and logs JSON with those IDs — unified output without a collector container.

## Acceptance criteria

- [x] Wrapper: gRPC-exporting helper child produces `SpanUpserted` (or visible hydrated stream lines) for that span
- [x] JSON stdout with matching IDs joins that span (behaviour from the join slice, now over the wire)
- [x] Still no Collector framework; traces only on the trace service

## Blocked by

- [OTLP listen and point the child](02-otlp-listen-and-point-child.md)
- [Join on IDs, pending, late parents](04-join-ids-pending-late-parents.md)

## Comments

Covered: `TestGRPCExportingChildJoinsJSONLog`; receiver gRPC trace mapping tests.
