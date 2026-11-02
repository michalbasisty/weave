#!/usr/bin/env node
// Phase 0 parity harness. Runs one implementation twice over the same fixture and
// diffs the surfaces that must survive the Go port: graph JSON, CLI stdout/exit,
// MCP transcripts, explorer HTML. Timing, dates, heap and file sizes are masked.
//
// Today: proves the masks and the determinism. At cutover:
//   node test/parity/run.mjs --a "node src/cli.ts" --b "bin/weave.exe"

import { spawnSync } from 'node:child_process';
import { cpSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const argv = process.argv.slice(2);
const has = (name) => argv.includes('--' + name);
const opt = (name, fallback) => {
  const i = argv.indexOf('--' + name);
  return i < 0 ? fallback : argv[i + 1];
};

const A = opt('a', 'node src/cli.ts').split(' ');
const B = opt('b', opt('a', 'node src/cli.ts')).split(' ');
const only = opt('only', 'graph,cli,mcp,html').split(',').map((s) => s.trim());
const unordered = has('unordered');
const corpus = resolve(opt('corpus', 'test/fixture'));

const work = mkdtempSync(join(tmpdir(), 'weave-parity-'));
cpSync(corpus, work, { recursive: true });
rmSync(join(work, '.weave'), { recursive: true, force: true });

const masks = [
  [/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/g, '<iso>'],
  [/\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/g, '<time>'],
  [/\d{1,2}\/\d{1,2}\/\d{4},? \d{1,2}:\d{2}:\d{2}(?: [AP]M)?/g, '<date>'],
  [/\b\d+(?:\.\d+)?(?:ms|s)\b/g, '<dur>'],
  [/\b\d+(?:\.\d+)?MB\b/g, '<mem>'],
  [/\b\d+(?:\.\d+)?KB\b/g, '<size>'],
  [/(trend\s+)[+-]?\d+%/g, '$1<pct>'],
  [/(graph file\s+)\S+/g, '$1<size>'],
];
const mask = (s) => masks.reduce((acc, [re, to]) => acc.replace(re, to), String(s ?? ''));

const home = (side) => mkdtempSync(join(tmpdir(), `weave-home-${side}-`));

function run(impl, args, env) {
  return spawnSync(impl[0], [...impl.slice(1), ...args], {
    cwd: process.cwd(),
    encoding: 'utf8',
    timeout: 120000,
    env: { ...process.env, ...env },
  });
}

function indexGraph(impl, env) {
  rmSync(join(work, '.weave'), { recursive: true, force: true });
  const r = run(impl, ['index', '--root', work], env);
  if (r.status !== 0) throw new Error(`index failed: ${r.stderr}`);
  const g = JSON.parse(readFileSync(join(work, '.weave', 'graph.json'), 'utf8'));
  delete g.builtAt;
  delete g.perf;
  if (unordered) canonical(g);
  return JSON.stringify(g);
}

// Only for --unordered: a Go/Node difference in how the OS lists a directory,
// never a semantic one. Strict mode forbids any order change.
function canonical(g) {
  g.nodes.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  g.edges.sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)));
  if (g.coverage) {
    for (const c of [g.coverage.routes, g.coverage.calls, g.coverage.specs, g.coverage.next, g.coverage.trpc, g.coverage.openapi]) {
      if (c && Array.isArray(c.silent)) c.silent.sort();
    }
  }
  if (Array.isArray(g.prevEndpoints)) g.prevEndpoints.sort();
}

const CLI = [
  ['', []],
  ['status', []],
  ['context', ['packages/backend/src/modules/orders/orders.routes.ts']],
  ['impact', ['packages/backend/src/modules/orders/orders.routes.ts']],
  ['search', ['orders']],
  ['plan', ['2026-01-01-orders']],
  ['change', ['2026-01-01-orders']],
  ['doctor', []],
  ['check', ['packages/backend/src/modules/orders/orders.routes.ts']],
  ['evidence', []],
  ['perf', ['--runs', '1']],
  ['projects', []],
];

