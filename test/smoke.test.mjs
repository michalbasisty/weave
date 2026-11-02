import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, cpSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

// Never touch the real project registry from a test run.
process.env.WEAVE_HOME = mkdtempSync(join(tmpdir(), 'weave-home-'));

const here = dirname(fileURLToPath(import.meta.url));
const cli = join(here, '..', 'src', 'cli.ts');
const fixture = join(here, 'fixture');

/** Start the MCP server on stdio and send JSON-RPC requests. */
function rpc(calls, extra = []) {
  return new Promise((resolve, reject) => {
    const proc = spawn(process.execPath, [cli, 'mcp', '--root', fixture, ...extra], { stdio: ['pipe', 'pipe', 'inherit'] });
    const out = new Map();
    let buf = '';
    proc.stdout.setEncoding('utf8');
    proc.stdout.on('data', (chunk) => {
      buf += chunk;
      let nl;
      while ((nl = buf.indexOf('\n')) >= 0) {
        const line = buf.slice(0, nl).trim();
        buf = buf.slice(nl + 1);
        if (!line) continue;
        const msg = JSON.parse(line);
        if (msg.id != null) out.set(msg.id, msg);
        if (out.size === calls.length) {
          proc.stdin.end();
          resolve(calls.map((c) => out.get(c.id)));
        }
      }
    });
    proc.on('error', reject);
    for (const c of calls) proc.stdin.write(JSON.stringify(c) + '\n');
  });
}

const text = (msg) => msg.result.content[0].text;

test('mcp handshake, tools and impact', async () => {
  const [init, list, impact] = await rpc([
    { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {} } },
    { jsonrpc: '2.0', id: 2, method: 'tools/list' },
    {
      jsonrpc: '2.0',
      id: 3,
      method: 'tools/call',
      params: { name: 'impact', arguments: { files: ['packages/backend/src/modules/orders/orders.routes.ts'] } },
    },
  ]);

  assert.equal(init.result.serverInfo.name, 'weave');
  assert.deepEqual(
    list.result.tools.map((t) => t.name),
    ['context', 'impact', 'search', 'plan', 'ask', 'changes', 'status'],
  );

  const t = text(impact);
  assert.match(t, /GET \/api\/v1\/orders/);
  assert.match(t, /POST \/api\/v1\/orders/);
  assert.match(t, /\/orders \(exact\)/);
  assert.match(t, /orders/);
});

test('context pack fits its token budget and names the tests', async () => {
  const [res] = await rpc([
    { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'context', arguments: { files: ['packages/backend/src/modules/orders/orders.routes.ts'], budget: 120 } } },
  ]);
  const t = text(res);
  assert.match(t, /endpoint {2}GET \/api\/v1\/orders/);
  assert.match(t, /orders\.spec\.ts|none reference/);
  assert.ok(t.length <= 120 * 4, `context exceeded budget: ${t.length} chars`);
});

test('init writes a working MCP config without touching existing files', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'weave-'));
  const run = spawnSync(process.execPath, [cli, 'init', '--root', dir], { encoding: 'utf8' });
  assert.equal(run.status, 0, run.stderr);
  const cfg = JSON.parse(readFileSync(join(dir, '.mcp.json'), 'utf8'));
  assert.equal(cfg.mcpServers.weave.command, 'node');
  assert.match(cfg.mcpServers.weave.args[0], /cli\.ts$/);
  assert.deepEqual(cfg.mcpServers.weave.args.slice(1), ['mcp', '--root', '.']);
  assert.match(readFileSync(join(dir, '.gitignore'), 'utf8'), /\.weave\//);

  const again = spawnSync(process.execPath, [cli, 'init', '--root', dir], { encoding: 'utf8' });
  assert.match(again.stdout, /\.mcp\.json \(exists\)/);
});

test('change record resolves tasks to files and blast radius', async () => {
  const [chg] = await rpc([
    { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'changes', arguments: { slug: '2026-01-01-orders' } } },
  ]);
  const t = text(chg);
  assert.match(t, /tasks 1\/2/);
  assert.match(t, /packages\/backend\/src\/modules\/orders\/orders\.routes\.ts/);
  assert.match(t, /GET \/api\/v1\/orders/);
});

test('search ranks exact names first', async () => {
  const [res] = await rpc([
    { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'search', arguments: { query: 'orders' } } },
  ]);
  const first = text(res).split('\n')[0];
  assert.match(first, /orders/);
});

test('@req marker links code to the requirement and names the test command', async () => {
  const [res] = await rpc([
    { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'context', arguments: { files: ['packages/backend/src/modules/orders/orders.routes.ts'], budget: 900 } } },
  ]);
  const t = text(res);
  assert.match(t, /REQ-ORD-1/);
  assert.match(t, /npx vitest run/);
});

