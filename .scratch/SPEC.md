# Spec: Praytel v1 (coloured stream)

**Label:** `ready-for-agent`

Primary test seam: correlator inbound channels → outbound domain events.  
Thin second seam: wrapper process (exec, OTLP env/bind, signals, exit code, stderr, append-only stdout).

Architecture: [`ARCHITECTURE.md`](../ARCHITECTURE.md). Decisions: [`.scratch/MAP.md`](MAP.md).

---

## Problem Statement

I instrument services with OpenTelemetry and I emit logs, but I have to look in two places to see how a local run actually behaved. I do not want a collector in Docker just to get value out of traces and logs while I develop — or while an agent wires code and I need to check it did not stub the hard parts. I want to prepend a tool to a command, run it, and see logs and traces together, and I want to focus that view on a component or span.

## Solution

I run `praytel [flags] -- cmd …`. Praytel starts a temporary OTLP listener, points the child at it, captures stdout/stderr, joins logs to spans on trace/span IDs, and prints a coloured, append-only log stream (TUI later). When the command finishes, praytel drains leftover OTLP, then exits with the child’s exit code. No leftover files. I can `--focus` / `--exclude` by component or span name (path through the span tree). Untagged lines are not guessed onto real spans.

## User Stories

1. As a developer, I want to prepend praytel to any command, so that I get unified logs and traces without changing how I launch the app.
2. As a developer, I want praytel to exit when the command exits (after a short drain), so that it feels like a normal Unix wrapper.
3. As a developer, I want the child’s exit code to be praytel’s exit code, so that my scripts and CI keep working.
4. As a developer, I want Ctrl-C and SIGTERM forwarded to the child, so that the wrapped process shuts down as if praytel were not there.
5. As a developer, I want no praytel state files left after a run, so that I can run it in dirty trees and throwaway sandboxes.
6. As a developer, I want default OTLP ports 4317 and 4318, so that SDKs that already target localhost defaults work with no extra config.
7. As a developer, I want to override those ports with flags, so that I can run when something else already bound the defaults.
8. As a developer, I want praytel to set the child’s OTEL exporter env to its listener, so that I do not hand-edit collector URLs for a local run.
9. As a developer, I want both OTLP/gRPC and OTLP/HTTP protobuf, so that Go, JS, and Python apps can all export to praytel.
10. As a developer, I want traces ingested on the OTLP trace surfaces, so that span trees show up without a full collector.
11. As a developer, I want OTLP logs ingested on the OTLP log surfaces, so that SDK log bridges correlate without scraping stdout.
12. As a developer, I do not want metrics ingested, so that the tool stays a logs-and-traces wrapper.
13. As a developer, I want stdout and stderr captured, so that apps that only log to the console still show up.
14. As a developer, I want JSON or logfmt logs with trace_id and span_id joined onto those spans, so that I see one story instead of two silos.
15. As a developer, I want unstructured / non-JSON lines kept, so that noisy or legacy loggers are not dropped.
16. As a developer, I want those untagged lines on a dedicated orphan span, so that they are not falsely attached to real work.
17. As a developer, I want lines that only have a trace_id on that trace’s orphan, so that claimed-but-spanless logs stay on the right trace without guessing a span.
18. As a developer, I want lines that name a span_id we have not seen yet to wait until that span arrives (or drain), so that slow OTLP batches do not mis-parent logs.
19. As a developer, I never want praytel to guess a real span from timestamps or “nearest” spans, so that concurrency does not lie about causality.
20. As a developer, I want assignment to be sticky once final, so that later spans do not steal logs.
21. As a developer, I want child spans that arrive before their parent to still appear, so that async exporters do not hide work.
22. As a developer, I want stdout and OTLP copies of the same message both shown, so that praytel does not silently merge what the app actually emitted.
23. As a developer, I want each log line tagged with source (stdout, stderr, otlp), so that I can tell how it arrived.
24. As a developer, I want a coloured append-only log stream first, so that I can pipe and grep with POSIX tools.
25. As a developer, I want trace context hydrated on those lines when known, so that grep still has IDs to hang onto.
26. As a developer, I do not want the stream to rewrite earlier lines, so that pipes stay honest.
27. As a developer, I want `--focus GLOB` to show the path through matching component or span names (match, parents, children), so that I can see a component in context without the whole request.
28. As a developer, I want repeatable `--focus` flags unioned, so that I can keep two components in view.
29. As a developer, I want `--exclude GLOB` applied after that expansion, so that I can drop health checks from a focused path.
30. As a developer, I want live stream focus to admit a line the first time it enters the path, including late admits when a parent link lands, so that I do not lose lines because OTLP was slow.
31. As a developer, I accept that the stream cannot un-print, so that live admit never rewrites history.
32. As an agent running tests, I want to wrap the service under test with praytel, so that I can see whether real spans and logs exist instead of stubs.
33. As an agent, I want to focus on a component name, so that I can ignore unrelated noise while I debug wiring.
34. As a developer, I want idle-timeout drain after the child exits, so that late flushes still arrive.
35. As a developer, I want a hard cap on that drain, so that praytel cannot hang forever.
36. As a developer, I want both drain timings as flags, so that slow CI machines and fast unit tests can differ.
37. As a developer, I want `--max-spans` and `--max-log-lines`, so that a load test cannot unbounded-grow memory.
38. As a developer, I want oldest closed traces evicted first, so that live work is kept longer than finished work.
39. As a developer, I want newest data shed if everything is still live, so that the child does not deadlock on a blocked OTLP export forever.
40. As a developer, I want eviction and shedding warned on stderr, so that I know the view is incomplete.
41. As a developer, I want shedding not to change the child’s exit code, so that a full forest is not a fake test failure.
42. As a developer, I want bind failure to fail praytel without starting the child, so that I do not run “blind” without a collector.
43. As a developer, I want exec failure to fail praytel non-zero, so that a bad command is obvious.
44. As a developer, I want malformed JSON (not plain text) warned and rate-limited on stderr, so that a flood of bad lines does not hide the stream.
45. As a developer, I want missing colour support to degrade to monochrome, so that dumb pipes still work.
46. As a developer, I want praytel’s own diagnostics on stderr via a named logger, so that they never mix into the captured child stream.
47. As a developer, I do not want praytel to export OTLP into its own receiver, so that wrapper internals never appear as child traces.
48. As a future TUI user, I want the same join and focus rules, so that switching renderer does not change the meaning of the data.
49. As a future TUI user, I want the UI to stay open after drain until I quit, so that I can inspect a finished run.
50. As a developer using the stream renderer, I want praytel to return after drain, so that batch jobs complete.
51. As a developer, I want flags rather than a config file, so that there is no hidden project state.
52. As a developer, I want env vars only for OTel-standard keys, so that I am not inventing `PRAYTEL_*` configuration.
53. As a developer, I want one command at a time, so that the tool stays a bash prepend, not an orchestrator.
54. As a developer, I want late OTLP after drain warned and dropped, so that the run actually ends.
55. As a developer, I want component on OTLP logs taken from resource service.name when present, so that focus globs match SDK resources.
56. As a correlator, I want a single owner goroutine, so that join is race-free without locks on the forest.
57. As a renderer author, I want `Run(ctx)` and domain events, so that I do not depend on correlator maps.
58. As a developer, I want pipeline backpressure to block rather than drop, so that slowness does not silently lose logs.
59. As a developer, I want pending logs to emit `LogReceived` then `LogAssigned` with a stable ID, so that the stream can print immediately and hydrate later without reparenting.
60. As a developer, I want synthetic orphans flagged as synthetic, so that I never confuse them with process-root wrapping of real traces.

