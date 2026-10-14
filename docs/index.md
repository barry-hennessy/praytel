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

Read these in order. Each builds on the one before and arrived with the release
named beside it; see the [release notes](releases.md). Every guide has commands
you can run yourself, against a small app that ships with praytel and against
tools you may already have, so you can try each one before instrumenting
anything of your own.

1. [View traces from a running app](guides/traces.md) (v0.1.0): understand the
   apps you use, and have good traces ready for when something breaks.
2. [View logs correlated to traces](guides/logs.md) (v0.1.0): each log line
   under the span that emitted it.
3. [Get an overview of a system](guides/overview.md) (v0.2.0): which components
   are hit and how they are really wired together.

Going deeper:

- [Architecture](guides/architecture.md): how ingest, join and rendering fit
  together.

## Reference

- [praytel(1)](reference/praytel.1.md): the wrapper command, its flags, TUI keys
  and exit status.

## Writing these docs

Everything under `docs/` is plain CommonMark with relative `.md` links, so it
reads in a forge, an editor or `less`. Each file starts with three frontmatter
fields: `title`, `summary` (one sentence) and `weight` (sort order within its
directory). Prose goes in `guides/`; `reference/` holds one file per command or
configuration surface, named after its manual page.

Each release carries the documentation for exactly what it ships: its own
guide and reference, those of the releases before it, and nothing that points
at a later one. Write a guide, including the commands it asks readers to run,
using only features from its own release or an earlier one.

The recordings in the guides live in `docs/assets/` and are generated from
scripts by `make docs`; see [demo/README.md](../demo/README.md). They are
refreshed when a release is published.
