# Praytel Architecture Map

**Label:** `wayfinder:map`

## Destination

A decided architecture document for praytel. **Reached:** [`ARCHITECTURE.md`](../ARCHITECTURE.md). Visualisation remaining under **Not yet specified** is not a blocker for the first implementation slice (coloured stream).

## Notes

- Domain: CLI tooling, OpenTelemetry, observability
- Language: Go
- Skills to consult: `codebase-design`, `golang-architecture`, `golang-structure`, `golang-interfaces-structs`
- Bones research: see [Research Bones framework](../agent-transcripts/) — lifecycle via `RootCtx()` + `errgroup`, `LoggingTracer` for component-scoped observability, own `Tracer`/`Span` interfaces
- Bubble Tea research: see [Research Bubble Tea TUI](../agent-transcripts/) — Elm architecture, 60fps coalesced rendering, channel-based streaming, composable nested models, `viewport` for scrollable panels, supports both full-screen TUI and plain inline output

## Decisions so far

- [Untagged log handling](tickets/01-untagged-log-handling.md) — never guess a real span; global orphan vs per-trace orphan vs pending-until-span-or-drain; no reparenting after final assignment
- [Internal storage representation](tickets/02-internal-storage-representation.md) — correlator-private mutable forest; renderers get value-copy events (`LogReceived` then `LogAssigned`, span upsert then late parent link); no shared maps, no event-log-as-source-of-truth, no eviction here
- [Filter and focus engine design](tickets/03-filter-focus-engine.md) — one filter projection; live admit (stream/TUI, late OK) vs InFocus at paint (buffered); `--focus`/`--exclude` globs; path not whole-trace; query language later
- [Renderer interface shape](tickets/04-renderer-interface.md) — `Run(ctx) error` only; one renderer per process; events in `domain`; `DrainComplete`/channel close = data done; TUI stays until quit or process `ctx`
- [Buffered vs unbuffered output strategy](tickets/05-buffering-strategy.md) — renderer chooses, prefer unbuffered; stream append-only; pipeline blocks, never drops; tree/summary hold until paint
- [Memory and disk pressure management](tickets/06-memory-pressure.md) — in-memory caps; evict closed then shed newest; warn on stderr + events; no spill files
- [Error surface](tickets/07-error-surface.md) — child’s exit code if it ran; stderr warnings; unstructured logs are not errors; `ErrMapper` only at `main`
- [OTLP log vs trace routing](tickets/08-otlp-log-routing.md) — separate OTLP trace and log services; one `LogLine` + `source`; no dedupe; no metrics
- [Praytel’s own observability](tickets/09-internal-observability.md) — `LoggingTracer` → stderr; noop tracer; never export into our own receiver
- [Assemble the architecture document](tickets/10-assemble-architecture-doc.md) — `ARCHITECTURE.md` is the destination artifact
- **OTLP protocols** — support both gRPC (4317) and HTTP/protobuf (4318) from the start; language-agnostic for the wrapped process
- **Log capture strategy** — structured stdout/stderr parsing for trace/span IDs is primary; OTLP log signals accepted when available; untagged log handling is a separate decision
- **Logical data model** — trace → span tree → attached log lines + orphan bucket; span lifecycle (open → closing → closed) is first-class; internal storage representation is a separate decision
- **Output modes** — pluggable renderers behind an interface; coloured log stream first, build toward TUI; all renderers share cross-cutting filter/focus capabilities
- **Process wrapping** — single command, transparent wrapper (`praytel -- cmd`); forward signals, drain, exit; no stray state between runs; TUI may stay open as the one exception
- **OTLP receiver approach** — minimal custom receiver using OTLP protobuf definitions + gRPC/HTTP servers; not the embedded OTel Collector framework
- **Join engine** — single correlator goroutine; typed channels per source (traces, OTLP logs, stdout lines); no shared mutable state
- **Configuration** — CLI flags primary; env vars only where the ecosystem expects them (e.g. `OTEL_EXPORTER_OTLP_ENDPOINT`); no config files, no praytel-specific env vars
- **Port allocation** — fixed defaults (4317 gRPC, 4318 HTTP) with `--grpc-port` / `--http-port` override flags
- **Drain strategy** — idle timeout (no new data for N seconds) with a hard cap; both configurable via flags
- **Package structure** — `cmd/praytel/` for main + wiring; `internal/{domain, receiver, capture, correlator, filter, render/stream, render/tui}`; domain is pure types, no logic

## Not yet specified

- TUI layout, keybindings, and interaction model — renderer seam is `Run(ctx)`; layout is still visualisation / a later prototype
- Trace visualisation formatting details (colour scheme, span header format, log line format, how orphans and pending look)
- Query language for focus/filter (beyond `--focus`/`--exclude` globs); whole-trace operator

## Out of scope

- Multi-process orchestration (several wrapped services in one praytel) — destination is a single transparent prepend
- Non-TUI `--keep-open` / browse-after-exit — TUI stay-open is the decided exception
- OTLP metrics ingest
