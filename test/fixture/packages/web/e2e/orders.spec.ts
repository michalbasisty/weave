import { test } from '@playwright/test';
import { BASE } from './helpers';

// @req REQ-ORD-1
test('orders page loads', async ({ page }) => {
  await page.goto(BASE + '/orders');
});
