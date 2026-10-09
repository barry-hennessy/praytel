---
title: Praytel
summary: Praytel unifies a command's logs and OpenTelemetry traces on the command line.
weight: 1
---

# Praytel

Praytel is a transparent CLI wrapper: `praytel [flags] -- command`. It runs one
command, captures its stdout/stderr and the OTLP traces and logs it exports,
joins them, and prints a single view. There is no daemon, no collector and no
leftover files.

```sh
praytel -- go test ./...          # append-only stream
praytel --tui -- ./server         # live trace/span tree
```

## Guides

- [Typed queries](guides/queries.md): select spans and logs precisely, and get
  machine-readable output.
- [Agent integration](guides/agents.md): let a coding agent run commands under
  praytel and inspect the evidence over MCP.
- [Architecture](guides/architecture.md): how ingest, join, selection and
  rendering fit together.

## Reference

- [praytel(1)](reference/praytel.1.md): the wrapper command, its flags, TUI keys
  and exit status.
- [praytel-query(1)](reference/praytel-query.1.md): check, format and explain a
  query without running a child.
- [praytel-mcp(1)](reference/praytel-mcp.1.md): the stdio MCP server and its
  tools.
- [praytel-agent(1)](reference/praytel-agent.1.md): print the agent playbook.
- [praytel-query-language(7)](reference/praytel-query-language.7.md): selectors,
  fields, operators and views.

## Writing these docs

Everything under `docs/` is plain CommonMark with relative `.md` links, so it
reads in a forge, an editor or `less`. Each file starts with three frontmatter
fields: `title`, `summary` (one sentence) and `weight` (sort order within its
directory). Prose goes in `guides/`; `reference/` holds one file per command or
configuration surface, named after its manual page.
