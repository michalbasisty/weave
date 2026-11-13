// weave for OpenChamber: the local service behind everything the extension shows.
//
// - It brings its own weave: a weave on PATH (0.8.0 or newer) is used as is,
//   otherwise the matching release binary is downloaded once into the service's
//   data folder, checked against the release's checksums.txt, and kept up to date.
// - GET /status draws the "weave gate" section in Work Status.
// - The panel asks /overview, /node, /search, /context, /impact, /changes, /about,
//   and for specs /specs, /spec, /plan, POST /spec/save, /spec/new, /spec/task.
// - The composer's @ menu searches the map (/mentions/search, /mentions/resolve).
// - After an agent turn that changed the tree, it runs the gate and, when the
//   change broke a call, tells that session once per distinct failure.
// - It tells every agent session that weave is there and when to use it.
//
// Plain Node, no packages. OpenChamber starts it on 127.0.0.1.
import { execFile } from 'node:child_process';
import crypto from 'node:crypto';
import { chmodSync, existsSync, mkdirSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';

const port = Number(process.env.OPENCHAMBER_SERVICE_PORT);
const token = process.env.OPENCHAMBER_SERVICE_TOKEN ?? '';
const callbackUrl = process.env.OPENCHAMBER_HOST_CALLBACK_URL ?? '';
const listen = process.env.WEAVE_SERVICE_NO_LISTEN !== '1';
if (listen && (!port || !token)) {
  console.error('OPENCHAMBER_SERVICE_PORT and OPENCHAMBER_SERVICE_TOKEN are required');
  process.exit(1);
}

const REPO = 'michalbasisty/weave';
const GITHUB = process.env.WEAVE_GITHUB_URL || 'https://github.com'; // tests point it at a fake
const MIN_VERSION = '0.8.0'; // plain-word search and the closest-route hint
const MAX_FAILS = 10;
const MAX_NOTES = 2;
const RECHECK_MS = 60_000; // re-run the gate on an unchanged tree once a minute
const UPDATE_EVERY_MS = 24 * 3600_000;

const win = process.platform === 'win32';
const exe = win ? 'weave.exe' : 'weave';
const dataDir = process.env.OPENCHAMBER_SERVICE_DATA_DIR || path.join(os.homedir(), '.weave-openchamber');
const binDir = path.join(dataDir, 'bin');
const managedBin = path.join(binDir, exe);

// ---------------------------------------------------------------- helpers

const run = (cmd, args, cwd, timeout) => new Promise((resolve) => {
  execFile(cmd, args, { cwd, timeout, maxBuffer: 8 * 1024 * 1024, windowsHide: true, env: { ...process.env, GIT_OPTIONAL_LOCKS: '0' } },
    (error, stdout, stderr) => resolve({
      code: error ? (typeof error.code === 'number' ? error.code : -1) : 0,
      out: String(stdout) + String(stderr),
      missing: error?.code === 'ENOENT',
    }));
});

const readJSON = (file, fallback) => { try { return JSON.parse(readFileSync(file, 'utf8')); } catch { return fallback; } };
const writeJSON = (file, value) => {
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, JSON.stringify(value, null, 2));
};

export const parseVersion = (text) => /(\d+)\.(\d+)\.(\d+)/.exec(text ?? '')?.slice(1, 4).map(Number) ?? null;
export const newer = (a, b) => { // a > b
  const x = parseVersion(a); const y = parseVersion(b);
  if (!x) return false;
  if (!y) return true;
  for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] > y[i];
  return false;
};

// ---------------------------------------------------------------- settings

const settingsFile = path.join(dataDir, 'settings.json');
const DEFAULT_SETTINGS = { autoGate: true, agentInstructions: true };
let settings = { ...DEFAULT_SETTINGS, ...readJSON(settingsFile, {}) };
const saveSettings = (next) => {
  settings = { ...settings, ...next };
  writeJSON(settingsFile, settings);
};

// ---------------------------------------------------------------- the weave binary

// A desktop app's PATH often lacks `go install`'s folder, so look in the usual places too.
const pathCandidates = () => {
  const home = os.homedir();
  const dirs = [
    ...(process.env.PATH ?? '').split(path.delimiter),
    path.join(home, 'go', 'bin'), path.join(home, '.local', 'bin'), path.join(home, 'bin'),
    '/opt/homebrew/bin', '/usr/local/bin', '/usr/bin',
  ];
  const seen = new Set();
  return dirs.filter(Boolean).map((d) => path.join(d, exe)).filter((p) => !seen.has(p) && seen.add(p) && existsSync(p));
};

export const assetName = (platform = process.platform, arch = process.arch) => {
  const p = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[platform];
  const a = { x64: 'amd64', arm64: 'arm64' }[arch];
  if (!p || !a) return null;
  return `weave-${p}-${a}${p === 'windows' ? '.exe' : ''}`;
};

const versionOf = async (bin) => {
  const r = await run(bin, ['--version'], os.tmpdir(), 10_000);
  return r.code === 0 ? (parseVersion(r.out)?.join('.') ?? null) : null;
};

// What the panel's About section and the status rows show.
export const weaveState = { bin: null, version: null, source: null, status: 'starting', error: null, checkedAt: 0 };

