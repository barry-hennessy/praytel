---
title: Release plan
summary: The five releases praytel is split into, what each adds, and the documentation that ships with it.
weight: 5
---

# Release plan

Praytel is released in five steps. Each one is useful on its own and builds on
the one before, so a reader can stop at any step and have a working tool.

| Version | Release | What you can do | Guide |
|---|---|---|---|
| v0.0.1 | Traces and logs | Run an app, watch its traces as a live tree, and see each log line under the span that emitted it. | [View traces from a running app](guides/traces.md), [View logs correlated to traces](guides/logs.md) |
| v0.0.2 | Overview | See which components a run hits and how they connect. | [Get an overview of a system](guides/overview.md) |
| v0.0.3 | Tests | Run a test suite, see where a test fails, and cut a large run down. | [View traces of tests](guides/tests.md) |
| v0.0.4 | Performance | Ask typed questions, starting with "what is slow?". | [Focus on performance](guides/performance.md) |
| v0.0.5 | Agents | Let a coding agent run, query and verify. | [Using praytel with agents](guides/agents.md) |

Each guide includes commands to run yourself, so every release can be tried
without instrumenting anything first.

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

Reference: [praytel-query(1)](reference/praytel-query.1.md),
[praytel-query-language(7)](reference/praytel-query-language.7.md). Guide:
[Typed queries](guides/queries.md).

### v0.0.5: Agents

- `praytel mcp` and its six tools.
- `praytel agent instructions`.

Reference: [praytel-mcp(1)](reference/praytel-mcp.1.md),
[praytel-agent(1)](reference/praytel-agent.1.md).

## Documentation per release

Each tag carries the documentation for exactly what it ships: its own guide
and reference, and those of the releases before it. A guide, including the
commands it asks you to run, uses only features from its own release or an
earlier one.
