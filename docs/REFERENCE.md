# Reference

Everything the README used to explain in one long page. The README is the pitch; this
file is the manual.

## Every command

```bash
weave                      # status, and what to do next
weave explore              # the map as one file you can open
weave serve [--port 7777]  # the map hosted, with actions (loopback only), opened in your browser (--no-open skips it); a busy default port moves to the next free one
                            # the project dropdown lists every repo weave has indexed; /p/<name>/ is its panel
weave wiki                 # a page per module, endpoint, screen, table, change
weave wiki --out docs/wiki # a wiki you can commit
weave wiki --check         # exit 1 when the pages describe an older tree
weave context <file...>    # --budget 250; --depth code for a window at each anchor
weave impact  <file...>    # the full blast radius
weave plan    <change> [T3]
weave change  [slug]
weave search  <query>
weave grep    <text>       # --regex, --limit; graph files rank first
weave check   [file...]    # no args = git diff; exit 1 on a broken contract (--markdown for a PR comment)
weave hook                 # for an agent hook: exit 2, failure on stderr
weave fires   [--since N]  # what the hook has seen: runs, failures, average time
weave delta   [ref]        # what the graph gained or lost
weave remember --note "…" <file...>
weave recall  [file...]
weave memory               # all notes (--prune drops ownership notes and repeats, --clear drops all)
weave gain    [--since N]  # tokens served over the last N days
weave evidence [REQ...]    # which requirements a JUnit report proves
weave doctor               # graph health and what it could not read
weave perf    [--runs N]   # index timings over time
weave projects             # every registered repo
weave init    [--upgrade] [--hooks] [--no-instructions] [--only claude,cursor,vscode,opencode]
weave forget  <name>
weave mcp     [--all]      # MCP server on stdio
weave --version

--root <dir>        point at a specific checkout
--project <name>    use a registered project from anywhere
--budget <tokens>   context size (default 350)
--depth code        context: a short window at each anchor (never whole files)
--out <file|dir>    output path for explore and wiki
--since <ref>       explore: what changed since this git ref
```

## Several projects

Every command registers the repo it ran in, so the list builds itself:

```bash
weave projects
```

```
name           size                                path
shop           745 files · 196 ep ·  87 changes    ~/code/shop
weave           25 files ·   5 ep ·  1 changes    ~/code/weave
```

Then work on any of them from anywhere — no `cd`, no paths:

```bash
weave --project shopfront plan 2026-10-19-ui-restyle
weave --project weave explore
```

Names match loosely (`shopfront`, `Shop-Front` and `shop-front` are the same), a unique fragment is
enough (`print`), and an ambiguous one is refused with the candidates listed.
`weave forget <name>` drops one.

One MCP entry for all of them:

```bash
claude mcp add weave -- /abs/path/weave mcp --all
```

Every tool then takes an optional `project` argument. Without `--all` the schema stays lean
and the server answers for the repo it was started in.

## Connecting an agent

`weave init` writes MCP configs for every client it can, without touching files that exist:

| client | file |
|---|---|
| Claude Code, Codex, anything reading `.mcp.json` | `.mcp.json` |
| Cursor | `.cursor/mcp.json` |
| VS Code / Copilot | `.vscode/mcp.json` |
| opencode | `opencode.json` (merged) |

`--only claude` (or `--only cursor,vscode`) writes just the ones you name.

It also appends `.weave/` to `.gitignore`, prints the `claude mcp add` line, and adds a short
"Repo map (weave)" section to `CLAUDE.md` and `AGENTS.md` (created if missing, between
`<!-- weave:start -->` and `<!-- weave:end -->`). The section says when to use which tool in
the terms a task arrives in: `search` for a screen or behaviour, `impact` before a rename,
`verify` before done, grep for a name you already know. The agent eval found that the server
instructions alone did not change what agents reach for. `--no-instructions` skips it.

`--upgrade` replaces only a stale `weave` entry or section. `--hooks` adds the gate at the end
of a turn: the Claude Code `Stop` hook in `.claude/settings.json`, and for opencode (and front
ends on it, such as OpenChamber) a plugin at `.opencode/plugins/weave.js`. On `session.idle`
the plugin runs `weave check`; when it fails with something new it shows a toast and sends the
failure back into the session, once per distinct failure.

For OpenChamber there is also an extension, [`extensions/openchamber/`](../extensions/openchamber/):
a Work Status section that runs `weave check` on the open project whenever the tree changes and
lists each broken call with its closest route, and a rail panel to search the map in plain words
and send a file's context or impact to the chat. Add the folder, its git URL, or the
`weave-openchamber.zip` attached to each release in Settings → Extensions.

