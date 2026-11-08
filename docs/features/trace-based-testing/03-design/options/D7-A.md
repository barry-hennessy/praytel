## Option A: Exact match, Starlark `==`, fail-closed on emptiness   (stance: minimal change)
Summary: Selectors are conjunctions of exact-equality constraints on `service`, `name`, `status` and `attrs`, matched over D1's `[]CheckSpan` with Starlark's own `==`. Nothing is coerced, globbed or vacuously true: no matching span is a failure everywhere, and an absent attribute is a sentinel that `expect.*` reports. S2 is resolved by adding `status` as a fourth selector field, not by editing the scenario.

How it works:
- `internal/praytest/selector.go`: `Selector{service, name, status *string; attrs []kv}`. `spans(service=, name=, status=, attrs=)` takes str for the first three (`status` in `"unset"|"ok"|"error"`, any other value is an error) and str/int/float/bool values in `attrs`. `.where(...)` takes the same kwargs and appends constraints (AND). Contradictory constraints are legal and match 0 spans. No globs.
- Match: a linear scan over `CheckView.Spans` in slice order. Attribute values map string->str, int64->int, float64->float, bool->bool, bytes->bytes, array->frozen list, map->frozen dict. Equality is `starlark.Equal`, so `2 == 2.0` matches and `"2" != 2`. Synthetic spans never match (D1 already drops them).
- `span`: a frozen struct with `service`, `name`, `status` (str), `status_message`, `duration_ms` (float, `End-Start`; `None` when `!Closed`), `attrs`, `parent` (span or `None`) and `trace` (the root span, not a trace object). `attrs` supports `in`, `.get(k, d)` and `[k]`. `[k]` on a missing key returns `Absent(k, span)`. `expect.*` given an `Absent` records `attribute k is absent on svc/name`. Any operator applied to it raises the same text as a runtime error (still exit 4). It is falsy.
- `exists(count=None)` means ≥1 match. `count` must be ≥1, and `count=0` is an error that says to use `absent`. `always`/`never` over 0 matches **fail** with `0 spans matched` (plus A4's sampling hint).
- Descriptions: a span prints as `svc/name`. A selector prints as `[svc][/name]{status="..",k="v"}`, omitting empty parts, which gives `payments{status="error"}`. A trace prints as the root's `svc/name{...}` with only root attributes whose key is `id` or ends in `.id`/`_id`, which gives `web/Orders.Create{order.id="A-1001"}`.
- Rollout: new code only. All builtins are registered in D4's surface golden.

Steelman: every rule here already exists somewhere the user knows it. Equality is Python's/Starlark's `==`, and identity is the graph box (service/name). Failing closed on empty is the only choice that makes R5 work: a renamed span turns every `always`/`never` red instead of silently green. One extra enum field fits S2 literally and is the cheapest exact match there is (M5). Recording (S1) maps each tick to one exact kwarg, so the generated selector equals what the user saw.

Pros: small, totally specified surface. No coercion table to document. Cheap per span. Recorded files need no escaping.
Cons / failure modes / hidden costs:
- Users cannot write "every X, if any": a vacuous `always` is impossible without a new builtin.
- No globs: volatile names (`GET /users/123`) can only be selected via attrs. Inconsistent with `--filter` globbing.
- The `.id` trace-description rule is a heuristic. Roots without such keys print bare, which is ambiguous across traces.
- `starlark.Equal` int/float equivalence is defined, but users may still expect `"200"` to match int 200.
- An `Absent` that is falsy lets `if s.attrs["x"]:` pass silently inside predicates.
- `status=` is the only non-attr field. Asks for `status_message=`/`duration` will follow.
- X1 ancestor walks need bounded `for` loops (D4).

Criteria scores:
- §4 table incl. absent=failure: 4 - all builtins exact. `status` is a necessary addition.
- Predictability: 4 - no coercion and no vacuous truth. The falsy `Absent` is the one wart.
- Renames/volatile: 3 - renames fail loudly, but without globs volatile names are not handled.
- Expressiveness S1-S4/X1: 4 - all scenarios work. Only direct `parent`/root `trace`.
- Recordability: 5 - each pick is one exact kwarg or one `expect`.
- Matching cost: 5 - equality on a slice scan.

Cost: S     Reversibility: easy to widen (adding globs or vacuous mode later is additive), hard to narrow once v0 ships
Unknowns / assumptions: go.starlark.net lets a custom type error on comparison but not on truthiness (assumed). The `.id` heuristic must be checked against real roots. Whether `bytes` attrs occur in practice.
