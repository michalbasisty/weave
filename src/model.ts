export type Kind =
  | 'module'
  | 'endpoint'
  | 'call'
  | 'model'
  | 'table'
  | 'page'
  | 'component'
  | 'test'
  | 'req'
  | 'task'
  | 'change'
  | 'file';

export type Conf = 'exact' | 'inferred';

export interface GNode {
  id: string;
  kind: Kind;
  name: string;
  file?: string;
  line?: number;
  meta?: Record<string, unknown>;
}

export interface GEdge {
  from: string;
  to: string;
  kind: string;
  conf: Conf;
}

export const GRAPH_VERSION = 3;

/** How much of the repo the extractors actually saw. Recall, not precision. */
export interface Coverage {
  routes: { files: number; found: number; silent: string[] };
  calls: { total: number; resolved: number; dynamic?: number; silent?: string[] };
  pages: { declared: number; mapped: number; skipped: number };
  specs: { files: number; parsed: number; silent: string[] };
  prisma: { models: number; tables: number };
  next?: { files: number; found: number; silent: string[] };
  trpc?: { files: number; found: number; silent: string[] };
  openapi?: { files: number; found: number; silent: string[] };
  patterns?: number;
}

export interface Graph {
  version: number;
  root: string;
  builtAt: string;
  fileCount: number;
  nodes: GNode[];
  edges: GEdge[];
  /** Endpoint names present in the previous index: lets `check` prove what a change removed. */
  prevEndpoints?: string[];
  coverage?: Coverage;
  /** Phase timings of the run that produced this index. */
  perf?: { totalMs: number; phases: Record<string, number>; heapMb: number };
}

export interface Targets {
  backend: string[];
  web: string[];
  prisma: string[];
}

export const id = {
  module: (n: string) => `module:${n}`,
  endpoint: (method: string, path: string) => `endpoint:${method.toUpperCase()} ${path}`,
  call: (method: string, path: string) => `call:${method.toUpperCase()} ${path}`,
  model: (n: string) => `model:${n}`,
  table: (n: string) => `table:${n}`,
  page: (route: string) => `page:${route}`,
  test: (file: string) => `test:${file}`,
  req: (n: string) => `req:${n}`,
  task: (n: string) => `task:${n}`,
  change: (n: string) => `change:${n}`,
  file: (n: string) => `file:${n}`,
};

export class Builder {
  nodes = new Map<string, GNode>();
  edges: GEdge[] = [];
  private seen = new Set<string>();

  node(n: GNode): string {
    const prev = this.nodes.get(n.id);
    if (!prev) this.nodes.set(n.id, n);
    else if (!prev.file && n.file) this.nodes.set(n.id, { ...prev, ...n });
    return n.id;
  }

  edge(from: string, to: string, kind: string, conf: Conf = 'exact'): void {
    if (from === to) return;
    const key = `${from}|${to}|${kind}`;
    if (this.seen.has(key)) return;
    this.seen.add(key);
    this.edges.push({ from, to, kind, conf });
  }

  finish(root: string, fileCount: number): Graph {
    return {
      version: GRAPH_VERSION,
      root,
      builtAt: new Date().toISOString(),
      fileCount,
      nodes: [...this.nodes.values()],
      edges: this.edges,
    };
  }
}