// node --test web/src/gestures.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { classify, velocity, decay, coastDistance, lines, pinchFont, twoFinger, swipeDone, STOP } from './gestures.js';

test('classify', () => {
  assert.equal(classify(3, 4), '');
  assert.equal(classify(2, 30), 'v');
  assert.equal(classify(-40, 10), 'h');
  assert.equal(classify(20, -20), 'v'); // a tie scrolls
});

test('velocity uses the last 100 ms', () => {
  const s = [{ t: 0, y: 0 }, { t: 500, y: 10 }, { t: 550, y: 60 }, { t: 600, y: 110 }];
  assert.equal(velocity(s), 1); // 100 px in the last 100 ms, not the slow start
  assert.equal(velocity([{ t: 0, y: 5 }]), 0);
  assert.equal(velocity([{ t: 0, x: 0 }, { t: 50, x: -25 }], 'x'), -0.5);
});

test('a flick slows down and stops', () => {
  let v = 2, t = 0;
  while (Math.abs(v) >= STOP) { v = decay(v, 16); t += 16; }
  assert.ok(t > 500 && t < 1500, `coasted ${t} ms`);
  assert.ok(decay(1, 115) > 0.49 && decay(1, 115) < 0.51); // half in ~115 ms
  assert.equal(coastDistance(0.01), 0);
  assert.ok(coastDistance(1) > 150 && coastDistance(1) < 170);
  assert.ok(coastDistance(-1) < 0);
});

test('lines', () => {
  assert.deepEqual(lines(50, 20), { n: 2, rest: 10 });
  assert.deepEqual(lines(-50, 20), { n: -2, rest: -10 });
  assert.deepEqual(lines(19, 20), { n: 0, rest: 19 });
  assert.deepEqual(lines(5, 0), { n: 0, rest: 5 });
});

test('pinch font', () => {
  assert.equal(pinchFont(12, 100, 200), 24);
  assert.equal(pinchFont(12, 100, 50), 8); // clamped
  assert.equal(pinchFont(20, 100, 300), 32);
  assert.equal(pinchFont(12, 0, 100), 12);
});

test('two fingers', () => {
  assert.equal(twoFinger(200, 205, 5, 3), '');
  assert.equal(twoFinger(200, 300, 10, 0), 'pinch');
  assert.equal(twoFinger(200, 210, -90, 10), 'swipe');
  assert.equal(twoFinger(200, 205, 5, 80), ''); // two fingers going up: neither
});

test('swipes', () => {
  assert.equal(swipeDone(100, 0), 1);
  assert.equal(swipeDone(-90, -0.1), -1);
  assert.equal(swipeDone(40, 0.8), 1); // short but quick
  assert.equal(swipeDone(40, 0.1), 0);
  assert.equal(swipeDone(40, -0.9), 0); // flicked back
});
