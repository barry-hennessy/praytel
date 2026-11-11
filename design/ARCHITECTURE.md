---
title: Architecture
summary: How praytel wraps one command, ingests its logs and OTLP data, joins them and sends them to the outputs of a plan.
weight: 80
---

# Architecture

Praytel is a **transparent CLI wrapper**: `praytel [flags] -- cmd …`. It forwards signals, runs one command to completion, captures that process’s logs and OpenTelemetry data, joins them, and prints a unified view. No daemon, no leftover files. The TUI may stay open after the child exits; everything else drains and exits.

This document is the decided architecture. Decision detail lives in [`.scratch/MAP.md`](../.scratch/MAP.md) and its tickets. [Pipeline](PIPELINE.md) gives the detail of the outputs, the plan, the query language and the checks. The code changes to this design in phases.

Go. [Bones](https://github.com/barry-hennessy/bones) for process lifecycle (`RootCtx`, `errgroup`) and `LoggingTracer`. [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the TUI sink. [Starlark](https://github.com/google/starlark-go) for the `--execute` front-end. Starlark makes a plan one time for each change. It does not see events.

## Shape

```
cmd/praytel             flags, wiring, errgroup
internal/domain         Trace, Span, LogLine, events (no join logic)
internal/receiver       OTLP gRPC+HTTP (traces and logs) — not the Collector
internal/capture        child stdout/stderr → LogLine
internal/correlator     one goroutine, private forest, outbound events
internal/ingest         wires one correlator to its listening receiver
internal/projection     the one read model of spans, logs and traces
internal/plan           plan types, validation, flags → plan
internal/plan/starlark  --execute front-end: Starlark → plan
internal/pipeline       driver goroutine; Stage, View and Sink interfaces
internal/view/...       tree, graph, stream, check, table
internal/sink           stdout, stderr, file, exit code, TUI panes
internal/query          query language: parser, type checker, evaluator
```

`internal/query` replaces the glob evaluator of `internal/filter`. The views and sinks replace `internal/render/stream` and `internal/render/tui`.

`main` makes a plan from the flags or from `--execute`. Then it compiles the plan and runs the driver. The current flags are shorthand for plans. Env vars only where OTel already uses them (`OTEL_EXPORTER_OTLP_*`). Default ports 4317/4318 with `--grpc-port` / `--http-port`.

## Ingest

Minimal custom receiver from OTLP protobufs: gRPC and HTTP/protobuf, **separate** trace and log services (`/v1/traces`, `/v1/logs`). No metrics. Cast to `domain` at the adaptor edge.

Capture parses structured stdout/stderr for `trace_id` / `span_id` from JSON or logfmt. Unstructured lines are not an error. OTLP logs become the same `LogLine` with `source` = `stdout` | `stderr` | `otlp`. Duplicate stdout+OTLP lines are both kept.

The child sees OTLP endpoints via env. Drain after child exit: idle timeout with a hard cap (both flags).

## Join

One correlator goroutine. Typed inbound channels (spans, OTLP logs, stdout lines). Private mutable forest (`traces`, `spans` by `(TraceID, SpanID)`, scoped pending assignments, lazy synthetic orphans). Nobody else reads those maps.

Outbound **value-copy events** in `domain`: `SpanUpserted`, `SpanParentLinked`, `SpanClosed`, `LogReceived`, `LogAssigned`, `DrainComplete`, plus `ForestEvicted` / `ForestShed`. Child spans insert immediately; parent link may be late. Logs that **name** an unseen span wait until that span or drain; then assignment is sticky.

Untagged logs never attach to a guessed real span. Global synthetic orphan (no IDs); per-trace synthetic orphan (`trace_id` only). See [Untagged log handling](../.scratch/tickets/01-untagged-log-handling.md).

## Outputs

A plan is a list of outputs. Each output has stages, one view, one trigger and one sink. One process can have more than one output.

One driver goroutine reads the correlator events. It applies each event to the one projection. Only the driver writes to the projection. The projection keeps the raw data. Thus each output can use different stages on the same data.

The trigger tells a view when to send output:
- `each`: after each event. The stream uses it. The stream is **append-only** and never removes a line that it wrote.
- `tick`: on a timer. TUI panes use it.
- `settled`: when a trace settles. A late event makes the trace live again with a new revision.
- `final`: at `DrainComplete` or when the event channel closes. `DrainComplete` / event-channel close means **data done**.

Pipeline channels **block**; they do not drop.

Filters are stages and queries. A record-local query decides when the event arrives, so it can operate on each event. A structural query can decide only from the projection, at the trigger `tick`, `settled` or `final`. A child span can arrive before its parent link, and a log can get its span late. Thus a filter is not a single drop-pipe in front of every output.

The TUI is a sink. It shows panes in tabs. Each pane is one output. The driver owns the data. The TUI owns the screen. The TUI stays until quit or process `ctx`.

Visual design (colours, TUI chrome, how orphans look) is not specified here.

## Pressure and errors

In-memory only. `--max-spans` / `--max-log-lines`. Evict oldest **closed** traces, then shed newest. Never silent (stderr + events). No spill files.

If the child started and failed, **its exit code** is praytel’s. If the child succeeded, praytel uses the highest code from its sinks: 3 if an output is incomplete, 4 if a check fails, and 0 if not. If the child never started (exec, bind, `--tui` cannot start), praytel fails. Warnings on stderr. `ErrMapper` only in `main`.

Praytel’s own logs: `LoggingTracer` on **stderr**, **noop tracer**, never export OTLP into the in-process receiver.

## Out of scope (this architecture)

Multi-process orchestration; non-TUI browse-after-exit; OTLP metrics.
