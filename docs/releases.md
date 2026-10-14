---
title: Release notes
summary: What each praytel release adds, in order, and where its documentation lives.
weight: 5
---

# Release notes

Praytel is released in stages. Each release is useful on its own and builds on
the one before, so you can stop at any of them and have a working tool.

| Version | Release | What you can do | Guide |
|---|---|---|---|
| v0.1.0 | Traces and logs on the command line | Run a command and watch its traces as a live tree, with each log line under the span that wrote it. | [View traces from a running app](guides/traces.md), [View logs correlated to traces](guides/logs.md) |
| v0.2.0 | An overview of your system | See which components a run hits and how they call each other. | [Get an overview of a system](guides/overview.md) |
| v0.3.0 | Traces of your tests | Run a test suite, see where a test fails, and cut a large run down. | [View traces of tests](guides/tests.md) |
| v0.4.0 | Focus on performance | Ask typed questions, starting with "what is slow?", and get machine-readable results. | [Focus on performance](guides/performance.md) |
| v0.5.0 | Praytel for agents | Let a coding agent run a command, query its telemetry and verify its work. | [Using praytel with agents](guides/agents.md) |

## v0.1.0: Traces and logs on the command line

- `praytel -- command`: wrap one command, forward signals, exit with its exit
  code.
- An OTLP receiver for traces and logs (gRPC and HTTP), and the environment
  that points the child at it. `--grpc-port`, `--http-port`.
- Capture of stdout and stderr, joined to spans by `trace_id` and `span_id`;
  orphan groups for lines that cannot be joined.
- The append-only stream, which is the default.
- `--tui`: the span tree, with state marks, durations, fuzzy trace search, log
  search (Tab) and scrolling.
- Drain after exit (`--drain-idle`, `--drain-timeout`), `--max-spans` and
  `--max-log-lines`.

Reference: [praytel(1)](reference/praytel.1.md).

## v0.2.0: An overview of your system

- The graph view (Ctrl-G): one box per span name, an arrow per observed call,
  narrowed by the same search as the tree.

## v0.3.0: Traces of your tests

- `--focus` and `--exclude` to cut a large suite down. They narrow the stream,
  the tree and the graph alike.
- A guide to reading a test run; everything else it uses shipped earlier.

## v0.4.0: Focus on performance

- The typed query language: `--query`, `--query-file`, and the `path`,
  `matches` and `trace` views, in the stream and the TUI.
- Machine output: `--format`, `--limit`, `--emit settled`, `--settle-after`.
- `praytel query check`, `fmt` and `explain`.

Reference: [praytel-query(1)](reference/praytel-query.1.md),
[praytel-query-language(7)](reference/praytel-query-language.7.md). Guide:
[Typed queries](guides/queries.md).

## v0.5.0: Praytel for agents

- `praytel mcp`: a stdio MCP server with six tools for running a command and
  investigating its telemetry progressively.
- `praytel agent instructions`: the playbook for an agent skill or system
  prompt.

Reference: [praytel-mcp(1)](reference/praytel-mcp.1.md),
[praytel-agent(1)](reference/praytel-agent.1.md).
