# Add `--snapshot` to emit a stable golden of the trace tree and graph

## Summary

Add a third output mode, `--snapshot`, that runs the child to completion, waits
for the drain, and prints one deterministic, plain-text rendering of the trace
tree and/or the component graph to stdout. Committed next to a test, it is a
golden file: a reviewable record of which components a test calls, with which
data, and how well it is traced. Re-running and diffing against it verifies
that nothing changed.

Stability across runs matters more than fidelity to the TUI.

## Motivation

We want to run a test suite under praytel and keep a golden of its trace tree
alongside the test, to be committed and reviewed, or compared in place in CI.
Today the only outputs are the append-only stream and the live `--tui`. Neither
is stable: sibling order is arrival order, durations and ids vary, and the
output depends on terminal width and colour.

## Usage

```
praytel --snapshot=tree -- go test -count=1 . > testdata/trace.golden
praytel --snapshot="tree graph" -- go test -count=1 . > testdata/trace.golden
praytel --snapshot=tree --snapshot-attrs="http.* -http.url" -- go test -count=1 .
```

Updating a golden is a shell redirect (`>`). Checking one is a diff:
`praytel ... | diff -u testdata/trace.golden -`.

## Flags

| Flag | Meaning |
|---|---|
| `--snapshot="tree graph"` | Space-separated views: `tree`, `graph`. No default; a bare `--snapshot` is a usage error. Output order is always tree then graph. Mutually exclusive with `--tui`. |
| `--snapshot-attrs=SPEC` | Which span attributes to show (below). |
| `--snapshot-logs` | Include log lines. Off by default. |

`--snapshot-attrs`, whitespace-separated key globs, `-` prefix excludes:

| Invocation | Attributes shown |
|---|---|
| flag absent | none |
| `--snapshot-attrs` (bare) | all |
| `--snapshot-attrs="a b"` | only `a` and `b` |
| `--snapshot-attrs="a b -c"` | only `a` and `b`, without `c` |
| `--snapshot-attrs="-c"` | all except `c` |

Excludes win. Shown attributes are sorted by key. A bare `--snapshot-attrs`
needs a custom `flag.Value` with `IsBoolFlag()`, so a value must be given with
`=`; the docs should always show `--snapshot-attrs="..."`.

The span rule width is fixed at 80. Content is never wrapped or truncated, so
long values and wide graphs can exceed 80 columns.

## Output

A version header, then one section per requested view:

```
# praytel-snapshot v1
## tree
...
## graph
...
```

The tree is the TUI's trace tree (status symbol, label, attributes, rule) minus
durations, colour, logs (unless `--snapshot-logs`) and synthetic spans.

## Making it stable

All of this runs on the settled model after the drain, never on event order.

- **Tree sibling order.** Sort siblings and roots by a canonical subtree key:
  the span's component and name (kept separate, since `spanLabel` merges them
  ambiguously), status, filtered attribute rows, and its children's keys,
  computed bottom-up. Spans with equal keys render identically, so their order
  cannot change the output. Then rewrite `spanView.order` to the canonical
  pre-order index (`filteredForest` sorts by it).
- **Graph order.** `graph.Build` is commutative, but `Node.Order` and
  `Edge.Order` come from arrival order and drive the whole layout
  (`OrderedNodeKeys`, `SortedEdgeKeys`, feedback-edge DFS, layering). After
  `buildGraph`, overwrite them with ranks from the sorted node keys and the
  sorted `(FromKey, ToKey)` pairs, so the graph depends only on the final set
  of nodes and edges.
- **Durations** are dropped (a `plain` flag on the model), with trailing spaces
  trimmed.
- **Colour and width.** Colour profile `Ascii`, then `ansi.Strip`; `m.width = 80`.
  Tree bars depend only on the settled set of caller components, which is
  order-independent.
- **Synthetic spans** (orphan logs, captured stdout/stderr) are dropped unless
  `--snapshot-logs`. `spanView` needs to keep the `Synthetic` flag from
  `SpanUpserted`.
- **Placeholders.** A declared-but-unseen `span/<id>` span is rendered as
  `<missing span>` and also fails the completeness gate.

## Completeness gate and exit behaviour

The snapshot is only printed if the run is provably complete. Otherwise
praytel prints nothing to stdout.

