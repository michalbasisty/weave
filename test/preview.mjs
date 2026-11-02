// Visual preview: node test/preview.mjs  → writes preview.html
// Hand-built graph so the page is small enough to inspect in one screen.
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { renderExplore } from '../src/render.ts';

const n = (i, k, name, file, extra = {}) => ({ id: i, kind: k, name, file, ...extra });
const e = (from, to, kind, conf = 'exact') => ({ from, to, kind, conf });

const nodes = [
  n('page:/warehouse', 'page', '/warehouse', 'packages/web/src/pages/Warehouse/Warehouse.tsx', {
    line: 12,
    meta: { desc: 'Magazyn: parts catalogue, orders in three states, and stock movements. Managers create orders; parts managers move them.' },
  }),
  n('page:/tasks/:id', 'page', '/tasks/:id', 'packages/web/src/pages/Tasks/TaskDetail.tsx', {
    meta: { desc: 'One task: assignee, photos, checklist, and the parts consumed while repairing.' },
  }),
  n('module:orders', 'module', 'orders', 'packages/backend/src/modules/orders'),
  n('module:trpc', 'module', 'trpc', 'packages/backend/src/trpc/router.ts'),
  n('endpoint:GET /api/v1/orders', 'endpoint', 'GET /api/v1/orders', 'packages/backend/src/modules/orders/orders.routes.ts', {
    line: 58,
    meta: { module: 'orders', desc: 'List orders with counts, filtered by status. Managers see all, technicians only their own department.' },
  }),
  n('endpoint:POST /api/v1/orders', 'endpoint', 'POST /api/v1/orders', 'packages/backend/src/modules/orders/orders.routes.ts', {
    line: 118,
    meta: { module: 'orders', desc: 'Create an order from a task. Requires at least one part and the orders.create permission.' },
  }),
  n('endpoint:QUERY /api/trpc/quotes.list', 'endpoint', 'QUERY /api/trpc/quotes.list', 'packages/backend/src/trpc/router.ts', {
    meta: { module: 'trpc', desc: 'query quotes.list' },
  }),
  n('table:part_orders', 'table', 'part_orders', 'packages/backend/prisma/schema.prisma', {
    meta: { desc: 'One order: requester, target status, and the delivery timestamp that takes stock.' },
  }),
  n('table:parts', 'table', 'parts', 'packages/backend/prisma/schema.prisma', {
    meta: { desc: 'Catalogue of spare parts with stock, minimum level and warehouse location.' },
  }),
  n('test:packages/web/e2e/admin.spec.ts', 'test', 'packages/web/e2e/admin.spec.ts', 'packages/web/e2e/admin.spec.ts'),
  n('change:2026-10-22-warehouse-orders', 'change', '2026-10-22-warehouse-orders', 'specs/changes/2026-10-22-warehouse-orders/tasks.md', {
    meta: { desc: 'The warehouse gets orders: a manager raises one from a task, a parts manager moves it through three states, and stock is taken on delivery.' },
  }),
  n('task:2026-10-22-warehouse-orders/T1', 'task', '2026-10-22-warehouse-orders T1', undefined, {
    meta: { title: 'Table part_orders + migration 20261022120000_part_orders', done: true },
  }),
  n('task:2026-10-22-warehouse-orders/T2', 'task', '2026-10-22-warehouse-orders T2', undefined, {
    meta: { title: 'Permission orders.create (technician default, one-time backfill)', done: true },
  }),
  n('task:2026-10-22-warehouse-orders/T3', 'task', '2026-10-22-warehouse-orders T3', undefined, {
    meta: { title: 'API /orders: list + counts, badge, one order, create, status move', done: false },
  }),
  n('req:REQ-WH-1', 'req', 'REQ-WH-1', 'specs/changes/2026-10-22-warehouse-orders/spec-delta.md', {
    meta: { title: 'A manager SHALL create an order from a task with at least one part' },
  }),
  n('req:REQ-WH-2', 'req', 'REQ-WH-2', 'specs/changes/2026-10-22-warehouse-orders/spec-delta.md', {
    meta: { title: 'Stock SHALL decrease only when the order is marked delivered' },
  }),
  n('file:packages/backend/src/modules/orders/orders.domain.ts', 'file', 'packages/backend/src/modules/orders/orders.domain.ts', 'packages/backend/src/modules/orders/orders.domain.ts'),
];

