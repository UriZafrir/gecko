// Touch gestures for phones and tablets (only where the main pointer is a
// finger; a mouse or trackpad is untouched):
//   - one finger up or down scrolls the scrollback by the pixel, a flick
//     coasts like a web page (faster flicks much further), and past either
//     end the terminal stretches and springs back;
//   - a fast-scroll thumb on the right comes up while scrolling (or on a
//     touch near the edge): drag it, or tap the strip, to jump;
//   - pinch changes the font size (previewed, then the terminal refits);
//   - two fingers sideways, or a swipe on the tab bar, change tabs;
//   - a swipe in from the left edge opens the workspaces drawer, a swipe
//     left on it closes it;
//   - one finger sideways moves the cursor (arrow keys, one per character
//     cell; a flick adds more);
//   - a tap focuses the terminal (keyboard up), a long-press opens the
//     selectable text view.
// Every one-finger move on a terminal is ours from the first pixel (and
// touch-action: none in style.css): if the browser got one first it would
// take the drag over as a page scroll, and the moves it still sent would
// come late and couldn't be stopped, which kills flings on real phones.
// The math is in gestures.js.
import {
  SLOP, classify, velocity, releaseVelocity, lastSteps, STOP, lines, distance, midpoint, pinchFont, twoFinger, swipeDone,
  MIN_FLING, flingTau, flingStep, flingDistance, boost, place, stretch, squeeze, spring, springDone, absorb,
  thumbSize, thumbTop, thumbLine, addSample, sampleStats, cells, cursorFling, wheelPlan, wheelDue,
} from './gestures.js';

const reduceMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

// Why a gesture made no fling, for the touch diagnostics ('' when it did).
const noFling = (why, cancelled, flung) => ({
  flung: !!flung,
  why: [flung ? '' : why || 'slower than a fling', cancelled ? 'the browser cancelled the touch' : ''].filter(Boolean).join('; '),
});
const phoneLayout = () => matchMedia('(max-width: 760px)').matches;
const point = (t) => ({ x: t.clientX, y: t.clientY });

// How far in from the left edge a drawer swipe may start (CSS px). The
// Android app keeps the system's back gesture off a strip this wide.
export const EDGE = 32;

// One gesture at a time across the page: the edge swipe claims a touch
// before a terminal sees it.
const shared = { claimed: false };

// Something on top of the terminal (palette, dialog, menu, text view):
// gestures stay out of it.
function overlayOpen(app) {
  return app.paletteOpen || app.textViewOpen || !!app.closeMenu || !document.getElementById('modal').hidden;
}

// When the program uses the mouse (tmux, vim, less, Claude Code) or the
// alternate screen, a drag scrolls as the mouse wheel would: xterm turns
// wheel events into mouse reports, or arrow keys in the alternate screen;
// one event per line, the fractions carried over to the next move.
function wheeler(p) {
  return (n, at) => {
    const t = p.term;
    const screen = t.element?.querySelector('.xterm-screen');
    if (!n || !screen) return;
    const r = screen.getBoundingClientRect();
    const x = at ? Math.min(Math.max(at.x, r.left + 1), r.right - 1) : r.left + r.width / 2;
    const y = at ? Math.min(Math.max(at.y, r.top + 1), r.bottom - 1) : r.top + r.height / 2;
    for (let i = 0; i < Math.abs(n); i++) {
      screen.dispatchEvent(new WheelEvent('wheel', { deltaY: n > 0 ? -1 : 1, deltaMode: WheelEvent.DOM_DELTA_LINE, clientX: x, clientY: y, bubbles: true, cancelable: true }));
    }
  };
}

const wheelMode = (t) => t.buffer.active.type !== 'normal' || t.modes.mouseTrackingMode !== 'none';

// tmux (here or on the far side of ssh) with its mouse off: wheel steps
// would become arrow keys, which tmux hands to the shell (its history), so
// a vertical swipe sends nothing there and says how to turn the mouse on.
function tmuxWithoutMouse(app, p) {
  const t = p.term, s = app.sessions.get(p.id);
  return t.buffer.active.type !== 'normal' && t.modes.mouseTrackingMode === 'none' && !!s && (s.kind === 'tmux' || !!s.remoteTmux);
}

function cellWidth(p) {
  const w = p.term._core?._renderService?.dimensions?.css?.cell?.width;
  return w > 0 ? w : p.el.clientWidth / Math.max(1, p.term.cols);
}