test('a changed requirement marks linked code suspect', () => {
  const spec = join(fixture, 'specs/changes/2026-01-01-orders/spec-delta.md');
  const original = readFileSync(spec, 'utf8');
  const index = () => spawnSync(process.execPath, [cli, 'index', '--root', fixture], { encoding: 'utf8' });
  try {
    writeFileSync(spec, original);
    index();
    writeFileSync(spec, original.replace('newest first', 'oldest first, and never cached'));
    const run = index();
    assert.equal(run.status, 0, run.stderr);
    const g = JSON.parse(readFileSync(join(fixture, '.weave', 'graph.json'), 'utf8'));
    const req = g.nodes.find((n) => n.id === 'req:REQ-ORD-1');
    assert.equal(req.meta.suspect, true);
  } finally {
    writeFileSync(spec, original);
    index();
  }
});

test('check passes on the fixture and prints the test command', () => {
  const run = spawnSync(
    process.execPath,
    [cli, 'check', '--root', fixture, 'packages/backend/src/modules/orders/orders.routes.ts'],
    { encoding: 'utf8' },
  );
  assert.equal(run.status, 0, run.stdout + run.stderr);
  assert.match(run.stdout, /npx vitest run/);
});

test('check fails when a route is renamed out from under a caller', () => {
  const routes = join(fixture, 'packages/backend/src/modules/orders/orders.routes.ts');
  const original = readFileSync(routes, 'utf8');
  const index = () => spawnSync(process.execPath, [cli, 'index', '--root', fixture], { encoding: 'utf8' });
  try {
    writeFileSync(routes, original);
    index();
    writeFileSync(routes, original.replace("router.get('/',", "router.get('/list',"));
    const run = spawnSync(process.execPath, [cli, 'check', '--root', fixture], { encoding: 'utf8' });
    assert.equal(run.status, 1, run.stdout + run.stderr);
    assert.match(run.stdout, /removed endpoint still called/);
    assert.match(run.stdout, /GET \/api\/v1\/orders/);
  } finally {
    writeFileSync(routes, original);
    index();
  }
});

test('check is quiet again after the route comes back', () => {
  const run = spawnSync(process.execPath, [cli, 'check', '--root', fixture], { encoding: 'utf8' });
  assert.equal(run.status, 0, run.stdout);
});

test('mcp errors are protocol-shaped and the server stays alive', async () => {
  const [unknownTool, unknownMethod, resources, pong] = await rpc([
    { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'nope' } },
    { jsonrpc: '2.0', id: 2, method: 'nothing/like/this' },
    { jsonrpc: '2.0', id: 3, method: 'resources/list' },
    { jsonrpc: '2.0', id: 4, method: 'ping' },
  ]);
  assert.equal(unknownTool.error.code, -32602);
  assert.match(unknownTool.error.message, /available: context, impact/);
  assert.equal(unknownMethod.error.code, -32601);
  assert.deepEqual(resources.result.resources, []);
  assert.deepEqual(pong.result, {});
});

test('mcp --all takes a project argument', async () => {
  const [list, status] = await rpc(
    [
      { jsonrpc: '2.0', id: 1, method: 'tools/list' },
      { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'status', arguments: { project: 'fixture' } } },
    ],
    ['--all'],
  );
  const tools = list.result.tools;
  assert.ok(tools.every((t) => t.inputSchema.properties.project), 'every tool should accept project');
  assert.match(text(status), /^files \d+/);
});

test('explore --all bundles every indexed project', () => {
  const home = process.env.WEAVE_HOME;
  const twin = join(home, 'twin');
  cpSync(fixture, twin, { recursive: true });
  writeFileSync(
    join(home, 'projects.json'),
    JSON.stringify({
      projects: [
        { name: 'fixture', root: fixture, lastUsed: '2026-01-01T00:00:00.000Z' },
        { name: 'twin', root: twin, lastUsed: '2026-01-02T00:00:00.000Z' },
      ],
    }),
  );
  const out = join(tmpdir(), `weave-all-${process.pid}.html`);
  const run = spawnSync(process.execPath, [cli, 'explore', '--all', '--root', fixture, '--out', out], { encoding: 'utf8' });
  assert.equal(run.status, 0, run.stderr);
  const html = readFileSync(out, 'utf8');
  rmSync(out);
  assert.match(run.stdout, /2 project\(s\)/);
  assert.match(html, /"name":"fixture"/);
  assert.match(html, /"name":"twin"/);
  const js = html.slice(html.lastIndexOf('<script>') + 8, html.lastIndexOf('</script>'));
  new Function(js);
  rmSync(twin, { recursive: true, force: true });
});

