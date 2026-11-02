# Design: what the research changed

The explorer's visual contract lives separately, in
[`explorer/DESIGN.md`](explorer/DESIGN.md); this file is about the product's research.

Research question: what is the right shape for a tool that makes spec-driven work
survive contact with a real codebase — universal across stacks, cheap enough that
agents actually call it, and strict enough that "nothing breaks" is a check rather
than a hope.

Four findings changed the design. Each one removed work rather than added it.
A fifth closes the loop the other four leave open.

---

## 1. Do not build a symbol graph. It already exists, twice.

**Serena** is an MCP server with 40+ languages of symbol-level retrieval over LSP —
`find_symbol`, `find_referencing_symbols`, `find_implementations`, rename, safe
delete. It is the de-facto answer for "give the agent IDE navigation", and it has
already solved the hard part: driving one language server per language, caching
symbols by content hash, restarting dead servers.

**SCIP** is the other answer: a language-agnostic index format emitted by
`scip-typescript`, `scip-go`, `scip-java`, `scip-python`, `scip-php`, and rust-analyzer.
It is exact, but it requires a per-language indexer and a typechecker-grade build,
usually in CI.

Either way, a new project writing its own per-language AST resolution loses. Worse,
it produces a graph that is *approximately* right, which agents then act on.

**Decision:** weave owns no symbol resolver. It uses imports and the semantic
contracts a framework publishes, and it says `exact` or `inferred` on every edge.
When a SCIP index or an LSP server is present, that is an upgrade path — not a
requirement, and not something weave reimplements.

**Why this is not a gap:** Serena answers "who references this function". Nobody
answers "which React page calls this endpoint, which table does it write, which spec
change owns it, and what breaks if I touch it". That is the semantic layer, and it is
not in the type system. That is the product.

---

## 2. Test selection is already solved — hand back the native command

Every ecosystem has affected-test selection:

| ecosystem | mechanism | caveat |
|---|---|---|
| vitest | `--changed <ref>`, `--since-last-run` | `--since-last-run` records the files each test actually loaded, including dynamic imports, and needs no git |
| jest | `--changedSince`, `--findRelatedTests <files>` | needs a static dependency graph (no dynamic `require`) |
| nx | `nx affected -t test` | selects by *declared inputs*; a missing input silently under-selects |
| playwright | `playwright test <files>` | none |
| phpunit / go test | file arguments / package paths | none |

Reimplementing this from a hand-built graph would be strictly worse than the runner's
own resolution, which sees dynamic imports, mocks, setup files and config.

**Decision:** detect the runner per package from `jest.config.*`, `vitest.config.*`,
`vite.config.*`, `package.json` deps, and the file's path, then emit the command:

```
run
  cd packages/backend && npx jest src/__tests__/orders.domain.test.ts
  cd packages/web && npx playwright test e2e/admin.spec.ts e2e/tasks.spec.ts
```

This is the single largest cost win. An agent that runs 6 named tests instead of 85
plus a 3-minute e2e suite is not marginally cheaper — it is a different workflow.
Grouping by package/tool also means the command is copy-pasteable, not a puzzle.

---

## 3. Traceability needs an ID *in the code*, not a file path in a spec

The mature requirements tooling converged on the same shape:

- **Doorstop**: every item is a file with a UID and `links:` to parents, plus a
  fingerprint. When a parent's fingerprint changes, the link becomes **suspect** until
  reviewed. That is exactly the "a requirement moved, did the code follow?" question.
- **Sphinx-Needs**: typed items with `:id:` and `:links:`, and its companion
  **Sphinx-CodeLinks** reads *one-line markers in source comments* that name a need ID,
  tracing code back to the requirement.
- **Spec Kit** and **Kiro** both fix IDs at authoring time (`FR-001`, `Requirement 1`,
  EARS `WHEN … THE SYSTEM SHALL …`), because an id is what a task and a test can cite.

QR-gent's convention is the weak end of this spectrum: `tasks.md` carries
`Evidence: \`modules/orders/orders.routes.ts\`` — a **path**, which a rename destroys
and a move invalidates. And its `spec-delta.md` files are prose: the template promises
`### ADDED REQ-<CAP>-<n>`, practice never adopted it. A tool that demands that format
fails on the repo it was built for; a tool that ignores it has no traceability at all.

**Decision:** support three levels, weakest to strongest, and never require the top:

1. `Evidence:` paths → `touches` (inferred when a directory, exact when a file).
2. A stable id anywhere (`REQ-*`, `FR-*`, `NFR-*`) → `satisfies` / `specified_in`.
3. `// @req REQ-ORD-1` in the code → `implements`, exact, survives renames.

Then add the Doorstop idea, which no MCP server has: hash each requirement's text at
index time. If the text changes between indexes, every file and test linked to it is
reported as **suspect**. It costs one `sha256` and one string compare, and it is the
only mechanism that catches "the spec moved and the code did not".

---

## 4. Fewer, intent-shaped tools; the answer should be the action

