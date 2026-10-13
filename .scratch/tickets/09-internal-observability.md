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
