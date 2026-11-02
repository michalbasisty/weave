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

Zero dependencies, zero build step, one JSON file. Node 22.6+ runs the TypeScript directly.

## Quick start

```bash
cd /path/to/weave && npm link     # once: gives you the `weave` command
cd /path/to/your/project

weave                # what is this repo, and what can I do
weave explore        # the map, in your browser
weave init           # connect Claude, Cursor, opencode, Codex
```

`weave` finds the project root by walking up from wherever you are, so it works from any
subdirectory. `--root` exists only for when that guess is wrong.

## Several projects

Every command registers the repo it ran in, so the list builds itself:

```bash
weave projects
```

```
name           size                                path
QR-gent         745 files · 196 ep ·  87 changes    G:\QR-gent
QR-gent-print   880 files · 242 ep · 105 changes    G:\QR-gent-print
weave            25 files ·   5 ep ·  1 changes    G:\projekty\weave
```

Then work on any of them from anywhere — no `cd`, no paths:

```bash
weave --project qrgent plan 2026-10-19-ui-restyle
weave --project print explore
```

Names match loosely (`qrgent`, `QR-gent` and `qr-gent` are the same), a unique fragment is
enough (`print`), and an ambiguous one is refused with the candidates listed. `weave
forget <name>` drops one.

**One MCP entry for all of them:**

```bash
claude mcp add weave -- node /abs/path/weave/src/cli.ts mcp --all
```

Every tool then takes an optional `project` argument. Without `--all` the schema stays
lean and the server answers for the repo it was started in.

## Connect an agent, once

Writes MCP configs for every client it can, without touching files that exist:

| client | file |
|---|---|
| Claude Code, Codex, anything reading `.mcp.json` | `.mcp.json` |
| Cursor | `.cursor/mcp.json` |
| VS Code / Copilot | `.vscode/mcp.json` |
| opencode | `opencode.json` (merged) |

It also appends `.weave/` to `.gitignore` and prints the `claude mcp add` line plus an `AGENTS.md` snippet for agents that read instructions instead of MCP.

Manual, if you prefer:

```bash
claude mcp add weave -- node /abs/path/weave/src/cli.ts mcp --root .
```

The absolute node path is deliberate: an MCP client spawns the server with its own `PATH`,
and `weave` is not guaranteed to be on it.

## Tools

| tool | what it returns | size |
|---|---|---|
| **`context(files)`** | the pack to read *before* editing: page, module, endpoints with `file:line`, tables, owning spec change, open tasks, linked requirements, tests **and the command to run them**, broken call sites. `budget` caps it (default 350 tokens). | ~350 tokens |
| `impact(files)` | the complete blast radius: every module, endpoint, table, page, test, requirement, importers of the file, and endpoints no test covers. | ~500 tokens |
| `search(query)` | endpoints, pages, modules, tables, requirements, tests by name. One line each, never file contents. | ~150 tokens |
| `changes(slug)` | a spec change record: tasks, open tasks, files they touch, and the blast radius of those files. With no slug: the newest 25 records with progress. | ~400 tokens |
| **`plan(slug)`** | the next unfinished task: its files, endpoints, tables, tests and the run command. Call this instead of reading `tasks.md`. | ~150 tokens |
| `status()` | graph health: counts, endpoints no page calls, endpoints no test covers, pages without e2e. | ~200 tokens |

The server rebuilds a stale index (older than 15 s) before answering, so the map follows the agent's edits.

`context` answers "I know the file, what is it?" · `plan` answers "I know the spec, what
do I do next?" · `impact` answers "I know the change, what else breaks?"

## CLI

```bash
weave                      # status, and what to do next
weave explore              # the map, in your browser
weave context <file...>    # --budget 250
weave impact  <file...>
weave plan    <change> [T3]
weave change  [slug]
weave search  <query>
weave check   [file...]    # no args = git diff; exit 1 on a broken contract
weave evidence [REQ...]
weave doctor
weave init
weave mcp
weave --help
```

Without `npm link`, every command also runs as `node src/cli.ts <command>`.

## Explorer (for people)

```bash
weave explore            # writes .weave/explore.html and opens it
weave explore --all      # every registered project, one file, with a switcher
```

One self-contained HTML file — 280 KB for a 745-file repo, 1.6 MB for three of them. No
server, no network, no build. It is for people only: agents use the MCP server, never
this page, so every choice here optimises for reading, not for parsing.