// The arrow key for one cursor step (dir > 0: right), as the terminal's
// cursor key mode wants it.
const arrow = (t, dir) => (t.modes.applicationCursorKeysMode ? '\x1bO' : '\x1b[') + (dir > 0 ? 'C' : 'D');

function cellHeight(p) {
  const h = p.term._core?._renderService?.dimensions?.css?.cell?.height;
  return h > 0 ? h : p.el.clientHeight / Math.max(1, p.term.rows);
}

// Width of the fast-scroll strip on the right that takes touches (the
// thumb drawn in it is thin).
export const THUMB_HIT = 36;

// The scrollback under a finger, by the pixel: the terminal scrolls whole
// lines and its drawing is shifted by the rest. Past either end it
// stretches (Android 12 style) and springs back; a fast-scroll thumb on
// the right shows where you are and can be dragged.
function scrollView(p) {
  const t = p.term, el = p.el;
  let shift = 0; // px the drawing is moved down (see place())
  let pull = 0, edge = ''; // overscroll, raw finger px past 'top' or 'bottom'
  let drawn = -1; // the line we scrolled the terminal to
  let anim = 0, gen = 0, hideT = 0, dragging = false;
  const bar = document.createElement('div');
  bar.className = 'fastscroll';
  const thumb = document.createElement('i');
  bar.appendChild(thumb);
  el.appendChild(bar);
  el.classList.add('touchpane');
  const screen = () => t.element?.querySelector('.xterm-screen');
  const h = () => cellHeight(p);
  const b = () => t.buffer.active;
  const max = () => b().baseY * h();
  const pos = () => b().viewportY * h() - shift;
  const usable = () => b().type === 'normal' && b().baseY > 0;

  const geometry = () => {
    const H = el.clientHeight;
    const size = thumbSize(H, t.rows, b().baseY + t.rows);
    let top = thumbTop(H, size, pos(), max()), len = size;
    if (pull > 0) {
      len = size * squeeze(pull, H);
      top = edge === 'bottom' ? H - len : 0;
    }
    return { H, size, top, len };
  };
  const drawThumb = () => {
    if (!bar.classList.contains('show')) return;
    const g = geometry();
    thumb.style.transform = `translateY(${g.top}px)`;
    thumb.style.height = `${g.len}px`;
  };
  const draw = () => {
    const s = screen();
    if (s) {
      const k = pull > 0 && !reduceMotion() ? stretch(pull, el.clientHeight) : 1;
      s.style.transformOrigin = edge === 'bottom' ? '50% 100%' : '50% 0';
      s.style.transform = k !== 1 ? `translateY(${shift}px) scaleY(${k})` : shift ? `translateY(${shift}px)` : '';
      el.dataset.stretch = k.toFixed(3);
    }
    drawThumb();
  };
  const show = () => {
    if (!usable()) return hide();
    bar.classList.add('show');
    drawThumb();
    clearTimeout(hideT);
    if (!dragging) hideT = setTimeout(hide, 1500);
  };
  const hide = () => { clearTimeout(hideT); bar.classList.remove('show'); };
  const go = (px, dir) => {
    const H = h();
    px = Math.min(max(), Math.max(0, px));
    let { line, shift: sh } = place(px, H, dir);
    if (line > b().baseY) { line = b().baseY; sh = line * H - px; }
    drawn = line;
    if (line !== b().viewportY) t.scrollToLine(line);
    shift = sh;
    draw();
  };
  const stop = () => { cancelAnimationFrame(anim); anim = 0; gen++; };
  // Run frame(dt) every frame while it returns true; a newer animation
  // (one frame may start the next) replaces it.
  const animate = (frame) => {
    stop();
    const id = gen;
    let t0 = performance.now();
    const tick = (now) => {
      if (id !== gen) return;
      const dt = Math.min(48, now - t0);
      t0 = now;
      const more = frame(dt);
      if (id !== gen) return;
      anim = more ? requestAnimationFrame(tick) : 0;
    };
    anim = requestAnimationFrame(tick);
  };
  // Come to rest on a whole line, the nearest one.
  const snap = () => {
    if (!shift) return;
    const H = h();
    const target = Math.round(pos() / H) * H;
    if (reduceMotion()) return go(target, 0);
    animate((dt) => {
      const now = pos();
      const next = Math.abs(target - now) < 0.5 ? target : now + (target - now) * (1 - Math.exp(-dt / 40));
      go(next, target > now ? 1 : -1);
      return next !== target;
    });
  };
  // The stretch springs back (v: pull px/ms, a fling hitting the end).
  const release = (v = 0) => {
    if (reduceMotion()) { pull = 0; edge = ''; draw(); return; }
    let x = pull, vel = v;
    animate((dt) => {
      ({ x, v: vel } = spring(x, vel, dt));
      const done = springDone(x, vel) || x < 0;
      pull = done ? 0 : x;
      if (done) edge = '';
      draw();
      return !done;
    });
  };
  // A finger move of d px (down is positive). Returns the end it ran into.
  const move = (d, stretchy = !reduceMotion()) => {
    let rest = d;
    if (pull > 0) {
      const more = pull + (edge === 'top' ? rest : -rest);
      if (more > 0) { pull = more; draw(); show(); return edge; }
      rest = edge === 'top' ? more : -more;
      pull = 0;
    }
    const px = pos() - rest, m = max();
    let hit = '';
    if (px < 0) { hit = 'top'; go(0, -1); }
    else if (px > m) { hit = 'bottom'; go(m, 1); }
    else go(px, rest < 0 ? 1 : -1);
    if (hit && stretchy) { edge = hit; pull = hit === 'top' ? -px : px - m; draw(); }
    show();
    return hit;
  };
  // A fling at v px/ms (finger direction), tuned like Chrome on Android.
  let vel = 0;
  const fling = (v) => {
    vel = v;
    el.dataset.fling = v.toFixed(2); // for tests
    const tau = flingTau(v);
    // Flings coast with animations off too, as Android's own lists do.
    if (!tau) { vel = 0; return snap(); }
    animate((dt) => {
      const s = flingStep(vel, dt, tau);
      vel = s.v;
      const hit = move(s.dx, false);
      if (hit) {
        const v0 = absorb(vel);
        vel = 0;
        edge = hit;
        pull = 0.01;
        release(v0); // takes over the frames
        return false;
      }
      if (Math.abs(vel) < STOP) { vel = 0; snap(); return false; }
      return true;
    });
  };
  const offScroll = t.onScroll(() => {
    if (b().viewportY === drawn) return;
    shift = 0; // the terminal moved by itself (output, typing): drawn as it is
    draw();
  });
  const offResize = t.onResize(() => { stop(); shift = 0; pull = 0; edge = ''; draw(); });

  return {
    usable, move, fling, snap, release, show, hide, stop, draw,
    get busy() { return !!anim; },
    get velocity() { return anim ? vel : 0; },
    get pulled() { return pull > 0; },
    settle() { if (pull > 0) release(); else snap(); },
    reset() { stop(); shift = 0; pull = 0; edge = ''; draw(); },
    // The thumb strip: is (x, y) on it, and on the thumb itself?
    hit(at) {
      const r = el.getBoundingClientRect();
      if (at.x < r.right - THUMB_HIT || at.y < r.top || at.y > r.bottom) return '';
      if (!usable() || !bar.classList.contains('show')) return 'edge';
      const g = geometry(), y = at.y - r.top;
      return y >= g.top - 16 && y <= g.top + g.len + 16 ? 'thumb' : 'track';
    },
    // Drag the thumb: held where the finger is on it, or by the middle
    // after a tap on the strip (a jump there).
    grab(at, onThumb) {
      stop();
      shift = 0; pull = 0; edge = '';
      const r = el.getBoundingClientRect(), g = geometry();
      dragging = true;
      bar.classList.add('drag');
      const off = onThumb ? at.y - r.top - g.top : g.size / 2;
      const to = (y) => {
        const line = thumbLine(y - r.top - off, g.H, g.size, b().baseY);
        go(line * h(), 0);
        show();
      };
      to(at.y);
      return { to, done: () => { dragging = false; bar.classList.remove('drag'); show(); } };
    },
    dispose() { stop(); hide(); offScroll.dispose(); offResize.dispose(); bar.remove(); },
  };
}

