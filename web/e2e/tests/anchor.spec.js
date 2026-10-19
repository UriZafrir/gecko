// Make anchor on phones: Android's This phone (a page at 127.0.0.1) lists
// ubuntu; the anchor is marked in the drawer, Make anchor on ubuntu moves
// the mark there and shows "via ubuntu", the choice survives a relaunch,
// and "Back to phone" switches back. Two throwaway daemons of their own
// (ports 7952 and 7953, fresh GECKO_HOMEs), never the real service.
import { test, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '../../..');
const shots = process.env.ANCHOR_SHOTS || '';
const procs = [];
let phoneURL = '';

async function daemon(name, port, hosts = [], theme = 'tokyo-night') {
  const home = mkdtempSync(join(tmpdir(), `gk-${name}-`));
  writeFileSync(join(home, 'config.json'), JSON.stringify({ name, hosts }));
  writeFileSync(join(home, 'settings.json'), JSON.stringify({ theme }) + '\n');
  const d = spawn(join(root, 'bin/gecko'), ['daemon', '--listen', `127.0.0.1:${port}`], {
    env: { ...process.env, GECKO_HOME: home, GECKO_WEB_DIR: join(root, 'web/dist'), SHELL: '/bin/sh', PS1: '$ ' },
    stdio: ['ignore', 'ignore', 'inherit'],
    detached: true,
  });
  procs.push({ d, home });
  let token = '';
  for (let i = 0; i < 100 && !token; i++) {
    await new Promise((r) => setTimeout(r, 100));
    try { token = readFileSync(join(home, 'token'), 'utf8').trim(); } catch {}
  }
  for (let i = 0; i < 50; i++) {
    try { if ((await fetch(`http://127.0.0.1:${port}/`)).ok) break; } catch {}
    await new Promise((r) => setTimeout(r, 100));
  }
  return { base: `http://127.0.0.1:${port}/`, token };
}

test.beforeAll(async () => {
  // ubuntu has a light theme: after Make anchor the window keeps the phone's.
  const ubuntu = await daemon('ubuntu', 7953, [], 'gecko-light');
  const phone = await daemon('phone', 7952, [{ name: 'ubuntu', url: ubuntu.base, token: ubuntu.token }]);
  phoneURL = `${phone.base}?token=${phone.token}`;
});

test.afterAll(() => {
  for (const { d, home } of procs) {
    try { process.kill(-d.pid, 'SIGTERM'); } catch {}
    try { rmSync(home, { recursive: true, force: true }); } catch {}
  }
});

const synced = (page, host) => page.waitForFunction((h) => window.gecko?.firstSync && window.gecko.conn.status === 'online' && window.gecko.hostName === h
  && window.gecko.hosts.length > 0, host, { timeout: 30_000 });

async function drawer(page) {
  if (await page.evaluate(() => document.body.classList.contains('side-hidden') || matchMedia('(max-width: 719px), (max-height: 500px)').matches)) {
    if (!(await page.evaluate(() => document.body.classList.contains('drawer')))) {
      await page.locator('#menu-btn').tap();
      await page.waitForTimeout(450); // the drawer slides in
    }
  }
  await expect(page.locator('#side')).toBeVisible();
}

// The anchor row: icon and highlight, readable against the sidebar.
async function expectAnchor(page, name) {
  const row = page.locator(`#side .host.anchor[data-host="${name}"]`);
  await expect(row).toHaveCount(1);
  await expect(row.locator('.anchor-badge .anc')).toBeVisible();
  await expect(page.locator('#side .host.anchor')).toHaveCount(1);
  const look = await row.evaluate((li) => {
    const hr = li.querySelector('.host-row'), b = li.querySelector('.anchor-badge');
    const cs = getComputedStyle(hr);
    const side = document.getElementById('side');
    return { bg: cs.backgroundColor, edge: cs.boxShadow, badge: getComputedStyle(b).color, sideBg: getComputedStyle(side).backgroundColor,
      fits: side.scrollWidth <= side.clientWidth + 1, right: b.getBoundingClientRect().right, sideRight: side.getBoundingClientRect().right };
  });
  expect(look.edge).toContain('inset');
  expect(look.bg).not.toBe('rgba(0, 0, 0, 0)');
  expect(look.fits).toBe(true);
  expect(look.right).toBeLessThanOrEqual(look.sideRight);
  return look;
}

