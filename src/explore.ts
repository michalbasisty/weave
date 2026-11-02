import type { Graph, GNode } from './model.ts';
import { Index, analyze } from './query.ts';

// A single self-contained HTML file: no server, no build, no network.
// Force-directed hairballs are unreadable past 50 nodes, so the graph view is a
// one-hop radial neighbourhood around whatever you select. The lists do the work.

export interface Project {
  name: string;
  root: string;
  builtAt: string;
  counts: { pages: number; endpoints: number; modules: number; tables: number; tests: number; changes: number; reqs: number };
  cov: {
    gaps: number;
    unmapped: number;
    calls: { resolved: number; total: number };
    routes: { found: number; files: number };
    specs: { parsed: number; files: number };
  };
  nodes: { i: string; k: string; n: string; f?: string; l?: number; x?: string; d?: string; t?: string; dn?: 1 }[];
  edges: [string, string, string, string][];
  flows: { p: string; e: string[]; t: string[] }[];
  modules: { m: string; e: string[]; t: string[] }[];
  changes: { c: string; done: number; total: number; open: string[]; s?: string; sum?: string; r: { id: string; title: string }[] }[];
  issues: { key: string; kind: string; title: string; items: string[] }[];
}

export function explorePayload(g: Graph): Omit<Project, 'name'> {
  const ix = new Index(g);
  const keep = new Set(['page', 'endpoint', 'module', 'table', 'test', 'req', 'change', 'task', 'call']);

  // Files appear only where they carry information a person can act on: the evidence
  // a spec task names. That is what answers "which change owns this file".
  const evidenceFiles = new Set<string>();
  for (const e of g.edges) {
    if (e.kind !== 'touches') continue;
    const n = ix.node(e.to);
    if (n?.kind === 'file' && n.file) evidenceFiles.add(n.id);
  }

  const nodes = g.nodes
    .filter((n) => keep.has(n.kind) || evidenceFiles.has(n.id))
    .map((n) => ({
      i: n.id,
      k: n.kind,
      n: n.name,
      f: n.file,
      l: n.line,
      x: n.kind === 'req' && n.meta?.suspect ? 'suspect' : undefined,
      d: typeof n.meta?.desc === 'string' && n.meta.desc ? n.meta.desc : undefined,
      t: typeof n.meta?.title === 'string' && n.meta.title ? n.meta.title : undefined,
      dn: n.kind === 'task' && n.meta?.done === true ? (1 as const) : undefined,
    }));
  const ids = new Set(nodes.map((n) => n.i));
  // Modules use Prisma models; the explorer speaks tables. Project the hop so a
  // module-to-table link survives into the payload (and the trace can show it).
  const modelTable = new Map<string, string>();
  for (const e of g.edges) if (e.kind === 'maps_to') modelTable.set(e.from, e.to);
  const tablesOf = (moduleId: string): string[] => {
    const seen = new Set<string>();
    for (const e of ix.from(moduleId, 'uses')) {
      const t = modelTable.get(e.to);
      if (t && ids.has(t)) seen.add(t);
    }
    return [...seen];
  };
  const wanted = new Set([
    'calls', 'hits', 'exercises', 'asserts', 'verifies', 'implements', 'satisfies',
    'has_task', 'in_module', 'uses', 'calls_missing', 'touches', 'declared_in',
  ]);
  const edges = g.edges
    .filter((e) => wanted.has(e.kind) && ids.has(e.from) && ids.has(e.to))
    .map((e) => [e.from, e.to, e.kind, e.conf] as [string, string, string, string]);

  const pages = ix.ofKind('page');
  const flows = pages
    .map((p) => {
      const eps = ix.from(p.id, 'calls').map((e) => e.to).filter((id) => ids.has(id));
      const tables = new Set<string>();
      for (const ep of eps) {
        const mod = ix.node(ep)?.meta?.module;
        if (typeof mod !== 'string') continue;
        for (const t of tablesOf('module:' + mod)) tables.add(t);
      }
      return { p: p.id, e: [...new Set(eps)], t: [...tables] };
    })
    .filter((f) => f.e.length);

  const modules = ix
    .ofKind('module')
    .map((m) => ({
      m: m.id,
      e: ix.to(m.id, 'in_module').map((e) => e.from).filter((id) => ids.has(id)),
      t: tablesOf(m.id),
    }))
    .filter((m) => m.e.length);
  for (const m of modules) for (const t of m.t) edges.push([m.m, t, 'uses', 'exact']);

  const changes = ix
    .ofKind('change')
    .map((c) => {
      const tasks = ix.from(c.id, 'has_task').map((e) => ix.node(e.to)).filter(Boolean) as GNode[];
      const reqs = new Map<string, string>();
      for (const t of tasks) {
        for (const e of ix.from(t.id, 'satisfies')) {
          const r = ix.node(e.to);
          if (r) reqs.set(r.name, String(r.meta?.title ?? ''));
        }
      }
      return {
        c: c.id,
        done: tasks.filter((t) => t.meta?.done === true).length,
        total: tasks.length,
        open: tasks.filter((t) => t.meta?.done !== true).slice(0, 4).map((t) => String(t.meta?.title ?? '')),
        sum: typeof c.meta?.desc === 'string' ? c.meta.desc : undefined,
        r: [...reqs].map(([id, title]) => ({ id, title })),
      };
    })
    .filter((c) => c.total)
    .sort((a, b) => b.c.localeCompare(a.c));

  const issues: Project['issues'] = [];
  const broken = ix.ofKind('call').filter((c) => ix.to(c.id).some((e) => e.kind === 'calls_missing'));
  if (broken.length) issues.push({ key: 'broken_calls', kind: 'error', title: 'Call sites with no matching endpoint', items: broken.map((b) => b.id) });
  const noTest = ix.ofKind('endpoint').filter((e) => !ix.to(e.id, 'hits').length && !ix.to(e.id, 'covers').length);
  if (noTest.length) issues.push({ key: 'untested_endpoints', kind: 'warn', title: `${noTest.length} endpoint${noTest.length === 1 ? '' : 's'} with no direct test`, items: noTest.map((e) => e.id) });
  const noE2e = ix.ofKind('page').filter((p) => !ix.to(p.id, 'exercises').length);
  if (noE2e.length) issues.push({ key: 'pages_without_e2e', kind: 'warn', title: `${noE2e.length} page${noE2e.length === 1 ? '' : 's'} with no e2e`, items: noE2e.map((p) => p.id) });
  const suspect = ix.ofKind('req').filter((r) => r.meta?.suspect === true);
  if (suspect.length) issues.push({ key: 'suspect_reqs', kind: 'warn', title: 'Requirements changed after the code was written', items: suspect.map((r) => r.id) });

  return {
    root: g.root,
    builtAt: g.builtAt,
    cov: coverageOf(g),
    counts: {
      pages: pages.length,
      endpoints: ix.ofKind('endpoint').length,
      modules: ix.ofKind('module').length,
      tables: ix.ofKind('table').length,
      tests: ix.ofKind('test').length,
      changes: changes.length,
      reqs: ix.ofKind('req').length,
    },
    nodes,
    edges,
    flows,
    modules,
    changes,
    issues,
  };
}

