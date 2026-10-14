---
title: Get an overview of a system
summary: How to use the graph view to see which components a run touches and how they call each other.
weight: 40
---

# Get an overview of a system

The span tree shows one request at a time. The graph view shows the shape of
the system: which components were hit and which ones call which. It is drawn
from what actually ran, not from a diagram someone wrote.

That answers three kinds of question:

- **What components does this hit?** Run one command or one test and see
  everything it touched.
- **How did your agent actually structure your system?** Run the code an agent
  wrote and compare the graph to what you asked for.
- **How do the tools you use work?** Wrap an instrumented tool and watch what
  it talks to.

## Run it

![Recording: switching from the span tree to the graph of components and the calls between them, then narrowing it with a search](../assets/overview.gif)

```sh
praytel --tui -- ./server
```

Press Ctrl-G to switch from the tree to the graph. Press it again to go back.

## Reading the graph

Each box is a container: a component, or a section inside one. An arrow from A
to B means a span in A was the parent of a span in B. Calls that stay inside
one container are counted on that box instead of drawn as arrows, so the graph
stays the size of your architecture rather than the size of your call stack.

## Where the boxes come from

| Level | Taken from |
|---|---|
| Component | The `service.name` resource attribute. |
| Section | The dotted segments of the span name, except the last. |
| Function | The last segment of the span name. |

A span named `Orders.Repo.Save` in service `checkout` belongs to the container
`checkout` → `Orders` → `Repo`, as function `Save`.

So the graph is only as good as your naming. Set `service.name` per service and
name spans `Section.function`, and the graph reads like your design. Name every
span `handler` and you get one box.

## Try it on Dagger

Run this in a directory containing a Dagger module, then press Ctrl-G:

```sh
praytel --tui -- dagger functions
```

The graph has two components. `dagger-cli` starts and connects to the engine,
and an arrow from `dagger-cli.loading type definitions` into
`unknown_service:dagger-engine.POST /query` marks where the CLI hands the work
over. Pan right to see the engine load the module. The engine sets no
`service.name`, so its component is named `unknown_service:dagger-engine`; the
graph shows the gap in its naming as plainly as its structure.

## Scope the graph

The graph always shows the same spans the tree would. Anything that narrows the
tree narrows the graph:

- type to search, and the graph shrinks to the matching traces;
- start praytel with `--focus` or `--exclude` to drop noise for the whole run.

Scroll with the arrow keys, including left and right for wide graphs.

## Next

[Focus on performance](performance.md).
