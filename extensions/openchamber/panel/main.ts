// The weave panel. Three tabs:
// - Map: the open project's screens, endpoints, modules and tables, a plain-word
//   search, and a page per item with what it links to, what it is, and what a
//   change to it reaches. Every item links to its neighbours.
// - Changes: the uncommitted change, checked by weave's gate, with what it reaches
//   and a button that hands the failures to the agent.
// - Settings: which weave runs, updating it, and what the extension does on its own.
// Every answer comes from the extension's local service.
import { connectHost, HostRequestError, type GuestItem } from '@openchamber/sdk';
import {
  applyHostReady, mountBadge, mountBanner, mountButton, mountEmpty, mountList, mountSearchField, mountSpinner,
  mountSwitch, mountTabs,
  type ListItem, type Tone,
} from '@openchamber/sdk/ui';

type Kind = 'page' | 'endpoint' | 'module' | 'table';
type Entry = { id: string; name: string; file: string; meta: string };
type Overview = { screens: number; endpoints: number; modules: number; tables: number; lists: Record<Kind, Entry[]>; error?: string };
type Hit = { kind: string; name: string; file: string };
type Ref = { id: string; kind: string; name: string; file: string };
type NodeView = { id: string; kind: string; name: string; file: string; line: number | null; desc: string; groups: { label: string; items: Ref[]; total: number }[] };
type Fail = { label: string; closest: string; at: string; text: string };
type Gate = { ok: boolean; head: string; fails: Fail[]; warns: string[]; tests: string[]; out: string; error?: string };
type Changes = { files: { file: string; state: string }[]; gate: Gate; impact: string; error?: string };
type About = {
  weave: { bin: string | null; version: string | null; source: 'path' | 'download' | null; status: string; error: string | null };
  settings: { autoGate: boolean; agentInstructions: boolean };
  platform: string;
  asset: string | null;
  updated?: boolean; latest?: string | null; reason?: string; error?: string;
};

// A page in the Map tab: a node of the graph, or a plain file.
type Detail = {
  title: string; kind: string; file: string; id: string | null;
  node: NodeView | null; mode: 'context' | 'impact'; text: string | null;
};

const host = connectHost();
const root = document.querySelector<HTMLElement>('#root')!;

const style = document.createElement('style');
style.textContent = (`
  body { margin: 0; font-family: var(--oc-font); color: var(--oc-fg); background: var(--oc-bg); }
  .w { display: flex; flex-direction: column; gap: 10px; padding: 10px 12px 12px; height: 100vh; box-sizing: border-box; }
  .w-body { flex: 1; min-height: 0; display: flex; flex-direction: column; gap: 10px; overflow: auto; }
  .w-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; }
  .w-stat { all: unset; cursor: pointer; border: 1px solid var(--oc-border); border-radius: var(--oc-radius); padding: 6px 8px; }
  .w-stat:hover { background: var(--oc-muted-surface); }
  .w-stat[aria-pressed="true"] { border-color: var(--oc-primary, var(--oc-fg)); background: var(--oc-muted-surface); }
  .w-stat b { display: block; font-size: 15px; font-variant-numeric: tabular-nums; }
  .w-stat span { font-size: 11px; color: var(--oc-muted); }
  .w-list { flex: 1; min-height: 120px; overflow: auto; }
  .w-head { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .w-title { flex: 1; min-width: 0; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .w-file { font: 11px var(--oc-mono); color: var(--oc-muted); overflow-wrap: anywhere; }
  .w-desc { font-size: 12px; color: var(--oc-muted); }
  .w-h { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .04em; color: var(--oc-muted); margin: 4px 0 -4px; }
  .w-pre { margin: 0; padding: 8px; border: 1px solid var(--oc-border); border-radius: var(--oc-radius); background: var(--oc-muted-surface);
           font: 12px/1.45 var(--oc-mono); white-space: pre-wrap; overflow-wrap: anywhere; max-height: 50vh; overflow: auto; }
  .w-row { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
  .w-chips { display: flex; flex-wrap: wrap; gap: 4px; }
  .w-chip { all: unset; cursor: pointer; font: 11.5px var(--oc-mono); padding: 2px 7px; border: 1px solid var(--oc-border);
            border-radius: 999px; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .w-chip:hover { background: var(--oc-muted-surface); }
  .w-fail { border: 1px solid var(--oc-border); border-left: 3px solid var(--oc-error, #e5484d); border-radius: var(--oc-radius); padding: 6px 8px; }
  .w-fail b { font: 600 12px var(--oc-mono); }
  .w-fail div { font-size: 12px; color: var(--oc-muted); overflow-wrap: anywhere; }
  .w-kv { display: grid; grid-template-columns: auto 1fr; gap: 4px 10px; font-size: 12px; }
  .w-kv span:nth-child(odd) { color: var(--oc-muted); }
  .w-kv span:nth-child(even) { overflow-wrap: anywhere; font-family: var(--oc-mono); }
`);
document.head.append(style);

