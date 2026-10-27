---
title: Binary call tracing
summary: Exploration of getting a praytel-style call tree from an uninstrumented binary, Go-specific and language-agnostic.
weight: 90
---

# Binary call tracing (exploration)

Question: can we get praytel's "what called what, during this run" tree from a
binary that has **no** OTel instrumentation, using its debug info?

Yes. Every viable route reduces to a stream of **call/return events per thread
(or goroutine)**. A stack per thread turns that stream into a tree, and each
frame becomes a span. Praytel already renders spans, so the cheapest parallel is
a bridge that emits OTLP and runs under `praytel --tui -- bridge ./bin`. No
changes to praytel itself.

A working prototype of the Go/Delve route lives in
[`experiments/dlvspans`](../experiments/dlvspans/README.md).

## Go-specific

| Approach | What you get | Recompile? | Timing fidelity | Platforms |
|---|---|---|---|---|
| **`dlv trace` → OTLP** (prototyped) | Exact per-call tree, args, return values, goroutine | No (DWARF ships by default); `-N -l` recommended | Poor: one breakpoint per call | macOS, Linux, Windows |
| `dlv trace --ebpf` | Same, via uprobes | No | Good | Linux amd64 only |
| Compile-time injection (`-toolexec` / AST rewrite adding `defer span()()`) | Exact tree, real timings, real OTel spans | Yes | Good | Everywhere |
| OTel Go compile-time instrumentation, Datadog Orchestrion | Library-level spans (net/http, sql, grpc), not every func | Yes | Good | Everywhere |
| OTel eBPF instrumentation (OBI, formerly Beyla / go.opentelemetry.io/auto) | Library-level spans | No | Good | Linux only, root |
| `runtime/trace` + `go tool trace` | Goroutines, scheduler, user regions/tasks | Code change | Good | Everywhere |
| pprof CPU profile | Sampled, aggregated call graph (no per-run ordering) | No | Statistical | Everywhere |
| `x/tools/cmd/callgraph`, go-callvis | Static "could call" graph | No run at all | n/a | Everywhere |

Notes:

- Compile-time injection is the high-fidelity option. The hard part is parent
  propagation without a `context.Context` in every signature: keep the span
  stack per goroutine (keyed by goroutine id), and link `go` statements
  to the spawning frame.
- Plain uretprobes are unsafe on Go: goroutine stacks move, and the patched
  return address breaks the program. eBPF tools probe the `RET` offsets instead.
  `dlv --ebpf` and OBI do this.

## Language-agnostic

| Tool | What you get | Notes |
|---|---|---|
| **uftrace** | Full function call tree with timings; `replay` prints a tree, `dump --chrome` exports | Linux. Best for C/C++/Rust built with `-pg` / `-finstrument-functions`; dynamic patching (`-P`) on x86_64 |
| DTrace pid provider | `dtrace -F -n 'pid$target:a.out::entry,pid$target:a.out::return' -c ./bin` prints an indented flow | macOS needs root and usually SIP relaxed (not usable on this dev box); also illumos/FreeBSD |
| bpftrace uprobes | Scriptable entry/return events | Linux, root; return-probe caveat above for Go |
| Frida (`frida-trace -i 'pattern' ./bin`) | Dynamic hooks on any symbol | macOS/Linux/Windows; code-signing friction on macOS |
| perf + Intel PT | Every branch, so the exact call tree (`perf script --call-trace`) | Linux, Intel CPUs; huge data |
| Valgrind callgrind | Aggregated call graph and cost | Linux; very slow; KCachegrind UI |
| lldb/gdb scripted breakpoints | Same model as `dlv trace` | Universal, slow |

## Recommendation

1. **Now:** the `dlv trace` bridge. It is zero-setup on macOS and
   Linux, and it shows the right *shape* in today's TUI.
2. **For real timings in Go:** a `-toolexec` instrumenter that emits OTel spans
   directly. It reuses praytel's OTLP path unchanged.
3. **For non-Go on Linux:** a uftrace → OTLP converter, using its chrome/JSON
   dump as the intermediate format.

The bridge pattern also suggests an ingest seam: a generic "call/return event
stream → spans" adaptor (Chrome trace JSON / Perfetto as input). That would let
praytel accept any of the tools above without one bridge per tool.

Open questions: span volume (hot loops produce millions of spans, so we need
`-match` filters, depth limits, or collapsing repeated siblings); how to show
goroutine spawn links; whether "function span" needs a distinct look in the TUI.