/** The one number a reader should see before trusting anything else on the page. */
function coverageOf(g: Graph): Project['cov'] {
  const c = g.coverage;
  if (!c) return { gaps: 0, unmapped: 0, calls: { resolved: 0, total: 0 }, routes: { found: 0, files: 0 }, specs: { parsed: 0, files: 0 } };
  const gaps = c.routes.silent.length + c.specs.silent.length + (c.next?.silent.length ?? 0) + (c.trpc?.silent.length ?? 0);
  return {
    gaps,
    unmapped: c.calls.silent?.length ?? 0,
    calls: { resolved: c.calls.resolved, total: c.calls.total },
    routes: { found: c.routes.found, files: c.routes.files },
    specs: { parsed: c.specs.parsed, files: c.specs.files },
  };
}

/** Blast radius text for a change record, precomputed so the page needs no logic. */
export function changeSummary(g: Graph, slug: string): string {
  const ix = new Index(g);
  const c = ix.node('change:' + slug);
  if (!c) return '';
  const files = new Set<string>();
  for (const e of ix.from(c.id, 'has_task')) {
    for (const t of ix.from(e.to, 'touches')) {
      const n = ix.node(t.to);
      if (n?.file) files.add(n.file);
    }
  }
  if (!files.size) return '';
  const f = analyze(g, [...files]);
  if (!f) return '';
  return `${f.endpoints.length} endpoint(s), ${f.pages.length} page(s), ${f.tables.length} table(s), ${f.tests.length} test(s)`;
}