test('doctor states recall and names a route file it could not parse', () => {
  const dyn = join(fixture, 'packages/backend/src/modules/orders/dynamic.routes.ts');
  const clean = spawnSync(process.execPath, [cli, 'doctor', '--root', fixture], { encoding: 'utf8' });
  assert.match(clean.stdout, /route files {3}1\/1 produced endpoints \(100%\)/);
  assert.match(clean.stdout, /task files {4}1\/1 parsed \(100%\)/);

  writeFileSync(
    dyn,
    "import { Router } from 'express';\nconst router = Router();\nconst ROUTE = '/x';\nrouter.get(ROUTE, (_req, res) => res.end());\nexport default router;\n",
  );
  try {
    const run = spawnSync(process.execPath, [cli, 'doctor', '--root', fixture], { encoding: 'utf8' });
    assert.match(run.stdout, /route files {3}1\/2 produced endpoints \(50%\)/);
    assert.match(run.stdout, /GAPS/);
    assert.match(run.stdout, /dynamic\.routes\.ts/);
  } finally {
    rmSync(dyn);
  }
});

test('next route handlers and trpc procedures become endpoints', () => {
  const s = spawnSync(process.execPath, [cli, 'search', 'quotes', '--limit', '20', '--root', fixture], { encoding: 'utf8' });
  assert.match(s.stdout, /GET \/api\/quotes/);
  assert.match(s.stdout, /POST \/api\/quotes/);
  const t2 = spawnSync(process.execPath, [cli, 'search', 'trpc', '--limit', '20', '--root', fixture], { encoding: 'utf8' });
  assert.match(t2.stdout, /QUERY \/api\/trpc\/quotes\.list/);
  assert.match(t2.stdout, /MUTATION \/api\/trpc\/quotes\.add/);
  assert.match(t2.stdout, /QUERY \/api\/trpc\/ping/);

  const d = spawnSync(process.execPath, [cli, 'doctor', '--root', fixture], { encoding: 'utf8' });
  assert.match(d.stdout, /next routes\s+1\/1/);
  assert.match(d.stdout, /tRPC routers\s+1\/1/);

  // A tRPC client call reaches the procedure it calls, through the page that renders it.
  const c = spawnSync(process.execPath, [cli, 'context', 'packages/web/src/pages/Orders.tsx', '--root', fixture], {
    encoding: 'utf8',
  });
  assert.match(c.stdout, /QUERY \/api\/trpc\/quotes\.list/);
});

test('mcp --manager exposes only the three verbs', async () => {
  const [list] = await rpc([{ jsonrpc: '2.0', id: 1, method: 'tools/list' }], ['--manager']);
  assert.deepEqual(
    list.result.tools.map((t) => t.name),
    ['plan', 'ask', 'status'],
  );
});

test('ask talks to any OpenAI-compatible endpoint', async () => {
  const { createServer } = await import('node:http');
  const seen = [];
  const server = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      seen.push({ url: req.url, auth: req.headers.authorization, body: JSON.parse(body) });
      res.setHeader('content-type', 'application/json');
      res.end(JSON.stringify({ choices: [{ message: { content: 'The warehouse change touches 6 endpoints and 4 tables.' } }] }));
    });
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const port = server.address().port;

  const out = await new Promise((resolve) => {
    const p = spawn(process.execPath, [cli, 'ask', 'what does the warehouse change touch?', '--root', fixture], {
      env: { ...process.env, WEAVE_LLM_BASE_URL: `http://127.0.0.1:${port}`, WEAVE_LLM_API_KEY: 'test-key', WEAVE_LLM_MODEL: 'cheap' },
    });
    let s = '';
    p.stdout.on('data', (d) => (s += d));
    p.on('close', () => resolve(s));
  });
  server.close();

  assert.match(out, /The warehouse change touches 6 endpoints and 4 tables/);
  assert.match(out, /facts used:/);
  assert.equal(seen.length, 1);
  assert.equal(seen[0].url, '/chat/completions');
  assert.equal(seen[0].auth, 'Bearer test-key');
  assert.equal(seen[0].body.model, 'cheap');
  assert.match(seen[0].body.messages[0].content, /ONLY the facts/);
  assert.match(seen[0].body.messages[1].content, /FACTS/);
});

