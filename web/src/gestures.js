// The math of touch gestures, kept free of the DOM so it can be tested
// (gestures.test.mjs): which way a drag goes, how fast it was going, how a
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

// Velocity (px/ms) of the last ~100 ms of samples [{t, y}] (or x): a least
// squares line through them, the way Android's VelocityTracker does it. A
// finger that stopped (nothing for 40 ms before now) has no velocity.
export function velocity(samples, key = 'y', window = 100, now) {
  if (samples.length < 2) return 0;
  const last = samples[samples.length - 1];
  if (now !== undefined && now - last.t > 40) return 0;
  const pts = [];
  for (let i = samples.length - 1; i >= 0 && last.t - samples[i].t <= window; i--) pts.push(samples[i]);
  if (pts.length < 2) return 0;
  let st = 0, sv = 0;
  for (const q of pts) { st += q.t; sv += q[key]; }
  st /= pts.length; sv /= pts.length;
  let num = 0, den = 0;
  for (const q of pts) { num += (q.t - st) * (q[key] - sv); den += (q.t - st) * (q.t - st); }
  return den > 0 ? num / den : 0;
}

// The speed a finger lifted at (time now) leaves with, and when it is 0
// why: { v, why }. A finger held still before lifting (under STILL_PX
// over the last STILL_MS) makes no fling. Samples that didn't move in the
// last ZERO_TAIL_MS are left out (real phones send a repeat of the last
// position right before the lift), so they don't count as holding still.
// A sample's r is when the page got it: the browser stamps moves it
// coalesced (a busy or slow phone) with an earlier time, so a gap counts
// as holding still only if it shows in both clocks (received: at the lift).
export const STILL_MS = 50, STILL_PX = 2, ZERO_TAIL_MS = 20;
export function releaseVelocity(samples, key = 'y', now, received) {
  if (samples.length < 2) return { v: 0, why: 'too few samples' };
  const end = now ?? samples[samples.length - 1].t;
  let n = samples.length;
  while (n > 2 && end - samples[n - 1].t <= ZERO_TAIL_MS && Math.abs(samples[n - 1][key] - samples[n - 2][key]) < 0.01) n--;
  const kept = n === samples.length ? samples : samples.slice(0, n);
  const last = kept[kept.length - 1];
  const got = received !== undefined && last.r !== undefined ? received - last.r : Infinity;
  const lift = [Math.round(end - last.t), got === Infinity ? null : Math.round(got)];
  if (Math.min(end - last.t, got) >= STILL_MS) return { v: 0, why: 'held still before lifting', lift };
  let from = kept[0];
  for (const q of kept) if (end - q.t >= STILL_MS) from = q;
  if (from !== last && end - from.t >= STILL_MS && Math.abs(last[key] - from[key]) < STILL_PX) return { v: 0, why: 'held still before lifting', lift };
  const v = velocity(kept, key, 100);
  return { v, why: Math.abs(v) < MIN_FLING ? 'slower than a fling' : '', lift };
}

