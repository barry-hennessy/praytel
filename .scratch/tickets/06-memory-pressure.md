# Memory and disk pressure management

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** [Internal storage representation](02-internal-storage-representation.md), [Buffered vs unbuffered output strategy](05-buffering-strategy.md)

## Question

How does praytel handle runs that generate more trace/log data than fits comfortably in memory?

The ARCHITECTURE.md flags this as a @TODO. For a dev-workflow tool wrapping one process, most runs are small — but integration test suites or load tests could produce substantial data.

Constraint from [Internal storage representation](02-internal-storage-representation.md): working store is an in-memory mutable forest. Pressure policy (drop, spill, ring) is this ticket, not a change of source-of-truth to an event log.

Constraint from [Buffered vs unbuffered output strategy](05-buffering-strategy.md): the **pipeline must not drop** to relieve pressure; it blocks. Pressure is the **forest** and any **renderer hold-until-paint**, not the event channels. Switching stream to “unbuffered” is not a relief valve — stream already is. A tree renderer under pressure might need to degrade or cap its buffer.

Decisions needed:
1. **Detection** — how does praytel know it's under pressure? Memory threshold? Event count?
2. **Eviction** — drop old completed traces? Spill to a temp file? Summarise and discard detail?
3. **Degradation** — does a buffered renderer degrade (summarise, cap) under pressure?
4. **Limits** — should there be hard caps (`--max-traces`, `--max-log-lines`) that the user sets?
5. **Signalling** — does praytel warn the user when it's shedding data?

Blocked by both storage representation and buffering strategy, since the eviction/spill approach depends on both.

## Resolution

**KISS: in-memory forest, no disk spill.** Bound retained spans and log lines with `--max-spans` and `--max-log-lines` (high defaults). Counts are correlator-retained (pending and orphans included), not RSS.

**At cap:** evict oldest **closed** traces from the forest, then if still over, **shed newest** inbound joins (do not block forever — child OTLP deadlock). Pipeline channels still block for *slowness*, not for *full forest*. Tree renderers must not keep an unbounded second copy.

**Never silent:** `ForestEvicted` / `ForestShed` domain events; stream warnings on **stderr**; TUI status; drain summary if anything was lost. Shedding does **not** by itself fail the process; exit-code policy is [Error surface](07-error-surface.md).
