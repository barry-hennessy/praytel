# OTLP listen and point the child

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

Before exec, bind OTLP defaults (gRPC 4317, HTTP 4318) with flag overrides. Set the child’s OTEL exporter env to that listener. Bind failure: do not start the child, exit non-zero, message on stderr. Do not ingest metrics. Do not export praytel’s own telemetry into this receiver. Payloads may still be discarded until later slices.

## Acceptance criteria

- [x] Wrapper test: child sees OTEL endpoint env pointing at the bound ports
- [x] Occupied default port + no override → child is not exec’d; non-zero exit
- [x] `--grpc-port` / `--http-port` bind and appear in child env
- [x] No metrics surface; noop tracer on the wrapper

## Blocked by

- [Transparent prepend](01-transparent-prepend.md)

## Comments

Covered on `main`: `TestChildSeesOTELExporterEnv`, `TestFlagPortsBindAndAppearInChildEnv`, `TestOccupiedDefaultPortDoesNotExecChild`, `TestWrapperHasNoOTelSDKImport`, receiver `TestNoMetrics*`.
