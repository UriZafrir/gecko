// Gecko's web UI as a phone or tablet web app (PWA): what people do with a
// finger, on WebKit (iPhone) and Chromium (Android) profiles.
import { test, expect } from '@playwright/test';
import { URL, open, activeId, rect, fingerTap, pinch, sleep } from './helpers.js';

let cdp = null;
test.beforeEach(async ({ page, context, browserName }) => {
  cdp = browserName === 'chromium' ? await context.newCDPSession(page) : null;
});

test.describe('tabs', () => {
  test('a tap switches to the tab tapped, every time', async ({ page }) => {
    await open(page, ['shell', 'build', 'logs', 'claude', 'server', 'tests']);
    // Output in the background re-renders the tab bar all the time.
    await page.evaluate(() => {
      const ids = [...window.gecko.sessions.keys()];
      window.gecko.conn.input(ids[ids.length - 1], 'while true; do echo $$; sleep 0.1; done\r');
    });
    let misses = [];
    for (let i = 0; i < 48; i++) {
      const ids = await page.$$eval('#tabs .tab', (els) => els.map((e) => e.dataset.tab));
      const want = ids[(i * 5 + 1) % ids.length];
      if (want === await activeId(page)) continue;
      const tab = page.locator(`#tabs .tab[data-tab="${want}"]`);
      await page.evaluate((want) => { // in view, as a finger would scroll the strip
        const strip = document.getElementById('tabs'), e = strip.querySelector(`[data-tab="${want}"]`);
        const s = strip.getBoundingClientRect(), r = e.getBoundingClientRect();
        if (r.left < s.left || r.right > s.right) strip.scrollLeft += r.left - s.left - 20;
      }, want);
      await sleep(120);
      let b = null;
      for (let k = 0; k < 20 && !b; k++) b = await tab.boundingBox() || (await sleep(50), null);
      await fingerTap(page, cdp, b.x + Math.min(b.width / 2, 28), b.y + b.height / 2, { hold: 40 + (i % 5) * 30, dx: (i % 4) * 2, dy: (i % 3) * 2 });
      await sleep(250);
      if (await activeId(page) !== want) misses.push(i);
    }
    expect(misses, 'taps that did not switch').toEqual([]);
    // Two quick taps on a tab are two taps, not a rename prompt.
    const b = await page.locator('#tabs .tab').first().boundingBox();
    await fingerTap(page, cdp, b.x + 20, b.y + b.height / 2, { hold: 30 });
    await sleep(80);
    await fingerTap(page, cdp, b.x + 20, b.y + b.height / 2, { hold: 30 });
    await sleep(300);
    expect(await page.evaluate(() => !!window.gecko.paletteOpen)).toBe(false);
  });

  test('the tab strip stays where the finger scrolled it', async ({ page }) => {
    await open(page, ['shell', 'build', 'logs', 'claude', 'server', 'tests', 'notes', 'deploy', 'docs']);
    const scrolled = await page.evaluate(() => { const s = document.getElementById('tabs'); s.scrollLeft = s.scrollWidth; return s.scrollLeft; });
    test.skip(scrolled === 0, 'all tabs fit');
    await page.evaluate(() => window.gecko.render());
    await sleep(5500); // and the periodic re-render
    expect(await page.evaluate(() => document.getElementById('tabs').scrollLeft)).toBe(scrolled);
    // The tab the finger brought into view takes a tap.
    const last = page.locator('#tabs .tab').last();
    const want = await last.getAttribute('data-tab');
    const b = await last.boundingBox();
    await fingerTap(page, cdp, b.x + Math.min(b.width / 2, 28), b.y + b.height / 2);
    await expect.poll(() => activeId(page)).toBe(want);
  });

  test('switching tabs does not bring the keyboard up', async ({ page }) => {
    await open(page);
    const b = await page.locator('#tabs .tab').nth(1).boundingBox();
    await fingerTap(page, cdp, b.x + 20, b.y + b.height / 2);
    await sleep(400);
    expect(await page.evaluate(() => document.activeElement?.className || '')).not.toContain('xterm-helper-textarea');
  });
});

