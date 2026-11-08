# D9: How does record mode extend the trace TUI: span cursor, pick panel, key handling, and where recorder state lives?
Status: decided     Depends on: D1, D2, D7, D8     Parent: none
Vision refs: S1 steps 2-5 and edge cases; §5 "Recording is today's TUI", Pipeline ("one renderer"); M1, M3

## Context
- The TUI has no span cursor today: ↑/↓ and PgUp/PgDn scroll the viewport (`internal/render/tui/tui.go:264-278`). S1 step 3 needs the user to "move to `Gateway.Charge`".
- Every rune key inserts into the search query (`internal/render/tui/tui.go:302-307`), so S1's Space, `s` and `a` only work inside a mode that captures them. Ctrl-T and Ctrl-W are unbound (`internal/render/tui/tui.go:233-301`; `docs/reference/praytel.1.md:72-84`). Some terminals and users treat Ctrl-W as delete-word.
- The footer is `renderPanel` with a cache keyed on its inputs (`internal/render/tui/tui.go:1273-1335`); S1 step 2 adds `RECORD <file> · Ctrl-T pick · Ctrl-W write` and step 5 a `wrote ...` message.
- `spanView` holds component, name, status, status message, times, attributes (`internal/render/tui/tui.go:53-70`); whether the pick panel reads from it or from D1's model depends on D1.
- `tui.New` takes events, stdin, stdout and a filter (`internal/render/tui/tui.go:1358-1369`; constructed at `cmd/praytel/run.go:70-71`). The model's `done` reflects event-channel close (`internal/render/tui/tui.go:217-219`), not child exit; the S1 WARNING needs child-running state from D2's lifecycle.
- Ctrl-W must refuse when a selector matches more than one span "in this run" (S1 edge case), which needs D7's matcher over the data the TUI holds.
- There is a graph view (Ctrl-G, `internal/render/tui/tui.go:245-252`, `graph.go`); whether picking works there is not stated.
- CONTRIBUTING: no spans in the TUI model's hot paths (`CONTRIBUTING.md:37-40`). Deterministic model tests exist (`internal/render/tui/tui_test.go`).
- M1: S1 fits in ≤ 60s in the VHS recording.

## Goals
- `praytel --tui` behaves byte-for-byte as today when not recording.
- The S1 flow (search, Ctrl-T, move, Enter, Space/`s`/`a`, Ctrl-W) works while the child runs and after it exits.
- Recorder logic (picks → D8 input, Ctrl-W checks) is testable without a terminal.

## Non-goals
- New screens beyond the pick panel (§5).
- Editing assertion values or absence selectors inside the TUI (S1 step 4 "for the user to edit").
- Ticking absence of an attribute (S1 edge case).

## Decision criteria (weighted, sum 100)
| Criterion | Weight | Why it matters (trace to vision/constraint) |
|---|---|---|
| No change to existing TUI keys/behaviour outside record mode | 20 | §5 "nothing to relearn"; S1 step 2 |
| Correct Ctrl-W checks on live data (cardinality, still-running) | 20 | S1 edge cases |
| Coupling and growth of `render/tui`; one renderer per process | 20 | ARCHITECTURE Render; §5 Pipeline |
| S1 flow fast and discoverable | 15 | M1 |
| Recorder logic testable headless and deterministic | 15 | M3 generator test; M2 |
| Key-binding conflict risk (search runes, terminal Ctrl-W) | 10 | S1 steps 3-5; §5 |

## Options
Full texts: `../options/D9-A.md`, `D9-B.md`, `D9-C.md`. Scores below are the decider's after checking the cited code; explorers' self-scores in brackets where changed.

### Option A: Inspector-style pick mode with docked panel and live file preview
Recorder state on `tui.Model` behind a nil pointer (`record.go` in `render/tui`); span cursor keyed by `selectionKey`; Enter docks a side/bottom panel of checkboxes plus a live preview of the bytes `Generate` would write; five function hooks (`ChildRunning`, `Volatile`, `Preview`, `Write`) bound in `cmd/praytel` to break the `praytest → tui` import. Cost M; about 500-700 lines in `render/tui`.

### Option B: Wrapper model in `internal/record` with a search-driven pick pane; tree untouched
A separate Bubble Tea `Root` model wraps the unchanged `tui.Model`, shrinks it with a synthetic `WindowSizeMsg`, and renders a bottom pane listing `CheckView().Spans` filtered by `inner.Query()`, then an attribute stage. Only `Query()` and a shared `RunProgram` are added to `tui`. Picks keyed by span content (service, name, start, attrs). Cost M.

