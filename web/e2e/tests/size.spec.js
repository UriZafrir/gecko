// One session open on a phone and on a desktop window: the device in use
// decides its size. A desktop window left open used to take the size back
// when it reconnected, and the phone showed jumbled text until a pinch.
import { test, expect } from '@playwright/test';
import { URL, open, activeId, rect, fingerTap, sleep } from './helpers.js';

const termSize = (pg) => pg.evaluate(() => { const p = window.gecko.activePane(); return [p.term.cols, p.term.rows]; });
const sessionSize = (pg, id) => pg.evaluate((id) => { const s = window.gecko.sessions.get(id); return [s.cols, s.rows]; }, id);

test('the device in use keeps the size; a desktop window left open does not take it back', async ({ page, context, browser, browserName }) => {
  const cdp = browserName === 'chromium' ? await context.newCDPSession(page) : null;
  await open(page, ['claude']);
  const id = await activeId(page);
  const phone = await termSize(page);
  await expect.poll(() => sessionSize(page, id)).toEqual(phone);

  const desk = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const dp = await desk.newPage();
  await dp.goto(URL());
  await dp.waitForFunction((id) => window.gecko?.firstSync && window.gecko.conn.status === 'online' && window.gecko.sessions.has(id), id, { timeout: 20_000 });
  await dp.evaluate((id) => window.gecko.focusSession(id), id);
  await sleep(400);
  const wide = await termSize(dp);
  expect(wide).not.toEqual(phone);

  // Used on the desktop: it takes the size.
  await dp.mouse.click(640, 400);
  await expect.poll(() => sessionSize(page, id)).toEqual(wide);

  // Back to the phone (no focus event there): the phone's size again.
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await expect.poll(() => sessionSize(page, id)).toEqual(phone);

  // The desktop window, untouched for a while, reconnects: it keeps its
  // hands off the size.
  await dp.evaluate(() => { const g = window.gecko; g.lastUse = 0; g.conn.tr.ws ? g.conn.tr.ws.close() : g.conn.tr.close(); });
  await sleep(500);
  await dp.waitForFunction(() => window.gecko.conn.status === 'online', null, { timeout: 15_000 });
  await sleep(1500);
  expect(await sessionSize(page, id)).toEqual(phone);

  // The desktop used again, then a touch on the phone's terminal: the phone
  // takes the size back without a pinch.
  await dp.mouse.click(640, 400);
  await expect.poll(() => sessionSize(page, id)).toEqual(wide);
  const t = await rect(page, '.pane.active');
  await fingerTap(page, cdp, t.x + t.w / 2, t.y + t.h / 3);
  await expect.poll(() => sessionSize(page, id)).toEqual(phone);

  await desk.close();
});
