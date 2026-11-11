---
title: Pipeline
summary: The planned core of praytel. Each output is a stage list, a view, a trigger and a sink over one projection of the event stream. A plan from flags or Starlark configures the outputs.
weight: 81
---

# Pipeline

Status: Decided. [Architecture](ARCHITECTURE.md) gives the summary. This
document gives the detail. The code changes to this design in phases. Each
phase keeps the golden files and the benchmarks.

This document is in ASD-STE100 Simplified Technical English. Technical names
and code identifiers are in `code` format.

## Terms

Use these terms with these meanings only.

| Term | Meaning |
|---|---|
| Event | One `domain.Event` value from the correlator. |
| Control event | `DrainComplete`, `ForestEvicted` or `ForestShed`. |
| Driver | The one goroutine that owns the projection. |
| Projection | The one model of spans, logs and traces. It keeps all data that the correlator sends. No stage changes it. |
| Stage | A record-local filter or change, for example `attrs` or `where`. An output applies its stages. Refer to "Stages". |
| View | A function that makes output from the projection. |
| Viewport | The visible area of a TUI pane: width, height and scroll position. |
| Trigger | The rule that tells a view when to send output: `each`, `tick`, `settled` or `final`. |
| Sink | The destination of output: stdout, stderr, a file, a TUI pane or the exit code. |
| Output | One set of stages, one view, one trigger and one sink. |
| Pane | One output with a TUI sink. |
| Plan | A list of outputs. |
| Snippet | Starlark code that you give as the value of `-e`. |
| Record-local query | A query that a single span or a single log can satisfy. |
| Structural query | A query that uses the relation between spans (`>` or `>>`). |
| Selection | A set of spans or logs. `span(...)`, `log(...)`, a query string or the selection operators describe it. |
| Anchor | A selection that sets the scope of a check: the left side of `>>`, or the first argument of `every`. |
| Check | A statement about selections, for example `never(s)`. It gives the result pass, fail or incomplete. |
| Trace state | `live`, `settled` or `final`. Refer to "Trace state". |

## Rules

1. If the child process fails, praytel must use the exit code of the child.
2. A stage must not remove a control event.
3. The current flags must continue to operate. Praytel compiles the current
   flags into a plan. There is no second code path.
4. A structural query must not connect to the trigger `each`. The plan
   validation stops this configuration.
5. Only the driver goroutine writes to the projection.

## Purpose

Praytel is a set of outputs over one stream of events. Each output is a
stage list, a view, a trigger and a sink. A plan is a list of outputs. The
current modes are plans. New functions are new views or new stages.

## Data flow

1. The correlator sends events on one channel.
2. The driver goroutine reads each event one time.
3. The driver applies the event to the projection.
4. The driver tells each view about the event.
5. When the trigger of a view is true, the view makes output.
6. The view sends the output to its sink.

There is one projection for each run. The projection keeps the raw data.
Thus each output can use different stages on the same data.

## Stages

A stage operates in one of two places:

- On an output with the trigger `each`, for example the stream: The stage
  operates on each event before the view gets the event. This is necessary
  because the stream cannot remove a line that it wrote.
- On an output that reads the projection, for example the tree: The stage
  operates when the view reads the projection. The view sees only the spans,
  logs and attributes that the stage lets through.

Each stage gives the two forms. A stage must not remove a control event.

## Two types of query

A record-local query can make a decision when the event arrives. Thus a
record-local query can be a stage. The stream view can use it.

A structural query frequently cannot make a decision when the event arrives.
A child span can arrive before the link to its parent. A log can get its span
assignment late. Thus a structural query must use the projection. It must make
its decision at the trigger `tick`, `settled` or `final`.

This is the same reason that `internal/filter` is "not a drop-pipe" in
`design/ARCHITECTURE.md`. The design keeps this rule.

## Trace state

OTLP does not send a "trace complete" record. Thus praytel cannot know that a
trace is complete. Praytel uses three states:

- `live`: The trace has an open span, or an event arrived recently.
- `settled`: All known real spans ended. No event for the trace arrived for
  the time in `--settle-after`.
- `final`: The run drained. No more events arrive.

If an event arrives for a settled trace, the trace becomes live again. The
revision number of the trace increases by one. Consumers identify a result
by the trace ID and the revision number.

A trace can also have reasons that it is incomplete: eviction, shed data,
drain timeout or an unknown parent.

## The TUI

The TUI is a sink. It shows panes. Each pane is one output with the trigger
`tick`. The driver owns all data. The TUI owns only the screen.

On each tick:
1. The driver finds the panes that changed. A pane changed if one of these
   values changed since the last tick:
   - The version of the projection.
   - The viewport of the pane.
   - The plan of the pane.
2. The driver calls `Render` for each pane that changed. It gives the
   viewport. The view makes only the visible lines.
3. The driver sends the lines to the TUI.
4. The TUI draws the panes.

When the user types:
- A key for scroll or a change of window size changes the viewport. It does
  not change the plan.
- A change to the search query changes the plan of the pane. Praytel compiles
  the plan of the pane again one time. Praytel does not compile on each tick.
- The tab key selects a different pane.

The graph layout takes much time. Thus the graph view operates asynchronously.
The driver gives it a frozen copy of the component graph. The layout result
goes back to the driver. This is the current procedure of `relayoutGraph`.

The current `--tui` is a plan with two panes in tabs: the tree and the graph.

## Plan

A plan is a set of Go structs. You can write a plan as JSON. The
`--print-plan` flag shows the plan as JSON. Thus you can examine and test each
plan.

Two front-ends make a plan:

