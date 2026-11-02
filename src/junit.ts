// JUnit XML is the one test report every runner can write. Reading it is how
// weave proves a requirement is implemented *and* verified, without asking the agent.

import { existsSync, statSync } from 'node:fs';
import { join } from 'node:path';

export type Status = 'pass' | 'fail' | 'skip';

export interface Case {
  name: string;
  suite: string;
  file?: string;
  status: Status;
}

const TESTCASE = /<testcase\b([^>]*?)(\/>|>([\s\S]*?)<\/testcase>)/g;
const ATTR = /(\w+)\s*=\s*"([^"]*)"/g;

export function parseJUnit(xml: string): Case[] {
  const out: Case[] = [];
  for (const m of xml.matchAll(TESTCASE)) {
    const attrs: Record<string, string> = {};
    for (const a of m[1].matchAll(ATTR)) attrs[a[1]] = decode(a[2]);
    const body = m[3] ?? '';
    const status: Status = /<(failure|error)\b/.test(body) ? 'fail' : /<skipped\b/.test(body) ? 'skip' : 'pass';
    out.push({
      name: attrs.name ?? '',
      suite: attrs.classname ?? attrs.suite ?? '',
      file: attrs.file,
      status,
    });
  }
  return out;
}

function decode(s: string): string {
  return s.replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&apos;/g, "'").replace(/&amp;/g, '&');
}

/** Requirement ids a test case points at: in its own name, or in its file's `@req` markers. */
export function reqsInCase(c: Case): string[] {
  return [...`${c.name} ${c.suite}`.matchAll(/\b((?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*)\b/g)].map((m) => m[1]);
}

const COMMON = [
  '.weave/junit.xml',
  'junit.xml',
  'test-results/junit.xml',
  'test-results.xml',
  'coverage/junit.xml',
  'reports/junit.xml',
  'packages/backend/junit.xml',
  'packages/web/junit.xml',
];

/** Locate the newest JUnit report the project already produced. */
export function findJUnitFile(root: string): string | null {
  const found: { path: string; mtime: number }[] = [];
  for (const rel of COMMON) {
    const p = join(root, rel);
    if (existsSync(p)) found.push({ path: p, mtime: statSync(p).mtimeMs });
  }
  if (found.length) return found.sort((a, b) => b.mtime - a.mtime)[0].path;
  return null;
}
