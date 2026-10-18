// The anchor: the Gecko whose tabs and machines this page shows, and which
// dials every other machine in the sidebar. By default it's the Gecko the
// page connects to (the door). Any machine in the sidebar can be made the
// anchor: the page then says hello to the door with the path to it, and
// the door forwards the page there over its own link (protocol 11). The
// sidebar always marks the anchor (an anchor icon and a highlight), and
// the bottom bar says "via <anchor>" whenever the anchor isn't the device
// you're holding. See docs/anchor.md.

const LOOPBACK = new Set(['localhost', '127.0.0.1', '::1', '[::1]', '']);

// isRemotePage reports whether a page at this host name was opened from
// another device than the Gecko it talks to.
export function isRemotePage(hostname) {
  const h = String(hostname || '').toLowerCase().replace(/\.$/, '');
  if (LOOPBACK.has(h) || h.endsWith('.localhost')) return false;
  return !/^127\.\d+\.\d+\.\d+$/.test(h);
}

// splitByAnchor puts the anchor (the local host of the Gecko this page
// talks to) first and the machines it reaches after it, in their order.
export function splitByAnchor(hosts) {
  const anchor = hosts.find((h) => h.local) || null;
  return { anchor, reached: hosts.filter((h) => h !== anchor) };
}

// The first protocol whose Gecko can be made the anchor through another.
export const ANCHOR_PROTOCOL = 11;
// How many Geckos a page may go through (the door enforces it too).
export const MAX_PATH = 4;

// The anchor path: the machines to go through from the door to the
// anchor, each named as the Gecko before it calls it ([]: the door is the
// anchor). It is kept per page origin (so the desktop app, a browser tab
// or the PWA reopens on it) and, in the Android app, per saved machine
// (This phone's address can change).
const KEY = 'gecko.anchorPath';

export function cleanPath(p) {
  if (!Array.isArray(p)) return [];
  const out = p.filter((x) => typeof x === 'string' && x && !/[\s/]/.test(x));
  return out.length > MAX_PATH ? [] : out;
}

export function loadPath(storage, native) {
  // The Android app keeps it per saved machine: that one counts.
  if (typeof native?.anchorPath === 'function') {
    try { return cleanPath(JSON.parse(native.anchorPath() || '[]')); } catch { return []; }
  }
  try { return cleanPath(JSON.parse(storage?.getItem(KEY) || '[]')); } catch { return []; }
}

export function savePath(path, storage, native) {
  const s = JSON.stringify(cleanPath(path));
  try { native?.setAnchorPath?.(s); } catch { /* an older app */ }
  try { path.length ? storage?.setItem(KEY, s) : storage?.removeItem(KEY); } catch { /* storage blocked */ }
}

// pathTo is the path after "Make anchor" on the machine called name, seen
// from a page whose path is path and whose door is door; null when name
// already is the anchor.
export function pathTo(path, door, name) {
  if (!name) return null;
  const cur = path.length ? path[path.length - 1] : door;
  if (name === cur) return null;
  if (name === door) return [];
  const i = path.indexOf(name);
  if (i >= 0) return path.slice(0, i + 1);
  return path.length >= MAX_PATH ? null : [...path, name];
}

// previousAnchors are the Geckos this page went through, nearest first:
// the way back, which the sidebar always offers, so a page is never left
// on an anchor that doesn't list the Gecko it came from.
export function previousAnchors(path, door) {
  if (!path.length || !door) return [];
  const chain = [door, ...path.slice(0, -1)];
  return chain.map((name, i) => ({ name, path: path.slice(0, i) })).reverse();
}

// anchorIsHere: the anchor is the device you're holding (its own window,
// the desktop app, Android's This phone with no other anchor chosen).
export const anchorIsHere = (remotePage, path) => !remotePage && !path.length;

// The marks a sidebar row gets. The anchor always has the icon and the
// highlight, wherever the page runs.
export function hostMarks(h, { here, door = '' }) {
  if (!h.local) return { cls: '', badge: '', where: '' };
  return {
    cls: ' anchor',
    badge: `<span class="badge anchor-badge" title="${here ? 'The anchor: this machine' : 'The anchor'}">${ANCHOR_ICON}<span class="anc-word">anchor</span></span>`,
    where: anchorWhere({ here, door, name: h.name }),
  };
}

// Where the anchor is, said under its name: this machine, the Gecko this
// page connects to, or reached through another one (door).
export function anchorWhere({ here, door, name }) {
  if (here) return 'this machine';
  return door && door !== name ? `through ${door}` : 'connected here';
}

// What the bottom bar and the sidebar say.
export const viaLabel = (name) => `via ${name || 'its Gecko'}`;
export const anchorTitle = (name) => `${name} is the anchor: tabs, machines and agents here are ${name}'s, and new machines are added to ${name}.`;
export const makeAnchorHint = (name) => `Show ${name}'s tabs and machines; ${name} connects to the rest`;

export const ANCHOR_ICON = '<svg class="anc" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="3.2" r="1.6"/><path d="M8 4.8v9.2M5 7.2h6M2.8 9.8c.4 2.4 2.6 4.2 5.2 4.2s4.8-1.8 5.2-4.2"/></svg>';
