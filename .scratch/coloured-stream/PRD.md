# PRD: Praytel v1 (coloured stream)

**Status:** done

Source spec: [`.scratch/SPEC.md`](../SPEC.md). Architecture: [`ARCHITECTURE.md`](../../ARCHITECTURE.md).

Primary test seam: correlator inbound channels → outbound domain events.  
Thin second seam: wrapper process (exec, OTLP env/bind, signals, exit code, stderr, append-only stdout).

See [`SPEC.md`](../SPEC.md) for problem, solution, user stories, implementation and testing decisions, and out of scope.

## Issues

| # | Title | Status |
|---|---|---|
| [01](issues/01-transparent-prepend.md) | Transparent prepend | done |
| [02](issues/02-otlp-listen-and-point-child.md) | OTLP listen and point the child | done |
| [03](issues/03-captured-lines-append-only-stream.md) | Captured lines as an append-only stream | done |
| [04](issues/04-join-ids-pending-late-parents.md) | Join on IDs, pending, late parents | done |
| [05](issues/05-child-grpc-traces.md) | Child gRPC traces | done |
| [06](issues/06-http-traces-and-otlp-logs.md) | HTTP protobuf traces and OTLP logs | done |
| [07](issues/07-drain-then-stop.md) | Drain then stop | done |
| [08](issues/08-live-focus-exclude.md) | Live `--focus` / `--exclude` | done |
| [09](issues/09-forest-caps.md) | Forest caps | done |
| [10](issues/10-signal-forward.md) | Signal forward | done |
