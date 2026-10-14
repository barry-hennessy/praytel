# HTTP protobuf traces and OTLP logs

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

OTLP HTTP/protobuf traces (`/v1/traces`) and OTLP logs (`/v1/logs`, gRPC and HTTP) map to the same domain types. OTLP logs are `LogLine` with `source=otlp`; component from resource `service.name` when present. If the child also writes stdout, **both** copies show. No dedupe.

## Acceptance criteria

- [x] HTTP-exporting helper child spans join like gRPC
- [x] OTLP log record becomes `LogReceived` with source otlp; `service.name` usable as component
- [x] Stdout + OTLP for the same body both appear
- [x] Metrics still absent

## Blocked by

- [Child gRPC traces](05-child-grpc-traces.md)

## Comments

Covered: `TestHTTPExportingChildJoinsJSONLog`, `TestStdoutAndOTLPLogBothAppear`; receiver HTTP traces + OTLP logs + no-metrics tests.
