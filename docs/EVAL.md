# Eval: does `context` change what an agent does?

This is weave's unproven claim. `weave gain` counts the tokens weave
serves, not the tokens it saves, so it cannot answer it. This is the protocol that can.

## Question

Does one `context(files)` call replace explore + grep + reads without lowering the
quality of the change?

## Tasks

Twelve tasks on a real repo, each naming a file an engineer would open anyway, and each
small enough to finish in one session:

| kind | example |
|---|---|
| add a field | add `notes` to the order table and return it from the detail endpoint |
| change a rule | reject an order over 50 items with a 422 |
| fix a broken call | a client calls `/api/v1/orders/:id/cancel`, no handler exists |
| rename | rename the `quotes.list` procedure and every caller |
| test gap | an endpoint with no test gets one, named by the graph |

Store them as `test/eval/tasks.jsonl`, one object per task:
`{"id","prompt","files","expect_files","expect_check"}`.

## Arms

- **baseline** — the agent's own tools only.
- **weave** — the same, plus the MCP server (`weave mcp --root .`).

Same model, same prompt, fresh session per run, no mention of weave in the prompt.
Three runs per task per arm: 72 sessions.

## What is recorded, per run

From the session transcript, not from the agent's summary:

- tool calls and tokens in / out;
- files read, and whether `expect_files` was among them;
- whether the named tests were run at all;
- `weave check` exit code after the run;
- wall time.

## Decision rule

Set before the first run, so the result cannot be reinterpreted afterwards:

- **Ship the MCP half** if median tokens per task drop ≥ 30% and success does not drop.
- **Rethink it** if tokens drop < 10%, or success drops at all.
- Anything between is a partial win: report per task kind, do not average it away.

## Running it

The harness is a shell script, not part of the binary, so the zero-runtime-dependency
rule holds:

```bash
bench/fetch.sh                                   # the RealWorld repo, among others
WEAVE=./weave bench/eval/run.sh bench/repos/realworld /tmp/eval 3 sonnet
node bench/eval/report.mjs /tmp/eval
```

`run.sh` takes each task in [`bench/eval/tasks.jsonl`](../bench/eval/tasks.jsonl), clones
the repo fresh for every run, and drives `claude -p` twice: once with no MCP server, once
with `weave mcp`. The agent gets file tools and a short command list (`git diff`, `npx
jest`, `npm test`), never a blanket permission bypass. Each run leaves its transcript,
the `weave check` result on the tree it left, the files it changed and its diff.
`report.mjs` reads those and applies the decision rule above.

Other repositories take their own task file: `TASKS=bench/eval/tasks-documenso.jsonl`.

## First pilot

2026-11-12, one run per task and arm, sonnet, release v0.6.0 plus this branch. 14 runs, so
it reads as a direction, not a result: the protocol asks for 72.

| repo | runs | success, baseline / weave | median tokens in, baseline / weave | change | median cost |
|---|---|---|---|---|---|
| RealWorld Express + React (109 files) | 8 | 4/4 / 4/4 | 210,173 / 227,623 | +8% | $0.09 / $0.09 |
| documenso (2,458 files) | 6 | 3/3 / 3/3 | 223,157 / 212,787 | −5% | $0.10 / $0.08 |

Decision rule: **rethink it** (tokens dropped by less than 10%), on both repos.

What the transcripts show:

- On the small repo the agent called weave 2 to 4 times per task and then read the same files
  anyway, so weave added calls instead of replacing them. The one clear win was the rename
  (251k vs 355k tokens, 11 vs 14 tool calls), where `impact` listed every caller up front.
- On documenso the agent called weave once in three tasks. It went straight to Grep, which
  works there because the tasks named the procedure (`document.inbox.getCount`) and grep
  finds a name. The case weave is built for, a URL that exists only once its pieces are
  joined, did not come up.
- Success was never the problem: every run finished, left a tree `weave check` passes, and
  named what it had to.

Next: tasks that describe behaviour rather than name the code ("the page that shows the
inbox count"), and a weave arm that is told the tool exists in the prompt, not only in the
server instructions. Both change what is measured, so they go in as new arms, not edits to
these.

## Second round: 72 sessions

2026-11-12, after the pilot. Twelve tasks that describe behaviour instead of naming code
([`tasks-behaviour.jsonl`](../bench/eval/tasks-behaviour.jsonl) on RealWorld,
[`tasks-documenso-behaviour.jsonl`](../bench/eval/tasks-documenso-behaviour.jsonl) on
documenso), two arms, three runs each, sonnet. The weave arm also got what `weave init` writes:
a "when to use weave" section in `CLAUDE.md` that told the agent to `search` first, `context`
before editing, `impact` before renames and `verify` before done.

| | baseline | weave |
|---|---|---|
| success (finished, gate passes, expected answer) | 36 / 36 | 36 / 36 |
| median tokens in, RealWorld | 165,323 | 293,631 (+78%) |
| median tokens in, documenso | 216,350 | 298,259 (+38%) |
| median turns | 7 | 10 |
| median cost, RealWorld / documenso | $0.07 / $0.09 | $0.10 / $0.10 |

By task kind, median tokens in:

| kind | runs per arm | baseline | weave | change |
|---|---|---|---|---|
| change a rule | 18 | 168,487 | 294,429 | +75% |
| rename | 9 | 213,211 | 342,460 | +61% |
| test gap | 3 | 248,579 | 262,010 | +5% |
| impact question | 6 | 203,435 | 207,027 | +2% |

The two impact questions split: "which screens show the inbox count" went from 364k to 206k
tokens (−43%, $0.14 to $0.07), because `impact` answered what took the baseline a dozen
searches. "Which screen sends a test webhook" was a one-grep question, and weave added 63%.

Decision rule: **rethink it**. Success held; tokens rose.

Why, from the transcripts:

- **Tokens in follow turns, not output size.** weave's answers were small (150 to 250
  characters for `search` and `context`), but every turn re-sends the whole conversation.
  The weave arm took three more turns, and that is the whole difference.
- **Every weave run paid a turn to load the tools.** Claude Code defers MCP tool schemas, so
  all 36 weave runs started with a `ToolSearch` call before the first weave call.
- **The calls were added, not substituted.** On a rule change the agent ran `search`,
  `context`, then the same `Grep` and `Read` the baseline ran, then `verify`. weave can only
  save where it removes searches, and a one-file edit has none to remove.

What changed because of it: the `CLAUDE.md` section and the tool descriptions no longer tell
the agent to call `context` before every edit or `verify` before done (the Stop hook runs the
gate without a turn). They point it at questions that span the app, renames and removals, and
say plainly to skip weave for an edit inside one file. That narrower wording is not measured
yet; the next round should run it against these same tasks.