const el = <K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] => {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
};
const slot = (): HTMLDivElement => el('div');
const button = (parent: Element, label: string, onClick: () => void, variant?: 'outline' | 'ghost'): void => {
  const s = slot();
  mountButton(s, { label, size: 'sm', ...(variant ? { variant } : {}), onClick });
  parent.append(s);
};

// ---------------------------------------------------------------- state

let directory: string | null = null;
let tab: 'map' | 'changes' | 'settings' = 'map';
let kind: Kind = 'page';
let query = '';
let overview: Overview | null = null;
let hits: Hit[] = [];
let stack: Detail[] = []; // Map tab pages, newest last
let changes: Changes | null = null;
let changesLoading = false;
let about: About | null = null;
let error: string | null = null;
let searchSeq = 0;

const call = async <T>(method: 'GET' | 'POST', path: string, query: Record<string, string> = {}, body?: unknown): Promise<T> => {
  try {
    const r = await host.serviceRequest({ method, path, query: { directory: directory ?? '', ...query }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    const out = typeof r.body === 'string' ? JSON.parse(r.body) : r.body;
    if (r.status >= 400) throw new Error(out?.message ?? `weave answered ${r.status}`);
    return out as T;
  } catch (e) {
    if (e instanceof HostRequestError && e.code === 'NO_SERVICE') throw new Error('Allow the local service for weave in Settings → Extensions.');
    throw e instanceof Error ? e : new Error(String(e));
  }
};
const get = <T>(path: string, q: Record<string, string> = {}): Promise<T> => call<T>('GET', path, q);

const toChat = (text: string): void => {
  void host.compose({ text, mode: 'append' }).catch(() => host.toast({ kind: 'error', message: 'Could not add to the chat input.' }));
};
const copy = (text: string): void => {
  void host.writeClipboard(text.slice(0, 32000)).then(() => host.toast({ kind: 'success', message: 'Copied' }));
};
const fenced = (title: string, text: string): string => `${title}\n\n\`\`\`\n${text}\n\`\`\`\n`;

// ---------------------------------------------------------------- layout

const wrap = el('div', 'w');
root.append(wrap);
const tabsSlot = slot();
const bannerSlot = slot();
const body = el('div', 'w-body');
wrap.append(tabsSlot, bannerSlot, body);

const tabs = mountTabs(tabsSlot, {
  items: [{ id: 'map', label: 'Map' }, { id: 'changes', label: 'Changes' }, { id: 'settings', label: 'Settings' }],
  activeId: tab, trackBackground: true,
  onChange: (id) => {
    tab = id as typeof tab;
    if (tab === 'changes') void loadChanges();
    if (tab === 'settings') void loadAbout();
    paint();
  },
});

let banner: ReturnType<typeof mountBanner> | null = null;
const paintBanner = (): void => {
  banner?.dispose();
  banner = null;
  const w = about?.weave;
  if (!directory && tab !== 'settings') {
    banner = mountBanner(bannerSlot, { tone: 'info', title: 'Open a project to see its map.' });
  } else if (w?.status === 'downloading') {
    banner = mountBanner(bannerSlot, { tone: 'info', title: 'Getting weave ready', body: 'A one-time download of the weave binary for this machine.' });
  } else if (w?.status === 'error') {
    banner = mountBanner(bannerSlot, { tone: 'error', title: w.error ?? 'weave is not available.', action: { label: 'Try again', onClick: () => void refresh() } });
  } else if (error ?? overview?.error) {
    banner = mountBanner(bannerSlot, { tone: 'error', title: (error ?? overview?.error)! });
  }
};

const paint = (): void => {
  tabs.update({
    activeId: tab,
    items: [
      { id: 'map', label: 'Map' },
      { id: 'changes', label: 'Changes', ...(changes && !changes.error && !changes.gate.error && changes.gate.fails.length ? { count: changes.gate.fails.length } : {}) },
      { id: 'settings', label: 'Settings' },
    ],
  });
  paintBanner();
  body.replaceChildren();
  if (tab === 'map') paintMap();
  else if (tab === 'changes') paintChanges();
  else paintSettings();
};

// ---------------------------------------------------------------- Map

const KINDS: [Kind, string][] = [['page', 'screens'], ['endpoint', 'endpoints'], ['module', 'modules'], ['table', 'tables']];
const COUNT: Record<Kind, 'screens' | 'endpoints' | 'modules' | 'tables'> = { page: 'screens', endpoint: 'endpoints', module: 'modules', table: 'tables' };
const KIND_LABEL: Record<string, string> = { page: 'screen', endpoint: 'endpoint', module: 'module', table: 'table', model: 'model', test: 'test', file: 'file' };
const impactFirst = new Set(['endpoint', 'table', 'model']);

let searchField: HTMLDivElement | null = null;
const searchSlot = slot();
mountSearchField(searchSlot, {
  value: '', placeholder: 'Search the map: "inbox count", /api/users, orders', label: 'Search the map',
  onChange: (v) => { query = v; void runSearch(); },
});
searchField = searchSlot;

const listSlot = el('div', 'w-list');
const list = mountList(listSlot, { items: [], onSelect: (id) => void openListItem(id), emptyText: '', ariaLabel: 'weave map' });

const mapItems = (): ListItem[] => {
  if (query.trim()) return hits.map((h, i) => ({ id: `hit:${i}`, title: h.name, leading: (KIND_LABEL[h.kind] ?? h.kind).slice(0, 8), subtitle: h.kind === 'test' ? undefined : h.file }));
  return (overview?.lists?.[kind] ?? []).map((e, i) => ({ id: `entry:${i}`, title: e.name, subtitle: e.file, meta: e.meta || undefined }));
};

const paintMap = (): void => {
  const top = stack[stack.length - 1];
  if (top) { paintDetail(top); return; }
  if (overview && !overview.error) {
    const stats = el('div', 'w-stats');
    for (const [k, label] of KINDS) {
      const b = el('button', 'w-stat');
      b.setAttribute('aria-pressed', String(!query.trim() && kind === k));
      b.append(el('b', undefined, String(overview[COUNT[k]])), el('span', undefined, label));
      b.onclick = () => { kind = k; if (query) { query = ''; searchSlot.querySelector('input')!.value = ''; } paint(); };
      stats.append(b);
    }
    body.append(stats);
  } else if (directory && !error) {
    const s = slot();
    mountSpinner(s, {});
    body.append(s);
  }
  body.append(searchField!, listSlot);
  list.update({
    items: mapItems(),
    emptyText: query.trim() ? 'Nothing matched. Try fewer or other words.' : overview ? `No ${COUNT[kind]} found in this project.` : '',
  });
};

const openListItem = async (id: string): Promise<void> => {
  const [src, i] = id.split(':');
  if (src === 'hit') {
    const h = hits[Number(i)];
    if (h) await openRef({ id: `${h.kind}:${h.name}`, kind: h.kind, name: h.name, file: h.file });
  } else {
    const e = overview?.lists?.[kind]?.[Number(i)];
    if (e) await openRef({ id: e.id, kind, name: e.name, file: e.file });
  }
};

const openRef = async (r: Ref, mode?: 'context' | 'impact'): Promise<void> => {
  const d: Detail = { title: r.name, kind: r.kind, file: r.file, id: r.kind === 'file' ? null : r.id, node: null, mode: mode ?? (impactFirst.has(r.kind) ? 'impact' : 'context'), text: null };
  stack.push(d);
  paint();
  if (d.id) {
    void get<NodeView>('/node', { id: d.id }).then((n) => { d.node = n; if (!d.file) d.file = n.file; paint(); }).catch(() => { d.id = null; paint(); });
  }
  await loadText(d);
};

const loadText = async (d: Detail): Promise<void> => {
  d.text = null;
  paint();
  if (!d.file) { d.text = ''; paint(); return; }
  try {
    const r = await get<{ text: string }>(`/${d.mode}`, { file: d.file });
    d.text = r.text || '(nothing)';
  } catch (e) {
    d.text = (e as Error).message;
  }
  paint();
};

const paintDetail = (d: Detail): void => {
  const head = el('div', 'w-head');
  button(head, '← Back', () => { stack.pop(); paint(); }, 'ghost');
  const badge = slot();
  mountBadge(badge, { label: KIND_LABEL[d.kind] ?? d.kind, tone: (d.kind === 'endpoint' ? 'info' : d.kind === 'page' ? 'primary' : 'neutral') as Tone });
  head.append(el('div', 'w-title', d.title), badge);
  body.append(head);
  if (d.file) body.append(el('div', 'w-file', d.node?.line ? `${d.file}:${d.node.line}` : d.file));
  if (d.node?.desc) body.append(el('div', 'w-desc', d.node.desc));

  for (const g of d.node?.groups ?? []) {
    body.append(el('div', 'w-h', g.total > g.items.length ? `${g.label} (${g.items.length} of ${g.total})` : `${g.label} (${g.total})`));
    const chips = el('div', 'w-chips');
    for (const item of g.items) {
      const c = el('button', 'w-chip', item.name);
      c.title = `${KIND_LABEL[item.kind] ?? item.kind} · ${item.file}`;
      c.onclick = () => void openRef(item);
      chips.append(c);
    }
    body.append(chips);
  }

  if (!d.file) return;
  const t = slot();
  mountTabs(t, {
    items: [{ id: 'context', label: 'What it is' }, { id: 'impact', label: 'What a change reaches' }],
    activeId: d.mode, onChange: (id) => { d.mode = id as Detail['mode']; void loadText(d); },
  });
  body.append(t);
  if (d.text === null) {
    const s = slot();
    mountSpinner(s, {});
    body.append(s);
    return;
  }
  body.append(el('pre', 'w-pre', d.text));
  const row = el('div', 'w-row');
  button(row, 'Add to chat', () => toChat(fenced(`weave ${d.mode} for ${d.title} (${d.file}):`, d.text!)));
  button(row, 'Copy', () => copy(d.text!), 'outline');
  body.append(row);
};

let searchTimer: ReturnType<typeof setTimeout> | undefined;
const runSearch = async (): Promise<void> => {
  clearTimeout(searchTimer);
  if (!query.trim()) { hits = []; paint(); return; }
  const seq = ++searchSeq;
  searchTimer = setTimeout(async () => {
    try {
      const r = await get<{ hits: Hit[] }>('/search', { q: query });
      if (seq === searchSeq) { hits = r.hits; error = null; }
    } catch (e) {
      if (seq === searchSeq) error = (e as Error).message;
    }
    if (tab === 'map' && !stack.length) {
      // Repaint the list only, so typing keeps its focus.
      list.update({ items: mapItems(), emptyText: 'Nothing matched. Try fewer or other words.' });
      body.querySelectorAll('.w-stat').forEach((b) => b.setAttribute('aria-pressed', String(!query.trim() && b.textContent?.endsWith(COUNT[kind]))));
      paintBanner();
    }
  }, 250);
};

// ---------------------------------------------------------------- Changes

const fixPrompt = (g: Gate): string => [
  'weave check says this change broke these front-end calls. Fix each one: point the call at the right route, or bring the route back.',
  ...g.fails.map((f) => `- ${f.text}`),
  ...(g.tests.length ? ['', 'Then run:', ...g.tests.map((t) => `  ${t}`)] : []),
].join('\n');

const STATE_TONE: Record<string, Tone> = { new: 'success', added: 'success', deleted: 'error', renamed: 'info', changed: 'warning' };

const paintChanges = (): void => {
  if (!directory) return;
  const head = el('div', 'w-row');
  button(head, changesLoading ? 'Checking…' : 'Re-run', () => void loadChanges(true), 'outline');
  body.append(head);
  if (!changes) {
    const s = slot();
    mountSpinner(s, {});
    body.append(s);
    return;
  }
  if (changes.error) {
    const s = slot();
    mountEmpty(s, { title: changes.error });
    body.append(s);
    return;
  }
  const g = changes.gate;
  const verdict = slot();
  if (g.error) {
    mountBanner(verdict, { tone: 'error', title: g.error });
  } else if (g.ok) {
    mountBanner(verdict, { tone: 'success', title: 'The gate passes', body: g.head || 'Nothing in this change breaks a front-end call.' });
  } else {
    mountBanner(verdict, {
      tone: 'error', title: `${g.fails.length || 1} broken call${g.fails.length === 1 ? '' : 's'}`, body: g.head,
      action: g.fails.length ? { label: 'Ask the agent to fix', onClick: () => toChat(fixPrompt(g)) } : undefined,
    });
  }
  body.append(verdict);
  for (const f of g.fails ?? []) {
    const box = el('div', 'w-fail');
    box.append(el('b', undefined, f.label), el('div', undefined, f.closest ? `closest now: ${f.closest}` : 'no route serves it'));
    if (f.at) {
      const at = el('button', 'w-chip', f.at);
      at.style.marginTop = '4px';
      at.onclick = () => void openFile(f.at.replace(/:\d+$/, ''));
      box.append(at);
    }
    body.append(box);
  }
  if (g.tests?.length) {
    body.append(el('div', 'w-h', 'Tests to run'));
    body.append(el('pre', 'w-pre', g.tests.join('\n')));
  }
  for (const w of g.warns ?? []) body.append(el('div', 'w-desc', w));

  body.append(el('div', 'w-h', `Uncommitted files (${changes.files.length})`));
  if (!changes.files.length) {
    body.append(el('div', 'w-desc', 'No uncommitted change.'));
    return;
  }
  const filesSlot = el('div');
  mountList(filesSlot, {
    items: changes.files.slice(0, 200).map((f, i) => ({ id: String(i), title: f.file, badge: { label: f.state, tone: STATE_TONE[f.state] ?? 'neutral' }, disabled: f.state === 'deleted' })),
    onSelect: (id) => void openFile(changes!.files[Number(id)].file),
    ariaLabel: 'Uncommitted files',
  });
  body.append(filesSlot);
  if (changes.impact) {
    body.append(el('div', 'w-h', 'What the change reaches'));
    body.append(el('pre', 'w-pre', changes.impact));
    const row = el('div', 'w-row');
    button(row, 'Add to chat', () => toChat(fenced('weave impact of my uncommitted change:', changes!.impact)));
    button(row, 'Copy', () => copy(changes!.impact), 'outline');
    body.append(row);
  }
};

const openFile = async (file: string, mode?: 'context' | 'impact'): Promise<void> => {
  tab = 'map';
  stack = [];
  await openRef({ id: `file:${file}`, kind: 'file', name: file.split('/').pop() ?? file, file }, mode);
};

let changesSeq = 0;
const loadChanges = async (force = false): Promise<void> => {
  if (!directory || (changesLoading && !force)) return;
  const seq = ++changesSeq;
  changesLoading = true;
  if (force) changes = null;
  paint();
  try {
    const r = await get<Changes>('/changes');
    if (seq === changesSeq) changes = r;
  } catch (e) {
    if (seq === changesSeq) changes = { files: [], gate: { error: (e as Error).message } as Gate, impact: '' };
  }
  if (seq === changesSeq) changesLoading = false;
  paint();
};

// ---------------------------------------------------------------- Settings

const paintSettings = (): void => {
  const a = about;
  if (!a) {
    const s = slot();
    mountSpinner(s, {});
    body.append(s);
    return;
  }
  body.append(el('div', 'w-h', 'weave'));
  const kv = el('div', 'w-kv');
  const w = a.weave;
  const rows: [string, string][] = [
    ['version', w.version ? `v${w.version}` : w.status],
    ['from', w.source === 'download' ? 'downloaded by this extension' : w.source === 'path' ? 'your install (PATH)' : '—'],
    ['binary', w.bin ?? '—'],
    ['machine', a.platform],
  ];
  for (const [k, v] of rows) kv.append(el('span', undefined, k), el('span', undefined, v));
  body.append(kv);
  const row = el('div', 'w-row');
  if (w.source === 'download') button(row, 'Check for an update', () => void update(), 'outline');
  if (w.source === 'path') row.append(el('div', 'w-desc', 'Your own weave is used because it is new enough. Update it the way you installed it.'));
  if (a.updated !== undefined) row.append(el('div', 'w-desc', a.updated ? `Updated to ${a.latest}.` : a.error ? `Update failed: ${a.error}` : a.reason ?? `Up to date${a.latest ? ` (${a.latest})` : ''}.`));
  body.append(row);

  body.append(el('div', 'w-h', 'Agents'));
  const s1 = slot();
  mountSwitch(s1, {
    label: 'Tell agents weave is here',
    description: 'Every session reads a short note on when to use weave and how to run it.',
    checked: a.settings.agentInstructions, onChange: (v) => void save({ agentInstructions: v }),
  });
  const s2 = slot();
  mountSwitch(s2, {
    label: 'Check each agent turn',
    description: 'When a turn changes the code and breaks a front-end call, tell that session what broke, once per failure.',
    checked: a.settings.autoGate, onChange: (v) => void save({ autoGate: v }),
  });
  body.append(s1, s2);
  body.append(el('div', 'w-desc', 'In the chat, type @ and pick a row from "weave map" to give the agent a screen, endpoint or table with its impact.'));
};

const loadAbout = async (): Promise<void> => {
  try {
    about = await get<About>('/about');
  } catch (e) {
    error = (e as Error).message;
  }
  paint();
  // A first start downloads weave: follow it until it is ready.
  if (about?.weave.status === 'downloading' || about?.weave.status === 'starting') {
    setTimeout(() => void loadAbout().then(() => { if (about?.weave.status === 'ready') void refresh(); }), 2000);
  }
};
const save = async (next: Partial<About['settings']>): Promise<void> => {
  try { about = await call<About>('POST', '/settings', {}, next); } catch (e) { host.toast({ kind: 'error', message: (e as Error).message }); }
  paint();
};
const update = async (): Promise<void> => {
  host.toast({ kind: 'info', message: 'Checking for a new weave…' });
  try { about = await call<About>('POST', '/update', {}, {}); } catch (e) { host.toast({ kind: 'error', message: (e as Error).message }); }
  if (about?.updated) void refresh();
  paint();
};

// ---------------------------------------------------------------- data

const refresh = async (): Promise<void> => {
  void loadAbout();
  if (!directory) { overview = null; paint(); return; }
  const dir = directory;
  try {
    const r = await get<Overview>('/overview');
    if (dir === directory) { overview = r; error = null; }
  } catch (e) {
    if (dir === directory) error = (e as Error).message;
  }
  if (tab === 'changes') void loadChanges();
  paint();
};

// "weave: what a change reaches" on a file in the Files view.
const onItem = (item: GuestItem | null): void => {
  if (!item || item.kind !== 'file' || item.isDirectory) return;
  const base = (item.directory ?? directory ?? '').replace(/[\\/]+$/, '');
  const rel = base && item.path.startsWith(base) ? item.path.slice(base.length).replace(/^[\\/]+/, '').replace(/\\/g, '/') : '';
  if (rel) void openFile(rel, 'impact');
};

host.onReady((ctx) => {
  applyHostReady(ctx, document.documentElement);
  if (ctx.item) onItem(ctx.item);
});
host.onItem(onItem);
host.onDirectory((dir) => {
  if (dir === directory) return;
  directory = dir;
  overview = null; hits = []; stack = []; changes = null; error = null;
  paint();
  void refresh();
  if (query.trim()) void runSearch();
});
// The agent may have changed the code: read the map and the change again when a turn ends.
host.onSessionLifecycle((ev) => { if (ev.phase === 'completed') void refresh(); });
paint();
void loadAbout();
