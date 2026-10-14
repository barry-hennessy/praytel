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

