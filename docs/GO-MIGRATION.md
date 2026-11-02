# Go migration: plan and Phase 0 evidence

Status: **Phase 0 complete.** Decision: **GO**, with one revised assumption — the hot
extractors must be hand scanners, not ported regexes.

The TypeScript implementation stays the shipped default until the parity gates pass.
Nothing here is a rewrite commitment; it is the measured basis for one.

## Gates

| # | Phase | Exit gate |
|---|---|---|
| 0 | Freeze, spikes, harness | Self-parity clean; the three spikes below |
| 1 | Foundations (`jsstr`, `jsjson`, model, scan, store) | Every `graph.json` re-encodes byte-identically |
| 2 | Index (extract, specs, build) | **G1** canonical graph equality on fixture, mutations, edge corpus, QR-gent |
| 3 | CLI (`query`, `check`, `init`, `perf`, `evidence`) | **G2** byte-equal stdout, equal exit codes, cross-read graphs |
| 4 | MCP + `ask` | **G3** transcript and `tools/list` goldens byte-equal |
| 5 | Explorer | **G4** `explore.html` byte-identical from the same graph |
| 6 | CI, perf, distribution, cutover | **G5** 23 tests on three OSes, targets met, two weeks dogfooding |

Parity harness: `node test/parity/run.mjs` runs one implementation twice and diffs
graph JSON, twelve CLI commands, an MCP transcript and the explorer HTML. Timing,
dates, heap and file sizes are masked; `perf` column padding is normalized because
it follows the real timings. At cutover, `--a "node src/cli.ts" --b "bin/weave"`.

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

### 4. Harness

`node test/parity/run.mjs` is clean in ~4.7 s: graph, twelve CLI commands (including
`check` and `perf`), a five-request MCP transcript, and the explorer HTML. It proves
the masks and determinism before any Go exists; the only mask that needed a
correction was `perf`'s timing-dependent column padding and the trend percentage.

## Revised cost and next step

~28 person-days (range 22–32), up from 26, because of the scanners. Phases, layout,
distribution, cutover and rollback are unchanged from the plan reviewed with Opus 5.5:

- one Go package whose files mirror the TS files; `cmd/weave` for the CLI;
- `go:embed` + concatenation for the explorer, CSS/JS extracted as shared assets;
- stdlib MCP (~350 lines), byte-identical tool schemas and error shapes;
- GitHub release binaries + `go install`, no npm wrapper, a shim for existing
  `node …/src/cli.ts` MCP entries that never looks up `weave` on PATH;
- the five unbuilt LeanCTX features are written in Go after G5, never in TS.

Phase 1 starts with `internal/jsstr` (locale oracle, UTF-16 length/slice) and
`internal/jsjson` (ordered objects, escaping, number formatting), then model, scan
and store — the pieces that make a byte-identical `graph.json` possible.

## Running Phase 0

```bash
node test/parity/run.mjs                 # self-parity, fixture corpus
node test/parity/spikes/spike-readdir.mjs
node test/parity/spikes/spike-locale.mjs
node test/parity/spikes/spike-regex.mjs  # Go regex vs Node, prefilters
go run test/parity/spikes/spike-scan.go G:/projekty/QR-gent
```

Requires Go 1.26 (installed) and Node 22.6+.
