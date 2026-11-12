// The weave panel: the open project's map. Search it in plain words, see what a
// file is in the app (context) or everything a change to it reaches (impact),
// and hand either to the agent. Every answer comes from the extension's local
// service, which runs the weave binary on this machine.
import { connectHost, HostRequestError } from '@openchamber/sdk';
import {
  applyHostReady, mountBanner, mountButton, mountEmpty, mountList, mountSearchField, mountSpinner, mountTabs,
  type ListItem,
} from '@openchamber/sdk/ui';

type Page = { name: string; file: string; endpoints: number };
type Overview = { screens: number; endpoints: number; modules: number; tables: number; pages: Page[]; error?: string };
type Hit = { kind: string; name: string; file: string };

const host = connectHost();
const root = document.querySelector<HTMLElement>('#root')!;

const style = document.createElement('style');
style.textContent = (`
  body { margin: 0; font-family: var(--oc-font); color: var(--oc-fg); background: var(--oc-bg); }
  .w { display: flex; flex-direction: column; gap: 10px; padding: 12px; height: 100vh; box-sizing: border-box; }
  .w-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; }
  .w-stat { border: 1px solid var(--oc-border); border-radius: var(--oc-radius); padding: 6px 8px; }
  .w-stat b { display: block; font-size: 15px; font-variant-numeric: tabular-nums; }
  .w-stat span { font-size: 11px; color: var(--oc-muted); }
  .w-list { flex: 1; min-height: 0; overflow: auto; }
  .w-detail { display: flex; flex-direction: column; gap: 8px; flex: 1; min-height: 0; }
  .w-head { display: flex; align-items: center; gap: 8px; }
  .w-title { flex: 1; min-width: 0; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .w-file { font: 11px var(--oc-mono); color: var(--oc-muted); overflow-wrap: anywhere; }
  .w-pre { flex: 1; min-height: 0; overflow: auto; margin: 0; padding: 8px; border: 1px solid var(--oc-border);
           border-radius: var(--oc-radius); background: var(--oc-muted-surface); font: 12px/1.45 var(--oc-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
  .w-row { display: flex; gap: 6px; flex-wrap: wrap; }
`);
document.head.append(style);

let directory: string | null = null;
let query = '';
let overview: Overview | null = null;
let hits: Hit[] = [];
let detail: { title: string; file: string; mode: 'context' | 'impact'; text: string | null } | null = null;
let error: string | null = null;
let searchSeq = 0;

const ask = async <T>(path: string, query: Record<string, string>): Promise<T> => {
  try {
    const r = await host.serviceRequest({ method: 'GET', path, query: { directory: directory ?? '', ...query } });
    const body = typeof r.body === 'string' ? JSON.parse(r.body) : r.body;
    if (r.status >= 400) throw new Error(body?.message ?? `weave answered ${r.status}`);
    return body as T;
  } catch (e) {
    if (e instanceof HostRequestError && e.code === 'NO_SERVICE') throw new Error('Allow the local service for weave in Settings → Extensions.');
    throw e instanceof Error ? e : new Error(String(e));
  }
};

// ---------------------------------------------------------------- layout

const wrap = document.createElement('div');
wrap.className = 'w';
root.append(wrap);
const bannerSlot = document.createElement('div');
const stats = document.createElement('div');
stats.className = 'w-stats';
const searchSlot = document.createElement('div');
const listSlot = document.createElement('div');
listSlot.className = 'w-list';
const detailSlot = document.createElement('div');
detailSlot.className = 'w-detail';
wrap.append(bannerSlot, stats, searchSlot, listSlot, detailSlot);

mountSearchField(searchSlot, {
  value: '', placeholder: 'Search the map: "inbox count", /api/users, orders', label: 'Search the map',
  onChange: (v) => { query = v; void runSearch(); },
});

let banner: ReturnType<typeof mountBanner> | null = null;
const list = mountList(listSlot, { items: [], onSelect: (id) => void open(id), emptyText: '', ariaLabel: 'weave map' });

const paintStats = (): void => {
  stats.replaceChildren();
  if (!overview || overview.error) return;
  for (const [n, label] of [[overview.screens, 'screens'], [overview.endpoints, 'endpoints'], [overview.modules, 'modules'], [overview.tables, 'tables']] as const) {
    const box = document.createElement('div');
    box.className = 'w-stat';
    const b = document.createElement('b');
    b.textContent = String(n);
    const s = document.createElement('span');
    s.textContent = label;
    box.append(b, s);
    stats.append(box);
  }
};

const items = (): ListItem[] => {
  if (query.trim()) {
    return hits.map((h, i) => ({ id: `hit:${i}`, title: h.name, leading: h.kind.slice(0, 8), subtitle: h.file }));
  }
  return (overview?.pages ?? []).map((p, i) => ({
    id: `page:${i}`, title: p.name, subtitle: p.file, meta: p.endpoints ? `${p.endpoints} call${p.endpoints === 1 ? "" : "s"}` : undefined,
  }));
};