// Gestures on one terminal.
export function attachPane(app, p) {
  const el = p.el;
  const wheel = wheeler(p);
  const sv = scrollView(p);
  let g = null;
  let coast = 0;
  let wheelVel = 0;
  let longPress = 0;
  const stopCoast = () => { cancelAnimationFrame(coast); coast = 0; wheelVel = 0; };
  const keys = (n) => {
    if (!n) return;
    app.conn.input(p.id, arrow(p.term, n).repeat(Math.abs(n)));
  };
  // What the last gesture looked like (⋮ > Touch diagnostics).
  const note = (g, v, extra = {}) => {
    app.touchStats = {
      kind: g.kind, ...sampleStats(g.samples), moves: g.moves, late: g.late,
      v: +v.toFixed(3), fling: Math.round(flingDistance(v)),
      steps: lastSteps(g.samples, g.kind === 'cursor' ? 'x' : 'y'), reduced: reduceMotion(), ...extra,
      at: new Date().toLocaleTimeString(),
    };
  };
  const end = () => {
    clearTimeout(longPress);
    g = null;
  };
  const claim = (ev) => {
    if (ev.cancelable) ev.preventDefault();
    ev.stopPropagation();
  };

  el.addEventListener('touchstart', (ev) => {
    if (shared.claimed || overlayOpen(app)) return;
    const wasMoving = !!coast || sv.busy;
    const flingV = coast ? wheelVel : sv.velocity; // a flick on top of this one adds up
    stopCoast();
    sv.stop();
    clearTimeout(longPress);
    if (ev.touches.length === 1) {
      const at = point(ev.touches[0]);
      const spot = sv.hit(at);
      if (spot === 'thumb' || spot === 'track') {
        g = { kind: 'thumb', drag: sv.grab(at, spot === 'thumb') };
        return claim(ev);
      }
      if (spot === 'edge') sv.show(); // a touch near the edge brings the thumb up
      g = { kind: '', start: at, last: at, acc: 0, flingV, stopped: wasMoving, moves: 0, late: 0, samples: addSample([], ev.timeStamp, at.x, at.y, undefined, performance.now()) };
      if (!wasMoving) {
        longPress = setTimeout(() => {
          if (g && g.kind === '') {
            g.kind = 'done';
            app.openTextView();
          }
        }, 550);
      }
    } else if (ev.touches.length === 2) {
      g?.drag?.done();
      sv.reset();
      const a = point(ev.touches[0]), b = point(ev.touches[1]);
      g = { kind: 'two', what: '', d0: distance(a, b), c0: midpoint(a, b), d1: distance(a, b), c1: midpoint(a, b), size: app.termFontSize(), samples: [{ t: ev.timeStamp, ...midpoint(a, b) }] };
      claim(ev);
    } else {
      end();
    }
  }, { capture: true, passive: false });

  el.addEventListener('touchmove', (ev) => {
    if (!g) return;
    if (g.kind === 'two') {
      if (ev.touches.length < 2) return claim(ev);
      const a = point(ev.touches[0]), b = point(ev.touches[1]);
      g.d1 = distance(a, b);
      g.c1 = midpoint(a, b);
      g.samples.push({ t: ev.timeStamp, ...g.c1 });
      if (!g.what) g.what = twoFinger(g.d0, g.d1, g.c1.x - g.c0.x, g.c1.y - g.c0.y);
      if (g.what === 'pinch') {
        // Preview: scale the drawn terminal around the fingers; the real
        // size (and the refit) comes when they lift.
        const size = pinchFont(g.size, g.d0, g.d1);
        const r = el.getBoundingClientRect();
        el.style.transformOrigin = `${g.c0.x - r.left}px ${g.c0.y - r.top}px`;
        el.style.transform = `scale(${size / g.size})`;
        el.dataset.pinch = size;
      } else if (g.what === 'swipe') {
        el.style.transform = `translateX(${(g.c1.x - g.c0.x) * 0.35}px)`;
      }
      return claim(ev);
    }
    if (g.kind === 'thumb') {
      g.drag.to(ev.touches[0].clientY);
      return claim(ev);
    }
    if (g.kind === 'done' || g.kind === 'none') return claim(ev);
    const at = point(ev.touches[0]);
    g.moves++;
    if (!ev.cancelable) g.late++; // the browser had taken it over
    if (g.kind === '') {
      addSample(g.samples, ev.timeStamp, at.x, at.y, undefined, performance.now());
      const c = classify(at.x - g.start.x, at.y - g.start.y, SLOP);
      if (!c) return claim(ev); // not decided yet, but ours already
      clearTimeout(longPress);
      g.kind = c === 'h' ? 'cursor' : tmuxWithoutMouse(app, p) ? 'none' : wheelMode(p.term) ? 'wheel' : 'scroll';
      if (g.kind === 'none' && !app.tmuxMouseNoted) {
        app.tmuxMouseNoted = true;
        app.toast('tmux has the mouse off, so swiping can\'t scroll it. Turn it on with `tmux set -g mouse on` (or `set -g mouse on` in ~/.tmux.conf).');
      }
      g.last = at; // start counting from here: no jump by the slop
    }
    if (g.kind === 'cursor') {
      g.acc += at.x - g.last.x;
      g.last = at;
      addSample(g.samples, ev.timeStamp, at.x, at.y, undefined, performance.now());
      const { n, rest } = cells(g.acc, cellWidth(p));
      g.acc = rest;
      g.keys = (g.keys || 0) + n;
      keys(n);
      return claim(ev);
    }
    if (g.kind === 'scroll' || g.kind === 'wheel') {
      const dy = at.y - g.last.y;
      g.last = at;
      addSample(g.samples, ev.timeStamp, at.x, at.y, undefined, performance.now());
      if (g.kind === 'scroll') {
        sv.move(dy); // by the pixel: the content stays under the finger
      } else {
        g.acc += dy;
        const { n, rest } = lines(g.acc, cellHeight(p));
        g.acc = rest;
        wheel(n, at);
      }
    }
    claim(ev);
  }, { capture: true, passive: false });

  const finish = (ev, cancelled = false) => {
    if (!g) return;
    if (g.kind === 'two') {
      if (ev.touches.length > 0) return claim(ev); // the other finger is still down
      const what = g.what, d0 = g.d0, d1 = g.d1, size = g.size, dx = g.c1.x - g.c0.x, v = velocity(g.samples, 'x');
      end();
      el.style.transform = '';
      el.style.transformOrigin = '';
      delete el.dataset.pinch;
      if (what === 'pinch') {
        const next = pinchFont(size, d0, d1);
        if (next !== size) app.setFont(next);
      } else if (what === 'swipe') {
        const dir = swipeDone(dx, v);
        if (dir) app.cycleTab(-dir); // swipe left: the next tab
      }
      return claim(ev);
    }
    if (g.kind === 'thumb') {
      g.drag.done();
      end();
      return claim(ev);
    }
    if (g.kind === 'cursor') {
      const { v, why, lift } = releaseVelocity(g.samples, 'x', ev.timeStamp, performance.now());
      const extra = cursorFling(v, cellWidth(p));
      note(g, v, { keys: g.keys || 0, flingKeys: extra, lift, ...noFling(why, cancelled, extra) });
      end();
      if (extra) {
        // The flick's extra steps, sent as it would coast.
        const tau = flingTau(v), cw = cellWidth(p);
        let vel = v, acc = 0, left = Math.abs(extra);
        let t0 = performance.now();
        const step = (now) => {
          const dt = Math.min(48, now - t0);
          t0 = now;
          const s = flingStep(vel, dt, tau);
          vel = s.v;
          acc += s.dx;
          let { n, rest } = cells(acc, cw);
          acc = rest;
          n = Math.sign(n) * Math.min(Math.abs(n), left);
          left -= Math.abs(n);
          keys(n);
          coast = left > 0 && Math.abs(vel) >= STOP ? requestAnimationFrame(step) : 0;
        };
        coast = requestAnimationFrame(step);
      }
      return claim(ev);
    }
    if (g.kind === 'scroll' || g.kind === 'wheel') {
      const r = releaseVelocity(g.samples, 'y', ev.timeStamp, performance.now());
      const still = r.why && r.why !== 'slower than a fling';
      const v = still ? 0 : boost(g.flingV, r.v);
      const pulled = g.kind === 'scroll' && sv.pulled;
      note(g, v, {
        boosted: g.flingV && !still ? +g.flingV.toFixed(3) : 0, lift: r.lift,
        ...(pulled ? { flung: false, why: 'pulled past the end' } : noFling(r.why, cancelled, Math.abs(v) >= MIN_FLING)),
      });
      const kind = g.kind, at = g.last;
      end();
      if (kind === 'scroll') {
        if (!pulled && Math.abs(v) >= MIN_FLING) sv.fling(v);
        else sv.settle();
        return claim(ev);
      }
      const plan = Math.abs(v) >= MIN_FLING ? wheelPlan(v, cellHeight(p)) : null;
      if (plan) {
        // The program scrolls by steps; they go out over time on a page's
        // slowing curve (and no faster than 60 a second).
        app.touchStats.wheelSteps = plan.steps;
        wheelVel = v;
        const t0 = performance.now();
        let sent = 0;
        const step = (now) => {
          const { due, done } = wheelDue(plan, now - t0);
          wheel(due - sent, at);
          sent = due;
          wheelVel = done ? 0 : v; // a new flick on top adds to it
          coast = done ? 0 : requestAnimationFrame(step);
        };
        coast = requestAnimationFrame(step);
      }
      return claim(ev);
    }
    if ((g.kind === '' && g.moves) || g.kind === 'none') note(g, 0);
    const tapped = g.kind === '' && !g.stopped;
    end();
    // A tap: xterm focuses itself (keyboard up), as it always has. A tap
    // that stopped a fling only stops it.
    if (!tapped) claim(ev);
    sv.settle();
  };
  el.addEventListener('touchend', finish, { capture: true, passive: false });
  el.addEventListener('touchcancel', (ev) => {
    // Taken from the page (the system or the app's own gestures): a scroll
    // or swipe still ends as it would have, flinging included.
    if (g && (g.kind === 'scroll' || g.kind === 'wheel' || g.kind === 'cursor')) return finish(ev, true);
    if (g?.kind === 'two') {
      el.style.transform = '';
      delete el.dataset.pinch;
    }
    g?.drag?.done();
    end();
    sv.settle();
    if (ev.cancelable) ev.preventDefault();
  }, { capture: true, passive: false });
  return () => { stopCoast(); sv.dispose(); };
}

