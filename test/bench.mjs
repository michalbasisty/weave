// Latency probe: node test/bench.mjs <repo-root>
import { spawn } from 'node:child_process';
import { join } from 'node:path';

const root = process.argv[2];
const cli = join(import.meta.dirname, '..', 'src', 'cli.ts');
const proc = spawn(process.execPath, [cli, 'mcp', '--root', root], { stdio: ['pipe', 'pipe', 'inherit'] });

let id = 0;
let buf = '';
const start = new Map();
const times = [];
const pending = new Map();

proc.stdout.setEncoding('utf8');
proc.stdout.on('data', (chunk) => {
  buf += chunk;
  let nl;
  while ((nl = buf.indexOf('\n')) >= 0) {
    const line = buf.slice(0, nl).trim();
    buf = buf.slice(nl + 1);
    if (!line) continue;
    const msg = JSON.parse(line);
    if (msg.id == null) continue;
    const t = start.get(msg.id);
    if (t) times.push({ id: msg.id, ms: Date.now() - t, bytes: JSON.stringify(msg.result ?? {}).length });
    const p = pending.get(msg.id);
    if (p) p(msg);
  }
});

const call = (method, params) =>
  new Promise((resolve) => {
    const myId = ++id;
    start.set(myId, Date.now());
    pending.set(myId, resolve);
    proc.stdin.write(JSON.stringify({ jsonrpc: '2.0', id: myId, method, params }) + '\n');
  });

const files = [
  'packages/backend/src/modules/orders/orders.routes.ts',
  'packages/web/src/pages/Warehouse/Orders.tsx',
  'packages/backend/src/utils/permissions.ts',
  'packages/web/src/pages/Tasks/TaskDetail.tsx',
  'packages/backend/src/modules/tasks/tasks.routes.ts',
];

await call('initialize', { protocolVersion: '2025-06-18', capabilities: {} });
for (const f of files) await call('tools/call', { name: 'context', arguments: { files: [f] } });
for (const f of files) await call('tools/call', { name: 'impact', arguments: { files: [f] } });

const afterInit = times.slice(1); // drop the initialize reply
const avg = afterInit.slice(1).reduce((a, b) => a + b.ms, 0) / Math.max(1, afterInit.length - 1);
console.log(`first tools/call  ${afterInit[0].ms} ms  (cold: builds the index)`);
console.log(`later avg         ${avg.toFixed(0)} ms  over ${afterInit.length - 1} calls`);
console.log(`median bytes      ${afterInit.map((t) => t.bytes).sort((a, b) => a - b)[Math.floor(afterInit.length / 2)]}`);
proc.stdin.end();
