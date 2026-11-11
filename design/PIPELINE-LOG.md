---
title: Pipeline log
summary: What each phase of the pipeline change did, the checks that ran, where the work departed from the plan, and what to know.
weight: 82
---

# Pipeline log

One section for each phase of [Pipeline](PIPELINE.md). Each section has the
checks that ran, the departures from the plan and the items to know.

## Phase 1: `internal/projection`

Branch `pipeline-phase1-projection`.

Checks:
- `go test ./...`: pass. New tests in `internal/projection`: one per event
  type, arrival-order convergence (child before parent, log before span,
  late assignment, eviction of another trace), eviction, clone
  independence, version, revision and `SetNow`.
- `scripts/golden`: no golden changed.
- TUI benchmarks, 3 runs before and after (`benchstat`): no significant
  change. Geomean time −0.58 %; memory and allocations flat.
- `golangci-lint run` on the changed packages: 0 issues.

Departures:
- No `Children` or `Roots` accessors. No code uses them yet. Phase 3 adds
  them with the views that need them.
- `canonicalise` changes a clone of the forest instead of making a separate
  canonical order. The forest it was given does not change, which is the
  purpose of the step, with much less change to the render code.
- `spanView` and `logView` stay as type aliases of `projection.Span` and
  `projection.Log`, to keep the diff small.
- One test line changed (`tui_test.go`): it read the old `m.spans` map.

To know:
- Behaviour kept from the old reducer: if a declared (pending) log arrives
  after its real span, the span becomes a placeholder again. The correlator
  does not send events in that order. The tests use the real order.
- The status message is raw in the projection. The TUI sanitises it where
  it shows it.

## Phase 2: Plan types, stages, flags to plan

Branch `pipeline-phase2-plan`.

Checks:
- `go test ./...`: pass. New tests: plan validation (each rule), JSON round
  trip, one plan golden per flag set
  (`cmd/praytel/internal/args/testdata/plans`, `-update` rewrites them),
  `--print-plan` without a command, stage forms agree (`OnEvent` and `Lens`
  give the same spans), control events pass every stage.
- `scripts/golden`: no golden changed.
- `golangci-lint run ./...`: 0 issues.
- No benchmark run: the TUI hot paths did not change. The snapshot now runs
  each event through one stage.

Departures:
- Stages are in `internal/stage`, not `internal/pipeline`. The pipeline will
  import the views, and the views use stages, so a separate leaf package
  prevents an import cycle.
- `logs` is a view option (`View.Logs`), not a stage. The snapshot needs
  the captured log lines to replay a failed child's output, so a stage must
  not remove them. Phase 4's `tree(logs=)` builtin agrees with this.
- The snapshot is one view, `snapshot`, with `sections` (`tree`, `graph`).
  The plan said views `tree` and `graph`. One document with one header and
  one exit decision is simpler as one view, and the golden format does not
  change. `tree` and `graph` remain the live views for TUI panes.
- No `--snapshot-attrs` is the stage `no_attrs`. The glob `-*` does not
  match keys that contain `/`, so it is not the same.
- The exit rule is `Sink.Exit`: the output decides the exit code (3 if
  incomplete; replay and the child's code if the child failed).
- `tui.SnapshotOptions.Attrs` stays. `tui.Snapshot` runs events through the
  `attrs` stage. Phase 3 removes the option when the snapshot view takes
  its stages from the plan.

To know:
- `--tui` is now a plan with two panes (`tree`, `graph`). The TUI renderer
  does not change until Phase 5.
- `--print-plan` prints JSON for any flag set and exits 0.

## Phase 3: Driver, views, triggers, sinks

Branch `pipeline-phase3-driver`.

Checks:
- `go test ./...`: pass. New tests in `internal/pipeline`: the snapshot
  through the driver is byte-equal to `tui.Snapshot` on the same events;
  fan-out to two outputs (a snapshot to a file and the stream to stdout) in
  one run; the exit sink's three rules (failed child, incomplete,
  interrupted); compile rejects an invalid plan, a TUI sink and an exit
  sink on a view that cannot report.
- The current `cmd/praytel` tests (stream, snapshot, failed-child replay,
  incomplete snapshot, signals) pass through the driver without change.
- `scripts/golden`: no golden changed.
- `golangci-lint run ./...`: 0 issues.

Departures:
- The end-to-end test with two outputs is in `internal/pipeline`, not
  `cmd/praytel`. The `cmd` tests run the built binary, and no flag makes
  two outputs before `-e` (Phase 4). Phase 4 adds the `cmd` test.
- The view contract is in a leaf package, `internal/view` (`View`,
  `Viewport`, `Reporter`, `Interrupter`). The views and the driver both
  use it without an import cycle.
- `Render` takes a viewport but no dirty set yet. Phase 6 adds the dirty
  set with the trigger `settled`.
- The driver runs the triggers `each` and `final`. `tick` stays with the
  TUI until Phase 5; `settled` comes in Phase 6. Compile rejects them.
- The stream view logs pressure warnings to the praytel logger, as before.
  `stream.Stream` stays as a thin wrapper on the view for its tests, whose
  span goldens name `stream.New` and `stream.Stream.Run`.
- `tui.SnapshotOptions.Attrs` stays for the `tui.Snapshot` helper that the
  tests use. The snapshot view ignores it: its attributes come from the
  output's stages.
