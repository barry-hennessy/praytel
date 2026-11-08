# D7: What are the exact semantics of selectors and assertions over spans?
Status: decided     Depends on: D1     Parent: none
Vision refs: §4 Assertion model, S1 (selector from picks, `.where`), S2 (`count`, absence, missing attribute), S4 (always/never), X1 (`span.parent`, `span.trace`); §5 Selectors; R5, R9; A4; M4, M5

## Context
- Vision table: `spans(service=, name=, attrs={})`, all optional, all must match; `expect.exists/absent/eq/ne/lt/le/gt/ge/always/never`; `run.one`; comparable fields `span.attrs[k]`, `status`, `status_message`, `duration_ms`, `service`, `name`; `span.parent`, `span.trace`.
- **Inconsistency to resolve:** S2 uses `spans(service = "payments", status = "error")`, but §4 lists only `service`, `name`, `attrs` as selector fields. S1 uses `.where(attrs=...)` to refine a selector; how `where` combines with existing fields is unstated.
- Service is `Component` from resource `service.name` (`internal/receiver/traces.go:119`); span name and service are the graph's box identity (vision §5).
- Attribute values are typed: string, int64, float64, bool, bytes, array, map (`internal/domain/value.go:12-110`). Status is unset/ok/error (`internal/domain/span.go:5-15`). Starlark has int, float, string, bool, list, dict; the mapping and cross-type comparisons (int attr vs `2`, int vs float, string "2" vs int) are open.
- Existing matching elsewhere is glob-based (`internal/filter/filter.go:354-370`, `internal/attrspec/attrspec.go:25-46`); the vision examples use exact values. Whether selector fields accept globs is open and user-visible.
- Unstated cases: `always`/`never` over zero matching spans; `count=0` versus `absent`; `span.parent` when the parent never arrived (orphan) or is synthetic; what `span.trace` is (root span, trace object with spans, or both); whether synthetic orphan spans (`internal/domain/event.go:19`) are ever matched; `duration_ms` type and rounding for unended spans; whether `"k" in s.attrs` and `s.attrs.get` work (S4 uses `in`).
- Absent attribute: comparison fails as `attribute payment.result is absent on payments/Gateway.Charge` (S2), not a key error. Synthesis §3.5 records CEL "no such key" pain on this exact point.
- Failure messages describe selectors (`payments{status="error"}`) and traces (`web/Orders.Create{order.id="A-1001"}`); the rule for which attributes describe a trace is part of these semantics.
- Sampling makes "0 spans matched" ambiguous (A4); the vision allows an added hint.
- M5 (v1.1): ≤ 20µs per property per span, so matching must be cheap enough to run per closing span.

## Goals
- Total semantics: every builtin is defined for 0, 1 and many matches, absent attributes, missing parents and type mismatches.
- Same-span and same-trace conditions are both expressible with the vision's builtins.
- Selectors never use IDs; renames surface as "0 spans matched" (R5).
- Every S1 pick maps to one selector field or one assertion.

## Non-goals
- Span events, links and logs in selectors (§7 Later).
- TraceQL-style structural operators beyond `parent`/`trace` (X1 is stretch).
- New builtins outside the §4 table plus `env()` (R9).

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| Matches the §4 table exactly, including absent attribute = failure | 20 | §4; S2 missing attribute |
| Predictability: no surprising type coercion or vacuous results | 20 | synthesis §3.5; R9 (semantics become public API) |
| Robustness to renames and volatile values | 15 | R5; synthesis §3.3 |
| Expressiveness for S1-S4 and X1 without new builtins | 15 | §4, X1, R9 |
| Recordability: each pick maps to a selector field or assertion | 15 | S1 steps 3-5 |
| Matching cost | 15 | M4, M5 |

## Options
Full texts: `../options/D7-A.md`, `D7-B.md`, `D7-C.md`. Scores below are the decider's, after checking the cited code; explorers' self-scores are in brackets where changed.

### Option A: Exact match, Starlark `==`, fail-closed on emptiness (minimal change)
Selector = AND of exact `service`/`name`/`status`/`attrs`; `.where` appends; no globs; `always`/`never`/`exists` fail on 0 matches; `count=0` is an error pointing to `absent`. `s.attrs[k]` on a missing key returns a falsy `Absent` that `expect.*` reports and that "raises on any operator". Trace description = root `svc/name` + root attributes whose key is `id` or ends in `.id`/`_id`. Cost S.

### Option B: Selectors as composable typed values (most extensible)
Pure-Go `internal/selector` (no Starlark import) with `Select`/`Matches`/`String`/`Source`, plus `parent=`/`trace=` sub-selector fields and Starlark callables in any field (globs as lambdas). Type rules like C. Fails on 0 matches; `count=0` = `absent`. Trace description = root + up to 2 root attributes whose values differ among same-box roots in the run. Cost L.

