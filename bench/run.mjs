#!/usr/bin/env node
// The numbers in the README and docs/COMPARISON.md, measured on real repositories.
//
//   node bench/run.mjs --weave ./weave --old ./weave-v0.1.0 --repos /path/to/repos
//
// --repos holds the checkouts named in bench/repos.json (bench/fetch.sh makes them).
// Writes bench/results.json; bench/charts.mjs draws the SVGs from it.
// Every number is something anyone can repeat: weave's own output, file sizes,
// and a plain substring search standing in for grep.

import { execFileSync, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const arg = (name, fallback) => {
  const i = process.argv.indexOf('--' + name);
  return i > 0 ? process.argv[i + 1] : fallback;
};
const WEAVE = path.resolve(arg('weave', 'weave'));
const OLD = arg('old', '') && path.resolve(arg('old'));
const REPOS = path.resolve(arg('repos', path.join(here, 'repos')));
const repos = JSON.parse(fs.readFileSync(path.join(here, 'repos.json'), 'utf8'));
const tokens = bytes => Math.round(bytes / 4); // the convention weave's own receipts use

const SKIP = new Set(['node_modules', '.git', 'dist', 'build', 'coverage', '.next', '.weave', '.turbo', 'out', 'vendor']);
const CODE = /\.(ts|tsx|js|jsx|mjs|cjs|vue|svelte|graphql|gql|prisma|json|yaml|yml)$/;
function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    if (SKIP.has(e.name) || e.name.startsWith('.')) continue;
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, out);
    else if (CODE.test(e.name) && fs.statSync(p).size < 512 * 1024) out.push(p);
  }
  return out;
}

function run(bin, args, cwd) {
  const t = process.hrtime.bigint();
  const r = spawnSync(bin, args, { cwd, encoding: 'utf8', maxBuffer: 64 << 20 });
  return { code: r.status, out: (r.stdout || '') + (r.stderr || ''), ms: Number(process.hrtime.bigint() - t) / 1e6 };
}
const median = xs => {
  const s = [...xs].sort((a, b) => a - b);
  return s.length ? (s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2) : 0;
};
const graphOf = root => JSON.parse(fs.readFileSync(path.join(root, '.weave', 'graph.json'), 'utf8'));
const brokenCalls = g => g.edges.filter(e => e.kind === 'calls_missing').length;

const results = { measuredAt: new Date().toISOString(), weave: run(WEAVE, ['--version']).out.trim(), repos: [] };

for (const r of repos) {
  const root = path.join(REPOS, r.dir);
  if (!fs.existsSync(root)) { console.error(`skip ${r.name}: ${root} missing (run bench/fetch.sh)`); continue; }
  console.error(`== ${r.name}`);
  fs.rmSync(path.join(root, '.weave'), { recursive: true, force: true });

  // speed: five fresh indexes, then the gate on a clean tree
  const idx = [];
  for (let i = 0; i < 5; i++) {
    fs.rmSync(path.join(root, '.weave', 'graph.json'), { force: true });
    idx.push(run(WEAVE, ['index', '--root', '.'], root).ms);
  }
  const chk = [];
  for (let i = 0; i < 3; i++) chk.push(run(WEAVE, ['check', '--root', '.'], root).ms);
  const g = graphOf(root);
  const nodes = new Map(g.nodes.map(n => [n.id, n]));
  const kinds = k => g.nodes.filter(n => n.kind === k);
  const out = (id, kind) => g.edges.filter(e => e.from === id && e.kind === kind).map(e => e.to);
  const inn = (id, kind) => g.edges.filter(e => e.to === id && e.kind === kind).map(e => e.from);
  const files = walk(root);
  const sources = files.map(f => fs.readFileSync(f, 'utf8'));
  const size = rel => { try { return fs.statSync(path.join(root, rel)).size } catch { return 0 } };

  // grep: does the endpoint's URL appear anywhere in the source, as written?
  const eps = kinds('endpoint');
  const grepHit = eps.filter(e => {
    const p = e.name.slice(e.name.indexOf(' ') + 1);
    return sources.some(s => s.includes(p));
  }).length;

  // tokens: what one screen costs to understand
  const perScreen = [];
  for (const p of kinds('page')) {
    if (!p.file) continue;
    const ctx = run(WEAVE, ['context', p.file, '--root', '.'], root);
    if (ctx.code !== 0) continue;
    const read = new Set([p.file]);
    for (const e of out(p.id, 'calls')) {
      const ep = nodes.get(e);
      if (ep?.file) read.add(ep.file);
      for (const t of inn(e, 'hits')) { const tn = nodes.get(t); if (tn?.file) read.add(tn.file); }
      for (const m of out(e, 'in_module')) for (const md of out(m, 'uses')) { const mn = nodes.get(md); if (mn?.file) read.add(mn.file); }
    }
    perScreen.push({
      screen: p.name,
      weave: tokens(Buffer.byteLength(ctx.out)),
      file: tokens(size(p.file)),
      byHand: tokens([...read].reduce((a, f) => a + size(f), 0)),
      files: read.size,
    });
  }

  // the version before the launch fixes, on the same tree
  let old = null;
  if (OLD) {
    fs.rmSync(path.join(root, '.weave'), { recursive: true, force: true });
    const o = run(OLD, ['index', '--root', '.'], root);
    if (o.code === 0) {
      const og = graphOf(root);
      old = {
        endpoints: og.nodes.filter(n => n.kind === 'endpoint').length,
        pages: og.nodes.filter(n => n.kind === 'page').length,
        broken: brokenCalls(og),
        calls: og.coverage?.calls,
      };
    }
    fs.rmSync(path.join(root, '.weave'), { recursive: true, force: true });
    run(WEAVE, ['index', '--root', '.'], root);
  }

  const linked = new Set(g.edges.filter(e => e.kind === 'calls' && e.from.startsWith('page:')).map(e => e.from));
  const mt = new Map(g.edges.filter(e => e.kind === 'maps_to').map(e => [e.from, e.to]));
  const usedTables = new Set(g.edges.filter(e => e.kind === 'uses').map(e => mt.get(e.to)));
  results.repos.push({
    name: r.name, url: r.url, commit: r.commit, stack: r.stack,
    files: g.fileCount ?? files.length,
    pages: kinds('page').length,
    endpoints: eps.length,
    modules: kinds('module').length,
    tables: kinds('table').length,
    tablesUsed: kinds('table').filter(t => usedTables.has(t.id)).length,
    pagesLinked: linked.size,
    calls: g.coverage?.calls,
    broken: brokenCalls(g),
    grep: { found: grepHit, of: eps.length },
    indexMs: Math.round(median(idx)),
    checkMs: Math.round(median(chk)),
    screens: {
      n: perScreen.length,
      weave: median(perScreen.map(s => s.weave)),
      file: median(perScreen.map(s => s.file)),
      byHand: median(perScreen.map(s => s.byHand)),
      files: median(perScreen.map(s => s.files)),
    },
    old,
  });
}

