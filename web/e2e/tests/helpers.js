// Shared steps for the PWA tests.
export const URL = () => process.env.GECKO_PWA_URL;
export const isChromium = (browserName) => browserName === 'chromium';
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// The page, connected and synced, with a fresh set of tabs (named), the
// first one current, no keyboard, the default text size and Tokyo Night.
export async function open(page, names = ['shell', 'build', 'logs']) {
  await page.goto(URL());
  await page.waitForFunction(() => window.gecko && window.gecko.firstSync && window.gecko.conn.status === 'online', null, { timeout: 20_000 });
  await page.evaluate(async (names) => {
    const g = window.gecko;
    for (const id of [...g.sessions.keys()]) await g.closeTab(id, true);
    g.setSetting('theme', 'tokyo-night');
    g.setFont(12);
    g.setSetting('keybar', true);
    for (const name of names) await g.newTab({ name });
    g.focusSession(document.querySelector('#tabs .tab').dataset.tab);
    document.activeElement?.blur();
  }, names);
  await page.waitForFunction((n) => document.querySelectorAll('#tabs .tab').length === n, names.length);
  await page.waitForTimeout(400);
}

export const activeId = (page) => page.evaluate(() => window.gecko.activeId());
export const rect = (page, sel) => page.$eval(sel, (e) => { const r = e.getBoundingClientRect(); return { x: r.left, y: r.top, w: r.width, h: r.height, b: r.bottom, r: r.right }; });

// A finger tap held for `hold` ms that drifts by (dx, dy) before lifting,
// like a real one: Chromium through DevTools touch events, WebKit through
// Playwright's tap (no hold or drift there).
export async function fingerTap(page, cdp, x, y, { hold = 80, dx = 0, dy = 0 } = {}) {
  if (!cdp) return page.touchscreen.tap(x, y);
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
  await sleep(hold / 2);
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x + dx / 2, y: y + dy / 2 }] });
  await sleep(hold / 2);
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x + dx, y: y + dy }] });
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
}

// Two fingers from `from` px apart to `to` px apart around (cx, cy).
export async function pinch(page, cdp, cx, cy, from, to, steps = 10) {
  const pts = (d) => [{ x: cx - d / 2, y: cy, id: 0 }, { x: cx + d / 2, y: cy, id: 1 }];
  if (cdp) {
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: pts(from) });
    for (let i = 1; i <= steps; i++) {
      await sleep(16);
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: pts(from + (to - from) * i / steps) });
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    return;
  }
  // WebKit here has no multi-touch input: the same touches, made in the page.
  await page.evaluate(({ cx, cy, from, to, steps }) => {
    const el = document.elementFromPoint(cx, cy);
    // No Touch constructor in this WebKit: events that carry the same fields.
    const touches = (d) => [0, 1].map((i) => ({ identifier: i, target: el, clientX: cx + (i ? d / 2 : -d / 2), clientY: cy }));
    const fire = (type, list, changed) => {
      const e = new Event(type, { bubbles: true, cancelable: true });
      Object.defineProperties(e, { touches: { value: list }, targetTouches: { value: list }, changedTouches: { value: changed } });
      el.dispatchEvent(e);
    };
    fire('touchstart', touches(from), touches(from));
    for (let i = 1; i <= steps; i++) fire('touchmove', touches(from + (to - from) * i / steps), touches(from + (to - from) * i / steps));
    fire('touchend', [], touches(to));
  }, { cx, cy, from, to, steps });
}
