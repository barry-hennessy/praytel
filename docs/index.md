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

Both guides have commands you can run against a small example app, or against
tools you may already have, so you can see whether praytel is useful to you
before instrumenting any code of your own.

1. [View traces from a running app](guides/traces.md): understand the apps you
   use, and have good traces ready for when something breaks.
2. [View logs correlated to traces](guides/logs.md): each log line under the
   span that emitted it.

## Reference

- [praytel(1)](reference/praytel.1.md): the wrapper command, its flags, TUI keys
  and exit status.