## Implementation Decisions

- Language Go. Bones `RootCtx` + `errgroup` for lifecycle. `LoggingTracer` on praytel stderr with a noop tracer. Bubble Tea is for a later TUI renderer, not v1 stream.
- Modules: domain (types and events only), receiver (OTLP gRPC + HTTP traces and logs, not the Collector framework), capture (stdio → `LogLine`), correlator (private forest, one goroutine), filter (own projection), render/stream (`Run`). Wiring in the praytel command package.
- One renderer per process, selected by flags; v1 default is the coloured stream. Shared renderer contract is `Run(ctx) error` with dependencies injected at construction.
- Correlator inbound: typed channels for spans, OTLP logs, and stdio lines. Outbound: value-copy domain events: `SpanUpserted`, `SpanParentLinked`, `SpanClosed`, `LogReceived`, `LogAssigned`, `DrainComplete`, `ForestEvicted`, `ForestShed`.
- `LogLine` is one type; `source` is stdout, stderr, or otlp. No dedupe across sources.
- Join rules: never guess a real span. No IDs → global synthetic orphan. Trace ID only → per-trace synthetic orphan (or global if the trace is unknown). Named unseen span_id → pending until span or drain, then real span or orphan. No reparenting after `LogAssigned`. Spans insert immediately; parent link may arrive later.
- Filter compiles `--focus` / `--exclude` globs (component or span name). Focus is path, not whole-trace. Repeatable focus is union; exclude after expansion. Stream uses live admit; buffered renderers will use `InFocus` at paint (not required for v1 stream beyond live admit).
- Child env: OTLP endpoints to the in-process listener. Default ports 4317/4318, flag overrides.
- Drain: idle timeout plus hard cap, both flags. Then `DrainComplete` and close the event channel. Stream `Run` returns. Process `ctx` cancel is praytel death, not child death.
- Caps: `--max-spans`, `--max-log-lines` on correlator-retained data. Evict oldest closed traces, then shed newest. Channels still block on slow consumers; they do not drop to relieve memory.
- Exit: child’s code if the child started; praytel non-zero if the child never started (exec, bind). Warnings never change the code. Fatals and warnings on stderr. `ErrMapper` only at process edge.
- Flags primary. No config file. No praytel-specific env vars.