const latestTag = async () => {
  const r = await fetch(`${GITHUB}/${REPO}/releases/latest`, { redirect: 'manual', signal: AbortSignal.timeout(15_000) });
  const where = r.headers.get('location') ?? '';
  return /\/tag\/(v[^/]+)$/.exec(where)?.[1] ?? null;
};

// Download one release binary, check it against checksums.txt, put it in place.
const download = async (tag) => {
  const asset = assetName();
  if (!asset) throw new Error(`no weave release for ${process.platform}/${process.arch}; install it with go install github.com/michalbasisty/weave/cmd/weave@latest`);
  const base = tag ? `${GITHUB}/${REPO}/releases/download/${tag}` : `${GITHUB}/${REPO}/releases/latest/download`;
  const get = async (name) => {
    const r = await fetch(`${base}/${name}`, { signal: AbortSignal.timeout(120_000) });
    if (!r.ok) throw new Error(`downloading ${name} answered HTTP ${r.status}`);
    return Buffer.from(await r.arrayBuffer());
  };
  const sums = (await get('checksums.txt')).toString('utf8');
  const want = sums.split('\n').map((l) => l.trim().split(/\s+/)).find(([, n]) => n === asset)?.[0];
  if (!want) throw new Error(`checksums.txt has no ${asset}`);
  const bin = await get(asset);
  const got = crypto.createHash('sha256').update(bin).digest('hex');
  if (got !== want) throw new Error(`${asset} does not match its checksum; not using it`);
  mkdirSync(binDir, { recursive: true });
  const tmp = path.join(binDir, `.${exe}.${process.pid}.tmp`);
  writeFileSync(tmp, bin);
  if (!win) chmodSync(tmp, 0o755);
  try {
    renameSync(tmp, managedBin);
  } catch (e) {
    rmSync(tmp, { force: true });
    throw e;
  }
};

let resolving = null;
// The weave this service runs: PATH first when it is new enough, then its own copy,
// downloading that the first time.
export const resolveWeave = () => {
  if (weaveState.bin && weaveState.status === 'ready') return Promise.resolve(weaveState.bin);
  resolving ??= (async () => {
    try {
      for (const bin of pathCandidates()) {
        const v = await versionOf(bin);
        if (v && !newer(MIN_VERSION, v)) return use(bin, v, 'path');
      }
      if (existsSync(managedBin)) {
        const v = await versionOf(managedBin);
        if (v && !newer(MIN_VERSION, v)) {
          void maybeUpdate();
          return use(managedBin, v, 'download');
        }
      }
      weaveState.status = 'downloading';
      await download(null);
      const v = await versionOf(managedBin);
      if (!v) throw new Error('the downloaded weave does not run on this machine');
      writeJSON(path.join(dataDir, 'update.json'), { checkedAt: Date.now() });
      return use(managedBin, v, 'download');
    } catch (e) {
      weaveState.status = 'error';
      weaveState.error = `Could not download weave (${e instanceof Error ? e.message : String(e)}). Check the connection to github.com, or install weave yourself: go install github.com/michalbasisty/weave/cmd/weave@latest`;
      return null;
    } finally {
      resolving = null;
    }
  })();
  return resolving;
};

const use = (bin, version, source) => {
  Object.assign(weaveState, { bin, version, source, status: 'ready', error: null });
  void publishInstructions();
  return bin;
};

// Our own copy follows the latest release, checked at most once a day.
const maybeUpdate = async (force = false) => {
  if (weaveState.source !== 'download') return { updated: false, reason: 'weave comes from PATH' };
  const marker = path.join(dataDir, 'update.json');
  const last = readJSON(marker, {}).checkedAt ?? 0;
  if (!force && Date.now() - last < UPDATE_EVERY_MS) return { updated: false };
  try {
    writeJSON(marker, { checkedAt: Date.now() });
    const tag = await latestTag();
    const current = weaveState.version ?? (await versionOf(managedBin));
    if (!tag || !newer(tag, current)) return { updated: false, latest: tag };
    await download(tag);
    const v = await versionOf(managedBin);
    if (v) Object.assign(weaveState, { bin: managedBin, version: v, source: 'download' });
    indexed.clear();
    return { updated: true, latest: tag };
  } catch (e) {
    return { updated: false, error: e instanceof Error ? e.message : String(e) };
  }
};

// ---------------------------------------------------------------- calling OpenChamber back

const hostCall = async (method, input) => {
  if (!callbackUrl || !token) return null;
  try {
    const r = await fetch(`${callbackUrl.replace(/\/+$/, '')}/service/call`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ method, input }),
      signal: AbortSignal.timeout(10_000),
    });
    const body = await r.json().catch(() => null);
    if (!r.ok) console.error(`openchamber ${method}: ${body?.error?.message ?? r.status}`);
    return r.ok ? body?.result ?? null : null;
  } catch (e) {
    console.error(`openchamber ${method}: ${e instanceof Error ? e.message : e}`);
    return null;
  }
};