// WCAG contrast of two CSS colors (rgb()/rgba(), alpha ignored).
function contrast(a, b) {
  const lum = (c) => {
    const [r, g, bl] = c.match(/[\d.]+/g).slice(0, 3).map(Number).map((v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; });
    return 0.2126 * r + 0.7152 * g + 0.0722 * bl;
  };
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

async function shot(page, name) {
  if (shots) await page.screenshot({ path: join(shots, `${test.info().project.name}-${name}.png`) });
}

test('This phone: the anchor icon is on the phone itself, and there is no "via"', async ({ page }) => {
  await page.goto(phoneURL);
  await page.evaluate(() => { localStorage.removeItem('gecko.anchorPath'); });
  await page.reload();
  await synced(page, 'phone');
  await page.evaluate(async () => { if (!window.gecko.sessions.size) await window.gecko.newTab({ name: 'shell' }); });
  await drawer(page);
  const look = await expectAnchor(page, 'phone');
  await expect(page.locator('#side .host.anchor[data-host="phone"] .where')).toHaveText('this machine');
  await expect(page.locator('#statusline .sl-anchor')).toHaveCount(0);
  expect(contrast(look.badge, look.sideBg)).toBeGreaterThan(3);
  // Light themes too.
  await page.evaluate(() => window.gecko.setSetting('theme', 'gecko-light'));
  await page.waitForTimeout(300);
  const light = await expectAnchor(page, 'phone');
  expect(contrast(light.badge, light.sideBg)).toBeGreaterThan(3);
  await page.evaluate(() => window.gecko.setSetting('theme', 'tokyo-night'));
  await page.waitForTimeout(300);
  await shot(page, '1-this-phone');
});

test('Make anchor on ubuntu, the icon moves and "via ubuntu" shows; it survives a relaunch; Back to phone', async ({ page }) => {
  await page.goto(phoneURL);
  await page.evaluate(() => { localStorage.removeItem('gecko.anchorPath'); });
  await page.reload();
  await synced(page, 'phone');
  await page.waitForFunction(() => window.gecko.hosts.some((h) => h.name === 'ubuntu' && h.status === 'connected'), null, { timeout: 30_000 });
  await drawer(page);
  // Long-press (a context menu) on ubuntu's row: Make anchor.
  // (as a MouseEvent: Playwright's dispatchEvent makes a plain Event without clientX)
  await page.locator('#side .host[data-host="ubuntu"] .host-main').evaluate((b) => {
    const r = b.getBoundingClientRect();
    b.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: r.left + 30, clientY: r.top + r.height / 2 }));
  });
  const item = page.locator('#ctxmenu button', { hasText: 'Make anchor' });
  await expect(item).toBeVisible();
  await shot(page, '2-menu');
  await item.click();
  await expect(page.locator('#modal [data-yes]')).toHaveText('Make anchor');
  await shot(page, '3-confirm');
  await page.locator('#modal [data-yes]').click();
  await synced(page, 'ubuntu');
  expect(await page.evaluate(() => [window.gecko.proxied, window.gecko.door])).toEqual([true, 'phone']);
  // The window's settings are the client's: the phone's theme, not ubuntu's light one.
  await page.waitForTimeout(300);
  expect(await page.evaluate(() => window.gecko.settings.theme)).toBe('tokyo-night');
  await page.evaluate(async () => { if (!window.gecko.sessions.size) await window.gecko.newTab({ name: 'on-ubuntu' }); });
  await expect(page.locator('#statusline .sl-anchor')).toHaveText('via ubuntu');
  await drawer(page);
  await expectAnchor(page, 'ubuntu');
  await expect(page.locator('#side .host.anchor[data-host="ubuntu"] .where')).toHaveText('through phone');
  // phone isn't one of ubuntu's machines: it's offered as the way back.
  await expect(page.locator('#side .prev-anchor[data-prev="phone"]')).toBeVisible();
  await shot(page, '4-ubuntu-anchor');
  // Relaunch: still on ubuntu.
  await page.reload();
  await synced(page, 'ubuntu');
  await expect(page.locator('#statusline .sl-anchor')).toHaveText('via ubuntu');
  // Back to phone.
  await drawer(page);
  await page.locator('#side .prev-anchor[data-prev="phone"] .host-main').click();
  await synced(page, 'phone');
  expect(await page.evaluate(() => window.gecko.proxied)).toBe(false);
  await drawer(page);
  await expectAnchor(page, 'phone');
  await expect(page.locator('#statusline .sl-anchor')).toHaveCount(0);
  await expect(page.locator('#side .prev-anchor')).toHaveCount(0);
  await shot(page, '5-back-on-phone');
  await page.reload();
  await synced(page, 'phone');
});

test('the command palette offers Make anchor', async ({ page }) => {
  await page.goto(phoneURL);
  await synced(page, 'phone');
  await page.waitForFunction(() => window.gecko.hosts.some((h) => h.name === 'ubuntu'), null, { timeout: 30_000 });
  const labels = await page.evaluate(() => window.gecko.commands().filter((c) => c.kind === 'Anchor').map((c) => c.label));
  expect(labels).toContain('Make ubuntu the anchor');
});

// A browser on another computer (or the PWA through gecko share): the page
// is always "via" its Gecko, and Make anchor goes one further.
test('from another device: via the door, then via ubuntu, and back', async ({ page, browserName }) => {
  test.skip(browserName !== 'chromium', 'another host name for the daemon needs Chromium');
  const remote = phoneURL.replace('127.0.0.1', 'devbox.test');
  await page.goto(remote);
  await page.evaluate(() => { localStorage.removeItem('gecko.anchorPath'); });
  await page.reload();
  await synced(page, 'phone');
  await page.waitForFunction(() => window.gecko.hosts.some((h) => h.name === 'ubuntu' && h.status === 'connected'), null, { timeout: 30_000 });
  await page.evaluate(async () => { if (!window.gecko.sessions.size) await window.gecko.newTab({ name: 'shell' }); });
  await expect(page.locator('#statusline .sl-anchor')).toHaveText('via phone');
  await drawer(page);
  await expectAnchor(page, 'phone');
  await expect(page.locator('#side .host.anchor[data-host="phone"] .where')).toHaveText('connected here');
  await page.evaluate(() => { window.gecko.makeAnchor('ubuntu'); }); // it asks first
  await page.locator('#modal [data-yes]').click();
  await synced(page, 'ubuntu');
  await page.evaluate(async () => { if (!window.gecko.sessions.size) await window.gecko.newTab({ name: 'on-ubuntu' }); });
  await expect(page.locator('#statusline .sl-anchor')).toHaveText('via ubuntu');
  await drawer(page);
  await expectAnchor(page, 'ubuntu');
  await expect(page.locator('#side .prev-anchor[data-prev="phone"] .where')).toHaveText('where you connect');
  await shot(page, '6-remote-ubuntu');
  await page.locator('#side .prev-anchor[data-prev="phone"] .host-main').click();
  await synced(page, 'phone');
  await expect(page.locator('#statusline .sl-anchor')).toHaveText('via phone');
});