- `internal/render/snapshot` is removed. The exit sink in
  `internal/pipeline` has its rules.

To know:
- The snapshot is built from the shared projection through the output's
  lens. Component colors decide where the tree draws its gutters and rules,
  so the snapshot view rebuilds them from the settled forest
  (`Model.recolor`): a component has a color when one of its real spans is
  a parent. Only "color or no color" shows in the ASCII snapshot.
- On an interrupt the driver keeps reading events until the channel
  closes, so the correlator never blocks. Before, the snapshot renderer
  returned at once and a goroutine read the rest.
- The incomplete message comes from the output's name:
  `praytel: snapshot incomplete: …` for the flag plan, as before.

## Phase 4: `--execute` (Starlark)

Branch `pipeline-phase4-execute`.

Checks:
- `go test ./...`: pass. New tests in `internal/plan/starlark`: each
  `testdata/*.star` (statements, a helper function, a loop, `load()` of a
  library) compiles to its JSON golden; a snippet table (expressions,
  lists, statements, two snippets, every join and validation error, the
  error position, a runtime error inside a function, an endless loop);
  `load()` rules (inside the directory, `.star` only, no cycles, no
  outputs while a module loads); `print()`.
- `cmd/praytel/internal/args`: each mode flag set and its documented `-e`
  form make byte-equal plans; `-e` with `--tui` or `--snapshot` is a
  usage error.
- `cmd/praytel` with the built binary: two outputs in one run (a snapshot
  to a file and the stream to stdout); a `.star` file; a bad snippet fails
  with its position before the child starts.
- `go test -fuzz FuzzSnippet -fuzztime 30s`: 1.6 M inputs, no panic, no
  invalid plan returned.
- `scripts/golden`: no golden changed.
- `golangci-lint run ./...`: 0 issues.

Departures:
- `go.starlark.net` has no release tags. The module is the pseudo-version
  `v0.0.0-20261021141710-4f4267fb140d` (2026-10-21).
- `exit` is a value that marks the output's sink, not `exit(code)`. The
  incomplete code is 3 for now; Phase 8 adds the check code.
- `tree()` and `graph()` take no arguments; `logs=` is on `snapshot()`
  only, the one view that has the option.
- The snapshot is `snapshot("tree", "graph", logs=False)`, as in Phase 2.
  Its sections render tree first, whatever order they are given in, so
  the plan matches `--snapshot`.
- An output's name is its pane name, or else its view kind. This makes
  the `-e` forms equal to the flag plans.
- `settled` is defined but the driver rejects it until Phase 6.
- A plan with TUI panes and other outputs is an error until Phase 5.
- A script's work is capped (10 M Starlark steps), because the scripts
  may use `while` loops and run before the child starts.
- Plan errors print as `praytel: plan: <file>:<line>:<col>: …` and exit 1,
  for `--print-plan` too.

To know:
- The reference page (`docs/reference/praytel.1.md`) has a Plans section:
  the builtins and the `-e` form of each mode flag.
- `print()` in a script writes to stderr.
- Commit signing (1Password) failed for this phase's commit with "failed
  to fill whole buffer". The work stays staged; see the note at the end
  of this log.

## Phase 5: The TUI as panes

Branch `pipeline-phase5-panes`.

Checks:
- `go test ./...`: pass. New TUI tests: three panes cycle with ctrl+g in
  plan order; a stream pane shows the stream's lines and the footer names
  the pane; a pane's stages (`attrs("-secret")`) hide attributes in that
  pane only and do not change the forest; the default panes are tree then
  graph. New driver tests: TUI outputs compile into panes in plan order;
  a plan with panes rejects stdout and stderr outputs; the TUI gets every
  event while a file output runs beside it; a TUI that ends does not block
  the driver.
- The current TUI tests pass without change (ctrl+g still toggles tree and
  graph with the default panes).
- Pseudo-terminal smoke test of the built binary: three panes (`calls`,
  `map`, `logs`) and a snapshot to a file; ctrl+g showed `map 2/3` then
  `logs 3/3`; ctrl+q exited 0; the file held the child's span.
- TUI benchmarks, 3 runs against the Phase 0 baseline: no significant
  change (geomean −0.04 %; memory and allocations flat).
- `golangci-lint run ./...`: 0 issues.

Departures:
- The TUI keeps its own projection and its interactive loop. The driver
  passes it every event and runs every other output. The plan wanted the
  driver to own one projection and render the panes each tick. That needs
  either a lock on the TUI's hot path (Bubble Tea reads the model on its
  own goroutine) or most of the 1,400-line TUI model rewritten; both put
  the tuned search, scroll and graph layout at risk. Panes, their stages,
  tabs and sharing a run work without it.
- The search box is not a stage. Fuzzy search stays view state, so a query
  change does not recompile the pane's plan. Phase 7 adds a query mode.
- Panes cycle with ctrl+g; the footer shows `name i/n`. There is no tab bar.
- A plan with panes can only send other outputs to files: the TUI owns the
  terminal, and a stdout or stderr output would write over it.

To know:
- When the TUI quits early, the driver stops passing it events, so the
  correlator never blocks. Before, nothing read the events after a quit.
- `stream()` can now be a pane (`stream() | tick | pane("logs")`).
