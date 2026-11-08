## Option C: TraceQL/Tracetest semantics, mapped onto the vision builtins   (stance: most conventional)
Summary: A selector works like one TraceQL `{ }` block: every field is an exact, typed match on the same span, ANDed together. Assertions work like Tracetest checks: they apply to every span the selector matched, and a selector that matches nothing fails. Cross-span (same-trace) rules go through `span.parent` and `span.trace` inside predicates, which plays the part of TraceQL's structural operators without adding new builtins.

How it works: everything lives in `internal/praytest` (new `select.go`, `value.go` and `describe.go`) over D1's `[]CheckSpan`, and every name is entered in the D4 registration table.
- **Selector**: `spans(service=, name=, status=, attrs={})`. `status` is a selector field, as in TraceQL's `status` intrinsic, which settles the S2/§4 inconsistency. `sel.where(...)` returns a new selector that ANDs the new fields with the old ones, like `&&`. If the same field is given twice, both values must hold. Values match exactly. There are no globs, the same as TraceQL `=` (it would need `=~` for patterns). In `attrs`, `None` means "key absent" (TraceQL `= nil`). A key that is missing never matches a non-None value.
- **Types**: int↔int64, float↔float64, str↔string, bool↔bool, bytes→str, array→list, map→dict. int and float compare numerically. Any other mismatch between types (`"2"` vs `2`) counts as not equal, both in selectors and in `eq`. An ordering comparison (`lt`, `ge` and so on) between mismatched types records a failure that names both types.
- **Absent attribute**: `span.attrs` is a read-only mapping that supports `in` and `.get`. `span.attrs["k"]` returns an absent marker instead of raising. An `expect.*` that receives the marker fails with `attribute k is absent on svc/Name`.
- **Cardinality**: `exists(count=N)` requires N≥1, and `count=0` is a script error that points to `absent`. `always` and `never` over zero matches fail with `0 spans matched` (Tracetest: a spec whose selector matched nothing fails), so a rename never passes silently (R5).
- **`duration_ms`**: a float, (End-Start)/1e6, with no rounding. It is `None` when the span is not closed, and comparing it then fails with "span still open".
- **Structure**: `span.parent` is a CheckSpan or `None` (a root). Orphans are already exit 3 per D1. `span.trace` is the root CheckSpan, like TraceQL's `trace:rootService`/`rootName`. Synthetic and declared spans are never matched, because D1 drops them.
- **Descriptions**: a selector renders as `svc/Name{k="v",status="error"}` with the fields that were set. A trace renders as `rootSvc/RootName{...}`, listing the root attributes whose values differ among same-named roots in the run, at most 2, sorted by key.
- Rollout: additive and v0.

Steelman: these are the semantics that users of Tempo, Grafana and Tracetest already know. "Fields in one selector refer to one span" and "empty selection fails" are the two rules people least often get wrong. Typed, exact matching removes the coercion surprises behind the CEL pain in synthesis §3.5. Every S1 pick maps to exactly one selector field (service, name, status, attribute) or one `eq`. Matching is a linear scan that only compares strings and typed values, which fits easily in M5's 20µs.

Pros: same-span versus same-trace is unambiguous. `absent` and `None` cover "attribute absent". There are no vacuous passes. The existing D4 dialect is enough.

Cons / failure modes / hidden costs:
- `never` over zero matches fails. That breaks the logic convention ("nothing went wrong" ought to pass), and v1.1 watch, where most spans don't match, will have to special-case it.
- Exact match is inconsistent with praytel's own glob filters (`internal/filter/filter.go:354-370`), so users of `--filter` will expect globs.
- Structural rules cost more to write than TraceQL `>>`. An ancestor walk needs a bounded `for`, since recursion is off (D4).
- The "discriminating root attributes" rule depends on the other traces in the run, so a description can change when the workload changes.
- Neither TraceQL nor Tracetest has `always`/`never`, so the convention is borrowed at that point rather than copied.

Criteria scores:
- §4 table exactly, absent=failure: 4 - all of it is covered, but `status` is added to the selector and `count=0` is rejected.
- Predictability: 5 - typed, no coercion, no vacuous results.
- Robustness to renames: 5 - empty selection fails for every selector-based builtin, and no IDs are used.
- Expressiveness S1-S4, X1: 4 - X1 works through `parent`, but ancestor rules need loops.
- Recordability: 5 - each pick is one field or one `eq`.
- Matching cost: 5 - exact comparisons with no regex or glob compilation.

Cost: M     Reversibility: hard (semantics become v0 public API; changing `never` over zero later would flip verdicts)

Unknowns / assumptions: That Tracetest fails a spec with zero matches is from memory; the research note does not confirm it. The trace-description rule has no prior-art precedent. The assumption is that bytes attributes are rare enough that str is acceptable.
