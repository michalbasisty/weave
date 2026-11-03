---
name: weave explorer
description: The single page `weave explore` writes: a repository drawn as a transit network.
colors:
  ground-light: "#f5f6f8"
  surface-light: "#ffffff"
  sunk-light: "#eceff4"
  ink-light: "#131a26"
  ink2-light: "#3a4454"
  ink3-light: "#5a6474"
  rule-light: "#dbe0e8"
  ground-dark: "#0b111d"
  surface-dark: "#101827"
  sunk-dark: "#18233a"
  ink-dark: "#e8ecf3"
  ink2-dark: "#bcc5d4"
  ink3-dark: "#8f9bb0"
  shell: "#14233f"
  shell-dark: "#18294f"
  link-light: "#1d4bb0"
  link-dark: "#9cb9f5"
  line-screen: "#2a5cc4"
  line-endpoint: "#12805a"
  line-module: "#a8650a"
  line-table: "#8a3ea0"
  line-screen-dark: "#7aa2f2"
  line-endpoint-dark: "#4cc493"
  line-module-dark: "#e3a64c"
  line-table-dark: "#c992dc"
  gate-fail: "#b4261a"
  proof-ok: "#17723f"
typography:
  display:
    fontFamily: "Segoe UI Variable Display, Segoe UI, system-ui, -apple-system, Helvetica Neue, Roboto, Arial, sans-serif"
    fontSize: "1.75rem"
    fontWeight: 650
    lineHeight: 1.15
    letterSpacing: "-0.015em"
  body:
    fontFamily: "Segoe UI Variable Text, Segoe UI, system-ui, -apple-system, Helvetica Neue, Roboto, Arial, sans-serif"
    fontSize: "0.9375rem"
    fontWeight: 400
    lineHeight: 1.5
  small:
    fontFamily: "Segoe UI Variable Text, Segoe UI, system-ui, sans-serif"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1.45
  code:
    fontFamily: "Cascadia Mono, Cascadia Code, Consolas, ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1.45
rounded:
  control: "6px"
  ring: "50%"
spacing:
  s1: "4px"
  s2: "8px"
  s3: "12px"
  s4: "16px"
  s5: "24px"
  s6: "32px"
  s7: "48px"
components:
  tab-current:
    backgroundColor: "{colors.shell}"
    textColor: "#ffffff"
    typography: "{typography.body}"
    height: "56px"
  station:
    backgroundColor: "{colors.surface-light}"
    textColor: "{colors.ink-light}"
    typography: "{typography.small}"
    rounded: "{rounded.ring}"
    size: "16px"
  row-current:
    backgroundColor: "{colors.sunk-light}"
    textColor: "{colors.ink-light}"
    typography: "{typography.body}"
    padding: "12px 16px"
  next-item:
    backgroundColor: "{colors.ground-light}"
    textColor: "{colors.ink-light}"
    typography: "{typography.body}"
    size: "44px"
  proof-panel:
    backgroundColor: "{colors.surface-light}"
    textColor: "{colors.ink-light}"
    typography: "{typography.small}"
    rounded: "{rounded.control}"
    padding: "24px"
---

# Design System: weave explorer

## Overview

**Creative North Star: "The line map."**

The repository is a transit network. Screens, endpoints, modules and tables are four
lines; every screen, endpoint, module, table, change and test is a station. The page's
job is wayfinding, not reporting: a person should be able to see the network's health,
read what changed since the last commit, take the first numbered next step, follow a
line to one item, and come back by the breadcrumb.

The shell is a navy signboard. The content is porcelain in light, deep navy in dark. The
four line colours are the only saturated colour in the page, and they appear only where
they mean a line: the strip on Today and on Map, station rings, and the item's trace.
Status rides on top of that world and nowhere else: red belongs to what `weave check`
fails, green ticks to proof that exists. Everything that is merely missing proof stays
neutral, because a gap is information, not alarm.

The rejected earlier version was a flat sidebar of seven equal views over an off-white
ledger, with a coloured chip or amber label on every row, and no breadcrumb on desktop.
It was legible line by line and impossible to navigate. This world replaces it: four
places instead of seven, a permanent "you are here", and the product's one idea drawn as
the map itself instead of four unconnected text columns.

