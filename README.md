# weave

Your agent says it is done. This is how you check.

<img src="docs/img/lines.svg" alt="A repository drawn as four lines: screens call endpoints, handled in modules, that use tables." width="100%">

Three things, one binary, no runtime dependencies:

- **A gate.** `weave check` exits 1 when a frontend call reaches an endpoint that no longer
  exists. The failure names the caller's `file:line`.
- **A panel.** `weave serve` hosts the map with the gate, file windows and notes in it.
- **A wiki.** `weave wiki` writes a page per module, endpoint, screen, table and change, and
  tells you when the pages describe an older commit.

```bash
go install github.com/michalbasisty/weave/cmd/weave@latest   # Go 1.22+ to build, nothing to run
cd /path/to/your/project
weave init --hooks    # connect Claude, Cursor, VS Code, opencode, and the Stop hook
weave serve           # http://127.0.0.1:7777 — the map, hosted
weave check           # what a change breaks (exit 1)
```

## The gate

<img src="docs/img/gate.svg" alt="The gate: an agent claims done, weave check exits 1 naming the caller's file and line, the agent keeps working." width="100%">

It runs at the end of an agent turn (Stop hook), before a commit, and in CI. Three failures,
no opinions:

- a **removed endpoint is still called** — it compares the endpoint set with the previous one;
- a **call site matches no endpoint** — a route that was renamed, or a typo;
- a **requirement's test fails**, when the code names the requirement and a JUnit report exists.

Everything else is a warning, because a gate that cries wolf gets switched off in a week.

## What it costs, and what it saves

<img src="docs/img/chart-cost.svg" alt="Tokens for the same answers: weave search 66, weave context 346, the screen's file alone 4,657, the same facts gathered by hand 61,687." width="100%">

Two numbers people ask about first:

| | |
|---|---|
| cost to install | **1,017 tokens** of MCP schema per session (10 tools) |
| cost to answer | **66 tokens** for a route question, **346** for a screen's whole context |
| cost to run | one static binary: index 954 files in 330 ms, `check` 406 ms |

The cheapest thing about it is not the tokens. It is that `GET /api/v1/tasks/:id` cannot be
found with grep — the source says `/:id` under a mount — and weave answers it in 66 tokens
with the `file:line`. The measurements, methods and the comparison with other tools are in
[`docs/COMPARISON.md`](docs/COMPARISON.md).

## The panel

`weave serve` binds `127.0.0.1`, mints a write token per run, and serves one page:

- **Check** runs the gate on the working tree; every `file:line` in the output opens that
  window of the file.
- **Reindex**, **Wiki** — rebuild, or regenerate the wiki and open it at `/wiki/`.
- **open** beside any path chip loads the file with a one-line note field that writes through
  `weave remember`, so the next session reads it.

It is a local tool, not a team server: loopback only, one origin, no uploads.

## The wiki

```bash
weave wiki                 # .weave/wiki — local, gitignored
weave wiki --out docs/wiki # a wiki you can commit and send
weave wiki --check         # exit 1 when the pages describe an older tree
```

A sidebar that mirrors the graph, search on every page (`/`), the handler's code next to the
endpoint, the tests that prove it with the command to run them, and prev/next at the foot.
Every page is generated — regenerating is the only way to change it, so the documentation
cannot drift into a document nobody trusts.

## Agents

`weave init` writes MCP configs for Claude Code, Cursor, VS Code and opencode. The server is
stdio, answers from one JSON index, and every answer ends with the tokens it cost:

| tool | what it returns |
|---|---|
| **`context(files)`** | the pack to read before editing: page, endpoints with `file:line`, tables, owning change, open tasks, tests **and the command to run them**, in a 350-token budget |
| `impact(files)` | the complete blast radius |
| `search(query)` | endpoints, pages, modules, tables, requirements, tests by name; `text: true` scans source |
| `plan(slug)` | the next unfinished task of a change, with its files and command |
| `changed(ref)` | what the graph gained or lost since a git ref |
| `remember` / `recall` | a note beside the files it concerns, returned as a claim, never evidence |

`context` answers "I know the file, what is it?" · `plan` answers "I know the spec, what next?"
· `impact` answers "I know the change, what else breaks?" What agents actually call, and what
that says about the tool surface, is in [`docs/AGENTS.md`](docs/AGENTS.md).

## It says what it could not parse

Every extractor misses something, and a tool that misses a route silently gives a confident
wrong answer. So coverage is measured and printed — route files, client calls, task files,
Prisma models, tRPC routers — and any file that looks like a contract but produced nothing is
named. That list is the honest boundary of every answer weave gives.

```bash
weave doctor
```

## More

- [`docs/REFERENCE.md`](docs/REFERENCE.md) — every command, every extractor, traceability,
  CI, known limits.
- [`docs/COMPARISON.md`](docs/COMPARISON.md) — all three charts and their methods.
- [`docs/PLAN.md`](docs/PLAN.md) — where this is going, and the replay that says what not to build.
- [`docs/DESIGN.md`](docs/DESIGN.md) — the research the product rests on.
- [`docs/explorer/DESIGN.md`](docs/explorer/DESIGN.md) — the visual contract of the map.

```bash
go test ./...    # unit, golden and integration suites
```
