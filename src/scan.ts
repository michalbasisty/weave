import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, posix, relative } from 'node:path';

const SKIP = new Set([
  'node_modules', '.git', 'dist', 'build', 'coverage', '.next', '.output', 'out',
  'vendor', '.turbo', '.weave', '.cache', 'tmp', '.nyc_output', 'playwright-report',
  'test-results', '.gradle', 'Pods', 'DerivedData',
]);

const CODE = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.vue', '.svelte', '.prisma', '.md', '.json']);
const MAX_BYTES = 512 * 1024;

export type SourceFile = { rel: string; abs: string; text: string; lines: string[] };

export function walk(root: string): SourceFile[] {
  const out: SourceFile[] = [];
  const visit = (dir: string) => {
    let entries: string[];
    try {
      entries = readdirSync(dir);
    } catch {
      return;
    }
    for (const name of entries) {
      if (SKIP.has(name) || name.startsWith('.')) continue;
      const abs = join(dir, name);
      let st;
      try {
        st = statSync(abs);
      } catch {
        continue;
      }
      if (st.isDirectory()) visit(abs);
      else if (st.size <= MAX_BYTES && CODE.has(ext(name))) {
        let text: string;
        try {
          text = readFileSync(abs, 'utf8');
        } catch {
          continue;
        }
        out.push({ rel: toPosix(relative(root, abs)), abs, text, lines: text.split(/\r?\n/) });
      }
    }
  };
  visit(root);
  return out;
}

/** Newest modification time under root, without reading any file. */
export function newestMtime(root: string): number {
  let max = 0;
  const visit = (dir: string) => {
    let entries: string[];
    try {
      entries = readdirSync(dir);
    } catch {
      return;
    }
    for (const name of entries) {
      if (SKIP.has(name) || name.startsWith('.')) continue;
      const abs = join(dir, name);
      let st;
      try {
        st = statSync(abs);
      } catch {
        continue;
      }
      if (st.isDirectory()) visit(abs);
      else if (st.mtimeMs > max) max = st.mtimeMs;
    }
  };
  visit(root);
  return max;
}

export function ext(p: string): string {
  const i = p.lastIndexOf('.');
  return i < 0 ? '' : p.slice(i).toLowerCase();
}

export function toPosix(p: string): string {
  return p.split('\\').join('/');
}

export function stripExt(p: string): string {
  const i = p.lastIndexOf('.');
  return i < 0 ? p : p.slice(0, i);
}

export function dirOf(p: string): string {
  const i = p.lastIndexOf('/');
  return i < 0 ? '' : p.slice(0, i);
}

export function baseName(p: string): string {
  const i = p.lastIndexOf('/');
  return i < 0 ? p : p.slice(i + 1);
}

/** Lines matching `re`, with 1-based line numbers. */
export function eachMatch(text: string, re: RegExp): { m: RegExpMatchArray; line: number }[] {
  const lines = text.split(/\r?\n/);
  const hits: { m: RegExpMatchArray; line: number }[] = [];
  for (let i = 0; i < lines.length; i++) {
    const m = lines[i].match(re);
    if (m) hits.push({ m, line: i + 1 });
  }
  return hits;
}

export const rel = posix.join;
