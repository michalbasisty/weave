import { Builder, id } from './model.ts';
import { commentAbove, normalizePath, type Endpoint } from './extract.ts';
import type { SourceFile } from './scan.ts';

// Next.js and tRPC publish their contracts structurally: file paths and router
// objects. No AST needed, but unlike Express nothing is explicit, so every file
// that looks like a contract and yields nothing is reported.

export interface ExtractStats {
  files: number;
  found: number;
  silent: string[];
  endpoints: Endpoint[];
}

// ---------------------------------------------------------------- Next.js

const APP_ROUTE = /(?:^|\/)(?:src\/)?app\/(?:(.*)\/)?route\.(?:t|j)sx?$/;
const PAGES_API = /(?:^|\/)(?:src\/)?pages\/api\/(.+)\.(?:t|j)sx?$/;
const METHOD_EXPORT = /export\s+(?:async\s+function|const|let)\s+(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\b/g;
const METHOD_BRANCH = /req\.method\s*={2,3}\s*['"]([A-Z]+)['"]/g;

/** `app/api/orders/[id]/route.ts` → `/api/orders/:id` */
function appPath(segments: string | undefined): string {
  const parts = (segments ?? '')
    .split('/')
    .filter((s) => s && !s.startsWith('(') && !s.startsWith('@')) // route groups, parallel slots
    .map((s) => (s.startsWith('[...') || s.startsWith('[[...') ? '*' : s.replace(/^\[(?:\.\.\.)?(.+)\]$/, ':$1')));
  return normalizePath('/' + parts.join('/'));
}

export function extractNextRoutes(files: SourceFile[], b: Builder): ExtractStats {
  let candidates = 0;
  let found = 0;
  const silent: string[] = [];
  const endpoints: Endpoint[] = [];

  for (const f of files) {
    const app = f.rel.match(APP_ROUTE);
    const pages = f.rel.match(PAGES_API);
    if (!app && !pages) continue;
    candidates++;

    const path = app ? appPath(app[1]) : normalizePath('/api/' + pages![1].replace(/\/index$/, '').replace(/\[(?:\.\.\.)?([^\]]+)\]/g, ':$1'));

    const methods = new Set<string>();
    for (const m of f.text.matchAll(METHOD_EXPORT)) methods.add(m[1]);
    for (const m of f.text.matchAll(METHOD_BRANCH)) methods.add(m[1]);
    if (!methods.size && /export\s+default\b/.test(f.text)) methods.add('ANY');
    if (!methods.size) {
      silent.push(f.rel);
      continue;
    }

    const desc = commentAbove(f.lines, 1) || '';
    const mod = f.rel.includes('/app/') ? (f.rel.split('/app/')[1] ?? '').split('/')[0] : 'api';
    if (mod) b.node({ id: id.module(mod), kind: 'module', name: mod, file: f.rel });
    for (const method of methods) {
      const eid = id.endpoint(method, path);
      b.node({ id: eid, kind: 'endpoint', name: `${method} ${path}`, file: f.rel, meta: { module: mod, desc, framework: 'next' } });
      b.edge(eid, id.file(f.rel), 'defined_in');
      if (mod) {
        b.edge(eid, id.module(mod), 'in_module');
        b.edge(id.module(mod), id.file(f.rel), 'contains');
      }
      endpoints.push({ method, path, file: f.rel, line: 0, module: mod, id: eid });
    }
    found++;
  }
  return { files: candidates, found, silent, endpoints };
}

// ---------------------------------------------------------------- tRPC

const ROUTER_BLOCK = /([A-Za-z_$][\w$]*)\s*:\s*(?:[\w$]+\.)?router\s*\(\s*\{/g;
const PROCEDURE = /([A-Za-z_$][\w$]*)\s*:\s*[^,{}\n]*?\.(query|mutation|subscription)\s*\(/g;

/** Index of the brace that closes the one at `open`, or text.length. */
function matchBrace(text: string, open: number): number {
  let depth = 0;
  for (let i = open; i < text.length; i++) {
    const c = text[i];
    if (c === '{') depth++;
    else if (c === '}') {
      depth--;
      if (depth === 0) return i;
    }
  }
  return text.length;
}

export function scanProcedures(text: string): { name: string; kind: 'QUERY' | 'MUTATION' | 'SUBSCRIPTION' }[] {
  const blocks: { name: string; start: number; end: number }[] = [];
  for (const m of text.matchAll(ROUTER_BLOCK)) {
    const open = m.index! + m[0].length - 1;
    blocks.push({ name: m[1], start: m.index!, end: matchBrace(text, open) });
  }
  const out: { name: string; kind: 'QUERY' | 'MUTATION' | 'SUBSCRIPTION' }[] = [];
  for (const m of text.matchAll(PROCEDURE)) {
    const at = m.index!;
    const parents = blocks
      .filter((b) => b.start < at && at < b.end)
      .sort((a, b) => a.start - b.start)
      .map((b) => b.name);
    const kind = m[2] === 'query' ? 'QUERY' : m[2] === 'mutation' ? 'MUTATION' : 'SUBSCRIPTION';
    out.push({ name: [...parents, m[1]].join('.'), kind });
  }
  return out;
}

export function extractTRPC(files: SourceFile[], b: Builder): ExtractStats {
  let candidates = 0;
  let found = 0;
  const silent: string[] = [];
  const endpoints: Endpoint[] = [];

  for (const f of files) {
    // A router file declares procedures; a provider/client file does not.
    if (!/\b(?:publicProcedure|protectedProcedure|procedure|t\.procedure)\b/.test(f.text)) continue;
    if (!/\brouter\s*\(\s*\{/.test(f.text)) continue;
    candidates++;

    const procs = scanProcedures(f.text);
    if (!procs.length) {
      silent.push(f.rel);
      continue;
    }
    const mod = 'trpc';
    b.node({ id: id.module(mod), kind: 'module', name: mod, file: f.rel });
    for (const p of procs) {
      const path = normalizePath('/api/trpc/' + p.name);
      const eid = id.endpoint(p.kind, path);
      b.node({
        id: eid,
        kind: 'endpoint',
        name: `${p.kind} ${path}`,
        file: f.rel,
        meta: { module: mod, framework: 'trpc', desc: `${p.kind.toLowerCase()} ${p.name}` },
      });
      b.edge(eid, id.file(f.rel), 'defined_in');
      b.edge(eid, id.module(mod), 'in_module');
      endpoints.push({ method: p.kind, path, file: f.rel, line: 0, module: mod, id: eid });
    }
    b.edge(id.module(mod), id.file(f.rel), 'contains');
    found++;
  }
  return { files: candidates, found, silent, endpoints };
}
