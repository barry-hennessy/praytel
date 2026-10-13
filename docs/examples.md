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

There is one example per release, in release order. Each uses
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