// the gate: rename each called route in place and see whether `weave check` names a caller
for (const r of repos.filter(x => x.mutate)) {
  const root = path.join(REPOS, r.dir);
  if (!fs.existsSync(root)) continue;
  console.error(`== mutations on ${r.name}`);
  // the gate compares with the commit, so the tree must start clean
  const dirty = execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim();
  if (dirty) { console.error(`skip mutations on ${r.name}: uncommitted changes`); continue; }
  run(WEAVE, ['index', '--root', '.'], root);
  const g = graphOf(root);
  const called = new Set(g.edges.filter(e => e.kind === 'calls' && e.from.startsWith('file:')).map(e => e.to));
  const targets = g.nodes.filter(n => n.kind === 'endpoint' && called.has(n.id) && n.file && n.line);
  let caught = 0, named = 0;
  const rows = [];
  for (const ep of targets) {
    const abs = path.join(root, ep.file);
    const before = fs.readFileSync(abs, 'utf8');
    const lines = before.split('\n');
    // the route string sits on the endpoint's line or, wrapped, just below it
    let i = ep.line - 1, m = null;
    for (; i < ep.line + 3 && i < lines.length; i++) {
      if ((m = /(['"`])(\/[^'"`]*)\1/.exec(lines[i]))) break;
    }
    if (!m) { rows.push({ endpoint: ep.name, skipped: true }); continue; }
    // rename the first literal segment: /articles/:slug -> /articles-renamed/:slug
    const renamed = m[2].replace(/^\/([A-Za-z][\w-]*)/, '/$1-renamed');
    if (renamed === m[2]) { rows.push({ endpoint: ep.name, skipped: true }); continue; }
    lines[i] = lines[i].replace(m[0], m[1] + renamed + m[1]);
    fs.writeFileSync(abs, lines.join('\n'));
    const c = run(WEAVE, ['check', '--root', '.'], root);
    fs.writeFileSync(abs, before);
    const hit = c.code === 1;
    const names = /removed endpoint still called: .* from [^\s;]+:\d+/.test(c.out);
    caught += hit ? 1 : 0;
    named += names ? 1 : 0;
    rows.push({ endpoint: ep.name, exit: c.code, named: names });
  }
  run(WEAVE, ['index', '--root', '.'], root);
  const tried = rows.filter(x => !x.skipped).length;
  results.repos.find(x => x.name === r.name).mutations = { tried, caught, named, rows };
}

// what every agent session pays before its first prompt: the tool schema
{
  const init = { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'bench', version: '1' } } };
  const list = { jsonrpc: '2.0', id: 2, method: 'tools/list' };
  const r = spawnSync(WEAVE, ['mcp', '--root', '.'], { cwd: here, input: JSON.stringify(init) + '\n' + JSON.stringify(list) + '\n', encoding: 'utf8', timeout: 10000 });
  const line = (r.stdout || '').trim().split('\n').map(l => { try { return JSON.parse(l) } catch { return null } }).find(j => j && j.id === 2);
  if (line) {
    const json = JSON.stringify(line.result.tools);
    results.schema = { tools: line.result.tools.length, tokens: tokens(Buffer.byteLength(json)) };
  }
}

fs.writeFileSync(path.join(here, 'results.json'), JSON.stringify(results, null, 2) + '\n');
console.error('wrote bench/results.json');
