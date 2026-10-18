// The anchor: the Gecko this page is connected to, when the page was
// opened from another device (a phone's web app through a share link or
// Tailscale Serve, the mobile apps, another computer's browser). That Gecko
// manages every other machine in the sidebar, so the page says so: "via
// devbox" in the bottom bar, an anchor badge in the sidebar, and its
// machines listed under it. Opened on the machine itself (its own window,
// the desktop app, Android's This phone at 127.0.0.1), nothing changes.
// See docs/anchor.md.

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

// What the bottom bar and the sidebar say.
export const viaLabel = (name) => `via ${name || 'its Gecko'}`;
export const anchorTitle = (name) => `Connected through ${name}: tabs, machines and agents here are ${name}'s, and new machines are added to ${name}.`;

export const ANCHOR_ICON = '<svg class="anc" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="3.2" r="1.6"/><path d="M8 4.8v9.2M5 7.2h6M2.8 9.8c.4 2.4 2.6 4.2 5.2 4.2s4.8-1.8 5.2-4.2"/></svg>';
