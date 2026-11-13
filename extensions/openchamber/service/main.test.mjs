// node --test extensions/openchamber/service/
// The service's own logic, and getting weave from a fake GitHub release.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import crypto from 'node:crypto';
import { mkdtempSync, writeFileSync } from 'node:fs';
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
