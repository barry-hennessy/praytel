---
title: praytel(1)
summary: Run a command and print its logs and OpenTelemetry traces as one joined view.
weight: 10
---

# praytel(1)

## Synopsis

```
praytel [flags] -- command [args ...]
```

## Description

`praytel` runs `command` to completion, forwards signals to it, captures its
stdout and stderr, receives the OTLP traces and logs it exports, joins them and
prints a unified view. The `--` separator is required.

By default the output is an append-only stream. `--tui` renders a live
trace/span tree instead.

## Flags

### Rendering

| Flag | Default | Meaning |
|---|---|---|
| `--tui` | off | Render an interactive live trace tree. |
| `--snapshot="tree graph"` | off | Print one deterministic plain-text snapshot of the trace tree and/or component graph (see [Snapshots](#snapshots)). No default: give at least one view, with `=`. Cannot be combined with `--tui`. |
| `--snapshot-attrs="GLOBS"` | none | Span attributes to show in a snapshot: whitespace-separated key globs, `-` excludes (excludes win). Bare `--snapshot-attrs` shows all; `"-c"` shows all but `c`. Always give a selection with `=`. |
| `--snapshot-logs` | off | Include log lines (and captured stdout/stderr) in a snapshot. |
| `-e`, `--execute=CODE\|FILE.star` | none | Declare the outputs in Starlark instead (see [Plans](#plans)). Repeatable; a value ending in `.star` is a file. Cannot be combined with `--tui` or `--snapshot`. |
| `--print-plan` | off | Print the plan the other flags make, as JSON, and exit. Needs no command. |

### Ingest and drain

| Flag | Default | Meaning |
|---|---|---|
| `--grpc-port PORT` | `4317` | OTLP gRPC listen port. |
| `--http-port PORT` | `4318` | OTLP HTTP listen port. |
| `--drain-idle DURATION` | `250ms` | Idle time with no OTLP after child exit before drain completes. |
| `--drain-timeout DURATION` | `2s` | Hard cap on drain after child exit. |

### Memory limits

| Flag | Default | Meaning |
|---|---|---|
| `--max-spans N` | `1000000` | Maximum retained spans, including pending and orphans. |
| `--max-log-lines N` | `1000000` | Maximum retained log lines, including pending and orphans. |

When a limit is reached praytel evicts the oldest closed traces, then sheds the
newest data. It reports both on stderr.

## Environment

Praytel sets these variables for the child so its OpenTelemetry SDK exports to
the in-process receiver. Any `OTEL_EXPORTER_OTLP_*` variables inherited from
the caller's environment are replaced.

| Variable | Value |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | The gRPC receiver. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | The HTTP receiver's `/v1/logs` path. |
| `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL` | `http/protobuf` |

## TUI keys

Typing edits the search. Trace search is a subsequence match over span names
and attributes; a single matching span surfaces its whole trace.

| Key | Action |
|---|---|
| any character | Insert into the search at the cursor. |
| Backspace | Delete the character before the cursor. |
| Esc | Clear the search. |
| Tab | Switch between searching traces and searching log bodies. |
| Ctrl-G | Switch between the tree view and the call graph view. |
| ↑ / ↓ (or Ctrl-K / Ctrl-J) | Scroll one line. |
| PgUp / PgDn | Scroll one page. |
| ← / → (or Ctrl-B / Ctrl-F) | Move the cursor in the search; with no search, scroll horizontally. |
| Shift-← / Shift-→ | Scroll horizontally, even during a search. |
| Home | Return to the left edge. |
| Ctrl-C, Ctrl-Q | Quit. |

The TUI stays open after the child exits, until you quit.

## Snapshots

`--snapshot` runs the child to completion, waits for the drain and prints a
golden-file rendering of the run to stdout, ordered by content rather than
arrival, so the same run produces the same bytes:

```
# praytel-snapshot v1
## tree
...
## graph
...
```

The tree is the TUI's, without durations, colour or synthetic spans; rules are
80 columns wide and long content is never wrapped. Attributes are sorted by
key. Update a golden with a redirect and check one with a diff:

```sh
praytel --snapshot=tree -- go test -count=1 . > testdata/trace.golden
praytel --snapshot=tree -- go test -count=1 . | diff -u testdata/trace.golden -
```

Praytel prints a snapshot only when the run is provably complete: the drain
finished, no data was evicted or shed, no span is unclosed, declared but unseen
or missing its parent, and at least one span arrived. Otherwise stdout stays
empty (see the exit status below).

Attribute values that change between runs (ports, temp paths, ids) make a
snapshot unstable; hide them with `--snapshot-attrs="-key"`.

For a test suite, praytel cannot do these for you:

- Run with `-count=1`; a cached `go test` emits no spans.
- Flush the tracer in `TestMain`; a binary that exits without flushing loses
  its last spans.
- Wrap each test in a root span carrying `test.name`, or tests cannot be told
  apart in one golden.
- Produce one golden per package, from the package directory.

## Plans

Every run follows a plan: a list of outputs, each made of stages, one view,
one trigger and one sink. The mode flags are shorthand for plans;
`--print-plan` shows the plan as JSON. `-e` declares outputs in
[Starlark](https://github.com/google/starlark-go/blob/master/doc/spec.md),
joining the parts of an output with `|`:

```sh
praytel -e 'snapshot("tree") | final | file("tree.txt") | exit' \
        -e 'stream() | each | stdout' -- go test -count=1 .
```

A snippet is an expression (an output, or a list of outputs) or statements
that call `output(...)`. A `.star` file is statements, and may `load()` other
`.star` files below the current directory. Scripts run once, before the child
starts; they cannot read files, the time or random values.

| Part | Builtins |
|---|---|
| Views | `stream()`, `snapshot("tree", "graph", logs=False)`, `tree()`, `graph()` |
| Stages | `attrs("GLOBS")` (as `--snapshot-attrs`), `no_attrs()` |
| Triggers | `each`, `tick`, `final` |
| Sinks | `stdout`, `stderr`, `file("PATH")`, `pane("NAME")`; add `exit` to let the output decide the exit code as `--snapshot` does |

The mode flags as `-e`:

| Flags | `-e` |
|---|---|
| (none) | `stream() \| each \| stdout` |
| `--tui` | `tree() \| tick \| pane("tree")` and `graph() \| tick \| pane("graph")` |
| `--snapshot=tree` | `no_attrs() \| snapshot("tree") \| final \| stdout \| exit` |
| `--snapshot="tree graph" --snapshot-attrs="G" --snapshot-logs` | `attrs("G") \| snapshot("tree", "graph", logs=True) \| final \| stdout \| exit` |

TUI panes cannot yet share a run with other outputs.

## Exit status

If the child started, praytel exits with the child's exit code. If the child
never started (it could not be executed, a port could not be bound, or the TUI
could not start) or the invocation is malformed, praytel exits with `1`.
Warnings and praytel's own logs go to stderr.

With `--snapshot`:

| Case | Stdout | Stderr | Exit |
|---|---|---|---|
| Child exits 0, snapshot complete | the snapshot | | `0` |
| Child exits non-zero | nothing | the child's captured output, then any problems | the child's code |
| Child exits 0, snapshot incomplete | nothing | the problems | `3` |

## Examples

```sh
praytel -- go test ./...
praytel --tui -- ./server
praytel --snapshot="tree graph" --snapshot-attrs="http.* -http.url" -- go test -count=1 .
praytel --grpc-port 14317 --http-port 14318 -- ./server
```
