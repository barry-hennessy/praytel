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
