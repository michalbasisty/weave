import type { GEdge, GNode, Graph } from './model.ts';
import { planTests } from './runners.ts';
import { reqsInCase, type Case } from './junit.ts';

const MAX = 8;

export class Index {
  g: Graph;
  byId = new Map<string, GNode>();
  out = new Map<string, GEdge[]>();
  inn = new Map<string, GEdge[]>();

  constructor(g: Graph) {
    this.g = g;
    for (const n of g.nodes) this.byId.set(n.id, n);
    for (const e of g.edges) {
      const o = this.out.get(e.from);
      if (o) o.push(e);
      else this.out.set(e.from, [e]);
      const i = this.inn.get(e.to);
      if (i) i.push(e);
      else this.inn.set(e.to, [e]);
    }
  }

  node(id: string): GNode | undefined {
    return this.byId.get(id);
  }
  from(id: string, kind?: string): GEdge[] {
    const all = this.out.get(id) ?? [];
    return kind ? all.filter((e) => e.kind === kind) : all;
  }
  to(id: string, kind?: string): GEdge[] {
    const all = this.inn.get(id) ?? [];
    return kind ? all.filter((e) => e.kind === kind) : all;
  }
  ofKind(kind: string): GNode[] {
    return this.g.nodes.filter((n) => n.kind === kind);
  }
}

// ---------------------------------------------------------------- analysis

export interface Facts {
  files: string[];
  dependents: string[];
  label: string;
  modules: string[];
  endpoints: GNode[];
  pages: { name: string; conf: 'exact' | 'inferred' }[];
  tables: string[];
  tests: string[];
  commands: string[];
  reqs: string[];
  suspect: string[];
  orphans: string[];
  untested: string[];
  unclaimed: string[];
  schemaChanged: boolean;
  changes: { slug: string; done: number; total: number; open: string[]; file: string; evidence: boolean; score: number }[];
}