### Option C: Pick-mode flag plus an optional `Recorder` interface
`tui.Model` gains `rec Recorder`, `picking`, a `selectionKey` cursor and a status string, all behind `rec != nil` (~100-line `record.go` + ~150 lines in `tui.go`). Picks, volatility, Ctrl-W checks and D8 calls live in TUI-free `internal/record`. Moves `CheckSpan` to `internal/domain` with a type alias in `tui`. Graph view refuses picking. Cost M (low end).

### Verified claims and corrections (path:line)
- **Import graph (point 1).** D8 fixes `Generate(rec, tui.CheckView)`, so `praytest → tui`; D12 S2 already plans `var _ func(Selector, tui.CheckSpan) bool = Selector.Matches` (`D12-watch-extension-seams.md:57`), so `selector → tui`. Neither creates a cycle as long as **`tui` imports neither**. A cycle only appears if recorder logic that calls `selector`/`praytest` sits inside `render/tui`. Therefore:
  - A's five hooks are indirection whose only purpose is to keep A's in-`tui` recorder from importing `praytest`. Confirmed as a real cost, not a necessity of the problem.
  - B's and C's placement of recorder logic in `internal/record` (imports `tui`, `selector`, `praytest`; imported only by `cmd/praytel`) breaks the cycle by construction.
  - **C's move of `CheckSpan` to `internal/domain` is unnecessary** once recorder logic is outside `tui`: `selector → tui` is acyclic. Not adopted; no D1 or D7 text needs amending. (D1 already accepts "the test runner imports `internal/render/tui`, and with it bubbletea" under "We give up".)
- **TUI search is trace-level, not span-level.** In `searchTraces` mode a query keeps whole traces in which any span matches (`tui.go:703-714`, `tui.go:776-779`); individually matching spans get a highlight (`spanHighlights`, `tui.go:1123-1134`, via unexported `matchSpan`). B's "after typing `Charge` the list has one to four rows" requires filtering `CheckView().Spans` by the query, i.e. a second implementation of `matchSpan` over `CheckSpan`, which drifts from what the tree shows. Discounted.
- **B's "only an accessor" is understated.** `Run` builds `NewModel` inside itself (`tui.go:1371-1390`), so B also needs a `RunProgram` extraction; shrinking the inner model with a synthetic `WindowSizeMsg` (`tui.go:228-231`) interacts with `relayoutGraph` in `Update` (`tui.go:200-203`) and is unverified. B's record line sits below the TUI's own footer, contradicting S1 step 2 ("The status line adds ...").
- **Cursor does not invalidate the layout cache (A, C).** `rows` styles visible spans on every frame through `renderSpan` (`tui.go:791-807`, `tui.go:827-844`), and the layout cache key is version/query/mode. A reverse-video cursor and a pick badge can be drawn there at no cache cost. Confirmed.
- **Panel height.** `contentHeight` is `height-3` (`tui.go:511-513`); subtracting an open panel's height is a one-line change. Confirmed.
- **Footer cache.** `panelKey` (`tui.go:545-554`) must gain the record fields, or a record status change would not redraw. Confirmed (C states it; A states it for one field only).
- **Control keys already arrive.** The TUI already binds `ctrl+h`, `ctrl+k`, `ctrl+j`, `ctrl+b`, `ctrl+f` (`tui.go:258,264,268,279,289`), so it already relies on raw-mode delivery of C0 control keys; raw mode turns off the tty's WERASE, so the terminal driver does not eat Ctrl-W. Bubble Tea v1.3.10 (`go.mod:8`) names 0x17 `ctrl+w`; its source is not in the local module cache, so that is **unverified here** and becomes a model test plus the VHS check.
- **Enter is unbound today** (no `enter` case in `tui.go:233-308`; the default branch only takes `tea.KeyRunes`), so using Enter in record mode changes nothing outside it.
- **Eviction.** `ForestEvicted` deletes a whole trace from `m.spans` (`tui.go:382-384`, `tui.go:457-468`). Picks keyed by `selectionKey` (A, C) or by content (B) then resolve to nothing at Ctrl-W. All three mention it; none specifies what the user can do next. Specified below.
- **Preview cost (A).** A preview regenerates `Generate` over a full `CheckView` (O(n log n) per D1) on every pick change and every tick in which `version` changed. That is the per-tick full fold D1 warns against, for a feature S1 does not ask for (D8 left it optional).
- **Recompute cost (point 6).** The panel only needs the one span under the cursor. D12 S1 already mandates an unexported `toCheckSpan(*spanView) CheckSpan` (`D12-watch-extension-seams.md:56`), which costs O(k log k) for k attributes. C's interface passes the whole `CheckView` to `Panel`/`Key`, which would force a full `CheckView()` per key press or per tick while the panel is open. Corrected below: the full view is built **only at Ctrl-W**.

