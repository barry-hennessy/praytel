# Brief: trace-based testing in praytel

## Request (verbatim from user)
I would like to add 'trace based testing' similar to what https://docs.tracetest.io/concepts/what-is-trace-based-testing/ offers, but via the command line in praytel.

Rough idea:
- Start recording (maybe from the TUI)
  - Praytel shows you a terminal
  - You run a command (maybe via the terminal)
  - You say you're done
  - You're shown the full TUI
  - You navigate/search for a span
  - You highlight the attributes you'd like to test for
  - You finish
  - Praytel writes out a test file: the command run, the output(s) required
- Wire up inputs and outputs

Ideally also property-based tests: state properties that should NOT hold for a given run. Perhaps praytel runs while the user runs their app; if it "trips" an invalid property the user is alerted immediately and a test case is generated. Creative use cases welcome.

**Hard constraint:** NO yaml test-case configuration. If an embeddable language is needed (selecting attrs, comparisons, complex logic) use https://pkg.go.dev/go.starlark.net/starlark.

## Constraints discovered in the repo
- Praytel is a transparent CLI wrapper `praytel [flags] -- cmd`; in-memory only, no daemon, no leftover files (design/ARCHITECTURE.md). Child's exit code is praytel's.
- Go; Bubble Tea TUI; custom OTLP receiver (gRPC 4317 / HTTP 4318, traces+logs, no metrics); correlator goroutine emits value-copy domain events; renderers consult internal/filter; `internal/attrspec` exists.
- Existing internal/testtrace + golden trace trees for praytel's own tests (docs/guides/golden-traces.md): relevant prior art inside the repo.
- design/IDEAS.md sketches a TraceQL-shaped query language (agents: ignore unless instructed; it is 0% commitment). Reference only; the user mandates Starlark for logic.
- Docs convention: CommonMark with frontmatter under docs/; every exported function starts a span (CONTRIBUTING.md).
- Test files written by praytel must be code, not YAML (Starlark script is the natural candidate).