| Front-end | Example |
|---|---|
| Current flags | `praytel --snapshot tree -- go test ./...` |
| `-e` (snippet) | `praytel -e 'tree() \| final \| stdout' -- cmd` |
| `-e` (file) | `praytel -e checks.star -- cmd` |

## The `--execute` flag

The flag `--execute` (short form `-e`) accepts Starlark. You can use the flag
more than one time. All values go into one plan.

- If the value ends with `.star`, praytel reads the file at that path. If the
  file is not there, praytel shows an error.
- If not, praytel reads the value as Starlark code (a snippet).

A snippet can be one expression or a set of statements:
- If the snippet is an expression, praytel adds its value to the plan. The
  value must be an output or a list of outputs.
- If the snippet is a set of statements, the statements call `output(...)`.

The `|` operator connects the parts of an output. Praytel defines `|` on its
own Starlark types. Example:

```sh
praytel -e 'where("span{ span:status = error }") | tree() | settled | stderr' -- ./server
```

Starlark runs one time when praytel starts. In the TUI, Starlark runs again
when a file changes or when the user changes a query. The script calls
builtins. The builtins make plan values. Then Starlark stops. Starlark does
not see events.

Starlark gives these functions:
- Variables and functions.
- Loops that make many checks.
- `load()` to use shared rule files.

Scripts cannot do I/O. They cannot read the time. They cannot make random
values. Thus a script always gives the same plan.

Queries go into Starlark as strings or as builder calls. Both make the same
query tree:

```python
where("span{ resource.service.name = \"checkout\" && span:status = error }")
where(span(service = "checkout", status = "error"))
```

Use the builder form on the command line. It needs fewer quotes.

## Selections

A selection is a set of spans or logs. Write a selection in one of these
forms:

- `span(name=, service=, kind=, status=, attrs={...})`
- `log(body=, severity=, source=, attrs={...})`
- A query string, for example `"span{ span:status = error }"`.

The keyword arguments give the intrinsic fields. The `attrs` dictionary gives
the attributes. Its keys are the OTel attribute keys, for example
`"db.system"`. A value can be a literal or a matcher, for example
`match(".*DROP.*")` or `glob("http.*")`.

Use these operators to make a new selection:

| Operator | Result |
|---|---|
| `a & b` | The items that are in `a` and in `b`. |
| `a \| b` | The items that are in `a` or in `b`. |
| `~a` | The items that are not in `a`. |
| `a >> b` | The items in `b` that are below an item in `a`. |
| `b.child_of(a)` | The items in `b` whose parent is in `a`. |

Starlark does not let a script change the meaning of `==` or `<`. Thus the
conditions are keyword arguments and dictionaries, not comparisons.

The `|` operator also connects the parts of an output. Refer to
"The `--execute` flag". Praytel selects the meaning from the types:
selection `|` selection is a union, and stage `|` view is a pipe.

## Checks

A check is a statement about selections. A script writes a check as a call
at the top level. The call adds the check to the plan. This is the same
procedure as a rule call in a Bazel `BUILD` file.

| Statement | Meaning |
|---|---|
| `never(s)` | No item matches `s`. |
| `some(s)` | At least one item in the run matches `s`. |
| `every(a, has = s)` | Each item in `a` has at least one item of `s` below it. |
| `at_least(n, s)`, `at_most(n, s)`, `exactly(n, s)` | A count. Use it alone, or as the `has` value of `every`. |

Examples:

```python
checkout = span(service = "checkout")
drop     = span(attrs = {"db.statement": match(".*DROP.*")})

never(checkout >> drop)
every(span(kind = "server"), has = span(name = "auth"))
every(span(kind = "server"), has = at_most(3, span(name = "retry")))
```

Time of the decision:
- `never` can fail when a bad item arrives.
- `every`, `some` and the counts make a decision only when the trace is
  settled or final.

The anchor rule: Each anchor of a check must match at least one time in the
run. If an anchor never matches, the check fails with the message "check
never exercised". This rule stops a check that passes only because its anchor
never matched. For example, a span rename can cause this problem. The rule
has no parameter. If you expect that an item never occurs, write `never(s)`.

This rule comes from the health checks of Hypothesis (Python). Hypothesis
reports an error when the assumptions of a test never become true.

Each check gives one of three results:
- Pass.
- Fail.
- Incomplete. The trace has a reason that it is incomplete.
  An incomplete trace never gives a pass.

The report identifies each check by the canonical text of its statement.
You can also give the argument `name = "..."`.

Prior art for this design:
- Dwyer, Avrunin and Corbett, "Patterns in property specifications" (1999):
  the absence, existence and bounded existence patterns, each with a scope.
- CodeQL: `exists`, `forall` and `not` over trees.
- Datalog integrity constraints: a rule that must never be true.
- Tracetest: a selector plus a count assertion.
- TraceQL: structural operators on spans.
- Polars, Ibis and SQLAlchemy: Python expressions that make a plan, and
  native code that runs the plan.

## Table tests

If the inputs are known before the run, Starlark calculates the expected
output. The script makes one `every` check for each row:

```python
def f(x): return x * 2

for x in [0, 1, 2, 7, -3]:
    every(span(attrs = {"test.name": "TestDouble", "input": x}),
          has = span(attrs = {"output": f(x)}))
```

If the inputs are not known before the run, use a variable. A variable binds
to a value in the anchor. The check then uses that value below the anchor.
Variable binding is a later function.

```python
x = var("x")
every(span(attrs = {"test.name": "TestDouble", "input": x}),
      has = span(attrs = {"output": x * 2}))
```

## Items that we do not do now

We do not give Starlark functions that run on each trace. We add this
function only if a real case cannot use variables, counts and the other
query expressions.
