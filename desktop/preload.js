// The only bridge between the page and the native shell.
const { contextBridge, ipcRenderer } = require('electron');

// First launch: no theme cached in this profile yet, so seed the cache
// with the one in settings.json (main.js passes it) before the page reads it.
try {
  const theme = (process.argv.find((a) => a.startsWith('--gecko-theme=')) || '').slice(14);
  if (theme && localStorage.getItem('gecko.theme') === null) localStorage.setItem('gecko.theme', JSON.stringify(theme));
} catch {}

// The page applies its theme in a module script, which runs before
// DOMContentLoaded; the window shows after that (see createWindow).
window.addEventListener('DOMContentLoaded', () => setTimeout(() => ipcRenderer.send('gecko:painted'), 30));

contextBridge.exposeInMainWorld('geckoDesktop', {
  platform: process.platform,
  // The window is see-through on every platform (main.js, windowStyle).
  transparent: true,
  // Which desktop build this is, e.g. "a1b2c3d · Oct 17 02:15".
  build: process.env.GECKO_DESKTOP_BUILD || '',
  setBounds: (b) => ipcRenderer.invoke('gecko:set-bounds', b),
  // Windows: the color of the caption buttons drawn over the tab bar.
  setTitleBar: (c) => ipcRenderer.send('gecko:title-bar', c),
  // The anchor this window chose (web/src/anchor.js), kept in the app's
  // settings so the next launch opens on it: a JSON list of machine names.
  anchorPath: () => ipcRenderer.sendSync('gecko:anchor-get'),
  setAnchorPath: (s) => ipcRenderer.send('gecko:anchor-set', String(s)),
});
