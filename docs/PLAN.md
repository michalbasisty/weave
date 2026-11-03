# weave: how to use it, and what to expand into

Status: plan. Nothing here is implemented beyond what already ships.

## 1. What weave is for, in one sentence

> It fails your build when your frontend still calls an API endpoint your agent just
> deleted or renamed, and tells you the file and line.

Every other surface — the map, the explorer, `plan`, the notes — is support for that
sentence, or it is a vitamin. The market research (see `docs/DESIGN.md` and the memory
entry on passive indexes) is unambiguous: agents ignore tools they are not forced
through, and people do not pay for maps.

## 2. Using it, day to day

Three loops, in the order they earn their keep.

### Loop A — the gate, where the agent cannot skip it

| when | command | what it does |
|---|---|---|
| end of an agent turn | `weave check` (Stop hook) | exit 1 with the failing `file:line`, fed back to the agent |
| before `git commit` | `weave check` (pre-commit hook) | same, one second, no conversation |
| on a pull request | `michalbasisty/weave@main` (already shipped) | fails the check, posts the coverage block |

A gate nobody runs is a wish. The three places above are the only places the agent cannot
argue with. `weave check --against origin/main` also catches an endpoint removed on the
branch that the base branch still calls.

### Loop B — before and after an edit

- Before touching a file whose blast radius is unknown: `weave impact <file>` — or the
  `impact` MCP tool if the agent is the one editing.
- While an agent works: `context(files)` is the one call worth making, because it names the
  test command as well as the tests.
- After the work: `weave check` again. Not `weave evidence` — that needs a JUnit report,
  which is a per-project setup step, not a daily one.

### Loop C — weekly, for the human

- `weave doctor` — what the index could not read. If a route file appears there, that
  stack is not covered and the gate is quieter than it looks.
- `weave explore` — read the Checks lane, not the map: what has no proof.
- `weave gain` — how many tokens the agent actually spent on weave answers.

### The one habit that matters

Run the gate on the branch before you ask the agent "is it done?". The agent's summary is
not evidence; the exit code is.

## 3. Local MCP hosting: is it a bad idea?

Short answer: **not bad, but premature, and only ever as a loopback daemon.**

weave's MCP is a stdio server today: the client spawns it per session. That is the right
default — no port, no auth, no shared state, and the client cannot reach it by accident.

| option | verdict | why |
|---|---|---|
| stdio, as now | **keep as the default** | zero attack surface, per-client isolation, one static binary |
| loopback HTTP (`127.0.0.1`, token) | **build later, in one narrow shape** | one warm index shared by editor + agent + hook; no per-client cold start; the hook can `curl` it instead of spawning |
| LAN / team / public URL | **no** | repo contents leave the machine, you inherit auth, multi-tenancy and rate limits, and the evidence says agents will not call it anyway |

If and when a local daemon is built, these are not optional:

- Bind `127.0.0.1` only. Never `0.0.0.0`, never a `--host` flag.
- A random token in `~/.weave/serve.token`, mode 0600, required on every request.
- Reject `Origin` headers that are not the local origin (a browser must not be able to
  drive it from a page you happen to have open).
- Read-only by default: `context`, `impact`, `search`, `status`, `check`. `remember`
  writes to disk and stays off unless `--allow-write`.
- Scope to one repo root; no path traversal above it; request body cap; no file serving.
- One user, one machine, one repo. Multi-repo is a CLI concern (`--project`), not a server
  concern.

The trigger for building it: a second agent client needs the same warm index, or the hook
latency bothers you. Not before. It is a server nobody queries otherwise.

## 4. Expanding: wiki, and the "AI OS" question

The honest answer first: **"AI OS" is the shape that kills tools like this.** An operating
system is a platform with a shell, memory, tools and a story about who controls them; weave
currently has one painful job. Adding surfaces before that job is proven is how CodeSee
died — a well-built map that nobody needed to pay for. The word to pursue instead is
*verification layer*: the thing that decides whether agent work is real.

That does not forbid expansion. It orders it, and every step has a kill gate.

### The ladder

**Step 1 — prove the gate fires at all (2–3 days).** Replay `weave check --against <parent>`
over QR-gent's history and ~10 public repos. Label every exit 1: escaped break, false
positive, or transitional. Kill: under ~1 escaped break per 200 commits — then weave stays a
personal tool and the rest of this document is moot. This is the only step that must happen
first.

**Step 2 — the gate where the agent lives (shipped).** `check --json` is still missing, but the
part that matters exists: `weave hook` exits 2 with the failing `file:line` on stderr,
`weave init --hooks` writes the Claude Code `Stop` hook, and every run lands in
`.weave/fires.jsonl`. `weave fires --since 7` is the experiment's instrument. The PR comment
is not built; the replay says it would be a green check.

