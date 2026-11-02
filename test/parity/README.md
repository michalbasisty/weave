# Parity harness and Phase 0 spikes

The Go port is gated on byte parity, not on a passing test suite. This directory is
the instrument for that gate and the measurements that decided to attempt the port.
The plan and findings live in [`docs/GO-MIGRATION.md`](../../docs/GO-MIGRATION.md).

## Harness

```bash
node test/parity/run.mjs
node test/parity/run.mjs --unordered            # Windows directory order
node test/parity/run.mjs --only graph,cli       # a subset
node test/parity/run.mjs --a "node src/cli.ts" --b "bin/weave.exe"   # at cutover
```

It copies `test/fixture` to a temp directory, gives each side its own `WEAVE_HOME`,
and compares:

- `graph`: `.weave/graph.json` with `builtAt` and `perf` removed, re-encoded by the
  same JSON parser (key order preserved).
- `cli`: bare `weave`, `status`, `context`, `impact`, `search`, `plan`, `change`,
  `doctor`, `check`, `evidence`, `perf --runs 1`, `projects` — stdout and exit code.
- `mcp`: `initialize`, `tools/list`, a `context` call, an unknown tool, an unknown
  method — the raw transcript.
- `html`: `explore` written from the same fresh index.

Masks: ISO and locale timestamps, durations, heap, file sizes, and the `perf` trend.
`perf` column padding is also collapsed because it follows real timings. Anything
else that differs is a failure. `--unordered` additionally sorts nodes, edges and
silent-file lists; use it only on Windows, where Node and Go list directories
differently.

## Spikes

| file | question |
|---|---|
| `spikes/spike-readdir.mjs` | does Go's sorted `ReadDir` match Node's OS order? |
| `spikes/spike-locale.mjs` | how far is `localeCompare` from byte order? |
| `spikes/spike-regex.mjs` | the ten hot regexes, with the port's prefilters |
| `spikes/spike-scan.go` | the same shapes as hand scanners, timed and counted |

The `go` spike files are standalone single-file programs (`go run file.go <repo>`),
so they need no module and no dependencies. They are evidence, not product code.

Standing rule: a scanner is not accepted until it counts exactly like the Node
regex on QR-gent and on the edge corpus (BOM, CRLF, emoji, invalid UTF-8,
Polish text, mixed-case siblings).
