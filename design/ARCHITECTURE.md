---
title: Architecture
summary: How praytel wraps one command, ingests its logs and OTLP data, joins them and renders a unified view.
weight: 80
---

# Architecture

Praytel is a **transparent CLI wrapper**: `praytel [flags] -- cmd …`. It forwards signals, runs one command to completion, captures that process’s logs and OpenTelemetry data, joins them, and prints a unified view. No daemon, no leftover files. The TUI may stay open after the child exits; everything else drains and exits.

This document is the decided architecture. Decision detail lives in [`.scratch/MAP.md`](../.scratch/MAP.md) and its tickets.

Go. [Bones](https://github.com/barry-hennessy/bones) for process lifecycle (`RootCtx`, `errgroup`) and `LoggingTracer`. [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the TUI renderer.

## Shape

```
cmd/praytel          flags, wiring, errgroup
internal/domain      Trace, Span, LogLine, events (no join logic)
internal/receiver    OTLP gRPC+HTTP (traces and logs) — not the Collector
internal/capture     child stdout/stderr → LogLine
internal/correlator  one goroutine, private forest, outbound events
internal/ingest      wires one correlator to its listening receiver
internal/filter      span admission consulted by every renderer
internal/render/stream
internal/render/tui  live span tree
internal/session     bounded in-process runs for agent integrations
internal/agentapi    transport-neutral progressive investigation tools
```

`main` constructs one renderer and `Run`s it. Flags are primary; env vars only where OTel already uses them (`OTEL_EXPORTER_OTLP_*`). Default ports 4317/4318 with `--grpc-port` / `--http-port`.

## Ingest

Minimal custom receiver from OTLP protobufs: gRPC and HTTP/protobuf, **separate** trace and log services (`/v1/traces`, `/v1/logs`). No metrics. Cast to `domain` at the adaptor edge.

Capture parses structured stdout/stderr for `trace_id` / `span_id` from JSON or logfmt. Unstructured lines are not an error. OTLP logs become the same `LogLine` with `source` = `stdout` | `stderr` | `otlp`. Duplicate stdout+OTLP lines are both kept.

The child sees OTLP endpoints via env. Drain after child exit: idle timeout with a hard cap (both flags).

## Join

One correlator goroutine. Typed inbound channels (spans, OTLP logs, stdout lines). Private mutable forest (`traces`, `spans` by `(TraceID, SpanID)`, scoped pending assignments, lazy synthetic orphans). Nobody else reads those maps.

Outbound **value-copy events** in `domain`: `SpanUpserted`, `SpanParentLinked`, `SpanClosed`, `LogReceived`, `LogAssigned`, `DrainComplete`, plus `ForestEvicted` / `ForestShed`. Child spans insert immediately; parent link may be late. Logs that **name** an unseen span wait until that span or drain; then assignment is sticky.

Untagged logs never attach to a guessed real span. Global synthetic orphan (no IDs); per-trace synthetic orphan (`trace_id` only). See [Untagged log handling](../.scratch/tickets/01-untagged-log-handling.md).

## Render

Shared interface: `Run(ctx context.Context) error`. Constructor takes event source, filter, IO. One renderer per process.

`DrainComplete` / event-channel close means **data done**. Stream returns after that. TUI stays until quit or process `ctx`. Stream is **append-only**, prefers unbuffered (`LogReceived`). Tree/summary may hold until paint. Pipeline channels **block**; they do not drop.

Every renderer consults `internal/filter` before showing a span: the stream and TUI **live admit** (late admission OK; the stream never un-prints), and buffered renderers check **`InFocus` at paint**. It is not a single drop-pipe in front of every renderer.

Visual design (colours, TUI chrome, how orphans look) is not specified here.

## Agent sessions

`praytel mcp` is a separate composition mode: its process owns several bounded,
in-memory sessions (`internal/session`) for one MCP client. Each session still
wraps one child, leaves no files, and is cancelled when the MCP server exits.
`internal/agentapi` exposes them as transport-neutral tools that disclose
evidence progressively, using the same query evaluator.

## Pressure and errors

In-memory only. `--max-spans` / `--max-log-lines`. Evict oldest **closed** traces, then shed newest. Never silent (stderr + events). No spill files.

If the child started, **its exit code** is praytel’s. If it never started (exec, bind, `--tui` cannot start), praytel fails. Warnings on stderr. `ErrMapper` only in `main`.

Praytel’s own logs: `LoggingTracer` on **stderr**, **noop tracer**, never export OTLP into the in-process receiver.

## Out of scope (this architecture)

Multi-process orchestration; non-TUI browse-after-exit; OTLP metrics.
