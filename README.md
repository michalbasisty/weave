# weave

Your agent says it is done. This is how you check.

`weave check` is a gate: it exits 1 when a change removes an HTTP endpoint that
something still calls, when a call site matches no endpoint, or when a requirement's
test fails. No agent can argue its way past an exit code.

Around that gate sits a map of the repo, served to agents over MCP:

| | |
|---|---|
| `context(files)` | what this file is: its page, endpoints, tables, owning spec change, open tasks, and the exact tests + command to run |
| `plan(slug)` | the next unfinished task of a spec change, with its files and command |
| `impact(files)` | everything a change can reach |

Full stack, end to end — one graph of both halves of the contract:

```
React page ──calls──▶ HTTP endpoint ──in──▶ module ──uses──▶ table
     ▲                      ▲
   test ─────hits──────────┘
     │
 spec change ──task──▶ file
```

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
exist:

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
| `impact(files)` | the complete blast radius: every module, endpoint, table, page, test, requirement, importers of the file, and endpoints no test covers. | ~500 tokens |
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
weave context <file...>    # --budget 250; --depth code for a window at each anchor
weave impact  <file...>
weave plan    <change> [T3]
weave change  [slug]
weave search  <query>
weave grep    <text>       # --regex, --limit; graph files rank first
weave delta   [ref]        # what the graph gained or lost (--from graph.json)
weave remember --note "…" <file...>
weave recall  [file...]
weave memory               # all notes (--clear)
weave gain    [--since N]  # tokens served over the last N days
weave check   [file...]    # no args = git diff; exit 1 on a broken contract
weave evidence [REQ...]
weave doctor
weave perf   [--runs N]    # index timings over time, or N fresh runs
weave init
weave mcp
weave --help
```

## Explorer (for people)

```bash
weave explore            # writes .weave/explore.html and opens it
weave explore --all      # every registered project, one file, with a switcher
```

One self-contained HTML file — 45 KB for this repo, 859 KB for a 937-file one. No
server, no network, no build. It is for people only: agents use the MCP server, never
this page, so every choice here optimises for reading, not for parsing.

The page teaches one line and then keeps every detail on it:

```
screen ──calls──▶ API endpoint ──handled in──▶ module ──uses──▶ table
```

| view | what you see |
|---|---|
| **Summary** | the whole map in one sentence, a worked example of the line above, what the index could and could not read, problems as counts, test coverage, and changes in flight |
| **Screens** | every route, the endpoints it calls and the tables it reaches, most connected first |
| **API** | every endpoint, grouped under the module that handles it, with `tested` / `no test` |
| **Data** | each table and the modules that read or write it |
| **Changes** | change records in progress and done, with tasks, requirements and files |
| **Problems** | calls that match no endpoint, endpoints and screens with no test, changed requirements |

Open any item and the four-lane **trace** shows the same line around it. Below the
trace: **Proved by** (tests), **Planned in** (change, tasks, requirements), and one
**Reach** sentence. Relations are written in words, never as graph vocabulary.

Navigation is six entries with a count and a one-line gloss; there are no duplicate
tabs. Every node has a URL (`#node/…`), so back, forward and a pasted link work.
Search is global and lives at the top of the rail. `/` focuses it, `↑`/`↓` walk the
list, `Esc` clears it or clears the selection, and clicking a file path copies it.
Light, dark and follow-the-system are one button.

The visual contract — tokens, type roles, scroll ownership, primitives, accepted debt —
is in [`docs/explorer/DESIGN.md`](docs/explorer/DESIGN.md).

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

A gate has to run where the agent cannot skip it. So `check` is a CLI with an exit code,
not an MCP tool the model chooses to call.

```bash
weave check              # no args: git diff against HEAD
weave check src/a.ts     # explicit files
weave check --against origin/main
weave check --strict-tests
```

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

- **One call replaces explore + grep + read.** `context` names the page, endpoints, tables, spec change and tests in ~350 tokens.
- **It names the tests to run *and* the command**, grouped per package and runner.
- **No whole files.** Every list is capped and line-oriented (`path:line`), not JSON. The two exceptions are opt-in and bounded: text search returns matching lines, and `context(depth: "code")` up to three 15-line windows.
- **No index server, no embeddings.** One file, rebuilt in ~280 ms for 937 files.
- **No per-language parsers.** Regex over the contracts a framework already publishes.

Measured on a 937-file Express + Prisma + React monorepo: **278 ms** to index (Node was
780 ms in the same harness), 242 endpoints, 37 pages, 57 tables, 5,694 edges.

## What it extracts, and how exact it is

| edge | source | confidence |
|---|---|---|
| `app.use('/api/v1/x', router)` + `router.get('/y')` → endpoint | Express mount table + router files | exact |
| `app/api/x/route.ts` exporting `GET`/`POST` → endpoint | Next.js file routing | exact |
| `quotes: { list: procedure.query(…) }` → `QUERY /api/trpc/quotes.list` | tRPC routers | exact |
| `openapi.json` / `swagger.json` paths | any language's spec | exact |
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

- Express, Next.js route handlers, tRPC routers and OpenAPI JSON specs. Nest, Fastify, GraphQL and SvelteKit need a `.weave/patterns.json` rule; the coverage block names what was skipped.
- Importers are one hop deep.
- No incremental indexing: a rebuild re-reads every file, fine to ~5k files. `delta` compares graphs, so a column-only schema edit shows nothing.
- `untested` means "no direct test reference" — e2e that drives the UI by clicking does not count.
- On Windows, the very first run of a freshly built unsigned binary can take a few seconds while antivirus scans the files it opens; later runs are fast. Linux CI does not see it.

The research behind the design is in [`docs/DESIGN.md`](docs/DESIGN.md); the Go port's
measurements and gates are in [`docs/GO-MIGRATION.md`](docs/GO-MIGRATION.md).
