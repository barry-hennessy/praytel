# Comparables

Feature set: trace-based testing from the CLI; record-then-assert workflow (run a command, pick spans/attributes, emit a test file); property/invariant checks on live runs with immediate alerting and test-case generation; code (not YAML) test definitions with an embedded language.

| # | Tool | Why it is comparable | Output slug |
|---|------|----------------------|-------------|
| 1 | Tracetest | The named reference; trace-based assertions on spans via selectors, YAML/UI test definitions | tracetest |
| 2 | Malabi (Aspecto) | Trace-based testing in-process for JS; assertions on spans in test code (code-not-YAML) | malabi |
| 3 | Hypothesis (property-based testing) | Gold standard for property tests, shrinking, failure -> reproducible example/database | hypothesis |
| 4 | Starlark in Bazel / Go embeddings (starlark-go) | The mandated embeddable language: sandboxing, determinism, how tools expose predicates/builtins | starlark-go |
| 5 | Playwright codegen / `go test` golden + Cypress Studio | Record-and-generate workflow: record interactions, pick assertions, write test file | playwright-codegen |
| 6 | Honeycomb Triggers/SLOs + Tempo TraceQL alerts / Grafana k6 tracing | Querying traces against conditions and alerting; TraceQL as assertion language | traceql-alerting |
| 7 | Jaeger / Grafana Tempo "trace-to-test" and Odigos/Helios (Lightrun) | Test generation from observed production traces | helios-trace-to-test |
| 8 | Hurl / Venom (CLI test runners) | CLI-based, file-defined tests with captures and asserts; file format ergonomics | hurl |

Wave 1 (cap 6 researchers): rows 1-6. Rows 7-8 held in reserve for gap-filling retries.
