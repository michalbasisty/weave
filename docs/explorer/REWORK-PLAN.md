# Explorer rework: UI, UX, and the human features

Status: **plan**. Nothing here is implemented yet. The four measured quick wins,
`weave spec new`, `weave spec task done`, and the Go port are already shipped.

Goal: make `weave explore` something a person opens without being taught, and make
the map useful beyond reading: see workflows, see the app itself, edit the spec, and
let a janitor check a change against it. Stitch is the design instrument, not the
shipped runtime: the explorer stays one embedded HTML file.

## 1. What is good, and what still hurts

Keep:

- the screen → API → module → table chain, the four-lane trace, plain-language
  relations, hash URLs, global search, dark mode, the 375 px back link.

Still weak, in the order a new user meets it:

1. **The Summary explains the map, not the first action.** It is dense: trust,
   problems, coverage, work in flight, all at once. A newcomer needs three doors:
   find a screen, find an endpoint, see what is broken.
2. **Search is hidden in the rail.** It is the fastest action on the page and the
   least visible control.
3. **Detail has no shape.** Trace, proved-by, planned-in and reach are stacked
   without a header, tab or fold. Long relation groups become a wall.
4. **No memory of the session.** No breadcrumb, recently viewed, back/forward
   controls, saved filter, or pinned comparison.
5. **No compare.** The CLI has `delta`; the page cannot show what a branch changed.
6. **No screens and no workflows.** The map explains structure, not the app.
7. **Specs are read-only.** A person who spots a missing task must leave the page.
8. **Payload weight.** 859 KB and 5,694 edges on 937 files; fine now, heavy for 5k.
9. **Accessibility details.** Focus management between list and detail, palette
   keyboard support, and contrast of small labels need a formal pass.

## 2. Stitch as the design instrument

The Stitch MCP exposes the workflow we need; use it to explore layout and
interaction, then translate the chosen screen into the embedded assets.

1. `stitch.create_project` — one project, `weave-explorer`.
2. Write `docs/explorer/stitch/DESIGN.md` in the Stitch design-system format from
   the current contract (§3 below), then `stitch.upload_design_md`.
3. `stitch.create_design_system_from_design_md` — tokens: paper/ink, four lane
   hues, serif for prose, mono for identifiers, hairline rules, 2 px radius, no
   cards, no shadows.
4. `stitch.generate_screen_from_text` for three screens:
   - **Summary / start here** (desktop 1440): three doors, live counts, problem
     strip, in-flight changes.
   - **Node detail** (desktop 1440): trace, then tabs for Relations, Tests,
     Spec, Files; one Reach line.
   - **Find** (mobile 375): palette/filter, results grouped by kind, detail as a
     bottom sheet with a back control.
5. `stitch.generate_variants` — two alternatives for the Summary and the detail
   header; choose one, not an average.
6. `stitch.download_file` — screenshots and HTML as references under
   `docs/explorer/stitch/`; never ship Stitch HTML. The CSS/JS assets and the
   golden HTML stay the implementation.
7. Review the chosen screens against the current page at 1440 / 768 / 375, both
   themes, before writing CSS.

Stitch's default bans (no Inter, no neon glow, no pure black, no 3 equal cards)
agree with this page. One conflict: Stitch bans serif in dashboards. This is not a
dashboard; it is an editorial map. If Stitch cannot hold the serif/mono split, use
a distinctive modern serif (Fraunces) for display and keep the body sans, rather
than dropping to Inter.

## 3. Design direction to hand to Stitch

**Atmosphere:** a drafting sheet on warm paper, measured (density 5/10), calm
motion (3/10), asymmetric only where it helps scanning. Not a SaaS dashboard.

| token | value |
|---|---|
| paper / panel | `#f4f1e8` / `#faf7ee` |
| ink / secondary / muted | `#1b1a17` / `#46433a` / `#6b675c` |
| rules | `#d9d3c3`, `#e7e1d2` |
| screen / API / module / data | `#2f5597` / `#1b6b5a` / `#8a5a17` / `#6b3f74` |
| error | `#a3271a` only for gate failures |
| type | Charter/Sitka/Cambria serif for prose; SFMono/Menlo/Consolas for identifiers; system sans for chrome; 12 px floor |
| shape | 2 px radius on controls only; hairlines; no card, no shadow |

Every screen must answer, in this order: where am I, what is this, what is
connected, what do I do next.

## 4. UX changes

1. **Start here.** Replace the current Summary first block with three actions:
   *Browse screens*, *Find an endpoint*, *See what is broken*, each with its live
   count. The sentence, trace example, trust line and coverage move below as
   supporting evidence, not the opening pitch.
2. **Command palette.** `/` or `Cmd/Ctrl+K` opens one field for nodes, views and
   actions (copy path, toggle theme, compare a ref, open spec). It replaces the
   rail-only search and is discoverable in the header.
3. **Session memory.** Breadcrumb above the detail, `←` / `→` in the header,
   *Recently viewed* in the rail, and saved filters/views in `localStorage`.
4. **Detail with tabs.** Sticky header (kind, name, path, one-line description),
   then tabs: **Overview** (trace + Reach), **Relations**, **Tests** (command
   first), **Spec** (change, tasks, requirements), **Files**. Each relation group
   gets a count and its own fold; no group ever exceeds eight visible rows.
5. **Compare mode.** Pick a git ref or a saved graph; show added / removed / moved
   nodes from `delta` and highlight them in the list and trace. This is the human
   version of `weave delta`, and it is the reason to open the page on a branch.
