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
for a live trace/span tree. In the TUI, type to fuzzy-search traces, press Tab to
search log bodies instead, use ↑/↓ to scroll, Esc to clear, and Ctrl-C to quit.
