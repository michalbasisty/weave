import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { basename, join } from 'node:path';

// Projects live in one small file at ~/.weave/projects.json. Registering is a side
// effect of using a repo, so there is no "add" step to forget.

export interface Entry {
  name: string;
  root: string;
  lastUsed: string;
  files?: number;
  endpoints?: number;
  changes?: number;
}

export function homeDir(): string {
  return process.env.WEAVE_HOME || join(homedir(), '.weave');
}

function file(): string {
  return join(homeDir(), 'projects.json');
}

export function load(): Entry[] {
  try {
    const j = JSON.parse(readFileSync(file(), 'utf8'));
    return Array.isArray(j.projects) ? j.projects : [];
  } catch {
    return [];
  }
}

export function save(entries: Entry[]): void {
  try {
    mkdirSync(homeDir(), { recursive: true });
    writeFileSync(file(), JSON.stringify({ projects: entries.slice(0, 200) }, null, 2) + '\n');
  } catch {
    // a read-only home must not break the command
  }
}

/** Remember a repo the moment it is used. Same name on a different root gets a suffix. */
export function touch(root: string): Entry {
  const entries = load();
  const norm = (p: string) => p.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
  const found = entries.find((e) => norm(e.root) === norm(root));
  if (found) {
    found.lastUsed = new Date().toISOString();
    save(entries);
    return found;
  }
  const base = basename(root.replace(/\\/g, '/')) || 'project';
  let name = base;
  for (let i = 2; entries.some((e) => e.name === name); i++) name = `${base}-${i}`;
  const entry: Entry = { name, root, lastUsed: new Date().toISOString() };
  entries.push(entry);
  save(entries);
  return entry;
}

/** Record what the last index found, for the project list. */
export function noteIndex(root: string, counts: { files: number; endpoints: number; changes: number }): void {
  const entries = load();
  const norm = (p: string) => p.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
  const found = entries.find((e) => norm(e.root) === norm(root));
  if (!found) return;
  found.files = counts.files;
  found.endpoints = counts.endpoints;
  found.changes = counts.changes;
  found.lastUsed = new Date().toISOString();
  save(entries);
}

const key = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, '');

/** Every entry whose name or folder matches, so the caller can explain ambiguity. */
export function candidates(name: string): Entry[] {
  const q = key(name);
  if (!q) return [];
  const all = load();
  const hit = (e: Entry) => key(e.name).startsWith(q) || key(basename(e.root)).startsWith(q);
  const starts = all.filter(hit);
  // `print` should find `QR-gent-print`; a bare prefix still wins when it exists.
  return starts.length ? starts : all.filter((e) => key(e.name).includes(q) || key(basename(e.root)).includes(q));
}

/** Exact name, or an unambiguous prefix. `qrgent` finds `QR-gent`; `qr` refuses both variants. */
export function resolve(name: string): Entry | null {
  const q = key(name);
  const entries = load();
  const exact = entries.filter((e) => key(e.name) === q);
  if (exact.length === 1) return exact[0];
  const hits = candidates(name);
  return hits.length === 1 ? hits[0] : null;
}

export function forget(name: string): boolean {
  const entries = load();
  const next = entries.filter((e) => e.name !== name);
  if (next.length === entries.length) return false;
  save(next);
  return true;
}

/** `weave projects` — one line per repo, newest first. */
export function table(entries = load()): string {
  if (!entries.length) return 'no projects yet — run `weave` inside a repo';
  const rows = [...entries].sort((a, b) => b.lastUsed.localeCompare(a.lastUsed));
  const width = Math.max(...rows.map((r) => r.name.length), 4);
  const lines = rows.map((r) => {
    const stats = r.files ? `${String(r.files).padStart(4)} files · ${String(r.endpoints ?? 0).padStart(3)} ep · ${String(r.changes ?? 0).padStart(2)} changes` : 'not indexed';
    return `${r.name.padEnd(width)}  ${stats.padEnd(34)}  ${r.root}`;
  });
  return ['name'.padEnd(width) + '  ' + 'size'.padEnd(34) + '  path', ...lines].join('\n');
}

export function exists(root: string): boolean {
  return existsSync(root);
}