### Scores (weight × score / 5)
| Criterion (w) | A | B | C | Chosen hybrid |
|---|---|---|---|---|
| No change outside record mode (20) | 4 | 5 | 5 | 5 |
| Ctrl-W checks on live data (20) | 4 | 4 [5]: content identity; evicted pick just "missing" | 4 | 5 |
| Coupling / growth of `render/tui` (20) | 2 | 3 [4]: second span-row renderer and second search matcher; `RunProgram` extraction | 4 [3]: logic is outside `tui`; only cursor, mode and a 6-method interface inside | 4 |
| S1 fast and discoverable (15) | 4 [5]: two-pane split cramped at 80x24; preview beyond S1 | 3 [4]: no highlight in tree; record line off the status line | 4 | 5 |
| Headless testable (15) | 4 | 5 | 4 | 4 |
| Key-binding conflict (10) | 3 | 4 | 3 | 4 |
| **Weighted /100** | **70** | **80** | **82** | **91** |

## Decision
Chosen: **Option C as the base, with four concrete changes**:
- no `CheckSpan` move;
- the panel is built from one span via D12's `toCheckSpan`, and the full `CheckView` is built only at Ctrl-W;
- Ctrl-T in the graph view switches to the tree (from A);
- Ctrl-W outside pick mode asks for Enter to confirm.

Eviction and in-flight writes are also specified. Specification:

1. **Packages and imports.**
   - `internal/render/tui/record.go` (new, about 120 lines): the interface, the opaque ref, the cursor helpers and the panel frame. `tui` imports nothing new (no `selector`, `praytest`, `record`).
   - `internal/record` (new, about 350 lines + tests): the record entry (item 10), picks, tick rules, Ctrl-W checks, the D8 calls. Imports `tui`, `selector`, `praytest`, `domain`. Only `cmd/praytel` imports it.
   - `CheckSpan` stays in `internal/render/tui` exactly as D1 defines it. `selector` imports `tui` for it (as D12 S2 already assumes). D9 does not change `toCheckSpan`, `CheckSpan`, `Selector.Matches` or `Select`; it only calls them.
   - Enforcement: add a depguard rule, with D4's depguard task, denying `internal/praytest`, `internal/selector` and `internal/record` in `**/internal/render/tui/**`.
2. **Interface** (in `tui`):
   ```go
   type SpanRef string // opaque; the span's selectionKey; never printed, never reaches a file
   type Recorder interface {
       Header() string                                            // footer segment: "RECORD gift.praytest.star · Ctrl-T pick · Ctrl-W write"
       Badge(ref SpanRef) string                                  // pick marks drawn after a span header, "" if none
       Rows(sp CheckSpan) int                                     // number of field rows for sp
       Panel(sp CheckSpan, ref SpanRef, row, width int) []string  // one line per field row; row is the panel cursor
       Key(sp CheckSpan, ref SpanRef, row int, key string) string // "space", "s", "a"; returns a status line ("" = none)
       Write(v CheckView, idx map[SpanRef]int) (status string, work func() string)
   }
   func (t TUI) WithRecorder(r Recorder) TUI     // tui.New and cmd/praytel/run.go:70-71 unchanged
   func (m Model) WithRecorder(r Recorder) Model // for model tests
   ```
   `sp` comes from `toCheckSpan(m.spans[ref])` (D12 S1; `Parent`/`Trace = -1`), O(k log k) per call. `idx` comes from an unexported `checkViewIndexed()` built on the same extraction as D1's `CheckView()`.
