# Ideas

While we're prototyping, let's jot down some places we could take this.
There are no wrong answers. 0% commitment guarantee.

Agents: ignore this file unless explicitly instructed to read from it.

- Go full TUI. No options, just get that very right.

## A query language for logs and traces

### Recommendation

Build a small, typed, TraceQL-shaped language over Praytel's in-memory run, then
use it everywhere: CLI filtering, the TUI, machine-readable output, and later MCP
tools. Borrow semantics, not an implementation, from:

- [Tempo TraceQL](https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/):
  evaluate one trace at a time, select span sets, and compose them with structural
  operators.
- [Loki LogQL](https://grafana.com/docs/loki/latest/query/): typed label filters,
  line filters, and parse/filter pipelines.
- [OpenTelemetry's data model and semantic conventions](https://opentelemetry.io/docs/specs/semconv/general/trace/):
  preserve resource, scope, span, and log fields rather than flattening them into
  display strings.

Do **not** claim TraceQL or LogQL compatibility. Each language assumes its own
backend, they are two separate languages, and importing Tempo/Loki would conflict
with Praytel's collector-free, in-memory purpose. A deliberately small dialect can
keep familiar names and operators without inheriting an unstable compatibility
promise.

SQL is also a poor primary syntax for the first version: hierarchy, “same span”
versus “same trace,” and attached logs become joins or recursive queries that are
harder for both humans and agents to write correctly. A future SQL adapter could
compile into the same AST.

### User model

A query is evaluated independently against each trace. It produces:

1. whether the trace matches;
2. the matching span IDs and log IDs (“evidence”); and
3. the view to retain: only matches, their path, or the whole trace.

Selectors are explicit about the record kind:

```text
# One span satisfies both predicates.
span{ resource.service.name = "checkout" && span:status = error }

# Matching logs; each result is associated with its assigned span.
log{ log:body *= "connection refused" && log:severity_number >= 17 }

# An error occurs anywhere below a frontend span.
span{ resource.service.name = "frontend" } >> span{ span:status = error }

# A checkout trace contains both a retry log and a database span. The two
# selectors need not match the same span.
log{ log:body *= "retry" } && span{ span:kind = client && db.system = "postgresql" }
```

Inside `{}` boolean operators apply to one span or one log. Between selectors they
combine sets within the same trace. This distinction prevents a common query bug:
mistaking “one span has A and B” for “the trace has one A span and another B span.”

Start with this grammar:

```text
query       = expression [ "|" view ]
expression  = selector
            | "(" expression ")"
            | expression ("&&" | "||") expression
            | expression (">" | ">>") expression
selector    = ("span" | "log") "{" [ predicate ] "}"
predicate   = field comparison value
            | "exists" "(" field ")"
            | "!" predicate
            | "(" predicate ")"
            | predicate ("&&" | "||") predicate
comparison  = "=" | "!=" | ">" | ">=" | "<" | "<=" | "=~" | "!~" | "*="
view        = "matches" | "path" | "trace"
```

- `>` means direct child and `>>` means descendant. Add parent, ancestor, and
  sibling operators only after real queries need them.
- `=~` and `!~` use Go's bounded-time RE2 regex semantics and are fully anchored,
  like TraceQL. `*=` is literal substring search, so users do not need `.*text.*`.
- Operator precedence is `!`, comparison, structural operators, `&&`, then `||`.
  The formatter always emits parentheses where a reader could be surprised.
- No implicit type coercion. `500` differs from `"500"`; incompatible ordered
  comparisons are false and can be reported by `--explain`.
- A missing field is distinct from an empty or zero value. `field != value` does
  not match a missing field; use `!exists(field)` explicitly.
- Attribute lookup follows exact OTel keys. Intrinsics use scoped names such as
  `span:name`, `span:duration`, `span:status`, `trace:duration`, `log:body`, and
  `log:source`; attributes use `resource.service.name`, `span.http.request.method`,
  or `log.error.type`.
- `log{...}` carries the matching log IDs as evidence and maps them to the log's
  final assigned span for structural operations. An unassigned log cannot satisfy
  a structural relation until assignment; it may still match a standalone log
  selector.

The default view for `--query` should be `path`, preserving today's useful
“match + ancestors + descendants” behavior. `matches` is better for compact agent
results; `trace` answers whole-trace questions. Existing `--focus` and `--exclude`
remain as simple shorthand compiled into the same AST/view plan. Do not maintain a
second evaluator for legacy flags.

Defer aggregations, grouping, query-time JSON/logfmt parsers, arithmetic, metrics,
and formatting stages. They substantially expand both grammar and semantics but
are not needed to replace focus globs or support useful agent queries.

### Data model prerequisites

The current model is sufficient for display but not for trustworthy typed queries:
span attributes are stringified; most resource metadata is discarded; and OTLP
log timestamps, severity, attributes, and scope are discarded. Introduce these
types at the receiver/capture boundary before exposing the language:

```go
type Value struct {
    Kind   ValueKind // string, int64, float64, bool, bytes, array, map
    // exactly one typed payload
}

type Resource struct { Attributes []Attr }
type Scope struct { Name, Version string; Attributes []Attr }

type Span struct {
    Key          SpanKey // TraceID + SpanID, never SpanID alone
    Parent       SpanKey
    Name         string
    Kind         SpanKind
    Start, End   time.Time
    Status       SpanStatus
    StatusMessage string
    Resource     Resource
    Scope        Scope
    Attributes   []Attr
    Events       []SpanEvent
    Links        []SpanLink
}

type LogLine struct {
    ID                 string
    Timestamp          time.Time
    ObservedTimestamp  time.Time
    SeverityNumber     int32
    SeverityText       string
    Body               Value
    Resource           Resource
    Scope              Scope
    Attributes         []Attr
    Source             Source
    DeclaredTraceID    string
    DeclaredSpanID     string
}
```

Keep ordered attribute slices as the canonical ingest representation so duplicate
keys and source order are not silently lost; build per-run lookup indexes in the
query projection. For JSON/logfmt stdout, retain the parsed scalar fields as log
attributes while preserving the original line as the body. Unstructured output
still has only `log:body`, `log:source`, and observed time.

Keying spans by `(TraceID, SpanID)` fixes an existing latent assumption: an OTel
span ID is scoped to its trace, not guaranteed globally unique for the entire run.
Do this migration before structural query operators.

The event stream remains the boundary: the correlator keeps its private mutable
forest, while a query projection consumes value-copy events. It should retain:

```text
RunProjection
  traces[TraceID] -> TraceProjection
    spans[SpanKey] -> typed fields, parent/children, attached log IDs
    logs[LogID]    -> typed fields, assignment
    revision       -> increments on every trace-affecting event
    state          -> live | settled | final
    incomplete[]   -> evicted, shed, drain_timeout, unknown_parent, ...
```

Do not let the parser, evaluator, CLI, TUI, or MCP reach into correlator maps. Put
the pure lexer/parser/type checker/evaluator in `internal/query`; put the
event-derived read model in `internal/projection`. Evolve `internal/filter` to
delegate to those packages, then remove its bespoke glob evaluator once all
callers have migrated. The TUI and agent interface should share evaluation code,
not necessarily mutable projection instances.

### Live and “complete trace” semantics

OTLP does not send a “trace complete” record. All currently known spans ending is
not proof that another service will not export a late span. Name the live concept
**settled**, not complete:

- `live`: the projection changed recently or has a known open span;
- `settled`: every known real span ended and no trace-affecting event arrived for
  `--settle-after` (default to a short, documented duration);
- `final`: the run drained and no more telemetry will be accepted.

Completeness is orthogonal to lifecycle state: a settled or final result can carry
`incomplete_reasons` when Praytel knows data was lost or forcibly bounded.

A late event reopens a settled trace, increments its revision, and may later emit
another settled result. Only `final` is terminal. Every machine result includes
`trace_id`, `revision`, `state`, `matched_at`, `evidence`, and `incomplete_reasons`.
Consumers deduplicate by `(trace_id, revision)`, never just trace ID.

This supports long-running servers without pretending to know the unknowable:

```sh
praytel --query 'span{ span:status = error }' \
  --emit settled --format ndjson -- ./server
```

The process stays alive; each settled matching revision is emitted promptly. On
shutdown/drain, final revisions and a terminal `run_finished` envelope are emitted.
Add `--until-match` as a separate lifecycle option only after settled output works;
selection and process termination must not be conflated in the query grammar.

### Developer experience

- `--query/-q STRING`; `--query-file FILE` for checked-in, reviewable queries.
- `praytel query check` parses and type-checks without running a child.
- `praytel query fmt` produces a canonical form for docs, fixtures, and review.
- `praytel query explain` prints the AST, inferred field types, required finality,
  and view expansion. `--explain-match` returns the exact span/log evidence.
- `--format text|json|ndjson`; schemas carry `schema_version` and never use ANSI.
- Diagnostics include a source range, expected tokens, and nearest known field.
- Shell completion exposes intrinsic fields and operators; TUI completion can add
  attribute keys observed in the current run.
- Bound all result sets with an explicit limit. Machine output always reports
  `returned`, `matched`, `truncated`, and any data-loss/incompleteness reason.

### Test and review plan

1. **Typed model migration:** receiver fixtures cover every OTLP value kind,
   resource/scope propagation, severity, events/links, and two traces sharing a
   span ID. Existing rendering stays behaviorally unchanged.
2. **Syntax package:** table tests for precedence and diagnostics; golden tests for
   canonical formatting; fuzz lexer/parser with the invariant “never panic,” and
   parse-format-parse AST equivalence for valid generated queries.
3. **Evaluator:** hand-built asymmetric trees distinguish child from descendant,
   same-record from same-trace, missing from empty, string from number, and
   assigned from pending logs. Expected evidence is authored independently of the
   evaluator.
4. **Event-order tests:** replay permutations such as child-before-parent,
   log-before-span, settle-reopen-settle, eviction, and drain. Snapshot evaluation
   after the same logical history must converge to the same final result.
5. **CLI contracts:** golden NDJSON envelopes and schema validation; exit behavior
   remains the child's exit code. Test broken pipes, invalid queries before child
   start, limits, and no-color output.
6. **Compatibility:** translate every current focus/exclude test to the compiled
   legacy AST and run it against the same evaluator before deleting old logic.

Land these as reviewable slices: typed model with unchanged UI; parser/formatter;
snapshot evaluator; live projection; CLI query; then TUI integration. Each slice
has fixtures and observable output without requiring the next one.

## Agent integration

### Recommendation

Deliver agent integration in two layers:

1. a stable `json`/`ndjson` CLI contract and a short agent skill/playbook; then
2. an optional `praytel mcp` stdio server built over the same session and query
   services.

The CLI contract comes first because it is useful in CI and shell scripts, easy to
snapshot-test, and independent of agent vendors. MCP then improves discovery,
long-running process ownership, cancellation, and progressive drill-down without
becoming a second query implementation.

Jaeger is now a relevant reference, not a first target to proxy. Its
[official MCP design](https://github.com/jaegertracing/jaeger/blob/main/docs/adr/002-mcp-server.md)
uses narrow read tools and progressive disclosure—search summaries, inspect
topology/critical path, then request a few span details—because whole traces can
exhaust model context. Praytel should copy that interaction pattern. It should not
adopt Jaeger's interface verbatim: Jaeger is a persistent trace backend with no
joined logs or child-process lifecycle, while Praytel is a bounded local run with
both.

### Agent workflow

```text
agent/client
    |
    | start_run(command, query)
    v
session service ── owns child, OTLP receiver, correlator, limits, cancellation
    |
    v
event projection ── typed traces + joined logs + revision/finality metadata
    |
    +── wait_for_matches(cursor, timeout)  small summaries + evidence
    +── get_trace_topology(trace_id)       structure, no attributes/log bodies
    +── get_span_details(span_ids)         bounded rich detail
    +── get_logs(log_ids)                  bounded bodies/attributes
```

An expected debugging loop is:

1. Start the test or server under Praytel with a focused query.
2. Wait for settled matches, a run terminal state, or a bounded timeout.
3. Inspect compact trace topology and matching evidence.
4. Request details only for suspicious spans/logs.
5. Stop a long-running child explicitly when verification is complete.

This makes “wait” a tool operation with a timeout and cursor, not an instruction to
poll or parse terminal text. It also gives agents a clear terminal record even when
the child fails to start, exits, is cancelled, or reaches drain timeout.

### MCP surface

Start with these tools:

| Tool | Purpose |
| --- | --- |
| `start_run` | Start one command with query, settle, drain, and memory limits; return `run_id` and initial cursor. |
| `wait_for_matches` | Long-poll from a cursor; return compact matching trace revisions plus run state. |
| `get_trace_topology` | Return a bounded tree skeleton with timings/status, but no attributes or log bodies. |
| `get_span_details` | Return typed fields for explicitly requested span IDs, capped per request. |
| `get_logs` | Return explicitly requested joined logs, capped by count and bytes. |
| `stop_run` | Signal and drain a running child; report its eventual exit state. |

Do not initially expose free-form filesystem access, arbitrary environment dumps,
raw full-trace export, a general shell tool, or mutation of telemetry. `start_run`
does execute a command, so its description and MCP annotations must make that side
effect explicit. Pass environment as an allowlisted object; never echo inherited
secrets in results. Restrict runs to the server's working directory unless the
client explicitly supplies an allowed subdirectory.

Every tool result has a common envelope:

```json
{
  "schema_version": 1,
  "run_id": "...",
  "run_state": "running",
  "next_cursor": "...",
  "returned": 3,
  "matched": 8,
  "truncated": true,
  "incomplete_reasons": ["max_results"],
  "items": []
}
```

Absence is only assertable when `run_state` is terminal, `truncated` is false, and
`incomplete_reasons` is empty. Put that rule in tool descriptions and the agent
playbook. Timeouts return a successful response with `timed_out: true` and a cursor;
protocol/tool failures are reserved for invalid calls or unavailable sessions.

Use opaque cursors tied to a monotonic per-session journal sequence. A retry with
the same cursor is idempotent. Keep a bounded journal of result summaries, not a
second unbounded copy of all telemetry. If a cursor has expired, return a typed
`cursor_expired` error and the oldest available cursor; never silently skip.

### Software boundaries

Refactor today's `cmd/praytel.run` composition into an internal session service
only when the CLI query path needs it; avoid a speculative framework beforehand:

```text
cmd/praytel             flags and subcommands: run, query, mcp
internal/session        one run's lifecycle; child/receiver/correlator ownership
internal/projection     event-derived queryable run state and settlement clock
internal/query          pure syntax, type checking, evaluation, explanations
internal/agentapi       transport-neutral request/response structs and handlers
internal/agentapi/mcp   MCP schema/stdio adapter only
```

CLI and MCP handlers call the same `session` and `agentapi` operations. They must
not shell out to each other. Keep MCP transport types at the outer adapter so a
future HTTP API or another agent protocol does not leak into domain/query code.
Prefer the official Go MCP SDK when implementation begins, but pin and wrap it at
this adapter boundary.

The stdio MCP server owns multiple independent runs because MCP clients expect the
server process to outlive an individual tool call. Apply both per-run and global
caps, cancel all children on server shutdown, and retain terminal sessions only for
a short bounded period. The existing transparent `praytel -- command` path remains
single-run and leaves no files or daemon.

### Installation and guidance

- `praytel mcp` speaks stdio; client configuration supplies the executable and
  working directory. It opens no network listener by default.
- Publish copyable snippets for Amp, Claude Code, VS Code/Copilot, and other common
  clients, but keep the protocol invocation vendor-neutral.
- `praytel agent instructions` prints a versioned, concise playbook explaining the
  progressive workflow, completeness rule, useful query examples, and limits.
- Add an explicit `praytel agent install --client NAME` only where a client's
  configuration format is stable and the command can show a diff and require
  confirmation before writing. Documentation remains the fallback.
- Report the Praytel and schema versions in MCP initialization and every recorded
  verification result so reviews can reproduce the behavior.

### Verifiability and reviewability

Agent output should be evidence, not an opaque conclusion:

- Each match cites the canonical query plus exact span/log evidence IDs.
- Topology uses deterministic depth-first order; attribute maps and result arrays
  have deterministic ordering for review diffs.
- `start_run` records command arguments, working directory, start time, Praytel
  version, and non-secret OTel configuration. It does not record inherited env.
- Terminal results include child exit code, signal, drain reason, data pressure,
  final revision counts, and whether any result was truncated.
- `praytel verify --record FILE` can write the same versioned envelopes returned by
  MCP for CI artifacts. Replay tests feed recorded domain events into the query
  projection without starting a process.

### Test plan

1. **Transport-neutral handlers:** fake session/projection and fake clock; test
   validation, limits, timeout semantics, idempotent cursors, cursor expiry, and
   redaction without an MCP runtime.
2. **MCP contract:** run an in-process SDK client against stdio/in-memory transport;
   snapshot `tools/list` JSON Schemas and validate every success/error result.
3. **Progressive disclosure:** a large synthetic trace proves search/topology are
   bounded and materially smaller than detail responses; details reject excessive
   span/log lists.
4. **Lifecycle races:** child exits before first wait, wait cancellation, stop while
   exporting, settled trace reopens, MCP shutdown with active children, and two
   concurrent isolated runs.
5. **Security:** working-directory traversal, forbidden env keys, ANSI/control
   characters, oversized log bodies, hostile query strings, and no secrets in
   diagnostics or recorded artifacts.
6. **End-to-end:** instrumented fixture programs cover a short passing test, an
   error trace with joined logs, no match, a long-running server stopped by the
   client, child start failure, and data shedding. Assert structured fields and
   terminal states, not model prose.
7. **Agent evaluation:** maintain a small set of tasks with expected tool-call
   invariants (query before details, bounded detail requests, no “no match” claim
   on partial data). Treat these as product evaluations, not deterministic unit
   tests or merge-blocking checks initially.

Implement in this order: query/NDJSON foundation; recorded verification artifacts;
single-run agent handlers; MCP adapter; long-running/multiple sessions; optional
client installers. That sequence gives useful, reviewable functionality at every
step and keeps MCP churn out of the core architecture.