The server lists five tools: `context`, `impact`, `search`, `changed` and `verify`. In a
repo with `specs/changes/` it adds `plan`, `changes`, `task_done`, `task_reopen`,
`change_new` and `change_delete`. `remember`, `recall` and `status` are CLI and panel
features; they still answer if an agent calls them, and `weave mcp --all-tools` lists them.

Manual, if you prefer the absolute path (an MCP client spawns the server with its own
`PATH`):

```bash
claude mcp add weave -- /abs/path/weave mcp --root .
```

## What it extracts, and how exact it is

weave reads every JavaScript, TypeScript, Python, Go, Ruby, PHP, GraphQL, Prisma, JSON and Markdown file
except what git ignores (`.gitignore` at any depth, `.git/info/exclude`, global
excludes), so generated clients and build output never show up as modules.

| edge | source | confidence |
|---|---|---|
| `app.use('/api/v1/x', router)` + `router.get('/y')`, `app.get('/y')`, `usersRouter.post('/y')` → endpoint | Express mount table + router files | exact |
| `app/api/x/route.ts` exporting `GET`/`POST` → endpoint | Next.js file routing | exact |
| `app/x/page.tsx`, `pages/x.tsx` → page | Next.js file routing | exact |
| `app/routes/api.render.tsx`, `app/routes/api+/health.ts`, `_auth+/t.$team+/_index.tsx` → endpoint (loader/action) or page (default export) | Remix and React Router v7 file routes, flat and `+` folders | exact |
| `quotes: { list: procedure.query(…) }`, `list: publicProcedure.input(z…).query(…)` → `QUERY /api/trpc/quotes.list` | tRPC routers, across lines | exact |
| `team: teamRouter` + `find: findTeamsRoute` in other files → `QUERY /api/trpc/team.find` | tRPC routers split across files: aliases, nested objects, imported procedures | exact |
| `openapi.json` / `swagger.json` / any `.json` with `openapi`+`paths` | any language's spec | exact |
| `@router.get("/items/{id}")` with `APIRouter(prefix=…)` and `include_router(…, prefix=…)` → `GET /items/:id` | FastAPI | exact |
| `@bp.route("/users/<int:id>", methods=[…])` with `Blueprint(url_prefix=…)` | Flask | exact |
| `path("api/", include("shop.urls"))`, `re_path(r'^tags/?$', …)`, `router.register(r'orders', …)` → `ANY /api/orders/` and `/api/orders/:pk/` | Django and Django REST framework URLconfs, following `include()` | exact |
| `v1 := r.Group("/api")`, `v1.GET("/users/:id")`, a group passed to `func Register(r *gin.RouterGroup)` | gin, echo, gorilla `PathPrefix` | exact |
| `r.Route("/orders", func(r chi.Router) { r.Get("/{id}", …) })`, `mux.HandleFunc("GET /reports/{id}", …)` | chi, net/http (Go 1.22 patterns) | exact |
| `scope :api`, `resources :articles, param: :slug, only: […]`, nested `resource :favorite`, `member`/`collection`, `get :feed, on: :collection`, `match … via:`, `devise_for` → endpoints at the controller's `def action` | Rails `config/routes.rb` | exact |
| `Route::get('users/{id}', 'UserController@show')`, `[X::class, 'm']`, `Route::resource`/`apiResource` with `only`/`except`, `Route::group(['prefix' => …])`, `Route::prefix(…)->group(…)`; `routes/api.php` under `/api` → endpoints at the controller method | Laravel `routes/*.php` | exact |
| your own wrapper, via `.weave/patterns.json` | one regex per rule | exact |
| `api.get('/orders')`, ``api.get(`/orders/${id}`)``, `requests.post('/users')`, `apiClient.get('/x')` → endpoint | client call sites; any receiver named like an HTTP client (`api`, `http`, `client`, `request`, `fetch`, `agent`) | exact / inferred |
| ``api.put(`/tasks/${id}/${action}`)`` with `action: 'start' \| 'stop'` (inline or a `type` alias) → `PUT /tasks/:id/start` and `/stop` | a literal-union segment is one call per value; a value no route serves is a broken call. Untyped, a variable last segment over two or more fixed routes is a choice between them | exact / inferred |
| `fetch('https://api.github.com/…')` | a call to another host is not a contract with this repo and is never counted as broken | — |
| page → endpoint | `<Route>` + lazy imports + the component import graph | exact / inferred |
| `model` → `@@map` table, relations | `schema.prisma` | exact |
| `pgTable('users', …)` / `mysqlTable` / `sqliteTable` → table | Drizzle schema files | exact |
| `@Entity('users') class User` → table (default: snake_case class name) | TypeORM entities | exact |
| `class Article(db.Model): __tablename__ = 'article'` (default: snake_case), `class Order(models.Model)` with `Meta.db_table` (default: `app_model`), abstract bases skipped | SQLAlchemy, Flask-SQLAlchemy, Django models | exact |
| `type UserModel struct { gorm.Model … }`, `TableName()` (default: GORM's snake_case plural) | GORM structs | exact |
| `@Controller('users')` + `@Get(':id')` (+ `setGlobalPrefix`) → endpoint | NestJS controllers | exact |
| `fastify.get('/x')`, `fastify.route({ method, url })` → endpoint | Fastify | exact |
| `type Query { users }` (`.graphql`, `.gql`, `typeDefs`), `@Query()` / `@Mutation()`, `queryField('x')` → `QUERY /graphql/users` | GraphQL schemas, NestJS resolvers, Pothos / Nexus | exact |
| `` gql`query { users { id } }` `` → call to `QUERY /graphql/users`, only when the repo declares a schema | GraphQL client documents | exact |
| module → table | `prisma.<model>`, Drizzle `.from(users)` / `db.query.users`, TypeORM `Repository<User>`, `Article.query` / `Order.objects` / `select(Article)`, GORM `&UserModel{}` inside the module | exact |
| file → file importers | relative imports, tsconfig/jsconfig `paths`, workspace package names (`@acme/lib/x`) | exact |
| test → endpoint / page | e2e request calls, `page.goto` | exact / inferred |
| change → task → file | `tasks.md` Evidence paths | exact |

**An inferred edge is a lead, not a fact**, and the tool never hides which is which.

### Your own patterns

Every codebase has a wrapper the built-in extractors do not know: a NestJS decorator, a
generated client, `request('/x')`. `.weave/patterns.json` adds one regex per rule; capture
group 1 is the path, `method` is the verb (leave it out for any verb):

```json
{
  "routes": [
    { "match": "@Get\\(\\s*['\"]([^'\"]+)['\"]", "method": "GET" },
    { "match": "@Post\\(\\s*['\"]([^'\"]+)['\"]", "method": "POST" }
  ],
  "calls": [
    { "match": "request\\(\\s*['\"`](/[^'\"`]+)", "method": "GET" }
  ]
}
```

`routes` become endpoints, `calls` become call sites matched against them. A broken pattern
is skipped, never fatal, and the index rebuilds when the file changes. Check what it found
with `weave doctor`.

### Coverage is printed, not assumed

```
coverage
  route files   28/28 produced endpoints (100%)
  client calls  224/224 resolvable matched an endpoint (100%)
  routes        33/35 <Route> entries mapped (94%, 2 redirect)
  task files    87/87 parsed (100%)
  schema        46/46 models mapped to tables (Prisma, Drizzle, TypeORM)
  file routes   12/12 route files produced endpoints (100%)
  file pages    14/14 page files produced pages (100%)
  tRPC routers  4/4 parsed (100%)
  GAPS          1 file looks like a contract but produced nothing:
    packages/backend/src/modules/x/dynamic.routes.ts
