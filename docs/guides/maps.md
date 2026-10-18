---
title: Keep an architecture map
summary: How to stamp the component graph of a test run into a reviewable file, and see in every pull request how the architecture changed.
weight: 65
---

# Keep an architecture map

An architecture diagram is right the day it is drawn. After that, the code
moves and the diagram doesn't. Nobody notices, because nothing checks it.

Meanwhile, the change that matters most in a pull request is often the
hardest one to see. A new call from `checkout` straight into the payments
database is one line in a forty-file diff. An agent that "simplified" a
service by bypassing its repository layer reads the same as one that didn't.

An architecture map fixes both. Praytel already draws the
[graph of components](overview.md) from what actually ran. `--map` writes that
graph down as plain text files, one per entry point, next to your code. You
commit them like any other golden file. From then on:

- **the map can't rot**, because it is regenerated from the tests, and CI fails
  when the committed map no longer matches what the code does;
- **architecture changes become visible**, because they show up in the pull
  request as a few added or removed lines, and `praytel map` can draw them;
- **intended changes are cheap**: rerun, commit the new map, and the reviewer
  sees exactly what moved.

## Stamp a map

Run your tests under praytel with `--map` and a directory:

```sh
praytel --map arch -- go test -count=1 ./...
```

Praytel runs the tests as usual and, once they pass, writes one file per
entry point: a trace's root span. If you
[start a span per test](tests.md#what-the-tests-need), every test is an entry
point:

```
arch/
  tests/
    TestAmexOrderIsConfirmed.map
    TestVisaOrderIsConfirmed.map
```

Each file lists the components that entry point reached and the calls between
them, sorted, one per line:

```
praytel-map v1
entry tests TestVisaOrderIsConfirmed

node checkout Orders.Place
node checkout Orders.Repo.Save
node inventory Stock.Repo.Lock
node inventory Stock.Reserve
node payments Gateway.Bank.Authorize
node payments Gateway.Charge
node tests TestVisaOrderIsConfirmed
node web Orders.Create

call checkout Orders.Place -> checkout Orders.Repo.Save
call checkout Orders.Place -> inventory Stock.Reserve
call checkout Orders.Place -> payments Gateway.Charge
call inventory Stock.Reserve -> inventory Stock.Repo.Lock
call payments Gateway.Charge -> payments Gateway.Bank.Authorize
call tests TestVisaOrderIsConfirmed -> web Orders.Create
call web Orders.Create -> checkout Orders.Place
```

A component is a span name within a service (its `service.name`), exactly as
in the graph view. A call means a span of one component was the parent of a
span of another.

The file deliberately leaves out everything that varies from run to run:
there are no counts, timings, IDs or ordering by arrival. Run the same tests
twice and you get the same bytes, whatever the scheduler did. That is what
makes the map safe to commit and cheap to review.

## See it

`praytel map` draws a map. It reads files, not telemetry, so it runs nothing:

```sh
praytel map arch                                  # every entry point
praytel map arch/tests/TestVisaOrderIsConfirmed.map
```

```
                                                                                                    ╭───────────────────────────╮
                                                                                                    │ checkout.Orders.Repo.Save │
                                                                                                ╭──▶│                           │
 ╭────────────────────────────────╮                                                             │   ╰───────────────────────────╯
 │ tests.TestAmexOrderIsConfirmed │                                                             │
 │ entry point                    │──╮     ╭───────────────────╮     ╭───────────────────────╮  │   ╭───────────────────────────╮     ╭─────────────────────────────────╮
 ╰────────────────────────────────╯  │     │ web.Orders.Create │     │ checkout.Orders.Place │  │   │ inventory.Stock.Reserve   │     │ inventory.Stock.Repo.Lock       │
                                     ╰────▶│                   │────▶│                       │──┼──▶│                           │────▶│                                 │
 ╭────────────────────────────────╮    ╭──▶│                   │     ╰───────────────────────╯  │   ╰───────────────────────────╯     ╰─────────────────────────────────╯
 │ tests.TestVisaOrderIsConfirmed │    │   ╰───────────────────╯                                │
 │ entry point                    │────╯                                                        │   ╭───────────────────────────╮     ╭─────────────────────────────────╮
 ╰────────────────────────────────╯                                                             │   │ payments.Gateway.Charge   │     │ payments.Gateway.Bank.Authorize │
                                                                                                ╰──▶│                           │────▶│                                 │
                                                                                                    ╰───────────────────────────╯     ╰─────────────────────────────────╯
```

## See what changed

Give `praytel map` two maps, and it draws the difference: both versions laid
out together, with each component and call marked by what happened to it.

```sh
praytel map old/arch arch
```

Here someone has dropped the stock reservation from placing an order, and
added a fraud check:

```
~ entry tests TestAmexOrderIsConfirmed
~ entry tests TestVisaOrderIsConfirmed
- node inventory Stock.Repo.Lock
- node inventory Stock.Reserve
+ node payments Fraud.Score
- call checkout Orders.Place -> inventory Stock.Reserve
+ call checkout Orders.Place -> payments Fraud.Score
- call inventory Stock.Reserve -> inventory Stock.Repo.Lock

                                                            ╭───────────────────────────╮
                                                            │ checkout.Orders.Repo.Save │
                                                        ╭──▶│                           │
                                                        │   ╰───────────────────────────╯
                                                        │
                                                        │   ┌┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┐     ┌┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┐
                                                        │   ┊ - inventory.Stock.Reserve ┊     ┊ - inventory.Stock.Repo.Lock ┊
                                                        ├┈┈▶┊ removed                   ┊┈┈┈┈▶┊ removed                     ┊
 ╭───────────────────╮     ╭─────────────────────────╮  │   └┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┘     └┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┘
 │ web.Orders.Create │     │ ~ checkout.Orders.Place │  │
 │                   │────▶│ calls changed           │──┤   ╔═══════════════════════════╗
 ╰───────────────────╯     ╰─────────────────────────╯  │   ║ + payments.Fraud.Score    ║
                                                        ├══▶║ added                     ║
                                                        │   ╚═══════════════════════════╝
                                                        │
                                                        │   ╭───────────────────────────╮
                                                        │   │ payments.Gateway.Charge   │
                                                        ╰──▶│                           │
                                                            ╰───────────────────────────╯

3 unchanged components not shown.
```

Every change is listed first as text, so it reads the same in a CI log, an
email or a terminal without colour. The drawing marks the same changes with
borders and line styles, so it reads without colour too:

| You see | It means |
|---|---|
| `+` and a double border `╔═╗`, double line `═` | added |
| `-` and a dotted border `┌┈┐`, dotted line `┈` | removed |
| `~` and a normal border | still there, but a call to or from it changed |
| a normal border | unchanged, shown for context |

Only the changes and their immediate neighbours are drawn. The rest of the
system is counted, not shown, so a two-line change makes a small picture
however big the system is. To see everything, draw the new map on its own.

`--format mermaid` prints the same diagram as a Mermaid flowchart, which
GitHub and GitLab render in comments and Markdown files.

`praytel map OLD NEW` exits like `diff`: `0` when the maps are the same, `1`
when they differ.

## A workflow

### While you work

Re-stamp the map after any change you think might move the architecture, or
just as part of running the tests:

```sh
praytel --map arch -- go test -count=1 ./...
```

When a file changes, praytel tells you on stderr, with the lines that
changed:

```
map: arch/tests/TestVisaOrderIsConfirmed.map changed
~ entry tests TestVisaOrderIsConfirmed
+ node payments Fraud.Score
+ call checkout Orders.Place -> payments Fraud.Score
```

That is the moment to ask whether you meant it. If you did, commit the map
with the code. If you didn't, you have found it before the reviewer did.

You can stamp just the tests you are working on. Praytel only writes the files
for entry points that ran, and leaves the rest alone:

```sh
praytel --map arch -- go test -count=1 -run TestVisa ./...
```

### In review

The map's diff is part of the pull request. A reviewer reads
`+ call checkout Orders.Place -> payments Fraud.Score` and knows, without
reading a line of code, that placing an order now calls the fraud check. Changes to `arch/`
are a natural place for a `CODEOWNERS` entry, so the people who care about the
architecture are asked when it changes and left alone when it doesn't.

For the picture, have CI post `praytel map` output as a comment. See
[In CI](#in-ci).

### Working with agents

An agent's change summary tells you what the agent believes it did. The map
tells you what the code does. Ask the agent to run the tests with `--map`,
and read the map's diff before you read its code: a call that should not exist
is quicker to spot there than anywhere else. See
[Using praytel with agents](agents.md).

## In CI

Two jobs, each with one question. Praytel's own repository runs both for its
demo shop: see [`maps.yml`](../../.github/workflows/maps.yml) and the `map`
targets in the [Makefile](../../Makefile).

**Is the committed map current?** Stamp the full suite from scratch, then ask
git whether anything changed. Removing the directory first means a map file
whose entry point no longer runs, such as a deleted test, shows up as deleted.

```sh
rm -rf arch
praytel --map arch -- go test -count=1 ./...
git add --intent-to-add arch         # so new files show in the diff too
git diff --exit-code arch
```

**What did this pull request change?** This job runs no tests. It compares the
committed map on the base branch with the one in the pull request, so it is
fast and its result can't be flaky:

```sh
mkdir -p old && git archive "origin/$BASE" arch | tar -x -C old || true
mkdir -p old/arch
praytel map --format mermaid old/arch arch > map-diff.md || true
```

Post `map-diff.md` as a pull request comment with whatever your forge
provides. A GitHub Actions version of both jobs:

```yaml
jobs:
  map-current:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go install github.com/barry-hennessy/praytel/cmd/praytel@latest
      - run: rm -rf arch && praytel --map arch -- go test -count=1 ./...
      - run: git add -N arch && git diff --exit-code arch

  map-diff:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
      - run: go install github.com/barry-hennessy/praytel/cmd/praytel@latest
      - run: |
          mkdir -p old/arch
          git archive "origin/${{ github.base_ref }}" arch | tar -x -C old || true
          { echo '### Architecture changes'; echo '```mermaid';
            praytel map --format mermaid old/arch arch; echo '```'; } > map-diff.md || true
      - uses: marocchino/sticky-pull-request-comment@v2
        with: { path: map-diff.md }
```

If your suite runs in shards, give every shard its own `--map` directory and
copy them together before the `git diff`. Each test writes only its own file,
so the shards never touch the same one.

## When praytel won't write a map

A map is only worth committing if it is the whole truth about the run that
made it. Praytel would rather write nothing than write a map that is missing
parts, so it leaves the directory untouched, says why on stderr, and exits
non-zero when:

| Reason | Why it matters |
|---|---|
| The command failed. | A failing test takes error paths that are not your architecture. Fix it, or leave it out with `-run`/`-skip`. |
| No traces arrived. | Usually cached test results (add `-count=1`) or an exporter that isn't set up. |
| Drain hit `--drain-timeout`. | Spans may still have been on their way. Flush the exporter before exit, or raise the timeout. |
| Data was shed or evicted. | The run exceeded `--max-spans` or `--max-log-lines`. |
| A span names a parent that never arrived. | Part of a trace is missing: an exporter that didn't flush, or trace context from outside the run, such as a `TRACEPARENT` set by your CI. |

## Getting a good map

The map is only as stable as your span names, the same as the graph view.

- **Name spans after operations, not data.** `GET /orders/{id}`, not
  `GET /orders/1234`. A name with an ID in it is a new component every run.
- **Start a span per test.** Each test then gets its own file. Running one
  test checks that test's map exactly, and the map's diff says which tests'
  paths changed.
- **Keep tests deterministic.** A call that happens only sometimes, such as a
  retry, a cache miss or a race, appears and disappears from the map, and CI
  will report it every time it does. That is usually worth knowing about.

If several tests share one root span name, they share a file, and running
only some of them rewrites that file with only their part. Run them all when
you stamp, or give each test its own root span.

## Next

[Typed queries](queries.md).
