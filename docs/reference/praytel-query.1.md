---
title: praytel-query(1)
summary: Check, format or explain a typed query without running a child command.
weight: 20
---

# praytel-query(1)

## Synopsis

```
praytel query check QUERY
praytel query fmt QUERY
praytel query explain QUERY
```

## Description

Each subcommand parses and type-checks `QUERY` first. No child process is run
and nothing is read from the network.

| Subcommand | Output |
|---|---|
| `check` | Nothing. Succeeds when the query is valid. |
| `fmt` | The query in canonical syntax. |
| `explain` | The view, the finality a conclusion needs, and the expression tree with each field's type. |

## Exit status

`0` when the query is valid, `1` otherwise. A diagnostic names the byte range
of the offending source, as `query START:END: message`.

## Examples

```sh
$ praytel query fmt 'span{span:status=error}|trace'
span{ span:status = error } | trace

$ praytel query explain 'span{ span:name = api } >> log{ log:body *= "failed" }'
view: path (evidence, span ancestors and descendants, and their logs)
required finality: none for matches; final for absence claims
expression:
  descendant
    span selector (span:name = api)
      field span:name: string
    log selector (log:body *= "failed")
      field log:body: string
```

## See also

[praytel-query-language(7)](praytel-query-language.7.md),
[praytel(1)](praytel.1.md)
