// node --test extensions/openchamber/service/
// The service's own logic, and getting weave from a fake GitHub release.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import crypto from 'node:crypto';
import { cpSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

process.env.WEAVE_SERVICE_NO_LISTEN = '1';
process.env.OPENCHAMBER_SERVICE_TOKEN = 'test-token';
process.env.OPENCHAMBER_SERVICE_DATA_DIR = mkdtempSync(path.join(os.tmpdir(), 'weave-svc-'));
const here = path.dirname(fileURLToPath(import.meta.url));
const main = path.join(here, 'main.js');
const m = await import(main);

test('versions and release assets', () => {
  assert.equal(m.newer('v0.9.0', '0.8.0'), true);
  assert.equal(m.newer('0.8.0', '0.8.0'), false);
  assert.equal(m.newer('0.10.0', '0.9.9'), true);
  assert.equal(m.newer('0.8.0', null), true);
  assert.equal(m.assetName('win32', 'x64'), 'weave-windows-amd64.exe');
  assert.equal(m.assetName('darwin', 'arm64'), 'weave-darwin-arm64');
  assert.equal(m.assetName('linux', 'ia32'), null);
});

test('gate output', () => {
  const out = [
    'check: 1 file(s) → 1 endpoint(s), 1 endpoint(s) removed',
    '  FAIL removed endpoint still called: GET /tags from web/api.js:218 (closest now: GET /tag-list)',
    '  run  cd backend && npx jest tag.test.ts',
  ].join('\n');
  const c = m.parseCheck({ code: 1, out });
  assert.equal(c.ok, false);
  assert.deepEqual(c.fails.map((f) => [f.label, f.closest, f.at]), [['GET /tags', 'GET /tag-list', 'web/api.js:218']]);
  assert.deepEqual(c.tests, ['cd backend && npx jest tag.test.ts']);
  const v = m.view({ code: 1, out });
  assert.equal(v.rows[0].value, '1 broken');
  assert.equal(v.rows[1].label, 'GET /tags');
  assert.match(m.fixPrompt(c), /GET \/tags[\s\S]*Then run:/);
  assert.equal(m.view({ pending: 'wait' }).rows[0].text, 'wait');
});

test('git status', () => {
  assert.deepEqual(m.parseStatus(' M a.ts\n?? b.ts\nR  old.ts -> new.ts\n D gone.ts\n'), [
    { file: 'a.ts', state: 'changed' }, { file: 'b.ts', state: 'new' }, { file: 'new.ts', state: 'renamed' }, { file: 'gone.ts', state: 'deleted' },
  ]);
});

test('map overview and a node with its neighbours', () => {
  const nodes = [
    { id: 'page:/', kind: 'page', name: '/', file: 'web/Home.js' },
    { id: 'endpoint:GET /tags', kind: 'endpoint', name: 'GET /tags', file: 'api/tag.ts' },
    { id: 'module:tag', kind: 'module', name: 'tag', file: 'api/tag' },
    { id: 'model:Tag', kind: 'model', name: 'Tag', file: 'schema.prisma' },
    { id: 'table:Tag', kind: 'table', name: 'Tag', file: 'schema.prisma' },
  ];
  const edges = [
    { from: 'page:/', to: 'endpoint:GET /tags', kind: 'calls' },
    { from: 'endpoint:GET /tags', to: 'module:tag', kind: 'in_module' },
    { from: 'module:tag', to: 'model:Tag', kind: 'uses' },
    { from: 'model:Tag', to: 'table:Tag', kind: 'maps_to' },
  ];
  const g = { nodes, edges, byId: new Map(nodes.map((n) => [n.id, n])) };
  const o = m.overview(g);
  assert.deepEqual([o.screens, o.endpoints, o.modules, o.tables], [1, 1, 1, 1]);
  assert.equal(o.lists.page[0].meta, '1 call');
  assert.equal(o.lists.endpoint[0].meta, '1 caller');
  assert.equal(o.lists.table[0].meta, 'Tag');
  const ep = m.node(g, 'endpoint:GET /tags');
  assert.deepEqual(ep.groups.map((x) => [x.label, x.items.map((i) => i.name)]), [['Called by', ['/']], ['Module', ['tag']]]);
  const table = m.node(g, 'table:Tag');
  assert.deepEqual(table.groups.find((x) => x.label === 'Endpoints').items.map((i) => i.name), ['GET /tags']);
  assert.equal(m.node(g, 'page:/nope'), null);
});

test('the agent note names the binary', () => {
  const t = m.instructionsText('/data/bin/weave');
  assert.match(t, /"\/data\/bin\/weave"/);
  assert.match(t, /weave impact <file>/);
  assert.ok(t.length < 4096);
});

// A release on a fake GitHub: checksums.txt plus a "weave" that prints its version.
const fakeGitHub = async (version, { badSum = false } = {}) => {
  const asset = m.assetName();
  const bin = Buffer.from(`#!/bin/sh\necho "weave ${version}"\n`);
  const sum = crypto.createHash('sha256').update(badSum ? Buffer.from('x') : bin).digest('hex');
  const server = http.createServer((req, res) => {
    if (req.url.endsWith('/releases/latest')) { res.writeHead(302, { location: `/michalbasisty/weave/releases/tag/v${version}` }); return res.end(); }
    if (req.url.endsWith('/checksums.txt')) return res.end(`${sum}  ${asset}\n`);
    if (req.url.endsWith(`/${asset}`)) return res.end(bin);
    res.writeHead(404); res.end();
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  return { server, url: `http://127.0.0.1:${server.address().port}` };
};

// Each download runs in a fresh process: the service keeps one weave per process.
const resolveIn = (env) => new Promise((resolve) => {
  const code = `const m = await import(${JSON.stringify(main)}); await m.resolveWeave(); console.log(JSON.stringify(m.weaveState));`;
  execFile(process.execPath, ['--input-type=module', '-e', code], {
    env: { PATH: '/nonexistent', HOME: mkdtempSync(path.join(os.tmpdir(), 'weave-home-')), WEAVE_SERVICE_NO_LISTEN: '1', ...env },
  }, (err, stdout, stderr) => resolve(err ? { error: stderr } : JSON.parse(stdout.trim().split('\n').pop())));
});

test('downloads weave when it is not installed', { skip: process.platform === 'win32' }, async () => {
  const gh = await fakeGitHub('0.9.0');
  try {
    const data = mkdtempSync(path.join(os.tmpdir(), 'weave-data-'));
    const s = await resolveIn({ WEAVE_GITHUB_URL: gh.url, OPENCHAMBER_SERVICE_DATA_DIR: data });
    assert.equal(s.status, 'ready');
    assert.equal(s.source, 'download');
    assert.equal(s.version, '0.9.0');
    assert.equal(s.bin, path.join(data, 'bin', 'weave'));
    // A second start uses the copy it has.
    gh.server.close();
    const again = await resolveIn({ WEAVE_GITHUB_URL: 'http://127.0.0.1:9', OPENCHAMBER_SERVICE_DATA_DIR: data });
    assert.equal(again.status, 'ready');
    assert.equal(again.version, '0.9.0');
  } finally {
    gh.server.close();
  }
});

test('refuses a binary that does not match its checksum', { skip: process.platform === 'win32' }, async () => {
  const gh = await fakeGitHub('0.9.0', { badSum: true });
  try {
    const s = await resolveIn({ WEAVE_GITHUB_URL: gh.url, OPENCHAMBER_SERVICE_DATA_DIR: mkdtempSync(path.join(os.tmpdir(), 'weave-data-')) });
    assert.equal(s.status, 'error');
    assert.match(s.error, /checksum/);
  } finally {
    gh.server.close();
  }
});

test('prefers a new enough weave on PATH', { skip: process.platform === 'win32' }, async () => {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'weave-path-'));
  writeFileSync(path.join(dir, 'weave'), '#!/bin/sh\necho "weave 0.8.1"\n', { mode: 0o755 });
  const s = await resolveIn({ PATH: `${dir}:/usr/bin:/bin`, WEAVE_GITHUB_URL: 'http://127.0.0.1:9', OPENCHAMBER_SERVICE_DATA_DIR: mkdtempSync(path.join(os.tmpdir(), 'weave-data-')) });
  assert.equal(s.source, 'path');
  assert.equal(s.version, '0.8.1');
});

test('HTTP: bearer, health, bad events', async () => {
  await new Promise((r) => m.server.listen(0, '127.0.0.1', r));
  const base = `http://127.0.0.1:${m.server.address().port}`;
  try {
    assert.equal((await fetch(`${base}/health`)).status, 401);
    const auth = { Authorization: 'Bearer test-token' };
    assert.equal((await fetch(`${base}/health`, { headers: auth })).status, 200);
    const ev = await fetch(`${base}/session-events`, { method: 'POST', headers: auth, body: '{"v":2}' });
    assert.equal(ev.status, 400);
    const ms = await fetch(`${base}/mentions/search`, { method: 'POST', headers: auth, body: JSON.stringify({ query: 'x', directory: null }) });
    assert.deepEqual(await ms.json(), { items: [] });
    const mr = await fetch(`${base}/mentions/resolve`, { method: 'POST', headers: auth, body: JSON.stringify({ id: 'nope' }) });
    assert.equal(mr.status, 404);
  } finally {
    m.server.close();
  }
});

test('specs: change records from the map, paths, Work Status rows, the task prompt', () => {
  const g = {
    nodes: [
      { id: 'change:2026-01-01-orders', kind: 'change', name: '2026-01-01-orders', file: 'specs/changes/2026-01-01-orders/tasks.md' },
      { id: 'task:2026-01-01-orders/T1', kind: 'task', name: '2026-01-01-orders T1', meta: { done: true, title: 'API list. Evidence: `a.ts`.' } },
      { id: 'task:2026-01-01-orders/T2', kind: 'task', name: '2026-01-01-orders T2', meta: { done: false, title: 'Web page.' } },
      { id: 'change:2025-12-01-done', kind: 'change', name: '2025-12-01-done', file: 'specs/changes/2025-12-01-done/tasks.md' },
    ],
    edges: [
      { from: 'change:2026-01-01-orders', to: 'task:2026-01-01-orders/T1', kind: 'has_task' },
      { from: 'change:2026-01-01-orders', to: 'task:2026-01-01-orders/T2', kind: 'has_task' },
    ],
  };
  const cs = m.changesOf(g);
  assert.deepEqual(cs.map((c) => [c.slug, c.done, c.total]), [['2026-01-01-orders', 1, 2], ['2025-12-01-done', 0, 0]]);
  assert.equal(cs[0].tasks[0].title, 'API list.');
  const rows = m.specStatusRows(cs);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].label, 'orders');
  assert.deepEqual(rows[0].content, { kind: 'bar', value: 50 });
  assert.match(rows[0].tooltip, /next T2 Web page/);

  const dir = mkdtempSync(path.join(os.tmpdir(), 'weave-specpath-'));
  assert.ok(m.specPath(dir, 'specs/changes/x/tasks.md'));
  for (const bad of ['../specs/a.md', 'specs/../../etc/passwd.md', 'README.md', 'specs/a.txt', 'specs\\a.md', '/specs/a.md']) {
    assert.equal(m.specPath(dir, bad), null, bad);
  }
  assert.ok(m.NAME_OK.test('billing-retry') && !m.NAME_OK.test('../x') && !m.NAME_OK.test('-rf'));
  assert.match(m.taskPrompt('2026-01-01-orders', 'next  T2  Web page.'), /"2026-01-01-orders"[\s\S]*next {2}T2[\s\S]*tick/);
  assert.match(m.instructionsText('/w'), /weave plan <change>/);
});