### Option C: TraceQL/Tracetest semantics on the vision builtins (most conventional)
Selector = one TraceQL `{}` block: exact, typed, ANDed fields incl. `status`; `.where` = `&&`; no globs; `attrs={"k": None}` = key absent. Type mismatches unequal; ordering across types records a failure naming both types. Fails on 0 matches; `count=0` is an error. `duration_ms` float, `None` when unclosed. `parent`/`trace` resolve to spans. Trace description = B's "differing root attributes" rule. Cost M.

### Verified claims
- Globs in praytel's filters are `path.Match` on name or component (`internal/filter/filter.go:354-369`). Confirmed; exact selectors are inconsistent with `--filter`.
- `shop`'s root `web/Orders.Create` carries `order.id` and `customer` (`demo/workloads/shop/main.go:93-95`). So A's `.id` rule yields S2's/S4's `{order.id="A-1001"}` text. B/C's "values differing among same-named roots in the run" rule yields **nothing** in S2's visa case (`-card visa` places one order, so there is one root and no differing value) and prints a bare `web/Orders.Create`, contradicting S2's own expected output. It also makes the description depend on the workload.
- A's claim that a custom type can raise on comparison but not on truth testing is not shown by the spike (`spike-starlark.md` tests none of this) or anything in the repo, and no `go.starlark.net` source is in the local module cache. Discounted: no part of the chosen design depends on a custom value raising on `==`.
- C's "Tracetest fails a spec with zero matches" is from memory (its own unknowns). Not credited; fail-on-empty is chosen on R5 grounds instead.
- C's con that v1.1 watch "will have to special-case" `never` over zero matches is overstated. Watch (S6) only reports per-span violations; the zero-match check is a whole-run fact that only exists after drain.

### Scores (weight × score / 5)
| Criterion (w) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| §4 exact, absent = failure (20) | 4 | 2 [3]: `parent=`, `trace=`, callable fields are new v0 surface beyond §4 | 4 | 4 |
| Predictability (20) | 3 [4]: Absent behaviour rests on an unverified interpreter claim | 3 [4]: callables can fail during selection; run-dependent descriptions | 4 [5]: run-dependent trace descriptions that miss S2 | 5 |
| Renames / volatile (15) | 4 [3] | 4 | 4 [5]: no answer for volatile names either | 4 |
| Expressiveness (15) | 4 | 5 | 4 | 4 |
| Recordability (15) | 5 | 3 [4]: callables cannot be recorded or printed | 5 | 5 |
| Matching cost (15) | 5 | 3 | 5 | 5 |
| **Weighted /100** | **82** | **65** | **86** | **90** |

## Decision
Chosen: **Hybrid, C as the base.** It takes C's typed exact matching and its record-and-continue handling of type mismatches, A's narrower `.where` and its run-independent trace description (fixed so it is never bare), and B's pure-Go matcher seam without B's extra fields. Specification:

1. **Selector fields.** `spans(service=, name=, status=, attrs={})`. All are optional and all must match the same span (AND). `status` takes exactly `"unset"`, `"ok"` or `"error"`. Any other value is a Starlark error (misuse aborts the check with exit 4, per D6). This settles S2's inconsistency in favour of S2: `status` is the only selector field added beyond §4. `attrs` values must be Starlark `str`, `int`, `float` or `bool`. `None`, lists, dicts and bytes are errors in v0, which leaves `None` free to mean "key absent" later. There are no `parent=`, `trace=`, `status_message=`, duration or callable fields in v1.
2. **`.where(**same kwargs)`** returns a new selector with the constraints ANDed. Setting `service`, `name` or `status`, or the same `attrs` key, to a *different* value than the receiver already holds is an error (`where: name already set to "X"`). Setting it to the same value is a no-op. Selectors are frozen values, so S1's module constants are safe under D4.
3. **Matching is exact, never glob.** Plain strings never get pattern meaning, and no characters are reserved. If globs are added later, they come as a distinct, additive form (for example a `glob("Gateway.*")` value or a `name_glob=` kwarg), never by reinterpreting existing strings. Name equality is byte equality, with no case folding or trimming.
4. **Types.** CheckSpan attributes map string→`str`, int64→`int`, float64→`float`, bool→`bool`, bytes→`bytes`, array→frozen `list`, map→frozen `dict`. Equality is Starlark equality: int and float compare numerically (`2 == 2.0`), bool and int are distinct, and `"2" != 2`. Nothing is coerced. In selectors, the Starlark literal is converted once, when the selector is created, to a `domain.Value`, and compared in Go against `CheckSpan.Attrs` (sorted, so binary search) with the same rules. `expect.eq/ne` on mismatched types simply compare unequal. `expect.lt/le/gt/ge` on mismatched or unordered types **record a failure** (`cannot compare string "2" with int 2`) and continue. They never raise. Inside user lambdas, Starlark's native rules apply (a cross-type `<` is a runtime error, exit 4).
5. **Span value.** A frozen struct with `service`, `name`, `status` (str), `status_message` (raw, per D1), `duration_ms`, `attrs`, `parent` and `trace`.
   - `duration_ms` is a float, `(End-Start)/1e6`, with no rounding. It is an **open marker** (built like the absent marker, so it carries its span) when `!Closed`, which happens only in the live record view, because file runs with unclosed spans are INCOMPLETE (D1/D2). `expect.*` given that marker records `span still open: svc/name`. (Amended after D6: a plain `None` cannot name its span; if an implementation keeps `None`, the line drops the span name.)
   - `parent` is a span, or `None` for a root (or when unresolved live). Orphans cannot reach a check in file runs, because they are exit 3 per D1. `trace` is the **root span**, not a trace object. Synthetic and declared spans are never matched or returned (D1 drops them).
