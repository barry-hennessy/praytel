# Pray-tel
We wire up services with tracing. We emit logs.
All in service of observability.

What's going right, what's going wrong. How are things wired.

But we silo the data. We set up exporters, stores, and GUIs. But only in our k8s environments.

Praytel unifies logs and tracing on the command line so your development workflow
can leverage both.

 - leverage logs and tracing as you develop; your tools _helping_ you focus!
 - build useful tracing for yourself during development and profit when a fire breaks out in production.
 - all without spinning up another docker container on your machine

Full documentation lives in [`docs/`](docs/index.md): three guides that build on
each other, from [viewing traces](docs/guides/traces.md) to
[seeing how a system fits together](docs/guides/overview.md), each with commands
you can run against a small example app, and a
[reference page per command](docs/index.md#reference).

# Tl; dr:

Run:
 - `praytel -- command` for the append-only stream
 - `praytel --tui -- command` for a live trace/span tree.

In the TUI, type to fuzzy-search traces (matches span names and attributes,
including trace attributes), press Tab to search log bodies
instead, use ↑/↓ or the mouse wheel to scroll (←/→ or Shift+wheel to pan),
Esc to clear, and Ctrl-C to quit.
