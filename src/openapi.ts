import { Builder, id } from './model.ts';
import type { Endpoint } from './extract.ts';
import type { SourceFile } from './scan.ts';

// OpenAPI is the one contract that exists regardless of language, so it is the
// cheapest way out of "we only understand Express + Next + tRPC".

const SPEC = /(^|\/)(openapi|swagger)(\.\d+)?\.json$/i;
const METHODS = ['get', 'post', 'put', 'patch', 'delete', 'head', 'options'] as const;

export interface OpenAPIStats {
  files: number;
  found: number;
  silent: string[];
  endpoints: Endpoint[];
}

interface Operation {
  summary?: string;
  description?: string;
  tags?: string[];
  operationId?: string;
}

export function extractOpenAPI(files: SourceFile[], b: Builder): OpenAPIStats {
  const endpoints: Endpoint[] = [];
  const silent: string[] = [];
  let specs = 0;
  let found = 0;

  for (const f of files) {
    if (!SPEC.test(f.rel) || f.text.length > 4 * 1024 * 1024) continue;
    specs++;
    let spec: { paths?: Record<string, Record<string, Operation>>; info?: { title?: string } };
    try {
      spec = JSON.parse(f.text);
    } catch {
      silent.push(f.rel);
      continue;
    }
    const paths = spec.paths ?? {};
    let here = 0;
    for (const [rawPath, item] of Object.entries(paths)) {
      if (!item || typeof item !== 'object') continue;
      const path = rawPath.replace(/\{([^}]+)\}/g, ':$1').replace(/\/$/, '') || '/';
      for (const method of METHODS) {
        const op = item[method];
        if (!op) continue;
        const tag = op.tags?.[0] ?? 'api';
        const desc = (op.summary || op.description || '').trim().slice(0, 220);
        const eid = id.endpoint(method.toUpperCase(), path);
        b.node({ id: eid, kind: 'endpoint', name: `${method.toUpperCase()} ${path}`, file: f.rel, meta: { module: tag, desc, framework: 'openapi' } });
        b.edge(eid, id.file(f.rel), 'defined_in');
        b.node({ id: id.module(tag), kind: 'module', name: tag, file: f.rel });
        b.edge(eid, id.module(tag), 'in_module');
        endpoints.push({ method: method.toUpperCase(), path, file: f.rel, line: 0, module: tag, id: eid });
        here++;
      }
    }
    if (here) found++;
    else silent.push(f.rel);
  }

  return { files: specs, found, silent, endpoints };
}
