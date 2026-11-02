import { appendFileSync, existsSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

// Timings for every phase of a run, appended to .weave/perf.jsonl so growth and
// regressions are visible instead of guessed at.

export interface PerfRecord {
  at: string;
  files: number;
  nodes: number;
  edges: number;
  totalMs: number;
  phases: Record<string, number>;
  heapMb: number;
}

const round = (n: number) => Math.round(n * 10) / 10;

export class Timer {
  private marks: Record<string, number> = {};
  private last = performance.now();
  private start = this.last;

  lap(name: string): number {
    const now = performance.now();
    this.marks[name] = round(now - this.last);
    this.last = now;
    return this.marks[name];
  }

  get phases(): Record<string, number> {
    return this.marks;
  }

  get total(): number {
    return round(performance.now() - this.start);
  }
}

function perfPath(root: string): string {
  return join(root, '.weave', 'perf.jsonl');
}

export function appendPerf(root: string, record: PerfRecord): void {
  const p = perfPath(root);
  try {
    if (existsSync(p) && statSync(p).size > 400_000) writeFileSync(p, '');
    appendFileSync(p, JSON.stringify(record) + '\n');
  } catch {
    // a read-only checkout must not fail an index run
  }
}

export function readPerf(root: string): PerfRecord[] {
  const p = perfPath(root);
  if (!existsSync(p)) return [];
  return readFileSync(p, 'utf8')
    .split('\n')
    .filter(Boolean)
    .flatMap((line) => {
      try {
        return [JSON.parse(line) as PerfRecord];
      } catch {
        return [];
      }
    });
}

export function perfReport(records: PerfRecord[], window = 8): string {
  if (!records.length) return 'no runs recorded yet — run: weave index';
  const rows = records.slice(-window);
  // Only the newest run's phase names: older runs may predate a phase being split.
  const phases = Object.keys(rows[rows.length - 1].phases);
  const out: string[] = [`last ${rows.length} index run(s)`];
  out.push(['when'.padEnd(20), 'files'.padStart(6), 'nodes'.padStart(6), 'edges'.padStart(6), 'total'.padStart(8), ...phases.map((p) => p.padStart(9))].join(' '));
  for (const r of rows) {
    out.push(
      [
        r.at.slice(0, 19).replace('T', ' ').padEnd(20),
        String(r.files).padStart(6),
        String(r.nodes).padStart(6),
        String(r.edges).padStart(6),
        (r.totalMs + 'ms').padStart(8),
        ...phases.map((p) => ((r.phases[p] ?? 0) + 'ms').padStart(9)),
      ].join(' '),
    );
  }
  const totals = rows.map((r) => r.totalMs);
  const avg = totals.reduce((a, b) => a + b, 0) / totals.length;
  const max = Math.max(...totals);
  const first = totals[0];
  const last = totals[totals.length - 1];
  const trend = first > 0 ? Math.round(((last - first) / first) * 100) : 0;
  out.push('');
  out.push(`avg ${Math.round(avg)}ms · max ${Math.round(max)}ms · heap ${rows[rows.length - 1].heapMb}MB · trend ${trend >= 0 ? '+' : ''}${trend}% over the window`);
  return out.join('\n');
}
