---
title: praytel(1)
summary: Run a command and print its logs and OpenTelemetry traces as one joined view.
weight: 10
---

# praytel(1)

## Synopsis

```
praytel [flags] -- command [args ...]
praytel query <check|fmt|explain> QUERY
praytel map [--format FORMAT] [OLD] NEW
praytel mcp
praytel agent instructions
```

## Description

`praytel` runs `command` to completion, forwards signals to it, captures its
stdout and stderr, receives the OTLP traces and logs it exports, joins them and
prints a unified view. The `--` separator is required.

By default the output is an append-only stream. `--tui` renders a live
trace/span tree instead, and `--query` selects records with a
[typed query](praytel-query-language.7.md).

The subcommands are documented separately:
[praytel-query(1)](praytel-query.1.md), [praytel-map(1)](praytel-map.1.md),
[praytel-mcp(1)](praytel-mcp.1.md) and
[praytel-agent(1)](praytel-agent.1.md).

## Flags

### Rendering

| Flag | Default | Meaning |
|---|---|---|
| `--tui` | off | Render an interactive live trace tree. |

### Selection

| Flag | Default | Meaning |
|---|---|---|
| `--query QUERY`, `-q QUERY` | | Select logs and traces with a typed query. |
| `--query-file PATH` | | Read a typed query from a file. |
| `--format FORMAT` | `text` | Query output format: `text`, `json` or `ndjson`. |
| `--emit LIFECYCLE` | `final` | Query lifecycle to emit: `final` or `settled`. |
| `--settle-after DURATION` | `250ms` | Quiet period before a closed trace is settled. |
| `--limit N` | `100` | Maximum query results per output envelope. Must be positive. |
| `--focus GLOB` | | Admit the span path matching this glob. Repeatable; the globs are unioned. |
| `--exclude GLOB` | | Drop matching names after focus expansion. Repeatable. |

### Architecture map

| Flag | Default | Meaning |
|---|---|---|
| `--map DIR` | | After the run, write one architecture map file per entry point under `DIR`. See [praytel-map(1)](praytel-map.1.md). |

### Development

| Flag | Default | Meaning |
|---|---|---|
| `--self-trace` | off | For developing praytel: export praytel's own spans to the OTLP endpoint it was started with, which must be set (by an outer praytel). Without `--grpc-port`/`--http-port`, listens on free ports. See [CONTRIBUTING.md](../../CONTRIBUTING.md). |

Praytel never exports its own spans without `--self-trace`, and never to its
own receiver.

### Ingest and drain

| Flag | Default | Meaning |
|---|---|---|
| `--grpc-port PORT` | `4317` | OTLP gRPC listen port. |
| `--http-port PORT` | `4318` | OTLP HTTP listen port. |
| `--drain-idle DURATION` | `250ms` | Idle time with no OTLP after child exit before drain completes. |
| `--drain-timeout DURATION` | `2s` | Hard cap on drain after child exit. |

### Memory limits

| Flag | Default | Meaning |
|---|---|---|
| `--max-spans N` | `1000000` | Maximum retained spans, including pending and orphans. |
| `--max-log-lines N` | `1000000` | Maximum retained log lines, including pending and orphans. |

When a limit is reached praytel evicts the oldest closed traces, then sheds the
newest data. It reports both on stderr.

### Flag combinations

- `--query` and `--query-file` are mutually exclusive.
- `--format` and `--emit` require `--query` or `--query-file`.
- `--emit settled` requires `--format ndjson`.
- `--format` and `--emit` are not used with `--tui`.
- `--query` cannot be combined with `--focus` or `--exclude`.
- `--map` cannot be combined with `--query`, `--query-file`, `--focus` or
  `--exclude`.

## Environment

Praytel sets these variables for the child so its OpenTelemetry SDK exports to
the in-process receiver. Any `OTEL_EXPORTER_OTLP_*` variables inherited from
the caller's environment are replaced.

| Variable | Value |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | The gRPC receiver. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | The HTTP receiver's `/v1/logs` path. |
| `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL` | `http/protobuf` |

## TUI keys

Typing edits the search. Trace search is a subsequence match over span names
and attributes; a single matching span surfaces its whole trace.

| Key | Action |
|---|---|
| any character | Append to the search. |
| Backspace | Delete the last character. |
| Esc | Clear the search. |
| Tab | Switch between searching traces and searching log bodies. |
| Ctrl-G | Switch between the tree view and the call graph view. |
| ↑ / ↓ (or Ctrl-K / Ctrl-J) | Scroll one line. |
| PgUp / PgDn | Scroll one page. |
| ← / → (or Ctrl-B / Ctrl-F) | Scroll horizontally. |
| Home | Return to the left edge. |
| Ctrl-C, Ctrl-Q | Quit. |

The TUI stays open after the child exits, until you quit.

## Exit status

If the child started, praytel exits with the child's exit code. If the child
never started (it could not be executed, a port could not be bound, or the TUI
could not start) or the invocation is malformed, praytel exits with `1`.
With `--map`, if the child succeeded but the map could not be written,
praytel exits with `1`.
Warnings and praytel's own logs go to stderr.

## Examples

```sh
praytel -- go test ./...
praytel --tui -- ./server
praytel --query 'span{ span:status = error }' --format ndjson -- ./server
praytel --grpc-port 14317 --http-port 14318 -- ./server
praytel --map arch -- go test -count=1 ./...
```

## See also

[Typed queries](../guides/queries.md),
[Architecture](../guides/architecture.md)
