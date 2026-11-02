#!/usr/bin/env node
import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { execSync } from 'node:child_process';
import { dirname, basename, join, resolve } from 'node:path';
import { isFresh, indexPath, loadGraph, noteCounts, reindex } from './store.ts';
import { newestMtime } from './scan.ts';
import { findJUnitFile, parseJUnit, type Case } from './junit.ts';
import { renderExplore } from './render.ts';
import * as registry from './registry.ts';
import { appendPerf, perfReport, readPerf } from './perf.ts';
import { analyze, changeView, context, doctor, evidence, evidenceFacts, impact, listChanges, phaseLine, plan, search } from './query.ts';
import { serveMcp } from './mcp.ts';
import type { Graph } from './model.ts';

function arg(name: string, fallback = ''): string {
  const i = process.argv.indexOf('--' + name);
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
}

const VALUE_FLAGS = new Set(['--root', '--project', '--limit', '--budget', '--out']);

/** Split argv once: the first bare token is the command, the rest are its arguments. */
function parseArgs(argv: string[]): { cmd: string; pos: string[] } {
  let cmd = '';
  const pos: string[] = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (VALUE_FLAGS.has(a) && !cmd) {
      i++;
      continue;
    }
    if (VALUE_FLAGS.has(a)) {
      i++;
      continue;
    }
    if (a.startsWith('-')) {
      if (!cmd && (a === '--help' || a === '-h')) cmd = 'help';
      continue;
    }
    if (!cmd) cmd = a;
    else pos.push(a);
  }
  return { cmd, pos };
}

const { cmd, pos } = parseArgs(process.argv.slice(2));

function positional(): string[] {
  return pos;
}

const root = pickRoot();
if (isProject(root)) registry.touch(root);

/** --root wins, then --project, then the nearest project above the cwd. */
function pickRoot(): string {
  const explicit = arg('root', '');
  if (explicit) return resolve(explicit);
  const name = arg('project', '');
  if (name) {
    const hit = registry.resolve(name);
    if (hit) return hit.root;
    const near = registry.candidates(name);
    console.error(
      near.length > 1
        ? `"${name}" matches several projects: ${near.map((e) => e.name).join(', ')}`
        : `unknown project "${name}". Known:\n${registry.table()}`,
    );
    process.exit(2);
  }
  return findRoot(process.cwd());
}

/** Find the project root by walking up: run `weave` anywhere inside a repo. */
function findRoot(start: string): string {
  const strong = ['.git', '.weave'];
  const weak = ['package.json', 'go.mod', 'composer.json', 'pyproject.toml', 'Cargo.toml', 'specs'];
  let dir = resolve(start);
  let weakHit = '';
  for (let i = 0; i < 16; i++) {
    // The nearest .git/.weave wins: a wrapper repo above must not swallow its checkouts.
    if (strong.some((m) => existsSync(join(dir, m)))) return dir;
    if (!weakHit && weak.some((m) => existsSync(join(dir, m)))) weakHit = dir;
    const up = dirname(dir);
    if (up === dir) break;
    dir = up;
  }
  return weakHit || resolve(start);
}

/** Read the last JUnit report, if the project wrote one. */
function loadCases(target = root): Case[] {
  const p = findJUnitFile(target);
  if (!p) return [];
  try {
    return parseJUnit(readFileSync(p, 'utf8'));
  } catch {
    return [];
  }
}

/** Read the index, rebuilding it when a source file or weave itself changed. */
function graphFor(): Graph {
  if (isFresh(root, import.meta.dirname)) {
    const g = loadGraph(root);
    noteCounts(root, g);
    return g;
  }
  return reindex(root);
}

