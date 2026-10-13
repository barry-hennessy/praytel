---
title: Release plan
summary: The five releases praytel is split into, what each adds, and the documentation that ships with it.
weight: 5
---

# Release plan

Praytel is released in five steps. Each one is useful on its own and builds on
the one before, so a reader can stop at any step and have a working tool.

| Version | Release | What you can do | Guide | Example |
|---|---|---|---|---|
| v0.0.1 | Traces and logs | Run an app, watch its traces as a live tree, and see each log line under the span that emitted it. | [View traces from a running app](guides/traces.md), [View logs correlated to traces](guides/logs.md) | [1](examples.md#1-traces-and-logs) |
| v0.0.2 | Overview | See which components a run hits and how they connect. | [Get an overview of a system](guides/overview.md) | [2](examples.md#2-overview) |
| v0.0.3 | Tests (planned) | Run a test suite, see where a test fails, and cut a large run down. | View traces of tests | 3 |
| v0.0.4 | Performance (planned) | Ask typed questions, starting with "what is slow?". | Focus on performance | 4 |
| v0.0.5 | Agents (planned) | Let a coding agent run, query and verify. | Using praytel with agents | 5 |

Every release has a runnable example against the same small app; see
[Examples](examples.md).

## What each release contains

### v0.0.1: Traces and logs

- `praytel -- command`: wrap one command, forward signals, exit with its exit
  code.
- The OTLP receiver for traces and logs (gRPC and HTTP) and the environment
  handed to the child. `--grpc-port`, `--http-port`.
- Capture of stdout and stderr, joined to spans by `trace_id` and `span_id`;
  orphan groups for lines that cannot be joined.
- The append-only stream, which is the default.
- `--tui`: the span tree, with state marks, durations, fuzzy trace search, log
  search (Tab) and scrolling.
- Drain after exit (`--drain-idle`, `--drain-timeout`), `--max-spans` and
  `--max-log-lines`.

Reference: [praytel(1)](reference/praytel.1.md).

### v0.0.2: Overview

- The graph view (Ctrl-G): one box per span name, an arrow per observed call,
  narrowed by the same search as the tree.

### v0.0.3: Tests

- `--focus` and `--exclude` to cut a large suite down. They narrow the stream,
  the tree and the graph alike.
- The guide to reading a test run; the rest of what it uses shipped in v0.0.1.

### v0.0.4: Performance

- The typed query language: `--query`, `--query-file`, and the `path`,
  `matches` and `trace` views.
- Machine output: `--format`, `--limit`, `--emit settled`, `--settle-after`.
- `praytel query check`, `fmt` and `explain`.

### v0.0.5: Agents

- `praytel mcp` and its six tools.
- `praytel agent instructions`.

## Documentation per release

Each tag carries the documentation for exactly what it ships: its own guide,
example and reference, and those of the releases before it. A guide uses only
features from its own release or an earlier one.
