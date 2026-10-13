# Signal forward

**Status:** done  
**Type:** AFK

## Parent

[PRD: Praytel v1 (coloured stream)](../PRD.md)

## What to build

SIGINT/SIGTERM to praytel are forwarded to the child so the wrapper stays invisible. After the child reacts, praytel finishes like a normal prepend (drain becomes real in the drain slice; until then, wait for child exit).

## Acceptance criteria

- [x] Wrapper: signal to praytel reaches a helper child (as far as the harness allows)
- [x] Child exit code after signal handling is still forwarded
- [x] Praytel does not swallow SIGINT without forwarding

## Blocked by

- [Transparent prepend](01-transparent-prepend.md)

## Comments

Covered: `TestSignalToPraytelReachesChild` (SIGINT and SIGTERM).