test.describe('pinch', () => {
  test('pinching a terminal changes the text size, and the page never zooms', async ({ page }) => {
    await open(page, ['shell']);
    const meta = await page.getAttribute('meta[name=viewport]', 'content');
    expect(meta).toContain('maximum-scale=1');
    const st = await rect(page, '#stage');
    const cx = st.x + st.w / 2, cy = st.y + st.h / 2;
    await pinch(page, cdp, cx, cy, 80, 140);
    await expect.poll(() => page.evaluate(() => window.gecko.fontSize())).toBeGreaterThan(12);
    expect(await page.evaluate(() => visualViewport.scale)).toBeCloseTo(1, 2);
    const big = await page.evaluate(() => window.gecko.fontSize());
    await pinch(page, cdp, cx, cy, 160, 90);
    await expect.poll(() => page.evaluate(() => window.gecko.fontSize())).toBeLessThan(big);
    // On the tab bar and the key bar: no zoom.
    const tb = await rect(page, '#tabbar');
    await pinch(page, cdp, tb.x + tb.w / 2, tb.y + tb.h / 2, 40, 260);
    const kb = await rect(page, '#keybar');
    await pinch(page, cdp, kb.x + kb.w / 2, kb.y + kb.h / 2, 40, 260);
    await sleep(500);
    expect(await page.evaluate(() => visualViewport.scale)).toBeCloseTo(1, 2);
    // iOS page zoom comes from gesture events: they are cancelled.
    const prevented = await page.evaluate(() => {
      const e = new Event('gesturestart', { cancelable: true, bubbles: true });
      document.getElementById('tabbar').dispatchEvent(e);
      return e.defaultPrevented;
    });
    expect(prevented).toBe(true);
  });

  test('a double tap does not zoom', async ({ page }) => {
    await open(page);
    const touchAction = await page.evaluate(() => [getComputedStyle(document.documentElement).touchAction, getComputedStyle(document.querySelector('#tabs .tab')).touchAction]);
    expect(touchAction[0]).toMatch(/pan-x pan-y|manipulation|none/);
    expect(touchAction[1]).toMatch(/manipulation|none/);
    const b = await page.locator('#new-tab').boundingBox();
    const y = (await rect(page, '#stage')).y + 40;
    await fingerTap(page, cdp, b.x - 60, y, { hold: 20 });
    await sleep(60);
    await fingerTap(page, cdp, b.x - 60, y, { hold: 20 });
    await sleep(600);
    expect(await page.evaluate(() => visualViewport.scale)).toBeCloseTo(1, 2);
  });
});