## Testing Decisions

A good test asserts **observable behaviour at a seam**: events emitted, bytes on stdout/stderr, process exit code, env presented to the child, ports bound. It does not inspect correlator maps, goroutine topology, or renderer internals.

**Seam 1 (primary) — correlator.** Feed domain spans and `LogLine`s; read the outbound event sequence. Cover: JSON-like IDs join; untagged → global orphan; trace-only → per-trace orphan; named span pending then `LogAssigned`; child-before-parent `SpanUpserted` then `SpanParentLinked`; sticky assignment; cap evict then shed events; drain complete; no invented parents.

**Seam 2 (thin) — wrapper.** A helper child: prints lines, optionally exports OTLP, exits with a chosen code. Assert: exit code passthrough; bind failure without exec; OTEL env; signal forward (as much as the test harness allows); append-only stdout vs diagnostics on stderr; drain flags eventually return.

Filter live-admit can be tested as events → admitted log IDs, or as part of seam 1 if the test driver includes filter. Receiver mapping (proto → domain) only if the mapping is non-trivial; prefer injecting domain into seam 1 for join tests.

There is no existing test suite; these seams are the prior art to establish.

## Out of Scope

- TUI, call-graph indented renderer, summary dump, visual design (colours, orphan presentation, keybindings).
- Focus query language and whole-trace operator.
- Multi-process orchestration; non-TUI keep-open / browse-after-exit.
- OTLP metrics; embedding the OpenTelemetry Collector.
- Disk spill of the forest; `--strict` exit on warnings; praytel-specific env/config files.
- Deduplicating stdout and OTLP; guessing correlation; self-tracing into the in-process receiver.

## Further Notes

v1 is the coloured stream that proves capture → join → filter live-admit → append-only print. Later renderers reuse domain events and `Run`. Implementation planning can slice vertically (wrapper + empty join) but tests should still treat correlator events as the contract for join behaviour.

Do not restating ticket prose in code comments; link the architecture doc.