6. **Attrs mapping and absent attributes.** `s.attrs` is a read-only mapping that supports `k in s.attrs`, `s.attrs.get(k, default)`, `len`, iteration over keys in sorted order, and `s.attrs[k]`. A missing `[k]` returns an **absent marker**: an opaque value that carries the key and the span description and prints as `<absent payment.result>`. The guarantee does **not** rest on how the interpreter treats custom types:
   - Every `expect.*` checks each argument for the marker with a Go type assertion **before** comparing. It records `attribute k is absent on svc/name` and continues. This is the only behaviour v1 promises for the marker (S2).
   - Elsewhere (in lambdas and `if`), the marker's behaviour is documented as unspecified in `praytest.5`, and the guide says to use `in` or `.get` in predicates. The implementation makes its truth value `False`. A unit test in `internal/praytest` pins what `==`, `<` and truth testing do with the marker on the pinned interpreter, so a bump that changes them shows up in review (D4 surface golden).
7. **Cardinality.** `expect.exists(run, sel, count=None)` fails on 0 matches, or on anything other than exactly `count`. `count` must be an int ≥ 1. `count=0` is a misuse error: `count must be >= 1; use expect.absent`. `expect.absent` fails on ≥ 1 match. `run.one` fails on 0 or > 1 matches (skip semantics are D6's).
8. **Always/never over zero matches FAIL** with `0 spans matched <selector>`. D6 may append A4's sampling hint. Neither builtin is vacuously true in v1. Offenders are listed in `CheckView.Spans` order, up to 10, then `... and N more`.
9. **Descriptions** (deterministic and independent of the run):
   - A span is `svc/name`.
   - A selector is `[svc][/name]{status="..",k="v",...}`. Parts that are not set are left out (`payments{status="error"}`, `Gateway.Bank.Authorize`). `status` comes first, then attrs by key, and values are printed as Starlark literals.
   - A trace is the root span's `svc/name` plus up to 2 root attributes in braces. If the root has attributes whose key is `id` or ends in `.id` or `_id`, those are used, sorted by key. Otherwise the first 2 root attributes by key are used. If the root has no attributes, the trace prints bare. Each value is truncated to 40 characters. This gives `web/Orders.Create{order.id="A-1001"}` for S2 and S4.
10. **Matcher seam.** The matcher is a pure-Go package with no Starlark import: `internal/selector`, with `Selector`, `Matches(CheckSpan) bool`, `Select([]CheckSpan) []int`, `String()` and `Source()`. File runs, D9's Ctrl-W cardinality check, D8's generator and v1.1 watch all use it. `internal/praytest` only binds it to Starlark (D4: the only importer). The matcher is a linear scan in slice order. Each candidate costs at most 3 string compares, 1 enum compare and k binary searches.

Why, against the criteria:
- **§4 table (20):** every §4 builtin keeps its meaning. Absent attributes are failures through a mechanism that needs only a Go type check. The two deviations are `status=` (forced by S2) and rejecting `count=0`.
- **Predictability (20):** nothing is coerced. No assertion is vacuously true. Type mismatches record failures instead of killing the check. Failure text does not change with what else ran. No promise depends on unverified interpreter behaviour.
- **Renames (15):** an empty selection fails in every selector-based builtin, so a rename or a typo in a selector in the required `trace-tests` job is red, not silently green (R5). Volatile names stay unsolved (see "We give up").
- **Expressiveness (15):** S1-S4 work as written, and so does X1 (`s.parent != None and s.parent.service == "web"`). Ancestor walks use a bounded `for` (D4).
- **Recordability (15):** each S1 pick is one exact kwarg (`service`, `name`, `status` or one attrs key) or one `expect.eq`. No escaping is needed, because there are no glob characters.
- **Cost (15):** exact compares on precompiled Go values, with no Starlark calls during selection. This fits M4 and leaves M5's per-span budget for the predicate itself.
- **Reversibility:** every v1 restriction is a *narrow* one, which later widens additively: globs as a distinct form, `allow_empty=True` on always/never, `None` in attrs meaning absent, `count=0`, `parent=`/`trace=` fields, more selector fields. The opposite choices (vacuous pass, glob strings, accepting `count=0`) could not be narrowed after v0 without flipping verdicts.

We give up:
- **Vacuous "every X, if any".** v1 cannot express it, because no §4 builtin observes emptiness without failing. The brief's "properties that should never hold" is still expressible with `never`, but `never` also asserts that at least one span matched. Authors whose span may legitimately be missing must split the case by input.
- **Globs and volatile span names** (`GET /users/123`). They can only be selected through attrs, and selectors behave differently from `--filter`. The guide must say so.
- **B's declarative `parent=`/`trace=` and callable fields.** Structural rules live in lambdas, so they are not recordable and not printable as selectors.
- **"Absent" as a selector condition** (`attrs={"k": None}`). Authors use `expect.eq("k" in span.attrs, False)` or a predicate.
- **A defined marker semantics outside `expect.*`.** `if s.attrs["x"]:` silently treats an absent attribute as false.
- **A guaranteed-unique trace description.** Roots without id-like keys show their first 2 attributes, which may not tell traces apart.

Rejected:
- A: closest in spirit, but its absent marker relies on an unverified claim that a custom value can raise on comparison, and it allows contradictory `.where` silently.
- B: `parent=`/`trace=` and callable fields widen the frozen v0 surface beyond §4 toward the TraceQL non-goal. Callables break recording and printing. Its trace-description rule contradicts S2's output.
- C: the chosen base, but its trace-description rule prints S2's single-order trace bare and changes with the workload, and `attrs={"k": None}` adds surface v1 does not need.

Revisit if:
- **Vacuous need:** 2 or more dogfooding files or issues (M7 corpus) wrap `always`/`never` in a case split, or ask for "if any". Then add `allow_empty=True` (additive, golden update).
- **Globs:** 2 or more issues or files need to select span names with variable parts that are not exposed as attributes. Then add a distinct glob form (never reinterpret plain strings).
- **Descriptions:** 1 report of two failing traces that print identical descriptions in one failure, or a dogfooding root without id-like keys whose description is noisy. Then revisit the attribute choice (for example a per-file `describe=` option), not the selector semantics.
- **Interpreter:** the marker pin test or the D4 surface golden changes on a `go.starlark.net` bump.
- **Cost:** `make bench-diff` shows selection above 10ms per case at 10k spans × 20 assertions (a fifth of M4), or the v1.1 watch benchmark shows `Matches` above 2µs p99 per span. Then index by (service, name).

Consequences for other decisions:
- **D1:** consistent. The design evaluates over `[]CheckSpan` in slice order, uses `Parent`/`Trace` indices, raw `status_message` and `duration_ms` derived as above. The live `Trace = -1` maps to `None`.
- **D4:** add to the registration table and the surface golden: `spans(service, name, status, attrs)`, the selector method `where`, `expect.exists/absent/eq/ne/lt/le/gt/ge/always/never`, `run.one`, and the span/attrs attribute names. No new builtins beyond §4 + `env()`. `internal/selector` must not import `go.starlark.net` (depguard already enforces this).
- **D6:** owns the text and positions of: absent marker failures, `span still open`, cross-type ordering failures, `0 spans matched <selector>` (plus optional A4 hint), and misuse errors (bad `status`, bad attr value type, conflicting `where`, `count=0`), which abort the check as exit 4. It uses the description rules in item 9.
- **D8:** the generator emits only exact `service`/`name`/`status`/attr kwargs with typed literals (int stays int, status as a string) through `Selector.Source()`. It never emits globs or `None`. Selectors whose attr pick is an array, map or bytes cannot be emitted in v0 and must be refused or omitted.
- **D9:** the Ctrl-W "matches N spans" check calls `internal/selector.Select` on `(*Model).CheckView().Spans`. Unclosed spans match normally. To avoid a cycle, recorder logic must live outside `internal/render/tui` or `internal/selector` must take a TUI-free span type. D9 decides which.
- **D12:** watch uses `Matches(CheckSpan)` per closing span and reports only violations. The zero-match failure is a post-drain rule and does not apply to watch. Predicates that read `parent`/`trace` may see `None` live where a file run sees a span. D12 must either defer those spans until the root resolves or document the difference.
