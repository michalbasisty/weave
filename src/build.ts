import { Builder, id, type Graph } from './model.ts';
import {
  classify,
  extractCalls,
  extractEndpoints,
  extractModels,
  extractPages,
  importMap,
  matchEndpoint,
  joinPosix,
  reachable,
  type Endpoint,
} from './extract.ts';
import { extractSpecs, resolveEvidence, dirIndex } from './specs.ts';
import { extractNextRoutes, extractTRPC } from './frameworks.ts';
import { extractOpenAPI } from './openapi.ts';
import { compile, loadPatterns, patternCalls, patternRoutes } from './patterns.ts';
import { Timer } from './perf.ts';
import { baseName, dirOf, eachMatch, walk, type SourceFile } from './scan.ts';

export function buildGraph(root: string): Graph {
  const timer = new Timer();
  const files = walk(root);
  timer.lap('walk');
  const known = new Set(files.map((f) => f.rel));
  const t = classify(files, root);
  const b = new Builder();

  for (const f of files) b.node({ id: id.file(f.rel), kind: 'file', name: f.rel, file: f.rel });

  // file -> file imports: the only way a change to a shared util can be traced back.
  for (const [from, targets] of importMap(files, known)) {
    for (const to of targets) b.edge(id.file(from), id.file(to), 'imports');
  }

  const endpoints = extractEndpoints(files, b);
  const nextStats = extractNextRoutes(files, b);
  const trpcStats = extractTRPC(files, b);
  const apiStats = extractOpenAPI(files, b);

  // Patterns from .weave/patterns.json: the escape hatch for a team's own wrapper.
  const pat = compile(loadPatterns(root));
  const customRoutes = patternRoutes(files, pat);
  const customEndpoints: Endpoint[] = [];
  for (const r of customRoutes) {
    const path = r.path.replace(/\/{2,}/g, '/');
    const eid = id.endpoint(r.method, path);
    b.node({ id: eid, kind: 'endpoint', name: `${r.method} ${path}`, file: r.file, line: r.line, meta: { module: 'api', desc: r.desc, framework: 'pattern' } });
    b.edge(eid, id.file(r.file), 'defined_in');
    customEndpoints.push({ method: r.method, path, file: r.file, line: r.line, module: 'api', id: eid });
  }

  const allEndpoints = [...endpoints, ...nextStats.endpoints, ...trpcStats.endpoints, ...apiStats.endpoints, ...customEndpoints];
  extractModels(t.prisma, b, files);
  const pageInfo = extractPages(t.web, b);
  const pages = pageInfo.pages;
  const calls = extractCalls(files.filter((f) => /\.(t|j)sx?$/.test(f.rel) && !/\.(test|spec)\./.test(f.rel)));
  const customCalls = patternCalls(files, pat);
  calls.push(...customCalls.map((c) => ({ ...c, dynamic: c.path.includes('${') })));
  timer.lap('extract');
  

  // page -> endpoint, through the component import graph
  const imp = importMap(t.web, known);
  const pageOfFile = new Map<string, string[]>();
  for (const p of pages) {
    if (!p.file) continue;
    const scope = reachable(p.file, imp);
    for (const c of calls) {
      if (!scope.has(c.file)) continue;
      const e = matchEndpoint(c, allEndpoints);
      if (!e) continue;
      const direct = c.file === p.file;
      b.edge(id.page(p.route), e.id, 'calls', direct && !c.dynamic ? 'exact' : 'inferred');
      const cur = pageOfFile.get(id.page(p.route)) ?? [];
      cur.push(e.id);
      pageOfFile.set(id.page(p.route), cur);
    }
  }

  // Any call site with no page in scope still belongs to its file. A call that
  // matches no endpoint is a broken contract: keep it, never drop it — unless its
  // path starts with a variable, which is unknowable rather than broken.
  let dynamicCalls = 0;
  const lineCache = new Map(files.map((f) => [f.rel, f.lines]));
  for (const c of calls) {
    if (c.path.trimStart().startsWith('${')) {
      dynamicCalls++;
      continue;
    }
    // A fetch of the app root (`/?t=…`) is a page load, not a contract.
    if (!c.path.split('?')[0].replace(/^\/+/, '')) continue;
    const e = matchEndpoint(c, allEndpoints);
    if (e) {
      b.edge(id.file(c.file), e.id, 'calls', c.dynamic ? 'inferred' : 'exact');
      continue;
    }
    // A call site may be marked as known-broken debt instead of blocking a merge.
    const lines = lineCache.get(c.file);
    if (lines && lines[c.line - 1]?.includes('weave-ignore')) continue;
    const callId = id.call(c.method, c.path);
    b.node({ id: callId, kind: 'call', name: `${c.method} ${c.path}`, file: c.file, line: c.line });
    b.edge(id.file(c.file), callId, 'calls_missing', c.dynamic ? 'inferred' : 'exact');
  }

  // A requirement named in the code is an exact edge — the one traceability
  // mechanism that survives renames, unlike a file path in a spec.
  for (const f of files) {
    for (const m of f.text.matchAll(/@req\s+((?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*)/g)) {
      b.edge(id.file(f.rel), id.req(m[1]), 'implements');
    }
  }

  extractTests(files, allEndpoints, pages, b);
  const { tasks } = extractSpecs(t.specs, b);
  timer.lap('specs');
  // task evidence -> files (memoised: the same paths repeat across hundreds of tasks,
  // and the directory fallback scans every known file)
  const evCache = new Map<string, string | null>();
  const dirs = dirIndex(known);
  for (const task of tasks) {
    const tid = id.task(`${task.change}/${task.id}`);
    for (const ev of task.evidence) {
      if (!evCache.has(ev)) evCache.set(ev, resolveEvidence(ev, known, dirs));
      const file = evCache.get(ev);
      if (!file) continue;
      if (file.endsWith('/')) {
        for (const k of known) if (k.startsWith(file)) b.edge(tid, id.file(k), 'touches', 'inferred');
      } else {
        b.edge(tid, id.file(file), 'touches');
      }
    }
  }

  timer.lap('evidence');
  const g = withCoverage(b.finish(root, files.length), files, allEndpoints, calls, dynamicCalls, pageInfo.declared, pageInfo.skipped.length, nextStats, trpcStats, apiStats, customRoutes.length, t.specs, tasks);
  timer.lap('coverage');
  g.perf = { totalMs: timer.total, phases: timer.phases, heapMb: Math.round(process.memoryUsage().heapUsed / 1048576) };
  return g;
}

/**
 * What the extractors actually covered. Precision is easy to claim; recall is the
 * number that decides whether an answer can be trusted, so it is measured and shown.
 */
function withCoverage(
  g: Graph,
  files: SourceFile[],
  endpoints: Endpoint[],
  calls: { file: string }[],
  dynamicCalls: number,
  declaredRoutes: number,
  skippedRoutes: number,
  nextStats: { files: number; found: number; silent: string[] },
  trpcStats: { files: number; found: number; silent: string[] },
  apiStats: { files: number; found: number; silent: string[] },
  patternRoutes: number,
  specFiles: SourceFile[],
  tasks: { change: string }[],
): Graph {
  const isTest = (rel: string) => /\.(test|spec)\.[mc]?[tj]sx?$/.test(rel) || /(^|\/)(__tests__|e2e)\//.test(rel);
  // Same rule as extraction: only code can declare a route. Documentation quotes
  // examples, and a .md file that "produced nothing" is not a coverage gap.
  const isCode = (rel: string) => /\.(t|j)sx?$/.test(rel);
  // indexOf first: running the router regex against every file costs more than the walk.
  const routeLike = files.filter((f) => isCode(f.rel) && !isTest(f.rel) && f.text.includes('router') && /router\.(get|post|put|patch|delete)\s*\(/.test(f.text));
  const withEndpoints = new Set(endpoints.map((e) => e.file));
  const silentRoutes = routeLike.filter((f) => !withEndpoints.has(f.rel)).map((f) => f.rel);

  const orphan = g.nodes.filter((n) => n.kind === 'call').length;

  // A file that calls an API in a way none of our patterns recognise is the one
  // gap we cannot see from the nodes: measure it from the source instead.
  // Patterns are call-shaped on purpose — a bare word like `got` matches prose.
  const CALLISH = /\bfetch\s*\(|\baxios\s*\.\s*(get|post|put|patch|delete|request)\s*\(|\buse(Query|Mutation|SWR|InfiniteQuery)\s*\(|\$fetch\s*\(|\btrpc\s*\.|\bky\s*\(|\bgot\s*\(/;
  const callFiles = new Set(calls.map((c) => c.file));
  const silentCalls = files
    .filter(
      (f) =>
        !isTest(f.rel) &&
        /\.(t|j)sx?$/.test(f.rel) &&
        !/\/(generated|__generated__|dist|build)\//.test(f.rel) &&
        CALLISH.test(f.text) &&
        !callFiles.has(f.rel),
    )
    .map((f) => f.rel);

  const taskFiles = specFiles.filter((f) => /\/tasks\.md$/.test(f.rel) && !/\/templates\//.test(f.rel));
  const parsed = new Set(tasks.map((t) => t.change));
  const silentSpecs = taskFiles
    .filter((f) => !parsed.has(f.rel.split('/').slice(-2)[0] ?? ''))
    .map((f) => f.rel);

  g.coverage = {
    routes: { files: routeLike.length, found: routeLike.length - silentRoutes.length, silent: silentRoutes.slice(0, 12) },
    calls: { total: calls.length - dynamicCalls, resolved: calls.length - dynamicCalls - orphan, dynamic: dynamicCalls, silent: silentCalls.slice(0, 12) },
    pages: { declared: declaredRoutes, mapped: declaredRoutes - skippedRoutes, skipped: skippedRoutes },
    specs: { files: taskFiles.length, parsed: parsed.size, silent: silentSpecs },
    prisma: { models: g.nodes.filter((n) => n.kind === 'model').length, tables: g.nodes.filter((n) => n.kind === 'table').length },
    next: { files: nextStats.files, found: nextStats.found, silent: nextStats.silent.slice(0, 12) },
    trpc: { files: trpcStats.files, found: trpcStats.found, silent: trpcStats.silent.slice(0, 12) },
    openapi: { files: apiStats.files, found: apiStats.found, silent: apiStats.silent.slice(0, 12) },
    patterns: patternRoutes,
  };
  return g;
}

function extractTests(
  files: ReturnType<typeof walk>,
  endpoints: Endpoint[],
  pages: { route: string; component: string; file?: string }[],
  b: Builder,
): void {
  const isTest = (rel: string) => /(^|\/)(__tests__|e2e|tests?)\//.test(rel) || /\.(test|spec)\.[tj]sx?$/.test(rel);
  for (const f of files) {
    if (!isTest(f.rel) || !/\.(test|spec)\.[tj]sx?$/.test(f.rel)) continue;
    const tid = id.test(f.rel);
    b.node({ id: tid, kind: 'test', name: f.rel, file: f.rel });

    for (const m of f.text.matchAll(/\bREQ-[A-Z0-9]+(?:-[A-Z0-9]+)*\b/g)) b.edge(tid, id.req(m[0]), 'asserts');
    for (const m of f.text.matchAll(/@req\s+((?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*)/g)) b.edge(tid, id.req(m[1]), 'verifies');

    const mod = testModule(f.rel);
    if (mod) b.edge(tid, id.module(mod), 'covers');
    const dn = dirOf(f.rel);
    if (/__tests__|tests?$|e2e/.test(dn)) b.edge(tid, id.file(f.rel), 'in');

    // literal API paths referenced by the test
    for (const m of f.text.matchAll(/['"`](\/api\/[^'"`$]+)['"`]/g)) {
      for (const method of ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']) {
        const e = matchEndpoint({ method, path: m[1], file: f.rel, line: 0, dynamic: false }, endpoints);
        if (e) b.edge(tid, e.id, 'hits');
      }
    }
    // client calls relative to the API base: admin.get('/machines?limit=1'), tech.get(`/tasks/${id}`)
    for (const m of f.text.matchAll(/\.(get|post|put|patch|delete)\s*\(\s*[`'"]([^`'"\n]+)[`'"]/g)) {
      const dynamic = m[2].includes('${');
      const path = dynamic ? m[2].replace(/\$\{[^}]*\}/g, '*') : m[2];
      const e = matchEndpoint({ method: m[1].toUpperCase(), path, file: f.rel, line: 0, dynamic }, endpoints);
      if (e) b.edge(tid, e.id, 'hits', dynamic ? 'inferred' : 'exact');
    }
    // e2e navigation: page.goto(BASE + '/tasks')
    for (const { m } of eachMatch(f.text, /(?:page\.goto|goto|visit)\s*\(([^)]*)\)/)) {
      const q = m[1].match(/['"`](\/[^'"`$]*)['"`]/);
      if (!q) continue;
      const p = matchPage(q[1], pages);
      if (p) b.edge(tid, id.page(p.route), 'exercises');
    }
  }
}

function testModule(rel: string): string {
  const m = rel.match(/modules\/([^/]+)\//);
  if (m) return m[1];
  const name = baseName(rel).replace(/\.(test|spec)\.[tj]sx?$/, '');
  const first = name.split('.')[0];
  return first && first !== 'index' ? first.replace(/[A-Z]/, (c) => c.toLowerCase()) : '';
}

function matchPage(path: string, pages: { route: string }[]) {
  const p = path.split('?')[0].replace(/\/$/, '') || '/';
  const want = p.split('/').filter(Boolean);
  let best: { route: string } | null = null;
  let bestLiteral = -1;
  for (const page of pages) {
    const have = page.route.split('/').filter(Boolean);
    if (have.length !== want.length) continue;
    let literal = 0;
    let ok = true;
    for (let i = 0; i < have.length; i++) {
      if (have[i].startsWith(':')) continue;
      if (have[i] !== want[i]) {
        ok = false;
        break;
      }
      literal++;
    }
    if (ok && literal > bestLiteral) {
      bestLiteral = literal;
      best = page;
    }
  }
  return best;
}

export { joinPosix };
