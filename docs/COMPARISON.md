# Comparison: what a call costs, and what the alternatives cost

Three charts, three questions. None of them is a claim that weave is faster than another
tool; they measure different things and each caption says which.

## 1. Tokens for the same answer

<img src="img/chart-cost.svg" alt="Tokens for the same answers: weave search 66, weave context 346, the screen's file alone 4,657, the same facts gathered by hand 61,687." width="100%">

**Method.** On a 954-file Express + Prisma + React repo: 24 screens sampled; `weave context
<file>` run with the default 350-token budget; the target file's own size; and the size of
every file an agent would open to learn the same facts — the screen, the files that declare
the endpoints it calls, and the tests that prove them (11 files median). Tokens are bytes ÷ 4,
the same convention the MCP receipts use.

**Why a log scale.** The honest range is 30×; a linear axis would hide everything below
`4,657`.

**What it does not say.** It measures the cost of the *answer*, not what an agent spends in a
session. An agent may read less and conclude less. A live eval is the open question
([`EVAL.md`](EVAL.md)).

## 2. What every session pays before the first prompt

<img src="img/chart-schema.svg" alt="MCP tool schema before the first prompt: weave 1,017 tokens, filesystem 3,500, Notion 9,000, GitHub 18,000." width="100%">

**Method.** weave's ten tool definitions serialised from its own schema (1,017 tokens). The
others are published measurements of those servers; they are stable enough to be quoted and
labelled, not re-measured here. This is the one comparison in which every row is the same
unit — tokens of JSON schema sitting in context on every turn.

## 3. What other index tools report

<img src="img/chart-others.svg" alt="Reduction in agent output tokens reported by other tools: repowise 31.6%, CodeGraph 24.4%, Serena 14.8%, Graphify 8.9%, code-review-graph 6.0%." width="100%">

**Method.** A 44-task benchmark published by the authors of those tools, measuring change in
agent output tokens with the tool available. weave has no equivalent end-to-end benchmark, so
its row is empty on purpose. Borrowing someone else's number for a different harness, model
and task set would be the kind of claim this project exists to argue against.

## The claim that is not a percentage

A route question — `GET /api/v1/tasks/:id` — cannot be answered by grep at all, because the
source writes `/:id` under a mount. Two of five questions in the same measurement returned
zero grep hits; weave answered both in under 80 tokens. That is a capability, not a speedup,
and it is the one a reader can verify in a minute by grepping their own repo.

The gate is the other one: renaming a route makes `weave check` exit 1 in 406 ms with the
caller's `file:line`. Measured on the same repo, reproducible with the commands in
[`REFERENCE.md`](REFERENCE.md).

## Reproducing

The measurement scripts are not in this repository (they are throwaway shell); the numbers
come from `weave context`, `weave search` and file sizes, all of which anyone can repeat in a
minute:

```bash
weave context src/pages/Tasks/TaskDetail.tsx | wc -c        # ÷4 → tokens
weave search "/api/v1/tasks/:id" | wc -c
wc -c <the files the page imports, its endpoints, their tests>
```
