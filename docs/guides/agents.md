---
title: Agent integration
summary: How to give a coding agent access to praytel over MCP so it can verify behaviour from traces and logs.
weight: 20
---

# Agent integration

An agent that can run a command under praytel can check what the code actually
did: which spans ran, which logs they emitted, and whether the hard part was
wired up or only stubbed.

## Connect an MCP client

`praytel mcp` runs a stdio MCP server rooted at its working directory.
Configure the client to launch it:

```json
{"command":"praytel","args":["mcp"]}
```

The server owns several bounded, in-memory runs for that one client. Each run
wraps one child, leaves no files, and is cancelled when the server exits.

## The investigation loop

The tools use progressive disclosure, so an agent searches compact evidence
before loading span or log detail:

1. `start_run` with a focused [typed query](queries.md) and bounded limits.
2. `wait_for_matches` until items arrive, the run is terminal, or `timed_out`
   is true.
3. `get_trace_topology` to inspect a trace's shape before asking for detail.
4. `get_span_details` and `get_logs` only for the IDs that look suspicious.
5. `stop_run` when verification of a long-running process is complete.

Each tool is described in [praytel-mcp(1)](../reference/praytel-mcp.1.md).

## When "no match" is a real answer

A "no match" conclusion is valid only after the run is terminal (`run_state` is
exited or failed), `truncated` is false, and `incomplete_reasons` is empty.
Before that, the telemetry may simply not have arrived yet.

## Put the playbook in a skill

`praytel agent instructions` prints the concise version of this loop, suitable
for pasting into an agent skill or system prompt. See
[praytel-agent(1)](../reference/praytel-agent.1.md).
