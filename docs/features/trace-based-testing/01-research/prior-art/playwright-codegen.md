# Record-and-generate test workflows: Playwright codegen, Cypress Studio, snapshot/golden tools (checked 2026-11-08)
Category: adjacent (same mechanism: record session, emit editable test code, choose assertions, regenerate). Scope: Playwright codegen + aria snapshots, Cypress Studio, Jest, goldie, cupaloy, teatest, VHS.

## Does it implement the feature(s)?  partial
- Record a run then write a test file: yes (Playwright, Cypress Studio, VHS tape recording).
- User picks what to assert from the recorded artefact: yes for DOM (Playwright, Cypress); none for golden tools (whole output is asserted).
- Property-based "must not hold" alerts: no. Not found in any of these tools.
- Trace/span-level assertions: no. All operate on DOM or text output.

## Mechanism
- Playwright codegen: `npx playwright codegen <url>` opens a browser plus the Inspector; actions are recorded and shown as code to copy. VS Code "Record new" creates `test-1.spec.ts`; "Record at cursor" inserts into an existing test. [playwright.dev/docs/codegen, retrieved 2026-11-08] (official docs, STRONG)
- Locator choice is automatic: favors role, text, test ID; refines until unique. [same]
- Assertions: toolbar icons for assert visibility / text / value, then click an element. "Pick locator" shows an editable locator for a hovered element. [same]
- Aria snapshots: codegen "Assert snapshot" action; an "Aria snapshot" tab previews the accessibility tree for the selected locator before saving. [playwright.dev/docs/aria-snapshots, 2026-11-08]
- Cypress Studio: records click/type/check/uncheck/select into the spec as Cypress commands, appended to the end of the test. Extending an existing test replays it to its last command, then records. Selector priority: data-cy, data-test, data-testid, data-qa, name, id, class, tag, attributes, nth-child; configurable via `Cypress.ElementSelector`. Manual assertions via right-click on an element. [docs.cypress.io/app/guides/cypress-studio, 2026-11-08] (official)
- Cypress Studio AI: diffs DOM between interactions and suggests assertions, each with a comment. User hovers to see before/after snapshots, then accepts or rejects each one. Needs Cypress 15.11+ and a Cloud account. [same]
- Golden/snapshot tools: the whole output is stored on first or `-update` run, then byte- or text-compared.
  - goldie: `go test -update`, `-clean`, Go-template golden files via `AssertWithTemplate`. [github.com/sebdah/goldie, 2026-11-08]
  - cupaloy: first run auto-creates the snapshot via go-spew. `UPDATE_SNAPSHOTS=true` rewrites, and the updating run still fails. [github.com/bradleyjkemp/cupaloy, 2026-11-08]
  - teatest: `RequireEqualOutput` uses the system `diff`; `-update` flag; `WaitFor` polls (default 1s, 50ms interval); pre-release `exp/` module. [pkg.go.dev/github.com/charmbracelet/x/exp/teatest, 2026-11-08]
  - VHS: `vhs record > x.tape` captures a session into an editable tape. `Output golden.ascii` (or `.txt`) produces text goldens committed to git. [github.com/charmbracelet/vhs, 2026-11-08]

## User experience
- Record, then optionally pause or switch to assert mode, click an element, and get code. The generated file is plain code the user edits, so there is no hidden format. (Playwright and Cypress docs above.)
- Jest inline snapshots: `toMatchInlineSnapshot()` with no argument is filled in by Jest on first run, so the expected value lands in the test source. [jestjs.io/docs/snapshot-testing, 2026-11-08]

## Strengths
- Recording produces ordinary code in the project's language, which the user then edits. (Playwright, Cypress docs.)
- Preview before commit: the aria snapshot tab, and Studio AI's before/after hover. (docs above.)
- Partial matching reduces brittleness. Aria snapshots can omit the name or attributes, and list children match in `contain` mode by default. Text can use regexes. [aria-snapshots]
- Jest property matchers keep generated fields (ids, dates) out of exact comparison while all other fields stay exact. [Jest docs]
- Update-flow safety: Jest does not write snapshots on CI without an explicit flag (since Jest 20). cupaloy fails the run that updated. Playwright's default mode fails the run that created a snapshot. Playwright's `--update-source-method` can emit a patch applied with `git apply`. (docs above.)

## Limitations / known problems
- Cypress Studio: E2E only, no cross-origin, iframes or Shadow DOM; appends only to the end of a test; recording disabled while a test fails; AI suggestions are rate-limited and cloud-gated; animations make noisy diffs. [Cypress docs]
- Golden approaches need determinism. Timestamps or random values fail every run (cupaloy, Jest docs). Jest warns against reflexive regeneration, since it blesses bugs. The goldie and cupaloy docs do not mention this. Pixel snapshots vary by OS and hardware. [playwright.dev/docs/test-snapshots, 2026-11-08]
- Jest `-u` regenerates all failing snapshots unless narrowed with `--testNamePattern`. [Jest docs]
- teatest's `WaitFor` is timing-based and the module is unstable. [pkg.go.dev]
- Whole-output goldens assert everything, so unrelated change causes a failure. This is inferred from the mechanism, not stated in a doc: UNVERIFIED as a documented claim.

## Notable decisions & trade-offs
- Auto-chosen locators trade stability against readability: priority lists (role/testid first; nth-child last) are an explicit brittleness policy, and Cypress makes the order user-configurable. [docs above]
- Review-before-accept (Studio AI) versus update-everything (`-u`) is the main design split in the regenerate flow.
- Hard-coded vs. templated expectations: goldie templates and Jest matchers let volatile parts be declared as variable.

## Evolution
- Cypress Studio is labeled Beta, and the AI-assertion path requires 15.11+ (recent). Earlier Studio had no AI suggestions: UNVERIFIED for version history.
- Playwright added aria snapshots and the "Assert snapshot" codegen action. Release dates not checked: UNVERIFIED.

## Gaps
- No source or issue-tracker evidence gathered; everything is official docs (single source each), so no independent user opinion. All evidence is effectively MODERATE at best, not STRONG.
- Not checked: Playwright codegen internals (selector generator source), autocast/asciinema, teatest golden storage specifics, VHS golden brittleness (timing, fonts).
- No tool found that records command-line plus telemetry traces.

## Sources
- https://playwright.dev/docs/codegen - 2026-11-08 - official docs
- https://playwright.dev/docs/test-snapshots - 2026-11-08 - official docs
- https://playwright.dev/docs/aria-snapshots - 2026-11-08 - official docs
- https://docs.cypress.io/app/guides/cypress-studio - 2026-11-08 - official docs
- https://jestjs.io/docs/snapshot-testing - 2026-11-08 - official docs
- https://github.com/sebdah/goldie - 2026-11-08 - README
- https://github.com/bradleyjkemp/cupaloy - 2026-11-08 - README
- https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest - 2026-11-08 - package docs
- https://github.com/charmbracelet/vhs - 2026-11-08 - README
