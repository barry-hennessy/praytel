# Untagged log handling

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** _(none)_

## Question

How should praytel handle log lines from stdout/stderr that don't contain a trace_id or span_id?

Options to consider:
- **Orphan bucket** — display them in a separate "untagged" section, outside the trace tree
- **Time-window heuristic** — attach to the span that was active at the timestamp (risky with concurrent spans)
- **Nearest-span heuristic** — attach to the most recently opened span on the same goroutine/thread (requires thread ID in logs)
- **Process-level attachment** — attach to a synthetic root span representing the whole process invocation
- **User annotation** — let the user tag untagged logs via a flag or pattern (`--tag-untagged-to root`)
- **Drop or dim** — show them but visually de-emphasise, making it clear they're uncorrelated

This affects the correlator's logic, the domain model (does `LogLine` require a span, or is it optional?), and every renderer's layout.

## Resolution

Never infer a real span. Assignment is honest and, once final, sticky.

**Final parent is always a span** — including two kinds of **synthetic** span (`Synthetic: true`, well-known names). Renderers and filter/focus treat them as spans; they do not special-case optional `span_id` on `LogLine`.

| What the line carries | Assignment |
|---|---|
| No `trace_id`, no `span_id` | Run-wide **global orphan** span (created lazily). Not a process-root wrapping real traces. |
| `trace_id`, no `span_id` | That trace’s **per-trace orphan** (synthetic child). If the trace is unknown, global orphan. |
| `span_id` whose span is already in the tree | That real span. |
| `span_id` not yet in the tree | **Pending** until the span arrives or drain/idle timeout completes. Then attach to the real span, or park on the per-trace orphan (global orphan if the trace was never seen). |

No time-window or nearest-span heuristics. No retroactive reparenting after a final assignment. Pending is waiting on an ID the log already declared, not guessing.

How pending lines are stored is for [Internal storage representation](02-internal-storage-representation.md). How orphans look (dim, separate section, colour) is visualisation fog.
