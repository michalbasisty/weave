# weave

Your agent says it is done. This is how you check.

<img src="docs/img/lines.svg" alt="A repository drawn as four lines: 37 screens call 248 endpoints, handled in 36 modules, that use 60 tables." width="100%">

`weave check` is a gate: it exits 1 when a change removes an HTTP endpoint that
something still calls, when a call site matches no endpoint, or when a requirement's
test fails. No agent can argue its way past an exit code.

Around that gate sits a map of the repo, served to agents over MCP:

| | |
|---|---|
| `context(files)` | what this file is: its page, endpoints, tables, owning spec change, open tasks, and the exact tests + command to run |
| `plan(slug)` | the next unfinished task of a spec change, with its files and command |
| `impact(files)` | everything a change can reach |

### It tells you what it could not parse

Every extractor misses things. A tool that misses a route silently is worse than no
tool, because its confident answer is wrong. So coverage is measured and printed:

```
coverage
  route files   28/28 produced endpoints (100%)
  client calls  224/224 resolvable matched an endpoint (100%)
  routes        33/35 <Route> entries mapped (94%, 2 redirect)
  task files    87/87 parsed (100%)
  prisma        46/46 models mapped to tables
  next routes   12/12 route files produced endpoints (100%)
  next pages    14/14 page files produced pages (100%)
  tRPC routers  4/4 parsed (100%)
  GAPS          1 file looks like a contract but produced nothing:
    packages/backend/src/modules/x/dynamic.routes.ts
```

When a file looks like a route file and yields no endpoints, it is named. That list is
the honest boundary of every answer weave gives.

An index is rebuilt whenever a source file **or weave itself** changed, so an answer is
never from yesterday.

Zero dependencies, one static binary, one JSON file. No Node, no Python, no daemon.

## Install

```bash
# Go is only needed to build
go install github.com/michalbasisty/weave/cmd/weave@latest

# or download a release binary (linux, macOS, Windows; amd64 and arm64)
#   https://github.com/michalbasisty/weave/releases

# or from a checkout
go build -o weave ./cmd/weave
```

Go 1.22+ to build. The resulting binary has no runtime dependencies.

## Quick start

```bash
cd /path/to/your/project

weave            # what is this repo, and what can I do
weave init       # connect Claude, Cursor, VS Code, opencode
weave explore    # the map, in your browser
weave check      # what a change breaks (exit 1)
```

`weave` finds the project root by walking up from wherever you are, so it works from
any subdirectory. `--root` exists only for when that guess is wrong.

## Several projects

Every command registers the repo it ran in, so the list builds itself:

```bash
weave projects
```

```
name           size                                path
QR-gent         745 files · 196 ep ·  87 changes    G:\QR-gent
weave            25 files ·   5 ep ·  1 changes    G:\projekty\weave
```

Then work on any of them from anywhere — no `cd`, no paths:

```bash
weave --project qrgent plan 2026-10-19-ui-restyle
weave --project weave explore
```

Names match loosely (`qrgent`, `QR-gent` and `qr-gent` are the same), a unique fragment
is enough (`print`), and an ambiguous one is refused with the candidates listed.
`weave forget <name>` drops one.

**One MCP entry for all of them:**

```bash
claude mcp add weave -- /abs/path/weave mcp --all
```

Every tool then takes an optional `project` argument. Without `--all` the schema stays
lean and the server answers for the repo it was started in.

## Connect an agent, once

`weave init` writes MCP configs for every client it can, without touching files that
exist. If a project still has a TypeScript-era entry (`node …/src/cli.ts`), run
`weave init --upgrade`: it replaces only the `weave` entry and leaves the rest alone.

| client | file |
|---|---|
| Claude Code, Codex, anything reading `.mcp.json` | `.mcp.json` |
| Cursor | `.cursor/mcp.json` |
| VS Code / Copilot | `.vscode/mcp.json` |
| opencode | `opencode.json` (merged) |

It also appends `.weave/` to `.gitignore` and prints the `claude mcp add` line plus an
`AGENTS.md` snippet for agents that read instructions instead of MCP.

Manual, if you prefer:

```bash
claude mcp add weave -- /abs/path/weave mcp --root .
```