Anthropic's own guidance is unambiguous: fewer well-described tools beat mirrors of
an API; consolidate multi-step retrieval into one call (`get_customer_context` rather
than `get_customer` + `list_transactions`); cap responses; and put the guidance in the
tool description and server instructions, because those are paid on every session.

The scale of the problem is documented: MCP tool definitions alone have been measured
at 55K–134K tokens across a handful of servers, and Code Mode (two tools, ~1K tokens)
covers 2,500 endpoints at 99.9% less context. weave is nowhere near that problem —
five tools cost about 1.2K tokens — but the lesson still applies at our size: the
tool should return the *decision*, not the data to make it with.

**Decision:**

- `context(files)` is the entry point and the default. It answers page, module,
  endpoints, tables, owning spec change, open tasks, linked requirements, tests **and
  the command to run them**, in a hard token budget (default 350).
- `impact(files)` exists only for the complete list.
- No tool returns file contents, ever.
- Enforcement is **not** a tool. An agent should not gate itself. `weave check` is a
  CLI that any harness can call on its own terms — git pre-commit, CI, a Claude Code
  `PostToolUse` hook, a Kiro `PostFileSave` hook. Same command, no cooperation needed.

`check` fails on the one thing that is unambiguously broken: **a call site in the
changed files that matches no endpoint**. On QR-gent (745 files, 196 endpoints) it
reports zero — the useful property of a gate is that it is quiet on a healthy repo.

---

## 5. Linking is not proof — the loop has to close on a test result

Doorstop and Sphinx-Needs stop at the link. A link says "this code belongs to
`REQ-ORD-1`"; it does not say the requirement holds. Faced with that gap, every
traceability tool grows a review step, and every review step is skipped under deadline.

The universal evidence format already exists and is written by every runner:
**JUnit XML**. `jest-junit`, `vitest --reporter=junit`, `playwright --reporter=junit`,
`phpunit --log-junit`, `go-junit-report`. So the full loop can be closed without
inventing a format and without trusting the agent:

```
spec text ──hash──▶ REQ-ORD-1 ──@req──▶ route file
                                  └──@req──▶ test file ──runs──▶ JUnit XML
                                                                      │
                                          weave evidence ◀────────────┘
                                                   │
                                             check: exit 1
```

**Decision:** `@req` in the *test* file creates a `verifies` edge; `weave evidence`
joins those edges to a JUnit report and reports `pass` / `FAIL` / `no test` per
requirement. `check` fails when a requirement implemented in the changed files has a
failing test, and warns when it is implemented but never exercised.

The deliberate omission: `run` does **not** inject `--reporter=junit` into the command
it prints. The flags differ per runner, `jest` needs an extra package, and a wrong flag
turns a working test command into a broken one. Enabling evidence is a one-line change
in the project, documented in `init`, not something the tool silently guesses.

---

## 6. Recall is the number that decides trust — so it is measured and printed

The first version reported the wrong metric. "Zero false positives" is precision: it says
we never name a caller that is not one. It says nothing about the route file the regex
walked past, and that miss is silent — `impact()` returns a confident answer that is
incomplete. For a tool whose whole job is "what will break", an invisible miss is the
only unforgivable failure.

Measured on the first real repo, the gaps were real: 5 of 87 `tasks.md` files produced no
tasks because they use `- [x] B1.1 …` and unnumbered checklists, not the `T1` the parser
expected. That is 6% of the spec graph missing, silently.

**Decision:** the index records its own coverage — route-like files that yielded no
endpoints, client calls that matched nothing, `<Route>` entries without a component,
`tasks.md` files that parsed to nothing — and `doctor` prints it, naming the files. A
file that looks like a contract and produces nothing is never dropped quietly.

This is also the honest differentiator: every competitor ships silent gaps. We are the
one that says where.

### Two corrections to earlier claims

- **"We never build a symbol graph" was too clean.** Attributing a component's API call
  to the page that renders it *is* symbol resolution, done by regex over imports — it
  breaks on barrel files, path aliases and re-exports. The line is real (no LSP, no AST)
  but it is not free. It is now a measured gap, not a claim.
- **The reason `check` is a CLI, restated.** "An agent must not satisfy its own gate" is
  wrong: the agent has a shell and can run `check` either way. The mechanism that makes a
  gate real is CI, not the tool-vs-CLI taxonomy. It stays a CLI because an exit code is
  what a pipeline can act on, and because the gate must survive the conversation ending.

### Known soft spots, stated rather than hidden

- `suspect` is advisory only: it warns, it never fails `check`. A typo fix in a
  requirement must not block a merge, or teams disable the feature inside a month.
- The JUnit join is by **file path first, test name second**. Runners disagree about
  report paths (`jest-junit`, vitest and playwright all differ), so an unmatched case
  falls back to a `REQ-…` id in the test name — the one place file names prove unreliable.
- Coverage is per-extractor, not per-language. A Next.js route handler is simply not
  seen, and the coverage block says so by omission only if no route-like file exists.

