# Error surface — what praytel shows vs swallows

**Label:** `wayfinder:grilling`  
**Status:** closed  
**Blocked by:** [Renderer interface shape](04-renderer-interface.md)

## Question

What errors does praytel surface to the user, and how?

Praytel is a transparent wrapper — the child process's exit code must pass through. But praytel itself can encounter errors:

- Child process fails to start (bad command, permission denied)
- OTLP receiver can't bind to ports (already in use)
- Log parsing failures (malformed JSON, unexpected format)
- Correlator receives spans for unknown traces (late arrivals after drain)
- Renderer failures (terminal doesn't support colours, TUI can't enter alt screen)
- Memory pressure events (`ForestEvicted` / `ForestShed`) — already decided: visible, non-fatal by default ([Memory and disk pressure management](06-memory-pressure.md))

Constraint from [Renderer interface shape](04-renderer-interface.md): presentation of errors is **inside** `Run` (stream vs TUI status). Child exit vs praytel `ctx` are already distinct.

Decisions needed:
1. **Exit code** — always forward child's exit code? What if praytel itself fails?
2. **Error channel** — stderr? A dedicated log line in the output? A status bar in TUI mode?
3. **Severity** — which errors are fatal (can't bind port) vs warnings (parse failure on one line) vs silent (unknown span dropped)?
4. **Bones ErrMapper** — use Bones' `ErrMapper` pattern for layer boundary errors?

Blocked by renderer interface because how errors are displayed depends on the output mode.

## Resolution

**Exit code:** if the child started, return **its** code (TUI quit, warnings, shedding included). If the child **never started** (exec fail, OTLP bind fail), praytel exits non-zero. Warnings never change the code. No `--strict` in v1.

**Channel:** fatal and warnings on **stderr** (not the log stdout stream). TUI also mirrors warnings in status. `ErrMapper` only at `main` for fatal internal → message.

| Case | Treatment |
|---|---|
| Exec fail, bind fail, `--tui` cannot start | Fatal |
| Shed/evict, late OTLP after drain, malformed JSON | Warning (rate-limit parse spam) |
| Non-JSON / unstructured lines | Not an error (orphan path) |
| No colour | Degrade, silent |
| Impossible correlator state after assign | Warning |
