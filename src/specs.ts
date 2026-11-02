import { createHash } from 'node:crypto';
import { Builder, id } from './model.ts';
import { baseName, dirOf, type SourceFile } from './scan.ts';

export interface Req {
  id: string;
  title: string;
  desc?: string;
  change: string;
  kind: 'ADDED' | 'MODIFIED' | 'REMOVED' | 'EXISTING';
  file: string;
  line: number;
  hash?: string;
}

export interface Task {
  id: string;
  change: string;
  title: string;
  done: boolean;
  reqs: string[];
  evidence: string[];
}

/** Stable requirement ids across the three conventions in the wild. */
const REQ_ID = /\b(?:REQ|FR|NFR)-[A-Z0-9]+(?:-[A-Z0-9]+)*\b/;
const REQ_ANY = new RegExp(REQ_ID.source, 'g');
const HEADING = /^(#{1,5})\s+(.*)$/;
const HEADING_ID = /^(?:(ADDED|MODIFIED|REMOVED)\s+)?((?:REQ|FR|NFR)-[A-Z0-9-]+)\s*[—–:-]?\s*(.*)$/;
const BULLET_ID = /^-\s+(?:\[[ xX]\]\s+)?\**((?:REQ|FR|NFR)-[A-Z0-9-]+)\**\s*[:—–-]\s*(.*)$/;
const TASK_START = /^-\s+\[( |x|X)\]\s+(.*)$/;
// `T1`, `B1.1`, `1.2` or nothing at all: real task lists use all four.
const TASK_ID = /^\**((?:[A-Z]{0,2}\d+(?:\.\d+)+)|(?:T\d+)|(?:\d+\.))\**\s+/;

export function extractSpecs(specFiles: SourceFile[], b: Builder): { reqs: Req[]; tasks: Task[] } {
  const reqs = new Map<string, Req>();
  const tasks: Task[] = [];

  for (const f of specFiles) {
    const isDelta = /\/spec-delta\.md$/.test(f.rel);
    const isCapability = /\/capabilities\/[^/]+\/spec\.md$/.test(f.rel);
    if (!isDelta && !isCapability) continue;
    const change = baseName(dirOf(f.rel));

    let cur: { id: string; title: string; kind: Req['kind']; text: string[]; line: number } | null = null;
    const flush = () => {
      if (!cur) return;
      const bodyRaw = cur.text.join('\n').replace(/\s+/g, ' ').trim();
      const body = cur.text.slice(1).join(' ').replace(/\s+/g, ' ').trim();
      const hash = createHash('sha256').update(cur.id + '\u0000' + bodyRaw).digest('hex').slice(0, 16);
      const prev = reqs.get(cur.id);
      if (!prev) {
        reqs.set(cur.id, { id: cur.id, title: cur.title, desc: body.slice(0, 220), change, kind: cur.kind, file: f.rel, line: cur.line, hash });
      } else if (!prev.hash) {
        reqs.set(cur.id, { ...prev, hash, title: prev.title || cur.title, desc: prev.desc || body.slice(0, 220) });
      }
      cur = null;
    };

    f.lines.forEach((line, i) => {
      const head = line.match(HEADING);
      if (head) {
        flush();
        const hm = head[2].match(HEADING_ID);
        if (hm) cur = { id: hm[2], title: hm[3].trim(), kind: (hm[1] as Req['kind']) ?? 'EXISTING', text: [line], line: i + 1 };
        return;
      }
      const bullet = line.match(BULLET_ID);
      if (bullet) {
        flush();
        cur = { id: bullet[1], title: bullet[2].trim(), kind: 'EXISTING', text: [line], line: i + 1 };
        return;
      }
      if (cur) cur.text.push(line);
    });
    flush();

    // Requirements mentioned without a block of their own still count for traceability.
    for (const m of f.text.matchAll(REQ_ANY)) {
      if (reqs.has(m[0])) continue;
      reqs.set(m[0], { id: m[0], title: '', change, kind: 'EXISTING', file: f.rel, line: 0 });
    }
  }

  for (const r of reqs.values()) {
    b.node({
      id: id.req(r.id),
      kind: 'req',
      name: r.id,
      file: r.file,
      line: r.line || undefined,
      meta: { title: r.title, desc: r.desc, hash: r.hash },
    });
    b.edge(id.req(r.id), id.file(r.file), 'specified_in');
  }

  const proposalOf = (slug: string) =>
    specFiles.find((f) => f.rel.endsWith(`/changes/${slug}/proposal.md`)) ??
    specFiles.find((f) => f.rel.endsWith(`/changes/${slug}/spec-delta.md`));

  for (const f of specFiles) {
    if (!/\/tasks\.md$/.test(f.rel) || /\/templates\//.test(f.rel)) continue;
    const change = baseName(dirOf(f.rel));
    const doc = proposalOf(change);
    b.node({ id: id.change(change), kind: 'change', name: change, file: f.rel, meta: { desc: doc ? firstParagraph(doc.text) : '' } });
    let cur: Task | null = null;
    let seq = 0;
    const flush = () => {
      if (cur) tasks.push(cur);
      cur = null;
    };
    for (const raw of f.lines) {
      const tm = raw.match(TASK_START);
      if (tm) {
        flush();
        seq++;
        const body = tm[2];
        const idm = body.match(TASK_ID);
        const taskId = idm ? idm[1] : `T${seq}`;
        const rest = idm ? body.slice(idm[0].length) : body;
        cur = {
          id: taskId,
          change,
          title: rest.replace(/`\[Satisfies:[^\]]*\]`/g, '').trim(),
          done: tm[1].toLowerCase() === 'x',
          reqs: raw.match(REQ_ANY) ?? [],
          evidence: backticks(raw),
        };
        continue;
      }
      if (!cur) continue;
      if (/^\s+\S/.test(raw) || raw.trim() === '') {
        cur.reqs.push(...(raw.match(REQ_ANY) ?? []));
        cur.evidence.push(...backticks(raw));
      } else flush();
    }
    flush();
  }

  for (const t of tasks) {
    const tid = id.task(`${t.change}/${t.id}`);
    b.node({ id: tid, kind: 'task', name: `${t.change} ${t.id}`, meta: { done: t.done, title: t.title } });
    b.edge(id.change(t.change), tid, 'has_task');
    for (const r of new Set(t.reqs)) b.edge(tid, id.req(r), 'satisfies');
  }
  return { reqs: [...reqs.values()], tasks };
}

function backticks(line: string): string[] {
  return [...line.matchAll(/`([^`]+)`/g)].map((m) => m[1]).filter((s) => /[/.]/.test(s) && !s.includes(' '));
}

/** The first real sentence of a proposal: skip the title, headings, tables, quotes and code. */
export function firstParagraph(md: string, max = 280): string {
  const lines = md.split(/\r?\n/);
  for (const raw of lines) {
    const t = raw.trim();
    if (!t || t.startsWith('#') || t.startsWith('|') || t.startsWith('>') || t.startsWith('```') || t.startsWith('-')) continue;
    if (t.startsWith('`') || /^[`\w./-]+`?\s*[,.]?$/.test(t)) continue; // a bare path is not a sentence
    if (!/^[A-Z0-9]/.test(t)) continue; // a wrapped continuation, not the start
    if (t.split(/\s+/).length < 5) continue;
    return t.replace(/\s+/g, ' ').slice(0, max);
  }
  return '';
}

/** Resolve a task evidence path to a known repo file (tries monorepo conventions). */
export function resolveEvidence(path: string, known: Set<string>): string | null {
  const p = path.replace(/^\.?\//, '').replace(/\/$/, '');
  const candidates = [p, `packages/backend/src/${p}`, `packages/web/src/${p}`, `packages/backend/${p}`, `packages/web/${p}`, `specs/${p}`];
  for (const c of candidates) if (known.has(c)) return c;
  for (const c of candidates) {
    for (const k of known) if (k.startsWith(c + '/')) return c + '/';
  }
  return null;
}
