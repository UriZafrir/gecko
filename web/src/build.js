// Which build of the web UI this is: the commit (with -dirty when the tree
// had changes) and when it was built, which make passes to build.mjs. The
// Android app shows the same id for itself, so the two can be compared.

/* global __GECKO_BUILD__ */
export const BUILD = typeof __GECKO_BUILD__ === 'object' ? __GECKO_BUILD__ : { commit: 'dev', time: '' };

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const two = (n) => String(n).padStart(2, '0');

// "a1b2c3d · Oct 17 02:15" in local time (or UTC with utc set); just the
// commit without a usable time.
export function buildId(commit, time, utc = false) {
  const c = commit || 'unknown';
  const d = time ? new Date(time) : null;
  if (!d || isNaN(d)) return c;
  const [mo, day, h, m] = utc
    ? [d.getUTCMonth(), d.getUTCDate(), d.getUTCHours(), d.getUTCMinutes()]
    : [d.getMonth(), d.getDate(), d.getHours(), d.getMinutes()];
  return `${c} · ${MONTHS[mo]} ${day} ${two(h)}:${two(m)}`;
}
