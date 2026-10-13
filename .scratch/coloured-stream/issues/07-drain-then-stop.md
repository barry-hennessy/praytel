# Drain then stop

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

After the child exits, wait for OTLP with an **idle timeout** and a **hard cap** (both flags). Then `DrainComplete`, close the event channel, stream `Run` returns. Data after drain: warn on stderr and drop. Stream renderer does not stay open.

## Acceptance criteria

- [x] Wrapper: late flush within idle window still joins; process returns
- [x] Hard cap elapsed → return even if data is still arriving
- [x] Post-drain export: warning, not hung process
- [x] Child exit code unchanged by drain

## Blocked by

- [Child gRPC traces](05-child-grpc-traces.md)

## Comments

Covered: `TestLateFlushWithinIdleJoins`, `TestHardCapReturnsWhileOTLPArrives`, `TestPostDrainExportWarnsAndReturns`, `TestDrainDoesNotChangeChildExit`.
