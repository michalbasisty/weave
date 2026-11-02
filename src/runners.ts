import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

// Test selection is a solved problem in every ecosystem. We never reimplement it:
// we detect the runner and hand back the command it already understands.

interface Pkg {
  deps: Set<string>;
  scripts: Record<string, string>;
}

export interface RunPlan {
  commands: string[];
  files: string[];
}

export function planTests(root: string, testFiles: string[]): RunPlan {
  const cache = new Map<string, Pkg | null>();
  const groups = new Map<string, { dir: string; tool: string; files: string[] }>();

  const pkgAt = (dir: string): Pkg | null => {
    if (cache.has(dir)) return cache.get(dir)!;
    const p = join(root, dir, 'package.json');
    let out: Pkg | null = null;
    if (existsSync(p)) {
      try {
        const j = JSON.parse(readFileSync(p, 'utf8'));
        out = {
          deps: new Set(Object.keys({ ...j.dependencies, ...j.devDependencies })),
          scripts: j.scripts ?? {},
        };
      } catch {
        out = null;
      }
    }
    cache.set(dir, out);
    return out;
  };

  const nearestPkg = (rel: string): { dir: string; pkg: Pkg } | null => {
    let dir = dirname(rel).replace(/\\/g, '/');
    for (let i = 0; i < 8; i++) {
      const pkg = pkgAt(dir);
      if (pkg) return { dir, pkg };
      if (dir === '.' || dir === '' || dir === '/') return null;
      const up = dirname(dir).replace(/\\/g, '/');
      if (up === dir) return null;
      dir = up;
    }
    return null;
  };

  const ungrouped: string[] = [];

  for (const file of testFiles) {
    const found = nearestPkg(file);
    if (!found) {
      if (existsSync(join(root, 'vendor/bin/phpunit')) || existsSync(join(root, 'phpunit.xml'))) {
        add(groups, '.', 'phpunit', file);
      } else if (existsSync(join(root, 'go.mod'))) {
        add(groups, '.', 'go', file);
      } else ungrouped.push(file);
      continue;
    }
    const { dir, pkg } = found;
    const rel = dir === '.' ? file : file.slice(dir.length + 1);
    const tool = pickTool(root, dir, pkg, file);
    add(groups, dir, tool, rel);
  }

  const commands: string[] = [];
  for (const { dir, tool, files } of groups.values()) {
    const list = files.join(' ');
    let cmd: string;
    switch (tool) {
      case 'vitest':
        cmd = `npx vitest run ${list}`;
        break;
      case 'jest':
        cmd = `npx jest ${list}`;
        break;
      case 'playwright':
        cmd = `npx playwright test ${list}`;
        break;
      case 'phpunit':
        cmd = `vendor/bin/phpunit ${list}`;
        break;
      case 'go':
        cmd = `go test ./${uniqueDirs(files).join(' ./')}/...`;
        break;
      default:
        cmd = `# run ${list}`;
    }
    commands.push(dir === '.' || dir === '' ? cmd : `cd ${dir} && ${cmd}`);
  }
  return { commands: commands.slice(0, 4), files: ungrouped };
}

function add(groups: Map<string, { dir: string; tool: string; files: string[] }>, dir: string, tool: string, file: string) {
  const key = `${dir}|${tool}`;
  const g = groups.get(key);
  if (g) g.files.push(file);
  else groups.set(key, { dir, tool, files: [file] });
}

function pickTool(root: string, dir: string, pkg: Pkg, file: string): string {
  const has = (...names: string[]) => names.some((n) => existsSync(join(root, dir, n)));
  const e2e = /(^|\/)e2e\//.test(file) || /\.e2e\.[tj]s$/.test(file);
  if (pkg.deps.has('@playwright/test') && e2e) return 'playwright';
  // A jest config next to a vite config means the unit tests run under jest.
  if (has('jest.config.js', 'jest.config.ts', 'jest.config.mjs', 'jest.config.cjs')) return 'jest';
  if (has('vitest.config.ts', 'vitest.config.js', 'vitest.config.mts', 'vite.config.ts')) return 'vitest';
  if (pkg.deps.has('vitest')) return 'vitest';
  if (pkg.deps.has('jest')) return 'jest';
  if (pkg.deps.has('@playwright/test')) return 'playwright';
  if (pkg.scripts.test?.includes('vitest')) return 'vitest';
  if (pkg.scripts.test?.includes('jest')) return 'jest';
  return 'unknown';
}

function uniqueDirs(files: string[]): string[] {
  return [...new Set(files.map((f) => dirname(f).replace(/\\/g, '/').replace(/^\.$/, '')))].filter(Boolean);
}