// What every agent session reads: weave is here, and when it is worth a call.
// The agent's shell may not share this PATH, so it gets the full path once.
export const instructionsText = (bin) => [
  'weave (an OpenChamber extension) maps web apps: screens → endpoints → modules → tables.',
  `Its binary is "${bin}"; below, \`weave\` stands for that full path. Run it from the project root.`,
  'Use it for questions that span the app. Skip it for a one-file edit.',
  '- Who calls this, what a change reaches, which tests to run: weave impact <file>',
  '- The code behind a screen, URL or behaviour when grep finds nothing: weave search "<plain words>"',
  '- What a file is in the app: weave context <file>',
  '- Before you finish a change to an endpoint or a front-end call: weave check. Exit 1 lists each broken call and the closest route.',
  '- When the project keeps spec change records (specs/changes/): weave plan <change> gives the next task with its files, endpoints and tests; weave spec task done <change> <T1> ticks it.',
  'If weave finds no screens or endpoints, the project is not a web app it maps; leave it.',
].join('\n');

let lastInstructions = null;
const publishInstructions = async () => {
  const text = settings.agentInstructions && weaveState.bin
    ? instructionsText(weaveState.bin)
    : '';
  if (text === lastInstructions) return;
  const r = await hostCall('instructions.set', { directory: null, text });
  if (r) lastInstructions = text;
};

// ---------------------------------------------------------------- the gate

// What the tree looks like now: HEAD plus the uncommitted change.
const fingerprint = async (dir) => {
  const head = await run('git', ['rev-parse', 'HEAD'], dir, 5000);
  const status = await run('git', ['status', '--porcelain=v1'], dir, 5000);
  return head.out + '\n' + status.out;
};

// "  FAIL removed endpoint still called: GET /tags from web/api.js:218 (closest now: GET /tag-list)"
export const parseFail = (text) => {
  const closest = /\(closest(?: now)?: ([^)]+)\)/.exec(text)?.[1] ?? '';
  const call = /\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|QUERY|MUTATION|ANY) (\S+)/.exec(text);
  const at = / from (\S+:\d+)/.exec(text)?.[1] ?? '';
  return { label: call ? `${call[1]} ${call[2]}` : 'broken call', closest, at, text };
};

export const parseCheck = (result) => {
  const lines = (result.out ?? '').split('\n');
  return {
    ok: result.code === 0,
    head: (lines.find((l) => l.startsWith('check:')) ?? '').replace(/^check:\s*/, ''),
    fails: lines.filter((l) => l.startsWith('  FAIL ')).map((l) => parseFail(l.slice(7))),
    warns: lines.filter((l) => l.startsWith('  warn ')).map((l) => l.slice(7)),
    tests: lines.filter((l) => l.startsWith('  run ')).map((l) => l.slice(6).trim()),
  };
};

export const view = (result) => {
  const controls = [{ kind: 'button', id: 'refresh', label: 'Re-run' }];
  if (result.pending) return { rows: [{ kind: 'note', id: 'pending', text: result.pending, tone: 'muted' }], controls };
  if (result.error) return { rows: [{ kind: 'note', id: 'error', text: result.error, tone: 'error' }], controls };
  const c = parseCheck(result);
  const rows = [{
    kind: 'line', id: 'gate', label: 'gate', icon: c.ok ? 'checkbox-circle-line' : 'close-circle-line',
    dot: c.ok ? 'success' : 'error',
    content: { kind: 'text', text: c.head || (c.ok ? 'nothing to check' : 'failed') },
    value: c.ok ? 'passes' : `${c.fails.length || 1} broken`, valueTone: c.ok ? 'success' : 'error',
    tooltip: c.head,
  }];
  c.fails.slice(0, MAX_FAILS).forEach((f, i) => rows.push({
    kind: 'line', id: `fail-${i}`, label: f.label, icon: 'link-unlink',
    content: { kind: 'text', text: f.closest ? `closest now: ${f.closest}` : 'no route serves it' },
    value: f.at, valueTone: 'muted', tooltip: f.text,
  }));
  if (c.fails.length > MAX_FAILS) rows.push({ kind: 'note', id: 'more', text: `${c.fails.length - MAX_FAILS} more in the weave panel`, tone: 'error' });
  if (c.tests.length) rows.push({ kind: 'line', id: 'tests', label: 'tests', icon: 'flask-line', content: { kind: 'text', text: c.tests[0] }, tooltip: c.tests.join('\n') });
  c.warns.slice(0, MAX_NOTES).forEach((w, i) => rows.push({ kind: 'note', id: `warn-${i}`, text: w, tone: 'muted' }));
  return { rows: rows.slice(0, 16), controls };
};

const gateCache = new Map(); // dir -> { print, at, result }

// Work Status shows the open change records under the gate: how far each is.
export const specStatusRows = (changes) => changes.filter((c) => c.total && c.done < c.total).slice(0, 3).map((c, i) => {
  const next = c.tasks.find((t) => !t.done);
  return {
    kind: 'line', id: `spec-${i}`, label: c.slug.replace(/^\d{4}-\d{2}-\d{2}-/, ''), icon: 'task-line',
    content: { kind: 'bar', value: Math.round((100 * c.done) / c.total) },
    value: `${c.done}/${c.total}`, valueTone: 'muted',
    tooltip: next ? `${c.slug}: next ${next.id} ${next.title}` : c.slug,
  };
});
const specRows = (dir) => {
  try { return existsSync(graphPath(dir)) ? specStatusRows(changesOf(graphs.get(dir)?.g ?? readGraph(dir))) : []; } catch { return []; }
};