test.describe('settings', () => {
  test('theme, text size and key bar from the Settings button', async ({ page }) => {
    await open(page);
    const gear = page.locator('#settings-btn');
    await expect(gear).toBeVisible();
    const g = await gear.boundingBox();
    await fingerTap(page, cdp, g.x + g.width / 2, g.y + g.height / 2);
    const card = page.locator('.card.settings');
    await expect(card).toBeVisible();
    await expect(card.locator('[data-theme="tokyo-night"]')).toHaveClass(/on/);
    await expect(page).toHaveScreenshot('settings.png', { mask: [page.locator('#panes'), card.locator('.note').last()] });
    // A theme: applied at once and saved.
    const bg0 = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--bg'));
    await card.locator('[data-theme="gecko-light"]').tap();
    await expect.poll(() => page.evaluate(() => window.gecko.settings.theme)).toBe('gecko-light');
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--bg'))).not.toBe(bg0);
    expect(await page.getAttribute('meta[name=theme-color]', 'content')).toMatch(/^#/);
    // Text size.
    await card.getByRole('button', { name: 'Bigger text' }).tap();
    await card.getByRole('button', { name: 'Bigger text' }).tap();
    await expect(card.locator('output')).toHaveText('14 px');
    expect(await page.evaluate(() => window.gecko.fontSize())).toBe(14);
    await card.getByRole('button', { name: 'Smaller text' }).tap();
    expect(await page.evaluate(() => window.gecko.fontSize())).toBe(13);
    // The key bar, off and on.
    await card.locator('[data-keybar]').tap();
    await expect(page.locator('#keybar')).toBeHidden();
    await card.locator('[data-keybar]').tap();
    await expect(page.locator('#keybar')).toBeVisible();
    // The same settings from the palette, and Done closes.
    await card.locator('[data-theme="tokyo-night"]').tap();
    await card.locator('[data-done]').tap();
    await expect(card).toBeHidden();
    await page.evaluate(() => window.gecko.openPalette('settings'));
    await expect(page.locator('#palette li').first()).toContainText('Settings');
    await page.keyboard.press('Escape');
  });

  test('every key on the key bar can be reached and sends', async ({ page }) => {
    await open(page, ['shell']);
    const sent = await page.evaluate(() => {
      const g = window.gecko, got = [];
      const orig = g.conn.input.bind(g.conn);
      g.conn.input = (id, d) => { got.push(d); return orig(id, d); };
      for (const k of ['esc', 'up', 'ctrl-c']) document.querySelector(`#keybar [data-k="${k}"]`).click();
      g.conn.input = orig;
      return got;
    });
    expect(sent).toEqual(['\x1b', '\x1b[A', '\x03']);
  });
});

test.describe('layout', () => {
  test('keyboard up and down: the key bar sits on it, nothing stays shifted', async ({ page }) => {
    await open(page, ['shell']);
    const H = await page.evaluate(() => innerHeight);
    const tab0 = await rect(page, '#tabbar');
    // The keyboard as iOS reports it: the visual viewport shrinks and pans
    // while the page keeps its height (Chrome resizes the page instead,
    // which the next step covers).
    await page.evaluate(() => {
      const vv = window.visualViewport;
      window.__vv = { h: Object.getOwnPropertyDescriptor(Object.getPrototypeOf(vv), 'height'), t: Object.getOwnPropertyDescriptor(Object.getPrototypeOf(vv), 'offsetTop') };
      Object.defineProperty(vv, 'height', { configurable: true, get: () => innerHeight - 300 });
      Object.defineProperty(vv, 'offsetTop', { configurable: true, get: () => 80 });
      vv.dispatchEvent(new Event('resize'));
    });
    await sleep(200);
    expect(await page.evaluate(() => document.documentElement.classList.contains('kb'))).toBe(true);
    const app = await rect(page, '#app');
    expect(app.y).toBeCloseTo(80, 0);
    expect(app.h).toBeCloseTo(H - 300, 0);
    const keys = await rect(page, '#keybar');
    expect(keys.b).toBeLessThanOrEqual(80 + H - 300 + 1);
    expect((await rect(page, '#tabbar')).y).toBeGreaterThanOrEqual(80 - 1);
    // The keyboard goes: back to the whole window.
    await page.evaluate(() => {
      const vv = window.visualViewport;
      delete vv.height; delete vv.offsetTop;
      vv.dispatchEvent(new Event('resize'));
    });
    await sleep(200);
    expect(await page.evaluate(() => document.documentElement.classList.contains('kb'))).toBe(false);
    expect((await rect(page, '#tabbar')).y).toBeCloseTo(tab0.y, 0);
    expect((await rect(page, '#keybar')).b).toBeCloseTo(H, 0);
    expect(await page.evaluate(() => window.scrollY)).toBe(0);
  });

  test('a resized window (Chrome with the keyboard) keeps the key bar at the bottom', async ({ page }) => {
    await open(page, ['shell']);
    const vp = page.viewportSize();
    await page.setViewportSize({ width: vp.width, height: vp.height - 320 });
    await sleep(400);
    expect((await rect(page, '#keybar')).b).toBeCloseTo(vp.height - 320, 0);
    expect((await rect(page, '#tabbar')).y).toBe(0);
    expect(await page.evaluate(() => document.documentElement.classList.contains('kb'))).toBe(true);
    await page.setViewportSize(vp);
    await sleep(400);
    expect((await rect(page, '#keybar')).b).toBeCloseTo(vp.height, 0);
    expect(await page.evaluate(() => document.documentElement.classList.contains('kb'))).toBe(false);
  });

  test('installed: clear of the notch, the status bar and the home indicator', async ({ page, browserName }) => {
    test.skip(browserName !== 'chromium', 'safe areas are emulated through Chromium DevTools');
    await open(page, ['shell']);
    await cdp.send('Emulation.setSafeAreaInsetsOverride', { insets: { top: 47, bottom: 34, left: 0, right: 0 } });
    await sleep(300);
    expect((await rect(page, '#tabs')).y).toBeGreaterThanOrEqual(47);
    expect(await page.$eval('#keybar', (e) => parseFloat(getComputedStyle(e).paddingBottom))).toBeGreaterThanOrEqual(34);
    expect((await rect(page, '#keybar')).b).toBeCloseTo(page.viewportSize().height, 0);
    await expect(page).toHaveScreenshot('safe-area.png', { mask: [page.locator('#panes')] });
    // Landscape: a notch on the side.
    await cdp.send('Emulation.setSafeAreaInsetsOverride', { insets: { top: 0, bottom: 21, left: 47, right: 47 } });
    await sleep(300);
    expect((await rect(page, '#tabbar')).x).toBeGreaterThanOrEqual(47);
    await cdp.send('Emulation.setSafeAreaInsetsOverride', { insets: { top: 0, bottom: 0, left: 0, right: 0 } });
  });

  test('no flicker when the connection comes back', async ({ page }) => {
    await open(page, ['shell', 'build']);
    await page.evaluate(() => window.gecko.conn.input(window.gecko.activeId(), 'seq 1 30\r'));
    await sleep(800);
    const log = await page.evaluate(async () => {
      const g = window.gecko, out = [], n = document.querySelectorAll('#tabs .tab').length;
      const iv = setInterval(() => out.push({ empty: !document.getElementById('empty').hidden, tabs: document.querySelectorAll('#tabs .tab').length, text: g.activePane()?.el.querySelector('.xterm-rows')?.textContent.length || 0 }), 30);
      g.conn.tr.ws ? g.conn.tr.ws.close() : g.conn.tr.close();
      await new Promise((r) => setTimeout(r, 3000));
      clearInterval(iv);
      return { out, n, status: g.conn.status };
    });
    expect(log.status).toBe('online');
    // A tab left over from an earlier test may still arrive; none may vanish.
    expect(log.out.filter((s) => s.empty || s.tabs < log.n || s.text < 20)).toEqual([]);
  });
});

test.describe('install', () => {
  test('manifest, icons and the service worker', async ({ page, request, browserName }) => {
    await open(page, ['shell']);
    const base = new globalThis.URL(URL()).origin;
    const m = await (await request.get(base + '/manifest.webmanifest')).json();
    expect(m).toMatchObject({ name: 'Gecko', short_name: 'Gecko', display: 'standalone', start_url: '/', scope: '/' });
    expect(m.theme_color).toMatch(/^#/);
    const size = async (src) => {
      const r = await request.get(base + src);
      expect(r.ok(), src).toBe(true);
      const b = await r.body();
      if (!src.endsWith('.png')) return 'any';
      expect(b.subarray(1, 4).toString()).toBe('PNG');
      return `${b.readUInt32BE(16)}x${b.readUInt32BE(20)}`;
    };
    for (const icon of m.icons) expect(await size(icon.src), icon.src).toBe(icon.sizes);
    expect(m.icons.some((i) => i.purpose === 'maskable' && i.sizes === '512x512')).toBe(true);
    expect(m.icons.some((i) => i.sizes === '192x192')).toBe(true);
    const apple = await page.getAttribute('link[rel=apple-touch-icon]', 'href');
    expect(await size(apple)).toBe('180x180');
    expect(await page.getAttribute('meta[name=apple-mobile-web-app-capable]', 'content')).toBe('yes');
    expect(await page.getAttribute('meta[name=viewport]', 'content')).toContain('viewport-fit=cover');
    if (browserName !== 'chromium') return;
    // The service worker never serves an old UI while the daemon answers...
    await page.waitForFunction(() => navigator.serviceWorker?.controller, null, { timeout: 15_000 }).catch(() => page.reload());
    await page.waitForFunction(() => navigator.serviceWorker?.controller, null, { timeout: 15_000 });
    const fresh = await page.evaluate(async () => (await fetch('/main.js')).headers.get('cache-control'));
    expect(fresh).toBe('no-cache');
  });

  test('installed and offline: the app opens and reconnects by itself', async ({ page, context, browserName }) => {
    test.skip(browserName !== 'chromium', 'offline with a service worker: Chromium');
    await open(page, ['shell']);
    await page.reload();
    await page.waitForFunction(() => navigator.serviceWorker?.controller && window.gecko?.firstSync, null, { timeout: 20_000 });
    await context.setOffline(true);
    await page.reload().catch(() => {});
    await expect(page.locator('#tabbar')).toBeVisible();
    await expect(page.locator('#settings-btn')).toBeVisible();
    await context.setOffline(false);
    await page.waitForFunction(() => window.gecko?.conn.status === 'online', null, { timeout: 30_000 });
  });
});

// A phone always opens Gecko from another address: the page goes through
// that Gecko (the anchor), which the bottom bar and the drawer say.
test.describe('from another device', () => {
  test('the anchor stays in view: the "via" bar above the keys, the badge in the drawer', async ({ page, browserName }) => {
    test.skip(browserName !== 'chromium', 'another host name for the daemon needs Chromium');
    await page.goto(URL().replace('127.0.0.1', 'devbox.test'));
    await page.waitForFunction(() => window.gecko?.firstSync, null, { timeout: 20_000 });
    await page.evaluate(async () => { if (!window.gecko.sessions.size) await window.gecko.newTab({ name: 'shell' }); });
    const via = page.locator('#statusline .sl-anchor');
    await expect(via).toBeVisible();
    const v = await via.boundingBox(), keys = await rect(page, '#keybar');
    expect(v.y + v.height).toBeLessThanOrEqual(keys.y + 1);
    if (await page.evaluate(() => document.body.classList.contains('side-hidden') || matchMedia('(max-width: 719px), (max-height: 500px)').matches)) {
      await page.locator('#menu-btn').tap();
    }
    await expect(page.locator('#side .anchor-badge')).toBeVisible();
  });
});
