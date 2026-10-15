# Pray-tel
We wire up services with open tracing, telemetry. We emit logs. Both to get visibility
into how our systems are running. What's going right, what's going wrong. How are things wired.

But we silo the data. We have to look in two places to get a clear overview of both.

Praytel unifies logs and tracing on the command line so your development workflow
can leverage both.

 - leverage logs and tracing yourself; your tools _helping_ you focus!
 - get value out of your observability in your development workflow - without spinning up a docker container

Full documentation lives in [`docs/`](docs/index.md): three guides that build on
each other, from [viewing traces](docs/guides/traces.md) to
[seeing how a system fits together](docs/guides/overview.md), and a
[reference page per command](docs/index.md#reference).

Run `praytel -- command` for the append-only stream, or `praytel --tui -- command`
for a live trace/span tree. In the TUI, type to fuzzy-search traces (matches span
names and attributes, including trace attributes), press Tab to search log bodies
instead, use ↑/↓ to scroll, Esc to clear, and Ctrl-C to quit.

Trace search is loose by design: it's a subsequence match (letters found in order,
not a literal substring), and matching a single span anywhere in a trace surfaces
that trace's whole tree, not just the span that matched. Expect broader results
than a `grep`-style search would give you. The matched span's label and attributes
are highlighted so you can see which one actually caused the trace to show up.
