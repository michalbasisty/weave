// Screenshot the explorer so it can be reviewed instead of guessed at.
// node test/shot.mjs  → test/shots/{light,dark}.png
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const pw = await import('file:///G:/QR-gent/node_modules/playwright/index.js');
const chromium = pw.chromium ?? pw.default?.chromium;
if (!chromium) throw new Error('playwright not found');

const page_url = pathToFileURL(join(import.meta.dirname, '..', 'preview.html')).href;
const out = join(import.meta.dirname, 'shots');
mkdirSync(out, { recursive: true });

const browser = await chromium.launch();
for (const scheme of ['light', 'dark']) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, colorScheme: scheme });
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));
  await page.goto(page_url);
  await page.waitForTimeout(250);
  await page.screenshot({ path: join(out, `${scheme}.png`) });

  await page.click('[data-v="flows"]');
  await page.waitForTimeout(150);
  await page.screenshot({ path: join(out, `${scheme}-flows.png`) });

  await page.click('.flow [data-go]');
  await page.waitForTimeout(150);
  await page.screenshot({ path: join(out, `${scheme}-detail.png`) });

  // compact rows, then the list hidden
  await page.click('#dense');
  await page.waitForTimeout(120);
  await page.screenshot({ path: join(out, `${scheme}-compact.png`) });
  await page.click('#collapse');
  await page.waitForTimeout(120);
  await page.screenshot({ path: join(out, `${scheme}-collapsed.png`) });
  console.log(scheme, errors.length ? `ERRORS: ${errors.join(' | ')}` : 'no console errors');
  await ctx.close();
}
await browser.close();
