# Buffered vs unbuffered output strategy

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** [Renderer interface shape](04-renderer-interface.md)

## Question

When should praytel buffer data before rendering, and when should it stream immediately?

The ARCHITECTURE.md identifies this tension:
- **Unbuffered** — log lines stream out as spans close. Low latency, useful for long-running processes.
- **Buffered** — wait for trace completion so duration, error status, and the full span tree are known. Required for call-graph indented output and rich trace headers.

Constraint from [Renderer interface shape](04-renderer-interface.md): buffering is **inside** the renderer, not a method on the shared interface. Stream `Run` ends after drain/channel close; call-graph/summary can hold events and paint using `InFocus` when they choose (span close and/or `DrainComplete`).

Decisions needed:
1. **Per-renderer or global?** — each renderer already owns this; confirm no global `--buffer` required, vs a flag that selects which renderer.
2. **Hybrid** — can the coloured stream start unbuffered and "upgrade" a trace's display once it completes?
3. **Backpressure** — if the renderer is slower than the data arrival rate, what happens? Drop? Buffer to disk? Slow the display?
4. **Interaction with drain** — `DrainComplete` + channel close is the flush trigger for non-TUI; TUI freezes live updates but stays open.

Blocked by renderer interface because flush-on-drain and hybrid stream-vs-tree behaviour live inside each renderer’s `Run`.

## Resolution

**The renderer decides.** Prefer unbuffered. Buffer only when the view needs a tree, duration, or error status. No global `--buffer` flag; flags select the renderer.

- **Stream:** append-only; write on `LogReceived` (optional extra **new** lines on `LogAssigned`). Never rewrite printed output. Return after `DrainComplete` / channel close.
- **Call-graph / summary:** hold events; paint with `InFocus` when the tree is usable (`SpanClosed` and/or `DrainComplete`).
- **TUI:** live redraw; freeze when data is done; stay open per renderer ticket.

**Backpressure:** block on modest bounded channels. Never drop. Never unbounded pipeline queues. A tree renderer’s hold-until-paint is explicit and is capped by [Memory and disk pressure management](06-memory-pressure.md).
