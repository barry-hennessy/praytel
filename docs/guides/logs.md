---
title: View logs correlated to traces
summary: How praytel attaches each log line to the span that produced it, and how to make your logs joinable.
weight: 20
---

# View logs correlated to traces

A trace tells you what components call eachother, and where time was spent.
A log line tells you what the code thought was happening.

Praytel joins the two, so each log line sits under the span that emitted it.
You can focus straight in on a component and see what _it_ logged, seperate
from any other.

## Run it

[![Recording: an app's JSON logs shown raw, then under praytel with each line beneath the span that emitted it, then a search of log bodies](../assets/logs.gif)](../assets/logs.mp4)

```sh
praytel -- ./server           # append-only stream
praytel --tui -- ./server     # logs nested in the span tree
```

## Try it yourself

### The example 'shop'
With the example `shop` app [installed](traces.md#the-shop-app), first see what it
prints on its own, then the same lines joined:

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


### Node.js style
To see the same join with a logger you might use yourself, take
OpenTelemetry's [getting-started dice app for Node.js](https://opentelemetry.io/docs/languages/js/getting-started/nodejs/)
and log with [pino](https://getpino.io). In an empty directory:

```sh
npm init -y
npm install express pino @opentelemetry/api @opentelemetry/auto-instrumentations-node
```

Save this as `app.js`:

```js
const express = require('express');
const logger = require('pino')();

const app = express();

app.get('/rolldice', (req, res) => {
  const result = Math.floor(Math.random() * 6) + 1;
  logger.info({ player: req.query.player, result }, 'rolling the dice');
  res.send(String(result));
});

app.listen(8080, () => logger.info('listening on http://localhost:8080'));
```

Nothing in the app mentions tracing. Run it under OpenTelemetry's zero-code
instrumentation:

```sh
OTEL_SERVICE_NAME=dice OTEL_LOGS_EXPORTER=none \
  praytel --tui -- node --require @opentelemetry/auto-instrumentations-node/register app.js
```

Then roll a few times from another terminal:

```sh
curl 'localhost:8080/rolldice?player=alice'
```

Each request is a trace, and its `rolling the dice` line sits under the
`request handler - /rolldice` span. The pino instrumentation adds `trace_id`
and `span_id` to every line pino writes to stdout, which is all praytel needs.

`OTEL_LOGS_EXPORTER=none` stops the same instrumentation also sending each
line over OTLP; without it every line would arrive twice.

The `MetadataLookupWarning` on stderr is the SDK looking for a cloud host and is
harmless. Ctrl-C stops the app; spans from the last few seconds before it may
not have been sent yet.

### Tools you might already be using
Dagger and Docker buildx, from
[View traces from a running app](traces.md#a-tool-you-already-have), show both
kinds of line:

```sh
praytel --tui -- dagger functions
praytel --tui -- docker buildx build --call=check .
```

Dagger exports its own output as OTLP logs that carry trace context, so a line
such as `INF connected` sits under the `connect` span that produced it. Buildx
writes its progress (`#3 [internal] load .dockerignore`) to stderr with no IDs,
so those lines land in the orphan group described below, even though buildx
exports spans.

## How a line gets joined

Praytel reads the child's stdout and stderr line by line and accepts logs
exported over OTLP. A line is attached to a span when it names one:

| Log shape | What praytel reads |
|---|---|
| JSON object | The `trace_id` and `span_id` keys. |
| logfmt | The `trace_id` and `span_id` keys. |
| OTLP log record | The record's trace and span IDs. |

```
{"msg":"charge declined","trace_id":"e36345b5...","span_id":"afba9044..."}
level=warn msg="charge declined" trace_id=e36345b5... span_id=afba9044...
```

So the one change to make in your app is to put the active trace and span IDs
on every log line. Most logging libraries have an OpenTelemetry hook that does
this. Colour codes from pretty console writers are stripped before parsing.

## Lines that cannot be joined

Unstructured lines are not an error; they are still shown. Praytel never
guesses which span an untagged line belongs to. Instead:

- a line with no IDs goes to a global `orphan` group;
- a line with only a `trace_id` goes to that trace's own orphan group;
- a line naming a span praytel has not seen yet waits for that span, or for
  the end of the run.

If the same message arrives on stdout and over OTLP, both copies are kept.

## Reading the stream

```
stdout orphan          78db31e8... 7fa53bec... plain line
stdout checkout.Charge e36345b5... afba9044... {"msg":"grpc-span", ...}
```

The columns are the source (`stdout`, `stderr` or `otlp`), the span the line
belongs to, the trace and span IDs, and the line itself. The stream only ever
appends. A line that arrives before its span can be printed again once it has
been joined.

## Searching logs in the TUI

Press Tab to toggle the search between traces and log bodies.

## Limits

`--max-log-lines` caps retained lines (one million by default), with the same
evict-then-shed behaviour as spans.