// The last few samples as time and distance steps, for the diagnostics.
export function lastSteps(samples, key = 'y', n = 5) {
  const out = [];
  for (let i = Math.max(1, samples.length - n); i < samples.length; i++) {
    out.push({ dt: +(samples[i].t - samples[i - 1].t).toFixed(1), d: +(samples[i][key] - samples[i - 1][key]).toFixed(1) });
  }
  return out;
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

// Flings of the scrollback, tuned like Chrome on Android. Speeds are CSS
// px/ms (a CSS px is a dp in the WebView), limited as Android limits them.
export const MIN_FLING = 0.05, MAX_FLING = 8;

// How far a fling at v goes: Android's fling spline (OverScroller), which
// Chrome uses, so a fast flick goes much further than a slow one (distance
// grows as v^1.74, not v): 0.5 px/ms about 60 px, 2 about 650, 8 about 7000.
export function flingDistance(v) {
  const a = Math.min(Math.abs(v), MAX_FLING * 2);
  if (a < MIN_FLING) return 0;
  return Math.sign(v) * 778 * Math.pow(0.45 * a, 1.7357);
}

// The time constant (ms) of an exponential slowdown that covers exactly
// flingDistance(v): speed v*e^(-t/tau), distance v*tau.
export function flingTau(v) {
  const a = Math.abs(v);
  return a < MIN_FLING ? 0 : Math.abs(flingDistance(v)) / a;
}

// One frame of a fling: the distance moved in dt ms and the new speed.
export function flingStep(v, dt, tau) {
  if (!(tau > 0)) return { dx: 0, v: 0 };
  const k = Math.exp(-dt / tau);
  return { dx: v * tau * (1 - k), v: v * k };
}

// A new flick while one is still going, the same way: they add up (Chrome's
// fling boosting), so a few quick flicks race through a long scrollback.
export function boost(vNow, vNew) {
  if (Math.abs(vNow) < 0.3 || Math.sign(vNow) !== Math.sign(vNew)) return vNew;
  return Math.sign(vNew) * Math.min(MAX_FLING * 2, Math.abs(vNew) + Math.abs(vNow));
}

// Where position px (from the top of the scrollback) is drawn: the first
// line the terminal shows, and the shift of its drawing. Moving down the
// scrollback (dir > 0) the incoming bottom line is drawn whole, moving up
// the incoming top line; the other edge waits for the next line.
export function place(px, lineHeight, dir) {
  if (!(lineHeight > 0)) return { line: 0, shift: 0 };
  let line = dir > 0 ? Math.ceil(px / lineHeight - 1e-6) : Math.floor(px / lineHeight + 1e-6);
  line = Math.max(0, line);
  return { line, shift: line * lineHeight - px };
}

// Overscroll: a pull of px past the end shows as a stretch that resists
// more the further it goes (never past size).
export function rubber(pull, size, c = 0.55) {
  if (!(size > 0) || !(pull > 0)) return 0;
  return (1 - 1 / (pull * c / size + 1)) * size;
}

// The Android 12 stretch of the content for a pull (scale along Y from the
// pulled edge), and the scrollbar thumb's squeeze (a fraction of its length).
export function stretch(pull, size) {
  return 1 + 0.5 * rubber(pull, size) / size || 1;
}
export function squeeze(pull, size) {
  return size > 0 ? Math.max(0.35, 1 - 1.6 * rubber(pull, size) / size) : 1;
}

// A critically damped spring (no wobble) pulling x back to 0, moved on by
// dt ms: how the stretch springs back. omega sets the pace (~300 ms).
export const SPRING = 0.016;
export function spring(x, v, dt, omega = SPRING) {
  const b = v + omega * x;
  const e = Math.exp(-omega * dt);
  return { x: (x + b * dt) * e, v: (v - omega * b * dt) * e };
}
export function springDone(x, v) {
  return Math.abs(x) < 0.5 && Math.abs(v) < 0.01;
}

// A fling that hits the end: the speed that the stretch takes up (it grows,
// then springs back; peak pull is about v * 0.9 / (omega * e)).
export function absorb(v) {
  return Math.min(Math.abs(v), 6) * 0.9;
}

// The fast-scroll thumb: its length on a track of trackPx for rows shown of
// total lines, where it sits for line top of max, and the line a thumb at y
// (its top, px) means.
export const MIN_THUMB = 44;
export function thumbSize(trackPx, rows, total) {
  if (!(total > 0) || rows >= total) return trackPx;
  return Math.min(trackPx, Math.max(MIN_THUMB, trackPx * rows / total));
}
export function thumbTop(trackPx, size, top, max) {
  return max > 0 ? (trackPx - size) * Math.min(1, Math.max(0, top / max)) : 0;
}
export function thumbLine(y, trackPx, size, max) {
  const room = trackPx - size;
  if (!(room > 0)) return 0;
  return Math.round(Math.min(1, Math.max(0, y / room)) * max);
}

// Whole lines in an accumulated drag of px, and what is left over (kept for
// the next move, so slow drags still add up).
export function lines(px, lineHeight) {
  if (!(lineHeight > 0)) return { n: 0, rest: px };
  const n = Math.trunc(px / lineHeight);
  return { n, rest: px - n * lineHeight };
}

// Add a touch sample (time t in ms, position) for velocity(): times only
// go forward, and a sample at the same time as the last replaces it (some
// devices report coalesced moves with one time stamp), so a zero time step
// never skews the speed. Keeps the last max samples.
export function addSample(samples, t, x, y, max = 30, r = t) {
  const last = samples[samples.length - 1];
  if (last && t <= last.t) {
    if (t < last.t - 1000) samples.length = 0; // a clock jump: start over
    else { last.x = x; last.y = y; last.r = r; return samples; }
  }
  samples.push({ t, x, y, r });
  if (samples.length > max) samples.shift();
  return samples;
}

// What a gesture's samples look like, for the touch diagnostics: how many,
// the smallest and largest time step between them (ms) and their span.
export function sampleStats(samples) {
  let lo = Infinity, hi = 0;
  for (let i = 1; i < samples.length; i++) {
    const dt = samples[i].t - samples[i - 1].t;
    lo = Math.min(lo, dt);
    hi = Math.max(hi, dt);
  }
  const span = samples.length > 1 ? samples[samples.length - 1].t - samples[0].t : 0;
  return { n: samples.length, dtMin: lo === Infinity ? 0 : lo, dtMax: hi, span };
}

// A sideways drag moves the cursor: one arrow key per character cell the
// finger moved (the rest carries over, like lines()).
export const cells = lines;

// The extra arrow keys a sideways flick at v (px/ms) adds, as far as it
// would coast, at most cap of them.
export const MAX_CURSOR_FLING = 80;
export function cursorFling(v, cellWidth, cap = MAX_CURSOR_FLING) {
  if (!(cellWidth > 0)) return 0;
  const n = Math.round(Math.abs(flingDistance(v)) / cellWidth);
  return Math.sign(v) * Math.min(cap, n);
}

// A fling where the program scrolls (tmux, less, vim, anything with the
// mouse on or the alternate screen): wheel steps, sent over time on the
// same slowing curve as a page, so the program animates; at most rate
// steps per ms (60 a second) and cap in all, so a hard fling over ssh
// doesn't flood it. steps: how many (signed); tau: the slowdown (ms).
export const WHEEL_RATE = 0.06, WHEEL_CAP = 200, WHEEL_STOP = 0.008;
export function wheelPlan(v, lineHeight, rate = WHEEL_RATE, cap = WHEEL_CAP) {
  if (!(lineHeight > 0)) return null;
  const n = Math.min(cap, Math.round(Math.abs(flingDistance(v)) / lineHeight));
  if (!n) return null;
  return { steps: Math.sign(v) * n, tau: Math.max(flingTau(v), n / rate) };
}

// How many of plan's steps are due t ms in (signed), and whether it has
// slowed to a stop (under WHEEL_STOP steps per ms).
export function wheelDue(plan, t) {
  const n = Math.abs(plan.steps), k = Math.exp(-t / plan.tau);
  return { due: Math.sign(plan.steps) * Math.floor(n * (1 - k)), done: (n / plan.tau) * k < WHEEL_STOP };
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
