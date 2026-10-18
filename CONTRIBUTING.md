# Contributing

Praytel is a Go module. `go test ./...` runs everything; CI also runs the
workflows in [`.github/workflows`](.github/workflows). How praytel is put
together is in [docs/guides/architecture.md](docs/guides/architecture.md).

## Developing praytel with praytel

Praytel is instrumented like any app: its components (the run, correlator,
receiver, capture and renderers) start spans, and its stderr logs carry their
trace and span IDs. In regular use those spans go nowhere. Start a praytel
with `--self-trace` under another praytel and they go to the outer one, so you
can look at praytel with praytel.

Build a praytel to do the looking, from any checkout:

```sh
go build -o /tmp/praytel ./cmd/praytel
```

### Look inside a run

```sh
/tmp/praytel --tui -- /tmp/praytel --self-trace -- shop -pace 50ms
```

The outer praytel shows the inner one's spans as service `praytel`: the
`praytel` run span, with `correlator`, `receiver`, `capture` and `stream` under
it, and the inner praytel's own log lines joined under the span that wrote
them. Each OTLP export the inner praytel receives is a trace of its own,
`receiver.grpc.traces`, `receiver.http.logs` and so on. Swap `shop` (see
[docs/examples.md](docs/examples.md)) for any command.

`--self-trace` needs the endpoint the outer praytel sets, and listens on free
ports unless given `--grpc-port`/`--http-port`, so the two never clash.

The outer praytel also picks up the inner one's output, including the child's
log lines, which name the child's traces. It hasn't seen those traces, so it
shows them as orphans.

### Trace the end-to-end tests

```sh
/tmp/praytel --tui -- go test -count=1 ./cmd/praytel -args -self-trace
```

`-self-trace` makes every praytel the tests start pass `--self-trace`, so each
one shows up as a trace of its own. Its `praytel.command` attribute is the
command it wrapped, which tells the runs apart:

```sh
/tmp/praytel -q 'span{ span.praytel.command *= "export-grpc-span" }' -- go test -count=1 ./cmd/praytel -args -self-trace
```

The `traced` job in [test.yml](.github/workflows/test.yml) runs the tests this
way, so a failing test in CI comes with what each praytel did.

Spans don't yet record errors
([bones#2](https://github.com/barry-hennessy/bones/issues/2)), so
`span{ span:status = error }` finds nothing in praytel's own spans; look for
error-level logs instead: `log{ log:body *= "level=ERROR" }`.

### Praytel's own architecture map

[`arch/`](arch) is praytel's [architecture map](docs/guides/maps.md), stamped
from its own spans; [`demo/workloads/shop/arch`](demo/workloads/shop/arch) is
the demo shop's. Both are made in one nested run, and CI fails when either is
out of date:

```sh
make map         # re-stamp both, then commit them with your change
make map-diff    # draw how they differ from main
```

A change to how praytel's components call each other shows up in `arch/` and
in the pull request's `maps` job summary, so it gets reviewed as a change to
the architecture.