| view | what you see |
|---|---|
| **Overview** | the landing screen: repo name, counts, one coverage line, what needs attention, changes in flight |
| **Flows** | one card per page: its description, then `CALLS` and `READS` as labelled rows |
| **Endpoints** | every endpoint, each tagged `tested` or `no test` |
| **Pages** | every route, its component file, its description |
| **Tables** | each table and which modules read it |
| **Specs** | change records with progress, the next open task, and the blast radius |
| **Risk** | the most depended-on nodes — where a change reaches furthest |
| **Issues** | broken calls, endpoints without a test, pages without e2e, changed requirements |

Select anything and the right pane shows its description, a one-line blast radius
("connected to 3 endpoints, 1 test"), and its relations split into outbound and inbound.

Two controls next to the view switcher keep it usable on any screen: a **density**
toggle (comfortable / compact) and **hide the list** to read a detail at full width.
Both survive a reload. Click a file path to copy it. `/` focuses the filter, `↑`/`↓`
walk the list, `Esc` clears.

**Every node explains itself.** Descriptions are read from the source, not written twice:

| node | where the text comes from |
|---|---|
| endpoint | the comment directly above `router.get(...)` |
| page | the comment above the `<Route>`, else the top of the component file |
| table | the `///` or `//` comment above the Prisma model |
| change | the first real sentence of its `proposal.md` |
| requirement | its heading plus the requirement text |
| task | its title, with done/open shown as a checkbox |

Clicking a change shows its requirements and its tasks with ticks, so the spec reads like
a document rather than a list of slugs. 694 descriptions across three projects; 1.3 MB.

Files are in the graph only where a spec task names them as evidence — click a task,
then a file, and you see which change owns it. `/` focuses search, `Esc` clears it.

No MCP support? Shell out to `context` — same output, same budget.

## Enforcement

A gate has to run where the agent cannot skip it. So `check` is a CLI with an exit code,
not an MCP tool the model chooses to call — nothing stops an agent from running it and
reading the output, and nothing should: the point is that CI decides, not the agent.

```bash
weave check              # no args: git diff against HEAD
weave check src/a.ts     # explicit files
weave check --strict-tests   # also fail when a touched endpoint has no test
```

**In CI**, this repository ships `action.yml`:

```yaml
- uses: actions/setup-node@v4
  with: { node-version: 22 }
- uses: michalbasisty/weave@main
  with: { root: '.', strict-tests: 'true' }
```

Exit 1 when:

- a **removed endpoint is still called** — it compares the endpoint set with the one
  the previous index saw, so renaming `GET /orders` to `GET /orders/list` fails with
  the caller's `file:line`, even though the caller was not the file you edited;
- a changed file contains a call site that matches no endpoint;
- a requirement implemented in the changed files has a failing test in the JUnit report;
- with `--strict-tests`, a touched endpoint has no test at all.

It warns, without failing, when a requirement's text changed and the code was not
re-checked, when no spec change record names the changed files, or when a touched
endpoint has no direct test. On a healthy 745-file repo it prints one line and exits 0.

```
git pre-commit   node src/cli.ts check --root .
claude code      PostToolUse hook on Edit|Write → the same command
kiro hook        .kiro/hooks/weave.json, trigger PostFileSave → the same command
ci               node src/cli.ts index && node src/cli.ts check
```

## Traceability

Three levels, weakest to strongest. None is required:

1. `Evidence: \`path\`` in `tasks.md` → change → task → file.
2. A stable id anywhere (`REQ-*`, `FR-*`, `NFR-*`) → task → requirement.
3. `// @req REQ-ORD-1` in the code → file → requirement, exact, survives renames.

Requirement text is hashed at index time. If it changes between indexes, every file
and test linked to it is reported as **suspect** — the Doorstop idea, without the
manual review step.

```js
// @req REQ-ORD-1
router.get('/', async (_req, res) => { … });
```

### Evidence: the part the agent cannot fake

Linking is not proof. `weave evidence` reads the JUnit XML your tests already produce
and joins it to requirements through the `@req` markers, so the answer is "a named
test ran and reported a result", not "the agent says so".

```
evidence: 2/3 requirement(s) verified, 41 test case(s)
REQ-ORD-1        pass    2 test(s)  orders page loads
REQ-ORD-2        FAIL    1 test(s)  order status moves to shipped [fail]
REQ-ORD-3        no test           implemented via @req, never exercised
```

Enable it once per project — no flags are added to `run` for you, because a wrong
flag breaks a working test command:

