# OTLP log vs trace routing

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** _(none)_

## Question

The receiver speaks gRPC and HTTP for OTLP. How are **trace** and **log** signals split and handed to the correlator?

Decisions needed:
- Separate protobuf services / HTTP paths vs one ingest that switches on payload
- Typed correlator channels: traces vs OTLP logs vs stdout (already decided) — what does an OTLP log record become before `LogReceived`?
- Resource attributes (service.name) vs stdout component field — same `domain` fields?
- If an app emits both stdout and OTLP logs for the same line, do we dedupe or show both?

Graduated from map fog after the pipeline and error surface were locked.

## Resolution

**Two OTLP ingest surfaces** (gRPC + HTTP): traces (`Export` / `/v1/traces`) and logs (`Export` / `/v1/logs`). No payload-sniffing unifier.

**One `domain.LogLine`** for stdout, stderr, and OTLP logs. Receiver/capture adaptors map at the edge. Field `source`: `stdout` | `stderr` | `otlp`. Component from resource `service.name` when present. Correlator OTLP-log channel carries `LogLine`; same pending/orphan/`LogReceived`→`LogAssigned` path as stdout.

**No dedupe** if both stdout and OTLP carry the same message. `source` is enough for a later filter. OTLP **metrics** not ingested.
