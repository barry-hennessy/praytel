---
title: Starlark queries
summary: A proposed alternative to the query text of Phases 7 to 9. Starlark builders are the only query language. Go keeps the query tree and evaluates it. Cases, properties with rapid, and a TUI prompt use the same language.
weight: 83
---

# Starlark queries

Status: Proposed. This document is an alternative to the query text of
[Pipeline](PIPELINE.md) Phases 7, 8 and 9. It does not change the pipeline
itself: the projection, the driver, stages, views, triggers and sinks stay.

This document is in ASD-STE100 Simplified Technical English. Technical names
and code identifiers are in `code` format.

## Summary

Praytel has two front-ends for one query tree (`query.Expr`):

- The Starlark builders, for example `span(service = "checkout") >> span(status = "error")`.
- The query text, for example `span{ resource.service.name = "checkout" } >> span{ span:status = error }`.

This design removes the query text. The Starlark builders become the only
language. Go keeps the query tree and the evaluator. Starlark still does not
see events, spans or logs.

The same language then does these jobs:

1. A golden file of a full run.
2. A golden file of part of a run, between two spans.
3. A property test with generated inputs.
4. A table test.
5. A property test that shrinks its input to a minimal example, with
   [rapid](https://pkg.go.dev/pgregory.net/rapid).
6. A prompt in the TUI, as a REPL.

## Terms

The terms in [Pipeline](PIPELINE.md#terms) keep their meanings. This document
adds these terms.

| Term | Meaning |
|---|---|
| Builder | A Starlark builtin that makes a selection, for example `span(...)` or `log(...)`. |
| Matcher | A Starlark value that compares one field, for example `match(".*DROP.*")` or `gt(499)`. |
| Canonical text | The Starlark source text that praytel writes for a selection, a check or a case. |
| Case | One command, the checks for its run, and the result of the run. |
| Shared check | A check before the first case in a file. It applies to each case in the file. |
| Property | A Starlark function and a generator for each of its parameters. Praytel calls the function with drawn values. |
| Example | One call of a property function and the case that the call makes. |
| Generator | A Starlark value that describes a set of values, for example `gen.ints(1, 50)`. |
| Draw | The selection of one value from a generator, for one example. |
| Shrink | The search for a smaller example that fails the same check. |
| Minimal example | The smallest failing example that shrinking found. |
| Verdict | The result of one case: pass, fail or incomplete, with the checks that failed. |
| Verdict cache | A map from the canonical text of a case to its verdict. |
| Target check | The check that the first failing example failed. Shrinking keeps it. |
| Near miss | A record that matches the identity fields of a selection, but not all of its other fields. |
| Context | The setting of a `where` lens that tells which spans around a match it shows. |

## Rules

1. Starlark is the only language for selections. Praytel must not parse a
   second query syntax.
2. Starlark must not see events, spans or logs. A script makes plan values
   only.
3. Praytel stores each selection in a plan as canonical text. Evaluation of
   the canonical text must give the same query tree.
4. A case has one command, one projection and its own checks.
5. Generators get their values from rapid only. A script has no other source
   of random values.
6. A property must not run a command for a case that is in the verdict
   cache.
7. Shrinking must keep the target check. A smaller example that fails a
   different check is not a smaller example of the same failure.
8. The current flags must continue to operate. This is Rule 3 of
   [Pipeline](PIPELINE.md#rules).

## Purpose

The query text and the builders do the same work. Two languages cost:

- About 800 lines of lexer, parser and formatter (`internal/query/lex.go`,
  `parse.go`, `format.go`) and their tests.
- Two syntaxes in the documentation, in the plan JSON and in the TUI.
- A check name that is in one syntax, and a script that is in the other.

The builders are already the recommended form on the command line, because
they need fewer quotes. One language also gives these results:

- A plan is Starlark text. You can copy a check from `--print-plan` into a
  script.
- A failed property writes its minimal example as a case in canonical text.
  You can run it, read it and keep it.
- The TUI prompt, a check file and `-e` accept the same text.

## What changes

| Part | Now (Phase 9) | This design |
|---|---|---|
| `internal/query/ast.go`, `eval.go`, `check.go`, `glob.go`, `path.go` | Query tree and evaluator | No change, with the additions in "Selection language" |
| `internal/query/lex.go`, `parse.go`, `format.go` | Query text | Removed |
| `internal/plan` | Stores query text | Stores canonical text |
| `internal/plan/starlark` | Builders, and strings as queries | Builders only. Adds `Format` and `ParseSelection` |
| `internal/filter` | Compiles globs to query trees | No change. It makes the trees directly |
| `internal/stage/where.go` | Path view only | Adds the context setting |
| TUI `/` search | Query text | Starlark expression. The `:` prompt is added |
| `internal/check` | Pass, fail, incomplete | Adds near misses |
| New: `internal/testrun` | None | Cases, properties, rapid |

The query text is on unmerged branches only. Thus no user has scripts in the
old syntax, and praytel needs no migration path.

## Canonical text

The plan stores canonical text. Example, from `testdata/checks.json`:

```json
{
  "kind": "never",
  "select": "span(service = \"checkout\") >> span(attrs = {\"db.statement\": match(\".*DROP.*\")})",
  "name": "never(span(service = \"checkout\") >> span(attrs = {\"db.statement\": match(\".*DROP.*\")}))"
}
```

Two functions in `internal/plan/starlark` change between text and tree:

- `Format(query.Expr) string` writes canonical text. It writes keyword
  arguments in a fixed order, and dictionary keys in sorted order.
- `ParseSelection(text string) (query.Expr, error)` evaluates one Starlark
  expression. The environment has the builders and matchers only. It has
  no `load`, no outputs and no checks. The step cap is 100 000.

All callers of `query.Parse` use `ParseSelection`: plan validation, the
`where` stage, the table view, `internal/check` and the TUI.

The fuzz test changes. It makes random query trees, formats them, parses the
text and compares the trees. `FuzzParse` is removed.

## Selection language

### Fields and matchers

The builders do not give all functions of the query text now. These
additions close the gap.

| Addition | Example | Query tree |
|---|---|---|
| `exists()` | `span(attrs = {"http.route": exists()})` | `Exists` |
| `ne(v)` | `span(status = ne("ok"))` | `Cmp` with `Ne` |
| `gt(v)`, `ge(v)`, `lt(v)`, `le(v)` | `span(attrs = {"http.status_code": ge(500)})` | `Cmp` with `Gt`, `Ge`, `Lt`, `Le` |
| `not_match(re)` | `log(body = not_match("^DEBUG"))` | `Cmp` with `NRe` |
| `resource = {...}` | `span(resource = {"host.name": "web-1"})` | `Cmp` with scope `resource` |
| `count(s, at_least =, at_most =, exactly =)` | `count(span(name = "retry"), at_least = 4)` | `Count` |

Starlark does not let a type change the meaning of `==`, `<` or `>=`. A
comparison in Starlark must give a boolean. Thus a count is a function with
keyword arguments, not `count(s) >= 4`.

### Structure

| Operator | Result | Status |
|---|---|---|
| `a >> b` | The items in `b` below an item in `a` | Exists |
| `b.child_of(a)` | The items in `b` whose parent is in `a` | Exists |
| `b.after(a)` | The items in `b` with an earlier sibling in `a` | Exists |
| `a << b` | The items in `a` with an item of `b` below them | New |
| `a.before(b)` | The items in `a` with a later sibling in `b` | New |
| `within(a)` | `a \| a >> span()` | New, shorthand |
| `between(a, b)` | `(a \| a >> span()) & (b \| span() << b)`: the chain from `a` down to `b` | New, shorthand |

### Checks

The checks of Phase 8 stay: `never`, `some`, `every`, `at_least`, `at_most`
and `exactly`. This design adds one shorthand:

| Check | Meaning |
|---|---|
| `always(a, s)` | Each item in `a` is also in `s`. It is `never(a & ~s)`, with `a` as the anchor. |

### `and`, `or` and `not`

Starlark does not let a type change `and`, `or` or `not`. A selection is
always true. Thus `a and b` gives `b`, and Starlark shows no error. Use `&`,
`|` and `~`.

Praytel refuses `and`, `or` and `not` in a TUI prompt line and in an `-e`
snippet that is one expression. Praytel parses these lines itself, so it can
find the operators before evaluation. In a `.star` file, these operators
have other correct uses. Thus a file gets no error.

## Near misses

A declarative check does not have a "want X, got Y" message. Example:
`exactly(1, span(name = "Gateway.Charge", status = "error"))` fails with
"0 items matched". The cause can be a missing span, or a span with the
status `ok`.

Praytel finds near misses in the query tree:

1. Find the `Select` nodes of the failed selection.
2. Divide the comparisons of each node into identity fields and other
   fields. The identity fields are `name`, `service` and `component`.
3. A near miss is a record that matches all identity fields and fails one or
   more other fields.
4. For each near miss, show each failed field, the value in the check and
   the value in the record.

Example output:

```
FAIL exactly(1, span(name = "Gateway.Charge", status = "error")): 0 matched
  near miss payments/Gateway.Charge in trace web/Orders.Create{order.id="A-1001"}
    status: want "error", got "ok"
```

Praytel shows a maximum of three near misses for each check.

## Cases

A case connects a command to its checks.

```python
case(name = "gift", command = ["shop", "-pace", "0s", "-card", "gift"],
     exit_code = 0, timeout = "10m", env = {})
```

These rules give the scope of a check:

1. A file without `case` is one run. Its checks apply to the command after
   `--`. This is the Phase 8 behaviour: `praytel -e checks.star -- cmd`.
2. A check before the first `case` is a shared check. It applies to each
   case in the file.
3. A check after a `case` belongs to the most recent `case`.
4. In a property function, rule 3 applies to the call. Each example has its
   own case.

The order of statements gives the scope. This lets a loop make one case and
its checks on each pass (refer to "Table tests").

The runner is the runner of the trace-based testing design. Refer to
"Relation to trace-based testing". It has `praytel test FILE|DIR|GLOB ...`,
the file suffix `.praytest.star`, the input rules and the exit code table. This
design replaces `test(name, command, check = fn)` and the `expect.*` builtins
with `case` and the declarative checks. Refer to "Relation to trace-based
testing".

## Scenario 1: a golden file of a full run

The `--snapshot` flag already compiles to a plan
(`cmd/praytel/internal/args/testdata/plans/snapshot-tree.json`). The
Starlark form is the same plan:

```sh
praytel -e 'snapshot(sections = ["tree", "logs"]) | final | file("testdata/run.golden")' -- go test ./...
```

As a file:

```python
# golden.star
output(snapshot(sections = ["tree", "logs"]) | final | file("testdata/run.golden"))
```

This design changes only the text of a `where` stage in the plan. The plan
goldens change one time.

## Scenario 2: a golden file between two spans

The `where` lens now shows the path view: each match, its ancestors and its
descendants (`internal/query/path.go`). The ancestors are not "between" two
spans. Thus a golden of part of a run needs two additions: a selection for
the part, and a lens that adds no context.

```python
decode = span(name = "Handler.Decode")
encode = span(name = "Handler.Encode")

output(
    where(between(decode, encode), context = "none")
    | snapshot(sections = ["tree"])
    | final
    | file("testdata/handler.golden"),
)
```

"Between" has three meanings. Use the selection for the meaning that you
need:

| Meaning | Selection |
|---|---|
| All work below the input span | `within(decode)` |
| The call chain from the input span down to the output span | `between(decode, encode)` |
| The siblings after the input span and before the output span | `within(span().after(decode) & span().before(encode))` |

The `context` setting of `where`:

| Value | The lens shows |
|---|---|
| `"path"` | Each match, its ancestors and its descendants. This is the default and the current behaviour. |
| `"subtree"` | Each match and its descendants. |
| `"none"` | Each match only. |

With `"subtree"` and `"none"`, a kept span can have a parent that the lens
removed. The lens then connects the span to its nearest kept ancestor. The
tree view writes `…` on that line. Thus a new span between two kept spans
adds no line to the golden.

If the input span occurs two times in a run, the golden shows two parts. The
order of the parts is the sibling order. Current goldens use the same rule.

## Scenario 3: a property test

A property is a Starlark function and a generator for each parameter.
Praytel calls the function one time for each example. The function makes a
case and its checks. Praytel runs the case and gets a verdict.

```python
cards = gen.one_of(["visa", "amex", "gift", "mastercard"])

def orders(card, qty):
    case(command = ["shop", "-pace", "0s", "-card", card, "-qty", str(qty)])

    # Invariants: these are true for each input.
    every(span(name = "Orders.Place"), has = exactly(1, span(name = "Gateway.Charge")))
    never(span(service = "payments") >> span(service = "inventory"))
    never(span(name = "Gateway.Bank.Authorize") & longer_than("200ms"))

    # Oracle: Starlark calculates the expected result from the input.
    if card == "gift":
        some(span(name = "Gateway.Charge", status = "error"))
        some(log(body = contains("authorization refused")))
    else:
        never(span(service = "payments", status = "error"))
        some(span(name = "Orders.Place", attrs = {"order.qty": qty}))

property("orders", orders, card = cards, qty = gen.ints(1, 50), examples = 30)
```

Starlark runs one time for each example. It makes plan values only. Rule 2
stays true.

The program gets its input from `command` and `env`. The checks examine the
trace and the logs. The exit code is a part of the case (`exit_code`).

Two types of input:

- Praytel generates the input. The script knows the input before the run.
  Starlark calculates the expected value, as in the example.
- The program generates the input, for example a Go test with
  `testing/quick`. The script does not know the input. Use `var("x")` to bind
  the input from a span attribute (Phase 9).

## Scenario 4: table tests

The program runs the table. The script makes one check for each row (Phase 9):

```python
def double(x): return x * 2

for x in [0, 1, 2, 7, -3]:
    every(span(attrs = {"test.name": "TestDouble", "input": x}),
          has = span(attrs = {"output": double(x)}))
```

Praytel runs the table. The script makes one case for each row:

```python
ROWS = [
    # card,   charge status, refusal logged
    ("visa",  "ok",          False),
    ("amex",  "ok",          False),
    ("gift",  "error",       True),
]

never(span(service = "payments") >> span(service = "inventory"))  # shared check

for card, status, refused in ROWS:
    case(name = card, command = ["shop", "-pace", "0s", "-card", card])
    exactly(1, span(name = "Gateway.Charge", status = status, attrs = {"card.type": card}))
    (some if refused else never)(log(body = contains("authorization refused")))
```

A table is a property with fixed examples. Thus `property(..., examples =
ROWS)` uses the same runner, without shrinking. The view `table()` (Phase 9)
writes TSV. It is a different function. Thus "table test" is a pattern, not
a builtin.

## Scenario 5: shrinking with rapid

### Why rapid

[rapid](https://pkg.go.dev/pgregory.net/rapid) is a Go library for property
tests. It has these properties:

- It shrinks on the stream of choices that the generators made. Thus a
  composite generator shrinks without code for each type. Hypothesis
  (Python) uses the same method.
- `rapid.Check` accepts a `rapid.TB` interface, not only `*testing.T`. Thus
  praytel can run it outside `go test`.
- Each draw has a label. Praytel uses the parameter name as the label.
- It has tagged releases (v1.3.0). `go.starlark.net` has none.

### Data flow

1. The script calls `property(name, fn, examples =, seed =, budget =,
   confirm =, **generators)`. Praytel stores the generators as values. Each
   generator is a tree of `gen.*` calls.
2. Praytel compiles each generator tree to a `rapid.Generator[starlark.Value]`.
3. Praytel calls `rapid.Check` with a praytel `TB` and a property function.
4. For each call, the property function:
   1. Draws one value for each parameter of `fn`, in parameter order. The
      label is the parameter name.
   2. Calls `fn` on a new Starlark thread with the step cap.
   3. Gets the case and its checks. It writes the canonical text of the case.
   4. Finds the verdict in the verdict cache. If the verdict is not there,
      it runs the case and adds the verdict to the cache.
   5. Tells rapid the result. Refer to "Target check".
5. rapid finds a failing example, then shrinks it.
6. The last call of the property function is the minimal example. Praytel
   keeps the drawn values and the case of each call. Thus it does not parse
   the log text of rapid.
7. Praytel writes the report and the repro file.

### Generators

| Starlark | rapid |
|---|---|
| `gen.ints(min, max)` | `Int64Range` |
| `gen.floats(min, max)` | `Float64Range` |
| `gen.bools()` | `Bool` |
| `gen.one_of(values)` | `SampledFrom` |
| `gen.just(v)` | `Just` |
| `gen.strings(regex)` | `StringMatching` |
| `gen.strings(min_len =, max_len =)` | `StringN` |
| `gen.lists(g, min_len =, max_len =)` | `SliceOfN` |
| `gen.distinct_lists(g, min_len =, max_len =)` | `SliceOfNDistinct`, with the canonical text as the key |
| `gen.permutations(values)` | `Permutation` |
| `gen.choice(g1, g2, ...)` | `OneOf` |
| `gen.map(g, fn)` | `Map`. `fn` is Starlark. It runs when praytel draws a value |
| `gen.filter(g, fn)` | `Filter` |

The values are Starlark values. `gen.map` and `gen.filter` call Starlark
when praytel draws a value. They do not see events. Rule 2 stays true.

### Verdict cache

rapid shrinks on bit blocks, not on values. It calls the property function
many times for each drawn value. Many calls make the same values. Thus
praytel must not run a command for each call.

The key of the verdict cache is the canonical text of the case: the command,
the environment and the checks. Two draws that make the same case share a
verdict. This is possible only because a case is data.

Evidence. A probe with rapid v1.3.0 used a property that fails when
`card == "gift"` and `qty > 3`. Each new example slept 20 ms, as a model of
a run. The probe used 30 examples and the seed 7.

| Setup | Property calls | Runs | Minimal example | Time |
|---|---|---|---|---|
| No cost for a run, no cache, random seed | 1 735 | 1 735 | `gift`, `4` | 4 ms |
| 20 ms for each run, no cache, `rapid.shrinktime` 200 ms or 2 s | 201 | 201 | `gift`, `50` (not shrunk) | 4.2 s |
| 20 ms for each run, verdict cache | 1 735 | 12 | `gift`, `4` | 0.27 s |

Without the cache, rapid used all of its time on the first block, and the
time limit did not stop it at the limit. With the cache, 12 runs gave the
minimal example. A real run takes 0.5 s to 2 s. Thus the cache is
necessary.

### Target check

rapid compares the Go traceback of a failure to find "the same failure".
Its shrinker compares the traceback only. Praytel reports each failure from
one Go call site. Thus, to rapid, all checks are the same failure. Shrinking
can then go from a failure of check A to a failure of check B.

Praytel prevents this:

1. The first failing example sets the target check. If many checks fail, the
   target is the first in file order.
2. After this, the property function tells rapid "fail" only if the target
   check fails. It tells rapid "pass" for other failures.
3. The failure message is the canonical text of the target check. It does
   not contain the drawn values. The reproduction step of rapid also
   compares the message text (`sameError`). A message with values gives a
   false "flaky test".

The report shows the other failed checks of the minimal example also.

### Flaky and incomplete runs

A run of an external process can fail for a cause that is not the input.

- A new failing case runs `confirm` times (default 2). Praytel records a
  failure only if all runs fail the target check. If not, praytel records
  "flaky", tells rapid "pass" and continues. The report shows the number of
  flaky examples.
- An incomplete case (refer to [Pipeline](PIPELINE.md#trace-state)) tells
  rapid "pass". Thus shrinking does not go to a case with a slow drain. The
  report shows the number of incomplete examples. If no example was
  complete, the property result is incomplete (exit 3).
- rapid also runs its first failure again to reproduce it. That run gets the
  verdict from the cache. Praytel's `confirm` step does this work.

### Budget

rapid gets a deadline from `*testing.T` only (`checkDeadline`). With a
praytel `TB`, its deadline is 24 hours. Thus praytel enforces its own time
budget:

1. `budget` (default `5m`) covers generation and shrinking of one property.
2. Praytel sets `rapid.shrinktime` to the remaining budget before shrinking.
3. When the budget is used, the property function runs no new case. It tells
   rapid "pass" for each case that is not in the cache. rapid then cannot
   find a smaller example, and it stops.
4. The report says that the example is possibly not minimal.

### Report and repro file

```
orders.praytest.star
  FAIL orders  example 4 of 30, seed 7
    first example:   card = "gift", qty = 50
    minimal example: card = "gift", qty = 4   (12 runs, 2 flaky, 0 incomplete)
    never(span(service = "payments", status = "error")): 1 span matched
      payments/Gateway.Bank.Authorize in trace web/Orders.Create{order.id="A-1001"}
    wrote testdata/repros/orders-1.praytest.star
```

The repro file is the canonical text of the minimal case. It is a normal
test file. It needs no generator and no `load`:

```python
# praytel test format v0
# Shrunk from property "orders" (orders.praytest.star:21), seed 7, after 12 runs.
case(name = "orders card=gift qty=4",
     command = ["shop", "-pace", "0s", "-card", "gift", "-qty", "4"])
every(span(name = "Orders.Place"), has = exactly(1, span(name = "Gateway.Charge")))
never(span(service = "payments") >> span(service = "inventory"))
never(span(name = "Gateway.Bank.Authorize") & longer_than("200ms"))
some(span(name = "Gateway.Charge", status = "error"))
some(log(body = contains("authorization refused")))
```

Praytel writes the file only with `--repro-dir=DIR`. It does not overwrite a
file.

### Seeds

`seed = 0` (the default) tells rapid to use a random seed. The report shows
the seed. `praytel test --seed=N` and `property(..., seed = N)` use a fixed
seed. The same seed gives the same examples. The same results also need a
program that does not change its behaviour from run to run.

### Constraints of rapid

A probe with rapid v1.3.0 found these constraints. Each item has a decision.

| # | Constraint | Decision |
|---|---|---|
| C1 | `rapid.Check` calls `testing.Short()`. Outside a test binary, this panics: `testing: Short called before Init`. | Praytel calls `testing.Init()` and `flag.CommandLine.Parse(nil)` one time. Praytel parses its own flags with a separate `FlagSet` (`args.go`). Thus there is no conflict. |
| C2 | The settings are global flags on `flag.CommandLine`: `rapid.checks`, `rapid.seed`, `rapid.shrinktime`, `rapid.nofailfile`. | Praytel sets each flag with `flag.Set` before each property. Properties run one at a time. This is a non-goal of trace-based testing (no parallel runs). |
| C3 | The environment variables `RAPID_*` change the defaults. | Praytel sets each flag. Thus the environment does not change a run. |
| C4 | The deadline comes from `*testing.T` only. | Refer to "Budget". |
| C5 | Shrinking makes many calls for each value. | Refer to "Verdict cache". |
| C6 | The shrinker compares tracebacks only. | Refer to "Target check". |
| C7 | rapid writes fail files to `testdata/rapid/NAME/*.fail`. The files are binary and are for one rapid version. | Praytel sets `rapid.nofailfile`. It writes a repro file in canonical text. |
| C8 | `-test.short` divides the number of checks by 5. | Praytel does not set `test.short`. |
| C9 | rapid imports `testing`. The praytel binary then contains it. | Measure the size of the binary in Phase S6. |
| C10 | The licence of rapid is MPL-2.0. | Open question Q4. |

## Scenario 6: the TUI as a REPL

The key `:` opens a prompt. Praytel evaluates each line as Starlark against
the projection of the run. The type of the result tells praytel what to do.

| Input | Type | Effect |
|---|---|---|
| `span(service = "payments", status = "error")` | Selection | A `where` lens on the current pane. The footer shows the count. |
| `span(name = "Orders.Place") >> span(name = "Gateway.Bank.Authorize") & longer_than("200ms")` | Selection | The same, for a structural selection. |
| `never(span(service = "payments") >> span(service = "inventory"))` | Check | Praytel evaluates it now, for each trace. It shows pass, fail or incomplete and the items that failed. |
| `table(span(name = "Gateway.Charge"), columns = ["service", "attrs.card.type", "duration"])` | Output | A new pane. |
| `slow = longer_than("200ms")` | Assignment | The name is available for the session. |

Other functions:

- `:w FILE.praytest.star` writes the assignments and checks of the session
  as a file. The command of the run becomes a `case`. The writer refuses
  IDs, timestamps and values on the volatile list. It is the writer of
  `--record` in the trace-based testing design.
- The prompt completes builder names, matcher names and the attribute keys
  in the projection.
- An error shows in the footer, with its column, as for the `/` search now.
- The `/` search also accepts Starlark. Fuzzy search does not change.
- A check that you pin as a pane runs again only when a trace settles or
  gets a new revision. It does not run on each tick (refer to the Phase 8
  log).

## Limitations

- **The REPL is not a data REPL.** Starlark does not see spans. Thus
  `for s in spans: print(s.duration)` is not possible. Use `table(...)` and
  counts to see values.
- **No arbitrary predicates.** A comparison of two fields of two spans
  needs `var`. An aggregate, for example "the sum of the child durations",
  is not possible. The section "Items that we do not do now" of
  [Pipeline](PIPELINE.md) still applies.
- **`and`, `or` and `not`.** Starlark gives no error for a selection. The
  prompt and `-e` refuse them. A file does not.
- **The TUI search is longer.** `span(status = "error")` has more characters
  than `{ span:status = error }`. Completion decreases this cost.
- **No TraceQL text.** A user cannot paste a Tempo query.
- **Shrinking reaches generated inputs only.** Ports, time and the random
  values of the program are not drawn. Thus shrinking cannot reduce them.
- **Properties are slow.** Each example is a process run and a drain. Use
  tens of examples, not thousands.
- **Large integers.** An integer attribute and a number compare as
  `float64`. Values above 2^53 can compare equal. This does not change.

## Relation to trace-based testing

The trace-based testing design (`docs/features/trace-based-testing` on the
branch `worktree-feature-pipeline-to-amp`) uses
imperative checks: `test(name, command, check = fn)` and `expect.*` in a
Starlark function. This design uses declarative checks. Each `expect.*` has
a declarative form:

| Trace-based testing | This design |
|---|---|
| `expect.exists(run, sel, count = 1)` | `exactly(1, sel)` |
| `expect.absent(run, sel)` | `never(sel)` |
| `expect.never(run, BANK, lambda s: s.duration_ms > 200)` | `never(BANK & longer_than("200ms"))` |
| `expect.always(run, CHARGE, lambda s: "card.type" in s.attrs)` | `always(CHARGE, span(attrs = {"card.type": exists()}))` |
| `run.one(sel)`, then `expect.eq(span.status, "error")` | `exactly(1, sel & span(status = "error"))`, with near misses |
| X1: `s.parent.service == "web"` | `never(span(service = "payments").child_of(span(service = "web")))` |

These parts of trace-based testing stay: `praytel test`, the file suffix, the
input rules, the exit code table, `--record` and `env()`.

These decisions of trace-based testing need a new decision: D4 (Starlark
boundary), D6 (check execution and failures) and D7 (selector and assertion
semantics). The vision also changes:

- §6, row P5, "reject shrinking": this design adopts shrinking.
- §7, non-goal "no input generation or shrinking": this design removes it.

## Plan

Each phase keeps the golden files and the benchmarks, as in
[Pipeline](PIPELINE.md). Each phase records its checks, departures and notes
in a log, as in [Pipeline log](PIPELINE-LOG.md).

| Phase | Work | Checks |
|---|---|---|
| S1 | Canonical text: `Format`, `ParseSelection`, plan stores Starlark text, callers use `ParseSelection` | Fuzz: random trees round-trip. Plan goldens regenerated one time. `--print-plan` output evaluates in `-e`. |
| S2 | Builder parity: the matchers, `resource`, `count`. Strings are no longer queries. Remove `lex.go`, `parse.go`, `format.go` | Each query of the Phase 7 and Phase 9 tests has a builder form with the same result. Filter benchmarks do not change. |
| S3 | Structure and lens: `<<`, `before`, `within`, `between`, `where(context =)`, connection to the nearest kept ancestor | A golden of part of a `shop` run. A new span between two kept spans does not change it. |
| S4 | Near misses | Golden tests of failure output. Each failure about a span has a near miss or "no candidate". |
| S5 | Cases: `case`, check scope, shared checks, runner of trace-based testing | The trace-based testing scenarios S1 to S5 in declarative form. |
| S6 | Properties: `gen.*`, `property`, rapid, verdict cache, target check, `confirm`, budget, repro file | The probe of "Verdict cache" as a test with a fake command. `shop -card` and `-qty` for real. Binary size before and after. |
| S7 | REPL: `:` prompt, typed results, `:w`, completion, refusal of `and`, `or`, `not` | TUI tests for each result type. `:w` output passes `praytel test`. |

S1 to S4 do not need trace-based testing. S5 and S6 need its runner. S7
needs S1 only, and `:w` needs S5.

## Open questions

- **Q1.** The scope of a check comes from the order of statements. The
  alternative is `case(..., checks = fn)`. The order is shorter in a loop.
  A function is clearer in a large file. Which do we use?
- **Q2.** Do we keep a converter from TraceQL text to Starlark, for example
  `praytel query convert`? It needs a parser, but only for import.
- **Q3.** What are the defaults for `examples` and `budget`? This design
  uses 30 and `5m`.
- **Q4.** rapid is MPL-2.0. The repository has no licence file. MPL-2.0
  applies to the files of rapid only, and praytel does not change them. Is
  this dependency acceptable?
- **Q5.** Scenario 3 needs a `-qty` flag in `shop`. Trace-based testing
  adds `-card` (its assumption A5). Is `-qty` acceptable too?
- **Q6.** rapid has state machine tests (`T.Repeat`). A property could
  generate a sequence of commands for a long-running server. Do we want
  this later?
