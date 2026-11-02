import { Builder, id, type Conf, type GEdge } from './model.ts';
import { baseName, dirOf, eachMatch, stripExt, type SourceFile } from './scan.ts';

// ---------------------------------------------------------------- targets

export interface TargetMap {
  backend: SourceFile[];
  web: SourceFile[];
  prisma: SourceFile[];
  specs: SourceFile[];
  all: SourceFile[];
}

export function classify(files: SourceFile[], root: string): TargetMap {
  const isTest = (f: SourceFile) => /(^|\/)(__tests__|e2e|tests?)\//.test(f.rel) || /\.(test|spec)\.[tj]sx?$/.test(f.rel);
  const backend = files.filter(
    (f) => !isTest(f) && (/^packages\/backend\//.test(f.rel) || /\/(routes|controller|handler|service)\b/.test(f.rel)),
  );
  const web = files.filter(
    (f) =>
      !isTest(f) &&
      (/^packages\/web\//.test(f.rel) || /\/(pages|components|views|hooks)\//.test(f.rel)) &&
      /\.(tsx|jsx|vue|svelte)$/.test(f.rel),
  );
  const prisma = files.filter((f) => f.rel.endsWith('schema.prisma') || f.rel.endsWith('.prisma'));
  const specs = files.filter((f) => /(^|\/)specs?\//.test(f.rel) && f.rel.endsWith('.md'));
  return { backend, web, prisma, specs, all: files };
}

// ---------------------------------------------------------------- imports

/** Resolve a relative import spec to a known file, handling `.js` suffixes and index files. */
export function resolveImport(importer: string, spec: string, known: Set<string>): string | null {
  if (!spec.startsWith('.')) return null;
  const base = stripExt(joinPosix(dirOf(importer), spec));
  const guesses = [
    base + '.ts', base + '.tsx', base + '.js', base + '.jsx', base + '.mjs',
    base + '/index.ts', base + '/index.tsx', base + '/index.js',
  ];
  for (const g of guesses) if (known.has(g)) return g;
  return null;
}

export function joinPosix(a: string, b: string): string {
  const parts = (a + '/' + b).split('/');
  const out: string[] = [];
  for (const p of parts) {
    if (p === '.' || p === '') continue;
    if (p === '..') out.pop();
    else out.push(p);
  }
  return out.join('/');
}

const IMPORT_RE = /(?:^|\n)\s*(?:import|export)[^'"]*?from\s*['"]([^'"]+)['"]/g;

export function importMap(files: SourceFile[], known: Set<string>): Map<string, string[]> {
  const map = new Map<string, string[]>();
  for (const f of files) {
    const targets: string[] = [];
    for (const m of f.text.matchAll(IMPORT_RE)) {
      const r = resolveImport(f.rel, m[1], known);
      if (r && r !== f.rel) targets.push(r);
    }
    map.set(f.rel, targets);
  }
  return map;
}

/** Files reachable from `start` through imports (BFS, capped). */
export function reachable(start: string, map: Map<string, string[]>, cap = 80): Set<string> {
  const seen = new Set<string>([start]);
  const queue = [start];
  while (queue.length && seen.size < cap) {
    const cur = queue.shift()!;
    for (const next of map.get(cur) ?? []) {
      if (seen.has(next) || seen.size >= cap) continue;
      seen.add(next);
      queue.push(next);
    }
  }
  return seen;
}

// ---------------------------------------------------------------- endpoints

export interface Endpoint {
  method: string;
  path: string;
  file: string;
  line: number;
  module: string;
  id: string;
}

const ROUTER_VERB = /router\.(get|post|put|patch|delete)\s*\(\s*['"`]([^'"`]*)['"`]/g;
const MOUNT = /app\.use\s*\(\s*['"`](\/api[^'"`]*)['"`]\s*,\s*(\w+)\s*\)/;

export function extractEndpoints(files: SourceFile[], b: Builder): Endpoint[] {
  const known = new Set(files.map((f) => f.rel));
  const routeFiles = files.filter((f) => /router\.(?:get|post|put|patch|delete)\s*\(/.test(f.text));
  const endpointList: Endpoint[] = [];

  // Find the app file: the one mounting the most /api routers.
  let appFile: SourceFile | undefined;
  let best = 0;
  for (const f of files) {
    const n = [...f.text.matchAll(new RegExp(MOUNT.source, 'g'))].length;
    if (n > best) {
      best = n;
      appFile = f;
    }
  }

  const mountOf = new Map<string, { base: string; mountLine: number }>();
  if (appFile) {
    const idents = new Map<string, string>();
    for (const m of appFile.text.matchAll(/import\s+(\w+)\s+from\s+['"](\.{1,2}\/[^'"]+)['"]/g)) {
      const r = resolveImport(appFile.rel, m[2], known);
      if (r) idents.set(m[1], r);
    }
    for (const { m, line } of eachMatch(appFile.text, MOUNT)) {
      const target = idents.get(m[2]);
      if (target) mountOf.set(target, { base: m[1].replace(/\/+$/, ''), mountLine: line });
    }
    b.node({ id: id.file(appFile.rel), kind: 'file', name: appFile.rel, file: appFile.rel });
  }

  // Fall back: no app mounts found -> a single base derived from the router files.
  let defaultBase = '';
  if (mountOf.size === 0) {
    const sample = routeFiles.map((f) => f.text.match(/router\.(?:get|post)\(\s*['"`](\/[^'"`]*)['"`]/)?.[1]).filter(Boolean) as string[];
    defaultBase = sample.length && sample.every((s) => s.startsWith('/api')) ? '/api/v1' : '';
  }

  for (const f of routeFiles) {
    const mount = mountOf.get(f.rel);
    const base = mount?.base ?? defaultBase;
    const mod = moduleOf(f.rel);
    if (mod) b.node({ id: id.module(mod), kind: 'module', name: mod, file: dirOf(f.rel) });
    b.node({ id: id.file(f.rel), kind: 'file', name: f.rel, file: f.rel });
    for (const m of f.text.matchAll(ROUTER_VERB)) {
      const line = lineAt(f.text, m.index ?? 0);
      const method = m[1].toUpperCase();
      const sub = m[2] === '/' ? '' : m[2];
      const path = normalizePath(base + sub);
      const eid = id.endpoint(method, path);
      const desc = commentAbove(f.lines, line);
      b.node({ id: eid, kind: 'endpoint', name: `${method} ${path}`, file: f.rel, line, meta: { module: mod, desc } });
      b.edge(eid, id.file(f.rel), 'defined_in');
      if (mod) {
        b.node({ id: id.module(mod), kind: 'module', name: mod, file: dirOf(f.rel) });
        b.edge(eid, id.module(mod), 'in_module');
        b.edge(id.module(mod), id.file(f.rel), 'contains');
      }
      endpointList.push({ method, path, file: f.rel, line, module: mod, id: eid });
    }
  }
  return endpointList;
}

function moduleOf(file: string): string {
  const m = file.match(/modules\/([^/]+)\//);
  if (m) return m[1];
  const d = dirOf(file);
  return d ? baseName(d) : '';
}

// ---------------------------------------------------------------- prisma

export interface Model {
  name: string;
  table: string;
  file: string;
  line: number;
  fields: { name: string; type: string; relation: boolean }[];
}

export function extractModels(files: SourceFile[], b: Builder): Model[] {
  const out: Model[] = [];
  for (const f of files) {
    const known: string[] = [];
    for (const m of f.text.matchAll(/^model\s+(\w+)\s*\{/gm)) known.push(m[1]);
    const modelSet = new Set(known);
    for (const m of f.text.matchAll(/^model\s+(\w+)\s*\{([\s\S]*?)^\}/gm)) {
      const name = m[1];
      const body = m[2];
      const table = (body.match(/@@map\(\s*"([^"]+)"/) ?? [])[1] ?? name;
      const line = f.text.slice(0, m.index).split('\n').length;
      const fields: Model['fields'] = [];
      for (const raw of body.split('\n')) {
        const t = raw.trim();
        if (!t || t.startsWith('//') || t.startsWith('@@')) continue;
        const fm = t.match(/^(\w+)\s+(\S+)/);
        if (!fm) continue;
        const type = fm[2].replace(/[?[\]]/g, '');
        fields.push({ name: fm[1], type, relation: modelSet.has(type) });
      }
      const mid = id.model(name);
      const desc = commentAbove(f.lines, line);
      b.node({ id: mid, kind: 'model', name, file: f.rel, line, meta: { desc } });
      b.node({ id: id.table(table), kind: 'table', name: table, file: f.rel, meta: { desc } });
      b.edge(mid, id.table(table), 'maps_to');
      for (const fl of fields) if (fl.relation) b.edge(mid, id.model(fl.type), 'relates');
      out.push({ name, table, file: f.rel, line, fields });
    }
  }
  // Which models each module touches (prisma.<model> or tx.<model>).
  const modelByLower = new Map(out.map((m) => [m.name.toLowerCase(), m.name]));
  for (const file of files) {
    const mod = moduleOf(file.rel);
    if (!mod) continue;
    for (const m of file.text.matchAll(/\b(?:prisma|tx|db)\.(\w+)\./g)) {
      const model = modelByLower.get(m[1].toLowerCase());
      if (model) b.edge(id.module(mod), id.model(model), 'uses');
    }
  }
  return out;
}

// ---------------------------------------------------------------- web: pages + calls

export interface Call {
  method: string;
  path: string;
  file: string;
  line: number;
  dynamic: boolean;
}

const CALL_RE = /\b(?:api|http|httpClient|axios|client|fetcher)\s*\.\s*(get|post|put|patch|delete)\s*\(\s*[`'"]([^`'"]+)[`'"]/;
const FETCH_RE = /\bfetch\s*\(\s*[`'"]([^`'"]+)[`'"]\s*(?:,\s*\{[^}]*method\s*:\s*['"](\w+)['"])?/;
const TRPC_CALL = /\btrpc(?:Client|Proxy)?\s*\.\s*([\w$.]+?)\s*\.\s*(useQuery|useMutation|useInfiniteQuery|useSuspenseQuery|useQueries|query|mutate)\s*\(/;

export function extractCalls(files: SourceFile[]): Call[] {
  const calls: Call[] = [];
  for (const f of files) {
    for (const { m, line } of eachMatch(f.text, CALL_RE)) {
      calls.push({ method: m[1].toUpperCase(), path: m[2], file: f.rel, line, dynamic: m[2].includes('${') });
    }
    for (const { m, line } of eachMatch(f.text, FETCH_RE)) {
      if (!m[1].startsWith('/') && !m[1].includes('/api/')) continue;
      calls.push({ method: (m[2] ?? 'GET').toUpperCase(), path: m[1], file: f.rel, line, dynamic: m[1].includes('${') });
    }
    for (const { m, line } of eachMatch(f.text, TRPC_CALL)) {
      const mutation = /mutation|mutate/i.test(m[2]);
      calls.push({ method: mutation ? 'MUTATION' : 'QUERY', path: `/api/trpc/${m[1]}`, file: f.rel, line, dynamic: false });
    }
  }
  return calls;
}

const ROUTE_LINE = /<Route\s+path=["']([^"']+)["'](.*)$/;
const NOT_A_PAGE = new Set(['ProtectedRoute', 'Navigate', 'Suspense', 'Fragment', 'Routes', 'Route']);

export function extractPages(
  webFiles: SourceFile[],
  b: Builder,
): { pages: { route: string; component: string; file?: string }[]; declared: number; skipped: string[] } {
  const known = new Set(webFiles.map((f) => f.rel));
  const byRel = new Map(webFiles.map((f) => [f.rel, f]));
  const appFiles = webFiles.filter((f) => f.rel.endsWith('App.tsx') || f.rel.endsWith('App.jsx') || /routes?\.tsx?$/.test(f.rel));
  const pages: { route: string; component: string; file?: string }[] = [];
  let declared = 0;
  const skipped: string[] = [];
  for (const f of appFiles) {
    const idents = new Map<string, string>();
    for (const m of f.text.matchAll(/import\s+(?:\w+\s*,\s*)?\{?\s*(\w+)\s*\}?\s*(?:,\s*\w+\s*)?from\s+['"](\.{1,2}\/[^'"]+)['"]/g)) {
      const r = resolveImport(f.rel, m[2], known);
      if (r) idents.set(m[1], r);
    }
    for (const m of f.text.matchAll(/const\s+(\w+)\s*=\s*(?:React\.)?lazy\(\s*\(\)\s*=>\s*import\(\s*['"](\.{1,2}\/[^'"]+)['"]/g)) {
      const r = resolveImport(f.rel, m[2], known);
      if (r) idents.set(m[1], r);
    }
    for (const { m, line } of eachMatch(f.text, ROUTE_LINE)) {
      const route = m[1];
      declared++;
      const component = [...m[2].matchAll(/<\s*([A-Z]\w*)/g)].map((x) => x[1]).find((c) => !NOT_A_PAGE.has(c));
      if (!component) {
        skipped.push(route);
        continue;
      }
      const pid = id.page(route);
      const cf = idents.get(component);
      const desc = commentAbove(f.lines, line) || (cf ? topComment(byRel.get(cf)?.lines ?? []) : '');
      b.node({ id: pid, kind: 'page', name: route, file: cf ?? f.rel, line, meta: { component, desc } });
      if (cf) b.edge(pid, id.file(cf), 'renders');
      b.edge(pid, id.file(f.rel), 'declared_in');
      pages.push({ route, component, file: cf });
    }
  }
  return { pages, declared, skipped };
}

/**
 * Link a call site to the endpoint it hits. Call sites are written relative to their
 * client's base URL and repos mix families (`/api/v1/...`, `/api/...`, `/api/trpc/...`),
 * so the comparison aligns on the tail of both paths instead of assuming one mount.
 */
export function matchEndpoint(call: Call, endpoints: Endpoint[]): Endpoint | null {
  let p = call.path.split('?')[0];
  if (p.includes('${')) {
    // A template literal cut short by an inner quote still names the path before it.
    const open = p.indexOf('${');
    if (p.indexOf('}', open) === -1) p = p.slice(0, open) || '/';
    else p = p.replace(/\$\{[^}]*\}/g, '*');
  }
  if (!p.startsWith('/')) p = '/' + p;
  const cs = segments(p);
  if (!cs.length) return null;

  let best: Endpoint | null = null;
  let bestScore = -1;
  for (const e of endpoints) {
    if (e.method !== call.method && e.method !== 'ANY') continue;
    const es = segments(e.path);
    const n = Math.min(es.length, cs.length);
    if (!n) continue;
    let score = 0;
    let ok = true;
    for (let i = 1; i <= n; i++) {
      const a = es[es.length - i];
      const b = cs[cs.length - i];
      if (a.startsWith(':') || a === '*') {
        score += 0.5;
        continue;
      }
      if (b === '*') {
        score += 0.4;
        continue;
      }
      if (a === b) {
        score += 2;
        continue;
      }
      ok = false;
      break;
    }
    if (ok && score > bestScore) {
      bestScore = score;
      best = e;
    }
  }
  return best;
}

function segments(p: string): string[] {
  return p.split('/').filter(Boolean);
}

/** 1-based line of a byte offset. */
function lineAt(text: string, index: number): number {
  let n = 1;
  for (let i = 0; i < index && i < text.length; i++) if (text.charCodeAt(i) === 10) n++;
  return n;
}

/** The comment block immediately above a 1-based line, as one sentence. */
export function commentAbove(lines: string[], line: number, max = 220): string {
  const out: string[] = [];
  let inBlock = false;
  for (let i = line - 2, n = 0; i >= 0 && n < 14; i--, n++) {
    const raw = lines[i] ?? '';
    const t = raw.trim();
    if (!t) {
      if (out.length || inBlock) break;
      continue;
    }
    if (inBlock) {
      out.unshift(cleanComment(t));
      if (t.startsWith('/*')) {
        inBlock = false;
        break;
      }
      continue;
    }
    if (t.endsWith('*/')) {
      inBlock = true;
      out.unshift(cleanComment(t));
      continue;
    }
    if (t.startsWith('//') || t.startsWith('*')) {
      out.unshift(cleanComment(t));
      continue;
    }
    break;
  }
  return out.filter((s) => !isNoise(s)).join(' ').replace(/\s+/g, ' ').trim().slice(0, max);
}

/** First comment block near the top of a file — what the file is for. */
export function topComment(lines: string[], max = 220): string {
  for (let i = 0; i < Math.min(lines.length, 24); i++) {
    const t = (lines[i] ?? '').trim();
    if (t.startsWith('//') || t.startsWith('/*')) return commentAbove(lines, i + 2 > lines.length ? lines.length : nextNonComment(lines, i) + 1, max);
  }
  return '';
}

function nextNonComment(lines: string[], from: number): number {
  let i = from;
  while (i < lines.length) {
    const t = (lines[i] ?? '').trim();
    if (!t.startsWith('//') && !t.startsWith('*') && !t.startsWith('/*')) return i;
    i++;
  }
  return i;
}

function cleanComment(t: string): string {
  return t
    .replace(/^\/\*\*?/, '')
    .replace(/^\/\/+/, '')
    .replace(/^\*+\/?/, '')
    .replace(/\*\/$/, '')
    .trim();
}

/** Rule lines, banner headings and one-word labels are noise, not a description. */
function isNoise(t: string): boolean {
  if (/^[=\-*~_#\s]+$/.test(t)) return true;
  if (/^[=\-*~_#]{2,}.*[=\-*~_#]{2,}$/.test(t)) return true;
  return t.replace(/[^\w]/g, '').length < 12;
}

export function normalizePath(p: string): string {
  const clean = p.replace(/\/{2,}/g, '/');
  if (clean.length > 1 && clean.endsWith('/')) return clean.slice(0, -1);
  return clean || '/';
}

export { type Conf, type GEdge };
export { Builder, id };
