# Assemble the architecture document

**Label:** `wayfinder:task`  
**Status:** closed  
**Blocked by:** [OTLP log vs trace routing](08-otlp-log-routing.md), [Praytel’s own observability](09-internal-observability.md)

## Question

_(Task, not a decision.)_ Rewrite `ARCHITECTURE.md` (and trim stale @TODOs) so it is the **decided** architecture: destination of this map. Index the closed tickets; do not restating every paragraph — the tickets hold detail. Include package layout, event grain, filter modes, renderer `Run`, caps, and error/exit rules.

Do this after the two remaining grilling tickets so the doc does not freeze missing receiver/observability decisions.

## Resolution

Rewrote [`ARCHITECTURE.md`](../../ARCHITECTURE.md) as the decided architecture. Tickets remain the detailed record; the doc is the index plus the rules needed to implement.
