# Praytel’s own observability

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** _(none)_

## Question

Bones wants `LoggingTracer` in application code, not a raw `slog.Logger`. Praytel **captures** the child’s logs/traces. How does praytel log **itself** (bind, drain, shed, exec) without contaminating the unified child view?

Decisions needed:
- Always stderr via a named `LoggingTracer` (`praytel` component)?
- Recursion: if praytel’s own OTel exporter pointed at its receiver, we’d ingest ourselves — forbid that?
- How much tracing of the correlator is in scope for v1 vs silent-enough stderr?

Graduated from map fog; error surface already says user-facing fatals/warnings are stderr.

## Resolution

**`LoggingTracer` to praytel stderr**, named at composition (`praytel`, `receiver`, `correlator`, …). **Noop tracer** in v1. **No OTLP exporter** on the wrapper; never ingest ourselves.

Child stdout/stderr are captured pipes only. Diagnostics and user-facing warnings share this logger (stderr), not the correlator event stream. `--verbose` for slog level. Real self-tracing is later, and must not target the in-process receiver.

## Addendum: self-tracing

The "later" arrived with `--self-trace`. It is a development flag, off by
default, so in regular use praytel's spans still go nowhere. With it, praytel
exports its own spans to the OTLP endpoint it inherited from an outer praytel
(`praytel -- praytel --self-trace -- cmd`), never to its own receiver, and
fails to start when there is no such endpoint. Only `cmd/praytel/selftrace.go`
imports the OTel SDK; `TestOnlySelfTraceImportsTheOTelSDK` replaces the old
no-SDK test, and `TestPraytelsSpansStayDarkWithoutSelfTrace` checks the
default. Error status on spans waits on
[bones#2](https://github.com/barry-hennessy/bones/issues/2).
