import { test } from 'node:test';
import assert from 'node:assert/strict';
import { BUILD, buildId } from './build.js';

test('build id: commit and time', () => {
  assert.equal(buildId('a1b2c3d', '2026-10-16T23:15:00Z', true), 'a1b2c3d · Oct 16 23:15');
  assert.equal(buildId('a1b2c3d-dirty', '2027-01-05T09:03:59Z', true), 'a1b2c3d-dirty · Jan 5 09:03');
});

test('build id: without a time', () => {
  assert.equal(buildId('a1b2c3d', ''), 'a1b2c3d');
  assert.equal(buildId('a1b2c3d', 'later'), 'a1b2c3d');
  assert.equal(buildId('', ''), 'unknown');
  assert.equal(BUILD.commit, 'dev');
});