```

When a file looks like a route file and yields no endpoints, it is named. That list is the
honest boundary of every answer weave gives.

## Enforcement

A gate has to run where the agent cannot skip it. `check` is a CLI with an exit code, not an
MCP tool the model chooses to call.

```bash
weave init --hooks       # one line in .claude/settings.json: the Stop hook
weave check              # no args: git diff against HEAD
weave check src/a.ts     # explicit files
weave check --against origin/main
weave check --strict-tests
```

The Stop hook runs the same check and, on a broken contract, exits 2 with the failing
`file:line` on stderr — which is what a Claude Code `Stop` hook hands back to the model:

```json
{ "hooks": { "Stop": [ { "hooks": [ { "type": "command", "command": "\"/abs/weave\" hook --root ." } ] } ] } }
```

Every hook run is recorded in `.weave/fires.jsonl`; `weave fires --since 7` reports how
often the gate ran, how often it failed, and what it caught.

**In CI**, this repository ships `action.yml`:

```yaml
permissions: { contents: read, pull-requests: write }   # write only for the PR comment
steps:
  - uses: actions/checkout@v4
    with: { fetch-depth: 0 }
  - uses: michalbasisty/weave@main
    with: { root: '.', strict-tests: 'true', against: origin/main, wiki: 'true' }