---

## What was deliberately not built

| not built | because |
|---|---|
| symbol/AST index per language | Serena and SCIP already do it, better, and a wrong one is worse than none |
| embedding / vector search | symbol and path ranking beat it on code, and it costs a model call |
| a new spec format | three conventions in the wild already; parse, never enforce |
| a test-impact algorithm | the runners' own resolution sees dynamic imports; ours would not |
| a database or daemon | one JSON file, rebuilt in ~1.2 s for 745 files |
| a web app for humans | a single generated HTML file; the graph is a *neighbourhood around a selection*, never a 1,900-node hairball |
| a code-execution sandbox | the win (2 tools) is real at 2,500 endpoints, not at 6 |
| gates run by the agent | the agent must not be able to satisfy the gate by claiming victory |
| injecting `--reporter=junit` into `run` | one wrong flag breaks a working test command |

---

## Measured on a real repo (QR-gent: Express 5, Prisma 7, React, 745 files)

| fact | value |
|---|---|
| index | 1.2 s, 1,888 nodes, 3,865 edges, one JSON file |
| warm `tools/call` | 57 ms (stat sweep, then in-memory) |
| median response | 1.4 KB |
| endpoints / pages / tables | 196 / 33 / 46 |
| spec change records with task→file edges | 87 (597 tasks) |
| `context()` on a page | 12 lines, ~350 tokens, names 7 tests and 2 commands |
| `plan()` on a change | 5 lines, ~150 tokens, no `tasks.md` read |
| `explore` | one 281 KB HTML file, 378 ms to generate, no server |
| broken call sites (false positives after fixes) | 0 |
| second index, unchanged tree | no spurious `suspect` |

Extraction bugs the real repo exposed, all now covered by tests:

- multi-line `router.put(` declarations were missed (196 endpoints, not 195);
- template literals containing nested quotes (``api.get(`/projects${archived ? '?x' : ''}`)``)
  fabricated two "broken" calls;
- `specs/templates/tasks.md` was counted as a change record;
- the rename gate compared full endpoint paths against mount-relative call paths and
  matched nothing — it needed tail alignment.

A gate with false positives or false negatives is dead on arrival, so the suite now
contains the failing case as a test: rename a route in the fixture, and `check` must
exit 1 naming the caller's `file:line`, then go quiet again once the route returns.

---

## Still unproven

1. **Does `context` actually change agent behaviour?** The claim is that one 350-token
   call replaces explore + grep + 3 reads. That needs an eval on real tasks, not a
   demo — Anthropic optimises tools against an evaluation set, and we have none yet.
2. **Is `suspect` used, or ignored?** Doorstop needs a review step. We flag and never
   clear. If agents learn to ignore the line, the feature is decoration.
3. **Framework coverage is the moat and the cost.** Express + Prisma + React is one
   extractor. Nest, Fastify, tRPC, GraphQL, Django, Rails, Symfony, Laravel: each is
   small, but the product only feels universal when the target repo is covered.
   The cheapest next step is OpenAPI as a generic endpoint source, since most backends
   can emit it regardless of language.
4. **Spec Kit / Kiro interop is untested.** Their ids are parsed; their `tasks.md`
   shapes are not. If agents arrive pre-trained on `/speckit.*`, matching artifact
   names matters more than any clever extraction.

---

## Sources

- Serena — LSP-backed symbol retrieval, 40+ languages, v2 REPL for composition:
  <https://github.com/oraios/serena>, <https://oraios.github.io/serena/01-about/035_tools.html>
- SCIP indexers and coverage: <https://scip-code.org/>, <https://sourcegraph.com/docs/code-navigation/precise-code-navigation>
- Anthropic, writing effective tools for agents: <https://www.anthropic.com/engineering/writing-tools-for-agents>
- Anthropic, code execution with MCP (150K → 2K tokens; progressive disclosure): <https://www.anthropic.com/engineering/code-execution-with-mcp>
- Cloudflare Code Mode (2,500 endpoints, ~1K tokens): <https://blog.cloudflare.com/code-mode-mcp/>
- Nx affected, declared-input caveat: <https://nx.dev/docs/features/ci-features/affected>
- vitest `--changed` vs `--since-last-run`: <https://main.vitest.dev/guide/recipes/affected-tests>
- jest `--findRelatedTests`, `--changedSince`: <https://jestjs.io/docs/cli>
- Doorstop items, links, fingerprints, suspect links: <https://doorstop.readthedocs.io/en/latest/reference/item.html>
- Sphinx-Needs links, conditional links, Sphinx-CodeLinks source markers: <https://sphinx-needs.readthedocs.io/en/stable/>
- GitHub Spec Kit templates (`FR-###`, Given/When/Then, tasks): <https://github.com/github/spec-kit>
- Kiro EARS requirements and hooks: <https://kiro.dev/docs/specs/feature-specs/>, <https://kiro.dev/docs/hooks/>
