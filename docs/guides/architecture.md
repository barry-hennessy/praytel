---
title: Architecture
summary: How praytel wraps one command, ingests its logs and OTLP data, joins them and renders a unified view.
weight: 80
---

# Architecture

Praytel is a **transparent CLI wrapper**: `praytel [flags] -- cmd …`. It forwards signals, runs one command to completion, captures that process’s logs and OpenTelemetry data, joins them, and prints a unified view. No daemon, no leftover files. The TUI may stay open after the child exits; everything else drains and exits.

This document is the decided architecture. Decision detail lives in [`.scratch/MAP.md`](../../.scratch/MAP.md) and its tickets.

Go. [Bones](https://github.com/barry-hennessy/bones) for process lifecycle (`RootCtx`, `errgroup`) and `LoggingTracer`. [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the TUI renderer.

## Shape

```
cmd/praytel          flags, wiring, errgroup
internal/domain      Trace, Span, LogLine, events (no join logic)
internal/receiver    OTLP gRPC+HTTP (traces and logs) — not the Collector
internal/capture     child stdout/stderr → LogLine
internal/correlator  one goroutine, private forest, outbound events
internal/ingest      wires one correlator to its listening receiver
internal/query       typed parser, checker, formatter, pure evaluator
internal/projection  event-derived query read model + trace settlement
internal/filter      legacy --focus/--exclude live admission
internal/session     bounded in-process runs for agent integrations
internal/agentapi    transport-neutral progressive investigation tools
internal/archmap     architecture maps: record, canonical file format, diff
internal/graph       component graph, layered layout and box drawing
internal/render/stream
internal/render/query
internal/render/tui  live tree with typed-query selection
```

`main` constructs one renderer and `Run`s it. Flags are primary; env vars only where OTel already uses them (`OTEL_EXPORTER_OTLP_*`). Default ports 4317/4318 with `--grpc-port` / `--http-port`.

## Ingest

Minimal custom receiver from OTLP protobufs: gRPC and HTTP/protobuf, **separate** trace and log services (`/v1/traces`, `/v1/logs`). No metrics. Cast to `domain` at the adaptor edge.

Capture parses structured stdout/stderr for `trace_id` / `span_id` from JSON or logfmt. Unstructured lines are not an error. OTLP logs become the same `LogLine` with `source` = `stdout` | `stderr` | `otlp`. Duplicate stdout+OTLP lines are both kept.

The child sees OTLP endpoints via env. Drain after child exit: idle timeout with a hard cap (both flags).

## Join

One correlator goroutine. Typed inbound channels (spans, OTLP logs, stdout lines). Private mutable forest (`traces`, `spans` by `(TraceID, SpanID)`, scoped pending assignments, lazy synthetic orphans). Nobody else reads those maps.

Outbound **value-copy events** in `domain`: `SpanUpserted`, `SpanParentLinked`, `SpanClosed`, `LogReceived`, `LogAssigned`, `DrainComplete`, plus `ForestEvicted` / `ForestShed`. Child spans insert immediately; parent link may be late. Logs that **name** an unseen span wait until that span or drain; then assignment is sticky.

Untagged logs never attach to a guessed real span. Global synthetic orphan (no IDs); per-trace synthetic orphan (`trace_id` only). See [Untagged log handling](../../.scratch/tickets/01-untagged-log-handling.md).

## Selection

Typed queries are checked before the child starts and evaluated per trace by
`internal/query` over an `internal/projection` read model. Selectors distinguish
spans from logs; structural operators use final log-to-span assignment. The default
view is **path** (match ∪ ancestors ∪ descendants), while `matches` and `trace`
provide compact evidence and whole-trace views. CLI machine output and the TUI use
the same evaluator. `--focus GLOB` / `--exclude GLOB` remain legacy shorthand for
component or span-name filtering when no typed query is supplied.

- Stream and TUI: **live admit** (late admission OK; stream never un-prints).
- Buffered renderers: **`InFocus` at paint**.

Not a single drop-pipe in front of every renderer. [Filter and focus engine design](../../.scratch/tickets/03-filter-focus-engine.md).

## Render

Shared interface: `Run(ctx context.Context) error`. Constructor takes event source, filter, IO. One renderer per process.

`DrainComplete` / event-channel close means **data done**. Stream returns after that. TUI stays until quit or process `ctx`. Stream is **append-only**, prefers unbuffered (`LogReceived`). Tree/summary may hold until paint. Pipeline channels **block**; they do not drop.

Visual design (colours, TUI chrome, how orphans look) is not specified here.

`--map` is not a renderer: it tees the same event stream into a recorder
that writes [architecture maps](maps.md) after drain, alongside whichever
renderer is running. The component graph and its drawing are shared by the
TUI's graph view and `praytel map`.

## Pressure and errors

In-memory only. `--max-spans` / `--max-log-lines`. Evict oldest **closed** traces, then shed newest. Never silent (stderr + events). No spill files.

If the child started, **its exit code** is praytel’s. If it never started (exec, bind, `--tui` cannot start), praytel fails. Warnings on stderr. `ErrMapper` only in `main`.

Praytel’s own logs: `LoggingTracer` on **stderr**. Its own spans are **off** (noop tracer) unless praytel is started with `--self-trace`, a development flag: then they are exported to the OTLP endpoint praytel inherited from an outer praytel, never to its own receiver, and flushed before exit. Only `cmd/praytel/selftrace.go` imports the OTel SDK. Spans can't yet record an error status ([bones#2](https://github.com/barry-hennessy/bones/issues/2)). See [CONTRIBUTING.md](../../CONTRIBUTING.md).

## Out of scope (this architecture)

Multi-process orchestration; non-TUI browse-after-exit; OTLP metrics.

The optional `praytel mcp` command is a separate composition mode: its process
owns several bounded, in-memory sessions for one MCP client. Each session still
wraps one child, leaves no files, and is cancelled when the MCP server exits.

## Tickets

| Ticket | Decision |
|---|---|
| [Untagged log handling](../../.scratch/tickets/01-untagged-log-handling.md) | Honest orphans; pending named spans |
| [Internal storage representation](../../.scratch/tickets/02-internal-storage-representation.md) | Private forest; event stream |
| [Filter and focus engine design](../../.scratch/tickets/03-filter-focus-engine.md) | Live admit vs InFocus; path globs |
| [Renderer interface shape](../../.scratch/tickets/04-renderer-interface.md) | `Run(ctx)` |
| [Buffered vs unbuffered output strategy](../../.scratch/tickets/05-buffering-strategy.md) | Renderer chooses; append-only stream |
| [Memory and disk pressure management](../../.scratch/tickets/06-memory-pressure.md) | Caps; evict then shed |
| [Error surface](../../.scratch/tickets/07-error-surface.md) | Child exit code; stderr warnings |
| [OTLP log vs trace routing](../../.scratch/tickets/08-otlp-log-routing.md) | Two OTLP services; one `LogLine` |
| [Praytel’s own observability](../../.scratch/tickets/09-internal-observability.md) | Stderr `LoggingTracer`; spans only with `--self-trace` |
