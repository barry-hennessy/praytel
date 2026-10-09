---
title: praytel-agent(1)
summary: Print the concise playbook an agent should follow when using praytel's MCP tools.
weight: 40
---

# praytel-agent(1)

## Synopsis

```
praytel agent instructions
```

## Description

Prints the agent playbook to stdout. It is short enough to include verbatim in
an agent skill or system prompt, and describes the progressive order in which
to call the [praytel-mcp(1)](praytel-mcp.1.md) tools.

## Output

```
Use Praytel progressively:
1. start_run with a focused query and bounded limits.
2. wait_for_matches until items arrive, the run is terminal, or timed_out is true.
3. inspect trace topology before requesting span or log details.
4. request details only for evidence IDs.
5. stop long-running runs when verification is complete.

Do not claim that telemetry is absent unless run_state is exited or failed,
truncated is false, and incomplete_reasons is empty.
```

## See also

[Agent integration](../guides/agents.md)
