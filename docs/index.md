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

Read these in order. Each builds on the one before, and they follow the
[release plan](releases.md): the first two ship together, then one per release.

1. [View traces from a running app](guides/traces.md): understand the apps you
   use, and have good traces ready for when something breaks.
2. [View logs correlated to traces](guides/logs.md): each log line under the
   span that emitted it.
3. [Get an overview of a system](guides/overview.md): which components are hit
   and how they are really wired together.

Each guide has a matching entry in [Examples](examples.md): one small
instrumented app and a command to run against it, so you can try a guide
before instrumenting anything of your own.

## Reference

- [praytel(1)](reference/praytel.1.md): the wrapper command, its flags, TUI keys
  and exit status.

## Writing these docs

Everything under `docs/` is plain CommonMark with relative `.md` links, so it
reads in a forge, an editor or `less`. Each file starts with three frontmatter
fields: `title`, `summary` (one sentence) and `weight` (sort order within its
directory). Prose goes in `guides/`; `reference/` holds one file per command or
configuration surface, named after its manual page.

The recordings in the guides live in `docs/assets/` and are generated from
scripts by `make docs`; see [demo/README.md](../demo/README.md). They are refreshed when a
release is published.
