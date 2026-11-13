#!/usr/bin/env node
// Draws docs/img/chart-*.svg from bench/results.json.
//   node bench/charts.mjs
// Static SVG, readable in GitHub's light and dark themes. Two series at most:
// weave (blue) and the alternative (orange); every bar carries its own value.

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const outDir = path.join(here, '..', 'docs', 'img');
const R = JSON.parse(fs.readFileSync(path.join(here, 'results.json'), 'utf8'));
const repo = name => R.repos.find(r => r.name.startsWith(name));
const fmt = n => Math.round(n).toLocaleString('en-US');
const esc = s => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

const W = 1200, PAD = 40, LABEL = 170, VALUE = 300;
const style = `
  <style>
    :root { --bg:#ffffff; --ink:#131a26; --ink2:#3a4454; --ink3:#5a6474; --rule:#e3e7ee; --weave:#2456c7; --alt:#c2621c; --none:#d5dae3; }
    @media (prefers-color-scheme: dark) {
      :root { --bg:#0d1117; --ink:#e8ecf3; --ink2:#c3cad6; --ink3:#8f9bb0; --rule:#222b3a; --weave:#5b86e6; --alt:#cf7434; --none:#2a3344; }
    }
    .bg { fill: var(--bg); }
    .title { font: 600 22px system-ui, -apple-system, "Segoe UI", sans-serif; fill: var(--ink); }
    .sub { font: 400 14px system-ui, -apple-system, "Segoe UI", sans-serif; fill: var(--ink3); }
    .group { font: 600 15px system-ui, -apple-system, "Segoe UI", sans-serif; fill: var(--ink); }
    .label { font: 400 14px system-ui, -apple-system, "Segoe UI", sans-serif; fill: var(--ink2); }
    .value { font: 600 14px ui-monospace, "SF Mono", Menlo, Consolas, monospace; fill: var(--ink); font-variant-numeric: tabular-nums; }
    .note { font: 400 13px system-ui, -apple-system, "Segoe UI", sans-serif; fill: var(--ink3); }
    .rule { stroke: var(--rule); stroke-width: 1; }
    .w { fill: var(--weave); } .a { fill: var(--alt); } .n { fill: var(--none); }
  </style>`;

// A bar whose data end is rounded and whose base sits square on the axis.
const bar = (x, y, w, h, cls) => {
  w = Math.max(w, 3);
  const r = Math.min(4, w / 2, h / 2);
  return `<path class="${cls}" d="M${x},${y} h${w - r} a${r},${r} 0 0 1 ${r},${r} v${h - 2 * r} a${r},${r} 0 0 1 -${r},${r} h-${w - r} z"/>`;
};

function legend(y, items) {
  let x = PAD, s = '';
  for (const [cls, text] of items) {
    s += `<rect class="${cls}" x="${x}" y="${y - 11}" width="14" height="14" rx="3"/><text class="label" x="${x + 22}" y="${y}">${esc(text)}</text>`;
    x += 34 + text.length * 7.6;
  }
  return s;
}

