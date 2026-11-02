# Go migration: plan and Phase 0 evidence

Status: **complete.** Phases 0–6 landed and the TypeScript implementation was removed at
cutover. The Go binary is the product: same CLI, same MCP schemas, same explorer bytes.

This file records how the port was measured and gated. It is history, not a plan.

## Progress (2026-11-02)

- **Phase 1 done**: `jsjson`, `jsstr`, model, scan, store. Both existing graphs re-encode
  byte-identically, including through the typed model.
- **Phase 2 done**: extractors, build, reindex, and `weave index` in Go. **G1 passed
  strict** — same bytes as TypeScript on `test/fixture` and on QR-gent (937 files,
  2,513 nodes, 5,694 edges), no `--unordered`.
- Warm index on QR-gent: **Go 278 ms, Node 780 ms**; extraction alone 92 ms vs 205 ms.
  A freshly copied tree on Windows costs ~3.9 s on the first Go run and 0.28 s on the
  next, while Node stays at ~0.78 s. That is Windows process-reputation/AV scanning of
  a new unsigned binary, not a walk regression; confirm on a fresh copy before treating
  it as one. Linux CI does not show it.
- Helper packages ported: `perf`, `registry`, `junit`, `runners`.
- **Phase 3 done**: `query`/`memory`, CLI wiring, `check`, `init`, `ask`, and the
  feature packages (`grep`, `usage`, `delta`, `baseline`).
- **Phases 4–6 done**: MCP server and schema goldens, explorer renderer
  (47,388 bytes identical), Go action, release workflow, three-OS CI.
- **Cutover**: `src/` and the Node tooling were deleted; the Go suite now holds the
  goldens (build graph, MCP schemas, explorer HTML) and an integration suite that
  drives the real binary. No Node at runtime or in tests.

## Gates

| # | Phase | Exit gate |
|---|---|---|
| 0 | Freeze, spikes, harness | Self-parity clean; the three spikes below |
| 1 | Foundations (`jsstr`, `jsjson`, model, scan, store) | Every `graph.json` re-encodes byte-identically |
| 2 | Index (extract, specs, build) | **G1** canonical graph equality on fixture, mutations, edge corpus, QR-gent |
| 3 | CLI (`query`, `check`, `init`, `perf`, `evidence`) | **G2** byte-equal stdout, equal exit codes, cross-read graphs |
| 4 | MCP + `ask` | **G3** transcript and `tools/list` goldens byte-equal |
| 5 | Explorer | **G4** `explore.html` byte-identical from the same graph |
| 6 | CI, perf, distribution, cutover | **G5** Go suite green on three OSes; targets met; cutover |

Parity was proven by a Node harness (removed at cutover) that ran one implementation
twice and diffed graph JSON, fifteen CLI commands, an MCP transcript and the explorer
HTML, with timing, dates, heap and file sizes masked. It was then run with
`--a "node src/cli.ts" --b "bin/weave"` until both sides matched.

## Findings

### 1. Directory order differs on Windows (`spike-readdir.mjs`)

Node returns the NTFS case-insensitive order (`alpha.ts apple apple.ts Beta.ts …`);
Go's `os.ReadDir` sorts bytewise (`Beta.ts README.md Zed … alpha.ts`). On Linux
Node uses `strcmp`, which matches Go. Strict gates therefore run on Linux; Windows
runs `--unordered` and compares exit codes exactly. Not a semantic difference.

### 2. `localeCompare` is not byte order (`spike-locale.mjs`)

2198 of 9025 printable-ASCII pairs compare differently from codepoint order in
`en-US`, and 2416 of 10000 random Polish/ASCII pairs do. The ICU order starts:

```
" _-,;:!?.'"()[]{}@*/\&#%`^+<=>|~$0123456789aAbBcCdDeEfFgGhHiIjJkKlLmMnNoOpPqQrR
```

`sort.Strings` would reorder list output. A `jsstr.LocaleCompare` built from this
oracle is required; non-ASCII falls back to byte order and is a documented divergence.

### 3. The regex cliff (`spike-regex.mjs`, `spike-scan.go`)

A literal port of the ten hottest patterns, prefilters included, is **7.2× slower**
than V8 on the 9.4 MB QR-gent corpus (947 ms vs 131 ms). `CALL_RE` 15.8×, `FETCH_RE`
28.5×, `CALLISH` 18.0×, `REQ_ID` 14.4×.

The cause is not RE2 generally, it is `\b`: a leading word boundary defeats RE2's
literal-prefix optimisation.

| pattern over 9.4 MB | Go ms |
|---|---|
| `fetch` | 2.0 |
| `\bfetch` | 224.5 |
| `\bfetch\s*\(` | 226.5 |
| `req literal` | 1.0 |
| `\b(?:REQ\|FR\|NFR)-…` | 301.3 |

Hand scanners avoid it. They reproduce the four hot shapes, count **exactly** like
the Node regexes on QR-gent, and are at least as fast:

| scanner | matches | Go ms | Node ms |
|---|---|---|---|
| calls | 339 | 10.2 | 13.5 |
| fetch | 6 | 1.0 | 2.5 |
| tRPC | 0 | 4.6 | 7.2 |
| requirements | 254 | 0.5 | 0.8 |

Consequences for the plan:

- **P2 grows by ~2 days**: `CALL_RE`, `FETCH_RE`, `TRPC_CALL`, `REQ_ID`, `IMPORT_RE`
  and `CALLISH` become scanners; regex stays for line-anchored, prefix-friendly
  patterns (`ROUTER_VERB` 4.1 ms, headings, task lines).
- Import parsing becomes a line scan: `IMPORT_RE` is 280 ms with the only useful
  prefilter (`from`) because nearly every code file matches it; the literal scan is
  ~3 ms.
- The parity corpus gains a per-pattern match count on both corpora, so a scanner
  that is one match off fails G1.
- Kill criterion (“Go extraction > 2× slower even with prefilters”) is **not**
  triggered once scanners are the strategy.

### 4. Harness (removed at cutover)

`node test/parity/run.mjs` proved byte parity on the graph, fifteen CLI commands, the
MCP transcript and the explorer HTML. It required both implementations, so it was
deleted with the TypeScript one. The same contracts now live in Go tests:

- `internal/weave` — fixture graph golden, explorer HTML golden, model round-trip;
- `internal/mcp` — `tools/list`/`initialize` goldens for four modes, transcript tests;
- `test/integration` — the real binary on the fixture: index, context (names and code),
  impact, search, grep, notes, delta, `check` failing on a renamed route, explore, MCP,
  gain, init.

## Cost and cutover

~31 person-days (range 24–35), up from 26: the scanners added two, and the five
features had to ship before the parity oracle went away. Phases, layout, distribution
and cutover followed the plan reviewed with Opus 5.5:

- one Go package whose files mirror the former TS files; `cmd/weave` for the CLI;
- `go:embed` + concatenation for the explorer, CSS/JS extracted as shared assets;
- stdlib MCP, byte-identical tool schemas and error shapes;
- GitHub release binaries + `go install`, no npm wrapper; existing
  `node …/src/cli.ts` MCP entries are replaced by running `weave init` again (no shim
  was shipped; the old path no longer exists);
- the five LeanCTX-style features shipped before cutover and were ported with the rest.

### Feature parity additions

The port preserved the new surface:

| feature | CLI | MCP | persisted |
|---|---|---|---|
| delta | `weave delta [ref]`, `--from`, `--all` | `changed` | none (git worktree or a saved graph) |
| text search | `weave grep`, `--regex`, `--limit` | `search(text:true)` | none |
| notes | `remember`, `recall`, `memory` | `remember`, `recall` | `.weave/memory.jsonl` |
| cost | `gain --since` | receipt on every answer | `.weave/usage.jsonl` |
| code depth | `context --depth code` | `context(depth:"code")` | none (reads at answer time) |

The shipped layout: `internal/jsstr` (locale oracle, UTF-16 length/slice) and
`internal/jsjson` (ordered objects, escaping, number formatting) underpin the model,
scan, store, extractors and renderer; `cmd/weave` is the CLI and MCP host.

## Running the suite

```bash
go test ./...
go test ./test/integration -v   # builds and drives the real binary
```

The Phase 0 spikes and the TypeScript parity harness were removed at cutover; their
findings are in this file and in git history.
