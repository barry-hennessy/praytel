## Option A: Inspector-style pick mode (span cursor, docked checkbox panel with live file preview)   (stance: most conventional)

Summary: Work like Playwright's pick-locator and inspector and Tracetest's "select a span, get a span signature" flow. Ctrl-T starts a modal pick mode with a highlighted span cursor. Enter docks a panel listing the span's fields as checkboxes, with a live preview of the file Ctrl-W would write below them. The recorder state sits on the TUI `Model` behind a nil pointer. Anything that would cause an import cycle (generation, volatility, cardinality) is passed in as hooks from `cmd/praytel`.

How it works:
- **Wiring.** `tui.New` (`internal/render/tui/tui.go:1358-1369`) takes a variadic `WithRecord(RecordOptions{File string; ChildRunning func() bool; Volatile func(string, domain.Value) (string, bool); Preview func(CheckView, []Pick) ([]byte, error); Write func(CheckView, []Pick, running bool) (string, error)})`. `cmd/praytel` binds these to `praytest.Volatile/Generate/SelfCheck/WriteNew` and to D2's `childRunning`. Hooks are needed because `praytest` imports `tui` (D8 `Generate(rec, tui.CheckView)`), so `tui` cannot import it. `Pick` mirrors D8's `PickedSpan`, but it is keyed by `selectionKey` instead of `Index`.
- **State.** `Model.rec *recorder`, in new `record.go`, holds `picks map[string]*Pick`, `cursor string` (a selectionKey, not a row), `panelOpen bool`, `row int` and `status string`. When `rec == nil`, every new branch is skipped.
- **Keys.** At the top of `update`'s `tea.KeyMsg` case (`tui.go:232`), if `rec != nil && rec.picking`, keys go to `pickUpdate` and never reach the rune default (`tui.go:302-307`). Cursor mode: ↑/↓/j/k move between `treeLayout.spans` and scroll to keep the cursor on screen, Enter opens the panel, `a` toggles absence, Esc leaves pick mode. Panel mode: ↑/↓ move between rows, Space toggles assert, `s` toggles selector, Esc closes. Ctrl-T in graph view switches to the tree view first. Ctrl-W is bound in every mode, and only when `rec != nil`.
- **Render.** The cursor is drawn in `rows` (`tui.go:791-807`) as a reverse-video header. The `treeLayout` cache key is unchanged. When the panel is open, `View` (`tui.go:471-509`) splits horizontally: the tree takes 60% and the panel takes the rest, or the panel takes the bottom half below 100 columns. Rows look like `[a][s] card.type = "gift"`. Duration, times, IDs and volatile keys are greyed, with their reason. `status` on an open span shows `span still open` (D8 item 6). The preview is regenerated on pick changes and on `version` changes, at most once per tick. The footer gets `panelKey.record string`, which is empty when not recording (`tui.go:544-554`).
- **Ctrl-W.** This builds `CheckView` plus a parallel slice of keys (unexported `checkViewKeys`, D1 item 5), maps picks to indices, and returns a `tea.Cmd` that calls `Write`. `Write` runs D7's matcher per pick and refuses with S1's `selector matches 2 spans in this run; ...`, then runs SelfCheck and the link write. A `recordedMsg` sets the status line to `wrote ...` or `not written: ...`.

Steelman: Every interaction maps to a pattern users have already seen in the codegen inspector or Tracetest: cursor, inspect, tick, preview, accept. The preview is the bytes `Generate` would write, so there are no surprises at Ctrl-W, which is what makes accepting safe. The cursor is anchored by span key, so streaming spans do not make it jump. Recorder state reads the model's own spans with no second fold (D1). Gating everything on `rec == nil` makes "unchanged outside record mode" a reviewable property that existing tests already pin.

Pros: S1 is discoverable from footer hints alone. Picks survive moving between spans. Tick rules (volatile, open span) are visible before the user picks. Model tests drive the whole flow with fake hooks.

Cons / failure modes / hidden costs:
- `render/tui` grows by roughly 500-700 lines: cursor, two-pane layout, panel, preview, hooks. Five function hooks are indirection that exists only to break an import cycle.
- The live preview goes beyond S1 step 3 and edges toward a "new screen" (non-goal). At 80x24, the side-by-side split leaves the tree cramped.
- Esc becomes modal: it clears the search outside pick mode and leaves pick mode inside it.
- If the user's habit is to press Ctrl-W to delete a word in the search box, they trigger a write instead. An empty pick set refuses, but a partial pick set writes, and WriteNew then blocks any retry.
- When a trace is evicted (`tui.go:382-384`), its picks have to be dropped with a notice, or Ctrl-W maps them to nothing.
- Preview regeneration costs O(n log n) per tick on large forests (`CheckView` copy).

Criteria scores:
- No change outside record mode: 4 - nil-guarded; touches `update`/`View`/`panel` code paths that stay byte-identical by test.
- Correct Ctrl-W checks on live data: 4 - fresh `CheckView`, `childRunning` and D7 matcher at keypress; eviction needs care.
- Coupling and growth of `render/tui`: 2 - the largest UI addition in the package, plus hooks.
- S1 flow fast and discoverable: 5 - familiar inspector shape, hints and preview.
- Recorder logic testable headless: 4 - deterministic model tests with fake hooks; checks are pure in `praytest`.
- Key-binding conflict risk: 3 - the modal capture solves runes; Ctrl-W habit and overloaded Esc remain.

Cost: M     Reversibility: easy (keys documented in `praytel.1` are the only public commitment)

Unknowns / assumptions:
- I assume `checkViewKeys` can be added beside D1's `CheckView` without exporting IDs.
- I assume bubbletea in raw mode delivers Ctrl-W in tmux and common terminals (not verified).
- Whether the preview fits the M1 VHS 60s budget is untested.
