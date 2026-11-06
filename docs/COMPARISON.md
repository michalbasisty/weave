# How the numbers were measured

Every chart in the README is drawn from [`bench/results.json`](../bench/results.json), which
[`bench/run.mjs`](../bench/run.mjs) writes. Rerun all of it:

```bash
go build -o weave ./cmd/weave
bench/fetch.sh                      # the repositories below, at fixed commits, into bench/repos
node bench/run.mjs --weave ./weave  # add --old <binary> to compare with an older release
node bench/charts.mjs               # redraws docs/img/chart-*.svg
```

## The repositories

| | commit | stack | files |
|---|---|---|---|
| [documenso](https://github.com/documenso/documenso) | `fdd0337` | Remix + tRPC + Prisma monorepo | 2,458 |
| RealWorld Express + React: [back end](https://github.com/gothinkster/node-express-realworld-example-app) + [front end](https://github.com/khaledosman/react-redux-realworld-example-app) in one repo | `30b68e1` + `53b0b4c` | Express + Prisma, React + Redux | 109 |
| [RealWorld NestJS](https://github.com/lujakob/nestjs-realworld-example-app) | `c1c2cc4` | NestJS + TypeORM | 43 |

None of them is configured for weave: no `.weave/patterns.json`, no hints. The RealWorld
halves are two upstream repositories copied side by side, the way a team that owns both
keeps them; `fetch.sh` builds that repo.

## 1. Can grep find the endpoint?

<img src="img/chart-grep.svg" alt="Endpoints whose URL appears in the source: documenso 30 of 327, NestJS 0 of 21, Express 20 of 20." width="100%">

**Method.** For every endpoint weave indexed, a plain substring search for its full path
(`/api/trpc/envelope.item.getManyByToken`, `/articles/:slug`) over every source file the walk
reads. A hit anywhere counts, even in a comment, so this flatters grep.

**Reading it.** Express spells every route out, so grep finds all 20. tRPC joins nested router
keys, Remix derives the URL from a file name, and NestJS joins `@Controller('articles')`,
`@Get(':slug')` and `setGlobalPrefix('api')`. There, the URL a front end calls exists only
once the pieces are put together, which is what the index does.

## 2. Tokens to understand a screen

<img src="img/chart-cost.svg" alt="Tokens to understand one screen, median: documenso 12,778 by reading the files vs 222 with weave context; RealWorld 3,898 vs 216." width="100%">

**Method.** For every screen weave found (119 on documenso, 8 on RealWorld): the output of
`weave context <screen file>` at the default budget, against the files an agent would open
to learn the same facts: the screen's file, the files that declare each endpoint it calls,
the tests that hit those endpoints, and the model files their modules use. Tokens are
bytes ÷ 4, the convention weave's own receipts use. Medians.

**What it does not say.** It prices the *answer*. An agent might read less than every file,
and conclude less. Whether `context` changes a whole session is the open question in
[`EVAL.md`](EVAL.md).

## 3. The gate on renamed routes

<img src="img/chart-gate.svg" alt="Renaming each of the 15 routes the RealWorld front end calls, one at a time: weave check failed and named the caller 14 times." width="100%">

**Method.** On RealWorld, for each endpoint that the front end calls, the first literal
segment of its route string is renamed in the back end (`/articles/:slug` →
`/articles-renamed/:slug`), `weave check` runs on the uncommitted change, and the file is
restored. A catch is exit 1 with `removed endpoint still called: … from <file>:<line>`.

**The miss.** Renaming `GET /articles/feed` leaves the front end's `/articles/feed` call
landing on `GET /articles/:slug`, and Express would route it there too. weave reports what
the router would do, not what the author meant.

**Found while measuring.** The first run caught 10 of 15. Two matcher rules let a broken
call settle on a neighbour: a variable last segment (`/articles/${slug}`) matched a fixed
route (`/articles/feed`), and a longer call (`/articles/${slug}/comments`) matched a shorter
endpoint with no literal in common. Both are fixed, with tests. A second `weave check` on the
same edit also used to pass, because it compared with the index the first run had just
written. It now compares with the last commit too.

## 4. Speed

<img src="img/chart-speed.svg" alt="weave check wall time: documenso about 1 second, RealWorld Express + React 45 ms, RealWorld NestJS 29 ms." width="100%">

**Method.** `weave check` on a clean tree, which re-indexes and runs the gate; median of
three runs after five fresh `weave index` runs. Linux, one process, no daemon.

## 5. Schema cost

<img src="img/chart-schema.svg" alt="MCP tool schema before the first prompt: weave 1,430 tokens, filesystem 3,500, Notion 9,000, GitHub 18,000." width="100%">

**Method.** weave's `tools/list` response, serialised, bytes ÷ 4. The other servers' figures
are as published by their maintainers and not re-measured here. This is the one row where
every server is measured in the same unit: JSON schema that sits in context on every turn.

## Before and after the launch fixes

Same repositories, same commits, release v0.1.0 against today's build (`--old`):

| documenso | v0.1.0 | now |
|---|---|---|
| endpoints found | 20 | 327 |
| screens found | 0 | 119 |
| frontend calls matched | 98 of 317 | 312 of 312 |
| broken calls reported on the unchanged repo (all false) | 315 | 0 |

v0.1.0 found no front-end calls in RealWorld and no endpoints in the NestJS app, so it had
nothing to check there. The README does not compare against it; this table is here so the
history is honest.

## What other index tools report

<img src="img/chart-others.svg" alt="Reduction in agent output tokens reported by other tools: repowise 31.6%, CodeGraph 24.4%, Serena 14.8%, Graphify 8.9%, code-review-graph 6.0%." width="100%">

A 44-task benchmark published by the authors of those tools, measuring the change in agent
output tokens with the tool available. weave has no equivalent end-to-end run yet, so it has
no row. Borrowing another harness's number would be the kind of claim this project exists to
argue against.
