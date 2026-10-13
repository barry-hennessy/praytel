---
title: Examples
summary: One small instrumented app and a command to run against it for each release, so every guide can be tried without instrumenting anything first.
weight: 7
---

# Examples

The guides use `./server` to stand for your own app. To try a guide before you
have instrumented anything, use `shop`: a small Go program in
[`demo/workloads/shop`](../demo/workloads/shop) that places four orders across
four in-process services (`web`, `checkout`, `inventory`, `payments`). It
exports OTLP traces and writes JSON logs tagged with trace and span IDs. It is
the same app the recordings in the guides are made from.

Two of its orders are there to be found:

| Order | Card | What happens |
|---|---|---|
| A-1002 | `amex` | `Gateway.Bank.Authorize` takes 450ms and logs a retry. |
| A-1003 | `gift` | `Gateway.Bank.Authorize` refuses the card; the error travels up to `Orders.Create`. |

## Set up

From the repository root, with Go installed:

```sh
go install ./cmd/praytel
(cd demo/workloads/shop && go install .)
```

Both binaries land in `$(go env GOPATH)/bin`. `shop` also runs without praytel,
exporting nothing, so you can compare the two.

There is one example per release, in [release order](releases.md). Each uses
only what that release and the ones before it provide.

## 1. Traces and logs

### Traces

```sh
praytel --tui -- shop
```

Four traces fill in, one per order, each rooted at `Orders.Create`. Type
`gift` to narrow to the order that failed, and follow the `×` marks down to
`Gateway.Bank.Authorize`. Esc clears the search; Ctrl-C quits.

Guide: [View traces from a running app](guides/traces.md).

### Logs

First see what the app prints on its own, then the same lines joined:

```sh
shop -pace 50ms | head -4
praytel -- shop -pace 50ms
```

In the joined stream each line gains the name of the span that emitted it:

```
stdout Gateway.Bank.Authorize 4a2aac9c... 18e4b9ff... {"level":"ERROR","msg":"authorization refused", ...}
```

`shop: taking orders` carries no IDs, so it is reported as an orphan. Under
`praytel --tui -- shop`, press Tab and type `refused` to search log bodies.

Guide: [View logs correlated to traces](guides/logs.md).

## 2. Overview

```sh
praytel --tui -- shop
```

Press Ctrl-G. There is one box per span name, labelled with its service:
`web.Orders.Create` calls `checkout.Orders.Place`, which fans out to
`inventory.Stock.Reserve`, `payments.Gateway.Charge` and
`checkout.Orders.Repo.Save`. Compare it with the calls in
[`main.go`](../demo/workloads/shop/main.go). The arrow into
`checkout.Orders.Repo.Save` reads `×3`, not `×4`: one order never saved. Type
`gift` and the graph shrinks to what that order touched, with
`inventory.Stock.Release` in place of the save.

Guide: [Get an overview of a system](guides/overview.md).

## 3. Tests

The workload has three tests; `TestGiftCardOrderIsConfirmed` fails on purpose.

```sh
cd demo/workloads/shop
praytel --tui -- go test -count=1 .
```

Each test is its own trace, named after the test. Type `TestGift`, then follow
the `×` marks: the test, `Orders.Create`, `Orders.Place`, `Gateway.Charge`,
and last `Gateway.Bank.Authorize`, with `authorization refused` logged beneath
it. That span is where the test failed. Praytel exits 1, as `go test` did.

To cut the run down to the payment path:

```sh
praytel --tui --focus 'Gateway*' -- go test -count=1 .
```

Guide: [View traces of tests](guides/tests.md).

## 4. Performance

```sh
praytel --tui --query 'span{ span:duration > 300ms }' -- shop
```

Only the `amex` order is left. Without `--tui` the same query prints one line
of evidence per matching trace, which is the form a script can use:

```sh
praytel --query 'span{ span:duration > 300ms } | matches' -- shop -pace 50ms
```

```
05b66746... revision=25 state=final spans=[4adbc203... 8cfe1c03... ad8c1747... b7315de3...] logs=[] incomplete=[]
```

Four spans match because the slow bank call makes each of its ancestors slow
too. To get the cause rather than everything above it, ask for the slow span
that logged a retry:

```sh
praytel --query 'span{ span:duration > 100ms } > log{ log:body *= "retry" }' -- shop -pace 50ms
```

Guide: [Focus on performance](guides/performance.md).

## 5. Agents

These are the command-line forms of what an agent does through `start_run`.
Build the test binary once so the queries run without recompiling:

```sh
cd demo/workloads/shop
go test -c -o shop.test .
```

Focus on the area of change: did anything in `payments` fail?

```sh
praytel -q 'span{ resource.service.name = "payments" && span:status = error } | matches' -- ./shop.test
```

One trace comes back, with two span IDs: `Gateway.Charge` and
`Gateway.Bank.Authorize`. Praytel exits 1 because the suite did; the exit code
is the child's, not a verdict on the query.

Check the work is real: did placing an order actually reach the repository?

```sh
praytel -q 'span{ span:name = "Orders.Place" } > span{ span:name = "Orders.Repo.Save" && span:kind = client } | matches' -- ./shop.test
```

Two traces match, not three. The gift-card order never saved, which is correct:
its payment was refused first.

A performance budget is the same shape. This prints nothing, so the budget
held:

```sh
praytel -q 'span{ span:name = "Orders.Create" && span:duration > 2s }' -- shop -pace 50ms
```

To hand the same loop to an agent, print the playbook and connect the MCP
server:

```sh
praytel agent instructions
```

Guide: [Using praytel with agents](guides/agents.md).