switch (cmd) {
  case 'index': {
    const t0 = Date.now();
    const g = reindex(root);
    const counts = new Map<string, number>();
    for (const n of g.nodes) counts.set(n.kind, (counts.get(n.kind) ?? 0) + 1);
    const parts = [...counts.entries()].filter(([k]) => k !== 'file').map(([k, v]) => `${k} ${v}`);
    console.log(`indexed ${g.fileCount} files, ${g.nodes.length} nodes, ${g.edges.length} edges in ${Date.now() - t0}ms`);
    console.log(parts.join('  '));
    if (g.perf) console.log(phaseLine(g.perf));
    console.log(indexPath(root));
    break;
  }
  case 'perf': {
    const runs = Number(arg('runs', '0'));
    if (runs > 0) {
      for (let i = 1; i <= runs; i++) {
        const g = reindex(root);
        console.log(`run ${i}: ${g.perf?.totalMs ?? 0}ms  ${g.perf ? phaseLine(g.perf, true) : ''}`);
      }
    }
    console.log(perfReport(readPerf(root)));
    break;
  }
  case 'context':
  case 'brief': {
    const files = positional();
    if (!files.length) {
      console.error('usage: context <file...> [--root <dir>] [--budget <tokens>]');
      process.exit(2);
    }
    console.log(context(graphFor(), files, Number(arg('budget', '350'))));
    break;
  }
  case 'impact': {
    const files = positional();
    if (!files.length) {
      console.error('usage: impact <file...> [--root <dir>]');
      process.exit(2);
    }
    console.log(impact(graphFor(), files));
    break;
  }
  case 'change': {
    const [slug = ''] = positional();
    const g = graphFor();
    console.log(slug ? changeView(g, slug) : listChanges(g));
    break;
  }
  case 'search': {
    const q = positional().join(' ');
    if (!q) {
      console.error('usage: search <query> [--root <dir>]');
      process.exit(2);
    }
    console.log(search(graphFor(), q, Number(arg('limit', '20'))));
    break;
  }
  case 'doctor':
  case 'status': {
    console.log(doctor(graphFor()));
    break;
  }
  case 'check': {
    process.exitCode = checkCmd(root, positional());
    break;
  }
  case 'evidence': {
    const cases = loadCases();
    if (!cases.length) {
      console.log(`no JUnit report found under ${root}\n  run your tests with a JUnit reporter, then retry (see: weave init)`);
      break;
    }
    console.log(evidence(graphFor(), cases, positional()));
    break;
  }
  case 'projects':
  case 'list': {
    console.log(registry.table());
    break;
  }
  case 'forget': {
    const [name = ''] = positional();
    if (!name) {
      console.error('usage: weave forget <name>');
      process.exit(2);
    }
    console.log(registry.forget(name) ? `forgot ${name}` : `no project named ${name}`);
    break;
  }
  case 'plan': {    const [slug = '', task = ''] = positional();
    if (!slug) {
      console.error('usage: plan <change-slug> [T3]');
      process.exit(2);
    }
    console.log(plan(graphFor(), slug, task));
    break;
  }
  case 'explore': {
    const all = process.argv.includes('--all');
    const out = resolve(arg('out', all ? join(registry.homeDir(), 'explore.html') : join(root, '.weave', 'explore.html')));
    mkdirSync(dirname(out), { recursive: true });
    const picked: { name: string; graph: Graph }[] = [];
    if (all) {
      const entries = registry.load().sort((a, b) => b.lastUsed.localeCompare(a.lastUsed));
      for (const e of entries) {
        try {
          let graph: Graph;
          try {
            graph = isFresh(e.root, import.meta.dirname) ? loadGraph(e.root) : reindex(e.root);
          } catch {
            graph = reindex(e.root); // stale format or unreadable index
          }
          picked.push({ name: e.name, graph });
        } catch {
          // unreadable checkout: skip it
        }
      }
      if (!picked.length) {
        console.error('no indexed projects yet — run `weave` inside a repo first');
        break;
      }
    } else {
      picked.push({ name: basename(root), graph: graphFor() });
    }
    const html = renderExplore(picked);
    writeFileSync(out, html);
    console.log(`explore: ${out} (${Math.round(html.length / 1024)} KB, ${picked.length} project(s))`);
    if (!process.argv.includes('--no-open')) {
      try {
        execSync(process.platform === 'win32' ? `start "" "${out}"` : process.platform === 'darwin' ? `open "${out}"` : `xdg-open "${out}"`, { stdio: 'ignore' });
      } catch {
        // no browser available: the path is printed above
      }
    }
    break;
  }
  case 'init': {
    initCmd(root);
    break;
  }
  case 'mcp': {
    const multi = process.argv.includes('--all');
    const graphs = new Map<string, Graph>();
    const lastCheck = new Map<string, number>();

    const rootFor = (project?: string): string => {
      if (!project) return root;
      const hit = registry.resolve(project);
      if (!hit) {
        const known = registry.load().map((e) => e.name).join(', ') || 'none';
        throw new Error(`unknown project "${project}". Known: ${known}`);
      }
      return hit.root;
    };

    const get = (project?: string): Graph => {
      const r = rootFor(project);
      const cached = graphs.get(r);
      const checked = lastCheck.get(r) ?? 0;
      // Cheap stat sweep first: 57 ms beats 1.1 s, and a burst of calls skips it.
      if (cached && Date.now() - checked < 1500) return cached;
      lastCheck.set(r, Date.now());
      if (cached && isFresh(r, import.meta.dirname)) return cached;
      const g = reindex(r);
      graphs.set(r, g);
      return g;
    };
    serveMcp(get, multi);
    break;
  }
  case 'help':
  case '--help':
  case '-h': {
    console.log(helpText());
    break;
  }
  default:
    home();
}

