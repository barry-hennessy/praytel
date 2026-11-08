# Tracetest (docs retrieved 2026-11-08; last GitHub release seen v1.7.1, 2024-10-10)
Category: direct

## Does it implement the feature(s)?  partial
- Declarative trace assertions against spans: yes.
- Run a command/trigger then assert on the trace: yes (triggers, not arbitrary shell commands).
- Interactive record-then-pick-attributes flow: partial. A web UI builds a selector from a span ("span signature"). No terminal or CLI equivalent was found.
- Property-based or "must NOT hold" live monitoring: no evidence found. Tests are post-hoc per run. UNVERIFIED that nothing similar exists.
- Embedded scripting language for logic: no. It has a small expression DSL and YAML only, which conflicts with the brief's hard constraint.

## Mechanism
- A test is a trigger plus specs. A spec pairs a selector with assertions. Selectors choose spans; each assertion is checked on every matched span. [concepts](https://docs.tracetest.io/concepts/what-is-trace-based-testing/) (2026-11-08)
- Selector syntax is `span[attr op "value"]`, for example `span[tracetest.span.type="http"]`. Assertion syntax is `attr:<name> <op> <value>`. [specs](https://docs.tracetest.io/cli/creating-test-specifications) (2026-11-08)
- Operators are `= != < <= > >= contains not-contains`. Arithmetic is `+ - * /`. Durations take the units ns, us, ms, s, m, h. An assertion can reference another attribute on the same span. Same source.
- The only function documented is `json_path`, used to query span events. Events are injected into attributes as a JSON array and the docs call this an MVP. Same source.
- Test file format is YAML: `type: Test`, `spec:` with name, trigger, specs, optional outputs and pollingProfile. [creating tests](https://docs.tracetest.io/cli/creating-tests) (2026-11-08)
- Trigger types are http, grpc, kafka, traceid, cypress, playwright, k6 and artillery. Same source.
- `outputs` entries have name, selector and value, so one test can pass values to later tests. This is the "wire up inputs and outputs" idea. Same source.
- Trace acquisition has two paths. Core either receives OTLP itself or reads the trace from a supported backend (Jaeger, Tempo, X-Ray, etc.) after the run. The run shows an "Awaiting Trace" state. [traceid trigger](https://docs.tracetest.io/web-ui/creating-tests-traceid) (2026-11-08)
- A TraceID trigger runs a test against an already-captured trace. It needs a direct-connection backend. Backends fed through a separate OTel Collector pipeline are not supported. Same source.
- Black-box mode has a "Skip awaiting trace" option. [black-box](https://docs.tracetest.io/web-ui/black-box-testing) (found via search; page not fetched, UNVERIFIED in detail)

## User experience
- The CLI runs a file with `tracetest run test --file <path>`, and suites with `tracetest run testsuite --file ... --wait-for-result`. This comes from search-result summaries and a third-party guide. [oneuptime](https://oneuptime.com/blog/post/2026-02-06-assertion-rules-span-attributes-tracetest/view) (2026, ANECDOTE). The official creating-tests page does not document `run` or `export`.
- Authoring is mostly in the web UI: select a span, and Tracetest proposes a selector from its attributes. If several spans look alike, the user adds attributes or an ancestor to narrow it. [concepts](https://docs.tracetest.io/concepts/what-is-trace-based-testing/) (2026-11-08)
- Trace Mode creates tests from trace IDs. Traces are deleted after 4 days. [search summary of docs.tracetest.io/web-ui/trace-mode](https://docs.tracetest.io/web-ui/trace-mode) (UNVERIFIED, page not fetched)

## Strengths
- The span-signature to selector flow is the closest existing analogue to the brief's "highlight attributes and finish". Source: concepts page above.
- The selector plus checks model is simple and widely copied.
- Outputs let tests be chained.
- Triggers integrate with k6, Playwright and Cypress.

## Limitations / known problems
- No operator precedence: `1 + 2 * 3` evaluates to 9. [specs](https://docs.tracetest.io/cli/creating-test-specifications) (2026-11-08, maintainer docs, STRONG)
- Span events have only `json_path`, and the docs call it an MVP. Same source.
- TraceID triggers do not work with collector-pipeline backends or with black-box testing. [traceid](https://docs.tracetest.io/web-ui/creating-tests-traceid)
- The README says Core is "not suitable for production" and is MIT licensed. [GitHub](https://github.com/kubeshop/tracetest) (2026-11-08)
- Maintenance status: a search summary says the kubeshop org page lists Tracetest under "Archived Projects", with Testkube as the active project. [kubeshop org](https://github.com/kubeshop) (UNVERIFIED, not read directly). The repo shows no archive banner, and the last release found is v1.7.1 (2024-10-10) [releases](https://github.com/kubeshop/tracetest/releases). A claim that Cloud/Enterprise is being discontinued came from stackpick.net only (ANECDOTE, UNVERIFIED).
- Logic is limited to what the DSL offers. There are no loops, conditionals or user-defined predicates, so complex logic is not expressible. This is inferred from the documented grammar, not an explicit docs statement.

## Notable decisions & trade-offs
- Declarative YAML and a small DSL give portability and UI round-tripping, at the cost of expressiveness (the precedence bug and the events MVP are symptoms).
- Tracetest needs a server (UI, database, trace store). That is the opposite of praytel's in-memory, daemonless model. Inference from the architecture described in the README and docs.
- Checks are applied only after the trace has been fetched. The "awaiting trace" polling is its run-time model. No live-violation alerts were found.

## Evolution
- Older docs use a top-level `specs:` list. Newer docs use the `type: Test` / `spec:` wrapper. [older snapshot](https://github.com/kubeshop/tracetest/blob/4f9435695ea4ce069ee5995743d2986acd1ca5e3/docs/docs/cli/creating-test-specifications.md)
- The v1.7.1 changelog adds a deprecation notice for CLI "transactions", which were later replaced by test suites. [releases](https://github.com/kubeshop/tracetest/releases) (2024-10-10). The transaction-to-suite link is UNVERIFIED.

## Gaps
- The official CLI reference (`run`, `export`, polling profile settings) was not read.
- Whether the project is archived was not confirmed from a primary source.
- No user opinion sources (issues, forums) were gathered.

## Sources
- https://docs.tracetest.io/concepts/what-is-trace-based-testing/ - retrieved 2026-11-08 - official docs
- https://docs.tracetest.io/cli/creating-test-specifications - 2026-11-08 - official docs
- https://docs.tracetest.io/cli/creating-tests - 2026-11-08 - official docs
- https://docs.tracetest.io/web-ui/creating-tests-traceid - 2026-11-08 - official docs
- https://docs.tracetest.io/web-ui/black-box-testing - search summary only
- https://docs.tracetest.io/web-ui/trace-mode - search summary only
- https://github.com/kubeshop/tracetest - 2026-11-08 - repo
- https://github.com/kubeshop/tracetest/releases - 2026-11-08 - changelog (first 100k chars read)
- https://github.com/kubeshop - search summary only
- https://oneuptime.com/blog/post/2026-02-06-assertion-rules-span-attributes-tracetest/view - 2026 - third-party blog
