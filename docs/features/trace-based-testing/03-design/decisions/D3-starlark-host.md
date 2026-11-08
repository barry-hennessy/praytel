# D3: How is Starlark hosted to load, register and execute trace test files?
Status: split (children D4, D5, D6 all decided)     Depends on: none     Parent: none
Children: D4 (package boundary and dependency containment), D5 (file evaluation model: load time, registration, load(), env()), D6 (check execution and failure collection; holds the R1 spike)
Vision refs: §4 Assertion model, §6 Starlark row, §7 v1 item 2; R1, R2, R3, R9; A3, A6

## Why split
Options would differ along three independent axes, and one comparison table would mix them:
1. **Containment**: where `go.starlark.net` is imported, how it is pinned, which dialect options are on, how the builtin surface is frozen (R3, R9). Independent of runtime semantics.
2. **Evaluation model**: what runs when a file is parsed (top-level, `test(...)` registration, `load()`, `env()`) versus when a case's `check(run)` runs. Decides S3's "parse every file before any child starts".
3. **Failure mechanics**: how `expect.*` records and continues without try/except, what `run.one` returns on 0/>1 matches, how absent attributes, runtime errors, step limit and cancellation surface, and which call frame's file:line:col is reported (R1, M6).
Axis 1 can be decided first and alone; axes 2 and 3 can then be decided in parallel.

## Context
- No Starlark dependency today (`go.mod:1-10`).
- Starlark facts (synthesis §1, §4): deterministic, host controls every builtin, step cap and goroutine-safe `Cancel`, file:line:col on errors, no exceptions, not a sandbox, no tagged releases.

## Goals
- A host layer whose behaviour and builtin surface are fixed by tests and documented in `docs/reference/praytest.5.md`.

## Non-goals
- Sandboxing (§7 non-goals, R2).
- Any second test-definition language (§7 non-goals).

## Decision criteria (weighted, sum 100)
Inherited by the children, which refine them.
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| All failures reported clearly with positions | 25 | R1, A3, M6 |
| Containment of the upstream dependency | 20 | R3 |
| Builtin surface small and frozen | 20 | R9 |
| Authoring expressiveness (helpers, loops, cases with other inputs) | 20 | §3 "code you own: add cases with other inputs, loops and helpers" |
| Usage errors found before any child starts | 15 | S3 "All-or-nothing usage errors" |

## Options
Not applicable (split).
## Decision
Split into D4, D5, D6.