const check = async (dir, force) => {
  const weave = await resolveWeave();
  if (!weave) return { error: weaveState.error ?? 'weave is not available.' };
  const print = await fingerprint(dir);
  const hit = gateCache.get(dir);
  if (!force && hit && hit.print === print && Date.now() - hit.at < RECHECK_MS) return hit.result;
  const r = await run(weave, ['check', '--root', dir], dir, 120_000);
  const result = r.missing ? { error: `could not run ${weave}` } : { code: r.code, out: r.out };
  gateCache.set(dir, { print, at: Date.now(), result });
  return result;
};

// ---------------------------------------------------------------- the map

const indexed = new Map(); // dir -> print
const indexing = new Map(); // dir -> Promise
const graphPath = (dir) => path.join(dir, '.weave', 'graph.json');

// The map is rebuilt only when the tree moves.
const ensureIndex = async (weave, dir) => {
  const print = await fingerprint(dir);
  if (indexed.get(dir) === print && existsSync(graphPath(dir))) return;
  if (!indexing.has(dir)) {
    indexing.set(dir, run(weave, ['index', '--root', dir], dir, 180_000)
      .then(() => { indexed.set(dir, print); graphs.delete(dir); })
      .finally(() => indexing.delete(dir)));
  }
  await indexing.get(dir);
};

const graphs = new Map(); // dir -> { mtime, g }
const readGraph = (dir) => {
  const g = JSON.parse(readFileSync(graphPath(dir), 'utf8'));
  g.byId = new Map(g.nodes.map((n) => [n.id, n]));
  return g;
};

export const overview = (g) => {
  const out = new Map();
  const inn = new Map();
  for (const e of g.edges) {
    if (e.kind !== 'calls') continue;
    out.set(e.from, (out.get(e.from) ?? 0) + 1);
    inn.set(e.to, (inn.get(e.to) ?? 0) + 1);
  }
  const byModule = new Map();
  for (const e of g.edges) if (e.kind === 'in_module') byModule.set(e.to, (byModule.get(e.to) ?? 0) + 1);
  const usedBy = new Map();
  for (const e of g.edges) if (e.kind === 'maps_to') usedBy.set(e.to, e.from);
  const list = (kind, meta) => g.nodes.filter((n) => n.kind === kind)
    .map((n) => ({ id: n.id, name: n.name, file: n.file ?? '', meta: meta(n) }))
    .sort((a, b) => a.name.localeCompare(b.name));
  const plural = (n, one, many) => (n ? `${n} ${n === 1 ? one : many}` : '');
  const lists = {
    page: list('page', (n) => plural(out.get(n.id) ?? 0, 'call', 'calls')),
    endpoint: list('endpoint', (n) => plural(inn.get(n.id) ?? 0, 'caller', 'callers') || 'no caller'),
    module: list('module', (n) => plural(byModule.get(n.id) ?? 0, 'endpoint', 'endpoints')),
    table: list('table', (n) => (usedBy.has(n.id) ? g.byId?.get(usedBy.get(n.id))?.name ?? '' : '')),
  };
  return {
    screens: lists.page.length, endpoints: lists.endpoint.length, modules: lists.module.length, tables: lists.table.length,
    lists,
    pages: lists.page.map((p) => ({ name: p.name, file: p.file, endpoints: out.get(p.id) ?? 0 })),
    builtAt: g.builtAt ?? null,
  };
};

const REL = {
  calls: ['Calls', 'Called by'],
  renders: ['Renders', 'Rendered by'],
  in_module: ['Module', 'Endpoints'],
  uses: ['Uses', 'Used by'],
  maps_to: ['Table', 'Model'],
  relates: ['Related', 'Related'],
  covers: ['Covers', 'Tests'],
  contains: ['Files', 'In module'],
  defined_in: ['Defined in', 'Defines'],
  declared_in: ['Declared in', 'Declares'],
};
const SHOWN = new Set(['page', 'endpoint', 'module', 'table', 'model', 'test']);

// One node and its neighbours, grouped the way a person reads them.
export const node = (g, id) => {
  const n = g.byId.get(id);
  if (!n) return null;
  const groups = new Map();
  const add = (label, other) => {
    if (!other || !SHOWN.has(other.kind)) return;
    if (!groups.has(label)) groups.set(label, []);
    const list = groups.get(label);
    if (!list.some((x) => x.id === other.id)) list.push({ id: other.id, kind: other.kind, name: other.name, file: other.file ?? '' });
  };
  for (const e of g.edges) {
    const names = REL[e.kind];
    if (!names) continue;
    if (e.from === id) add(names[0], g.byId.get(e.to));
    if (e.to === id) add(names[1], g.byId.get(e.from));
  }
  // A table's endpoints: through its model's module.
  if (n.kind === 'table') {
    for (const e of g.edges) if (e.kind === 'maps_to' && e.to === id) {
      for (const u of g.edges) if (u.kind === 'uses' && u.to === e.from) {
        for (const m of g.edges) if (m.kind === 'in_module' && m.to === u.from) add('Endpoints', g.byId.get(m.from));
      }
    }
  }
  return {
    id: n.id, kind: n.kind, name: n.name, file: n.file ?? '', line: n.line ?? null,
    desc: n.meta?.desc ?? '',
    groups: [...groups].map(([label, items]) => ({ label, items: items.sort((a, b) => a.name.localeCompare(b.name)).slice(0, 60), total: items.length })),
  };
};

