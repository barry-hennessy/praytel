## Option B: Wrapper model with a search-driven pick pane; the tree stays untouched   (stance: radical alternative)
Summary: Premise questioned: "add a span cursor to the live tree". Instead a separate `internal/record` Bubble Tea model wraps the unchanged `tui.Model` in the same program and adds a bottom pick pane. The pane lists the spans that match the tree's current search; picks are made there, never in the tree.

How it works:
- `tui.Model` is already an exported `tea.Model` (`internal/render/tui/tui.go:143,163,199,471`). The only change to `render/tui` is a `Query() string` accessor, plus D1's `CheckView()`. No new keys or cursor in `tui.go:233-308`.
- New `internal/record`: `Root{inner tui.Model; pane *pickPane; st *State; running *atomic.Bool}`. All non-key messages go to `inner`, so the live tree keeps updating while the user picks. In pick mode, `inner` gets a `WindowSizeMsg` with about 60% of the height. The pane renders below it, followed by one record line (`RECORD f · Ctrl-T pick · Ctrl-W write` / `wrote ...` / `not written: ...`).
- Keys: `Root` takes `ctrl+t` (toggle the pane) and `ctrl+w` before `inner` sees them. In the pane's list stage, ↑/↓ move the pane cursor. Runes, Backspace and Esc go to `inner`, so there is one search, and the list is `CheckView().Spans` filtered by `inner.Query()`, indented by `Parent` depth. Enter opens the attribute stage, where Space marks assert, `s` marks selector, `a` marks absent, and Esc goes back. Untickable rows (time, duration, `praytest.Volatile`, status on open spans) show the reason (D8 item 4, 6).
- `State` is plain Go: picks keyed by span identity (Service, Name, Start, attrs). At Ctrl-W it takes a fresh `inner.CheckView()` and maps each pick to an index. It then calls `selector.Select` and refuses with S1's `matches N spans` message if any selector matches more than one span. Otherwise it builds `praytest.Recording` (`StillRunning` from D2's `childRunning`) and calls `Generate`/`SelfCheck`/`WriteNew`.
- Wiring: `run.go:70-71` builds `record.New(...)` when `--record` is set. `TUI.Run`'s program setup (`tui.go:1371-1390`) moves into a shared `tui.RunProgram(model)`.

Steelman: The live tree needs no cursor, mode flags or rune remapping, so not-recording is byte-for-byte today's TUI by construction. The search the user already typed becomes the pick list: after typing `Charge`, they press Ctrl-T and the list has one to four rows, which is faster than walking a tree cursor. All recorder logic sits outside `render/tui`, which removes D7's import-cycle concern. It is testable as a pure model fed a synthetic `CheckView`.

Pros: no regression surface in `tui.go`. Space/`s`/`a` are only live in the attribute stage, so they never collide with search. It works in both tree and graph views, and while the child runs.

Cons / failure modes / hidden costs:
- The tree does not highlight the picked span. The user correlates the pane row with the tree by eye.
- A second rendering of span rows (the pane list) can drift from the tree's labels.
- ↑/↓ mean "pane cursor" in pick mode and "scroll" otherwise. PgUp/PgDn still scroll the tree.
- The record line sits under the TUI's own footer, not inside it, which is a small deviation from S1 step 2.
- Identity keyed on Start breaks if a span is evicted before Ctrl-W. The pick is then reported missing.
- Ctrl-W still collides with terminals and multiplexers that grab it.
- Arguably a "new screen" beyond the pick panel (§5); it is framed as the panel.

Criteria scores:
- No change outside record mode: 5. The only change to `tui` is an accessor.
- Ctrl-W checks live: 5. It reads a fresh `CheckView` and `childRunning` at the keypress.
- Coupling / one renderer: 4. One program and a thin `tui`, but row rendering is duplicated.
- S1 fast/discoverable: 4. A short filtered list, but no highlight in the tree.
- Headless testable: 5. Pure model plus `State`.
- Key conflict: 4. Runes stay search; Ctrl-W risk remains.

Cost: M     Reversibility: easy (all internal; `tui` gains one accessor and one helper)

Unknowns / assumptions: Shrinking `inner` with a synthetic `WindowSizeMsg` is assumed to lay out cleanly (`clampScroll`, graph relayout). The graph view inside a 60% pane is unverified. Assumes span `Start` plus content is unique enough to identify a pick.