function cliSurface(impl, env) {
  rmSync(join(work, '.weave'), { recursive: true, force: true });
  return CLI.map(([cmd, args]) => {
    const r = run(impl, [...(cmd ? [cmd] : []), ...args, '--root', work], env);
    let out = mask(r.stdout);
    // perf pads columns from real timings, so widths move even when values are masked.
    if (cmd === 'perf') out = out.replace(/[ \t]+/g, ' ').trim();
    return {
      cmd: cmd || '(bare)',
      status: r.status,
      out,
      err: mask((r.stderr || '').split('\n')[0]),
    };
  });
}

const MCP = [
  { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'parity', version: '0' } } },
  { jsonrpc: '2.0', id: 2, method: 'tools/list' },
  { jsonrpc: '2.0', id: 3, method: 'tools/call', params: { name: 'context', arguments: { files: ['packages/backend/src/modules/orders/orders.routes.ts'] } } },
  { jsonrpc: '2.0', id: 4, method: 'tools/call', params: { name: 'nope', arguments: {} } },
  { jsonrpc: '2.0', id: 5, method: 'nope' },
];

function mcpSurface(impl, env) {
  const input = MCP.map((m) => JSON.stringify(m)).join('\n') + '\n';
  const r = spawnSync(impl[0], [...impl.slice(1), 'mcp', '--root', work], {
    input,
    encoding: 'utf8',
    timeout: 120000,
    env: { ...process.env, ...env },
  });
  return mask(r.stdout);
}

function htmlSurface(impl, env) {
  rmSync(join(work, '.weave'), { recursive: true, force: true });
  const out = join(work, 'parity-explore.html');
  const r = run(impl, ['explore', '--root', work, '--out', out, '--no-open'], env);
  if (r.status !== 0) throw new Error(`explore failed: ${r.stderr}`);
  return mask(readFileSync(out, 'utf8'));
}

function report(label, a, b) {
  if (a === b) {
    console.log(`  ok   ${label}`);
    return 0;
  }
  console.log(`  DIFF ${label}`);
  const la = a.split('\n');
  const lb = b.split('\n');
  for (let i = 0; i < Math.max(la.length, lb.length); i++) {
    if (la[i] === lb[i]) continue;
    const x = la[i] ?? '';
    const y = lb[i] ?? '';
    let at = 0;
    while (at < x.length && at < y.length && x[at] === y[at]) at++;
    console.log(`       first difference at character ${at}`);
    console.log(`       A: ${x.slice(Math.max(0, at - 60), at + 120)}`);
    console.log(`       B: ${y.slice(Math.max(0, at - 60), at + 120)}`);
    break;
  }
  return 1;
}

let failures = 0;
const envA = { WEAVE_HOME: home('a') };
const envB = { WEAVE_HOME: home('b') };
const t0 = Date.now();

try {
  if (only.includes('graph')) failures += report('graph', indexGraph(A, envA), indexGraph(B, envB));
  if (only.includes('cli')) {
    const a = cliSurface(A, envA);
    const b = cliSurface(B, envB);
    a.forEach((row, i) => {
      failures += report(`cli ${row.cmd}`, JSON.stringify(row), JSON.stringify(b[i]));
    });
  }
  if (only.includes('mcp')) failures += report('mcp', mcpSurface(A, envA), mcpSurface(B, envB));
  if (only.includes('html')) failures += report('html', htmlSurface(A, envA), htmlSurface(B, envB));
} finally {
  rmSync(work, { recursive: true, force: true });
  for (const e of [envA, envB]) rmSync(e.WEAVE_HOME, { recursive: true, force: true });
}

console.log(
  failures
    ? `\n${failures} surface(s) differ — parity is not proven`
    : `\nself-parity clean in ${Date.now() - t0} ms (${only.join(', ')})`,
);
process.exit(failures ? 1 : 0);
