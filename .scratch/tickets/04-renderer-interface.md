# Renderer interface shape

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** [Filter and focus engine design](03-filter-focus-engine.md)

## Question

What is the interface contract that all praytel renderers (coloured stream, call-graph indented, TUI, plain summary) implement?

Constraint from [Internal storage representation](02-internal-storage-representation.md): correlator **pushes events** (value copies). Snapshots are not the primary seam.

Constraint from [Filter and focus engine design](03-filter-focus-engine.md): filter is **not** a single pre-drop pipe. Stream/TUI use **live admit**; buffered renderers keep events (or their own buffer) and call **InFocus** at paint. Renderer interface must allow both.

Decisions needed:
1. **Push vs pull** — largely decided (push from correlator). Remaining: does a renderer subscribe to correlator events, to filter live-admit, or choose at construction?
2. **Granularity** — event vocabulary exists (`SpanUpserted`, `SpanParentLinked`, `LogReceived`, `LogAssigned`); confirm that is the renderer grain, plus span close/status.
3. **Lifecycle** — `Start(ctx)`, `Render(event)`, `Flush()`, `Close()`? What's the minimal surface?
4. **Filter integration** — live-admit vs InFocus at paint (see above); not “pre-filtered vs not” as a single global.
5. **Bubble Tea bridging** — in TUI mode, the renderer *is* the Bubble Tea model. How does the interface accommodate both a synchronous stream writer and an async Elm-architecture loop?

This is the key seam in the architecture — it separates the capture/correlate pipeline from all presentation. Getting it right makes every renderer a deep module behind a small interface.

Blocked by filter/focus because live-admit vs InFocus-at-paint has to show up in how renderers are wired, not as one global pre-filter.

## Resolution

**Shared interface is lifecycle only:** `Run(ctx context.Context) error`. Event source, live-admit vs `InFocus`, IO, Bubble Tea: all constructor-injected. `main` selects **one** renderer per process and runs it in the Bones `errgroup`.

No public `Start` / `Handle(Event)` / `Flush` / `Close`. Flush-on-drain is inside the renderer.

**When `Run` returns:**
- Event channel close and `DrainComplete` mean **no more data**, not process exit.
- Stream (and other non-TUI): return after channel close + final write.
- TUI: mark run complete, stay until user quits or `ctx` is cancelled (`ctx` = praytel process death, not child death).

**Event types live in `internal/domain`.** Correlator produces; filter and renderers consume. Grain: `SpanUpserted`, `SpanParentLinked`, `SpanClosed`, `LogReceived`, `LogAssigned`, `DrainComplete`. Value copies as already decided.