6. **Empty, loading, stale.** A real skeleton for the first paint; an explicit
   "index is N minutes old, rebuild" action; empty states that say what to do
   rather than apologising for the filter.
7. **Mobile.** Sticky search, list/detail with a bottom-sheet detail and a
   predictable back gesture, 44 px targets, no horizontal scrolling.
8. **Accessibility.** Focus moves to the detail heading on selection, returns to
   the list on back; palette traps focus; tabs are real tabs with arrow keys;
   `aria-live` for copy and rebuild; contrast checked on both themes.

## 5. The human features

### 5.1 App-level specs and workflows

New convention, additive to `specs/changes/`:

```md
# specs/app/workflows.md

## REQ-APP-1  A manager can see which orders wait for approval
WHEN an order changes state, THE system SHALL notify the approver.

## Workflow: warehouse intake
1. /warehouse
2. POST /api/v1/orders
3. /tasks/:id
```

- Parse `REQ-*` headings into app requirements (`meta.app`, `meta.area`), hashed
  like the change requirements so drift marks `@req` code suspect.
- Parse `## Workflow:` steps into `workflow` nodes; resolve routes to pages and
  `METHOD /path` to endpoints; edges `workflow → page|endpoint`.
- `context(files)` adds the workflows and app requirements that touch the file.
- Explorer: an **App** view with requirements and workflows; a workflow step
  links into the page/endpoint detail.
- `weave reqs` lists app and change requirements with implementation and
  evidence status.

### 5.2 Spec editing

CLI-first, because the explorer is a file. `spec new` and `spec task done` shipped.

Next: `spec task add <change> "<title>" --files a,b`, `spec app new <area>`,
`spec open <change>` (opens `$EDITOR`/`code`), and `spec link <REQ> <file>` to add
`@req`. An optional `weave studio` (localhost, edit + preview) is a later phase,
not a requirement for this rework.

### 5.3 Screen images

- `.weave/screens.json`: `{ "/warehouse": "screens/warehouse.png" }`, relative to
  the project root. The explorer embeds the image in the page detail, above the
  trace; the manifest is validated by `weave screens --check`.
- Optional `weave screens --capture` uses Playwright only when the project already
  has it; it never becomes a runtime dependency. Without it, supplied images still
  work.

### 5.4 Janitor

`weave janitor [--against ref] [--run] [--json]` and MCP `janitor` in the full
server (not `--manager`):

1. Deterministic first: `check`, touched endpoints without tests, changed files
   not named by any change, suspect requirements, app requirements with no
   evidence, endpoints no app spec or workflow mentions.
2. Only ambiguous diffs go to a decision model, with a typed question:
   `match` / `drift` / `needs_human` and a confidence. Below 0.75, report but do
   not pass or fail automatically.
3. Write `.weave/janitor.md` and `.weave/janitor.json`; exit 1 on errors or a
   confident drift verdict, 2 on configuration problems.
4. `--run` executes only the test commands the graph names, never the whole suite.
5. `internal/judge` is an interface: `RulesJudge` (default), `AskJudge` (existing
   OpenAI-compatible chat model, strict JSON), and `JevJudge` (TypeSafe Jev: one
   choice question, calibrated probability). Jev needs `TYPESAFE_API_KEY` and its
   request schema; until both exist, the rules + Ask path ships.

## 6. Phases and acceptance

| # | Phase | Deliverable | Acceptance |
|---|---|---|---|
| 0 | Stitch concepts | 3 screens + 2 variants + screenshots under `docs/explorer/stitch/` | one chosen and reviewed at 1440/768/375, light/dark |
| 1 | Shell, start, palette | new Summary, command palette, breadcrumb, recent | keyboard-only path from open to a node; 36 matrix clean |
| 2 | Detail, compare | tabs, folded groups, delta mode | golden HTML regenerated; no relation group over 8 rows |
| 3 | App specs, workflows, screens | parser, App view, workflow steps, image manifest | fixture test builds a workflow; image shown in page detail |
| 4 | Spec editing | `task add`, `app new`, `open`, `link` | integration tests; no file overwritten |
| 5 | Janitor | CLI + MCP + reports + judge interface | exit 1 on a failing requirement and on confident drift; 0 on a clean change |
| 6 | QA and release | visual QA, a11y pass, goldens, README | 36 matrix, keyboard, contrast, `go test ./...`, tagged release |

Estimate: 8–12 focused days. Phases 1–2 deliver the visible UX win; 3–5 deliver
the product expansion.

## 7. Risks

- **Stitch is a reference, not a runtime.** Generated HTML cannot be shipped into
  the single embedded page without breaking the zero-dependency, byte-golden
  contract. Translation is manual and must preserve the CSS/JS assets.
- **Design drift.** The page is already distinctive; a generic Stitch default is a
  regression. The tokens and bans above are the contract.
- **Payload growth.** App specs, workflows and images add weight. Cap lists,
  lazy-load images, and measure the page size before and after.
- **Spec format is a commitment.** App specs are a new convention; keep them
  additive and optional, so repos without `specs/app/` behave exactly as today.
- **Model privacy.** The janitor sends facts and possibly diff summaries to a
  provider; document what leaves the repo and keep a rules-only mode.

## 8. Decisions this plan assumes

- Spec editing: **CLI first** (studio later if wanted).
- App specs: **`specs/app/*.md`** with `REQ-*` and `## Workflow:` sections.
- Screens: **manifest first**, optional Playwright capture.
- Janitor: **rules + existing `ask` model**, with a Jev adapter behind
  `TYPESAFE_API_KEY`; no model is required for the deterministic checks.

Change any of these before phase 1; they change the work, not the direction.
