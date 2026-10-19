// When the page counts as opened from another device (and so labels the
// Gecko it talks to as the anchor), and how machines are grouped under it.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isRemotePage, splitByAnchor, viaLabel } from './anchor.js';

test('pages on this machine are not remote', () => {
  for (const h of ['localhost', '127.0.0.1', '127.0.0.2', '[::1]', '::1', 'app.localhost', 'LOCALHOST', '']) {
    assert.equal(isRemotePage(h), false, h);
  }
});

test('pages from another device are remote', () => {
  for (const h of ['devbox.tail1234.ts.net', '100.101.102.103', '192.168.1.20', 'devbox', 'devbox.local', '[fd7a:115c:a1e0::1]']) {
    assert.equal(isRemotePage(h), true, h);
  }
});

test('the anchor comes first, its machines after it in order', () => {
  const hosts = [{ name: 'gpu' }, { name: 'devbox', local: true }, { name: 'laptop' }];
  const { anchor, reached } = splitByAnchor(hosts);
  assert.equal(anchor.name, 'devbox');
  assert.deepEqual(reached.map((h) => h.name), ['gpu', 'laptop']);
  assert.deepEqual(splitByAnchor([]), { anchor: null, reached: [] });
});

test('bottom bar label', () => {
  assert.equal(viaLabel('devbox'), 'via devbox');
});

import { pathTo, previousAnchors, loadPath, savePath, cleanPath, anchorIsHere, hostMarks, anchorWhere, MAX_PATH } from './anchor.js';

// Every way the UI runs, and whether the anchor is the device in your hand.
const MODES = [
  { mode: 'desktop app / own window', hostname: '127.0.0.1', path: [], here: true },
  { mode: 'Android This phone', hostname: '127.0.0.1', path: [], here: true },
  { mode: 'Android This phone, ubuntu made the anchor', hostname: '127.0.0.1', path: ['ubuntu'], here: false },
  { mode: 'browser on another computer', hostname: 'devbox.tail1234.ts.net', path: [], here: false },
  { mode: 'PWA through gecko share', hostname: '192.168.1.20', path: [], here: false },
  { mode: 'iOS app on a machine', hostname: '100.101.102.103', path: [], here: false },
  { mode: 'browser, gpu made the anchor through devbox', hostname: 'devbox.local', path: ['gpu'], here: false },
];

test('the anchor row always has the icon and the highlight, in every mode', () => {
  for (const m of MODES) {
    const here = anchorIsHere(isRemotePage(m.hostname), m.path);
    assert.equal(here, m.here, m.mode);
    const door = m.path.length ? 'door' : 'anchor';
    const a = hostMarks({ name: 'anchor', local: true }, { here, door });
    assert.equal(a.cls, ' anchor', m.mode);
    assert.match(a.badge, /class="anc"/, m.mode);
    assert.match(a.badge, /anchor-badge/, m.mode);
    assert.equal(a.where, m.here ? 'this machine' : m.path.length ? 'through door' : 'connected here', m.mode);
    // Other machines: no mark.
    const o = hostMarks({ name: 'gpu', kind: 'link' }, { here, door });
    assert.deepEqual(o, { cls: '', badge: '', where: '' }, m.mode);
  }
});

test('where the anchor is', () => {
  assert.equal(anchorWhere({ here: true, door: 'phone', name: 'phone' }), 'this machine');
  assert.equal(anchorWhere({ here: false, door: 'devbox', name: 'devbox' }), 'connected here');
  assert.equal(anchorWhere({ here: false, door: 'phone', name: 'ubuntu' }), 'through phone');
});

test('Make anchor: forward, back to the door, back along the path', () => {
  assert.deepEqual(pathTo([], 'phone', 'ubuntu'), ['ubuntu']);
  assert.equal(pathTo([], 'phone', 'phone'), null); // already the anchor
  assert.equal(pathTo(['ubuntu'], 'phone', 'ubuntu'), null);
  assert.deepEqual(pathTo(['ubuntu'], 'phone', 'phone'), []); // switching back
  assert.deepEqual(pathTo(['ubuntu'], 'phone', 'gpu'), ['ubuntu', 'gpu']);
  assert.deepEqual(pathTo(['ubuntu', 'gpu'], 'phone', 'ubuntu'), ['ubuntu']);
  assert.deepEqual(pathTo(['ubuntu', 'gpu'], 'phone', 'phone'), []);
  assert.equal(pathTo(['a', 'b', 'c', 'd'], 'phone', 'e'), null); // too far
  assert.equal(pathTo([], 'phone', ''), null);
});

test('the way back is always offered, nearest first', () => {
  assert.deepEqual(previousAnchors([], 'phone'), []);
  assert.deepEqual(previousAnchors(['ubuntu'], 'phone'), [{ name: 'phone', path: [] }]);
  assert.deepEqual(previousAnchors(['ubuntu', 'gpu'], 'phone'), [{ name: 'ubuntu', path: ['ubuntu'] }, { name: 'phone', path: [] }]);
});

const memory = () => {
  const m = new Map();
  return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k), m };
};

test('the choice is remembered: per page origin, and per machine in the Android app', () => {
  const st = memory();
  assert.deepEqual(loadPath(st, null), []);
  savePath(['ubuntu'], st, null);
  assert.deepEqual(loadPath(st, null), ['ubuntu']); // the desktop app, a browser, the PWA, iOS
  savePath([], st, null);
  assert.equal(st.m.size, 0);
  // Android: the app's store counts (This phone's address can change).
  let kept = '';
  const app = { anchorPath: () => kept, setAnchorPath: (s) => { kept = s === '[]' ? '' : s; } };
  savePath(['ubuntu'], st, app);
  assert.equal(kept, '["ubuntu"]');
  st.m.clear();
  assert.deepEqual(loadPath(st, app), ['ubuntu']);
  savePath([], st, app);
  assert.deepEqual(loadPath(st, app), []);
  // An older app without it: the page's own storage.
  savePath(['gpu'], st, { theme() {} });
  assert.deepEqual(loadPath(st, { theme() {} }), ['gpu']);
});

test('only plausible paths are used', () => {
  assert.deepEqual(cleanPath('x'), []);
  assert.deepEqual(cleanPath(['a b', 'ok', 3, '', 'x/y']), ['ok']);
  assert.deepEqual(cleanPath(Array(MAX_PATH + 1).fill('m')), []);
  const st = memory();
  st.setItem('gecko.anchorPath', '{not json');
  assert.deepEqual(loadPath(st, null), []);
});
