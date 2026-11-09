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

The harness is a shell script, not part of the binary: it drives `claude -p` or
`opencode run`, writes one JSONL line per run, and a `weave eval report` sums it.
Keeping it out of the Go binary keeps the zero-runtime-dependency rule intact.
