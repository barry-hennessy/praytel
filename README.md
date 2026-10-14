# Praytel

We instrument services with OpenTelemetry and we write logs, both to see how
our systems run: what goes right, what goes wrong, how things are wired. But
the two end up in different places, and both usually need a collector, a
backend and a browser tab before you see anything.

Praytel puts them together on the command line. Wrap one command, and its
traces and logs are joined and shown in the terminal you ran it from: each log
line under the span that wrote it. There is no daemon, no collector and no
leftover files, so it fits into the way you already develop.

```sh
praytel -- ./server           # append-only stream of joined logs
praytel --tui -- ./server     # live trace tree, with logs under their spans
```

## Install

With Go installed:

```sh
go install github.com/barry-hennessy/praytel/cmd/praytel@latest
```

The command you wrap must export OpenTelemetry over OTLP. Praytel points it at
an in-process receiver through the standard `OTEL_EXPORTER_OTLP_*` variables,
so an SDK that configures itself from the environment needs no change. To try
praytel before instrumenting anything, the
[first guide](docs/guides/traces.md#try-it-yourself) runs it against a small
app that ships with this repository and against tools you may already have.

## What praytel does

Praytel is released in stages. Each one is useful on its own and builds on the
one before; each has a guide with commands to run yourself.

### Traces and logs on the command line

Run a command under praytel to see its traces as a live tree, with each log
line under the span that wrote it. Type to search traces, press Tab to search
log bodies. Lines that name no span are still shown, as orphans, never guessed.

[View traces from a running app](docs/guides/traces.md) ·
[View logs correlated to traces](docs/guides/logs.md)

### An overview of your system

Press Ctrl-G in the TUI for a graph of the spans that ran and which called
which, drawn from what actually happened rather than from a diagram.

[Get an overview of a system](docs/guides/overview.md)

### Traces of your tests

Wrap a test run to see where a test failed, not only that it did, and narrow a
large suite with `--focus` and `--exclude`.

[View traces of tests](docs/guides/tests.md)

### Focus on performance

Ask typed questions of a run, starting with "what is slow?", and get
machine-readable results:

```sh
praytel --tui --query 'span{ span:duration > 100ms }' -- ./server
praytel --query 'span{ span:status = error } | matches' --format ndjson -- go test ./...
```

[Focus on performance](docs/guides/performance.md) ·
[Typed queries](docs/guides/queries.md)

## Documentation

Everything is in [`docs/`](docs/index.md): the guides in release order, a
[reference page per command](docs/index.md#reference), how praytel is
[put together](docs/guides/architecture.md), and the
[release notes](docs/releases.md).