// groups: [{ name, note?, bars: [{ cls, label, value, text }] }]
function hbars({ file, title, sub, groups, max, legendItems, footer, alt }) {
  const barH = 20, gap = 10, groupGap = 30;
  let y = 72 + (sub ? 24 : 0) + (legendItems ? 34 : 0);
  let body = '';
  const plotW = W - PAD * 2 - LABEL - VALUE;
  for (const g of groups) {
    // the group's name and note on their own line, its bars below
    body += `<text class="group" x="${PAD}" y="${y + 6}">${esc(g.name)}${g.note ? `<tspan class="note" dx="12">${esc(g.note)}</tspan>` : ''}</text>`;
    let by = y + 20;
    for (const b of g.bars) {
      const x0 = PAD + LABEL;
      const w = Math.max((b.value / max) * plotW, 3);
      body += `<text class="label" x="${x0 - 14}" y="${by + 15}" text-anchor="end">${esc(b.label)}</text>`;
      body += `<line class="rule" x1="${x0}" x2="${x0}" y1="${by - 3}" y2="${by + barH + 3}"/>`;
      body += bar(x0, by, w, barH, b.cls);
      body += `<text class="value" x="${x0 + w + 10}" y="${by + 15}">${esc(b.text)}</text>`;
      by += barH + gap;
    }
    y = by + groupGap;
  }
  if (footer) { body += `<text class="note" x="${PAD}" y="${y}">${esc(footer)}</text>`; y += 24; }
  const H = y + 8;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="${esc(alt)}">
${style}
  <rect class="bg" x="0" y="0" width="${W}" height="${H}" rx="12"/>
  <text class="title" x="${PAD}" y="48">${esc(title)}</text>
  ${sub ? `<text class="sub" x="${PAD}" y="74">${esc(sub)}</text>` : ''}
  ${legendItems ? legend(72 + (sub ? 34 : 10), legendItems) : ''}
  ${body}
</svg>
`;
  fs.writeFileSync(path.join(outDir, file), svg);
  console.error('wrote docs/img/' + file);
}

const doc = repo('documenso'), rw = repo('RealWorld (Express'), nest = repo('RealWorld (Nest');
const pct = (a, b) => b ? Math.round((a / b) * 100) : 0;

// 1. Can a search for the URL find the endpoint?
{
  const rows = [doc, nest, rw].filter(Boolean);
  hbars({
    file: 'chart-grep.svg',
    title: 'Find the code behind a URL',
    sub: 'Endpoints whose full URL appears anywhere in the source, so grep can find them',
    legendItems: [['a', 'grep for the URL'], ['w', 'weave search']],
    max: 100,
    groups: rows.map(r => ({
      name: r.name.replace(' (', ' · ').replace(')', ''),
      note: `${fmt(r.endpoints)} endpoints · ${r.stack}`,
      bars: [
        { cls: 'a', label: 'grep', value: pct(r.grep.found, r.grep.of), text: `${pct(r.grep.found, r.grep.of)}%  (${r.grep.found} of ${r.grep.of})` },
        { cls: 'w', label: 'weave', value: 100, text: `100%  (${r.endpoints} of ${r.endpoints})` },
      ],
    })),
    footer: 'tRPC procedures, Remix file routes and Nest decorators build the URL from pieces; no file spells it out.',
    alt: rows.map(r => `${r.name}: grep finds ${r.grep.found} of ${r.grep.of} endpoints, weave all ${r.endpoints}`).join('; '),
  });
}

// 2. What it costs to understand one screen
{
  const rows = [doc, rw].filter(Boolean);
  const max = Math.max(...rows.map(r => r.screens.byHand));
  hbars({
    file: 'chart-cost.svg',
    title: 'Tokens to understand one screen',
    sub: 'Median over every screen: its endpoints, handlers, tables and tests',
    legendItems: [['a', 'reading the files yourself'], ['w', 'one weave context call']],
    max,
    groups: rows.map(r => ({
      name: r.name.replace(' (', ' · ').replace(')', ''),
      note: `${r.screens.n} screens · ${r.screens.files} files per screen (median) · the screen's own file: ${fmt(r.screens.file)} tokens`,
      bars: [
        { cls: 'a', label: 'read the files', value: r.screens.byHand, text: fmt(r.screens.byHand) + ' tokens' },
        { cls: 'w', label: 'weave context', value: r.screens.weave, text: `${fmt(r.screens.weave)} tokens  (${Math.round(r.screens.byHand / r.screens.weave)}× less)` },
      ],
    })),
    footer: 'Tokens = bytes ÷ 4. "The files" = the screen, the files that declare its endpoints, their tests and the models they use.',
    alt: rows.map(r => `${r.name}: reading the files ${r.screens.byHand} tokens, weave context ${r.screens.weave}`).join('; '),
  });
}

// 3. Speed
{
  const rows = [doc, rw, nest].filter(Boolean);
  const max = Math.max(...rows.map(r => r.checkMs));
  hbars({
    file: 'chart-speed.svg',
    title: 'Fast enough to run on every agent turn',
    sub: 'weave check: re-index the repository and run the gate. Median wall time, one static binary',
    max,
    groups: [{
      name: 'weave check',
      bars: rows.map(r => ({
        cls: 'w',
        label: r.name.replace('RealWorld (', 'RW ').replace(')', ''),
        value: r.checkMs,
        text: `${fmt(r.checkMs)} ms  (${fmt(r.files)} files)`,
      })),
    }],
    alt: rows.map(r => `${r.name} (${r.files} files): weave check ${r.checkMs} ms`).join('; '),
  });
}

