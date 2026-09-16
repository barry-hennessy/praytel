# Filter and focus engine design

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** [Internal storage representation](02-internal-storage-representation.md)

## Question

How should praytel's filtering and focus system work — both its user-facing syntax and its internal engine?

Requirements from the grill:
- Filter by component name, span name
- Focus on "all traces that pass *through* a component/span name" — parents and children of matching spans, not just the matches themselves
- This is a graph query on the span tree, not a simple string filter
- The engine sits upstream of all renderers (cross-cutting)
- Correlator forest is private; the public stream is events with value copies ([Internal storage representation](02-internal-storage-representation.md)). Filter cannot read correlator maps.

Decisions needed:
1. **Query syntax** — how does the user express filters? Flags like `--focus orders`? A mini-query language? Glob patterns?
2. **Graph traversal** — "pass through" means: find matching spans, walk up to root, walk down to leaves. Does filter maintain its own projection of the forest from events, and walk that?
3. **Live vs static** — does the filter apply dynamically as new data arrives, or only on the buffered result?
4. **Composability** — can filters be combined? `--focus orders --exclude health-check`?

Blocked by internal storage because the traversal strategy depends on how the span tree is physically represented.

## Resolution

**One `filter` module:** consumes correlator events, holds its **own projection** (cannot see correlator maps), compiles a query.

**Two access modes** (same query, renderer chooses):
- **Live admit** — stream and TUI: forward a log the first time its span is in the focus set; **late admission** when `SpanParentLinked` (etc.) grows the path; never retract on the stream.
- **Complete / query-at-paint** — buffered interfaces: call `InFocus(spanID)` (or equivalent) against the projection when they already buffer.

Not a single drop-gate in front of every renderer. Span-structure events always update the projection.

**v1 query:** flags + globs only. Internal compiled query type so a language can plug in later.
- `--focus GLOB` — match component **or** span name; **path** = match ∪ ancestors ∪ descendants (not whole-trace). Repeatable → union of paths.
- `--exclude GLOB` — same matchers, applied after expansion; repeatable → drop if any match.
- No `--focus-exact` yet. Whole-trace (`trace-contains`) waits for the query language.

Global orphan is in focus only if the glob matches it (or it lies on a focused path). Per-trace orphan is included only if it is on that path, not because it shares a `TraceID`.
