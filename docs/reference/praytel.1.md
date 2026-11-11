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
| `--format=text\|ndjson` | `text` | How [checks](#checks) and tables report. |

### Ingest and drain

| Flag | Default | Meaning |
|---|---|---|
| `--grpc-port PORT` | `4317` | OTLP gRPC listen port. |
| `--http-port PORT` | `4318` | OTLP HTTP listen port. |
| `--drain-idle DURATION` | `250ms` | Idle time with no OTLP after child exit before drain completes. |
| `--drain-timeout DURATION` | `2s` | Hard cap on drain after child exit. |
| `--settle-after DURATION` | `500ms` | Quiet time before a trace whose spans have all ended counts as settled (see [Plans](#plans)). |

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
and attributes; a single matching span surfaces its whole trace. A search
that starts with `/` is a [query](#queries) instead: the tree and graph show
what it selects, in place, and the footer shows a query that does not parse.

| Key | Action |
|---|---|
| any character | Insert into the search at the cursor. |
| Backspace | Delete the character before the cursor. |
| Esc | Clear the search. |
| Tab | Switch between searching traces and searching log bodies. |
| Ctrl-G | Show the next pane: with `--tui`, switch between the tree view and the call graph view. |
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
| Views | `stream()`, `snapshot("tree", "graph", logs=False)`, `tree()`, `graph()`, `table(SELECTION, columns=[...])` (see [Checks](#checks)) |
| Stages | `attrs("GLOBS")` (as `--snapshot-attrs`), `no_attrs()`, `where(SELECTION)` (see [Queries](#queries)) |
| Triggers | `each` (per event: the stream), `tick` (TUI panes), `settled` (per trace, when it settles), `final` (once the run drains) |
| Sinks | `stdout`, `stderr`, `file("PATH")`, `pane("NAME")`; add `exit` to let the output decide the exit code as `--snapshot` does |

Away from the TUI, `tree()` and `graph()` print as a snapshot prints them.

A trace is **settled** when every span it has ended and nothing new arrived
for `--settle-after` (default 500ms). OTLP never says a trace is complete,
so a late span makes it live again; a `settled` output then sends it again,
with a higher revision. Each trace a `settled` output sends starts with
`# trace ID revision N STATE`, where STATE is `settled`, or `final` for a
trace still open when the run drained:

```sh
praytel -e 'tree() | settled | stderr' -- ./server
```

A file path may name the trace: `{trace_id}`, `{name}` (the root span's
name) or `{KEY}` (an attribute of the root span). The output then writes one
file per trace, with `final` or `settled`. Characters that would leave the
directory are replaced:

```sh
praytel -e 'attrs("test.name") | snapshot("tree") | final | file("testdata/{test.name}.golden")' \
  -- go test -count=1 .
```

The mode flags as `-e`:

| Flags | `-e` |
|---|---|
| (none) | `stream() \| each \| stdout` |
| `--tui` | `tree() \| tick \| pane("tree")` and `graph() \| tick \| pane("graph")` |
| `--snapshot=tree` | `no_attrs() \| snapshot("tree") \| final \| stdout \| exit` |
| `--snapshot="tree graph" --snapshot-attrs="G" --snapshot-logs` | `attrs("G") \| snapshot("tree", "graph", logs=True) \| final \| stdout \| exit` |

Outputs with `pane("NAME")` are the TUI's panes, in order: `tree()`,
`graph()` or `stream()` with `tick`, and any stages. Ctrl-G moves to the next
pane. Other outputs can run beside the TUI, but only to files, since the TUI
owns the terminal:

```sh
praytel -e 'tree() | tick | pane("calls")' \
        -e 'stream() | tick | pane("logs")' \
        -e 'snapshot("tree") | final | file("trace.txt")' -- ./server
```

## Queries

A query selects spans and logs within one trace at a time:

```text
span{ resource.service.name = "checkout" && span:status = error }
log{ log:body *= "connection refused" && log:severity >= 17 }
span{ span:component = "frontend" } >> span{ span:status = error }
```

Inside `{}`, conditions on one record join with `&&`, `||`, `!` and
parentheses. Fields are `span:name`, `span:status` (`unset`, `ok`,
`error`), `span:kind` (`server`, `client`, …), `span:component`, `span:id`,
`span:parent_id`, `trace:id`, `log:body`, `log:source` (`stdout`, `stderr`,
`otlp`), `log:severity`, `log:severity_text`; and attributes as `span.KEY`,
`log.KEY`, `resource.KEY` or a bare `KEY` of the record. Comparisons are
`=`, `!=`, `<`, `<=`, `>`, `>=`, `=~` / `!~` (anchored regular
expressions) and `*=` (contains). Types do not convert: `500` is not
`"500"`. `exists(FIELD)` tests for a field.

Between selections: `A && B` (the trace has both), `A || B` (either),
`A & B` (items in both), `!A` (spans not in A), `A > B` (B whose parent is
in A), `A >> B` (B below A) and `A ~> B` (B with an earlier sibling in A).
`count(A) >= N` keeps the items of `A` only if the trace has that many
(`=`, `!=`, `<`, `<=`, `>`, `>=`).

`span:duration` and `trace:duration` compare with durations: `200ms`,
`1.5s`, `1h30m`. A variable `$x` binds to the value it first meets on the
left of `>`, `>>` or `~>`, once per span there, and the right side can use
it in arithmetic (`+ - * /`):

```text
span{ test.name = "TestDouble" && input = $x } >> span{ output = $x * 2 }
```

In `-e`, `where(...)` takes a query string or a selection built with
`span(name=, service=, component=, kind=, status=, attrs={...})` and
`log(body=, source=, severity=, attrs={...})`; a value is a literal,
`match("RE")`, `glob("GLOB")`, `contains("TEXT")`, `longer_than("200ms")` or
`shorter_than("1s")` (for `duration=`), or a variable `var("x")` with
arithmetic. Selections join with `&`, `|`, `>>`, `~`, `b.child_of(a)` and
`b.after(a)`:

```sh
praytel -e 'where(span(service = "checkout") >> span(status = "error")) | tree() | settled | stderr' -- ./server
```

`where` shows each selected span in place: with its ancestors and
descendants and their logs (a log query keeps only the selected logs). On
the stream, which prints each line as it comes, `where` takes only a query
that one log decides, such as `log(source = "stderr")`.

## Checks

A check is a statement about selections, declared in `-e` with a call:

| Statement | Holds when |
|---|---|
| `never(S)` | nothing matches `S` |
| `some(S)` | something in the run matches `S` |
| `every(A, has = S)` | each span of `A` has an item of `S` below it |
| `every(A, has = at_most(N, S))` | … at most `N` of them (also `at_least`, `exactly`) |
| `at_least(N, S)`, `at_most(N, S)`, `exactly(N, S)` | the run has that many items of `S` |

Each check judges a trace when it settles, again if a late span gives it a
new revision, and the whole run when it drains; it reports to stderr at the
end, and gives `name = "..."` or its own text as its name:

```sh
praytel -e demo/checks/shop.star -- sh -c 'cd demo/workloads/shop && go run .'
```

The **anchor rule**: the left side of each `>` and `>>`, and the first
argument of `every`, must match at least once in the run, or the check fails
with `check never exercised`. A span renamed in the code then fails the
check instead of passing it with nothing to judge. To say something never
happens, write `never(S)`.

A trace with something missing (a span that named a parent that never
arrived, a span that logs named but that never arrived, a span that never
ended, telemetry dropped under memory pressure) never passes: its result is
`incomplete`. `--format=ndjson` reports one JSON object per judged trace
(`check`, `trace_id`, `revision`, `outcome`, `evidence`,
`incomplete_reasons`) and one for the run.

Loops and functions make table tests:

```python
for x in [0, 1, 2]:
    every(span(attrs = {"test.name": "TestDouble", "input": x}),
          has = span(attrs = {"output": x * 2}))
```

A variable makes the same check for inputs not known before the run, each
test with its own:

```python
x = var("x")
every(span(attrs = {"test.name": "TestDouble", "input": x}),
      has = span(attrs = {"output": x * 2}))
```

And a count bounds each anchor, such as an N+1 query:

```python
every(span(kind = "server"), has = at_most(10, span(attrs = {"db.system": "postgresql"})))
```

`table(SELECTION, columns = [...])` writes one row per selected span, with
the fields of its columns (`span:name`, `span:duration`, `test.name`, …):
tab-separated under a header, or JSON objects with `--format=ndjson`. Rows
are sorted, so a table can be a golden:

```sh
praytel -e 'table(span(name = "test.TestDouble"), columns = ["input", "output"]) | final | file("testdata/double.tsv")' -- go test -count=1 .
```

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

With [checks](#checks), the report always goes to stderr. If the child
exits non-zero its code wins; otherwise praytel exits `4` if a check fails,
`3` if one is incomplete, and `0` if all pass.

## Examples

```sh
praytel -- go test ./...
praytel --tui -- ./server
praytel --snapshot="tree graph" --snapshot-attrs="http.* -http.url" -- go test -count=1 .
praytel --grpc-port 14317 --http-port 14318 -- ./server
```