```

It builds the Go binary, indexes, checks, prints coverage, writes the report to the job
summary and, on a pull request, comments with each removed endpoint that is still called, the
line that calls it and the screens that reach that line:

| endpoint | called from | screens |
|---|---|---|
| `GET /api/v1/orders` | `packages/web/src/pages/Orders.tsx:9` | `/orders` |

`comment: on-failure` (the default) posts only when the check fails and then keeps that one
comment current, `always` posts on every run, `false` never does. `weave check --markdown`
prints the same report locally. With `wiki: 'true'` fails a
branch whose committed `docs/wiki` describes an older tree. It exits 1 when:

- a **removed endpoint is still called** — compared with the previous index's endpoint set;
- a changed file contains a call site that matches no endpoint;
- a requirement implemented in the changed files has a failing test in the JUnit report;
- with `--strict-tests`, a touched endpoint has no test at all.

It warns, without failing, when a requirement's text changed and the code was not re-checked,
when no spec change record names the changed files, or when a touched endpoint has no direct
test.

## Traceability

Three levels, weakest to strongest. None is required:

1. `Evidence: \`path\`` in `tasks.md` → change → task → file.
2. A stable id anywhere (`REQ-*`, `FR-*`, `NFR-*`) → task → requirement.
3. `// @req REQ-ORD-1` in the code → file → requirement, exact, survives renames.

Requirement text is hashed at index time. If it changes between indexes, every file and test
linked to it is reported as **suspect**.

`weave evidence` reads the JUnit XML your tests already produce and joins it to requirements
through the `@req` markers, so the answer is "a named test ran and reported a result", not
"the agent says so".

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

## Carrying knowledge forward

Four small pieces, stored beside the graph (inside `.weave/`, never committed):

- **`delta [ref]`** (`changed` over MCP) compares the graph with a git ref or a saved
  `graph.json` and prints what was added, removed or moved — naming a removed endpoint the
  old graph still had callers for.
- **`grep <text>`** (`search(text: true)`) scans source lines instead of names; files that
  carry graph nodes rank first.
- **`remember --note "…" <file...>`** keeps a decision or gotcha next to the files it
  concerns; `context` returns it as `note … (not evidence)`, and `recall` lists it.
  A note that only says who owns a file (`Owner: …`, `Owned by …`) is not stored or shown,
  because CODEOWNERS and the map already say it; the same note twice is kept once.
  `weave memory --prune` removes such notes written before.
- **`gain [--since N]`** reads the receipt every MCP answer carries and reports total,
  median and p95 tokens by tool.

`context --depth code` is the one place a tool returns source: up to three 15-line windows at
the anchors, opt-in, capped by the same budget, and it refuses `.env`, keys, certificates and
anything outside the repo.

## Design rules

- **No new spec format.** It uses whatever exists — `[Satisfies:]`, `REQ-` ids, or the
  `Evidence:` paths most task files already carry.
- **No parsers per language.** A language it does not cover degrades to "no nodes", never to
  wrong nodes.
- **No database, no daemon.** One file, rebuilt on demand. `serve` is optional and loopback.
- **Budgeted output.** Every tool has a cap.

## Test

```bash
go test ./...                    # unit, golden and integration suites
go test ./test/integration -v    # the binary, end to end
```

The integration suite builds the real binary and drives it on a miniature Express + Prisma +
React fixture: index, context, impact, search, grep, notes, delta, the gate failing on a
renamed route, the Stop hook, explore, wiki, MCP, gain and init.

## Known limits

- Express, Fastify, NestJS, Next.js, Remix / React Router v7 file routes, React Router
  `<Route>` tables, tRPC routers, GraphQL and OpenAPI JSON specs; FastAPI, Flask, Django and
  Django REST framework; gin, echo, chi, gorilla and net/http; Rails and Laravel route files;
  Prisma, Drizzle, TypeORM, SQLAlchemy, Django and GORM tables. Rails and Laravel models
  (ActiveRecord, Eloquent) are not read yet, Rails `concern`s and `constraints` blocks are read
  through without their own effect, and a Go router mounted through a variable (`r.Mount("/api", api())`) loses its prefix. SvelteKit needs a `.weave/patterns.json` rule (see [Your own patterns](#your-own-patterns)); the coverage block
  names what was skipped.
- An OpenAPI spec is read in JSON only, up to 4 MB. A YAML spec is named in the coverage
  block, never guessed at.
- Importers are one hop deep, and `impact` reads the endpoints those importers call so a
  shared client is not called isolated — it does not walk the full transitive tree.
- No incremental indexing: a rebuild re-reads every file, fine to ~5k files. `delta`
  compares graphs, so a column-only schema edit shows nothing.
- `untested` means "no direct test reference" — e2e that drives the UI by clicking does not
  count.
- On Windows, the very first run of a freshly built unsigned binary can take a few seconds
  while antivirus scans it. Linux CI does not see it.
