#!/usr/bin/env node
// A browser smoke test for the hosted panel. It builds weave, copies test/fixture to
// a temp repo, adds the capability spec next to this file, starts `weave serve`, and
// drives headless Chrome over CDP. No packages; needs Node 22+ (global WebSocket) and
// Chrome, Chromium or Edge.
//
//   node test/ui/panel.mjs           check and clean up
//   node test/ui/panel.mjs --keep    keep the temp repo and screenshots
//   node test/ui/panel.mjs --shots   save screenshots into the temp dir
//   node test/ui/panel.mjs --out DIR save the screenshots the README shows into DIR
//
// Exit 1 on the first failed check; every check prints a line either way.

import { execFileSync, spawn } from 'node:child_process';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..', '..');
const flags = new Set(process.argv.slice(2));
const keep = flags.has('--keep');
const outAt = process.argv.indexOf('--out');
const outDir = outAt > 0 ? path.resolve(process.argv[outAt + 1]) : '';
const shots = flags.has('--shots') || keep || !!outDir;

if (typeof WebSocket === 'undefined') {
  console.error('needs Node 22+ (global WebSocket)');
  process.exit(1);
}

const sleep = ms => new Promise(r => setTimeout(r, ms));
let failures = 0;
const check = (name, ok, detail = '') => {
  if (ok) console.log('  ok   ' + name);
  else { failures++; console.log('  FAIL ' + name + (detail ? ': ' + detail : '')); }
};

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.on('error', reject);
    s.listen(0, '127.0.0.1', () => {
      const port = s.address().port;
      s.close(() => resolve(port));
    });
  });
}

function chromePath() {
  const candidates = [
    process.env.CHROME,
    'C:/Program Files/Google/Chrome/Application/chrome.exe',
    'C:/Program Files (x86)/Google/Chrome/Application/chrome.exe',
    'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
    'C:/Program Files/Microsoft/Edge/Application/msedge.exe',
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
  ].filter(Boolean);
  const hit = candidates.find(p => fs.existsSync(p));
  if (!hit) throw new Error('Chrome not found; set CHROME to the executable');
  return hit;
}

function killTree(child) {
  if (!child || child.exitCode !== null) return;
  try {
    if (process.platform === 'win32') {
      execFileSync('taskkill', ['/PID', String(child.pid), '/T', '/F'], { stdio: 'ignore' });
    } else {
      child.kill('SIGKILL');
    }
  } catch { /* already gone */ }
}

class Page {
  static async connect(url) {
    const ws = new WebSocket(url);
    const page = new Page(ws);
    await new Promise((resolve, reject) => {
      ws.onopen = resolve;
      ws.onerror = reject;
    });
    return page;
  }

  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    this.errors = [];
    ws.onmessage = e => {
      const m = JSON.parse(e.data);
      if (m.id && this.pending.has(m.id)) {
        this.pending.get(m.id)(m);
        this.pending.delete(m.id);
      } else if (m.method === 'Runtime.exceptionThrown') {
        this.errors.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text);
      }
    };
  }

  send(method, params = {}) {
    return new Promise(resolve => {
      const id = ++this.id;
      this.pending.set(id, resolve);
      this.ws.send(JSON.stringify({ id, method, params }));
    });
  }

  async eval(expression) {
    const r = await this.send('Runtime.evaluate', { expression, returnByValue: true });
    if (r.result?.exceptionDetails) {
      throw new Error(r.result.exceptionDetails.exception?.description || r.result.exceptionDetails.text);
    }
    return r.result?.result?.value;
  }

  async waitFor(expression, ms = 8000) {
    const end = Date.now() + ms;
    while (Date.now() < end) {
      try {
        if (await this.eval(expression)) return true;
      } catch { /* navigation or a missing node: keep waiting */ }
      await sleep(120);
    }
    return false;
  }

  async shot(file) {
    const r = await this.send('Page.captureScreenshot', { format: 'png' });
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, Buffer.from(r.result.data, 'base64'));
  }
}

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'weave-ui-'));
let serve = null;
let chrome = null;
let page = null;