function helpText(): string {
  return `weave — repo map for agents and people

  weave                         status, and what to do next
  weave explore                 open the map in your browser (--all: every project)
  weave projects                every repo weave knows, and where they are
  weave context <file...>       one file, explained (--budget tokens)
  weave impact  <file...>       full blast radius
  weave plan    <change> [T3]   the next task, its files, its command
  weave change  [slug]          spec change records
  weave search  <query>
  weave check   [file...]       fail when a change breaks a contract (no args: git diff)
  weave evidence [REQ...]       which requirements a JUnit report proves
  weave doctor                  graph health
  weave perf    [--runs N]      index timings over time, or N fresh runs
  weave init                    connect Claude, Cursor, opencode, Codex
  weave forget  <name>          drop a project from the list
  weave mcp                     MCP server on stdio (--all: serve every project)

--root <dir>        point at a specific checkout
--project <name>    use a registered project from anywhere
--budget <tokens>   context size (default 350)
--out <file>        explore output path (default .weave/explore.html)`;
}

function isProject(dir: string): boolean {
  return ['.git', '.weave', 'package.json', 'go.mod', 'composer.json', 'pyproject.toml', 'Cargo.toml'].some((m) =>
    existsSync(join(dir, m)),
  );
}

function home(): void {
  if (!process.argv.includes('--root') && !isProject(root)) {
    console.log(`${root}
  not a project yet — no .git, .weave or package.json here.

  cd into a repository, or:  weave --root <dir>`);
    return;
  }
  const g = graphFor();
  const c = new Map<string, number>();
  for (const n of g.nodes) c.set(n.kind, (c.get(n.kind) ?? 0) + 1);
  const n = (k: string) => c.get(k) ?? 0;
  console.log(`${g.root}`);
  console.log(`  ${g.fileCount} files · ${n('endpoint')} endpoints · ${n('page')} pages · ${n('table')} tables · ${n('change')} change records`);
  const others = Math.max(0, registry.load().length - 1);
  console.log(`
  weave explore          open the map
  weave plan <slug>      the next task of a change
  weave check            what breaks (exit 1)
  weave evidence         what your tests prove
  weave context <file>   one file, explained
  weave projects         ${others ? `${others} other project(s) — switch with --project <name>` : 'your projects, all in one list'}
  weave init             connect your agent

  weave --help           all commands`);
}

// ---------------------------------------------------------------- check

