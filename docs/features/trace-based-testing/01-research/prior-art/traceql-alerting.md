# TraceQL / Honeycomb triggers / trace-to-test generators (checked 2026-11-08)
Category: adjacent (query-and-alert) plus a partial "trace-to-test" set (Helios, Kolo; Odigos not found)

## Does it implement the feature(s)?  partial
- Conditions over spans/attributes: yes (TraceQL). Honeycomb: attribute filters and derived columns, but these are aggregate queries, not tree-aware.
- Structural operators: TraceQL only.
- Streaming (live) violation alerting: no for all three. Evaluation is periodic over stored data.
- Test generation from a violation: not in TraceQL or Honeycomb. Helios and Kolo generate tests from traces, not from alerts.

## Mechanism
### Grafana Tempo TraceQL
- `{ }` selects spans. `|` pipes into aggregates (`count() avg() max() min() sum()`), `by()`, `select()`. [Tempo docs, retrieved 2026-11-08](https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/)
- Scopes: `span.`, `resource.`, `event.`, `link.`, `instrumentation.`; intrinsics use a colon (`span:duration`, `trace:duration`, `span:childCount`).
- Comparisons: `= != > >= < <= =~ !~`. Regexes are fully anchored. `= nil` is supported.
- `&&` inside one `{}` must match the same span. Across `{}` blocks the spans can differ, within one trace.
- Structural: `>>` descendant, `<<` ancestor, `>` child, `<` parent, `~` sibling. Union forms are `&>>` and similar. Experimental negations are `!>>` and similar. Results come from the right-hand side. Same source.
- Example "must not happen" property: `{ resource.service.name="frontend" } >> { status = error }`.
- Metrics mode: `rate`, `count_over_time` and `quantile_over_time` are appended to a query. This needs TraceQL metrics enabled on the Tempo backend. [Tempo metrics queries, retrieved 2026-11-08](https://grafana.com/docs/tempo/latest/metrics-from-traces/metrics-queries/)
- Alerting: TraceQL alert rules are a private preview. They need Grafana 12.1+ and the experimental `tempoAlerting` flag, and "shouldn't be used in production". They support range queries only, so a Reduce expression is needed before the threshold. [Grafana trace-based alerts, retrieved 2026-11-08](https://grafana.com/docs/grafana/latest/alerting/best-practices/trace-based-alerts/)
- The recommended path is PromQL over span metrics. Those metrics are generated before sampling if the alert must reflect all traffic. Same source.

### Honeycomb triggers
- A trigger attaches to a saved query graph. The user sets a duration (the data window) and a frequency (how often it runs). [Honeycomb triggers, retrieved 2026-11-08](https://docs.honeycomb.io/notify/alert/triggers/)
- The query must produce a single value, with a static threshold (`>`, `<`, `<=`, and `=>` as listed in the docs) or a dynamic baseline. A `HAVING` clause is allowed once. At most 1,000 groups are evaluated per run, and groups beyond that can be missed. Frequency is 1-1440 minutes, default 15. [Create a trigger, retrieved 2026-11-08](https://docs.honeycomb.io/notify/alert/triggers/create/)
- Surfacing: PagerDuty, Slack, Teams, webhook, email. The notification links to the triggering graph. Modes: limited (triggered then resolved), continuous, and per-group resolution. Same sources.
- Derived columns: UNVERIFIED. The fetched trigger pages do not cover them. Do not infer details from this document.

### Trace-to-test generation
- Helios: marketing and customer write-ups say it generates test code per trace, or from a span in one click. No official docs or independent review found, so everything is UNVERIFIED beyond this snippet. [web search summary, 2026-11-08](https://thenewstack.io/why-distributed-testing-is-the-best-way-to-test-microservices/) (the search tool did not return the specific Helios page; this link is the one it listed).
- Kolo (Python, adjacent): `kolo generate-test` produces pytest/unittest files from a recorded trace, and its docs mark the feature as alpha. [Kolo docs via search, 2026-11-08](https://docs.kolo.app/howto/generate-tests). A direct fetch of the page only returned a redirect stub, so the details are UNVERIFIED.
- Lightrun: the docs found cover dynamic traces and snapshots only, with no test generation. [Lightrun docs](https://docs.lightrun.com/jetbrains/create-a-dynamic-trace/)
- Odigos: eBPF auto-instrumentation only, and no test generation was found. [YC listing](https://www.ycombinator.com/companies/odigos-technologies-inc)

## User experience
- Tempo: write a query in Grafana Explore, then optionally turn it into an alert rule (preview only).
- Honeycomb: build a query, save it, attach a trigger, and wait for a notification.
- Neither involves picking attributes from a recorded run.

## Strengths
- TraceQL has a compact grammar that is tree-aware (structural operators, same-span versus cross-span `&&`). Source: Tempo docs above. Evidence: STRONG (official docs).
- Honeycomb has clear evaluation semantics (window, frequency, hysteresis via consecutive evaluations of 1-5, recovery modes). Evidence: STRONG (official docs).

## Limitations / known problems
- Post-hoc only. Tempo alerting has to query stored traces, so retention, sampling and tail-sampling skew all affect results. Grafana advises span metrics instead. Evidence: STRONG (official).
- TraceQL alerting is preview and unsupported. Evidence: STRONG (official).
- Honeycomb evaluation is periodic, with a minimum 1-minute frequency. It is not per-trace or immediate. Evidence: STRONG (official).
- Violations never produce a regression test. The user must write one by hand.

## Notable decisions & trade-offs
- Grafana pushes users to aggregate metrics for alerting and keeps trace queries for investigation. This trades per-trace precision for cheapness and stability.
- Honeycomb requires a single scalar result per trigger, which gives up per-trace logic.

## Evolution
- Tempo: the negated structural operators are experimental, and `span:childCount` needs vParquet5 or later. TraceQL alerting appeared in Grafana 12.1 as a preview. Other changelog history was not gathered.

## Gaps
- Honeycomb derived-column syntax and trace-level behaviour.
- Helios official docs and the generated test format.
- Any Odigos trace-to-test feature (none found).
- Whether Tempo's TraceQL alert rules link back to the violating trace.

## Sources
- https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/ - retrieved 2026-11-08 - official docs
- https://grafana.com/docs/tempo/latest/metrics-from-traces/metrics-queries/ - 2026-11-08 - official docs
- https://grafana.com/docs/grafana/latest/alerting/best-practices/trace-based-alerts/ - 2026-11-08 - official docs
- https://docs.honeycomb.io/notify/alert/triggers/ - 2026-11-08 - official docs
- https://docs.honeycomb.io/notify/alert/triggers/create/ - 2026-11-08 - official docs
- https://thenewstack.io/why-distributed-testing-is-the-best-way-to-test-microservices/ - 2026-11-08 - blog, ANECDOTE
- https://docs.kolo.app/howto/generate-tests - 2026-11-08 - official docs, content not verified
- https://docs.lightrun.com/jetbrains/create-a-dynamic-trace/ - 2026-11-08 - official docs
- https://www.ycombinator.com/companies/odigos-technologies-inc - 2026-11-08 - listing