try {
  console.log('panel smoke: building');
  const bin = path.join(tmp, process.platform === 'win32' ? 'weave.exe' : 'weave');
  execFileSync('go', ['build', '-o', bin, './cmd/weave'], { cwd: repo, stdio: 'inherit' });

  const root = path.join(tmp, 'repo');
  fs.cpSync(path.join(repo, 'test', 'fixture'), root, { recursive: true });
  fs.rmSync(path.join(root, '.weave'), { recursive: true, force: true });
  const capDir = path.join(root, 'specs', 'capabilities', 'orders');
  fs.mkdirSync(capDir, { recursive: true });
  fs.copyFileSync(path.join(here, 'orders-spec.md'), path.join(capDir, 'spec.md'));

  const port = await freePort();
  serve = spawn(bin, ['serve', '--root', root, '--port', String(port)], { cwd: root, stdio: ['ignore', 'pipe', 'pipe'] });
  let out = '';
  serve.stdout.on('data', d => { out += d; });
  serve.stderr.on('data', d => { out += d; });
  let token = '';
  const tokenEnd = Date.now() + 20000;
  while (!token && Date.now() < tokenEnd) {
    const m = out.match(/t=([a-f0-9]+)/);
    if (m) token = m[1];
    else await sleep(100);
  }
  if (!token) throw new Error('serve did not print a token:\n' + out);

  const dbg = await freePort();
  chrome = spawn(chromePath(), [
    '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
    `--remote-debugging-port=${dbg}`, `--user-data-dir=${path.join(tmp, 'chrome')}`,
    '--window-size=1400,900', 'about:blank',
  ], { stdio: 'ignore' });

  let target = null;
  const debugEnd = Date.now() + 20000;
  while (!target && Date.now() < debugEnd) {
    try {
      const list = await (await fetch(`http://127.0.0.1:${dbg}/json/list`)).json();
      target = list.find(t => t.type === 'page');
    } catch { /* not listening yet */ }
    if (!target) await sleep(150);
  }
  if (!target) throw new Error('Chrome did not expose a page');

  page = await Page.connect(target.webSocketDebuggerUrl);
  await page.send('Runtime.enable');
  await page.send('Page.enable');
  await page.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: 'light' }] });

  const base = `http://127.0.0.1:${port}/?t=${token}`;
  const go = async hash => {
    await page.send('Page.navigate', { url: base + hash });
    await sleep(600);
  };

  // ---- The map itself, for the README: Today, an endpoint's line, the gate's list
  if (shots) {
    const shotDir = outDir || tmp;
    await go('#today');
    await page.waitFor(`!!document.querySelector('#detail h1, #detail .intro')`);
    await page.shot(path.join(shotDir, 'today.png'));
    await go('#node/endpoint%3AGET%20%2Fapi%2Fv1%2Forders');
    await page.waitFor(`!!document.querySelector('#detail .item')`);
    await page.shot(path.join(shotDir, 'endpoint.png'));
    await go('#checks');
    await page.waitFor(`!!document.querySelector('#list .lhead')`);
    await page.shot(path.join(shotDir, 'checks.png'));
  }

  // ---- Specs: menu place, views table, owner, rendered spec page
  await go('#specs');
  check('Specs opens', await page.waitFor(`!!document.querySelector('#wfullview .wcaps a.wcname')`));
  check('menu is Overview, the four lines, Checks, Changes, Specs, Wiki', await page.eval(
    `[...document.querySelectorAll('#tabs a')].map(a => a.textContent.replace(/\\d+$/, '')).join(',') === 'Overview,Screens,Endpoints,Modules,Tables,Checks,Changes,Specs,Wiki'`));
  check('Specs takes the full width', await page.eval(`document.querySelector('#main').classList.contains('wfull')`));
  check('Views table lists the screen', await page.eval(`document.querySelectorAll('.wviewtable tbody tr').length >= 1`));
  check('Views row names the owning spec', await page.eval(
    `[...document.querySelectorAll('.wviewtable .wvspec a')].some(a => a.textContent === 'orders')`));
  check('screen link points at the map node', await page.eval(
    `[...document.querySelectorAll('.wviewtable .wvname a')].some(a => a.getAttribute('href').startsWith('#node/page'))`));

  await page.eval(`document.querySelector('.wcaps a.wcname').click()`);
  check('capability opens as a page', await page.waitFor(`!!document.querySelector('.specdoc h1')`));
  check('spec page has its title', await page.eval(`document.querySelector('.specdoc h1').textContent.includes('Capability: orders')`));
  check('spec page renders the Parts table', await page.eval(`!!document.querySelector('.specdoc table')`));
  check('spec page has a raw link', await page.eval(`!!document.querySelector('.wspecbar .wopen')`));
  check('spec page offers edit', await page.eval(
    `[...document.querySelectorAll('.wspecbar .wopen')].some(b => b.textContent === 'edit')`));
  if (shots) await page.shot(path.join(outDir || tmp, 'spec-page.png'));

  await page.eval(`[...document.querySelectorAll('.wspecbar .wopen')].find(b => b.textContent === 'edit').click()`);
  check('edit opens the Markdown', await page.waitFor(`!!document.querySelector('.wspecedit')`));
  await page.eval(`(() => {
    const t = document.querySelector('.wspecedit');
    t.value += '\\n## Smoke\\n\\nEdited in the panel.\\n';
    document.querySelector('.wspecbar .wmark').click();
    return 'ok';
  })()`);
  check('save renders the new text', await page.waitFor(
    `!!document.querySelector('.specdoc') && document.querySelector('.specdoc').innerText.includes('Edited in the panel.')`));
  const specFile = fs.readFileSync(path.join(root, 'specs', 'capabilities', 'orders', 'spec.md'), 'utf8');
  check('save writes spec.md', specFile.includes('Edited in the panel.'));
  if (shots) await page.shot(path.join(outDir || tmp, 'spec-edited.png'));

  await page.eval(`document.querySelector('.wback').click()`);
  check('back returns to the list', await page.waitFor(`!!document.querySelector('.wcaps a.wcname')`));

  // ---- Check: the gate runs and its output lands in the dock
  await page.eval(`document.querySelector('.wgate').click()`);
  check('Check shows the gate verdict', await page.waitFor(
    `[...document.querySelectorAll('.wpane')].some(p => p.dataset.on === '1' && /Exit \\d/.test(p.innerText))`));
  check('Check opens the dock', await page.eval(`document.querySelector('.wdialog').open`));

  // ---- File: a path chip opens a window in the same dock
  await page.eval(`document.querySelector('.wdialog').close()`);
  await go('#node/page%3A%2Forders');
  check('page detail offers open', await page.waitFor(`!!document.querySelector('.path + .wopen')`));
  check('page detail links to VS Code and Cursor', await page.eval(
    `(() => { const h = [...document.querySelectorAll('a.openin')].map(a => a.getAttribute('href')); return h.length === 2 && h[0].startsWith('vscode://file/') && h[1].startsWith('cursor://file/') && h[0].includes('Orders.tsx') })()`));
  await page.eval(`document.querySelector('.path + .wopen').click()`);
  check('File shows the file window', await page.waitFor(`document.querySelector('.wdialog').open && !!document.querySelector('.wpane[data-on="1"] pre')`));
  check('File names the path', await page.eval(`document.querySelector('.wdialog b').textContent.endsWith('Orders.tsx')`));
  await page.eval(`document.querySelector('.wdialog').close()`);

  // ---- Wiki: built and shown in place
  await page.eval(`document.querySelector('#tabs a[href="#wiki"]').click()`);
  check('Wiki renders in the page', await page.waitFor(
    `!!document.querySelector('.wwikiframe') && !!document.querySelector('.wwikiframe').contentDocument.title`, 30000));
  check('Wiki marks the menu', await page.eval(
    `document.querySelector('#tabs a[href="#wiki"]').getAttribute('aria-current') === 'page'`));
  if (shots) await page.shot(path.join(outDir || tmp, 'wiki.png'));

  // ---- Tick: the write path rewrites tasks.md
  await page.eval(`document.querySelector('#tabs a[href="#specs"]').click()`);
  await page.waitFor(`!!document.querySelector('#wfullview .wtick[aria-pressed="false"]')`);
  const task = await page.eval(`document.querySelector('#wfullview .wtick[aria-pressed="false"]').dataset.task`);
  const sel = `[data-task=${JSON.stringify(task)}]`;
  await page.eval(`document.querySelector('#wfullview .wtick${sel}').click()`);
  check('tick flips in the panel', await page.waitFor(`!!document.querySelector('#wfullview .wtick${sel}[aria-pressed="true"]')`));
  const taskID = String(task).split(' ').pop();
  const tasksFile = fs.readFileSync(path.join(root, 'specs', 'changes', '2026-01-01-orders', 'tasks.md'), 'utf8');
  check('tick rewrites tasks.md', new RegExp(`- \\[x\\] ${taskID}\\b`).test(tasksFile), tasksFile.trim());
  if (shots) await page.shot(path.join(outDir || tmp, 'specs.png'));

  if (page.errors.length) {
    failures++;
    console.log('  FAIL page errors: ' + page.errors.join(' | '));
  }
} catch (err) {
  failures++;
  console.error('  FAIL ' + (err?.stack || err));
} finally {
  killTree(chrome);
  killTree(serve);
  if (keep) {
    console.log('kept ' + tmp);
  } else {
    await sleep(300);
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

console.log(failures === 0 ? 'panel smoke: ok' : `panel smoke: ${failures} failed`);
process.exit(failures === 0 ? 0 : 1);
