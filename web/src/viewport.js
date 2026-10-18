// Phones and tablets in a browser or as an installed web app: the page
// behaves like an app's window.
//   - It never zooms: a pinch on a terminal changes the font size
//     (touch.js); anywhere else it does nothing. Chrome follows the
//     viewport and touch-action (index.html, style.css); iOS Safari ignores
//     both for pinches, so its gesture events are cancelled here, and a
//     page that got zoomed anyway (an older iOS, an accessibility zoom) is
//     put back.
//   - The window is the part of the screen the keyboard leaves: iOS keeps
//     the page full height under the keyboard and pans it to the caret,
//     which hid the key bar and the tab bar and could leave the page
//     shifted after the keyboard closed. The app is sized to the visual
//     viewport (--app-h, --app-top) and the page scrolled back to the top.
//   - html.kb says the keyboard is up (the key bar then drops the home
//     indicator's inset: the keyboard covers it).
export const KB_MIN = 120; // px of the screen taken before it counts as a keyboard

// The keyboard's height from the visual viewport and the tallest the page
// has been at this width (Chrome resizes the page itself; iOS doesn't).
export function keyboardHeight(full, vvHeight, scale = 1) {
  if (scale > 1.01) return 0; // zoomed: the viewport is small for that reason
  const h = full - vvHeight;
  return h >= KB_MIN ? Math.round(h) : 0;
}

export function attachViewport() {
  const root = document.documentElement;
  const vv = window.visualViewport;
  root.classList.add('vv');
  const meta = document.querySelector('meta[name=viewport]');
  const base = meta?.getAttribute('content') || '';

  // iOS: no page zoom by pinching (the terminal's own pinch still gets its
  // touches and gesture events; only the browser's zoom is cancelled).
  for (const t of ['gesturestart', 'gesturechange', 'gestureend']) {
    document.addEventListener(t, (ev) => { if (ev.cancelable) ev.preventDefault(); }, { passive: false });
  }
  // Two fingers outside a terminal: not a page zoom either (a terminal
  // claims its own two-finger touches, touch.js).
  document.addEventListener('touchmove', (ev) => {
    if (ev.touches.length > 1 && ev.cancelable && !ev.target.closest?.('.pane')) ev.preventDefault();
  }, { passive: false });

  let full = 0, width = 0, unzoomT = 0, raf = 0;
  const unzoom = () => {
    // Setting the viewport again snaps iOS back to scale 1.
    if (!meta) return;
    meta.setAttribute('content', base.replace('maximum-scale=1', 'maximum-scale=1.0'));
    requestAnimationFrame(() => meta.setAttribute('content', base));
  };
  const update = () => {
    raf = 0;
    if (!vv) return;
    if (innerWidth !== width) { width = innerWidth; full = 0; } // rotated, split, unfolded
    full = Math.max(full, innerHeight, vv.height);
    const zoomed = vv.scale > 1.01;
    const kb = keyboardHeight(full, vv.height, vv.scale);
    root.classList.toggle('kb', kb > 0);
    if (!zoomed) {
      root.style.setProperty('--app-h', `${Math.round(vv.height)}px`);
      root.style.setProperty('--app-top', `${Math.round(vv.offsetTop)}px`);
    }
    // Keyboard gone: nothing stays scrolled (iOS leaves the page panned).
    if (!kb && !zoomed && (window.scrollY || window.scrollX)) window.scrollTo(0, 0);
    clearTimeout(unzoomT);
    if (zoomed) unzoomT = setTimeout(unzoom, 400);
  };
  const soon = () => { if (!raf) raf = requestAnimationFrame(update); };
  vv?.addEventListener('resize', soon);
  vv?.addEventListener('scroll', soon);
  window.addEventListener('resize', soon);
  window.addEventListener('orientationchange', () => setTimeout(update, 300));
  // A field losing focus is when iOS lets go of the keyboard's pan.
  document.addEventListener('focusout', () => setTimeout(update, 100));
  update();
  return update;
}
