// Touch gestures for phones and tablets (only where the main pointer is a
// finger; a mouse or trackpad is untouched):
//   - one finger up or down scrolls, and a flick coasts;
//   - pinch changes the font size (previewed, then the terminal refits);
//   - two fingers sideways, or a swipe on the tab bar, change tabs;
//   - a swipe in from the left edge opens the workspaces drawer, a swipe
//     left on it closes it;
//   - a tap focuses the terminal (keyboard up), a long-press opens the
//     selectable text view.
// The math is in gestures.js.
import { SLOP, classify, velocity, decay, STOP, lines, distance, midpoint, pinchFont, twoFinger, swipeDone } from './gestures.js';

const reduceMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
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

// A finger drag on the terminal scrolls by whole lines: through the
// scrollback, or, when the program uses the mouse (tmux, vim, less, Claude
// Code) or the alternate screen, as the mouse wheel would.
function scroller(p) {
  return (n, at) => {
    const t = p.term;
    if (!n) return;
    if (t.buffer.active.type === 'normal' && t.modes.mouseTrackingMode === 'none') {
      t.scrollLines(-n);
      return;
    }
    // xterm turns wheel events into mouse reports, or arrow keys in the
    // alternate screen; one event per line.
    const screen = t.element?.querySelector('.xterm-screen');
    if (!screen) return;
    const r = screen.getBoundingClientRect();
    const x = at ? Math.min(Math.max(at.x, r.left + 1), r.right - 1) : r.left + r.width / 2;
    const y = at ? Math.min(Math.max(at.y, r.top + 1), r.bottom - 1) : r.top + r.height / 2;
    for (let i = 0; i < Math.abs(n); i++) {
      screen.dispatchEvent(new WheelEvent('wheel', { deltaY: n > 0 ? -1 : 1, deltaMode: WheelEvent.DOM_DELTA_LINE, clientX: x, clientY: y, bubbles: true, cancelable: true }));
    }
  };
}

function cellHeight(p) {
  const h = p.term._core?._renderService?.dimensions?.css?.cell?.height;
  return h > 0 ? h : p.el.clientHeight / Math.max(1, p.term.rows);
}

// Gestures on one terminal.
export function attachPane(app, p) {
  const el = p.el;
  const scroll = scroller(p);
  let g = null;
  let coast = 0;
  let longPress = 0;
  const stopCoast = () => { cancelAnimationFrame(coast); coast = 0; };
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
    const wasCoasting = !!coast;
    stopCoast();
    clearTimeout(longPress);
    if (ev.touches.length === 1) {
      const at = point(ev.touches[0]);
      g = { kind: '', start: at, last: at, acc: 0, samples: [{ t: ev.timeStamp, ...at }] };
      if (!wasCoasting) {
        longPress = setTimeout(() => {
          if (g && g.kind === '') {
            g.kind = 'done';
            app.openTextView();
          }
        }, 550);
      }
    } else if (ev.touches.length === 2) {
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
    if (g.kind === 'done') return claim(ev);
    const at = point(ev.touches[0]);
    if (g.kind === '') {
      const c = classify(at.x - g.start.x, at.y - g.start.y, SLOP);
      if (!c) return;
      clearTimeout(longPress);
      g.kind = c === 'v' ? 'scroll' : 'done';
      g.last = at; // start counting from here: no jump by the slop
    }
    if (g.kind === 'scroll') {
      g.acc += at.y - g.last.y;
      g.last = at;
      g.samples.push({ t: ev.timeStamp, ...at });
      if (g.samples.length > 20) g.samples.shift();
      const { n, rest } = lines(g.acc, cellHeight(p));
      g.acc = rest;
      scroll(n, at);
    }
    claim(ev);
  }, { capture: true, passive: false });

  const finish = (ev) => {
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
    if (g.kind === 'scroll') {
      let v = velocity(g.samples, 'y');
      let acc = g.acc;
      const at = g.last;
      end();
      if (!reduceMotion() && Math.abs(v) >= STOP) {
        let t0 = performance.now();
        const step = (now) => {
          const dt = Math.min(48, now - t0);
          t0 = now;
          acc += v * dt;
          v = decay(v, dt);
          const { n, rest } = lines(acc, cellHeight(p));
          acc = rest;
          scroll(n, at);
          coast = Math.abs(v) >= STOP ? requestAnimationFrame(step) : 0;
        };
        coast = requestAnimationFrame(step);
      }
      return claim(ev);
    }
    const tapped = g.kind === '';
    end();
    // A tap: xterm focuses itself (keyboard up), as it always has.
    if (!tapped) claim(ev);
  };
  el.addEventListener('touchend', finish, { capture: true, passive: false });
  el.addEventListener('touchcancel', (ev) => {
    if (g?.kind === 'two') {
      el.style.transform = '';
      delete el.dataset.pinch;
    }
    end();
    if (ev.cancelable) ev.preventDefault();
  }, { capture: true, passive: false });
  return () => stopCoast();
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
