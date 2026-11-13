---
title: Starlark queries
summary: A proposed alternative to the query text of Phases 7 to 9. Starlark builders are the only query language. Go keeps the query tree and evaluates it. Cases, properties with an own shrinking engine, and a TUI prompt use the same language.
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
5. A property test that shrinks its input to a minimal example. Praytel
   has its own small engine for this. The design compares it with
   [rapid](https://pkg.go.dev/pgregory.net/rapid) and gopter.
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
| Choice sequence | The list of integer choices that the draws of one example made. Replay of the list gives the same example. |
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
5. Generators get their values from the choice sequence only. A script has
   no other source of random values.
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
| New: `internal/testrun` | None | Cases, properties, the shrinking engine |

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

## Scenario 5: shrinking

Praytel has its own small engine for generation and shrinking. The engine
uses a choice sequence, as Hypothesis (Python) does. The section
"Alternatives considered" compares it with rapid, gopter and other options.

### Why an own engine

A praytel example is a process run and a drain. It takes 0.5 s to 2 s.
Libraries for property tests expect calls that take microseconds, in
`go test`. Thus praytel needs an engine with these properties:

- It makes few calls during shrinking.
- It asks praytel if a candidate fails the target check. It does not
  compare Go tracebacks or error text.
- It stops at praytel's time budget.
- It has no global settings and needs no `testing` package.
- It shrinks through `gen.map`, `gen.filter` and nested generators.

The engine is small, because the generators are a closed set that praytel
defines.

### Choice sequence

Each draw makes one or more choices. A choice is an integer from 0 to a
maximum. The engine records each choice and its maximum. The list of
choices is the choice sequence of the example.

- To generate, the engine gets each choice from a random source
  (`math/rand/v2`, PCG, with the seed).
- To replay, the engine gets each choice from a recorded sequence. If a
  generator asks for more choices than the sequence has, the engine gives 0.
  If a choice is more than its new maximum, the engine gives the maximum.
- Each generator call marks its span: the first and the last choice that it
  made. The shrinker uses the spans to delete list elements.

Choice 0 is the simplest value of each generator. Thus a smaller sequence
gives a simpler example.

### Generators

| Starlark | Choices | Choice 0 gives |
|---|---|---|
| `gen.ints(min, max)` | One choice. The value is the choice in zigzag order from the value nearest to 0 in the range | The value nearest to 0 |
| `gen.bools()` | One choice, 0 or 1 | `False` |
| `gen.one_of(values)` | One choice: the index | The first value |
| `gen.just(v)` | None | `v` |
| `gen.strings(alphabet =, min_len =, max_len =)` | A list of `one_of(alphabet)` | The shortest string of the first character |
| `gen.lists(g, min_len =, max_len =)` | After `min_len` elements, one choice before each element: 0 stops the list | The shortest list |
| `gen.permutations(values)` | One index choice for each position | The values in their order |
| `gen.choice(g1, g2, ...)` | One choice for the generator, then its choices | A value of `g1` |
| `gen.map(g, fn)` | The choices of `g`. `fn` is Starlark | `fn` of the simplest value of `g` |
| `gen.filter(g, fn)` | The choices of `g`, a maximum of 100 tries | The simplest value that `fn` accepts |

`gen.map` and `gen.filter` call Starlark when the engine draws a value. They
do not see events. Rule 2 stays true. A filter that rejects 100 tries makes
the example invalid. Invalid examples do not count.

Later: `gen.floats` and `gen.strings(regex)`.

### Data flow

1. The script calls `property(name, fn, examples =, seed =, budget =,
   confirm =, **generators)`. Praytel stores the generators as values.
2. For each example, the engine:
   1. Draws one value for each parameter of `fn`, in parameter order.
   2. Calls `fn` on a new Starlark thread with the step cap.
   3. Gets the case and its checks. It writes the canonical text of the case.
   4. Finds the verdict in the verdict cache. If the verdict is not there,
      it runs the case and adds the verdict to the cache.
3. The first failing example sets the target check. The engine then shrinks.
4. The engine keeps the drawn values, the case and the choice sequence of
   the minimal example.
5. Praytel writes the report and the repro file.

### Shrink passes

A candidate is a changed choice sequence. The engine accepts a candidate
only if both of these are true:

- The candidate is smaller in shortlex order: a shorter sequence, or a
  sequence of the same length with a lower first different choice.
- The case of the candidate fails the target check.

Each accepted candidate is smaller. Thus shrinking always stops.

The engine applies these passes in this order. It repeats them until no pass
finds a smaller candidate, or until the budget is used:

1. Delete spans. Delete the span of each list element, the largest first.
2. Set to zero. Set each choice to 0.
3. Lower. Find the lowest failing value of each choice with a binary search.

The Hypothesis reducer has more passes, for example "sort" and
"redistribute" (MacIver and Donaldson, "Test-case reduction via test-case
generation", ECOOP 2020). Add a pass only if a real property needs it.

### Verdict cache

Different candidates can make the same case. Example: a choice that is more
than its new maximum. The engine must not run the same case two times. The
key of the cache is the canonical text of the case: the command, the
environment and the checks. Two candidates that make the same case share a
verdict. This is possible only because a case is data.

### Target check

The first failing example sets the target check. If many checks fail, the
target is the first in file order. During shrinking, a candidate that fails
a different check is not accepted. Thus shrinking cannot go from a failure
of check A to a failure of check B.

The report shows the other failed checks of the minimal example also.

### Flaky and incomplete runs

A run of an external process can fail for a cause that is not the input.

- A new failing case runs `confirm` times (default 2). The engine records a
  failure only if all runs fail the target check. If not, the engine records
  "flaky", and the candidate is not accepted. The report shows the number of
  flaky examples.
- An incomplete case (refer to [Pipeline](PIPELINE.md#trace-state)) is not
  a failure. Thus shrinking does not go to a case with a slow drain. The
  report shows the number of incomplete examples. If no example was
  complete, the property result is incomplete (exit 3).

### Budget

`budget` (default `5m`) covers generation and shrinking of one property. The
engine examines the budget before each run. When the budget is used, the
engine stops and keeps the smallest failing example. The report then says
that the example is possibly not minimal.

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
# choices: [2, 3]
case(name = "orders card=gift qty=4",
     command = ["shop", "-pace", "0s", "-card", "gift", "-qty", "4"])
every(span(name = "Orders.Place"), has = exactly(1, span(name = "Gateway.Charge")))
never(span(service = "payments") >> span(service = "inventory"))
never(span(name = "Gateway.Bank.Authorize") & longer_than("200ms"))
some(span(name = "Gateway.Charge", status = "error"))
some(log(body = contains("authorization refused")))
```

The `choices` comment keeps the choice sequence. A later version can use it
to shrink again after a change to the property.

Praytel writes the file only with `--repro-dir=DIR`. It does not overwrite a
file.

### Seeds

`seed = 0` (the default) tells the engine to use a random seed. The report
shows the seed. `praytel test --seed=N` and `property(..., seed = N)` use a
fixed seed. The same seed and the same script give the same examples. The
same results also need a program that does not change its behaviour from run
to run.

## Alternatives considered

### Evidence

Probes compared the engines on one property. The property fails when
`card == "gift"` and `qty > 3`, with `card` from three values and `qty` from
1 to 50. Each engine used the seed 7 and 30 examples. A "run" is a call with
new values. Each run is a process run in praytel.

| Engine | Calls | Runs | First example | Minimal example |
|---|---|---|---|---|
| rapid v1.3.0 | 1 735 | 12 (with a verdict cache) | `gift`, `50` | `gift`, `4` |
| rapid v1.3.0, 20 ms for each call, no cache | 201 | 201 | `gift`, `50` | `gift`, `50` (not shrunk) |
| gopter v0.2.11 | 9 | 7 | `gift`, `21` | `gift`, `4` |
| Choice-sequence prototype (about 100 lines, passes 2 and 3 only) | 16 | 12 | `gift`, `14` | `gift`, `4` |

A property with one generator for each parameter is easy for each engine.
The table does not measure lists, nested generators or `gen.map`.

### Comparison

| | Own engine | rapid v1.3.0 | gopter v0.2.11 | `testing/quick` | Go fuzzing (`testing.F`) |
|---|---|---|---|---|---|
| Shrinking | Choice sequence | Choice sequence, as bit blocks | A shrinker for each value type | None | Byte minimisation |
| Shrinks through `map` and nested generators | Yes | Yes | No: `Map`, `FlatMap` and `CombineGens` drop the shrinker | No | Not applicable |
| Calls in the probe | 16 | 1 735 | 9 | Not applicable | Not applicable |
| Runs outside `go test` | Yes | Only after `testing.Init()` and `flag.CommandLine.Parse(nil)` | Yes: `Properties.Run(reporter)` | Yes | No |
| Settings | Arguments | Global flags and `RAPID_*` variables | A `TestParameters` value for each run | Arguments | Flags of `go test` |
| Identity of a failure | The target check | The Go traceback and the error text | A boolean result | A boolean result | A crash |
| Time budget | Before each run | `rapid.shrinktime`; the deadline comes from `*testing.T` only | `MaxShrinkCount` only | Not applicable | `-fuzztime` |
| Saved failures | Repro file in canonical text | Binary fail files for one rapid version | None | None | Corpus files |
| Last release | Not applicable | 2026-03-30 | 2024-04-03 (module `go 1.12`) | Frozen (standard library) | Standard library |
| Licence | Praytel | MPL-2.0 | MIT | BSD | BSD |
| Cost to praytel | An engine (estimate: 500 to 800 lines with tests) | About 200 lines of adapter code, and the workarounds in "Why not rapid" | An adapter, and no shrinking through `gen.map` | No shrinking | Wrong model |

### Why not rapid

rapid is a good library for `go test`. Its design does not fit a slow,
external, flaky example:

- `rapid.Check` calls `testing.Short()`. Outside a test binary, this panics:
  `testing: Short called before Init`.
- The settings are global flags on `flag.CommandLine` and `RAPID_*`
  environment variables.
- The deadline comes from `*testing.T` only. With another `TB`, it is 24
  hours.
- It shrinks bit blocks. It makes many calls for each value: 1 735 calls in
  the probe. With 20 ms for each call and no cache, it did not shrink. A
  time limit of 200 ms or 2 s gave the same 201 calls and 4.2 s.
- Its shrinker identifies a failure by the Go traceback. Praytel reports each
  failure from one Go call site. Thus shrinking can go from check A to check
  B. Its reproduction step also compares the error text.
- It writes binary fail files to `testdata/rapid/`.

Each item has a workaround. Together, the workarounds are larger than the
part of rapid that praytel uses.

### Why not gopter

gopter runs outside `go test` and has a settings value for each run. Its
problems:

- `Map`, `FlatMap` and `CombineGens` drop the shrinker. A user makes most
  composite inputs with `gen.map`. Thus most composite inputs do not shrink.
- It shrinks each argument separately. It cannot delete one element from a
  structure that another generator made.
- The API uses reflection and `interface{}`.
- The last release is from April 2024.

### Later: reduction of recorded inputs

Shrinking needs generators. A recorded failure with a large input file has
none. Delta debugging (`ddmin`, Zeller and Hildebrandt, 2002) can reduce the
lines of such a file or the arguments of a command. It uses the same target
check, verdict cache and budget. This is a possible later function, for
example `praytel test --reduce=FILE`.

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
| S6 | Properties: choice sequence, `gen.*`, `property`, shrink passes, verdict cache, target check, `confirm`, budget, repro file | The probe of "Alternatives considered" as a test with a fake command: minimal example `gift`, `4` in 16 calls or fewer. A list property shrinks to one element. A `gen.map` value shrinks. A flaky fake command gives no failure. `shop -card` and `-qty` for real. |
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
- **Q4.** This design writes an own engine (estimate: 500 to 800 lines with
  tests). The alternative is gopter, with no shrinking through `gen.map`.
  Is the own engine acceptable? Refer to "Alternatives considered".
- **Q5.** Scenario 3 needs a `-qty` flag in `shop`. Trace-based testing
  adds `-card` (its assumption A5). Is `-qty` acceptable too?
- **Q6.** A property could generate a sequence of commands for a
  long-running server, as the state machine tests of rapid (`T.Repeat`) do.
  The choice sequence supports this. Do we want it later?
