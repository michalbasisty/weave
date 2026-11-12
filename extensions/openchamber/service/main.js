// weave for OpenChamber: the local service behind the "weave gate" Work Status
// section. OpenChamber starts it on 127.0.0.1 and draws what GET /status answers.
// It runs the weave binary already on this machine; it has no dependencies.
import { execFile } from 'node:child_process';
import { existsSync } from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

const port = Number(process.env.OPENCHAMBER_SERVICE_PORT);
const token = process.env.OPENCHAMBER_SERVICE_TOKEN ?? '';
if (!port || !token) {
  console.error('OPENCHAMBER_SERVICE_PORT and OPENCHAMBER_SERVICE_TOKEN are required');
  process.exit(1);
}

const MAX_FAILS = 10;
const MAX_NOTES = 2;
const RECHECK_MS = 60_000; // re-run even on an unchanged tree once a minute

// The service gets PATH and HOME only, and a desktop app's PATH often misses
// `go install`'s folder, so look in the usual places too.
const findWeave = () => {
  const exe = process.platform === 'win32' ? 'weave.exe' : 'weave';
  const home = os.homedir();
  const dirs = [
    ...(process.env.PATH ?? '').split(path.delimiter),
    path.join(home, 'go', 'bin'), path.join(home, '.local', 'bin'), path.join(home, 'bin'),
    '/opt/homebrew/bin', '/usr/local/bin', '/usr/bin',
  ];
  for (const d of dirs) {
    if (d && existsSync(path.join(d, exe))) return path.join(d, exe);
  }
  return null;
};

const run = (cmd, args, cwd, timeout) => new Promise((resolve) => {
  execFile(cmd, args, { cwd, timeout, maxBuffer: 4 * 1024 * 1024, env: { ...process.env, GIT_OPTIONAL_LOCKS: '0' } },
    (error, stdout, stderr) => resolve({ code: error ? (typeof error.code === 'number' ? error.code : -1) : 0, out: String(stdout) + String(stderr), missing: error?.code === 'ENOENT' }));
});

// What the tree looks like now: HEAD plus the uncommitted change. The gate only
// re-runs when it moves (or after RECHECK_MS), since a big repo takes a second.
const fingerprint = async (dir) => {
  const head = await run('git', ['rev-parse', 'HEAD'], dir, 5000);
  const status = await run('git', ['status', '--porcelain=v1'], dir, 5000);
  return head.out + '\n' + status.out;
};

// "  FAIL removed endpoint still called: GET /tags from web/api.js:218 (closest now: GET /tag-list)"
const parseFail = (text) => {
  const closest = /\(closest(?: now)?: ([^)]+)\)/.exec(text)?.[1] ?? '';
  const call = /\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|QUERY|MUTATION|ANY) (\S+)/.exec(text);
  const at = / from (\S+:\d+)/.exec(text)?.[1] ?? '';
  return { label: call ? `${call[1]} ${call[2]}` : 'broken call', closest, at, text };
};

export const view = (result) => {
  const controls = [{ kind: 'button', id: 'refresh', label: 'Re-run' }];
  if (result.error) return { rows: [{ kind: 'note', id: 'error', text: result.error, tone: 'error' }], controls };
  const lines = result.out.split('\n');
  const head = lines.find((l) => l.startsWith('check:')) ?? '';
  const fails = lines.filter((l) => l.startsWith('  FAIL ')).map((l) => parseFail(l.slice(7)));
  const warns = lines.filter((l) => l.startsWith('  warn ')).map((l) => l.slice(7));
  const tests = lines.filter((l) => l.startsWith('  run ')).map((l) => l.slice(6).trim());
  const ok = result.code === 0;
  const rows = [{
    kind: 'line', id: 'gate', label: 'gate', icon: ok ? 'checkbox-circle-line' : 'close-circle-line',
    dot: ok ? 'success' : 'error',
    content: { kind: 'text', text: head.replace(/^check:\s*/, '') || (ok ? 'nothing to check' : 'failed') },
    value: ok ? 'passes' : `${fails.length || 1} broken`, valueTone: ok ? 'success' : 'error',
    tooltip: head,
  }];
  fails.slice(0, MAX_FAILS).forEach((f, i) => rows.push({
    kind: 'line', id: `fail-${i}`, label: f.label, icon: 'link-unlink',
    content: { kind: 'text', text: f.closest ? `closest now: ${f.closest}` : 'no route serves it' },
    value: f.at, valueTone: 'muted', tooltip: f.text,
  }));
  if (fails.length > MAX_FAILS) rows.push({ kind: 'note', id: 'more', text: `${fails.length - MAX_FAILS} more: run weave check`, tone: 'error' });
  if (tests.length) rows.push({ kind: 'line', id: 'tests', label: 'tests', icon: 'flask-line', content: { kind: 'text', text: tests[0] }, tooltip: tests.join('\n') });
  warns.slice(0, MAX_NOTES).forEach((w, i) => rows.push({ kind: 'note', id: `warn-${i}`, text: w, tone: 'muted' }));
  return { rows: rows.slice(0, 16), controls };
};

const cache = new Map(); // dir -> { print, at, result }

const check = async (dir, force) => {
  const weave = findWeave();
  if (!weave) return { error: 'weave is not installed on this machine. go install github.com/michalbasisty/weave/cmd/weave@latest, or put the binary on PATH.' };
  const print = await fingerprint(dir);
  const hit = cache.get(dir);
  if (!force && hit && hit.print === print && Date.now() - hit.at < RECHECK_MS) return hit.result;
  const r = await run(weave, ['check', '--root', dir], dir, 120_000);
  const result = r.missing ? { error: `could not run ${weave}` } : { code: r.code, out: r.out };
  cache.set(dir, { print, at: Date.now(), result });
  return result;
};

const json = (res, status, body) => {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(body));
};

const server = http.createServer(async (req, res) => {
  if (req.headers.authorization !== `Bearer ${token}`) return json(res, 401, { message: 'unauthorized' });
  const url = new URL(req.url ?? '/', 'http://127.0.0.1');
  if (url.pathname === '/health') return json(res, 200, { ok: true });
  if (url.pathname !== '/status') return json(res, 404, { message: 'not found' });
  const dir = url.searchParams.get('directory') ?? '';
  if (!path.isAbsolute(dir) || !existsSync(dir)) {
    return json(res, 200, { rows: [{ kind: 'note', id: 'no-project', text: 'Open a project to run the gate on it.', tone: 'muted' }] });
  }
  if (req.method === 'POST') {
    let body = '';
    for await (const chunk of req) body += chunk;
    let id = '';
    try { id = JSON.parse(body || '{}').id ?? ''; } catch { /* not JSON */ }
    if (id !== 'refresh') return json(res, 400, { message: 'unknown control' });
    await check(dir, true);
    return json(res, 200, { ok: true });
  }
  if (req.method !== 'GET') return json(res, 405, { message: 'method not allowed' });
  json(res, 200, view(await check(dir, false)));
});

if (process.env.WEAVE_SERVICE_NO_LISTEN !== '1') server.listen(port, '127.0.0.1');
