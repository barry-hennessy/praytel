---
title: Release plan
summary: The six releases praytel is split into, what each adds, and the documentation that ships with it.
weight: 5
---

# Release plan

Praytel is released in six steps. Each one is useful on its own and builds on
the one before, so a reader can stop at any step and have a working tool.

This is the proposed cut of what exists today. The releases are numbered in
order. Release 1 is v0.1.0; later version numbers are not assigned yet.

| # | Release | What you can do | Guide | Example |
|---|---|---|---|---|
| 1 | Traces | Run an app and watch its traces as a live tree. | [View traces from a running app](guides/traces.md) | [1](examples.md#1-traces) |
| 2 | Logs | See each log line under the span that emitted it. | [View logs correlated to traces](guides/logs.md) | [2](examples.md#2-logs) |
| 3 | Tests | Run a test suite and see where a test fails. | [View traces of tests](guides/tests.md) | [3](examples.md#3-tests) |
| 4 | Overview | See which components a run hits and how they connect. | [Get an overview of a system](guides/overview.md) | [4](examples.md#4-overview) |
| 5 | Performance | Ask typed questions, starting with "what is slow?". | [Focus on performance](guides/performance.md) | [5](examples.md#5-performance) |
| 6 | Agents | Let a coding agent run, query and verify. | [Using praytel with agents](guides/agents.md) | [6](examples.md#6-agents) |

Every release has a runnable example against the same small app; see
[Examples](examples.md).

## What each release contains

### 1. Traces

- `praytel --tui -- command`: wrap one command, forward signals, exit with its
  exit code.
- The OTLP receiver for traces (gRPC and HTTP) and the environment handed to
  the child. `--grpc-port`, `--http-port`.
- The span tree: state marks, durations, fuzzy trace search, scrolling.
- Drain after exit (`--drain-idle`, `--drain-timeout`) and `--max-spans`.

Reference: [praytel(1)](reference/praytel.1.md).

### 2. Logs

- Capture of stdout and stderr, and OTLP logs.
- Joining lines to spans by `trace_id` and `span_id`; orphan groups for lines
  that cannot be joined.
- The append-only stream, which becomes the default without `--tui`.
- Log search in the TUI (Tab) and `--max-log-lines`.

### 3. Tests

- Nothing test-specific in the binary; this release is about making a test run
  readable.
- `--focus` and `--exclude` to cut a large suite down.
- Error status carried through to the `×` mark.

### 4. Overview

- The component and section graph view (Ctrl-G), scoped by the same search and
  filters as the tree.

### 5. Performance

- The typed query language: `--query`, `--query-file`, and the `path`,
  `matches` and `trace` views.
- Machine output: `--format`, `--limit`, `--emit settled`, `--settle-after`.
- `praytel query check`, `fmt` and `explain`.

Reference: [praytel-query(1)](reference/praytel-query.1.md),
[praytel-query-language(7)](reference/praytel-query-language.7.md). Guide:
[Typed queries](guides/queries.md).

### 6. Agents

- `praytel mcp` and its six tools.
- `praytel agent instructions`.

Reference: [praytel-mcp(1)](reference/praytel-mcp.1.md),
[praytel-agent(1)](reference/praytel-agent.1.md).

## Documentation per release

Each guide uses only features from its own release or an earlier one, apart
from clearly marked pointers forward to later guides. The reference pages describe the
whole current surface; when a release is cut, trim them to the features listed
above for that release and the ones before it.