**Step 3 — the local daemon (1 week).** The narrow one from §3, plus `/check` and
`/graph.json`. This is what turns three clients into one index and lets a hook be a `curl`.

**Step 4 — a generated wiki, read-only, freshness-stamped (shipped).** `weave wiki` writes
488 pages on QR-gent: one per module, endpoint, screen, table and change, plus an index
that answers "gaps in proof" before it shows a long list. Every page states what the thing
is, what connects to it, what proves it (with the command) and which decisions were noted
beside it. `stamp.json` records the commit, so `weave wiki --check` fails when the pages
describe an older tree — the "docs that cannot silently rot" claim, now real. `--out
docs/wiki` makes the site committable.

**Step 5 — decisions, not documents (only if Step 1 and 5 outside users say go).** The
research's lighter unit of intent is a one-line decision with its reason and its reach.
`weave remember` already stores notes next to files; the expansion is to surface them beside
the change they explain and to flag a decision whose files moved. Still no spec authoring.

### What "AI OS" would need that weave does not have

A shell, a memory model, tool permissions, and a reason for anyone to trust it with both.
Recommendation: drop the framing from the pitch, keep the ambition as "the proof layer of an
agent workspace" — the part that says whether the work is done. If a platform emerges around
weave, it will be built on the gate, not on the wiki.

## 5. Not building, for 90 days

- Explorer rework beyond what shipped (Stitch screens, compare mode, app specs, workflows,
  screenshots, janitor).
- New MCP tools, `ask` improvements, memory features.
- Spec authoring, Spec Kit/Kiro task interop.
- Multi-project switcher work.
- Remote or team hosting of any kind.
- A wiki editor, comments, or anything with a login.

## 7. Replay: the result (2026-11-03)

Method: one reusable git worktree per repo, `weave check --against <parent>` at each
commit. Parent graphs came from `baseline.GraphAt`, and no comparison fell back to the
local index, so every run compared against a real historical graph.

| repo | commits | endpoints | gate fired | true breaks | false positives |
|---|---|---|---|---|---|
| QR-gent (Express + Prisma + React, agent-driven) | 122 | 248 | 3 → 0 after fixes | 0 | 3 |
| shadcn-ui/taxonomy (Next App Router) | 134 | 9 | 0 | 0 | 0 |
| vercel/nextjs-subscription-payments (Next + Supabase) | 154 | 3 | 0 | 0 | 0 |
| gothinkster/node-express-realworld (Express + Prisma) | 65 | 19 | 0 | 0 | 0 |
| trpc/examples-next-prisma-starter (tRPC, half-blind) | 150 | 2 | 0 | 0 | 0 |
| **total** | **625** | | **3** | **0** | **3 (fixed)** |

**Verdict: the gate is a vitamin on this evidence. Kill the CI/PR product.**

The criterion was ≥1 escaped break per 200 commits. 625 commits produced zero, and the
places where a break *could* have appeared were real: nine commits deleted a route file or
an endpoint outright, and in every case the caller was deleted or updated in the same
commit. Public repos of this kind do not ship broken FE↔API contracts; they either fix them
inside the commit or they never had the coupling.

Three things this does not prove, stated so the verdict is not overclaimed:

- **Working-session breaks are invisible here.** A break introduced by one agent edit and
  fixed by the next never reaches a commit. The replay can only see what shipped.
- **`examples-next-prisma-starter` is half-blind**: procedures written
  `list: publicProcedure.input(z.object({…})).query(…)` are not parsed (1 of 2 routers), so
  tRPC removals there are invisible. That is a real extractor gap against the README's
  "tRPC routers, exact" claim.
- **False positives were the finding that mattered.** Before the fixes, 3 of 30
  route-touching commits failed — one of them a commit that only changed a type annotation.
  At that rate the gate would have been switched off in a week. Both causes are fixed and
  tested: a variable glued to literal text (`/tasks${qs}`) is unknowable, and a match needs
  one literal segment in common (so `users/:id` no longer "matches" `api.get('/reports…')`).

What remains worth one week: the **Stop hook**, which tests the one hypothesis the replay
cannot reach — that breaks exist inside a session and vanish before commit. Everything in
steps 3–5 of the ladder above is dead until that experiment shows a fire.


## 8. The numbers that decide each gate

| gate | metric | threshold |
|---|---|---|
| Step 1 | escaped breaks per 200 commits | ≥ 1 |
| Step 1 | false positives on the replay set | 0 |
| Step 2 | gate still blocking on day 14 (5 outside devs) | 4 of 5 |
| Step 3 | hook latency at p95 | < 400 ms |
| Step 4 | wiki pages stale at 7 days | 0 |
| Step 5 | users who read a decision note monthly | 3 of 5 |
