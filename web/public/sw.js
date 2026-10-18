// Gecko's service worker: the app can be installed (desktop app window,
// phone home screen), and an installed app still opens when the machine
// running Gecko can't be reached (it then says it is reconnecting and
// connects by itself), instead of the browser's error page, which an
// installed app on iOS can't even reload.
//
// Never stale: everything comes from the network first, always; the copy
// kept here is only used when the network fails. The daemon is the source
// of truth, and a new build replaces the copy on the next load. A page
// opened from the copy reloads into the daemon's own build once it
// connects (main.js, reloadIfNewBuild).
const CACHE = 'gecko-offline-v1';
const SHELL = ['/', '/main.js', '/main.css', '/manifest.webmanifest', '/icon.svg'];

self.addEventListener('install', (e) => {
  self.skipWaiting();
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).catch(() => {}));
});

self.addEventListener('activate', (e) => e.waitUntil((async () => {
  for (const k of await caches.keys()) if (k !== CACHE) await caches.delete(k);
  await self.clients.claim();
})()));

self.addEventListener('fetch', (e) => {
  const req = e.request;
  const url = new URL(req.url);
  // The app's own files only: not the WebSocket, the API or another site.
  if (req.method !== 'GET' || url.origin !== location.origin || url.pathname.startsWith('/api/') || url.pathname === '/ws') return;
  e.respondWith((async () => {
    try {
      const res = await fetch(req); // the daemon sends no-cache: always checked
      if (res.ok && res.type === 'basic' && (SHELL.includes(url.pathname) || url.pathname.startsWith('/assets/'))) {
        const copy = res.clone();
        e.waitUntil(caches.open(CACHE).then((c) => c.put(url.pathname, copy)).catch(() => {}));
      }
      return res;
    } catch (err) {
      const hit = await caches.match(req.mode === 'navigate' ? '/' : url.pathname);
      if (hit) return hit;
      throw err;
    }
  })());
});
