# How agents use weave, and what to do about it

Written from a real usage log, not a survey: one project, one month, an agent working
through a 954-file repo with the MCP server installed the whole time.

```
gain: 29 call(s) in the last 365 day(s)
tokens      total 8,376 · median 305 · p95 685
by tool     context 18 (5,619) · status 3 (1,248) · impact 1 (799) · search 2 (357)
            changes 1 (213) · plan 1 (51) · recall 1 (49) · changed 1 (30) · remember 1 (10)
```

## What the log says

- **`context` is the product.** 18 of 29 calls, 62% of the conversation. Everything else is
  long tail, and no other tool reached three calls.
- **`ask` was never called.** It is the only tool that costs a model call, and it was ignored
  in favour of the free ones — which is what the research on passive tools predicted.
- **`status` is expensive per use** (416 tokens a call) and answered a question the page
  already answers. Three calls spent 1,248 tokens.
- **The agent found the tools without being told what to call.** No prompt in the log named
  `context`; the server instructions did.
- **Median answer: 305 tokens.** That is the budget doing its job; p95 of 685 is the cost of
  a big screen, not a leak.

## What that changes

1. **Keep `context` first and only.** Its description is the only one that needs to be read;
   the rest are lookups. Every answer already ends with the tokens it cost, so waste is
   visible.
2. **Move the gate out of the model's hands.** The log says an agent will use a tool when it
   needs an answer; it will not use one to check itself. That is why `weave hook` exists:
   the Stop hook runs the gate and hands back the failure, no cooperation required.
3. **Make the default surface smaller.** The next code change is to stop advertising `ask`
   from `weave mcp` (it stays a CLI command). Nine tools instead of ten, one fewer decision
   point, and no loss in the log.
4. **Do not add a wiki tool.** A page is for a person. An agent that wants the same facts
   asks `context`; an agent that wants prose has its own reading budget. The wiki ships an
   `llms.txt` for a model that happens to be reading the site, but that is a file, not a
   tool: it costs the server nothing and changes no schema.

## What is still unproven

Whether `context` *changes behaviour*: fewer tokens, fewer wrong files, a higher pass rate.
The replay over 625 commits found no true breaks for the gate to catch, and no live eval has
been run. [`EVAL.md`](EVAL.md) is the protocol for it; the number that would replace this
document's opinion with evidence is a before/after on the same twelve tasks.

Until then, the honest claim is the one in the README: the answer is 66–346 tokens, the gate
is quiet when it is right, and a route question is not answerable with grep.

## 2026-11-05: what changed since this log

The log above still describes the ten-tool server that month was measured on. Since then:

- **Four write tools joined the surface.** `task_done` and `task_reopen` close a task;
  `change_new` and `change_delete` (`confirm: true`) create and remove a change record.
  The schema is 1,434 tokens for fourteen tools, not 1,017 for ten. The advice to keep
  the surface small still holds in general; these were added because the CLI, the panel
  and MCP needed one writer, not because the log asked for them.
- **The panel writes.** `weave serve` ticks and reopens tasks, starts changes, renders
  capability specs, and maps every screen to its endpoints, tables and owning spec in a
  Views table. The "explorer stays read-only" assumption is gone; writes go through
  `weave.SetSpecTask`, `NewSpecChange` and `DeleteSpecChange`, with a `tasks.md` lock.
- **Still open:** dropping `ask` from the advertised set, and whether spec prose should
  be editable in the panel (today: rendered and raw, read-only).