3. **State on `Model`** (all nil-guarded; with `rec == nil` no new branch runs and the existing `tui_test.go` tests pin today's behaviour): `rec Recorder`, `picking bool`, `panelOpen bool`, `cursor string` (a `selectionKey`), `cursorLine int` (fallback position), `panelRow int`, `recStatus string`, `confirming bool`, `writing bool`. `panelKey` gains `picking`, `panelOpen`, `recStatus`, `confirming`, `writing` and `rec.Header()`.
4. **Keys in record mode, outside pick mode.** Everything works as today (search, Tab, Ctrl-G, arrows scroll, Esc clears search). New: **Ctrl-T** enters pick mode; **Ctrl-W** writes as in item 8. The help line is prefixed with `rec.Header()` (S1 step 2).
5. **Pick mode, cursor stage.**
   - Entry (Ctrl-T): if the view is the graph, switch to the tree (`m.view = viewTree`, scroll reset) first. The cursor starts on the **first span in layout order that the search highlights** (`spanHighlights`, `tui.go:1123`), else the first visible span. With no spans the status reads `no spans yet` and pick mode is not entered. With S1's `Charge` query the cursor usually lands on `Gateway.Charge` with zero moves.
   - ↑/↓ (and `ctrl+k`/`ctrl+j`) move the cursor to the previous/next entry of `cachedTree().spans` and scroll so its header is on screen. PgUp/PgDn still scroll the viewport. Enter opens the panel. `a` toggles an absence pick on the cursor span. Esc leaves pick mode (picks kept). Ctrl-G leaves pick mode (picks kept) and toggles the view.
   - **Search is locked while picking**: runes go to `rec.Key`, and Backspace/←/→/Tab do nothing. To change the search: Esc, edit, Ctrl-T again. The search typed before Ctrl-T still narrows what the cursor visits, because the cursor walks the filtered layout.
   - If the cursor span disappears from the layout (eviction, log reassignment in `searchLogs` mode), the cursor snaps to the span now at `cursorLine`. Live arrivals may push the cursor off screen until the next move; accepted.
   - The cursor header is drawn in reverse video in `renderSpan`, and `rec.Badge(ref)` (for example `[2✓ 1s]`, `[absent]`) is appended to any picked span's header, so picks stay visible in the tree.
6. **Pick mode, panel stage.**
   - Placement: the panel is drawn between the tree and the footer, height `min(rows+2, contentHeight/2)`, and `contentHeight` subtracts it, so at 80x24 the tree keeps at least 10 rows.
   - Line 1 is drawn by `tui` from `spanView`: `payments/Gateway.Charge  12ms  span 3fa1c2…  (duration, times and IDs cannot be ticked)`. IDs and times never reach the recorder.
   - The remaining lines come from `rec.Panel`: one row each for `status` and `status_message`, then attributes by key, as `[✓][s] card.type = "gift"`.
   - Keys: ↑/↓ move rows, Space toggles assert, `s` toggles selector, `a` toggles absence, Esc returns to the cursor stage.
   - Tick rules, enforced by the recorder and refused with the reason on the status line:
     - attributes on `praytest.Volatile`: `looks volatile: port`;
     - array, map and bytes values: `not recordable in v0` (D7/D8);
     - a `status`/`status_message` assert or a `status` selector on a span with `Closed=false`: `span still open; its status is not final` (D8 item 6);
     - `s` on `status_message`: `not a selector field` (D7 item 1);
     - asserts on a span marked absent, and the reverse: `clear the absence pick first`.
7. **Footer.** While picking: `PICK ↑/↓ span • enter fields • a absent • esc back • ctrl+w write`. In the panel: `space assert • s selector • a absent • esc back • ctrl+w write`. `recStatus` replaces the help line until the next key press; the data-pressure warning keeps its current place.
8. **Ctrl-W.**
   - With zero picks: status `nothing picked; Ctrl-T to pick`. Nothing else happens.
   - **In pick mode: writes immediately** (S1 step 5 unchanged).
   - **Outside pick mode with picks: asks first**: `write gift.praytest.star (1 case, 2 assertions)? enter writes · any other key cancels`. Enter writes; any other key cancels and is consumed. Pick mode has no text entry, so the delete-word habit can only fire outside it, where the confirm catches it. A repeated Ctrl-W (deleting several words) cancels.
   - While a write is in flight, Ctrl-W shows `writing…` and is ignored.
   - **Write path**, step by step:
     1. In `Update`: `v, idx := m.checkViewIndexed()`, one O(n log n) call bounded by D1's 25ms-at-10k revisit. Then `status, work := rec.Write(v, idx)`.
     2. `record.Write` reads D2's `childRunning` and exit code **at this moment**, so the WARNING matches the view written.
     3. It drops picks whose ref is missing from `idx`. If any were dropped, it returns `not written: 1 picked span was evicted under data pressure and has been unpicked (payments/Gateway.Charge); Ctrl-W again writes the rest` and no work.
     4. For every non-absence picked span it runs `selector.Select(v.Spans, box.where(selector marks))`. If N ≠ 1 it refuses with S1's `selector matches N spans in this run; mark an attribute with s to pick one`.
     5. It builds D8's `Recording` (`StillRunning`, `ExitCode`, and `PickedSpan.Index` from `idx`).
     6. `work` runs as a `tea.Cmd` off the update loop. It calls `praytest.Generate`, then `SelfCheck` (which runs D6/D12's `(*Case).Check(ctx, CheckView) CheckResult` on the live view), then `WriteNew`. It returns `wrote gift.praytest.star (1 case, 2 assertions[, 1 absence to edit])` or `not written: <reason>`, delivered as a `recordedMsg`.
   - Picks are kept on every refusal. After a successful write, a further Ctrl-W is refused by `WriteNew` (`exists; refusing to overwrite`).
   - Ctrl-W works the same while the child runs and after it exits (`m.done` plays no part).
9. **Tests.**
   - `internal/record`: table tests over fabricated `CheckSpan`/`CheckView` values for every tick rule, the cardinality refusal, eviction, the still-running WARNING and D8's status text, with no terminal.
   - `internal/render/tui`: model tests with a fake `Recorder` drive the full S1 key sequence, including `tea.KeyMsg{Type: tea.KeyCtrlW}` and `KeyCtrlT`, the confirm path, the graph-to-tree switch, and the cursor snap after `ForestEvicted`. The existing tests, unchanged, prove that `rec == nil` is byte-identical.
   - VHS (`make docs`) records S1 and must pass Ctrl-T/Ctrl-W through.
10. **Record entry** (the hand-off D10 assigns to D9, `D10-cli-dispatch-runner.md:102,145`): `record.Run(ctx, lt, cfg)` in `internal/record` receives D10's parsed record-mode config and runs these steps:
    1. It checks that FILE does not exist (`praytel: gift.praytest.star exists; refusing to overwrite`, exit `1`) before any child starts. `WriteNew` stays the backstop.
    2. It runs D2's `runCase` with the TUI, `Isolate=false`, and `OnStart`/`OnExit` setting `childRunning` and the exit code.
    3. It builds the `Recorder` and calls `tui.New(...).WithRecorder(r)`.

    The exit code follows the decision tree's default (the child's code, as `--tui`).

Why it wins on the criteria:
- **No change outside record mode (20), 5:** every branch is behind `rec != nil`, `tui.New` and `run.go` are unchanged, and Enter/Ctrl-T/Ctrl-W are unbound today.
- **Ctrl-W on live data (20), 5:** one fresh `CheckView` and the child state are read in the same `Update` call, and D7's `Select` runs on that view. Evicted picks and open spans have specified, recoverable outcomes, and D8's self-check is the backstop.
- **Coupling (20), 4:** about 270 lines in `render/tui` (A needs 500-700), no hooks, and no test code imported into the renderer. One renderer, one fold, one search matcher. Not 5: `tui.go`'s `update` still gains a mode.
- **S1 fast/discoverable (15), 5:** the vision's sequence as written. The cursor starts on the highlighted match, the footer lists the keys, and badges show picks in the tree.
- **Headless testable (15), 4:** all rules sit in pure `internal/record`; cursor behaviour needs `Model.Update` tests in the existing style.
- **Key conflict (10), 4:** runes are captured only in pick mode, and the confirm catches the delete-word habit. Browser/IDE interception of Ctrl-T/Ctrl-W remains (see Revisit).
- **Reversibility: easy.** Only the keys documented in `praytel-test.1` are public; the interface is internal.

We give up:
- **Picking in the graph view.** Ctrl-T switches to the tree.
- **Editing the search while picking.** Esc, edit, Ctrl-T.
- **A uniform Ctrl-W.** Outside pick mode it needs Enter; inside pick mode it writes at once.
- **A's live file preview.** The user sees the file only after writing; the self-check guarantees it passes against the run.
- **B's zero-touch `tui.go`** and its short filtered span list.
- **Silently surviving eviction.** An evicted pick costs one refusal, after which the user re-picks or accepts the rest.
- **Cursor stability under live arrivals.** The cursor stays on its span but can scroll off screen until the next move.
- **Protection where the terminal host takes the key.** Browser-based terminals reserve Ctrl-T/Ctrl-W (new/close tab), and some IDE terminals may bind them. praytel cannot see those presses.

Rejected:
- A: puts recorder state and a two-pane layout into `render/tui` (500-700 lines) behind five cycle-breaking hooks, and adds a per-tick full-view preview S1 does not ask for.
- B: needs a second span-row renderer and a second search matcher (TUI search is trace-level, `tui.go:703-714`), shows no cursor in the tree, and moves the record line off the status line contrary to S1 step 2.
- C as written: moves `CheckSpan` to `internal/domain` for a cycle that does not exist once recorder logic is outside `tui`, passes the whole `CheckView` to the panel on every key press, and refuses the graph view instead of switching.

Revisit if:
- **M1:** the VHS S1 recording exceeds 60s, or the guide walkthrough needs more than 5 cursor presses to reach the target span in S1 or S2. Then add `n`/`N` jump-to-next-highlight in the cursor stage, or reconsider B's filtered list.
- **Key interception:** 1 report that Ctrl-T or Ctrl-W never reaches praytel in a supported terminal (browser-based, VS Code, tmux config), or VHS fails to deliver them. Then add documented alias keys (function keys) for pick and write.
- **Accidental write:** 1 report of an unintended write from pick mode. Then require the Enter confirm in every mode.
- **Ctrl-W latency:** the model test bench shows `checkViewIndexed` + `Select` in `Update` above 50ms at 10k spans. Then move the view build into the `tea.Cmd` over a copied span set.
- **Coupling:** record-mode code in `render/tui` exceeds 400 lines, or 2 recorder changes in one quarter need edits to `tui.go`. Then move to B's wrapper shape around the unchanged model.
- **Eviction:** any dogfooding or M1 session hits the eviction refusal. Then expose `--max-spans` on `praytel test`.
- **Graph picking:** 2 issues ask to pick from the graph view.

Consequences for other decisions:
- **D1 (decided):** consistent, no amendment. Record reads `(*Model).CheckView()` only at Ctrl-W, through `checkViewIndexed`, which is the same extraction plus a ref→index map. The panel uses per-span `toCheckSpan`. `CheckSpan` stays in `internal/render/tui`.
- **D2 (decided):** consistent. The recorder reads `childRunning` and, once the child has exited, its exit code at Ctrl-W (D8 already asks D2 for the code via `OnExit`).
- **D7 (decided):** takes the "recorder logic outside `internal/render/tui`" branch of D7's D9 consequence. No TUI-free span type is introduced.
- **D8 (decided):** consistent. Uses `Volatile`, the open-span refusal, `Recording`, and `Generate` → `SelfCheck` → `WriteNew` with D8's status text. No live preview.
- **D12 (decided):** consistent. `toCheckSpan`, `Selector.Matches` and `Check(ctx, CheckView) CheckResult` are called unchanged. `toCheckSpan` gains a v1 caller (the pick panel). S2's compile assertion keeps `tui.CheckSpan`.
- **D4 (decided):** its depguard task adds the `render/tui` deny rule in item 1.
- **D10 (decided):** consistent. D10 hands a parsed record-mode config to D9's record entry (`record.Run`, item 10). D9 owns the FILE-exists pre-check before any child; the suffix and `--` checks stay in `ParseTest`.
- **D11 (open):** the VHS S1 recording is the M1 check and must exercise Ctrl-T and Ctrl-W.
- **Docs:** `praytel-test.1` lists the pick keys, the Enter confirm outside pick mode, and the browser-terminal caveat for Ctrl-T/Ctrl-W.
- **Vision:** no text changes. The design follows S1 steps 2-5 as written. Graph switching, the search lock and the confirm outside pick mode are behaviour the vision leaves open; they are documented in `praytel-test.1`.

## Amendments after Phase 4 critique
- **Item 10 signature note:** `record.Run` cannot take `cmd/praytel/internal/args.Config` (Go's internal-package rule forbids `internal/record` importing `cmd/praytel/internal/args`). It takes `record.Config{File, Argv, Stdin, Stdout}` plus an injected `record.RunFunc`; D10 item 7's `args.Config` (TUI on, root `MaxSpans`/`MaxLogLines` defaults) is built in `cmd/praytel` and captured by the closure. The decision itself (entry lives in `internal/record`, lifecycle injected) is unchanged. Logged by the orchestrator.
