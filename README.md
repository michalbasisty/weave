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
| **`ask(question)`** | a cheap model answers from the graph in ≤5 lines and names the facts it used. For questions that need reasoning, not retrieval. | ~200 tokens |
| `status()` | graph health: counts, endpoints no page calls, endpoints no test covers, pages without e2e, phase timings, and this server's own call latency. | ~250 tokens |

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
weave perf   [--runs N]    # index timings over time, or N fresh runs
weave init
weave mcp
weave --help
```

## Performance

Every index run appends a line to `.weave/perf.jsonl`: total, per-phase timings, node
and edge counts, heap. `weave perf` prints the last eight runs with the trend, and
`--runs 3` indexes three times to separate cold from warm cost.

```
last 8 index run(s)
when                  files  nodes  edges    total     walk  extract    specs  evidence  coverage
2026-11-02 14:28:58     745   2048   4203  1302.1ms  529.7ms  248.2ms   85.3ms         0   438.9ms
2026-11-02 14:33:21     745   2048   4203   628.2ms  356.8ms  197.9ms   60.5ms     4.9ms       8ms
avg 1094ms · max 1302ms · heap 63MB · trend -48% over the window
```

`doctor` also reports the last run's phases and the index file size, and the MCP
`status` tool adds this server's own uptime, call count and p95 latency — so a slow
agent session is visible instead of felt.

Without `npm link`, every command also runs as `node src/cli.ts <command>`.

## The manager

An expensive model should not do retrieval. Give it one verb instead of six:```bash
weave ask "what does the warehouse change touch, and what is untested?"
```

A cheap model answers in five lines, from the graph, and prints the facts it used:

```
The change touches GET/POST /api/v1/orders and 2 tables (part_orders, parts).
POST /orders has no direct test; the warehouse page is covered by e2e/admin.spec.ts.

-- from deepseek-v4.1-flash in 940ms; facts used:
endpoint  GET /api/v1/orders  orders.routes.ts:58
...
```

It is grounded, not creative: the prompt forbids naming any file, endpoint, table or
requirement that is not in the facts, and the answer is followed by the facts so the
caller can check it.

```bash
# OpenCode Go (default)
export OPENCODE_GO_API_KEY=...
# DeepSeek direct
export DEEPSEEK_API_KEY=...            # base https://api.deepseek.com/v1, model deepseek-chat
# anything OpenAI-compatible
export WEAVE_LLM_API_KEY=... WEAVE_LLM_BASE_URL=... WEAVE_LLM_MODEL=...
```

Then hand the expensive agent a manager instead of a toolbox:

```bash
claude mcp add weave -- node /abs/path/weave/src/cli.ts mcp --manager
```

`--manager` exposes only `ask`, `plan` and `status`. No raw `impact`/`context`/`search`,
so the expensive model cannot burn context re-deriving what the cheap one already knows,
and no tool returns file contents — it reads code with its own tools when it must.

Without a key, `ask` says exactly which variable to set and everything else still works.

## Explorer (for people)

```bash
weave explore            # writes .weave/explore.html and opens it
weave explore --all      # every registered project, one file, with a switcher
```

One self-contained HTML file — 44 KB for this repo, 832 KB for a 937-file one. No
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

Open any item and the four-lane **trace** shows the same line around it: the screens
that reach it, its endpoints, the module, and the tables — two hops either way. Below
the trace: **Proved by** (tests), **Planned in** (change, tasks, requirements), and one
**Reach** sentence ("a change here can reach 19 screens, 64 endpoints and 18 tests").
Relations are written in words, never as graph vocabulary.

Navigation is six entries with a count and a one-line gloss; there are no duplicate
tabs. Every node has a URL (`#node/…`), so back, forward and a pasted link work.
Search is global and lives at the top of the rail. `/` focuses it, `↑`/`↓` walk the
list, `Esc` clears it or clears the selection, and clicking a file path copies it.
Light, dark and follow-the-system are one button.

**Every node explains itself.** Descriptions are read from the source, not written twice:

| node | where the text comes from |
|---|---|
| endpoint | the comment directly above `router.get(...)` |
| page | the comment above the `<Route>`, else the top of the component file |
| table | the `///` or `//` comment above the Prisma model |
| change | the first real sentence of its `proposal.md` |
| requirement | its heading plus the requirement text |
| task | its title, open tasks first, done ones behind one fold |

Clicking a change opens its requirements and its tasks, so the spec reads like a
document rather than a list of slugs.

Files are in the graph only where a spec task names them as evidence — click a task,
then a file, and you see which change owns it.

The visual contract — tokens, type roles, scroll ownership, primitives, accepted debt —
is in [`docs/explorer/DESIGN.md`](docs/explorer/DESIGN.md).

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
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }        # the base ref must be readable
- uses: actions/setup-node@v4
  with: { node-version: 22 }
- uses: michalbasisty/weave@main
  with: { root: '.', strict-tests: 'true', against: origin/main }
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
| `openapi.json` / `swagger.json` paths | any language's spec: `{id}` → `:id`, `summary` → description | exact |
| your own wrapper, via `.weave/patterns.json` | one regex per rule, capture group 1 is the path | exact |
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

### When your stack has its own wrapper

Every team eventually writes `defineRoute()`, a generated client, or a `request()`
helper. Rather than a parser per framework, name them once:

```json
// .weave/patterns.json
{
  "routes": [{ "match": "defineRoute\\('([^']+)'\\)", "method": "GET" }],
  "calls":  [{ "match": "request\\('([^']+)'\\)" }]
}
```

Capture group 1 is the path. `doctor` reports how many endpoints came from patterns,
and a pattern that matches nothing shows up as a gap rather than silently doing nothing.

## Design rules

- **No new spec format.** It uses whatever exists — `[Satisfies:]`, `REQ-` ids, or only the `Evidence:` paths most task files already carry.
- **No parsers per language.** A language it does not cover degrades to "no nodes", never to wrong nodes.
- **No database, no daemon.** One file, rebuilt on demand.
- **Budgeted output.** Every tool has a cap; `context` takes an explicit token budget and honours it.

## Test

```bash
node --test test/smoke.test.mjs   # 23 tests, incl. a renamed route that must fail the gate
node test/bench.mjs <repo>        # cold / warm call latency and response size
```

The fixture is a miniature Express + Prisma + React repo. The tests drive the MCP
server over stdio and assert the whole chain, the budget, `init` never overwriting,
`@req` linking, suspect detection and `check`.

The research behind the design — what was measured, what was deliberately not built —
is in [`docs/DESIGN.md`](docs/DESIGN.md).

## Known limits

- Express and Next.js route handlers (App Router and `pages/api`), tRPC routers, and any
  OpenAPI JSON spec. Nest, Fastify, GraphQL and SvelteKit still need either an extractor
  or a `.weave/patterns.json` rule; until then their handlers are invisible, and the
  coverage block is where that shows.
- Importers are one hop deep. A util imported by 40 modules reports "used by 40 files — wide blast radius" instead of listing 40 endpoints.
- No incremental indexing: a rebuild re-reads every file. Fine to ~5k files.
- `untested` means "no direct test reference" — e2e that drives the UI by clicking does not count.
