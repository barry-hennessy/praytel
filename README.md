# Pray-tel
We wire up services with tracing. We emit logs.
All in service of observability.

What's going right, what's going wrong. How are things wired.

But we silo the data. We set up exporters, stores, and GUIs. But only in our k8s environments.

Praytel unifies logs and tracing on the command line so your development workflow
can leverage both.

 - leverage logs and tracing as you develop; your tools _helping_ you focus!
 - build useful tracing for yourself during development and profit when a fire breaks out in production.
 - all without spinning up another docker container on your machine

Full documentation and guides live in [`docs/`](docs/index.md).

# Tl; dr:

Run:
 - `praytel -- command` for the append-only stream
 - `praytel --tui -- command` for a live trace/span tree.

In the TUI, type to fuzzy-search traces (matches span names and attributes,
including trace attributes), press Tab to search log bodies
instead, use ↑/↓ to scroll, Esc to clear, and Ctrl-C to quit.

## Typed queries

Use `--query` for precise, typed matching across joined spans and logs:

```sh
praytel --query 'span{ resource.service.name = "checkout" && span:status = error }' -- go test ./...
praytel --query 'log{ log:body *= "connection refused" && log:severity_number >= 17 }' --format ndjson -- ./server
praytel --query 'span{ resource.service.name = "frontend" } >> span{ span:duration > 100ms }' -- ./server
praytel --tui --query 'span{ span:status = error } | path' -- ./server
```

`>` means direct child and `>>` means any descendant. Predicates inside one
selector apply to the same record; `&&` between selectors means the same trace
contains both. Queries default to the matching path. Append `| matches` for only
the evidence records or `| trace` for the complete trace.

Validate and canonicalize checked-in queries without running a child:

```sh
praytel query check 'span{ span:status = error }'
praytel query fmt 'span{span:status=error}'
praytel query explain 'span{ span:name = api } >> log{ log:body *= "failed" }'
```

For a long-running process, `--emit settled --format ndjson` emits a closed trace
after it has been quiet for `--settle-after`. “Settled” is provisional: late OTLP
can produce a newer revision. Only a drained `final` result is terminal.

## Agent integration

`praytel mcp` runs a stdio MCP server rooted at its working directory. It exposes
`start_run`, `wait_for_matches`, `get_trace_topology`, `get_span_details`,
`get_logs`, and `stop_run`. Tools use progressive disclosure so an agent searches
compact evidence before loading span or log detail. Configure an MCP client to run:

```json
{"command":"praytel","args":["mcp"]}
```

Run `praytel agent instructions` for the concise playbook to include in an agent
skill. A “no match” conclusion is valid only after the run is terminal and both
`truncated` and `incomplete_reasons` show that the result is complete.