```
vitest      npx vitest run --reporter=junit --outputFile=.weave/junit.xml
jest        npx jest --reporters=jest-junit   (JEST_JUNIT_OUTPUT_FILE=.weave/junit.xml)
playwright  npx playwright test --reporter=junit
phpunit     vendor/bin/phpunit --log-junit .weave/junit.xml
go          go test -json ./... | go-junit-report > .weave/junit.xml
```

`check` then fails on a failing requirement test in the changed files, and warns on a
requirement that is implemented but never exercised.

## Why it is cheap

- **One call replaces explore + grep + read.** `context` names the page, endpoints, tables, spec change and tests in ~350 tokens; getting that by hand costs thousands and several tool round-trips.
- **It names the tests to run *and* the command**, grouped per package and runner. An agent that runs 6 named tests instead of 85 plus a 3-minute e2e suite is a different workflow, not a marginal saving.
- **Nothing returns file contents.** Every list is capped and line-oriented (`path:line`), not JSON — JSON costs 2-3× the tokens for the same facts.
- **No index server, no embeddings.** One file, rebuilt in ~1.2 s for 750 files; warm calls cost ~57 ms (a stat sweep, then in-memory answers).
- **No per-language parsers.** Regex over the contracts a framework already publishes, so the maintenance cost stays near zero.

Measured on a 745-file Express + Prisma + React monorepo: 1.2 s to index, 57 ms per warm call, 1.4 KB median response, 196 endpoints, 33 pages, 46 tables, 3,865 edges.

The CLI builds the index on first use, so `context` works on a fresh clone with no setup step.

## What it extracts, and how exact it is

| edge | source | confidence |
|---|---|---|
| `app.use('/api/v1/x', router)` + `router.get('/y')` → endpoint | Express mount table + router files | exact |
| `app/api/x/route.ts` exporting `GET`/`POST` → endpoint | Next.js file routing (route groups and `[id]` handled) | exact |
| `quotes: { list: procedure.query(…) }` → `QUERY /api/trpc/quotes.list` | tRPC router objects, nested routers flattened | exact |
| `api.get('/orders')`, ``api.get(`/orders/${id}`)``, `trpc.quotes.list.useQuery()` → endpoint | client call sites, matched on the tail of both paths | exact / inferred |
| page → endpoint | `<Route path element>` + lazy imports + the component import graph | exact in the page file, inferred through imported components |
| `model` → `@@map` table, relations | `schema.prisma` | exact |
| module → table | `prisma.<model>` / `tx.<model>` use inside the module | exact |
| file → file importers | relative imports, `.js`→`.ts` resolution | exact |
| test → endpoint | e2e request calls and literal `/api/...` paths | exact / inferred |
| test → page | `page.goto(BASE + '/tasks')` | exact |
| change → task → file | `tasks.md` `- [x] T1 … Evidence: \`path\`` | exact |
| task → requirement | `[Satisfies: REQ-…]` when present | exact |

Confidence is printed next to page edges (`~` in `context`). **An inferred edge is a lead, not a fact**, and the tool never hides which is which.

## Design rules

- **No new spec format.** It uses whatever exists — `[Satisfies:]`, `REQ-` ids, or only the `Evidence:` paths most task files already carry.
- **No parsers per language.** A language it does not cover degrades to "no nodes", never to wrong nodes.
- **No database, no daemon.** One file, rebuilt on demand.
- **Budgeted output.** Every tool has a cap; `context` takes an explicit token budget and honours it.

## Test

```bash
node --test test/smoke.test.mjs   # 14 tests, incl. a renamed route that must fail the gate
node test/bench.mjs <repo>        # cold / warm call latency and response size
```

The fixture is a miniature Express + Prisma + React repo. The tests drive the MCP
server over stdio and assert the whole chain, the budget, `init` never overwriting,
`@req` linking, suspect detection and `check`.

The research behind the design — what was measured, what was deliberately not built —
is in [`docs/DESIGN.md`](docs/DESIGN.md).

## Known limits

- Express and Next.js route handlers (App Router and `pages/api`). Nest, Fastify,
  GraphQL and SvelteKit need their own extractor; until then their handlers are invisible
  and the coverage block is where that shows.
- Importers are one hop deep. A util imported by 40 modules reports "used by 40 files — wide blast radius" instead of listing 40 endpoints.
- No incremental indexing: a rebuild re-reads every file. Fine to ~5k files.
- `untested` means "no direct test reference" — e2e that drives the UI by clicking does not count.
