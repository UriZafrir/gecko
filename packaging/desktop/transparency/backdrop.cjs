// A solid magenta window over the whole screen, for check.cjs to put Gecko
// over: whatever magenta shows inside Gecko's window came through it. It
// never takes focus, sits at desktop level where the platform has one, and
// writes the screen's geometry to --info=<file>.
//
//   electron packaging/desktop/transparency/backdrop.cjs --info=screen.json

const { app, BrowserWindow, screen } = require('electron');
const fs = require('node:fs');

const info = (process.argv.find((a) => a.startsWith('--info=')) || '').slice(7);
const page = `<!doctype html><html><body style="margin:0;height:100vh;background:#ff00ff;color:#fff;font:600 40px/1.3 system-ui,sans-serif;display:flex;align-items:center;justify-content:center;text-align:center">
Behind the Gecko window.<br>If you can see magenta through it, transparency works.</body></html>`;

app.whenReady().then(() => {
  const d = screen.getPrimaryDisplay();
  const w = new BrowserWindow({
    ...d.bounds,
    show: false,
    frame: false,
    focusable: false,
    skipTaskbar: true,
    hasShadow: false,
    backgroundColor: '#ff00ff',
    ...(process.platform === 'darwin' ? { type: 'desktop' } : {}),
    ...(process.platform === 'linux' ? { type: 'desktop' } : {}),
  });
  w.loadURL('data:text/html;charset=utf-8,' + encodeURIComponent(page));
  w.once('ready-to-show', () => w.showInactive());
  w.webContents.once('did-finish-load', () => {
    if (info) fs.writeFileSync(info, JSON.stringify({ bounds: d.bounds, workArea: d.workArea, scaleFactor: d.scaleFactor }));
  });
});
