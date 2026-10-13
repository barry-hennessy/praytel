# Pray-tel
We wire up services with open tracing, telemetry. We emit logs. Both to get visibility
into how our systems are running. What's going right, what's going wrong. How are things wired.

But we silo the data. We have to look in two places to get a clear overview of both.

Praytel unifies logs and tracing on the command line so your development workflow
can leverage both.

 - run commands and focus log output on specific components
 - let agents wire code, and verify that they haven't just stubbed the hard bits!
 - let agents leverage logs and tracing to focus them
 - leverage logs and tracing yourself; focus you!
 - get value out of your logs in your development workflow without spinning up a docker container

Full documentation lives in [`docs/`](docs/index.md): five guides that build on
each other, from [viewing traces](docs/guides/traces.md) to
[finding what is slow](docs/guides/performance.md), a
[reference page per command](docs/index.md#reference), and
[runnable examples](docs/examples.md) for each guide.

Run `praytel -- command` for the append-only stream, or `praytel --tui -- command`
for a live trace/span tree. In the TUI, type to fuzzy-search traces (matches span
names and attributes, including trace attributes), press Tab to search log bodies
instead, use ↑/↓ to scroll, Esc to clear, and Ctrl-C to quit.

Trace search is loose by design: it's a subsequence match (letters found in order,
not a literal substring), and matching a single span anywhere in a trace surfaces
that trace's whole tree, not just the span that matched. Expect broader results
than a `grep`-style search would give you. The matched span's label and attributes
are highlighted so you can see which one actually caused the trace to show up.

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
