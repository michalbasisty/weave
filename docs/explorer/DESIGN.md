# Explorer: design contract

Scope: the single page `weave explore` writes, rendered by `internal/weave/render.go`
(CSS and JS embedded from `internal/weave/assets/`). It is for people; agents use the
MCP server. One HTML file, no framework, no build, no network.

## 0. Research log

- **Route**: an existing app shell (list + detail) with no design system and no
  component layer, and the owner asked for a redesign, so the Design System Gate's
  second branch applies: extract this contract first, then rebuild the surface.
  Read `redesign-skill.md`, `layout-skill.md`, `perfection/README.md`.
- **No Layer B brand**: the brief named no brand and explicitly rejected the generic
  Next/shadcn dashboard look, so type and colour below are set by role, not borrowed.
- **Evidence**: an Opus 5.5 review of the renderer (then `src/render.ts`, now
  `internal/weave/render.go`) and the 937-file QR-gent render, a 375 px baseline, and
  screenshots of every view before and after.

## 1. Direction

A **drafting sheet**: warm paper, ink, hairline rules, serif for human prose,
monospace for machine names, and four lane colours used as markers, never as fills.
No cards, no shadows, no gradients, no pills, no emoji, no stat tiles.

The page has one idea to teach: **a screen calls an API endpoint, the endpoint is
handled in a module, the module uses tables.** Everything is organised around that
line; the "trace" strip in every detail view is the signature element.

The opposite trap is also the brief: an eight-tab dashboard with nine colours and a
red tag on every row. Colour is a signal, not decoration; red is reserved for what
`weave check` would fail on.

## 2. Tokens

| token | light | dark |
|---|---|---|
| `--paper` (page) | `#f4f1e8` | `#15171b` |
| `--panel` (raised row) | `#faf7ee` | `#1c1f24` |
| `--ink` / `--ink2` / `--ink3` | `#1b1a17` / `#46433a` / `#6b675c` | `#e8e4da` / `#b6b1a5` / `#8c877c` |
| `--rule` / `--rule2` | `#d9d3c3` / `#e7e1d2` | `#2d3138` / `#23262b` |
| `--screen` (Screens lane) | `#2f5597` | `#93b3e8` |
| `--api` (API lane) | `#1b6b5a` | `#7fc7b6` |
| `--module` (Module lane) | `#8a5a17` | `#d8ad6f` |
| `--data` (Tables lane) | `#6b3f74` | `#c6a4cf` |
| `--err` (gate failure only) | `#a3271a` | `#e28c7e` |

Type: one display step (28 px serif), a 17–22 px serif heading scale, 15 px body,
13 px small, 12 px micro (**hard minimum**). Roles, not fonts by accident:

- **serif** `Charter, "Sitka Text", "Iowan Old Style", Cambria, Georgia, serif` —
  descriptions, the summary sentence, lede, page titles. This is human text.
- **mono** `ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono"` —
  routes, endpoint names, table names, file paths, dates, counts. This is code.
- **sans** `system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial` —
  chrome only: navigation, labels, buttons, list metadata.

Every stack carries its full fallback chain. A single-family `font:` shorthand is a
defect: unknown keywords fall back to the browser serif.

Spacing: 4 / 8 / 12 / 16 / 24 / 32. Radius: 2 px, used only on controls and inputs.
No `box-shadow` anywhere.

## 3. Layout and scroll ownership

- Shell: `body` grid `48px | 1fr`; below it `rail 236px | main`.
- The rail owns its own scroll on wide screens and never scrolls the page.
- `main` is list-detail: the list is its own scroll container, the detail pane is
  another; each has `min-height: 0` so the grid can shrink them.
- Summary and search results are single-pane: `main.solo`, list hidden.
- Below 900 px: the rail becomes a wrapped nav strip above the content, the two panes
  become one column, and selecting a node replaces the list with its detail. The
  detail carries a `← Back to <view>` link. Focus moves to the detail heading.
- No horizontal scrolling of primary content at 375 px; long identifiers wrap
  (`overflow-wrap: anywhere`), paths truncate.

## 4. Primitives

| primitive | states |
|---|---|
| **rail link** | rest, hover (panel), current (`aria-current="page"`, 2 px ink rule), count, one-line gloss |
| **row** (screen / endpoint / table / change) | rest, hover, current (`aria-current="true"`, 3 px ink rule); kind-coloured refs inside |
| **trace lane** | four fixed columns (Screens, API endpoints, Module, Tables); the selected item's lane is raised; > 8 items collapse into "+ N more" |
| **ref** | a text link with a 4 × 10 px lane marker; hover shifts to the lane colour; no border, no pill |
| **group header** | sticky over its list (module in API, In progress / Done in Changes) |
| **detail** | kind label, mono title, copyable path button, serif description, trace, sections |
| **section** | "Proved by" (tests), "Planned in" (change, tasks, requirements), "Reach" (one sentence), plain relation groups |
| **problem group** | neutral; only `kind === 'error'` takes red |

Relations never use edge names. The vocabulary is fixed in `SAY` in
`internal/weave/assets/explorer.js` (for example `calls` → "Calls these endpoints" /
"Called from these screens",
`uses` → "Uses tables" / "Used by modules", `in_module` → "Handled in module").

## 5. Navigation

- Six views only: Summary, Screens, API, Data, Changes, Problems. Flows merged into
  Screens, Modules became API group headers, Risk became the "Reach" line, Issues
  became Problems.
- URL hash is state: `#summary`, `#api`, `#node/<encoded id>`. Back/forward work and
  a node is linkable without a list.
- Search is global and lives at the top of the rail: results grouped by kind, any
  node reachable. `/` focuses it, `Esc` clears it or clears the selection.
- No auto-selection; an empty pane explains the current view and lists its five most
  connected items.

## 6. Accessibility

- Every control is a native `<button>`, `<a>`, `<input>` or `<details>`; nav links
  carry `aria-current`, never fake tab roles.
- `:focus-visible` ring on every interactive element; the copy path is a button and
  announces through `aria-live`.
- Arrow keys walk the list only when focus is inside it, so the detail pane keeps its
  own scrolling.
- `prefers-reduced-motion` removes the 120 ms colour transitions. There is no other
  motion.
- Contrast: inks and lane hues are checked against both grounds; red never carries
  meaning alone (the problem group has a text label too).

## 7. Accepted debt

- `innerHTML` re-render per interaction. Measured fine at 2 140 nodes; revisit above
  roughly 10 000.
- Descriptions come from source comments, so a banner comment can land on the wrong
  node. The page shows them as prose, not as quoted evidence.
- Counts in the trace are two hops only; a table reached through four modules shows
  the modules and their endpoints, not the whole transitive closure.
- Table links depend on `prisma.<model>` usage in module code. Where a codebase hides
  its ORM behind a wrapper, the Data lane stays empty and `.weave/patterns.json` is
  the only escape hatch.
