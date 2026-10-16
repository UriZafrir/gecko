// node --test web/src/gestures.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { classify, velocity, decay, coastDistance, lines, pinchFont, twoFinger, swipeDone, STOP, flingDistance, flingTau, flingStep, boost, place, rubber, stretch, squeeze, spring, springDone, absorb, SPRING, thumbSize, thumbTop, thumbLine, MIN_THUMB, MAX_FLING } from './gestures.js';

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

test('velocity: a least squares line, and none once the finger stopped', () => {
  const s = [{ t: 0, y: 0 }, { t: 10, y: 21 }, { t: 20, y: 39 }, { t: 30, y: 60 }];
  assert.ok(Math.abs(velocity(s) - 2) < 0.05);
  assert.ok(Math.abs(velocity(s, 'y', 100, 35) - 2) < 0.05);
  assert.equal(velocity(s, 'y', 100, 200), 0); // held still before lifting
});

test('fast flicks go much further than slow ones', () => {
  const slow = flingDistance(1), fast = flingDistance(4);
  assert.ok(slow > 150 && slow < 300, `slow ${slow}`);
  assert.ok(fast / slow > 8, `4x the speed, ${fast / slow}x as far`); // v^1.74
  assert.ok(Math.abs(flingDistance(2) - 650) < 30);
  assert.ok(flingDistance(MAX_FLING) > 6000);
  assert.equal(flingDistance(0.01), 0);
  assert.equal(flingDistance(-2), -flingDistance(2));
  assert.ok(flingDistance(100) === flingDistance(MAX_FLING * 2)); // capped
});

test('a fling slows exponentially and covers its distance', () => {
  for (const v0 of [0.8, 2, -5]) {
    const tau = flingTau(v0);
    let v = v0, x = 0, t = 0;
    while (Math.abs(v) >= STOP) { const s = flingStep(v, 16, tau); x += s.dx; v = s.v; t += 16; }
    const want = flingDistance(v0);
    assert.ok(Math.abs(x - want) / Math.abs(want) < 0.12, `v ${v0}: ${x} of ${want}`);
    // exponential: the same time always takes off the same share of speed
    const a = flingStep(v0, 100, tau).v / v0, b = flingStep(v0 / 2, 100, tau).v / (v0 / 2);
    assert.ok(Math.abs(a - b) < 1e-9);
    assert.ok(t < 6000, `coasted ${t} ms`);
  }
  assert.deepEqual(flingStep(1, 16, 0), { dx: 0, v: 0 });
  // Frames of any length add up to the same place.
  const tau = flingTau(3);
  let x1 = 0, v1 = 3; for (let i = 0; i < 60; i++) { const s = flingStep(v1, 16, tau); x1 += s.dx; v1 = s.v; }
  let x2 = 0, v2 = 3; for (let i = 0; i < 30; i++) { const s = flingStep(v2, 32, tau); x2 += s.dx; v2 = s.v; }
  assert.ok(Math.abs(x1 - x2) < 1e-6);
});

test('flicks in a row add up; the other way does not', () => {
  assert.equal(boost(2, 3), 5);
  assert.equal(boost(-2, -3), -5);
  assert.equal(boost(2, -3), -3);
  assert.equal(boost(0.1, 3), 3); // the last one had stopped
  assert.equal(boost(20, 20), MAX_FLING * 2);
});

test('place: whole lines plus a shift, the incoming edge drawn', () => {
  assert.deepEqual(place(100, 20, 1), { line: 5, shift: 0 });
  assert.deepEqual(place(105, 20, 1), { line: 6, shift: 15 }); // down: next line drawn, shifted down 15
  assert.deepEqual(place(105, 20, -1), { line: 5, shift: -5 }); // up: shifted up 5
  for (const px of [0, 3.5, 47.25, 999.9]) {
    for (const dir of [1, -1]) {
      const { line, shift } = place(px, 18.4, dir);
      assert.ok(Math.abs(line * 18.4 - shift - px) < 1e-9); // drawn exactly at px
      assert.ok(Math.abs(shift) < 18.4);
    }
  }
  assert.deepEqual(place(5, 0, 1), { line: 0, shift: 0 });
});

test('overscroll resists and never passes the size', () => {
  assert.equal(rubber(0, 600), 0);
  assert.equal(rubber(-5, 600), 0);
  assert.ok(rubber(10, 600) > 5 && rubber(10, 600) < 10); // gives way at first
  assert.ok(rubber(600, 600) < 250);
  assert.ok(rubber(1e6, 600) < 600);
  assert.ok(rubber(200, 600) - rubber(100, 600) < rubber(100, 600)); // more pull, less give
  assert.equal(stretch(0, 600), 1);
  assert.ok(stretch(300, 600) > 1.05 && stretch(300, 600) < 1.2);
  assert.equal(stretch(100, 0), 1);
  assert.equal(squeeze(0, 600), 1);
  assert.ok(squeeze(300, 600) < 0.8);
  assert.equal(squeeze(1e9, 600), 0.35);
});

test('the stretch springs back to rest, without overshooting', () => {
  let x = 120, v = 0, t = 0, low = x;
  while (!springDone(x, v)) { ({ x, v } = spring(x, v, 16)); t += 16; low = Math.min(low, x); }
  assert.ok(t > 150 && t < 1000, `settled in ${t} ms`);
  assert.ok(low > -0.5, 'no overshoot');
  // A fling hitting the end: out, then back.
  x = 0; v = absorb(3); let peak = 0; t = 0;
  while (t < 2000 && !(t > 0 && springDone(x, v))) { ({ x, v } = spring(x, v, 16)); peak = Math.max(peak, x); t += 16; }
  assert.ok(peak > 20 && peak < 120, `peak ${peak}`);
  assert.ok(springDone(x, v), 'came back');
  assert.ok(Math.abs(peak - absorb(3) / (SPRING * Math.E)) < 5);
  assert.equal(absorb(-50), absorb(6));
});

test('fast-scroll thumb', () => {
  assert.equal(thumbSize(600, 30, 30), 600); // nothing to scroll
  assert.equal(thumbSize(600, 30, 60), 300);
  assert.equal(thumbSize(600, 30, 20000), MIN_THUMB); // still finger-sized
  assert.equal(thumbTop(600, 100, 0, 500), 0);
  assert.equal(thumbTop(600, 100, 500, 500), 500);
  assert.equal(thumbTop(600, 100, 250, 500), 250);
  assert.equal(thumbTop(600, 100, 9999, 500), 500);
  assert.equal(thumbLine(250, 600, 100, 1000), 500); // the middle
  assert.equal(thumbLine(-40, 600, 100, 1000), 0);
  assert.equal(thumbLine(900, 600, 100, 1000), 1000);
  assert.equal(thumbLine(10, 600, 600, 1000), 0);
  for (const top of [0, 137, 999]) assert.equal(thumbLine(thumbTop(600, 80, top, 999), 600, 80, 999), top);
});