The absolute binary path is deliberate: an MCP client spawns the server with its own
`PATH`, and `weave` is not guaranteed to be on it.

## Tools

| tool | what it returns | size |
|---|---|---|
| **`context(files)`** | the pack to read *before* editing: page, module, endpoints with `file:line`, tables, owning spec change, open tasks, linked requirements, tests **and the command to run them**, broken call sites. `budget` caps it (default 350 tokens). `depth: "code"` adds up to three 15-line windows at the anchors, opt-in. | ~350 tokens |
| `impact(files)` | the complete blast radius: every module, endpoint, table, page, test, requirement, importers of the file, and the endpoints those importers call. | ~500 tokens |
| `search(query)` | endpoints, pages, modules, tables, requirements, tests by name. One line each. `text: true` scans source lines instead, graph files first. | ~150 tokens |
| `remember(files, note)` | keep a short note for the next session: a decision, a gotcha. `context` returns it, labelled as a note, never as evidence. | ~20 tokens |
| `recall(files)` | notes previously kept for these files, newest first. | ~60 tokens |
| `changed(ref)` | what the graph gained or lost since a git ref (default HEAD) or a saved graph: added/removed/moved endpoints, pages, tables, modules, requirements; a removed endpoint still called by the base is named with its caller count. | ~100 tokens |
| `changes(slug)` | a spec change record: tasks, open tasks, files they touch, and the blast radius of those files. With no slug: the newest 25 records with progress. | ~400 tokens |
| **`plan(slug)`** | the next unfinished task: its files, endpoints, tables, tests and the run command. Call this instead of reading `tasks.md`. | ~150 tokens |
| **`ask(question)`** | a cheap model answers from the graph in ≤5 lines and names the facts it used. For questions that need reasoning, not retrieval. | ~200 tokens |
| `status()` | graph health: counts, endpoints no page calls, endpoints no test covers, pages without e2e, phase timings, and this server's own call latency. | ~250 tokens |

Every tool answer ends with a receipt: the tokens it cost, the age of the graph and the
coverage it was built from. `weave gain` turns those lines into a ledger.

The server rebuilds a stale index before answering, so the map follows the agent's
edits; a burst of calls within 1.5 s reuses the in-memory graph.

`context` answers "I know the file, what is it?" · `plan` answers "I know the spec, what
do I do next?" · `impact` answers "I know the change, what else breaks?"

## CLI

```bash
weave                      # status, and what to do next
weave explore              # the map, in your browser
weave serve  [--port 7777] # host the map, the wiki and the actions on 127.0.0.1
weave context <file...>    # --budget 250; --depth code for a window at each anchor
weave impact  <file...>
weave plan    <change> [T3]
weave spec    new <name>       # create proposal.md, tasks.md, spec-delta.md
weave spec    task done <change> <T1>
weave change  [slug]
weave search  <query>
weave grep    <text>       # --regex, --limit; graph files rank first
weave delta   [ref]        # what the graph gained or lost (--from graph.json)
weave remember --note "…" <file...>
weave recall  [file...]
weave memory               # all notes (--clear)
weave gain    [--since N]  # tokens served over the last N days
weave check   [file...]    # no args = git diff; exit 1 on a broken contract
weave hook                 # check for a Stop hook: exit 2 and the failure on stderr
weave fires   [--since N]  # what the hook has seen: runs, failures, average time
weave evidence [REQ...]
weave doctor
weave perf   [--runs N]    # index timings over time, or N fresh runs
weave init
weave mcp
weave --help
weave --version
```

## Explorer (for people)

```bash
weave explore            # writes .weave/explore.html and opens it
weave explore --all      # every registered project, one file, with a switcher
```

One self-contained HTML file — 45 KB for this repo, ~720 KB for a 761-file one. No
server, no network, no build. It is for people only: agents use the MCP server, never
this page, so every choice here optimises for reading, not for parsing.

The page draws the repository as a transit network, because that is what the graph is:

```
screens ──call──▶ endpoints ──handled in──▶ modules ──use──▶ tables
```

Four places, and a breadcrumb on every one of them:

| place | what you see |
|---|---|
| **Today** | the gate sentence, the four-line strip with counts, what changed since the last commit (`--since <ref>`, default HEAD), a numbered **Next up**, and a Proof panel with what the index could and could not read |
| **Map** | the same four lines as four lists: Screens (most connected first), Endpoints (grouped by the module that handles them, with a *no test only* filter), Modules, Tables |
| **Changes** | change records in progress and done, each with its next task, open, deferred and done tasks, requirements and reach |
| **Checks** | **Broken** (what `weave check` fails: calls with no endpoint, endpoints removed but still called) separated from **Gaps in proof** (no test, no e2e, changed requirements) |

Open any item and its **trace** is drawn as the line itself: four stations, the item as
the interchange, each station headed with the relation in words (*Calls 14 endpoints*,
*Handled in 8 modules*, *Reaches 22 tables*), horizontal on a desktop and vertical on a
phone. Below it: **Proved by** (tests), **Planned in** (change, tasks, requirements) and
one **Reach** sentence.

Every view has a URL — `#today`, `#map/endpoints`, `#changes`, `#checks`,
`#node/…` — so back, forward and a pasted link work; the old hashes (`#api`, `#problems`)
still resolve. Search is global in the header, grouped by kind. `/` focuses it, `Enter`
opens the first match, `↑`/`↓` walk the list, `Esc` clears the search and then the
selection, and clicking a file path copies it. Light, dark and follow-the-system are one
button. `weave explore --all` puts every project in one file with a switcher.

The visual contract — tokens, type roles, scroll ownership, primitives, accepted debt —
is in [`docs/explorer/DESIGN.md`](docs/explorer/DESIGN.md).

## Hosted (one panel, and things you can do in it)

`weave explore` writes a file you read. `weave serve` hosts the same page and lets you work
in it:

```bash
weave serve                 # http://127.0.0.1:7777/?t=…  (loopback only)
weave serve --port 8080
```

One page, three actions in the signboard:

- **Check** — runs the gate on the working tree and prints the failures; every `file:line`
  in the output is a link that opens that window of the file.
- **Reindex / Wiki** — rebuild the index, or regenerate the wiki and open it (the wiki is
  served at `/wiki/`).
- **open** beside any path chip — the file, with its line numbers, and a one-line **note**
  field that writes through `weave remember`, so `context()` returns it next session.

The server binds `127.0.0.1` only, mints a write token per run, refuses another origin's
requests, and serves one window of a file rather than whole files. It is not a team server:
nothing outside the machine can reach it, and nothing is uploaded.

## Wiki (for people and for reviewers)

`weave explore` is one page you open; `weave wiki` is a site you share. It writes one
static page per module, endpoint, screen, table and change, from the same index the gate
reads:

```bash
weave wiki                    # .weave/wiki/ — local, gitignored
weave wiki --out docs/wiki    # a wiki you can commit and send to someone
weave wiki --check            # exit 1 when the pages no longer describe HEAD
```

Every page answers four questions in the same order: what this is, what it connects to,
what proves it (with the command to run), and which decisions were recorded beside it
(`weave remember`). Nothing on a page is editable — regenerating is the only way to change
it, so the wiki cannot drift into a document nobody trusts.

Each page footer carries the commit it was built from and the coverage the index rested
on, and `stamp.json` records that commit for the whole set. That is what makes
`weave wiki --check` possible: the CI line below fails a branch that changes the code and
leaves the documentation describing an older tree.

```yaml
- run: ./weave wiki --out docs/wiki --check
```

A page that names a removed endpoint does not exist after regeneration, which is the point:
the documentation is a projection of the graph, not a second source of truth.

## Carrying knowledge forward

Four small pieces, stored beside the graph (inside `.weave/`, never committed):

- **`delta [ref]`** (`changed` over MCP) compares the graph with a git ref or a saved
  `graph.json` and prints what was added, removed or moved — and names a removed
  endpoint that the old graph still had callers for.
- **`grep <text>`** (`search(text: true)`) scans source lines instead of names. Files
  that carry graph nodes rank first.
- **`remember --note "…" <file...>`** keeps a decision or gotcha next to the files it
  concerns; `context` returns it as `note … (not evidence)`, and `recall` lists it.