const edges = [
  e('page:/warehouse', 'endpoint:GET /api/v1/orders', 'calls'),
  e('page:/warehouse', 'endpoint:POST /api/v1/orders', 'calls'),
  e('page:/tasks/:id', 'endpoint:POST /api/v1/orders', 'calls', 'inferred'),
  e('page:/warehouse', 'endpoint:QUERY /api/trpc/quotes.list', 'calls'),
  e('endpoint:GET /api/v1/orders', 'module:orders', 'in_module'),
  e('endpoint:POST /api/v1/orders', 'module:orders', 'in_module'),
  e('endpoint:QUERY /api/trpc/quotes.list', 'module:trpc', 'in_module'),
  e('module:orders', 'table:part_orders', 'uses'),
  e('module:orders', 'table:parts', 'uses'),
  e('test:packages/web/e2e/admin.spec.ts', 'endpoint:GET /api/v1/orders', 'hits'),
  e('test:packages/web/e2e/admin.spec.ts', 'page:/warehouse', 'exercises'),
  e('change:2026-10-22-warehouse-orders', 'task:2026-10-22-warehouse-orders/T1', 'has_task'),
  e('change:2026-10-22-warehouse-orders', 'task:2026-10-22-warehouse-orders/T2', 'has_task'),
  e('change:2026-10-22-warehouse-orders', 'task:2026-10-22-warehouse-orders/T3', 'has_task'),
  e('task:2026-10-22-warehouse-orders/T3', 'req:REQ-WH-1', 'satisfies'),
  e('task:2026-10-22-warehouse-orders/T3', 'file:packages/backend/src/modules/orders/orders.domain.ts', 'touches'),
  e('req:REQ-WH-2', 'file:packages/backend/src/modules/orders/orders.domain.ts', 'specified_in'),
];

const idx = new Map(nodes.map((x) => [x.id, x]));
const byKind = (k) => nodes.filter((x) => x.kind === k);
const graph = {
  version: 3,
  root: 'G:/projekty/QR-gent',
  builtAt: new Date().toISOString(),
  fileCount: 745,
  nodes,
  edges,
  coverage: {
    routes: { files: 28, found: 28, silent: [] },
    calls: { total: 237, resolved: 237, dynamic: 1 },
    pages: { declared: 35, mapped: 33, skipped: 2 },
    specs: { files: 87, parsed: 87, silent: [] },
    prisma: { models: 46, tables: 46 },
  },
};

// Keep payload ids consistent with what explorePayload expects.
const missing = nodes.filter((x) => !idx.has(x.id)).length;
if (missing) throw new Error('bad fixture');

const html = renderExplore([{ name: 'QR-gent', graph }]);
// Pretty-print the embedded payload so the page is reviewable line by line.
const start = html.indexOf('const D={') + 'const D='.length;
const end = html.indexOf('\nconst $=');
if (start < 8 || end < start) throw new Error('no payload');
const json = html.slice(start, end).replace(/;\s*$/, '');
const pretty = JSON.stringify(JSON.parse(json.replace(/\\u003c/g, '<')), null, 1).replace(/</g, '\\u003c');
// A fixed frame height so the page also renders correctly inline in a chat panel.
const framed = html
  .slice(0, start) + pretty + ';' + html.slice(end);
const withFrame = framed.replace('<style>', '<style>html,body{height:780px!important;overflow:hidden}');
writeFileSync(join(import.meta.dirname, '..', 'preview.html'), withFrame);
console.log(`preview.html — ${byKind('endpoint').length} endpoints, ${byKind('page').length} pages`);
