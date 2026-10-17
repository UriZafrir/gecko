// The only bridge between the page and the native shell.
const { contextBridge, ipcRenderer } = require('electron');

// The page applies its theme in a module script, which runs before
// DOMContentLoaded; the window shows after that (see createWindow).
window.addEventListener('DOMContentLoaded', () => setTimeout(() => ipcRenderer.send('gecko:painted'), 30));

contextBridge.exposeInMainWorld('geckoDesktop', {
  platform: process.platform,
  // Background transparency: real on macOS/Linux, acrylic on Windows.
  transparent: true,
  // Which desktop build this is, e.g. "a1b2c3d · Oct 17 02:15".
  build: process.env.GECKO_DESKTOP_BUILD || '',
  setBounds: (b) => ipcRenderer.invoke('gecko:set-bounds', b),
});