| Case | Stdout | Stderr | Exit code |
|---|---|---|---|
| Child exits 0, snapshot complete | the snapshot | | 0 |
| Child exits non-zero | nothing | captured child stdout/stderr replayed | the child's code |
| Child exits 0, snapshot incomplete | nothing | list of problems | distinct praytel code (proposed: 3) |

A snapshot is incomplete if any of these hold:

- `DrainComplete` was never seen
- `ForestEvicted` or `ForestShed` was seen (data was dropped)
- declared placeholder spans remain
- a span names a parent that never arrived
- an unclosed span remains
- zero spans (uninstrumented code, a cached `go test`, or spans never flushed)
- OTLP arrived after the drain (needs a receiver drop counter; to be verified)

When the child fails, the child's code wins and any problems are appended to
stderr. With `>`, the shell truncates the golden before praytel runs, so a
failed run leaves an empty file. That shows up in `git diff` and fails a CI
comparison.

## Implementation outline

- `cmd/praytel/internal/args/args.go`: `Config.Snapshot`, `SnapshotAttrs`,
  `SnapshotLogs`; validation and mutual exclusion with `--tui`.
- `cmd/praytel/run.go`: a third renderer branch beside `stream.New` and
  `tui.New`; after `renderGroup.Wait()`, merge the snapshot result into the
  exit code. Today `renderErr` is only logged.
- `internal/render/tui/snapshot.go`: a pure core,
  `Snapshot(events <-chan domain.Event, opts) SnapshotResult` (text, problems,
  captured child output). It must live in package `tui` because
  `internal/graph` and the tree helpers are private to it. Events in, text
  out, no I/O: a future record/replay mode can reuse it.
- `internal/render/snapshot`: a thin renderer wrapper doing stdout/stderr and
  the exit code.
- `Model` changes: `spanView.synthetic`; a `plain` flag; a `canonicalise()` step.
- Docs: `docs/reference/praytel.1.md`, plus a short guide section.

## Tests

- **Property test.** Feed one event set in many shuffled orders with random ids
  and timestamps; the tree and graph output must be byte-identical. The
  existing graph tests use `rand` to generate graphs, but none permutes one
  graph and compares output, so this is new coverage.
- **Tie fixture.** Two identical sibling subtrees.
- **Golden fixtures** in `testdata/snapshot/*.txt`, with an `-update` flag like
  `graph_render_test.go`.
- **Gate tests.** One each for evicted, shed, placeholder, unclosed span,
  missing parent and an empty run.
- **`args_test.go`.** View list parsing (bare, unknown, duplicates) and a
  table test for the `--snapshot-attrs` rules.
- **`wrapper_test.go`.** Run `testdata/child` twice and compare.
- Run the property test and fixtures on both arm64 and amd64: the layout uses
  `float64` accumulation (`weightedMedian`, `cost`), where Go may fuse
  multiply-add differently per architecture.

## Conventions the test suite must follow

Praytel cannot do these for you; the docs should say so:

- Run with `-count=1`; a cached `go test` emits no spans.
- Flush the tracer in `TestMain` (`tp.Shutdown`); a binary that exits without
  flushing silently loses its last spans.
- Wrap each test in a root span carrying `test.name`; otherwise tests cannot be
  told apart in one golden.
- One golden per package: run from the package directory.
- Use `--grpc-port 0 --http-port 0` (to be verified) for parallel packages.

## Spikes before implementing

- Does `--grpc-port 0` / `--http-port 0` work end to end, i.e. does `ChildEnv()`
  report the bound port?
- Can the receiver report spans dropped after the drain?
- Does the correlator ever leave a span half-linked at drain, or emit a stale
  `SpanUpserted` after a newer one?
- Does clearing `sp.logs` leave anything of `Model.logs` in the output?

## Out of scope / follow-ups

- Redaction of unstable attribute values (ports, tmp paths, ids, timestamps):
  `.scratch/snapshot/issues/01-value-redaction.md`. Until then, hide an
  unstable attribute with `--snapshot-attrs="-key"`.
- Praytel-managed golden files (`--golden`, `--update`, per-test filenames).
- Record/replay (`--record`, offline `render` / `diff`).
- A Go test library (`golden.Assert`) for in-process tests.

## Open decisions

- Exit code for an incomplete snapshot (3 proposed).
- Whether `--snapshot-width` should exist (width is a constant 80 for now).
