# Internal storage representation

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** [Untagged log handling](01-untagged-log-handling.md)

## Question

What internal data structure should the correlator use to store the trace tree and attached logs during a run?

The logical model (trace → span tree → logs) is decided, but the physical representation that serves it under pressure is not. Considerations:

- Spans arrive out of order (child before parent is common in async systems)
- Logs arrive continuously and must be attached to their span quickly
- Must hold **pending** log lines keyed by declared `span_id` until the span arrives or drain fires ([Untagged log handling](01-untagged-log-handling.md))
- Must accommodate two **synthetic** span kinds: run-wide global orphan, and per-trace orphan children — created lazily, never used as a process-root wrapping real traces
- The TUI renderer reads this structure at 60fps for display
- Long or high-throughput runs may generate substantial data (the ARCHITECTURE.md @TODO)
- The correlator is single-goroutine, so no locking — but read access from renderers needs a strategy (snapshot? copy-on-read? channel of diffs?)

Options range from a simple `map[TraceID]*Trace` with slices of spans/logs, to an append-only event log that materialises views on demand, to a ring buffer that evicts old data under pressure.

Blocked by untagged log handling because the orphan strategy affects what the storage needs to accommodate.

## Resolution

**Private mutable forest + outbound event channel.** The correlator goroutine is the only writer. Other packages never see the maps.

**Forest (join state only the correlator holds):**
- `traces map[TraceID]*Trace`
- `spans map[SpanID]*Span` — child may exist before parent; parent link filled later
- `pending map[SpanID][]LogLine` — logs that named an unseen span
- lazy global orphan span; lazy per-trace orphan children

Cast to `domain` structs at the receiver/capture edge. Events leaving the correlator are **value copies**, not pointers into the forest.

**Events (the interface):** at least `SpanUpserted`, `SpanParentLinked`, `LogReceived`, `LogAssigned`. Every log has a stable ID. `LogReceived` fires on capture (correlation may be pending). `LogAssigned` fires when pending resolves or when assignment was immediate — never again after that.

**Not this ticket:** eviction, spill, ring buffers — [Memory and disk pressure management](06-memory-pressure.md). Exact renderer method shape — [Renderer interface shape](04-renderer-interface.md), which consumes this event stream (possibly after filter).
