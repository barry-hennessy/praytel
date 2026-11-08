## Option B: Selectors as composable typed values   (stance: most extensible)
Summary: A selector is a frozen value compiled to a pure-Go `selector.Selector` that file runs, the recorder and `--watch` all evaluate. Fields are `service`, `name`, `status` and `attrs`, which resolves the S2 inconsistency. Two structural fields, `parent=` and `trace=`, take another selector. Any field may hold a Starlark callable instead of an exact value. The value has one canonical printed form, used for failure messages and recorded files.

How it works:
- New package `internal/selector` with no Starlark import: `Selector{Kind; Service, Name Matcher; Status *domain.SpanStatus; Attrs []AttrMatch; Parent, Trace *Selector}`. `Matcher` is an interface (`Exact`, `Func`). `Select(CheckView) []int` and `Matches(CheckView, i) bool` give watch a per-span path (D12). `String()` produces `payments{status="error"}` and `Source()` produces `spans(...)`. `Kind` is `span` in v1 and is the seam for a later `logs(...)`.
- `internal/praytest` binds `spans(service=, name=, status=, attrs={}, parent=, trace=)`. `.where(**same)` ANDs onto a copy, and setting a field twice is a load error. All names go in the D4 registration table.
- Types: Starlark int, float, string and bool map to int64, float64, string and bool. bytes become `bytes`, arrays become a frozen tuple, maps a frozen dict. int and float compare numerically. Every other cross-type pair is unequal, and ordering it raises `cannot compare string "2" with int 2`. Nothing is coerced.
- Matching is exact by default. Globs are written as callables, e.g. `name=lambda n: n.startswith("Gateway.")`, so no glob builtin is added.
- Absent attribute: `s.attrs[k]` returns an `absent` sentinel. A comparison against it fails as `attribute k is absent on svc/name`. `in` and `.get` work.
- Over zero matches, `always` and `never` fail with `0 spans matched <sel>`, plus the A4 sampling hint. `count=0` is accepted and treated the same as `absent`.
- `span.parent` is a span, or `None` for a root or orphan. `span.trace` is the root span. Synthetic spans never match (D1). `duration_ms` is a float, or `None` when the span is unclosed, and comparing `None` names the span as unclosed.
- Trace description: the root's `service/name` plus up to 2 root attributes whose values differ between roots of the same box in the run. With this rule S4 prints `order.id="A-1002"`.

Steelman: One value type serves three consumers: run, record and watch. It also yields the failure-message text, so these cannot drift apart. `parent=` and `trace=` let X1 and same-trace rules be written declaratively. Unlike lambdas, those rules are recordable, watchable and printable. Callable fields give globs and volatile-value tolerance without growing R9's builtin list. The `Kind` seam keeps logs additive.

Pros: totally defined semantics; no coercion; X1 works without `while` or recursion (D4); the matcher is reusable by the IDEAS.md query language later.

Cons / failure modes / hidden costs:
- `parent=`, `trace=` and callable fields go beyond the §4 table and touch the TraceQL non-goal. They also become frozen v0 surface.
- A callable field can raise an error or hit the step cap while matching. The error appears in selection, not in an assertion, which confuses users.
- Callables cannot be printed canonically (`name=<lambda>`), so recorded files and offender text lose fidelity.
- Nested `trace=` costs O(spans × traces) unless results are memoized per view. Watch sees `Trace=-1` before the root closes.
- Failing `always` on zero matches breaks the usual meaning of "always", where an empty set passes.
- The "differing root attrs" rule changes with what else ran.

Criteria scores:
- §4 table exact, absent fails: 3 - absent handling is exact, but the option adds fields beyond the table.
- Predictability: 4 - no coercion and no vacuous passes; callable fields can fail during selection.
- Robustness to renames and volatile values: 4 - no IDs anywhere; callables tolerate volatile values.
- Expressiveness without new builtins: 5 - covers S1-S4 and X1 declaratively.
- Recordability: 4 - every pick maps to a field; callables do not record.
- Matching cost: 3 - exact values take a fast path, but callables and nested selectors cost more per span.

Cost: L     Reversibility: hard. `parent=`, `trace=` and callable fields can be held back from v1, but once they ship in v0 they are one-way.

Unknowns / assumptions: Starlark callable overhead per span against M5's 20µs is not measured. It is assumed watch can defer `trace=` until the root resolves. It is assumed users accept `always` failing on zero matches.
