---
title: Focus on performance
summary: How to use typed queries on span duration to surface the slow parts of a run.
weight: 50
---

# Focus on performance

Every span carries a duration, so a run under praytel is already a profile of
your system at the level you instrumented it. Typed queries let you ask for the
slow parts directly instead of scanning the tree for big numbers.

The query language is shaped like TraceQL, but it is praytel's own small
dialect and is not compatible with Tempo's.

## Show only what is slow

[![Recording: a duration query leaves only the slow trace in the tree, first with its surrounding path and then as matches only](../assets/performance.gif)](../assets/performance.mp4)

```sh
praytel --tui --query 'span{ span:duration > 100ms }' -- ./server
```

The tree now holds only the traces with a span over 100ms. By default you get
the **path**: each slow span, its ancestors, its descendants and their logs.
That is usually what you want, because the slow span is rarely the cause; one
of its children is.

Change the view to change how much comes back:

| Query ending | You see |
|---|---|
| (nothing) or `\| path` | Slow spans with their ancestors, descendants and logs. |
| `\| matches` | Only the slow spans. |
| `\| trace` | The whole of every trace that contains a slow span. |

## Try it yourself

With `shop` installed as in
[View traces from a running app](traces.md#the-shop-app):

```sh
praytel --tui --query 'span{ span:duration > 300ms }' -- shop
```

Only the `amex` order is left. Without `--tui` the same query prints one line
of evidence per matching trace, which is the form a script can use:

```sh
praytel --query 'span{ span:duration > 300ms } | matches' -- shop -pace 50ms
```

```
05b66746... revision=25 state=final spans=[4adbc203... 8cfe1c03... ad8c1747... b7315de3...] logs=[] incomplete=[]
```

Four spans match because the slow bank call makes each of its ancestors slow
too. To get the cause rather than everything above it, ask for the slow span
that logged a retry:

```sh
praytel --query 'span{ span:duration > 100ms } > log{ log:body *= "retry" }' -- shop -pace 50ms
```

The same questions work on the tools from the first guide. Where does a
Dockerfile lint spend its time?

```sh
praytel --tui --query 'span{ span:duration > 100ms }' -- docker buildx build --call=check .
```

What is left is the build, the BuildKit solve (`moby.buildkit.v1.Control/Solve`
and the frontend's `LLBBridge/Solve`) and the status stream that runs alongside
it. Every other call buildx makes finishes well under 100ms. With Dagger, scope
the question to the CLI to see how long it takes to reach its engine before
any work starts:

```sh
praytel --tui --query 'span{ resource.service.name = "dagger-cli" && span:duration > 100ms }' -- dagger functions
```

## Ask a narrower question

Slow spans in one service:

```sh
praytel --query 'span{ resource.service.name = "checkout" && span:duration > 100ms }' -- ./server
```

Slow calls made beneath a particular entry point (`>>` means any descendant):

```sh
praytel --query 'span{ span:name = "POST /orders" } >> span{ span:duration > 100ms }' -- ./server
```

Slow outbound calls only:

```sh
praytel --query 'span{ span:kind = client && span:duration > 250ms }' -- ./server
```

Whole requests that were slow, however the time was spent:

```sh
praytel --query 'span{ trace:duration > 1s } | trace' -- ./server
```

Slow spans that also logged a retry:

```sh
praytel --query 'span{ span:duration > 100ms } > log{ log:body *= "retry" }' -- ./server
```

## Get numbers out

For a script, drop `--tui` and choose a format:

```sh
praytel --query 'span{ span:duration > 100ms } | matches' --format ndjson -- go test ./...
```

For a server that never exits, emit each trace once it has gone quiet rather
than waiting for the end:

```sh
praytel --query 'span{ span:duration > 100ms }' --emit settled --format ndjson -- ./server
```

A settled trace is provisional; late data can produce a newer revision.

## What this is not

A span duration is one measurement from one run, on your machine, at the
granularity you instrumented. It tells you where to look. It is not a
benchmark: for a number you would defend, repeat the run and compare.

## Next

[Using praytel with agents](agents.md). The full language is in
[Typed queries](queries.md) and
[praytel-query-language(7)](../reference/praytel-query-language.7.md).