**Key Characteristics:**
- Four places: Today, Map, Changes, Checks. A breadcrumb on every view.
- One line diagram reused at every scale: strip, list lane selector, item trace.
- Navy signboard, porcelain or navy ground, four line colours, red reserved for gate failures.
- System humanist sans for reading; monospace only for routes, tables, paths, counts and commands.

## Colors

A restrained transit palette: cool neutrals carry the page, four line hues carry the map,
two status colours carry the gate.

### Primary
- **Cobalt line** (#2a5cc4 light, #7aa2f2 dark): Screens. The first station of every trace.
- **Signal green line** (#12805a / #4cc493): Endpoints. Also the hue of "tested", because a
  green endpoint line and a green tick mean the same thing: a path that exists and is checked.

### Secondary
- **Amber line** (#a8650a / #e3a64c): Modules. Warm, never used for warnings.
- **Violet line** (#8a3ea0 / #c992dc): Tables. Deliberately not scarlet, so red keeps a single meaning.

### Neutral
- **Porcelain ground** (#f5f6f8): the page. **Surface** (#ffffff) for panes and the proof panel;
  **sunk** (#eceff4) for a selected row and hover.
- **Ink** three steps (#131a26 / #3a4454 / #5a6474 light): primary, secondary and tertiary text.
  The tertiary step measures 5.5:1 on both the ground and the surface, so 12–13 px secondary
  text keeps a margin above AA rather than sitting on the line.
- **Rules** (#dbe0e8, #e8ebf0): one weight, one pixel. Structure comes from rules and space,
  never from cards inside cards.
- **Signboard navy** (#14233f light, #18294f dark): the header only. In dark it sits one step
  above the ground so the signboard still reads as a separate surface.

### Named Rules
**The Four Colours Rule.** Saturated colour means a line of the map, and nothing else. A new
feature does not get a new hue; it uses one of the four or stays neutral.
**The One Red Rule.** Red (#b4261a / #f2907f) marks only what `weave check` would fail, plus the
"still called by" line for a removed endpoint. Missing tests, missing e2e and changed
requirements are printed in ink, and labelled.

## Typography

**Display Font:** Segoe UI Variable Display (with Segoe UI, system-ui)
**Body Font:** Segoe UI Variable Text (with Segoe UI, system-ui)
**Label/Mono Font:** Cascadia Mono (with Cascadia Code, Consolas, ui-monospace)

**Character:** one humanist UI family borrowed from the operating system, because the page must
work offline and ships no webfont. Monospace is a tool, not a costume: it is used for the things
a person copies or compares character by character, and for nothing else.

### Hierarchy
- **Display** (650, 1.75 rem / 28 px, 1.15): the repository name on Today, a list heading.
- **Headline** (600, 1.375 rem / 22 px, 1.3): an item name on its page.
- **Title** (600, 1.125 rem / 18 px, 1.3): section headings inside a page.
- **Body** (400, 0.9375 rem / 15 px, 1.5): prose and rows; measure capped at 70–72 ch.
- **Small** (400, 0.8125 rem / 13 px, 1.45): row metadata, relation words, hints.
- **Micro** (600, 0.75 rem / 12 px): counts, method verbs, table numbers. Tabular figures on
  every number that sits in a column.

### Named Rules
**The Mono-Is-a-Path Rule.** Monospace marks a route, an endpoint, a table, a file path, a count
or a command. A name a person reads (a change title, a station label, an interface stamp) is set
in the sans, even when it sits beside one.

## Layout

The shell is a grid of three rows: the signboard (56 px), the breadcrumb bar (44 px), and the
panes. On Today the single pane is a 1180 px column. On Map, Changes and Checks it is list plus
item: a 400 px list that owns its scroll, and a detail pane that owns its own. Both panes carry
`min-height: 0`, so the grid — not the document — scrolls them; selecting an item never moves the
header.

Today's first viewport, in order: the repository name, one gate sentence, the line strip (four
stations with counts, each linking into the Map), then "Since the last commit" and a numbered
"Next up" on the left with a Proof panel on the right.

Spacing runs 4 / 8 / 12 / 16 / 24 / 32 / 48. A section is separated by 32 px and a one-pixel rule;
more space above a heading than below it. Corners: 6 px on controls and inputs, 50 % on station
rings, nothing else. No shadow anywhere; depth is the signboard, the rule and the sunken row.

Below 1100 px the Proof panel moves under the work list and the trace folds to two columns; below
900 px the shell becomes a document that scrolls normally, the header becomes two rows (brand, search,
theme; then the four places), the panes become one column where selecting an item replaces the list,
and the trace turns vertical. At 375 px nothing scrolls sideways and every row wraps its own text.

## Elevation & Depth

Flat, with two layers only: the signboard above the ground, and the surface above the ground for
panes and the proof panel. Separation is 1 px rules and background steps, never shadows. A selected
row is a sunken fill plus a filled station ring; it is never a coloured border.

## Shapes

Circles are the page's one shape, because circles are stations. A ring is 12 px in a list row, 16 px
in a breadcrumb-less item heading, 26 px on the line strip and 34 px where the item itself sits as
the interchange. Rings are drawn with a 3–8 px stroke in the line's colour, hollow when the thing is
not the subject, filled when it is selected or done. Everything else is a rectangle with a 6 px
radius at most; lists and sections have no container at all.

## Components

### Signboard
- **Shape:** full-bleed navy bar, 56 px tall, no radius.
- **Tabs:** four places, each a 44 px target; current is white text plus a 3 px white underline in
  the bar's own colour, never a pill.
- **Search:** 38 px field in the bar, search glyph at the left, `/` keycap at the right; 44 px and
  16 px text on a phone.
- **Theme:** one button cycling Auto → Light → Dark, icon plus the word on desktop, icon only on a phone.

### Breadcrumb
- **Shape:** a 44 px bar on the surface, one line, `repo › Map › Endpoints › GET /api/v1/tasks`.
- **Behaviour:** the last two crumbs stay on a phone — the parent becomes the back control with a
  chevron, the current crumb stays visible. A selected item always has a crumb, so the way back is
  never hidden.

### Line strip
- **Shape:** four stations on one line; each station a ring on the segment, a bold count and the line
  name under it, the relationship word between (`call`, `handled in`, `use`).
- **Behaviour:** every station is a link to that lane of the Map; the current lane is a filled ring.
  On a phone the counts stack under the rings and the relationship words are dropped.

### Rows (screen, endpoint, table, change, check)
- **Shape:** a ring, a name, an optional aside (tested tick, `2/8`), and one or two metadata lines;
  52 px minimum, one-pixel rule between rows, no card.
- **States:** hover sunken; current sunken with a filled ring and a 600-weight name.
- **Endpoints:** method and path are two columns, so a path that wraps stays under the path.

### Trace (signature)
- **Shape:** one line through four stations: Screens → Endpoints → Modules → Tables. The item's own
  station is the interchange, a heavy 8 px ring, and each station heading states the relation in
  words (`Calls 14 endpoints`, `Handled in 8 modules`, `Reaches 22 tables`).
- **Behaviour:** six references per station, then "Show all N"; horizontal above 900 px, vertical
  below, where the track runs down the left. Never fewer than the four stations.

### Next up
- **Shape:** a numbered list; the number is a 26 px navy circle, a failure is a red circle. Each item
  shows the change, its next task, the date and progress; a bar on the right.
- **Order:** gate failures, then the live task of each change in progress, then test gaps, then changes
  whose only open tasks are deferred (hollow number, "only deferred tasks left").

### Proof panel
- **Shape:** one surface box, 340 px, 24 px padding, 6 px radius; two meters and a three-row readout
  of what the index could parse.

## Do's and Don'ts

### Do:
- **Do** put the gate sentence and the line strip in the first viewport of Today; nothing above them.
- **Do** give every view a breadcrumb and every selected item a visible current row.
- **Do** state a relation in words (`Calls these endpoints`) instead of an edge name.
- **Do** keep counts exact and tabular; the page prints only what it measured.
- **Do** say why a section is empty and what would fill it.

### Don't:
- **Don't** add a fifth place or a coloured border to mark selection; the ring and the sunken row do it.
- **Don't** use red, amber or a warning icon for missing proof.
- **Don't** use monospace for interface text, dates or labels.
- **Don't** hide the way back on desktop, and don't let the header scroll away with a list.
- **Don't** put a card around a list or a card inside a card.

## Navigation and state

Four places, each with a URL: `#today` (the default), `#map/screens`, `#map/endpoints`,
`#map/modules`, `#map/tables`, `#changes`, `#checks`, plus `#node/<encoded id>` and `#search`.
Back, forward and a pasted link all work. The old hashes (`#now`, `#summary`, `#screens`, `#api`,
`#data`, `#problems`) are mapped onto the new places. An item's place follows its kind, so
`#node/…` opens a screen under Map, a change under Changes and a broken call under Checks.

Search is global, in the signpost, and grouped by kind in the list. `/` focuses it, `Enter` opens
the first match, `↑`/`↓` walk the list, `Esc` clears the search, then the selection, then leaves the
item. The project switcher appears only when the file holds more than one repository.

## Accessibility

- Native `<a>`, `<button>`, `<input>`, `<details>`; the four places are links with `aria-current`,
  not tabs with fake roles. The current breadcrumb is `aria-current="page"`.
- `:focus-visible` draws a 2 px ring in the link colour, and in the signboard ink on navy.
- Every ring is `aria-hidden`; the meaning it carries is in the adjacent text (`tested`, `no direct
  test`, `done`).
- Counts and status are text, never colour alone: "no direct test" is words next to an ink label,
  and the checks list prints a sentence when nothing is broken.
- 44 px targets on a phone; the list, search and tabs are all reachable by keyboard.
- Contrast: secondary text is at least 5.5:1 on both the ground and the surface in both themes; line
  colours are used as strokes and rings, and every piece of text they touch is ink.
- `prefers-reduced-motion` removes the 150 ms colour transitions; there is no other motion.
- At 375 px there is no horizontal scroll: paths wrap, method and path are two columns, and the
  trace becomes a vertical line.

## Accepted debt

- `innerHTML` re-render per interaction. Measured fine at 2 000 nodes; revisit above roughly 10 000.
- The "Since the last commit" delta needs git and a baseline index; on a large repo the first build
  of that baseline costs a few seconds (cached per commit under `.weave/baselines/`).
- List rows are rendered per view, so a style check on the landing view alone misses most of the page.
- Descriptions come from source comments, so a banner comment can land on the wrong node; the page
  shows them as prose, never quoted evidence, and they are truncated at the index, not by the page.
- The trace is two hops either way; a table reached through four modules shows the modules and their
  endpoints, not the transitive closure.

## Research log

- **Route:** the owner rejected the previous page twice ("ui still looks ugly and unreadable by
  human", "easily I can get lost in it"), so this is a redesign, not a refinement: product truth,
  payload and capabilities were kept, the visual world and the information architecture were replaced.
- **Critique of the previous page** (two reviewers, before any change): 21/40 on Nielsen's heuristics;
  no "you are here" on desktop, the trace drawn as four unconnected text columns, alarm colour on
  missing tests, seven flat views with overlapping content, and deferred "Later: …" work ranked as
  next. One detector finding (`monotonous-spacing`, a token-scale false positive); contrast passed AA,
  but the tertiary ink cleared it by only 0.4.
- **Direction:** a code-led round over seven grounded directions; the transit-diagram wayfinding
  direction beat the assigned card-catalogue direction on both audience identification and product
  clarity, and the user chose it. The catalogue's insight (extent as tab size) and the timetable's
  (one row size, rank by weight) survive as tab counts and the flat row scale.
- **Evidence:** the 761-file QR-gent render at 1440 and 375 px in both themes, a 390 px capture, the
  detector run, and the finish review over those captures.