// Gestures outside the terminal: the tab bar, the left edge, the drawer.
export function attachPage(app) {
  const side = document.getElementById('side');
  const tabs = document.getElementById('tabs');
  const panes = document.getElementById('panes');
  let g = null;
  const reset = () => {
    side.style.transition = '';
    side.style.transform = '';
    panes.style.transition = '';
    panes.style.transform = '';
  };

  document.addEventListener('touchstart', (ev) => {
    g = null;
    shared.claimed = false;
    if (ev.touches.length !== 1 || overlayOpen(app) || !phoneLayout() && !ev.target.closest?.('#tabbar')) return;
    const at = point(ev.touches[0]);
    const drawer = document.body.classList.contains('drawer');
    let kind = '';
    if (drawer && ev.target.closest?.('#side, #scrim')) kind = 'close';
    else if (!drawer && phoneLayout() && at.x <= EDGE && ev.target.closest?.('#main')) kind = 'open';
    else if (ev.target.closest?.('#tabbar') && tabs.scrollWidth <= tabs.clientWidth + 1) kind = 'tabs'; // a strip that scrolls keeps its scrolling
    if (!kind) return;
    g = { kind, start: at, dir: '', samples: [{ t: ev.timeStamp, ...at }] };
    if (kind === 'open') shared.claimed = true; // the terminal under it stays out
  }, { capture: true, passive: true });

  document.addEventListener('touchmove', (ev) => {
    if (!g) return;
    const at = point(ev.touches[0]);
    const dx = at.x - g.start.x, dy = at.y - g.start.y;
    if (!g.dir) {
      g.dir = classify(dx, dy);
      if (g.dir === 'v') { // a vertical drag: not ours (the drawer's list scrolls)
        g = null;
        shared.claimed = false;
        return;
      }
      if (!g.dir) return;
    }
    g.samples.push({ t: ev.timeStamp, ...at });
    if (g.samples.length > 20) g.samples.shift();
    const w = side.offsetWidth;
    if (g.kind === 'open') {
      side.style.transition = 'none';
      side.style.transform = `translateX(${Math.min(0, -w + Math.max(0, dx))}px)`;
    } else if (g.kind === 'close') {
      side.style.transition = 'none';
      side.style.transform = `translateX(${Math.min(0, dx)}px)`;
    } else {
      panes.style.transition = 'none';
      panes.style.transform = `translateX(${dx * 0.3}px)`;
    }
    if (ev.cancelable) ev.preventDefault();
    ev.stopPropagation();
  }, { capture: true, passive: false });

  const finish = () => {
    if (!g || !g.dir) {
      g = null;
      shared.claimed = false;
      return;
    }
    const last = g.samples[g.samples.length - 1];
    const dx = last.x - g.start.x;
    const dir = swipeDone(dx, velocity(g.samples, 'x'));
    const kind = g.kind;
    g = null;
    shared.claimed = false;
    if (kind === 'tabs') {
      panes.style.transition = 'transform 0.18s ease-out';
      panes.style.transform = '';
      setTimeout(reset, 200);
      if (dir) app.cycleTab(-dir);
      return;
    }
    // Let the drawer's own transition finish the move from where the finger left it.
    side.style.transition = '';
    side.style.transform = '';
    if (kind === 'open' && dir > 0) document.body.classList.add('drawer');
    if (kind === 'close' && dir < 0) app.closeDrawer();
  };
  document.addEventListener('touchend', finish, { capture: true });
  document.addEventListener('touchcancel', () => { g = null; shared.claimed = false; reset(); }, { capture: true });
}
