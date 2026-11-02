import { buildGraph } from './build.ts';
import * as registry from './registry.ts';
import { GRAPH_VERSION, type Graph } from './model.ts';
import { newestMtime } from './scan.ts';
import { appendPerf, perfReport, readPerf } from './perf.ts';
import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

export function indexPath(root: string): string {
  return join(root, '.weave', 'graph.json');
}

export function loadGraph(root: string): Graph {
  const p = indexPath(root);
  if (!existsSync(p)) throw new Error(`no index at ${p} — run: weave index`);
  const g = JSON.parse(readFileSync(p, 'utf8')) as Graph;
  // An index written by an older weave lacks fields the current code reads.
  if (g.version !== GRAPH_VERSION) throw new Error(`index format ${g.version} is stale — rebuilding`);
  return g;
}

export function saveGraph(root: string, g: Graph): string {
  const dir = join(root, '.weave');
  mkdirSync(dir, { recursive: true });
  const p = indexPath(root);
  writeFileSync(p, JSON.stringify(g));
  return p;
}

export function isFresh(root: string, codeDir?: string): boolean {
  const idx = indexPath(root);
  if (!existsSync(idx)) return false;
  const stamp = statSync(idx).mtimeMs;
  // A changed extractor invalidates every index it produced, not just changed sources.
  if (codeDir && newestMtime(codeDir) > stamp) return false;
  return newestMtime(root) <= stamp;
}

export function reindex(root: string): Graph {
  const prev = loadIfPresent(root);
  const g = buildGraph(root);
  if (prev) {
    markSuspect(g, prev);
    g.prevEndpoints = prev.nodes.filter((n) => n.kind === 'endpoint').map((n) => n.name);
  }
  saveGraph(root, g);
  noteCounts(root, g);
  if (g.perf) {
    appendPerf(root, {
      at: g.builtAt,
      files: g.fileCount,
      nodes: g.nodes.length,
      edges: g.edges.length,
      totalMs: g.perf.totalMs,
      phases: g.perf.phases,
      heapMb: g.perf.heapMb,
    });
  }
  return g;
}

/** Keep the project list's numbers current, even when the index was already warm. */
export function noteCounts(root: string, g: Graph): void {
  const count = (k: string) => g.nodes.reduce((n, x) => n + (x.kind === k ? 1 : 0), 0);
  registry.noteIndex(root, { files: g.fileCount, endpoints: count('endpoint'), changes: count('change') });
}

function loadIfPresent(root: string): Graph | null {
  const p = indexPath(root);
  if (!existsSync(p)) return null;
  try {
    const g = JSON.parse(readFileSync(p, 'utf8')) as Graph;
    return g.version === GRAPH_VERSION ? g : null;
  } catch {
    return null;
  }
}

/**
 * A requirement whose text changed since the last index makes every file and test
 * linked to it "suspect" — the doorstop/sphinx-needs idea, minus the manual review.
 */
function markSuspect(g: Graph, prev: Graph): void {
  const before = new Map<string, string>();
  for (const n of prev.nodes) {
    if (n.kind === 'req' && typeof n.meta?.hash === 'string') before.set(n.id, n.meta.hash as string);
  }
  for (const n of g.nodes) {
    if (n.kind !== 'req') continue;
    const old = before.get(n.id);
    const now = n.meta?.hash;
    if (old && typeof now === 'string' && old !== now) n.meta = { ...n.meta, suspect: true };
  }
}
