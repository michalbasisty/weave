#!/usr/bin/env node
// Sums bench/eval/run.sh transcripts per task and arm, and applies the decision
// rule docs/EVAL.md set before the first run.
//   node bench/eval/report.mjs <out-dir> [--json]
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const dir = process.argv[2];
if (!dir) { console.error('usage: report.mjs <out-dir> [--json]'); process.exit(2); }
const tasks = new Map(fs.readFileSync(path.join(here, 'tasks.jsonl'), 'utf8').trim().split('\n').map(l => JSON.parse(l)).map(t => [t.id, t]));
const median = xs => { const s = [...xs].sort((a, b) => a - b); return s.length ? (s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2) : 0; };

const runs = [];
for (const f of fs.readdirSync(dir).filter(f => f.endsWith('.meta'))) {
  const base = f.slice(0, -5);
  const meta = JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8'));
  const lines = fs.readFileSync(path.join(dir, base + '.jsonl'), 'utf8').split('\n').filter(Boolean).map(l => { try { return JSON.parse(l) } catch { return null } }).filter(Boolean);
  const tools = {}, read = new Set();
  for (const l of lines) {
    if (l.type !== 'assistant') continue;
    for (const c of l.message?.content || []) {
      if (c.type !== 'tool_use') continue;
      tools[c.name] = (tools[c.name] || 0) + 1;
      const p = c.input?.file_path || c.input?.path;
      if (p) read.add(String(p).replace(/^.*?\/repo\//, ''));
    }
  }
  const res = lines.find(l => l.type === 'result') || {};
  const u = res.usage || {};
  const touched = fs.readFileSync(path.join(dir, base + '.files'), 'utf8').split('\n').filter(Boolean);
  const want = tasks.get(meta.id)?.expect_files || [];
  runs.push({
    ...meta,
    ok: !res.is_error && res.subtype === 'success',
    turns: res.num_turns ?? 0,
    toolCalls: Object.values(tools).reduce((a, b) => a + b, 0),
    weaveCalls: Object.entries(tools).filter(([k]) => k.startsWith('mcp__weave')).reduce((a, [, v]) => a + v, 0),
    tokensIn: (u.input_tokens || 0) + (u.cache_read_input_tokens || 0) + (u.cache_creation_input_tokens || 0),
    tokensOut: u.output_tokens || 0,
    cost: res.total_cost_usd || 0,
    found: want.filter(w => read.has(w) || touched.includes(w)).length + '/' + want.length,
    touched: touched.length,
    answer: (res.result || '').slice(0, 300),
  });
}
runs.sort((a, b) => (a.id + a.arm + a.run).localeCompare(b.id + b.arm + b.run));
if (process.argv.includes('--json')) { console.log(JSON.stringify(runs, null, 2)); process.exit(0); }

const fmt = n => Math.round(n).toLocaleString('en-US');
console.log('task              arm       turns  tools  weave  tokens in  out     cost   secs  check  expected');
for (const r of runs) {
  console.log([r.id.padEnd(17), r.arm.padEnd(8), String(r.turns).padStart(5), String(r.toolCalls).padStart(6), String(r.weaveCalls).padStart(6),
    fmt(r.tokensIn).padStart(10), fmt(r.tokensOut).padStart(6), ('$' + r.cost.toFixed(2)).padStart(7), String(r.secs).padStart(6), String(r.check).padStart(6), r.found.padStart(9)].join(' '));
}
const arm = a => runs.filter(r => r.arm === a);
const b = arm('baseline'), w = arm('weave');
if (!b.length || !w.length) process.exit(0);
const m = (rs, k) => median(rs.map(r => r[k]));
const drop = k => 1 - m(w, k) / (m(b, k) || 1);
console.log('\nmedian per run      baseline      weave    change');
for (const [k, label] of [['tokensIn', 'tokens in'], ['tokensOut', 'tokens out'], ['toolCalls', 'tool calls'], ['secs', 'seconds'], ['cost', 'cost $']]) {
  console.log(label.padEnd(16) + String(k === 'cost' ? m(b, k).toFixed(2) : fmt(m(b, k))).padStart(12) + String(k === 'cost' ? m(w, k).toFixed(2) : fmt(m(w, k))).padStart(11) + ((-drop(k) * 100).toFixed(0) + '%').padStart(10));
}
const success = rs => rs.filter(r => r.ok && r.check === 0 && r.found.split('/')[0] === r.found.split('/')[1]).length;
console.log(`success (finished, check passes, expected files found): baseline ${success(b)}/${b.length}, weave ${success(w)}/${w.length}`);
const d = drop('tokensIn');
const verdict = success(w) < success(b) ? 'rethink it: success dropped'
  : d >= 0.3 ? 'ship the MCP half: tokens dropped by 30% or more and success held'
  : d < 0.1 ? 'rethink it: tokens dropped by less than 10%'
  : 'partial win: report per task kind';
console.log('decision rule (docs/EVAL.md): ' + verdict + (runs.length < 24 ? `  [only ${runs.length} runs; the protocol asks for 72]` : ''));
