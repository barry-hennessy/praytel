## Option C: Pick-mode flag plus a Recorder hook   (stance: minimal change)
Summary: `tui.Model` gets one optional `Recorder` interface, a `picking` flag and a span cursor kept as a `selectionKey`. In pick mode, keys go to the cursor and the recorder instead of the search. The pick panel takes the place of the footer's help line. Everything else (picks, the Volatile check, Ctrl-W checks, the D8 calls) lives in a new TUI-free `internal/record` package behind that interface.

How it works:
- **`internal/render/tui/record.go` (new, about 100 lines):** `type SpanRef string` (an opaque `selectionKey`) and `type Recorder interface { Header() string; Panel(v CheckView, at int, width int) []string; Key(v CheckView, ref SpanRef, at int, key string) string; Write(v CheckView, idx map[SpanRef]int) string }`. Also an unexported `checkViewIndexed()` that returns `CheckView` plus ref→index, built from D1's `toCheckSpan` seam. `TUI.WithRecorder(r)` adds the recorder, so `tui.New` and `cmd/praytel/run.go:70-71` stay as they are.
- **`tui.go` (about +150 lines):** the new fields `rec Recorder`, `picking bool`, `cursor string` and `recStatus string`. Every new branch is guarded by `m.rec != nil`.
  - **Ctrl-T** turns pick mode on or off. It works in the tree view only, and the graph view (`tui.go:245-252`) shows "pick works in tree view".
  - **Keys in pick mode:** ↑/↓ (today `tui.go:264-270`) move the cursor over `treeLayout.spans` (`tui.go:720`), so the search typed before Ctrl-T still narrows what the cursor visits. The view scrolls to keep the header line in sight. Enter focuses the panel. Runes go to `rec.Key` instead of the `default:` search insert (`tui.go:302-307`). Esc leaves the panel, then pick mode.
  - **Ctrl-W** calls `rec.Write` whether or not pick mode is on.
  - **Drawing:** the cursor is drawn as reverse video on the header in `renderSpan`. `rows` styles spans on every frame (`tui.go:791-807`) and the layout cache does not key on the cursor, so the cursor does not invalidate the cache. `contentHeight` (`tui.go:511-513`) subtracts the panel height. `panelKey` (`tui.go:545-554`) gains `picking`, `cursor`, `recStatus` and a recorder version.
- **`internal/record` (new, about 300 lines):**
  - **Picks:** stored by `SpanRef` and resolved to `Index` only at Ctrl-W, because canonical indices shift as spans arrive.
  - **Panel:** rows show raw `CheckSpan` values. Rows that `praytest.Volatile` flags are greyed out with the reason. A status tick on a `Closed=false` span is refused.
  - **Ctrl-W:** `selector.Select` runs on each picked selector and gives "matches N spans" if N≠1. A ref missing from the index (evicted) is refused. Then `Generate` → `SelfCheck` → `WriteNew`, with `StillRunning` read from D2's `childRunning`.
  - **Import rules:** `record` imports `tui`, `praytest` and `selector`. Nothing imports `record` except `cmd/praytel`. `tui` must not import `praytest`, because D8's `Generate(…, tui.CheckView)` would create an import cycle.
- **D7 seam:** move the `CheckSpan` struct to `internal/domain` and keep `type CheckSpan = domain.CheckSpan` in `tui`. `selector` then takes it without importing `tui`.
- **Rollout:** this is dead code until `--record` (D10) calls `WithRecorder`.

Steelman: The S1 flow needs only one new idea: a mode in which keys mean something else. This design reuses the viewport, the search, the cache and the footer, and adds no screen. With `rec == nil` no new branch runs, so plain `--tui` behaves byte for byte as today and the existing `tui_test.go` tests prove it. The recorder's logic is headless and is tested with a fabricated `CheckView`. The TUI only learns "cursor, mode, hand keys to an interface".

Pros: smallest change to `tui.go`. One renderer and one fold. No import cycle. Picks survive live reordering. Search and Tab work unchanged before Ctrl-T.

Cons / failure modes / hidden costs:
- **Two-level focus:** span cursor, then Enter into the panel. This adds Esc juggling, and changing the search means leaving pick mode first.
- **Cost of `CheckView()`:** each call is O(n log n). It is recomputed when a key is pressed, and at most once per tick when `version` changed while the panel is open. Large live traces make the open panel lag.
- **Cursor and viewport:** live arrivals move lines, so the cursor can drift off screen until the next move.
- **`tui.go` grows** to about 1550 lines, with its mode logic spread across `update`.
- **Ctrl-W outside the terminal:** raw mode delivers Ctrl-W as a key, but browser and VS Code terminals or a tmux binding can catch it first.
- **Graph view:** no picking there.
- **Moving `CheckSpan`:** the D1 type moves into `domain`, which touches D1's file.

Criteria scores:
- No change outside record mode: 5 - every addition is behind `rec != nil`, and the `New` signature is unchanged.
- Correct Ctrl-W checks on live data: 4 - `Select` runs on a fresh view at write time, refs resolve then, and `childRunning` comes from D2. Unclosed spans have no special handling beyond the status-tick refusal.
- Coupling and growth of `render/tui`: 3 - the logic stays outside behind an interface, but `tui.go` gains a mode, a cursor and panel sizing.
- S1 fast and discoverable: 4 - this is the vision's sequence as written, and the footer lists the pick keys.
- Testable headless and deterministic: 4 - `record` is pure over `CheckView`. The cursor needs `Model.Update` tests like the existing ones.
- Key-binding conflict risk: 3 - runes are captured only in pick mode. The terminal-level Ctrl-W risk remains, and search is locked while picking.

Cost: M (low end)     Reversibility: easy

Unknowns / assumptions:
- Moving `CheckSpan` into `domain` is acceptable under D1. The alternative is for `selector` to define its own input type.
- D6's `Check` accepts the live view, as D8 requires.
- Lag from `CheckView()` with the panel open is tolerable at shop scale. No benchmark has been run.
- Whether VHS and target terminals pass Ctrl-W through is untested.
