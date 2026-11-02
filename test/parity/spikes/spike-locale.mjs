#!/usr/bin/env node
// Spike B: how far is Node's localeCompare from Go's byte order?
// If it differs, the port needs a comparator built from this oracle, not sort.Strings.
const ascii = Array.from({ length: 95 }, (_, i) => String.fromCharCode(32 + i));
const sign = (n) => (n < 0 ? -1 : n > 0 ? 1 : 0);
const cp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

let diffs = 0;
const examples = [];
for (const a of ascii) {
  for (const b of ascii) {
    const l = sign(a.localeCompare(b));
    const c = cp(a, b);
    if (l !== c) {
      diffs++;
      if (examples.length < 12) examples.push(`${JSON.stringify(a)} vs ${JSON.stringify(b)}: locale ${l}, codepoint ${c}`);
    }
  }
}

const localeOrder = [...ascii].sort((a, b) => a.localeCompare(b)).join('');

let seed = 42;
const rnd = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 2 ** 32);
const chars = [...'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ąćęłńóśźżĄĆĘŁŃÓŚŹŻ _-./'];
const word = () => Array.from({ length: 3 + Math.floor(rnd() * 6) }, () => chars[Math.floor(rnd() * chars.length)]).join('');
let randomDiffs = 0;
for (let i = 0; i < 10000; i++) {
  const a = word();
  const b = word();
  if (sign(a.localeCompare(b)) !== cp(a, b)) randomDiffs++;
}

console.log('locale:', Intl.DateTimeFormat().resolvedOptions().locale, '| ICU:', process.versions.icu ?? 'n/a');
console.log(`ASCII pairs: ${ascii.length * ascii.length}, differing from codepoint: ${diffs}`);
console.log('examples:');
for (const e of examples) console.log('  ' + e);
console.log('locale order of printable ASCII:', JSON.stringify(localeOrder));
console.log(`random Polish/ASCII pairs: 10000, differing from codepoint: ${randomDiffs}`);
console.log(diffs || randomDiffs ? 'VERDICT: a JS-compatible comparator is required' : 'VERDICT: byte order is sufficient');