// 4. The gate: every called route broken three ways on six back ends, plus a
// harmless edit that must pass
{
  const stacks = R.repos.filter(r => r.mutations?.kinds);
  if (stacks.length) {
    const kinds = [['rename', 'Rename the route'], ['remove', 'Delete the route'], ['method', 'Change its method'], ['control', 'Harmless edit, must pass']];
    let tried = 0, caught = 0, ctried = 0, cfail = 0;
    for (const r of stacks) for (const [k] of kinds) {
      const v = r.mutations.kinds[k];
      if (!v) continue;
      if (k === 'control') { ctried += v.tried; cfail += v.caught; } else { tried += v.tried; caught += v.named; }
    }
    const colW = 230, rowH = 46, x0 = PAD + 190, headH = 40;
    let body = '';
    let y = 150;
    kinds.forEach(([, label], j) => { body += `<text class="group" x="${x0 + j * colW}" y="${y - 16}">${esc(label)}</text>`; });
    const section = (title, rows) => {
      if (!rows.length) return;
      body += `<text class="note" x="${PAD}" y="${y + 18}">${esc(title)}</text>`;
      y += headH - 10;
      for (const r of rows) {
        body += `<text class="label" x="${PAD}" y="${y + 22}">${esc(r.name.replace(/^RealWorld \((.*)\)$/, '$1'))}</text>`;
        kinds.forEach(([k], j) => {
          const v = r.mutations.kinds[k], x = x0 + j * colW, w = colW - 90;
          if (!v) { body += `<text class="note" x="${x}" y="${y + 22}">no verb in the route</text>`; return; }
          const good = k === 'control' ? v.tried - v.caught : v.named;
          body += `<rect class="n" x="${x}" y="${y + 8}" width="${w}" height="20" rx="4"/>`;
          body += bar(x, y + 8, w * good / v.tried, 20, 'w');
          body += `<text class="value" x="${x + w + 10}" y="${y + 23}">${good}/${v.tried}</text>`;
        });
        y += rowH;
      }
      y += 10;
    };
    section('The RealWorld React front end against six back ends', stacks.filter(r => !r.real));
    section('Real products, as published', stacks.filter(r => r.real));
    const H = y + 70;
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="Breaking each route a front end calls, on ${stacks.length} apps: weave check failed and named the caller ${caught} of ${tried} times; ${ctried - cfail} of ${ctried} harmless edits passed.">
${style}
  <rect class="bg" x="0" y="0" width="${W}" height="${H}" rx="12"/>
  <text class="title" x="${PAD}" y="48">Break a route, and the gate names who still calls it</text>
  <text class="sub" x="${PAD}" y="74">Every route a front end calls, broken one at a time on ${stacks.length} apps, then weave check</text>
  <text class="value" x="${PAD}" y="104">${caught} of ${tried} breaks failed with the caller's file:line · ${cfail} false failures in ${ctried} harmless edits</text>
  ${body}
  <text class="note" x="${PAD}" y="${H - 38}">Most misses are a route whose caller then lands on a neighbour: /articles/feed on /articles/:slug, or a Next.js catch-all ([...nextauth]).</text>
  <text class="note" x="${PAD}" y="${H - 16}">The harmless edit is a blank line under the route. No type checker sees any of this: a route and its callers only share a string.</text>
</svg>
`;
    fs.writeFileSync(path.join(outDir, 'chart-gate.svg'), svg);
    console.error('wrote docs/img/chart-gate.svg');
  }
}

// 5. Schema cost per session
if (R.schema) {
  const others = [['GitHub', 18000], ['Notion', 9000], ['filesystem', 3500]];
  hbars({
    file: 'chart-schema.svg',
    title: 'Context every session pays before the first prompt',
    sub: 'MCP tool schema, in tokens. weave measured; the others as published by their maintainers',
    max: 18000,
    groups: [{
      name: 'MCP servers',
      bars: [
        { cls: 'w', label: `weave (${R.schema.tools} tools)`, value: R.schema.tokens, text: fmt(R.schema.tokens) },
        ...others.map(([n, v]) => ({ cls: 'a', label: n, value: v, text: fmt(v) })),
      ],
    }],
    alt: `Tool schema tokens: weave ${R.schema.tokens}, ` + others.map(([n, v]) => `${n} ${v}`).join(', '),
  });
}
