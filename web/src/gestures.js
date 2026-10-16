// The math of touch gestures, kept free of the DOM so it can be tested
// (touch.test.mjs): which way a drag goes, how fast it was going, how a
// flick slows down, and when a pinch or a swipe counts.

// A drag counts once it has moved this far (CSS px); before that it may
// still be a tap or a long-press.
export const SLOP = 10;

// 'v', 'h' or '' (not decided yet) for a drag of dx, dy.
export function classify(dx, dy, slop = SLOP) {
  const ax = Math.abs(dx), ay = Math.abs(dy);
  if (Math.max(ax, ay) < slop) return '';
  return ay >= ax ? 'v' : 'h';
}

// Velocity (px/ms) over the last ~100 ms of samples [{t, y}] (or x).
export function velocity(samples, key = 'y', window = 100) {
  if (samples.length < 2) return 0;
  const last = samples[samples.length - 1];
  let first = samples[0];
  for (let i = samples.length - 2; i >= 0; i--) {
    if (last.t - samples[i].t > window) break;
    first = samples[i];
  }
  const dt = last.t - first.t;
  return dt > 0 ? (last[key] - first[key]) / dt : 0;
}

// A flick's speed after dt ms: exponential friction, the way native lists
// coast (half the speed about every 115 ms).
export const FRICTION = 0.994;
export function decay(v, dt, friction = FRICTION) {
  return v * Math.pow(friction, dt);
}

// Below this (px/ms) a flick has stopped.
export const STOP = 0.02;

// How far a flick at v (px/ms) still travels: the sum of decay to rest.
export function coastDistance(v, friction = FRICTION) {
  return Math.abs(v) < STOP ? 0 : v / -Math.log(friction);
}

// Whole lines in an accumulated drag of px, and what is left over.
export function lines(px, lineHeight) {
  if (!(lineHeight > 0)) return { n: 0, rest: px };
  const n = Math.trunc(px / lineHeight);
  return { n, rest: px - n * lineHeight };
}

export function distance(a, b) {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

export function midpoint(a, b) {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
}

export const MIN_FONT = 8, MAX_FONT = 32;

// The font size a pinch from d0 to d1 apart makes of size.
export function pinchFont(size, d0, d1, min = MIN_FONT, max = MAX_FONT) {
  if (!(d0 > 0) || !(d1 > 0)) return size;
  return Math.max(min, Math.min(max, Math.round(size * (d1 / d0))));
}

// What two fingers are doing: 'pinch' when the gap between them changed
// more than they moved together, 'swipe' when they slid sideways together,
// '' not yet clear.
export function twoFinger(d0, d1, dx, dy, slop = 24) {
  const spread = Math.abs(d1 - d0);
  if (spread < slop && Math.abs(dx) < slop) return '';
  if (Math.abs(dx) > spread && Math.abs(dx) > Math.abs(dy)) return 'swipe';
  return spread >= slop ? 'pinch' : '';
}

// Does a horizontal swipe of dx at speed v (px/ms) switch tabs (or open or
// close the drawer)? Far enough, or a quick flick.
export function swipeDone(dx, v, far = 80, quick = 0.5) {
  if (Math.abs(dx) >= far) return Math.sign(dx);
  if (Math.abs(v) >= quick && Math.abs(dx) >= 30 && Math.sign(v) === Math.sign(dx)) return Math.sign(dx);
  return 0;
}
