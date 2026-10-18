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
