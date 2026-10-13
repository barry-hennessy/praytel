---
title: praytel-mcp(1)
summary: Serve praytel's trace and log investigation tools to one MCP client over stdio.
weight: 30
---

# praytel-mcp(1)

## Synopsis

```
praytel mcp
```

## Description

Runs a Model Context Protocol server on stdin/stdout until the client
disconnects or the process is signalled. The server is rooted at its working
directory. It takes no flags.

The server owns several bounded, in-memory runs. Each run wraps one child
command, leaves no files, and is cancelled when the server exits. It never
exposes a child's inherited environment or raw command output.

Client configuration:

```json
{"command":"praytel","args":["mcp"]}
```

## Tools

| Tool | Effect | Purpose |
|---|---|---|
| `start_run` | Executes a process; not idempotent | Start a local command under praytel and collect joined traces and logs. Returns a `run_id` and a cursor. |
| `wait_for_matches` | Read-only, idempotent | Wait from a cursor for compact matching trace revisions. A timeout is returned as `timed_out=true`. |
| `get_trace_topology` | Read-only, idempotent | Return a bounded depth-first trace skeleton without attributes or log bodies. |
| `get_span_details` | Read-only, idempotent | Return typed details for at most 20 explicit span IDs from topology or query evidence. |
| `get_logs` | Read-only, idempotent | Return typed details for at most 50 explicit joined log IDs from query evidence. |
| `stop_run` | Changes process state | Send SIGTERM to a run's child and wait for telemetry drain. |

## Completeness

Do not conclude that telemetry is absent unless `run_state` is terminal
(exited or failed), `truncated` is false, and `incomplete_reasons` is empty.

## See also

[Agent integration](../guides/agents.md),
[praytel-agent(1)](praytel-agent.1.md),
[praytel-query-language(7)](praytel-query-language.7.md)