function gitChanged(root: string): string[] {
  const run = (cmd: string) => {
    try {
      return execSync(cmd, { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim();
    } catch {
      return '';
    }
  };
  const out = run('git diff --name-only HEAD') || run('git status --porcelain');
  return out
    .split('\n')
    .map((l) => l.trim().replace(/^[AMDR?]{1,2}\s+/, ''))
    .filter((l) => l && !l.startsWith('.weave/'));
}

function checkCmd(root: string, args: string[]): number {
  const strict = process.argv.includes('--strict-tests');
  // Always rebuild: grading a stale graph is worse than spending a second.
  const g = reindex(root);
  const files = args.length ? args : gitChanged(root);
  const f = files.length ? analyze(g, files) : null;

  const errors: string[] = [];
  const warnings: string[] = [];

  // Endpoints that existed in the previous index and are gone now.
  const prev = new Set(g.prevEndpoints ?? []);
  const now = new Set(g.nodes.filter((n) => n.kind === 'endpoint').map((n) => n.name));
  const removed = [...prev].filter((e) => !now.has(e));
  const callSites = g.nodes.filter((n) => n.kind === 'call');
  const broken = callSites
    .map((c) => ({ call: c, gone: removed.find((r) => sameShape(r, c.name)) }))
    .filter((x): x is { call: (typeof callSites)[number]; gone: string } => !!x.gone);
  if (broken.length) {
    errors.push(`removed endpoint still called: ${broken.slice(0, 4).map((b) => `${b.gone} from ${loc(b.call)}`).join('; ')}`);
  }
  if (removed.length && !broken.length) {
    warnings.push(`removed ${removed.length} endpoint(s), no live caller: ${removed.slice(0, 4).join(', ')}`);
  }

  if (f) {
    if (f.orphans.length) {
      errors.push(`call site(s) in changed files match no endpoint: ${f.orphans.slice(0, 4).join(', ')}`);
    }
    if (f.suspect.length) warnings.push(`requirement text changed, code not re-checked: ${f.suspect.slice(0, 4).join(', ')}`);
    if (!f.changes.length) warnings.push('no spec change record names these files');
    if (f.untested.length) {
      (strict ? errors : warnings).push(`${f.untested.length} touched endpoint(s) have no direct test`);
    }

    // Evidence: a named test ran and said something about the requirement.
    const cases = loadCases(root);
    if (cases.length) {
      const ev = evidenceFacts(g, cases, f.reqs);
      const failing = ev.filter((e) => e.fail > 0);
      const unverified = ev.filter((e) => e.unverified);
      if (failing.length) errors.push(`requirement test failing: ${failing.map((e) => `${e.id} (${e.tests[0] ?? ''})`).slice(0, 3).join('; ')}`);
      if (unverified.length) warnings.push(`implemented but never tested: ${unverified.map((e) => e.id).slice(0, 4).join(', ')}`);
    } else if (f.reqs.length) {
      warnings.push(`${f.reqs.length} requirement(s) linked but no JUnit report — evidence not checked`);
    }
  }

  const scope = f
    ? `${f.files.length} file(s) → ${f.endpoints.length} endpoint(s), ${f.pages.length} page(s), ${f.tests.length} test(s)`
    : `${now.size} endpoint(s) indexed, no changed file named`;
  console.log(`check: ${scope}${removed.length ? `, ${removed.length} endpoint(s) removed` : ''}`);
  for (const e of errors) console.log(`  FAIL ${e}`);
  for (const w of warnings) console.log(`  warn ${w}`);
  for (const c of f?.commands ?? []) console.log(`  run  ${c}`);
  return errors.length ? 1 : 0;
}

/**
 * Do two `METHOD /path` names address the same route? Call sites are recorded relative
 * to the API mount, so the shorter path is compared against the tail of the longer one.
 */
function sameShape(a: string, b: string): boolean {
  const sa = a.split(/\s+/);
  const sb = b.split(/\s+/);
  if (sa[0] !== sb[0] || !sa[1] || !sb[1]) return false;
  const xa = sa[1].split('/').filter(Boolean);
  const xb = sb[1].split('/').filter(Boolean);
  const n = Math.min(xa.length, xb.length);
  if (!n) return false;
  for (let i = 1; i <= n; i++) {
    const p = xa[xa.length - i];
    const q = xb[xb.length - i];
    if (p.startsWith(':') || q.startsWith(':') || p === '*' || q === '*' || p === q) continue;
    return false;
  }
  return true;
}

function loc(n: { file?: string; line?: number }): string {
  return `${n.file ?? '?'}${n.line ? ':' + n.line : ''}`;
}

// ---------------------------------------------------------------- init

function initCmd(target: string): void {
  const cli = join(import.meta.dirname, 'cli.ts');
  const args = [cli, 'mcp', '--root', '.'];
  const did: string[] = [];
  const skipped: string[] = [];

  const writeJson = (rel: string, value: unknown) => {
    const p = join(target, rel);
    if (existsSync(p)) {
      skipped.push(rel + ' (exists)');
      return;
    }
    mkdirSync(dirname(p), { recursive: true });
    writeFileSync(p, JSON.stringify(value, null, 2) + '\n');
    did.push(rel);
  };

  const stdio = { command: 'node', args };
  writeJson('.mcp.json', { mcpServers: { weave: stdio } });
  writeJson('.cursor/mcp.json', { mcpServers: { weave: stdio } });
  writeJson('.vscode/mcp.json', { servers: { weave: { type: 'stdio', ...stdio } } });

  // opencode keeps its own JSON; merge instead of replacing.
  const oc = join(target, 'opencode.json');
  if (existsSync(oc)) {
    try {
      const j = JSON.parse(readFileSync(oc, 'utf8'));
      if (j.mcp?.weave) skipped.push('opencode.json (already has weave)');
      else {
        j.mcp = { ...(j.mcp ?? {}), weave: { type: 'local', command: args, enabled: true } };
        writeFileSync(oc, JSON.stringify(j, null, 2) + '\n');
        did.push('opencode.json');
      }
    } catch {
      skipped.push('opencode.json (not valid JSON — add by hand)');
    }
  } else {
    writeJson('opencode.json', { mcp: { weave: { type: 'local', command: args, enabled: true } } });
  }

  // Index cache must not be committed.
  const gi = join(target, '.gitignore');
  if (existsSync(gi)) {
    const text = readFileSync(gi, 'utf8');
    if (!/^\.weave\/?$/m.test(text)) {
      writeFileSync(gi, text.replace(/\n?$/, '\n') + '.weave/\n');
      did.push('.gitignore += .weave/');
    }
  } else {
    writeFileSync(gi, '.weave/\n');
    did.push('.gitignore');
  }

  console.log(`weave init in ${target}`);
  for (const d of did) console.log(`  + ${d}`);
  for (const s of skipped) console.log(`  = ${s}`);
  console.log(`
claude code (user scope, any project):
  claude mcp add weave -- node "${cli}" mcp --root .

serve every registered project from one MCP entry (no per-repo config):
  claude mcp add weave -- node "${cli}" mcp --all
  then: context/plan/impact take an optional "project" argument (names from \`weave projects\`).

agents that read AGENTS.md, paste this:
  ## Repo map
  Before editing any file, call weave's context(files) once. It returns the page,
  endpoints, tables, owning spec change, open tasks, the tests to run and the
  command to run them. Use impact(files) for the full blast radius.

enforce it without the agent's cooperation (any of these, same command):
  git pre-commit   : node "${cli}" check --root .
  claude code hook : {"matcher":"Edit|Write","hooks":[{"type":"command","command":"node \\"${cli}\\" check --root ."}]}
  kiro hook        : .kiro/hooks/weave.json  -> trigger PostFileSave, command above
  ci               : node "${cli}" index --root . && node "${cli}" check --root .

mark code with the requirement it implements (exact traceability, any language):
  // @req REQ-ORDERS-3
  then context/impact report it, and a changed requirement marks the code suspect.

prove a requirement is tested (needed for \`weave evidence\` and the check gate):
  vitest     npx vitest run --reporter=junit --outputFile=.weave/junit.xml
  jest       npx jest --reporters=jest-junit   (JEST_JUNIT_OUTPUT_FILE=.weave/junit.xml)
  playwright npx playwright test --reporter=junit
  phpunit    vendor/bin/phpunit --log-junit .weave/junit.xml
  then: weave evidence

no MCP support? shell out instead:
  node "${cli}" context <file...> --root .`);
}
