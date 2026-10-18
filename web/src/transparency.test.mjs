// The desktop app's see-through background, piece by piece: the window
// options for each platform (desktop/main.js), the page's surfaces at a
// given opacity (paintTheme) and the CSS that has to stay transparent.
// packaging/desktop/transparency/check.cjs checks the real thing on each
// platform, in CI (.github/workflows/transparency.yml).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import Module, { createRequire } from 'node:module';
import { THEMES, paintTheme, uiTokens } from './themes.js';

const css = fs.readFileSync(new URL('./style.css', import.meta.url), 'utf8');

// desktop/main.js with a stand-in for Electron: it asks for the
// single-instance lock, gets none and quits, so nothing else runs.
function loadMain() {
  const app = new Proxy({}, { get: (_, k) => (k === 'requestSingleInstanceLock' ? () => false : k === 'commandLine' ? { appendSwitch() {} } : () => {}) });
  const electron = { app, BrowserWindow: {}, Menu: {}, dialog: {}, ipcMain: { handle() {}, on() {} }, shell: {}, screen: {} };
  const load = Module._load;
  Module._load = function (req, ...rest) { return req === 'electron' ? electron : load.call(this, req, ...rest); };
  try {
    return createRequire(import.meta.url)('../../desktop/main.js');
  } finally {
    Module._load = load;
  }
}
const { windowStyle } = loadMain();
const ALPHA0 = /^#[0-9a-f]{6}00$/i;

for (const platform of ['darwin', 'win32', 'linux']) {
  test(`${platform}: the window itself is see-through`, () => {
    const s = windowStyle(platform);
    assert.equal(s.transparent, true);
    assert.match(s.backgroundColor, ALPHA0);
    // A system material (macOS vibrancy, Windows acrylic or mica) sits
    // between the page and the desktop and hides what is behind the
    // window: the bug this test is here for.
    for (const k of ['vibrancy', 'visualEffectState', 'backgroundMaterial']) assert.equal(s[k], undefined, `${k} is set`);
  });
}

test('macOS: traffic lights inset over the page', () => {
  assert.equal(windowStyle('darwin').titleBarStyle, 'hiddenInset');
});

test('Windows: caption buttons over the page, which the tab bar makes room for', () => {
  const s = windowStyle('win32');
  // A transparent window with the native frame has none at all on Windows
  // (no title bar, no buttons, not resizable): the overlay brings them back.
  assert.equal(s.titleBarStyle, 'hidden');
  assert.match(s.titleBarOverlay.color, ALPHA0);
  assert.ok(s.titleBarOverlay.height > 0);
  assert.match(css, /html\.desktop-win32 #tabbar \{ padding-right: calc\(100vw - env\(titlebar-area-x, 0px\) - env\(titlebar-area-width, 100vw\)\); \}/);
  assert.match(css, /html\.desktop-win32 \.brand, html\.desktop-win32 #tabbar \{ -webkit-app-region: drag; \}/);
});

test('Linux: no frame tricks (the window manager draws the frame)', () => {
  assert.equal(windowStyle('linux').titleBarStyle, undefined);
});

// paintTheme on a stand-in document.
function paint(theme, opacity) {
  const props = new Map(), classes = new Set(), calls = [];
  globalThis.document = {
    documentElement: {
      style: { setProperty: (k, v) => props.set(k, v), removeProperty: (k) => props.delete(k) },
      classList: { add: (c) => classes.add(c), remove: (c) => classes.delete(c), toggle: (c, on) => (on ? classes.add(c) : classes.delete(c)), [Symbol.iterator]: () => classes[Symbol.iterator]() },
    },
    querySelector: () => null,
  };
  globalThis.window = { geckoDesktop: { setTitleBar: (c) => calls.push(c) } };
  try {
    const term = paintTheme(theme, opacity);
    return { props, term, calls };
  } finally {
    delete globalThis.document;
    delete globalThis.window;
  }
}

for (const t of THEMES) {
  test(`${t.id}: at 50% the big surfaces are half see-through, the terminal clear`, () => {
    const { props, term } = paint(t, 0.5);
    assert.match(props.get('--surface'), /^#[0-9a-f]{6}80$/i);
    assert.match(props.get('--surface-panel'), /^#[0-9a-f]{6}80$/i);
    assert.equal(props.get('--surface-term'), 'transparent');
    // xterm's own background: the theme's color at alpha 0 (its contrast
    // checks need the real color), except an LCD that stays solid.
    if (t.lcd) assert.equal(term.background, t.term.background);
    else assert.match(term.background, ALPHA0);
    if (t.image) assert.equal(props.get('--win-opacity'), '0.5');
  });

  test(`${t.id}: solid at 100%`, () => {
    const { props, term, calls } = paint(t, 1);
    const ui = uiTokens(t);
    assert.equal(props.get('--surface'), ui.bg);
    if (!t.image) {
      assert.equal(props.get('--surface-term'), ui.bg);
      assert.equal(term.background, t.term.background);
    }
    assert.deepEqual(calls, [{ symbolColor: ui.text }]);
  });
}

test('CSS: the page, the main area and the terminal let the window show through', () => {
  assert.match(css, /html\.desktop, html\.desktop body \{ background: transparent; \}/);
  assert.match(css, /html\.desktop #main \{ background: var\(--surface\); \}/);
  assert.match(css, /html\.desktop #tabbar, html\.desktop #stage \{ background: transparent; \}/);
  assert.match(css, /\.pane \.xterm, \.pane \.xterm-viewport, \.pane \.xterm-screen \{ background: var\(--surface-term\) !important; \}/);
  // xterm's viewport keeps a native scrollbar that is opaque and has
  // nothing to scroll (xterm draws its own): hidden.
  assert.match(css, /\.pane \.xterm-viewport \{ scrollbar-width: none; \}/);
  assert.match(css, /html\.has-image\.desktop body \{ background: transparent; \}/);
});