const getGraph = async (weave, dir) => {
  await ensureIndex(weave, dir);
  const print = indexed.get(dir);
  const hit = graphs.get(dir);
  if (hit && hit.print === print) return hit.g;
  const g = readGraph(dir);
  graphs.set(dir, { print, g });
  return g;
};

// "endpoint  GET /tags  backend/src/app/routes/tag/tag.controller.ts"
export const parseSearch = (out) => out.split('\n').filter((l) => l.trim() && !l.startsWith('nothing matched')).map((l) => {
  const kind = l.slice(0, l.indexOf(' '));
  const [name = '', file = ''] = l.slice(kind.length).trim().split(/\s{2,}/);
  return { kind, name, file };
});

const safeFile = (dir, file) => !!file && !file.startsWith('-') && !path.isAbsolute(file)
  && !file.split(/[\\/]/).includes('..') && existsSync(path.join(dir, file));

const search = async (weave, dir, q) => {
  q = q.slice(0, 200).replace(/^-+/, ''); // never a flag
  if (!q.trim()) return [];
  const r = await run(weave, ['search', q, '--root', dir], dir, 30_000);
  return parseSearch(r.out);
};

// Uncommitted files: "XY path" or "XY old -> new".
export const parseStatus = (out) => out.split('\n').filter((l) => l.length > 3).map((l) => {
  const code = l.slice(0, 2);
  const file = l.slice(3).split(' -> ').pop().replace(/^"|"$/g, '');
  const state = code.includes('?') ? 'new' : code.includes('D') ? 'deleted' : code.includes('A') ? 'added' : code.includes('R') ? 'renamed' : 'changed';
  return { file, state };
});

const changes = async (weave, dir) => {
  const st = await run('git', ['status', '--porcelain=v1', '--untracked-files=all'], dir, 10_000);
  if (st.code !== 0) return { error: 'This project is not a git repository, so there is no change to look at.' };
  const files = parseStatus(st.out).filter((f) => !f.file.startsWith('.weave/'));
  const gate = await check(dir, false);
  const live = files.filter((f) => f.state !== 'deleted' && safeFile(dir, f.file)).map((f) => f.file).slice(0, 40);
  const impact = live.length ? (await run(weave, ['impact', ...live, '--root', dir], dir, 60_000)).out.trim() : '';
  return { files, gate: gate.error ? { error: gate.error } : { ...parseCheck(gate), out: gate.out?.trim() ?? '' }, impact };
};

// ---------------------------------------------------------------- specs

// Change records and their tasks, read from the map: no extra weave run, so the
// @ menu and Work Status can use them inside their time limits.
export const changesOf = (g) => {
  const byId = g.byId ?? new Map(g.nodes.map((n) => [n.id, n]));
  const out = [];
  for (const c of g.nodes.filter((n) => n.kind === 'change')) {
    const tasks = g.edges.filter((e) => e.kind === 'has_task' && e.from === c.id).map((e) => byId.get(e.to)).filter(Boolean)
      .map((t) => ({
        id: t.id.slice(t.id.lastIndexOf('/') + 1),
        title: String(t.meta?.title ?? '').replace(/\s+Evidence:.*$/, '').trim(),
        done: t.meta?.done === true,
      }));
    out.push({ slug: c.name, file: c.file ?? '', done: tasks.filter((t) => t.done).length, total: tasks.length, tasks });
  }
  return out.sort((a, b) => b.slug.localeCompare(a.slug));
};

