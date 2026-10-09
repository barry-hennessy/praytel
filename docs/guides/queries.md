---
title: Typed queries
summary: How to select spans and logs with praytel's typed query language and emit the results for humans or machines.
weight: 70
---

# Typed queries

The TUI's fuzzy search is loose on purpose. When you need an exact answer, such
as "did checkout produce an error span?", pass a typed query with `--query`.
Queries are checked before the child starts, so a typo fails fast instead of
after a long run.

```sh
praytel --query 'span{ resource.service.name = "checkout" && span:status = error }' -- go test ./...
praytel --query 'log{ log:body *= "connection refused" && log:severity_number >= 17 }' --format ndjson -- ./server
praytel --query 'span{ resource.service.name = "frontend" } >> span{ span:duration > 100ms }' -- ./server
praytel --tui --query 'span{ span:status = error } | path' -- ./server
```

## Reading a query

A query is evaluated one trace at a time.

- A **selector** is `span{ ... }` or `log{ ... }`. Every predicate inside one
  selector must hold for the same record.
- `&&` and `||` between selectors combine evidence from the same trace: the
  trace must contain both, or either.
- `>` means direct child and `>>` means any descendant, so
  `span{ span:name = api } >> log{ log:body *= "failed" }` finds a failing log
  anywhere beneath the `api` span.

Comparisons keep telemetry types and never coerce. A missing field does not
satisfy `!=`; write `!exists(field)` to select absence.

The full grammar, field list and operator table are in
[praytel-query-language(7)](../reference/praytel-query-language.7.md).

## Choosing how much comes back

A query ends with an optional view:

| View | Selects |
|---|---|
| `\| path` (default) | The matching records, their span ancestors and descendants, and the logs attached to those spans. |
| `\| matches` | Only the records that satisfied a selector. |
| `\| trace` | Every span and log in a matching trace. |

## Output for scripts

`--format` chooses `text` (default), `json` or `ndjson`. `--limit` caps the
results per output envelope (default 100).

For a long-running process, `--emit settled --format ndjson` emits a closed
trace once it has been quiet for `--settle-after`. "Settled" is provisional:
late OTLP can produce a newer revision. Only a drained `final` result is
terminal.

## Keeping queries in the repo

Store a query in a file and pass it with `--query-file`. Validate and
canonicalize checked-in queries without running a child:

```sh
praytel query check 'span{ span:status = error }'
praytel query fmt 'span{span:status=error}'
praytel query explain 'span{ span:name = api } >> log{ log:body *= "failed" }'
```

See [praytel-query(1)](../reference/praytel-query.1.md).

## Focus and exclude

`--focus GLOB` and `--exclude GLOB` are the older shorthand for filtering by
component or span name. They still work when no typed query is given, but
cannot be combined with `--query`.