- **`gain [--since N]`** reads the receipt every MCP answer carries (tokens, graph age,
  coverage) and reports total, median and p95 tokens by tool.

`context --depth code` is the one place a tool returns source: up to three 15-line
windows at the anchors, opt-in, hard-capped by the same budget, and it refuses `.env`,
keys, certificates and anything outside the repo.

## Enforcement

<img src="docs/img/gate.svg" alt="The gate: an agent claims done, weave check exits 1 naming the caller's file and line, the agent keeps working; when the call is fixed the exit code is 0." width="100%">

A gate has to run where the agent cannot skip it. So `check` is a CLI with an exit code,
not an MCP tool the model chooses to call.

```bash
weave init --hooks       # one line in .claude/settings.json: the Stop hook
weave check              # no args: git diff against HEAD
weave check src/a.ts     # explicit files
weave check --against origin/main
weave check --strict-tests
```

**The Stop hook is the in-session gate.** `weave hook` runs the same check and, when the
contract is broken, exits 2 with the failing `file:line` on stderr — which is what a Claude
Code `Stop` hook hands back to the model, so the turn does not end on a claim of done:

```json
{ "hooks": { "Stop": [ { "hooks": [ { "type": "command", "command": "\"/abs/weave\" hook --root ." } ] } ] } }
```

Every hook run is recorded in `.weave/fires.jsonl`. `weave fires --since 7` reports how
often the gate ran, how often it failed and what it caught — the one number that says
whether the in-session check earns its place, since a break introduced and fixed between
two commits never appears in history.

**In CI**, this repository ships `action.yml`:

```yaml
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }
- uses: michalbasisty/weave@main
  with: { root: '.', strict-tests: 'true', against: origin/main }
```

The action builds the Go binary, indexes, checks, and prints coverage. Exit 1 when:

- a **removed endpoint is still called** — it compares the endpoint set with the one
  the previous index saw;
- a changed file contains a call site that matches no endpoint;
- a requirement implemented in the changed files has a failing test in the JUnit report;
- with `--strict-tests`, a touched endpoint has no test at all.

It warns, without failing, when a requirement's text changed and the code was not
re-checked, when no spec change record names the changed files, or when a touched
endpoint has no direct test.

## Traceability

Three levels, weakest to strongest. None is required:

1. `Evidence: \`path\`` in `tasks.md` → change → task → file.
2. A stable id anywhere (`REQ-*`, `FR-*`, `NFR-*`) → task → requirement.
3. `// @req REQ-ORD-1` in the code → file → requirement, exact, survives renames.

Requirement text is hashed at index time. If it changes between indexes, every file
and test linked to it is reported as **suspect**.

### Evidence: the part the agent cannot fake

`weave evidence` reads the JUnit XML your tests already produce and joins it to
requirements through the `@req` markers, so the answer is "a named test ran and
reported a result", not "the agent says so".

```
evidence: 2/3 requirement(s) verified, 41 test case(s)
REQ-ORD-1        pass    2 test(s)  orders page loads
REQ-ORD-2        FAIL    1 test(s)  order status moves to shipped [fail]
REQ-ORD-3        no test           implemented via @req, never exercised
```

Enable it once per project:

```
vitest      npx vitest run --reporter=junit --outputFile=.weave/junit.xml
jest        npx jest --reporters=jest-junit   (JEST_JUNIT_OUTPUT_FILE=.weave/junit.xml)
playwright  npx playwright test --reporter=junit
phpunit     vendor/bin/phpunit --log-junit .weave/junit.xml
go          go test -json ./... | go-junit-report > .weave/junit.xml
```

## Why it is cheap

<img src="docs/img/tokens.svg" alt="Two measured comparisons: a route question answers in 66 tokens where grep returns no hits; a screen's context answers in 346 tokens where reading the same facts costs 61,687." width="100%">