const paint = (): void => {
  banner?.dispose();
  banner = null;
  const message = error ?? overview?.error ?? (directory ? null : 'Open a project to see its map.');
  if (message) banner = mountBanner(bannerSlot, { tone: directory ? 'error' : 'info', title: message });
  paintStats();
  const showDetail = detail !== null;
  listSlot.style.display = showDetail ? 'none' : '';
  searchSlot.style.display = showDetail ? 'none' : '';
  detailSlot.style.display = showDetail ? '' : 'none';
  list.update({ items: items(), emptyText: query.trim() ? 'Nothing matched. Try fewer words.' : 'No screens found in this project.' });
  if (detail) paintDetail();
};

// ---------------------------------------------------------------- detail

const paintDetail = (): void => {
  const d = detail!;
  detailSlot.replaceChildren();
  const head = document.createElement('div');
  head.className = 'w-head';
  const back = document.createElement('div');
  mountButton(back, { label: '← Back', variant: 'ghost', size: 'sm', onClick: () => { detail = null; paint(); } });
  const title = document.createElement('div');
  title.className = 'w-title';
  title.textContent = d.title;
  head.append(back, title);
  const file = document.createElement('div');
  file.className = 'w-file';
  file.textContent = d.file;
  const tabs = document.createElement('div');
  mountTabs(tabs, {
    items: [{ id: 'context', label: 'What it is' }, { id: 'impact', label: 'What a change reaches' }],
    activeId: d.mode, onChange: (id) => void load(d.title, d.file, id as 'context' | 'impact'),
  });
  detailSlot.append(head, file, tabs);
  if (d.text === null) {
    const spin = document.createElement('div');
    mountSpinner(spin, {});
    detailSlot.append(spin);
    return;
  }
  const pre = document.createElement('pre');
  pre.className = 'w-pre';
  pre.textContent = d.text;
  const actions = document.createElement('div');
  actions.className = 'w-row';
  const add = document.createElement('div');
  mountButton(add, {
    label: 'Add to chat', size: 'sm',
    onClick: () => void host.compose({ text: `weave ${d.mode} for ${d.file}:\n\n\`\`\`\n${d.text}\n\`\`\`\n`, mode: 'append' })
      .catch(() => host.toast({ kind: 'error', message: 'Could not add to the chat input.' })),
  });
  const copy = document.createElement('div');
  mountButton(copy, {
    label: 'Copy', size: 'sm', variant: 'outline',
    onClick: () => void host.writeClipboard(d.text!.slice(0, 32000)).then(() => host.toast({ kind: 'success', message: 'Copied' })),
  });
  actions.append(add, copy);
  detailSlot.append(pre, actions);
};

const load = async (title: string, file: string, mode: 'context' | 'impact'): Promise<void> => {
  detail = { title, file, mode, text: null };
  paint();
  try {
    const r = await ask<{ text: string }>(`/${mode}`, { file });
    if (detail?.file === file && detail.mode === mode) detail.text = r.text || '(nothing)';
  } catch (e) {
    if (detail?.file === file) detail.text = (e as Error).message;
  }
  paint();
};

const open = async (id: string): Promise<void> => {
  const [kind, i] = id.split(':');
  if (kind === 'page') {
    const p = overview?.pages[Number(i)];
    if (p?.file) await load(p.name, p.file, 'context');
  } else {
    const h = hits[Number(i)];
    if (h?.file) await load(h.name, h.file, h.kind === 'endpoint' || h.kind === 'table' || h.kind === 'model' ? 'impact' : 'context');
  }
};

// ---------------------------------------------------------------- data

let searchTimer: ReturnType<typeof setTimeout> | undefined;
const runSearch = async (): Promise<void> => {
  clearTimeout(searchTimer);
  if (!query.trim()) { hits = []; paint(); return; }
  const seq = ++searchSeq;
  searchTimer = setTimeout(async () => {
    try {
      const r = await ask<{ hits: Hit[] }>('/search', { q: query });
      if (seq === searchSeq) { hits = r.hits; error = null; }
    } catch (e) {
      if (seq === searchSeq) error = (e as Error).message;
    }
    paint();
  }, 250);
};

const refresh = async (): Promise<void> => {
  if (!directory) { overview = null; paint(); return; }
  const dir = directory;
  try {
    const r = await ask<Overview>('/overview', {});
    if (dir === directory) { overview = r; error = null; }
  } catch (e) {
    if (dir === directory) error = (e as Error).message;
  }
  paint();
};

host.onReady((ctx) => applyHostReady(ctx, document.documentElement));
host.onDirectory((dir) => {
  if (dir === directory) return;
  directory = dir;
  overview = null; hits = []; detail = null; error = null;
  paint();
  void refresh();
  if (query.trim()) void runSearch();
});
// The agent may have changed the map: read it again when a turn ends.
host.onSessionLifecycle((ev) => { if (ev.phase === 'completed') void refresh(); });
paint();