// The whole specs flow over HTTP against a real weave. CI builds one and sets
// WEAVE_BIN; without it, this test is skipped.
test('HTTP: specs list, read, tick, create, save, plan, @ menu', { skip: !process.env.WEAVE_BIN || process.platform === 'win32' }, async () => {
  process.env.PATH = `${path.dirname(path.resolve(process.env.WEAVE_BIN))}${path.delimiter}${process.env.PATH}`;
  const repo = mkdtempSync(path.join(os.tmpdir(), 'weave-specs-'));
  cpSync(path.join(here, '..', '..', '..', 'test', 'fixture'), repo, { recursive: true, filter: (p) => !p.includes(`${path.sep}.weave`) });
  const git = (...a) => new Promise((r) => execFile('git', ['-C', repo, '-c', 'user.name=t', '-c', 'user.email=t@t', ...a], r));
  await git('init', '-q'); await git('add', '-A'); await git('commit', '-qm', 'fixture');

  await new Promise((r) => m.server.listen(0, '127.0.0.1', r));
  const base = `http://127.0.0.1:${m.server.address().port}`;
  const auth = { Authorization: 'Bearer test-token' };
  const q = `directory=${encodeURIComponent(repo)}`;
  const get = async (p) => (await fetch(`${base}${p}${p.includes('?') ? '&' : '?'}${q}`, { headers: auth })).json();
  const post = async (p, body) => { const r = await fetch(`${base}${p}?${q}`, { method: 'POST', headers: auth, body: JSON.stringify(body) }); return [r.status, await r.json()]; };
  try {
    let specs = await get('/specs');
    assert.equal(specs.full, true);
    const orders = specs.changes.find((c) => c.slug === '2026-01-01-orders');
    assert.deepEqual([orders.done, orders.total], [1, 2]);
    assert.ok(specs.views.some((v) => v.name === '/orders'));

    const page = await get(`/spec?path=${encodeURIComponent(orders.file)}`);
    assert.match(page.html, /<li>\[ \] T2 Web page/);
    assert.equal((await post('/spec/save', { path: '../../etc/passwd.md', text: 'x' }))[0], 400);

    const plan = await get('/plan?change=2026-01-01-orders');
    assert.match(plan.text, /next {2}T2/);

    assert.equal((await post('/spec/task', { change: '2026-01-01-orders', task: 'T2', done: true }))[0], 200);
    specs = await get('/specs');
    assert.equal(specs.changes.find((c) => c.slug === '2026-01-01-orders').done, 2);
    assert.match(readFileSync(path.join(repo, orders.file), 'utf8'), /- \[x\] T2/);

    const [st, made] = await post('/spec/new', { name: 'billing-retry', title: 'Retry failed charges' });
    assert.equal(st, 200);
    const created = (await get('/specs')).changes.find((c) => c.slug === made.slug);
    assert.ok(created, made.slug);
    const proposal = `specs/changes/${made.slug}/proposal.md`;
    assert.equal((await post('/spec/save', { path: proposal, text: '# Retry failed charges\n\nWhy: lost revenue.\n' }))[0], 200);
    assert.match(readFileSync(path.join(repo, proposal), 'utf8'), /lost revenue/);
    assert.equal((await post('/spec/save', { path: proposal, text: '   ' }))[0], 400);
    assert.equal((await post('/spec/new', { name: '../evil' }))[0], 400);

    const menu = await fetch(`${base}/mentions/search`, { method: 'POST', headers: auth, body: JSON.stringify({ query: 'spec', directory: repo }) });
    const items = (await menu.json()).items;
    const row = items.find((i) => i.title === made.slug);
    assert.ok(row, JSON.stringify(items));
    const resolved = await (await fetch(`${base}/mentions/resolve`, { method: 'POST', headers: auth, body: JSON.stringify({ id: row.id }) })).json();
    assert.match(resolved.text, /Work on the next task/);
  } finally {
    m.server.close();
  }
});
