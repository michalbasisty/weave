#!/usr/bin/env node
// Spike A: does the Go port need to care about directory listing order?
// Node (libuv) returns the OS order; Go's os.ReadDir sorts. Create the same files
// in a deliberately non-alphabetical order and compare.
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { readdirSync } from 'node:fs';

const dir = mkdtempSync(join(tmpdir(), 'weave-readdir-'));
const names = ['Zed.ts', 'apple.ts', 'Beta.ts', 'alpha.ts', 'README.md', 'zz.ts', 'Zed', 'apple'];
for (const n of names) {
  if (n === 'Zed' || n === 'apple') mkdirSync(join(dir, n));
  else writeFileSync(join(dir, n), 'x');
}

const nodeOrder = readdirSync(dir);
const go = spawnSync('go', ['run', join(import.meta.dirname, 'spike-readdir.go'), dir], { encoding: 'utf8' });
if (go.status !== 0) {
  console.error('go run failed:', go.stderr);
  process.exit(1);
}
const goOrder = go.stdout.trim().split(/\r?\n/).filter(Boolean);

const sorted = [...names].sort();
console.log('node :', nodeOrder.join('  '));
console.log('go   :', goOrder.join('  '));
console.log('sorted:', sorted.join('  '));
console.log(`node == sorted: ${JSON.stringify(nodeOrder) === JSON.stringify(sorted)}`);
console.log(`go   == sorted: ${JSON.stringify(goOrder) === JSON.stringify(sorted)}`);
console.log(`same order: ${JSON.stringify(nodeOrder) === JSON.stringify(goOrder)}`);
console.log(`platform: ${process.platform}`);

rmSync(dir, { recursive: true, force: true });