/** Everything a change to `inputs` can reach. Null when no file matched. */
export function analyze(g: Graph, inputs: string[]): Facts | null {
  const ix = new Index(g);
  const root = g.root.replace(/\\/g, '/').replace(/\/$/, '') + '/';
  const files = new Set<string>();
  for (const raw of inputs) {
    let p = raw.replace(/\\/g, '/').trim();
    if (p.startsWith(root)) p = p.slice(root.length);
    p = p.replace(/^\.\//, '');
    if (ix.byId.has('file:' + p)) files.add(p);
    else {
      for (const n of ix.ofKind('file')) {
        if (n.name === p || n.name.endsWith('/' + p) || n.name.includes(p)) files.add(n.name);
      }
    }
  }
  if (!files.size) return null;

  // Direct importers. One hop only: a shared util's full transitive tree is noise,
  // but "who imports this" is the answer an agent needs before touching it.
  const dependents = new Set<string>();
  for (const f of files) {
    for (const e of ix.to('file:' + f, 'imports')) {
      const n = ix.node(e.from);
      if (n?.file && !files.has(n.file)) dependents.add(n.file);
    }
  }

  const modules = new Set<string>();
  for (const f of files) {
    const m = f.match(/modules\/([^/]+)\//);
    if (m) modules.add(m[1]);
  }
  const ownModules = new Set(modules);
  // A widely imported file touches every importer's module: only expand when that stays readable.
  if (dependents.size <= 12) {
    for (const f of dependents) {
      const m = f.match(/modules\/([^/]+)\//);
      if (m) modules.add(m[1]);
    }
  }
  for (const m of [...modules]) if (!ix.byId.has('module:' + m)) modules.delete(m);

  // Seed endpoints: defined in a changed file or module, or called by one.
  const endpoints = new Set<string>();
  for (const e of ix.ofKind('endpoint')) {
    if (e.file && files.has(e.file)) endpoints.add(e.id);
    else if (e.meta?.module && modules.has(String(e.meta.module))) endpoints.add(e.id);
  }
  for (const f of files) {
    for (const e of ix.from('file:' + f, 'calls')) endpoints.add(e.to);
    for (const r of ix.to('file:' + f, 'renders')) for (const c of ix.from(r.from, 'calls')) endpoints.add(c.to);
  }
  for (const ep of endpoints) {
    const m = ix.node(ep)?.meta?.module;
    if (typeof m === 'string' && ix.byId.has('module:' + m)) modules.add(m);
  }

  const pages = new Map<string, 'exact' | 'inferred'>();
  for (const f of files) {
    for (const kind of ['renders', 'declared_in']) {
      for (const r of ix.to('file:' + f, kind)) {
        const n = ix.node(r.from);
        if (n?.kind === 'page') pages.set(n.name, 'exact');
      }
    }
  }
  for (const ep of endpoints) {
    for (const e of ix.to(ep, 'calls')) {
      const n = ix.node(e.from);
      if (n?.kind !== 'page') continue;
      if (pages.get(n.name) !== 'exact') pages.set(n.name, e.conf);
    }
  }

  const schemaChanged = [...files].some((f) => f.endsWith('.prisma'));
  const models = new Set<string>();
  if (schemaChanged) for (const m of ix.ofKind('model')) models.add(m.id);
  for (const m of modules) for (const e of ix.from('module:' + m, 'uses')) models.add(e.to);
  const tables = new Set<string>();
  for (const m of models) for (const e of ix.from(m, 'maps_to')) tables.add(ix.node(e.to)?.name ?? e.to);

  const tests = new Set<string>();
  for (const t of ix.ofKind('test')) {
    if (t.file && files.has(t.file)) tests.add(t.name);
    for (const e of ix.from(t.id)) {
      if (endpoints.has(e.to)) tests.add(t.name);
      if (modules.has(e.to.replace('module:', ''))) tests.add(t.name);
    }
    for (const e of ix.from(t.id, 'exercises')) {
      if (pages.has(ix.node(e.to)?.name ?? '')) tests.add(t.name);
    }
  }

  const reqs = new Set<string>();
  for (const f of files) {
    // a @req marker in the code is the strongest link there is
    for (const e of ix.from('file:' + f, 'implements')) reqs.add(ix.node(e.to)?.name ?? e.to);
    if (!f.endsWith('.md')) continue;
    for (const e of ix.to('file:' + f, 'specified_in')) reqs.add(ix.node(e.from)?.name ?? e.from);
  }

  // Call sites in the changed files that match no endpoint: a contract already broken.
  const orphans = new Set<string>();
  for (const f of files) {
    for (const e of ix.from('file:' + f, 'calls_missing')) orphans.add(ix.node(e.to)?.name ?? e.to);
  }

  // Spec change records whose tasks name any touched file. Ranked: a change that
  // mentions the module/page wins over one that merely touched the same file.
  const changes: Facts['changes'] = [];
  const keywords = [...modules, ...[...pages.keys()].map((p) => p.replace(/[/:]/g, ' ').trim())].filter(Boolean).map((s) => s.toLowerCase());
  for (const c of ix.ofKind('change')) {
    const tasks = ix.from(c.id, 'has_task').map((e) => ix.node(e.to)).filter(Boolean) as GNode[];
    if (!tasks.length) continue;
    const exactHit = tasks.filter((t) =>
      ix.from(t.id, 'touches').some((e) => e.conf === 'exact' && files.has(ix.node(e.to)?.name ?? '')),
    );
    const anyHit = exactHit.length
      ? exactHit
      : tasks.filter((t) => ix.from(t.id, 'touches').some((e) => files.has(ix.node(e.to)?.name ?? '')));
    if (!anyHit.length) continue;
    const hit = anyHit;
    for (const e of ix.from(hit[0].id, 'satisfies')) reqs.add(ix.node(e.to)?.name ?? e.to);
    const done = tasks.filter((t) => t.meta?.done).length;
    const open = tasks.filter((t) => !t.meta?.done).map((t) => `${t.name.split(' ').pop()} ${String(t.meta?.title ?? '')}`.trim());
    const slug = c.name.toLowerCase();
    // A task that names the file itself beats one that only names its directory.
    let score = hit.length * 10 + (exactHit.length ? 15 : 0) + (hit.length === tasks.length ? 5 : 0);
    if (keywords.some((k) => k.length > 3 && slug.includes(k))) score += 25;
    changes.push({ slug: c.name, done, total: tasks.length, open, file: c.file ?? '', evidence: hit.length === tasks.length, score });
  }
  changes.sort((a, b) => b.score - a.score);

  const untested = [...endpoints].filter((ep) => !ix.to(ep, 'hits').length && !ix.to(ep, 'covers').length).map((ep) => ix.node(ep)?.name ?? ep);
  const unclaimed = [...endpoints].filter((ep) => ![...ix.to(ep, 'calls')].some((e) => ix.node(e.from)?.kind === 'page')).map((ep) => ix.node(ep)?.name ?? ep);

  // Requirements whose text changed since the index the code was written against.
  const suspect = [...reqs].filter((r) => ix.node('req:' + r)?.meta?.suspect === true);

  const { commands } = planTests(g.root, [...tests]);

  const pageDirs = new Set(
    ix.ofKind('page').map((p) => p.file).filter((x): x is string => !!x).map((p) => p.slice(0, p.lastIndexOf('/'))),
  );
  const isPageFile = [...files].some(
    (f) => ix.to('file:' + f, 'renders').length > 0 || pageDirs.has(f.slice(0, f.lastIndexOf('/'))),
  );
  let label = [...files][0].split('/').pop() ?? '';
  if (ownModules.size) label = `module ${[...ownModules][0]}`;
  if (isPageFile && pages.size) label = String([...pages.keys()][0]);

  return {
    files: [...files],
    dependents: [...dependents],
    label,
    modules: [...modules],
    endpoints: [...endpoints].map((e) => ix.node(e) as GNode),
    pages: [...pages].map(([name, conf]) => ({ name, conf })),
    tables: [...tables],
    tests: [...tests],
    commands,
    reqs: [...reqs],
    suspect,
    orphans: [...orphans],
    untested,
    unclaimed,
    schemaChanged,
    changes,
  };
}

function loc(n?: GNode): string {
  if (!n?.file) return '';
  const short = n.file.split('/').slice(-2).join('/');
  return `${short}${n.line ? ':' + n.line : ''}`;
}

// ---------------------------------------------------------------- impact

export function impact(g: Graph, inputs: string[]): string {
  const f = analyze(g, inputs);
  if (!f) return `no file matched: ${inputs.join(', ')}`;
  const out: string[] = [`impact: ${f.files.length} file(s)`];
  out.push(...f.files.slice(0, MAX).map((x) => `  ${x}`));
  if (f.dependents.length) {
    out.push(`used by   ${f.dependents.length} file(s)`);
    out.push(...f.dependents.slice(0, 5).map((d) => `  ${d}`));
  }
  if (f.modules.length) out.push(`modules   ${f.modules.join(', ')}`);
  out.push(`endpoints ${f.endpoints.length}`);
  out.push(...f.endpoints.slice(0, MAX).map((e) => `  ${e.name}  ${loc(e)}`));
  if (f.schemaChanged) out.push(`tables    all ${f.tables.length} (schema changed)`);
  else if (f.tables.length) out.push(`tables    ${f.tables.join(', ')}`);
  out.push(`pages     ${f.pages.length}`);
  out.push(...f.pages.slice(0, MAX).map((p) => `  ${p.name} (${p.conf})`));
  out.push(`tests     ${f.tests.length}`);
  out.push(...f.tests.slice(0, MAX).map((t) => `  ${t}`));
  if (f.reqs.length) out.push(`reqs      ${f.reqs.join(', ')}`);
  for (const c of f.changes.slice(0, 2)) {
    out.push(`spec      ${c.slug} ${c.done}/${c.total}${c.open.length ? `, open: ${c.open[0]}` : ''}`);
  }
  if (f.untested.length) {
    out.push(`untested  ${f.untested.length} endpoint(s) with no direct test`);
    out.push(...f.untested.slice(0, 4).map((e) => `  ${e}`));
  }
  if (f.unclaimed.length) {
    out.push(`unclaimed ${f.unclaimed.length} endpoint(s) no page calls`);
    out.push(...f.unclaimed.slice(0, 4).map((e) => `  ${e}`));
  }
  if (f.orphans.length) {
    out.push(`BROKEN    ${f.orphans.length} call site(s) match no endpoint`);
    out.push(...f.orphans.slice(0, 4).map((o) => `  ${o}`));
  }
  if (f.suspect.length) out.push(`suspect   ${f.suspect.join(', ')} — spec text changed, code not re-checked`);
  if (f.commands.length) {
    out.push('run');
    out.push(...f.commands.map((c) => `  ${c}`));
  }
  if (!f.endpoints.length && !f.pages.length && !f.tables.length) {
    out.push('no endpoint, page or table reaches this file — change looks isolated');
  }
  return out.join('\n');
}

// ---------------------------------------------------------------- context pack

/**
 * The one call an agent should make before editing: what this file is, what serves it,
 * which spec owns it, which tests to run, and what else moves. Hard budget so the
 * answer stays cheaper than reading the files.
 */
export function context(g: Graph, inputs: string[], budgetTokens = 350): string {
  const f = analyze(g, inputs);
  if (!f) return `no file matched: ${inputs.join(', ')}`;
  const limit = Math.max(400, budgetTokens * 4);
  const lines: string[] = [];
  let used = 0;
  const push = (s: string) => {
    if (used + s.length + 1 > limit) return false;
    lines.push(s);
    used += s.length + 1;
    return true;
  };

  push(`${f.label}  ${f.files[0]}`);
  if (f.dependents.length) {
    push(`used by   ${f.dependents.length} file(s)${f.dependents.length > 12 ? ' — wide blast radius' : ''}`);
  }
  if (f.pages.length) {
    const shown = f.pages.slice(0, 4).map((p) => p.name + (p.conf === 'inferred' ? '~' : ''));
    push(`page      ${shown.join(', ')}${f.pages.length > 4 ? ` +${f.pages.length - 4}` : ''}`);
  }
  if (f.modules.length) push(`module    ${f.modules.join(', ')}`);

  const eps = f.endpoints.slice(0, 4);
  eps.forEach((e, i) => push(`${i ? '          ' : 'endpoint  '}${e.name}  ${loc(e)}`));
  if (f.endpoints.length > 4) push(`          +${f.endpoints.length - 4} more`);

  if (f.tables.length) push(`tables    ${f.tables.slice(0, 8).join(', ')}${f.tables.length > 8 ? ' …' : ''}`);
  if (f.reqs.length) push(`reqs      ${f.reqs.slice(0, 5).join(', ')}`);
  if (f.orphans.length) push(`BROKEN    ${f.orphans.slice(0, 3).join('; ')}${f.orphans.length > 3 ? ` +${f.orphans.length - 3}` : ''} — matches no endpoint`);
  if (f.suspect.length) push(`suspect   ${f.suspect.slice(0, 3).join(', ')} — spec changed, code not re-checked`);

  const ch = f.changes[0];
  if (ch) {
    push(`spec      ${ch.slug} ${ch.done}/${ch.total}${ch.evidence ? '' : ' (partly names this file)'}`);
    if (ch.open.length) push(`open      ${ch.open.slice(0, 3).join('; ')}`);
  }

  if (f.tests.length) {
    push(`tests     ${f.tests.length}`);
    for (const t of f.tests.slice(0, 6)) if (!push(`          ${t}`)) break;
  } else {
    push('tests     none reference this — write one first (red before green)');
  }
  if (f.untested.length) push(`untested  ${f.untested.slice(0, 3).join(', ')}${f.untested.length > 3 ? ` +${f.untested.length - 3}` : ''}`);
  const others = f.endpoints.length ? f.pages.slice(1, 5).map((p) => p.name) : [];
  if (others.length) push(`also      ${others.join(', ')} share these endpoints`);
  if (f.commands.length) {
    push('run');
    for (const c of f.commands) if (!push('  ' + c)) break;
  }
  return lines.join('\n');
}

// ---------------------------------------------------------------- plan

/**
 * The next unit of work for a spec change, with only what is needed to start it:
 * the task, its files, its endpoints, its tests and the command to run them.
 * This is the cheapest entry an agent can make into spec-driven work.
 */
export function plan(g: Graph, slug: string, taskId?: string): string {
  const ix = new Index(g);
  const c = ix.node('change:' + slug);
  if (!c) return `no change "${slug}" — try: changes() for the list`;
  const tasks = ix.from(c.id, 'has_task').map((e) => ix.node(e.to)).filter(Boolean) as GNode[];
  if (!tasks.length) return `${slug}: no tasks`;
  const task = taskId
    ? tasks.find((t) => t.name.endsWith(' ' + taskId))
    : tasks.find((t) => t.meta?.done !== true);
  if (!task) return `${slug}: all ${tasks.length} task(s) done — call verify`;
  const short = task.name.split(' ').pop();

  const out: string[] = [`next  ${short}  ${String(task.meta?.title ?? '')}`];
  const files = ix.from(task.id, 'touches').map((e) => ix.node(e.to)?.name).filter((x): x is string => !!x);
  if (files.length) {
    out.push(`  files  ${files[0]}`);
    for (const f of files.slice(1, 6)) out.push(`         ${f}`);
  }
  const reqs = ix.from(task.id, 'satisfies').map((e) => ix.node(e.to)?.name).filter((x): x is string => !!x);
  if (reqs.length) out.push(`  reqs   ${reqs.join(', ')}`);

  if (files.length) {
    const f = analyze(g, files);
    if (f) {
      if (f.endpoints.length) out.push(`  api    ${f.endpoints.slice(0, 3).map((e) => e.name).join(', ')}`);
      if (f.tables.length) out.push(`  tables ${f.tables.slice(0, 6).join(', ')}`);
      if (f.tests.length) out.push(`  tests  ${f.tests.length}`);
      if (f.orphans.length) out.push(`  BROKEN ${f.orphans.slice(0, 3).join(', ')}`);
      if (f.suspect.length) out.push(`  suspect ${f.suspect.slice(0, 3).join(', ')}`);
      for (const cmd of f.commands) out.push(`  run    ${cmd}`);
    }
  }
  out.push(`  change ${slug} ${tasks.filter((t) => t.meta?.done === true).length}/${tasks.length}`);
  return out.join('\n');
}

// ---------------------------------------------------------------- evidence

export interface ReqEvidence {
  id: string;
  pass: number;
  fail: number;
  skip: number;
  tests: string[];
  unverified: boolean;
}

/**
 * Join the graph's requirement→test edges with a JUnit report. This is the only
 * claim that cannot be faked by an agent: a named test ran and reported a result.
 */
export function evidenceFacts(g: Graph, cases: Case[], only?: string[]): ReqEvidence[] {
  const ix = new Index(g);
  const filter = only?.length ? new Set(only) : null;
  const byReq = new Map<string, ReqEvidence>();

  const touch = (id: string): ReqEvidence => {
    let e = byReq.get(id);
    if (!e) {
      e = { id, pass: 0, fail: 0, skip: 0, tests: [], unverified: false };
      byReq.set(id, e);
    }
    return e;
  };

  const tests = ix.ofKind('test');
  for (const c of cases) {
    const ids = new Set(reqsInCase(c));
    const rel = (c.file ?? '').replace(/\\/g, '/');
    if (rel) {
      const node = tests.find((t) => t.name === rel || t.name.endsWith('/' + rel) || rel.endsWith('/' + t.name));
      if (node) {
        for (const e of ix.from(node.id)) {
          if (e.kind === 'verifies' || e.kind === 'asserts') ids.add(e.to.replace(/^req:/, ''));
        }
      }
    }
    for (const id of ids) {
      if (filter && !filter.has(id)) continue;
      const e = touch(id);
      e[c.status === 'pass' ? 'pass' : c.status === 'fail' ? 'fail' : 'skip']++;
      if (e.tests.length < 3) e.tests.push(`${c.name}${c.status === 'pass' ? '' : ` [${c.status}]`}`);
    }
  }

  // Implemented in code but never exercised by a test.
  for (const n of ix.ofKind('req')) {
    const id = n.name;
    if (filter && !filter.has(id)) continue;
    if (byReq.has(id)) continue;
    const implemented = ix.to(n.id, 'implements').length > 0;
    if (implemented) touch(id).unverified = true;
  }

  return [...byReq.values()].sort((a, b) => a.id.localeCompare(b.id));
}

export function evidence(g: Graph, cases: Case[], only?: string[]): string {
  const facts = evidenceFacts(g, cases, only);
  if (!facts.length) return 'evidence: no requirement is linked to a test yet — add `@req ID` to a test file';
  const verified = facts.filter((f) => f.pass > 0 && f.fail === 0);
  const out: string[] = [`evidence: ${verified.length}/${facts.length} requirement(s) verified, ${cases.length} test case(s)`];
  for (const f of facts) {
    const status = f.fail > 0 ? 'FAIL' : f.pass > 0 ? 'pass' : f.skip > 0 ? 'skip' : 'no test';
    const detail = f.tests.length ? f.tests[0] : 'implemented via @req, never exercised';
    out.push(`${f.id.padEnd(16)} ${status.padEnd(7)} ${String(f.pass + f.fail).padStart(2)} test(s)  ${detail}`);
  }
  return out.join('\n');
}

// ---------------------------------------------------------------- change view

/** Newest change records first, with task progress. Capped: an agent needs orientation, not 88 rows. */
export function listChanges(g: Graph, limit = 25): string {
  const ix = new Index(g);
  const all = ix.ofKind('change').sort((a, b) => b.name.localeCompare(a.name));
  if (!all.length) return 'no change records';
  const rows = all.slice(0, limit).map((c) => {
    const tasks = ix.from(c.id, 'has_task');
    const done = tasks.filter((e) => ix.node(e.to)?.meta?.done === true).length;
    return `${c.name} ${done}/${tasks.length}`;
  });
  if (all.length > limit) rows.push(`… ${all.length - limit} older`);
  return rows.join('\n');
}

export function changeView(g: Graph, slug: string): string {
  const ix = new Index(g);
  const cid = 'change:' + slug;
  const c = ix.node(cid);
  if (!c) {
    const near = ix.ofKind('change').filter((n) => n.name.includes(slug)).map((n) => n.name);
    return near.length ? `no change "${slug}". close: ${near.slice(0, 6).join(', ')}` : `no change "${slug}"`;
  }
  const tasks = ix.from(cid, 'has_task').map((e) => ix.node(e.to)).filter(Boolean) as GNode[];
  const files = new Set<string>();
  for (const t of tasks) {
    for (const e of ix.from(t.id, 'touches')) {
      const n = ix.node(e.to);
      if (n?.file) files.add(n.file);
    }
  }
  const out: string[] = [`change ${slug}`];
  out.push(`tasks ${tasks.filter((t) => t.meta?.done).length}/${tasks.length}`);
  const openTasks = tasks.filter((t) => !t.meta?.done);
  for (const t of openTasks.slice(0, 6)) out.push(`  open ${t.name.split(' ').pop()}  ${String(t.meta?.title ?? '')}`);
  for (const f of [...files].slice(0, MAX)) out.push(`  ${f}`);
  if (files.size) {
    out.push('');
    out.push(impact(g, [...files]));
  }
  return out.join('\n');
}

// ---------------------------------------------------------------- search

export function search(g: Graph, q: string, limit = 20): string {
  const needle = q.toLowerCase();
  const scored: { n: GNode; s: number }[] = [];
  const weight: Record<string, number> = {
    endpoint: 6, page: 5, module: 4, model: 4, table: 2, req: 5, test: 3, file: 1, task: 2, change: 3,
  };
  for (const n of g.nodes) {
    if (n.kind === 'file') continue;
    const name = n.name.toLowerCase();
    let s = 0;
    if (name === needle) s = 20;
    else if (name.startsWith(needle)) s = 12;
    else if (name.includes(needle)) s = 7;
    else if ((n.file ?? '').toLowerCase().includes(needle)) s = 3;
    else if (String(n.meta?.title ?? '').toLowerCase().includes(needle)) s = 4;
    if (!s) continue;
    scored.push({ n, s: s + (weight[n.kind] ?? 0) });
  }
  scored.sort((a, b) => b.s - a.s || a.n.name.localeCompare(b.n.name));
  if (!scored.length) return `nothing matched "${q}"`;
  const perKind = new Map<string, number>();
  const picked: GNode[] = [];
  for (const { n } of scored) {
    const seen = perKind.get(n.kind) ?? 0;
    if (seen >= 3) continue;
    perKind.set(n.kind, seen + 1);
    picked.push(n);
    if (picked.length >= limit) break;
  }
  return picked.map((n) => `${n.kind.padEnd(9)} ${n.name}${n.file ? '  ' + n.file : ''}`).join('\n');
}

// ---------------------------------------------------------------- doctor

/** One line of phase timings, shared by `index`, `perf` and `doctor`. */
export function phaseLine(p: { totalMs: number; phases: Record<string, number>; heapMb: number }, compact = false): string {
  const parts = Object.entries(p.phases).map(([k, v]) => `${k} ${Math.round(v)}ms`);
  return compact ? parts.join(' · ') : `${parts.join(' · ')} · heap ${p.heapMb}MB`;
}

export function doctor(g: Graph): string {
  const ix = new Index(g);
  const counts = new Map<string, number>();
  for (const n of g.nodes) counts.set(n.kind, (counts.get(n.kind) ?? 0) + 1);
  const order = ['module', 'endpoint', 'model', 'table', 'page', 'test', 'req', 'task', 'change', 'file'];
  const eps = ix.ofKind('endpoint');
  const noPage = eps.filter((e) => !ix.to(e.id, 'calls').some((x) => ix.node(x.from)?.kind === 'page'));
  const noTest = eps.filter((e) => !ix.to(e.id, 'hits').length && !ix.to(e.id, 'covers').length);
  const pageNoTest = ix.ofKind('page').filter((p) => !ix.to(p.id, 'exercises').length);
  const lines = [`files ${g.fileCount}`, order.filter((k) => counts.has(k)).map((k) => `${k} ${counts.get(k)}`).join('  ')];
  lines.push(`endpoints with no page: ${noPage.length}`);
  for (const e of noPage.slice(0, MAX)) lines.push(`  ${e.name}`);
  lines.push(`endpoints with no test: ${noTest.length}`);
  for (const e of noTest.slice(0, MAX)) lines.push(`  ${e.name}`);
  lines.push(`pages with no e2e: ${pageNoTest.length}`);
  for (const p of pageNoTest.slice(0, MAX)) lines.push(`  ${p.name}`);
  lines.push(...coverageLines(g));
  if (g.perf) {
    lines.push('performance');
    lines.push(`  last index   ${g.perf.totalMs}ms · ${phaseLine(g.perf, true)}`);
    lines.push(`  graph file   ${graphSize(g)} · heap ${g.perf.heapMb}MB`);
  }
  return lines.join('\n');
}

function graphSize(g: Graph): string {
  const bytes = JSON.stringify(g).length;
  return bytes > 1048576 ? `${(bytes / 1048576).toFixed(1)}MB` : `${Math.round(bytes / 1024)}KB`;
}

/** Recall, stated plainly: what the extractors found versus what the repo contains. */
export function coverageLines(g: Graph, full = false): string[] {
  const c = g.coverage;
  if (!c) return ['coverage  unknown — re-index with this version of weave'];
  const pct = (a: number, b: number) => (b ? Math.floor((a / b) * 1000) / 10 : 100);
  const out = ['coverage'];
  out.push(`  route files   ${c.routes.found}/${c.routes.files} produced endpoints (${pct(c.routes.found, c.routes.files)}%)`);
  out.push(
    `  client calls  ${c.calls.resolved}/${c.calls.total} resolvable matched an endpoint (${pct(c.calls.resolved, c.calls.total)}%)` +
      (c.calls.dynamic ? ` · ${c.calls.dynamic} built from a variable, unknowable` : ''),
  );
  out.push(`  routes        ${c.pages.mapped}/${c.pages.declared} <Route> entries mapped (${pct(c.pages.mapped, c.pages.declared)}%${c.pages.skipped ? `, ${c.pages.skipped} redirect` : ''})`);
  out.push(`  task files    ${c.specs.parsed}/${c.specs.files} parsed (${pct(c.specs.parsed, c.specs.files)}%)`);
  out.push(`  prisma        ${c.prisma.tables}/${c.prisma.models} models mapped to tables`);
  if (c.next && (c.next.files || c.next.found)) {
    out.push(`  next routes   ${c.next.found}/${c.next.files} route files produced endpoints (${pct(c.next.found, c.next.files)}%)`);
  }
  if (c.trpc && (c.trpc.files || c.trpc.found)) {
    out.push(`  tRPC routers  ${c.trpc.found}/${c.trpc.files} parsed (${pct(c.trpc.found, c.trpc.files)}%)`);
  }
  const silent = [...c.routes.silent, ...c.specs.silent, ...(c.next?.silent ?? []), ...(c.trpc?.silent ?? [])];
  if (silent.length) {
    out.push(`  GAPS          ${silent.length} file(s) look like contracts but produced nothing:`);
    for (const f of silent.slice(0, full ? 40 : 6)) out.push(`    ${f}`);
    out.push('    an answer that does not mention these may be incomplete — this is where a regex missed');
  }
  return out;
}
