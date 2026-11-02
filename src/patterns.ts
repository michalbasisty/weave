import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { SourceFile } from './scan.ts';

// Per-repo extraction patterns. Every mid-size team has a wrapper the built-in
// regexes do not know (`defineRoute()`, a generated client, `request()`); this is
// the escape hatch that costs five lines of config instead of a rewrite.

export interface PatternRule {
  /** Regex source; capture group 1 is the path. */
  match: string;
  /** HTTP method for a route rule, or the method a call uses. Defaults to ANY. */
  method?: string;
}

export interface Patterns {
  routes: PatternRule[];
  calls: PatternRule[];
}

export interface Compiled {
  routes: { re: RegExp; method: string }[];
  calls: { re: RegExp; method: string }[];
}

const FILE = join('.weave', 'patterns.json');

export function loadPatterns(root: string): Patterns {
  const p = join(root, FILE);
  if (!existsSync(p)) return { routes: [], calls: [] };
  try {
    const j = JSON.parse(readFileSync(p, 'utf8')) as Partial<Patterns>;
    return {
      routes: Array.isArray(j.routes) ? j.routes.filter((r) => r && typeof r.match === 'string') : [],
      calls: Array.isArray(j.calls) ? j.calls.filter((r) => r && typeof r.match === 'string') : [],
    };
  } catch {
    return { routes: [], calls: [] };
  }
}

export function compile(p: Patterns): Compiled {
  const build = (rules: PatternRule[]) =>
    rules.flatMap((r) => {
      try {
        return [{ re: new RegExp(r.match, 'g'), method: (r.method ?? 'ANY').toUpperCase() }];
      } catch {
        return []; // a broken pattern must not break the index
      }
    });
  return { routes: build(p.routes), calls: build(p.calls) };
}

const isCode = (rel: string) => /\.(t|j)sx?$/.test(rel);

export function patternRoutes(files: SourceFile[], c: Compiled): { path: string; method: string; file: string; line: number; desc: string }[] {
  const out: { path: string; method: string; file: string; line: number; desc: string }[] = [];
  for (const f of files) {
    if (!isCode(f.rel)) continue;
    for (const { re, method } of c.routes) {
      re.lastIndex = 0;
      for (const m of f.text.matchAll(re)) {
        const raw = m[1];
        if (!raw) continue;
        const path = raw.startsWith('/') ? raw : '/' + raw;
        const line = f.text.slice(0, m.index ?? 0).split('\n').length;
        out.push({ path, method, file: f.rel, line, desc: 'custom pattern' });
      }
    }
  }
  return out;
}

export function patternCalls(files: SourceFile[], c: Compiled): { path: string; method: string; file: string; line: number }[] {
  const out: { path: string; method: string; file: string; line: number }[] = [];
  for (const f of files) {
    if (!isCode(f.rel)) continue;
    for (const { re, method } of c.calls) {
      re.lastIndex = 0;
      for (const m of f.text.matchAll(re)) {
        const raw = m[1];
        if (!raw) continue;
        const line = f.text.slice(0, m.index ?? 0).split('\n').length;
        out.push({ path: raw, method, file: f.rel, line });
      }
    }
  }
  return out;
}
