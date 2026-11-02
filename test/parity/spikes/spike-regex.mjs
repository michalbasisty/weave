#!/usr/bin/env node
// Spike C, fair version: same regexes, same scopes, same literal prefilters on
// both sides. The decision is the go/node ratio per pattern, and the counts must
// match exactly.
import { readdirSync, readFileSync } from 'node:fs';
import { join, extname, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const root = resolve(process.argv[2] || 'G:/projekty/QR-gent');
const codeExt = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const specExt = new Set(['.md']);
const allExt = new Set([...codeExt, ...specExt, '.prisma', '.json']);
const skip = new Set(['node_modules', '.git', '.weave', 'dist', 'build', 'coverage']);

function walk(dir, out = []) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.isDirectory()) {
      if (!skip.has(e.name)) walk(join(dir, e.name), out);
    } else if (allExt.has(extname(e.name).toLowerCase())) {
      out.push(join(dir, e.name));
    }
  }
  return out;
}

const texts = walk(root)
  .filter((f) => {
    try { return readFileSync(f).length <= 1 << 20; } catch { return false; }
  })
  .map((f) => {
    const ext = extname(f).toLowerCase();
    return { body: readFileSync(f, 'utf8'), code: codeExt.has(ext), spec: specExt.has(ext) };
  })
  .sort((a, b) => (a.body < b.body ? -1 : a.body > b.body ? 1 : 0));

const anyOf = (body, pre) => !pre || pre.some((s) => body.includes(s));

const patterns = [
  { name: 'IMPORT_RE', scope: 'code', pre: ['from'], re: /(?:^|\n)\s*(?:import|export)[^'"]*?from\s*['"]([^'"]+)['"]/g },
  { name: 'ROUTER_VERB', scope: 'code', pre: ['router'], re: /router\.(get|post|put|patch|delete)\s*\(\s*['"`]([^'"`]*)['"`]/g },
  { name: 'CALL_RE', scope: 'code', pre: ['api', 'http', 'axios', 'client', 'fetcher'], re: /\b(?:api|http|httpClient|axios|client|fetcher)\s*\.\s*(get|post|put|patch|delete)\s*\(\s*[`'"]([^`'"]+)[`'"]/g },
  { name: 'FETCH_RE', scope: 'code', pre: ['fetch'], re: /\bfetch\s*\(\s*[`'"]([^`'"]+)[`'"]\s*(?:,\s*\{[^}]*method\s*:\s*['"](\w+)['"])?/g },
  { name: 'TRPC_CALL', scope: 'code', pre: ['trpc'], re: /\btrpc(?:Client|Proxy)?\s*\.\s*([\w$.]+?)\s*\.\s*(useQuery|useMutation|useInfiniteQuery|useSuspenseQuery|useQueries|query|mutate)\s*\(/g },
  { name: 'CALLISH', scope: 'code', pre: ['fetch', 'axios', 'useQuery', 'useMutation', 'useSWR', '$fetch', 'trpc', 'ky(', 'got('], re: /\bfetch\s*\(|\baxios\s*\.\s*(get|post|put|patch|delete|request)\s*\(|\buse(Query|Mutation|SWR|InfiniteQuery)\s*\(|\$fetch\s*\(|\btrpc\s*\.|\bky\s*\(|\bgot\s*\(/g },
  { name: 'REQ_ID', scope: 'spec', pre: ['REQ-', 'FR-'], re: /\b(?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b/g },
  { name: 'HEADING', scope: 'spec', pre: ['#'], re: /^(#{1,5})\s+(.*)$/g },
  { name: 'TASK_START', scope: 'spec', pre: ['- ['], re: /^-\s+\[( |x|X)\]\s+(.*)$/g },
  { name: 'TASK_ID', scope: 'spec', pre: null, re: /^\**((?:[A-Z]{0,2}\d+(?:\.\d+)+)|(?:T\d+)|(?:\d+\.))\**\s+/g },
];

const nodeResults = [];
for (const p of patterns) {
  let best = Infinity;
  let files = 0;
  let matches = 0;
  for (let round = 0; round < 3; round++) {
    const t0 = performance.now();
    let f = 0;
    let n = 0;
    for (const t of texts) {
      if (p.scope === 'code' && !t.code) continue;
      if (p.scope === 'spec' && !t.spec) continue;
      if (!anyOf(t.body, p.pre)) continue;
      f++;
      for (const _ of t.body.matchAll(p.re)) n++;
    }
    const ms = performance.now() - t0;
    if (ms < best) { best = ms; files = f; matches = n; }
  }
  nodeResults.push({ pattern: p.name, files, matches, ms: best });
}

const go = spawnSync('go', ['run', join(import.meta.dirname, 'spike-regex.go'), root], { encoding: 'utf8', maxBuffer: 64 << 20 });
if (go.status !== 0) {
  console.error(go.stderr);
  process.exit(1);
}
const parsed = JSON.parse(go.stdout);
const goResults = parsed.patterns;

console.log(`corpus: ${texts.length} files — ${root}`);
console.log('pattern       files  matches   node ms    go ms   go/node');
let bad = 0;
for (let i = 0; i < patterns.length; i++) {
  const n = nodeResults[i];
  const g = goResults[i];
  if (n.matches !== g.matches) bad++;
  console.log(
    `${n.pattern.padEnd(12)} ${String(n.files).padStart(6)} ${String(n.matches).padStart(8)} ${n.ms.toFixed(1).padStart(9)} ${g.ms.toFixed(1).padStart(8)} ${(g.ms / n.ms).toFixed(2).padStart(9)}`,
  );
}
const sum = (rows, k) => rows.reduce((a, r) => a + r[k], 0);
console.log(`\ntotal        ${' '.repeat(6)} ${String(sum(nodeResults, 'matches')).padStart(8)} ${sum(nodeResults, 'ms').toFixed(1).padStart(9)} ${sum(goResults, 'ms').toFixed(1).padStart(8)} ${(sum(goResults, 'ms') / sum(nodeResults, 'ms')).toFixed(2).padStart(9)}`);
console.log(`prefilter cost (Go, all files): ${parsed.prefilterMs.toFixed(1)} ms`);
console.log(bad ? `\n${bad} pattern(s) count differently — fix the port's semantics first` : '\nall ten patterns count identically');