test('openapi specs and custom patterns become endpoints', () => {
  const s = spawnSync(process.execPath, [cli, 'search', 'reports', '--limit', '20', '--root', fixture], { encoding: 'utf8' });
  assert.match(s.stdout, /GET \/api\/reports\/:id/);
  assert.match(s.stdout, /DELETE \/api\/reports\/:id/);

  // A wrapper route is invisible until a pattern names it.
  const before = spawnSync(process.execPath, [cli, 'search', 'legacy', '--limit', '10', '--root', fixture], { encoding: 'utf8' });
  assert.doesNotMatch(before.stdout, /GET \/api\/legacy/);

  mkdirSync(join(fixture, '.weave'), { recursive: true });
  const pat = join(fixture, '.weave', 'patterns.json');
  writeFileSync(pat, JSON.stringify({ routes: [{ match: "defineRoute\\('([^']+)'\\)", method: 'GET' }] }));
  try {
    const after = spawnSync(process.execPath, [cli, 'search', 'legacy', '--limit', '10', '--root', fixture], { encoding: 'utf8' });
    assert.match(after.stdout, /GET \/api\/legacy/);
    const d = spawnSync(process.execPath, [cli, 'doctor', '--root', fixture], { encoding: 'utf8' });
    assert.match(d.stdout, /patterns\s+1 endpoint/);
    assert.match(d.stdout, /openapi\s+1\/1 specs/);
  } finally {
    rmSync(pat);
  }
});

test('impact accepts several files at once', async () => {
  const [res] = await rpc([
    {
      jsonrpc: '2.0',
      id: 1,
      method: 'tools/call',
      params: {
        name: 'impact',
        arguments: { files: ['packages/backend/src/modules/orders/orders.routes.ts', 'packages/web/src/pages/Orders.tsx'] },
      },
    },
  ]);
  const t = text(res);
  assert.match(t, /impact: 2 file\(s\)/);
  assert.match(t, /modules\s+orders/);
});

const JUNIT = (body) => `<testsuites><testsuite name="orders">${body}</testsuite></testsuites>`;
const passing = JUNIT(
  '<testcase classname="orders" name="orders page loads" file="packages/web/e2e/orders.spec.ts" time="0.1"/>',
);
const failing = JUNIT(
  '<testcase classname="orders" name="orders page loads" file="packages/web/e2e/orders.spec.ts" time="0.1"><failure message="expected 3 rows"/></testcase>',
);

test('explore renders a self-contained page that parses', () => {
  const out = join(tmpdir(), `weave-explore-${process.pid}.html`);
  const run = spawnSync(process.execPath, [cli, 'explore', '--root', fixture, '--out', out], { encoding: 'utf8' });
  assert.equal(run.status, 0, run.stderr);
  const html = readFileSync(out, 'utf8');
  rmSync(out);
  assert.match(html, /^<!doctype html>/);
  assert.match(html, /page:\/orders/);
  assert.doesNotMatch(html, /undefined/);
  const js = html.slice(html.lastIndexOf('<script>') + 8, html.lastIndexOf('</script>'));
  new Function(js); // syntax only: throws on a broken template
  assert.ok(html.length > 8000, `page too small: ${html.length}`);
});

test('plan returns the next unfinished task with its command', () => {
  const spec = join(fixture, 'specs/changes/2026-01-01-orders/tasks.md');
  const original = readFileSync(spec, 'utf8');
  writeFileSync(spec, original.replace('- [x] T1', '- [ ] T1'));
  try {
    const run = spawnSync(process.execPath, [cli, 'plan', '2026-01-01-orders', '--root', fixture], { encoding: 'utf8' });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /next\s+T1/);
    assert.match(run.stdout, /files\s+packages\/backend\/src\/modules\/orders\/orders\.routes\.ts/);
    assert.match(run.stdout, /run\s+/);
  } finally {
    writeFileSync(spec, original);
  }
});

test('evidence joins a JUnit report to requirements', () => {
  mkdirSync(join(fixture, '.weave'), { recursive: true });
  const jp = join(fixture, '.weave', 'junit.xml');
  writeFileSync(jp, passing);
  try {
    const run = spawnSync(process.execPath, [cli, 'evidence', '--root', fixture], { encoding: 'utf8' });
    assert.match(run.stdout, /REQ-ORD-1\s+pass/);
    assert.match(run.stdout, /1\/1 requirement\(s\) verified/);
  } finally {
    rmSync(jp);
  }
});

test('check fails when the requirement test fails', () => {
  mkdirSync(join(fixture, '.weave'), { recursive: true });
  const jp = join(fixture, '.weave', 'junit.xml');
  writeFileSync(jp, failing);
  try {
    const run = spawnSync(
      process.execPath,
      [cli, 'check', '--root', fixture, 'packages/backend/src/modules/orders/orders.routes.ts'],
      { encoding: 'utf8' },
    );
    assert.equal(run.status, 1, run.stdout);
    assert.match(run.stdout, /requirement test failing: REQ-ORD-1/);
  } finally {
    rmSync(jp);
  }
});
