# Pray-tel
We wire up services with open tracing, telemetry. We emit logs. Both to get visibility
into how our systems are running. What's going right, what's going wrong. How are things wired.

But we silo the data. We have to look in two places to get a clear overview of both.

Praytel unifies logs and tracing on the command line so your development workflow
can leverage both.

 - run commands and focus log output on specific components
 - let agents wire code, and verify that they haven't just stubbed the hard bits!
 - let agents leverage logs and tracing to focus them
 - leverage logs and tracing yourself; focus you!
 - get value out of your logs in your development workflow without spinning up a docker container

Run `praytel -- command` for the append-only stream, or `praytel --tui -- command`
for a live trace/span tree. In the TUI, type to fuzzy-search traces (matches span
names and attributes, including trace attributes), press Tab to search log bodies
instead, use ↑/↓ to scroll, Esc to clear, and Ctrl-C to quit.

Trace search is loose by design: it's a subsequence match (letters found in order,
not a literal substring), and matching a single span anywhere in a trace surfaces
that trace's whole tree, not just the span that matched. Expect broader results
than a `grep`-style search would give you.
