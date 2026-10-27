# dlvspans (experiment)

Turn an uninstrumented Go binary's call tree into OTLP spans, so praytel can
render it like any other trace. It runs the binary under `dlv trace`, parses
each call/return line, keeps a stack per goroutine, and emits one span per call
(args and return values as attributes) to `OTEL_EXPORTER_OTLP_*`.

See [`design/BINARY-TRACING.md`](../../design/BINARY-TRACING.md) for the wider
landscape and trade-offs.

```sh
go build -o dlvspans .
(cd example && go build -gcflags='all=-N -l' -o demo .)
praytel --tui -- ./dlvspans ./example/demo
praytel --tui -- ./dlvspans -match 'mypkg\.' ./mybin --its-flags
```

Result in the TUI:

```
● demo.run demo 507ms
┆ ● demo.goroutine 1 179ms
┆ ┆ ● demo.main.main 142ms
┆ ┆ ┆ ● demo.main.handle 115ms       go.args="apple,kiwi"  go.return=27
┆ ┆ ┆ ┆ ● demo.main.parse 13ms       go.args="apple,kiwi"
┆ ┆ ┆ ┆ ● demo.main.total 63ms       go.args=main.Order {Items: []string len: 2 ...}
┆ ┆ ┆ ┆ ┆ ● demo.main.price 13ms     go.args="apple"  go.return=15
┆ ┆ ┆ ┆ ┆ ● demo.main.price 13ms     go.args="kiwi"   go.return=12
```

Caveats:

- Build the target with `-gcflags='all=-N -l'`. Optimised builds inline calls,
  so inlined frames are misnamed and args read as `(unreadable ...)`.
- Every traced call is a ptrace breakpoint (~10ms here). Durations show
  structure and relative weight, not real latency. Keep `-match` narrow.
- Spans are timestamped when the bridge reads the line, not by the target.
