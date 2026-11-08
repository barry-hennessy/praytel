# Malabi (Aspecto), checked 2026-11-08
Category: adjacent (in-test-code span assertion; JS only)

## Does it implement the feature(s)?  partial
- Test-as-code assertions on spans: yes (JS/TS, Mocha).
- Record-then-generate tests, TUI span picking, property/negative checks: no (UNVERIFIED for generation; none in README).
- Status: repository archived by owner 2024-10-31, read-only (https://github.com/aspecto-io/malabi, retrieved 2026-11-08). Treat as abandoned. A search result listed last push May 2024 (secondary, ANECDOTE).

## Mechanism
- The service under test calls `instrument(config)` and `serveMalabiFromHttpApp(18393, config)` before other imports. This adds a custom exporter to the OTel SDK that keeps spans in memory, and an HTTP endpoint inside the service that exposes them. [README raw](https://raw.githubusercontent.com/aspecto-io/malabi/master/README.md), 2026-11-08; in-memory exporter wording from [CNCF blog](https://www.cncf.io/blog/2021/08/11/trace-based-testing-with-opentelemetry-meet-open-source-malabi/) 2021-08-11 (via search snippet, UNVERIFIED on full text).
- Test runner is a separate process with its own `instrument({serviceName:'tests-runner'})`. It reaches the service via `MALABI_ENDPOINT_PORT_OR_URL`. Spans are pulled over HTTP: tutorial names `fetchRemoteTelemetry` / `clearRemoteTelemetry` ([DEV post](https://dev.to/aspecto/how-to-use-opentelemetry-to-improve-your-integration-tests-5h37), via search snippet, ANECDOTE). Wire format UNVERIFIED (README does not describe it).
- Scoping: `malabi(async () => {...})` runs the callback as a new trace and returns a telemetry repository; only spans from code inside the callback are assertable. [README](https://raw.githubusercontent.com/aspecto-io/malabi/master/README.md), 2026-11-08.
- Storage backends via `MALABI_STORAGE_BACKEND`: `InMemory` or `Jaeger` (user runs the agent; `OTEL_EXPORTER_JAEGER_*`, `MALABI_JAEGER_QUERY_*` vars). Same source.

## User experience
```js
const telemetryRepo = await malabi(async () => {
  await axios(`http://localhost:${PORT}/todo`);
});
const call = telemetryRepo.spans.outgoing().first;
expect(call.httpFullUrl).equals('https://jsonplaceholder.typicode.com/todos/1');
expect(call.statusCode).equals(200);
```
- Span selection is by semantic type helpers (`spans.outgoing()`, `spans.sequelize()`), then `.first` / `.length`. Typed convenience properties: `httpFullUrl`, `statusCode`, `dbOperation`, `dbResponse`. Assertions use the host framework (chai/jest expect); Malabi has no own matcher DSL. Source: README, 2026-11-08.
- Run: `MALABI_ENDPOINT_PORT_OR_URL=... ts-mocha --paths "./test/*.ts" --require ./test/tracing.ts`. README says Jest works poorly with OTel; Mocha recommended.

## Strengths
- Tests are plain code with ordinary assertions; no config DSL (README).
- Type-keyed selectors with friendly property names hide raw OTel attribute keys (README).
- Trace scoping per test via callback avoids cross-test span bleed (README).

## Limitations / known problems
- Archived; JS-only; requires instrumenting the service and exposing an extra HTTP port (README, repo banner).
- Selection is limited to built-in span types; no documented arbitrary attribute/tree query (README; absence, MODERATE).
- Test framework constraint (Mocha over Jest) (README).
- No recording or test generation.

## Notable decisions & trade-offs
- Chose a test-runner-pulls-from-service model (remote endpoint) over in-process, because runner and service are separate processes. Cost: extra endpoint, extra process setup. Rationale inferred, not stated (UNVERIFIED).

## Evolution
- Announced 2021-08-11 (CNCF blog above); archived 2024-10-31. Releases section empty on GitHub page (retrieved 2026-11-08).

## Closest in-process alternative: Go OTel `tracetest` (platform)
- `NewInMemoryExporter()` with `GetSpans()` returning `SpanStubs` and `Reset()`; `NewSpanRecorder()` registered via `trace.WithSpanProcessor(sr)` with `Started()` / `Ended()`. SpanStub fields: Name, SpanContext, Parent, SpanKind, Start/EndTime, Attributes, Events, Links, Status. Package gives capture only; assertion pattern is left to the user. [pkg.go.dev](https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace/tracetest), retrieved 2026-11-08.
- Generic write-up of in-memory exporters for unit tests: [OneUptime](https://oneuptime.com/blog/post/2026-02-06-in-memory-span-exporters-unit-tests/markdown) (listed in search, content not read, ANECDOTE).

## Gaps
- Malabi source not read (endpoint protocol, selector implementation, attribute-level query).
- No user complaints/issues reviewed; no opinion evidence gathered.
- tracetest-style Go assertion helpers beyond the OTel package not researched here.

## Sources
- https://github.com/aspecto-io/malabi - 2026-11-08 - official repo (archived)
- https://raw.githubusercontent.com/aspecto-io/malabi/master/README.md - 2026-11-08 - official docs
- https://www.cncf.io/blog/2021/08/11/trace-based-testing-with-opentelemetry-meet-open-source-malabi/ - 2021-08-11 - maintainer post (snippet only)
- https://dev.to/aspecto/how-to-use-opentelemetry-to-improve-your-integration-tests-5h37 - undated - maintainer tutorial (snippet only)
- https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace/tracetest - 2026-11-08 - official API docs
- https://oneuptime.com/blog/post/2026-02-06-in-memory-span-exporters-unit-tests/markdown - 2026-02-06 - blog (unread)