- **One call replaces explore + grep + read.** `context` names the page, endpoints, tables, spec change and tests inside a 350-token budget. Measured over 24 screens on a 954-file monorepo: **346 tokens median** for the pack, against **4,657** for the screen's own file and **61,687** for the same facts read the old way (11 files: the screen, its endpoints, their tests).
- **Some questions grep cannot answer.** `GET /api/v1/tasks/:id` appears in source as `/:id` under a mount, so grep returns **0 hits**; `weave search` answers it in **66 tokens**. Where the literal does exist, grep plus opening the matched files cost 12,872 and 18,245 tokens for the same answer.
- **It names the tests to run *and* the command**, grouped per package and runner.
- **No whole files.** Every list is capped and line-oriented (`path:line`), not JSON. The two exceptions are opt-in and bounded: text search returns matching lines, and `context(depth: "code")` up to three 15-line windows.
- **No index server, no embeddings.** One file, rebuilt in 330 ms for 954 files.
- **No per-language parsers.** Regex over the contracts a framework already publishes.

The whole tool costs **1,017 tokens of MCP schema** per session (10 tools); GitHub's MCP server loads about 18,000 across 93. Warm MCP calls are ~3 ms, `check` takes 406 ms on a one-file rename, a fresh index 330 ms, and the 489-page wiki 440 ms.

## What it extracts, and how exact it is

| edge | source | confidence |
|---|---|---|
| `app.use('/api/v1/x', router)` + `router.get('/y')` → endpoint | Express mount table + router files | exact |
| `app/api/x/route.ts` exporting `GET`/`POST` → endpoint | Next.js file routing | exact |
| `app/x/page.tsx`, `pages/x.tsx` → page | Next.js file routing | exact |
| `quotes: { list: procedure.query(…) }`, `list: publicProcedure.input(z…).query(…)` → `QUERY /api/trpc/quotes.list` | tRPC routers, across lines | exact |
| `openapi.json` / `swagger.json` / any `.json` with `openapi`+`paths` paths | any language's spec | exact |
| your own wrapper, via `.weave/patterns.json` | one regex per rule | exact |
| `api.get('/orders')`, ``api.get(`/orders/${id}`)`` → endpoint | client call sites | exact / inferred |
| page → endpoint | `<Route>` + lazy imports (inline or through a chunk map) + the component import graph | exact / inferred |
| `model` → `@@map` table, relations | `schema.prisma` | exact |
| module → table | `prisma.<model>` use inside the module | exact |
| file → file importers | relative imports | exact |
| test → endpoint / page | e2e request calls, `page.goto` | exact / inferred |
| change → task → file | `tasks.md` Evidence paths | exact |

**An inferred edge is a lead, not a fact**, and the tool never hides which is which.

## Design rules

- **No new spec format.** It uses whatever exists — `[Satisfies:]`, `REQ-` ids, or the `Evidence:` paths most task files already carry.
- **No parsers per language.** A language it does not cover degrades to "no nodes", never to wrong nodes.
- **No database, no daemon.** One file, rebuilt on demand.
- **Budgeted output.** Every tool has a cap.

## Test

```bash
go test ./...                    # unit, golden and integration suites
go test ./test/integration -v    # the binary, end to end
```

The integration suite builds the real binary and drives it on a miniature Express +
Prisma + React fixture: index, context (names and code), impact, search, grep, notes,
delta, check failing on a renamed route, explore, MCP, gain and init.

## Known limits

- Express, Next.js route handlers and pages, tRPC routers and OpenAPI JSON specs. Nest, Fastify, GraphQL and SvelteKit need a `.weave/patterns.json` rule; the coverage block names what was skipped.
- An OpenAPI spec is read in JSON only, up to 4 MB. A YAML spec is named in the coverage block, never guessed at.
- Importers are one hop deep, and `impact` reads the endpoints those importers call so a shared client is not called isolated — it does not walk the full transitive tree.
- No incremental indexing: a rebuild re-reads every file, fine to ~5k files. `delta` compares graphs, so a column-only schema edit shows nothing.
- `untested` means "no direct test reference" — e2e that drives the UI by clicking does not count.
- On Windows, the very first run of a freshly built unsigned binary can take a few seconds while antivirus scans the files it opens; later runs are fast. Linux CI does not see it.

The research behind the design is in [`docs/DESIGN.md`](docs/DESIGN.md); the Go port's
measurements and gates are in [`docs/GO-MIGRATION.md`](docs/GO-MIGRATION.md).