export const NAME_OK = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
// A spec path from the panel: Markdown under specs/, no way out of it.
export const specPath = (dir, p) => {
  const rel = String(p ?? '').replace(/^\.\//, '');
  if (!rel.startsWith('specs/') || !rel.endsWith('.md') || rel.includes('..') || rel.includes('\\') || rel.includes('\0')) return null;
  const abs = path.resolve(dir, rel);
  return abs.startsWith(path.resolve(dir) + path.sep) ? { rel, abs } : null;
};

const runInput = (cmd, args, cwd, input, timeout) => new Promise((resolve) => {
  const child = execFile(cmd, args, { cwd, timeout, maxBuffer: 8 * 1024 * 1024, windowsHide: true },
    (error, stdout, stderr) => resolve({ code: error ? (typeof error.code === 'number' ? error.code : -1) : 0, out: String(stdout) + String(stderr) }));
  child.stdin.end(input);
});

// Everything the Specs tab lists. A weave older than `spec list --json` still
// gives the change records, from the map.
const specsList = async (weave, dir) => {
  await ensureIndex(weave, dir);
  const r = await run(weave, ['spec', 'list', '--json', '--root', dir], dir, 60_000);
  if (r.code === 0) {
    try { return { ...JSON.parse(r.out), full: true }; } catch { /* an older weave */ }
  }
  return { changes: changesOf(await getGraph(weave, dir)), capabilities: [], views: [], full: false };
};

const specRead = async (weave, dir, p) => {
  const sp = specPath(dir, p);
  if (!sp || !existsSync(sp.abs)) return [400, { message: 'no such spec' }];
  if (statSync(sp.abs).size > 512 * 1024) return [400, { message: 'larger than 512 KB' }];
  const r = await run(weave, ['spec', 'show', sp.rel, '--json', '--root', dir], dir, 20_000);
  if (r.code === 0) {
    try { return [200, JSON.parse(r.out)]; } catch { /* an older weave */ }
  }
  const text = readFileSync(sp.abs, 'utf8');
  return [200, { path: sp.rel, title: /^#\s+(.+)$/m.exec(text)?.[1] ?? sp.rel, text, html: '' }];
};

const specSave = async (weave, dir, body) => {
  const sp = specPath(dir, body?.path);
  if (!sp || !existsSync(sp.abs)) return [400, { message: 'no such spec' }];
  const text = typeof body.text === 'string' ? body.text : '';
  if (!text.trim()) return [400, { message: 'A spec cannot be empty.' }];
  if (text.length > 512 * 1024) return [400, { message: 'A spec is at most 512 KB.' }];
  const r = await runInput(weave, ['spec', 'save', sp.rel, '--root', dir], dir, text, 20_000);
  if (r.code !== 0) {
    if (!/usage: weave spec/.test(r.out)) return [400, { message: r.out.trim() || 'weave could not save it' }];
    // An older weave: write it the same careful way, through a temp file.
    const tmp = `${sp.abs}.${process.pid}.tmp`;
    writeFileSync(tmp, text);
    renameSync(tmp, sp.abs);
  }
  return [200, { ok: true, path: sp.rel }];
};

const specNew = async (weave, dir, body) => {
  const name = String(body?.name ?? '').trim();
  if (!NAME_OK.test(name)) return [400, { message: 'A name is letters, digits, dash or underscore, like billing-retry.' }];
  const args = ['spec', 'new', name, '--root', dir];
  const title = String(body?.title ?? '').trim().slice(0, 200);
  if (title) args.push('--title', title);
  const r = await run(weave, args, dir, 20_000);
  if (r.code !== 0) return [400, { message: r.out.trim() || 'weave could not create it' }];
  const slug = /specs\/changes\/(\S+)/.exec(r.out)?.[1] ?? name;
  return [200, { ok: true, slug, out: r.out.trim() }];
};

const specTask = async (weave, dir, body) => {
  const change = String(body?.change ?? ''); const task = String(body?.task ?? '');
  if (!NAME_OK.test(change) || !NAME_OK.test(task)) return [400, { message: 'change and task are required' }];
  const r = await run(weave, ['spec', 'task', body.done === false ? 'reopen' : 'done', change, task, '--root', dir], dir, 20_000);
  if (r.code !== 0) return [400, { message: r.out.trim() || 'weave could not change it' }];
  indexed.delete(dir); // tasks.md changed: read the map again
  return [200, { ok: true, out: r.out.trim() }];
};

// The next task of a change, as weave plans it: files, endpoints, tables, tests.
const plan = async (weave, dir, change, task) => {
  if (!NAME_OK.test(change) || (task && !NAME_OK.test(task))) return [400, { message: 'bad change' }];
  await ensureIndex(weave, dir);
  const r = await run(weave, ['plan', change, ...(task ? [task] : []), '--root', dir], dir, 30_000);
  return [200, { text: r.out.trim(), code: r.code }];
};

// What an agent needs to do one task: the plan, and how to tick it.
export const taskPrompt = (change, planText) => [
  `Work on the next task of the spec change "${change}". weave planned it:`,
  '',
  '```',
  planText,
  '```',
  '',
  'Read the change record in specs/changes/ first. When the task is done and its tests pass, tick it with the "tick" command above, then run weave check.',
].join('\n');

const panel = async (req, url, dir) => {
  if (url.pathname === '/about') {
    void resolveWeave();
    return [200, about()];
  }
  const weave = await resolveWeave();
  if (!weave) return [200, { error: weaveState.error ?? 'weave is not available.' }];
  switch (url.pathname) {
    case '/overview':
      try { return [200, overview(await getGraph(weave, dir))]; } catch { return [200, { error: 'weave could not map this project.' }]; }
    case '/node': {
      try {
        const n = node(await getGraph(weave, dir), url.searchParams.get('id') ?? '');
        return n ? [200, n] : [404, { message: 'not in the map' }];
      } catch { return [200, { error: 'weave could not map this project.' }]; }
    }
    case '/search': {
      await ensureIndex(weave, dir);
      return [200, { hits: (await search(weave, dir, url.searchParams.get('q') ?? '')).slice(0, 60) }];
    }
    case '/context':
    case '/impact': {
      const file = url.searchParams.get('file') ?? '';
      if (!safeFile(dir, file)) return [400, { message: 'no such file in this project' }];
      await ensureIndex(weave, dir);
      const r = await run(weave, [url.pathname.slice(1), file, '--root', dir], dir, 30_000);
      return [200, { text: r.out.trim() }];
    }
    case '/changes':
      await ensureIndex(weave, dir);
      return [200, await changes(weave, dir)];
    case '/specs':
      return [200, await specsList(weave, dir)];
    case '/spec':
      return specRead(weave, dir, url.searchParams.get('path'));
    case '/plan':
      return plan(weave, dir, url.searchParams.get('change') ?? '', url.searchParams.get('task') ?? '');
  }
  return [404, { message: 'not found' }];
};

const about = () => ({
  weave: { ...weaveState },
  settings,
  platform: `${process.platform}/${process.arch}`,
  asset: assetName(),
});

// ---------------------------------------------------------------- @ menu

const mentionIds = new Map(); // id -> { dir, file, kind, name }
let mentionSeq = 0;
const remember = (item) => {
  const id = `w${(++mentionSeq).toString(36)}`;
  mentionIds.set(id, item);
  if (mentionIds.size > 2000) mentionIds.delete(mentionIds.keys().next().value);
  return id;
};
const impactKinds = new Set(['endpoint', 'table', 'model']);

const mentionSearch = async ({ query, directory }) => {
  if (!directory || !path.isAbsolute(directory) || !existsSync(directory)) return { items: [] };
  const weave = weaveState.status === 'ready' ? weaveState.bin : null;
  if (!weave) { void resolveWeave(); return { items: [] }; }
  // The menu waits two seconds at most: never build the map inside a search.
  if (!existsSync(graphPath(directory))) { void ensureIndex(weave, directory); return { items: [] }; }
  void ensureIndex(weave, directory);
  let hits;
  if (query.trim()) {
    hits = await search(weave, directory, query);
  } else {
    const g = graphs.get(directory)?.g ?? readGraph(directory);
    hits = g.nodes.filter((n) => n.kind === 'page').slice(0, 20).map((n) => ({ kind: 'page', name: n.name, file: n.file ?? '' }));
  }
  // Open change records whose name matches (all of them for "spec", "task" or an
  // empty query): picking one gives the agent the next task's plan.
  let specRows = [];
  try {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    const all = /^(specs?|tasks?|changes?|plan)$/.test(words[0] ?? '') || !words.length;
    const g = graphs.get(directory)?.g ?? readGraph(directory);
    specRows = changesOf(g).filter((c) => c.done < c.total && (all || words.every((w) => c.slug.toLowerCase().includes(w))))
      .slice(0, 5).map((c) => {
        const next = c.tasks.find((t) => !t.done);
        return {
          id: remember({ dir: directory, kind: 'change', name: c.slug, file: c.file }),
          title: `${c.slug}`.slice(0, 120),
          subtitle: `next task · spec ${c.done}/${c.total}${next ? ` · ${next.id} ${next.title}` : ''}`.slice(0, 120),
        };
      });
  } catch { /* no map yet */ }
  return {
    items: [...specRows, ...hits.filter((h) => h.file).slice(0, 20 - specRows.length).map((h) => ({
      id: remember({ dir: directory, ...h }),
      title: `${h.name}`.slice(0, 120),
      subtitle: `${impactKinds.has(h.kind) ? 'impact' : 'context'} · ${h.kind} · ${h.file}`.slice(0, 120),
    }))],
  };
};

const mentionResolve = async ({ id }) => {
  const m = mentionIds.get(id);
  if (!m) return [404, { message: 'That weave row is gone; pick it again.' }];
  const weave = await resolveWeave();
  if (weave && m.kind === 'change') {
    const r = await run(weave, ['plan', m.name, '--root', m.dir], m.dir, 9000);
    return [200, { text: taskPrompt(m.name, r.out.trim()).slice(0, 16_000) }];
  }
  if (!weave || !safeFile(m.dir, m.file)) return [404, { message: 'weave could not read that file.' }];
  const mode = impactKinds.has(m.kind) ? 'impact' : 'context';
  const r = await run(weave, [mode, m.file, '--root', m.dir], m.dir, 9000);
  const text = `weave ${mode} for ${m.kind} ${m.name} (${m.file}):\n${r.out.trim()}`;
  return [200, { text: text.slice(0, 16_000) }];
};

// ---------------------------------------------------------------- after a turn

const turnStart = new Map(); // sessionId -> fingerprint
const toldSession = new Map(); // sessionId -> failure key

export const fixPrompt = (c) => [
  'weave check: your change broke these front-end calls. Fix them before you stop.',
  ...c.fails.slice(0, MAX_FAILS).map((f) => `- ${f.text}`),
  ...(c.tests.length ? ['Then run:', ...c.tests.slice(0, 3).map((t) => `  ${t}`)] : []),
].join('\n');

const afterTurn = async (event) => {
  const dir = event.project?.directory;
  const sid = event.session?.id;
  if (!sid || event.session?.parentId || !dir || !existsSync(dir)) return;
  if (event.type === 'turn-started') { turnStart.set(sid, await fingerprint(dir)); return; }
  if (event.type !== 'turn-finished' || !settings.autoGate) return;
  const before = turnStart.get(sid);
  turnStart.delete(sid);
  const now = await fingerprint(dir);
  if (before !== undefined && before === now) return; // the turn changed nothing
  if (!existsSync(graphPath(dir))) return; // weave has never mapped this project
  const result = await check(dir, true);
  if (result.error) return;
  const c = parseCheck(result);
  if (c.ok || !c.fails.length) { toldSession.delete(sid); return; }
  const key = c.fails.map((f) => f.text).join('\n');
  if (toldSession.get(sid) === key) return;
  toldSession.set(sid, key);
  await hostCall('sessions.send', { sessionId: sid, prompt: fixPrompt(c) });
};

// ---------------------------------------------------------------- HTTP

const json = (res, status, body) => {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(body));
};
const readBody = async (req) => {
  let body = '';
  for await (const chunk of req) {
    body += chunk;
    if (body.length > 1_000_000) break;
  }
  try { return JSON.parse(body || '{}'); } catch { return null; }
};

const PANEL = new Set(['/overview', '/node', '/search', '/context', '/impact', '/changes', '/about', '/specs', '/spec', '/plan']);
const SPEC_WRITES = { '/spec/save': specSave, '/spec/new': specNew, '/spec/task': specTask };

export const server = http.createServer(async (req, res) => {
  try {
    if (req.headers.authorization !== `Bearer ${token}`) return json(res, 401, { message: 'unauthorized' });
    const url = new URL(req.url ?? '/', 'http://127.0.0.1');
    if (url.pathname === '/health') return json(res, 200, { ok: true });

    if (req.method === 'POST' && url.pathname === '/mentions/search') {
      const body = await readBody(req);
      if (!body || typeof body.query !== 'string') return json(res, 400, { message: 'bad request' });
      const items = await Promise.race([
        mentionSearch(body),
        new Promise((r) => setTimeout(() => r({ items: [] }), 1800)),
      ]);
      return json(res, 200, items);
    }
    if (req.method === 'POST' && url.pathname === '/mentions/resolve') {
      const body = await readBody(req);
      if (!body || typeof body.id !== 'string') return json(res, 400, { message: 'bad request' });
      const [status, out] = await mentionResolve(body);
      return json(res, status, out);
    }
    if (req.method === 'POST' && url.pathname === '/session-events') {
      const body = await readBody(req);
      if (!body || body.v !== 1 || !body.event?.type) return json(res, 400, { ok: false, error: 'not a session event' });
      json(res, 200, { ok: true });
      void afterTurn(body.event).catch((e) => console.error('after turn:', e));
      return;
    }
    if (req.method === 'POST' && url.pathname === '/settings') {
      const body = await readBody(req);
      if (!body) return json(res, 400, { message: 'bad request' });
      const next = {};
      for (const k of Object.keys(DEFAULT_SETTINGS)) if (typeof body[k] === 'boolean') next[k] = body[k];
      saveSettings(next);
      await publishInstructions();
      return json(res, 200, about());
    }
    if (req.method === 'POST' && url.pathname === '/update') {
      const r = weaveState.source === 'download' ? await maybeUpdate(true) : { updated: false, reason: 'weave comes from PATH; update it there' };
      return json(res, 200, { ...r, ...about() });
    }

    const dir = url.searchParams.get('directory') ?? '';
    if (SPEC_WRITES[url.pathname]) {
      if (req.method !== 'POST') return json(res, 405, { message: 'method not allowed' });
      if (!path.isAbsolute(dir) || !existsSync(dir)) return json(res, 400, { message: 'open a project first' });
      const weave = await resolveWeave();
      if (!weave) return json(res, 200, { error: weaveState.error ?? 'weave is not available.' });
      const body = await readBody(req);
      if (!body) return json(res, 400, { message: 'bad request' });
      const [status, out] = await SPEC_WRITES[url.pathname](weave, dir, body);
      gateCache.delete(dir);
      return json(res, status, out);
    }
    if (PANEL.has(url.pathname)) {
      if (req.method !== 'GET') return json(res, 405, { message: 'method not allowed' });
      if (url.pathname !== '/about' && (!path.isAbsolute(dir) || !existsSync(dir))) return json(res, 400, { message: 'open a project first' });
      const [status, body] = await panel(req, url, dir);
      return json(res, status, body);
    }

    if (url.pathname !== '/status') return json(res, 404, { message: 'not found' });
    if (!path.isAbsolute(dir) || !existsSync(dir)) {
      return json(res, 200, { rows: [{ kind: 'note', id: 'no-project', text: 'Open a project to run the gate on it.', tone: 'muted' }] });
    }
    if (req.method === 'POST') {
      const body = await readBody(req);
      if (body?.id !== 'refresh') return json(res, 400, { message: 'unknown control' });
      await check(dir, true);
      return json(res, 200, { ok: true });
    }
    if (req.method !== 'GET') return json(res, 405, { message: 'method not allowed' });
    if (weaveState.status !== 'ready') {
      void resolveWeave();
      if (weaveState.status === 'error') return json(res, 200, view({ error: weaveState.error }));
      return json(res, 200, view({ pending: 'Getting weave ready (a one-time download)…' }));
    }
    const v = view(await check(dir, false));
    v.rows = [...v.rows, ...specRows(dir)].slice(0, 16);
    json(res, 200, v);
  } catch (e) {
    console.error(e);
    if (!res.headersSent) json(res, 500, { message: 'weave service error' });
  }
});

if (listen) {
  server.listen(port, '127.0.0.1');
  // Started with OpenChamber (keepRunning): get weave and tell the agents about it now.
  void resolveWeave().then(() => maybeUpdate());
}
