// Checks that the desktop app's background opacity really shows what is
// behind the window, on the platform it runs on. It opens a solid magenta
// window (backdrop.cjs), starts Gecko over it with a fresh GECKO_HOME,
// changes the opacity setting live over DevTools, screenshots the screen
// and measures how much magenta shows through an empty part of the
// terminal and of the sidebar:
//
//   opacity 1    nothing of the backdrop (a solid window)
//   opacity 0.5  about half of it
//
// On Windows, where a see-through window has no native frame, it also
// checks the caption buttons and moving and resizing with the mouse.
// CHECK_PORT and CHECK_CDP pick the daemon's and DevTools' ports;
// CHECK_DESKTOP runs another desktop/ dir.
//
// It also screenshots a light theme at 0.7 for a look at text over
// transparency. Writes PNGs and results.json to the out dir and exits 1 when
// a check fails. Needs a built repo: bin/gecko(.exe) (with the web client
// embedded) and desktop/node_modules (Electron). Plain Node, no packages.
//
//   node packaging/desktop/transparency/check.cjs <repo> <out> [label]
//
// Linux: run it in an X server with a compositor (Xvfb plus xcompmgr), as
// .github/workflows/transparency.yml does; ImageMagick's import takes the
// screenshots. macOS: screencapture. Windows: System.Drawing.

const { spawn, execFileSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const zlib = require('node:zlib');

const repo = path.resolve(process.argv[2] || '.');
const out = path.resolve(process.argv[3] || 'out/transparency');
const label = process.argv[4] || 'check';
const PORT = Number(process.env.CHECK_PORT || 17698);
const CDP = Number(process.env.CHECK_CDP || 17699);
const win32 = process.platform === 'win32';
const mac = process.platform === 'darwin';
// CI Linux runners and dev machines rarely have Electron's setuid sandbox set up.
const noSandbox = process.platform === 'linux' ? ['--no-sandbox'] : [];
// CI machines have no GPU: software WebGL, so the terminal uses the WebGL
// renderer as on a real desktop (and the blending fix in web/src/webgl.js is
// part of what is checked).
const gpu = ['--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'];
fs.mkdirSync(out, { recursive: true });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => {
  const l = `[${new Date().toISOString().slice(11, 19)}] ${a.join(' ')}`;
  console.log(l);
  fs.appendFileSync(path.join(out, 'check.log'), l + '\n');
};

// ---------------------------------------------------------------- PNG

// 8-bit RGB or RGBA, not interlaced: what screencapture, import (PNG24:/
// PNG32:) and System.Drawing write.
function readPNG(file) {
  const b = fs.readFileSync(file);
  let p = 8, w = 0, h = 0, depth = 0, type = 0, interlace = 0;
  const idat = [];
  while (p < b.length) {
    const len = b.readUInt32BE(p), kind = b.toString('latin1', p + 4, p + 8), d = b.subarray(p + 8, p + 8 + len);
    if (kind === 'IHDR') { w = d.readUInt32BE(0); h = d.readUInt32BE(4); depth = d[8]; type = d[9]; interlace = d[12]; }
    if (kind === 'IDAT') idat.push(d);
    if (kind === 'IEND') break;
    p += 12 + len;
  }
  if (depth !== 8 || (type !== 2 && type !== 6) || interlace) throw new Error(`${file}: unsupported PNG (depth ${depth}, type ${type}, interlace ${interlace})`);
  const bpp = type === 6 ? 4 : 3, stride = w * bpp;
  const raw = zlib.inflateSync(Buffer.concat(idat));
  const px = Buffer.alloc(h * stride);
  for (let y = 0; y < h; y++) {
    const f = raw[y * (stride + 1)], src = raw.subarray(y * (stride + 1) + 1, (y + 1) * (stride + 1));
    const row = px.subarray(y * stride, (y + 1) * stride), up = y ? px.subarray((y - 1) * stride, y * stride) : null;
    for (let i = 0; i < stride; i++) {
      const a = i >= bpp ? row[i - bpp] : 0, u = up ? up[i] : 0, c = up && i >= bpp ? up[i - bpp] : 0;
      let v = src[i];
      if (f === 1) v += a;
      else if (f === 2) v += u;
      else if (f === 3) v += (a + u) >> 1;
      else if (f === 4) { const q = a + u - c, pa = Math.abs(q - a), pb = Math.abs(q - u), pc = Math.abs(q - c); v += pa <= pb && pa <= pc ? a : pb <= pc ? u : c; }
      row[i] = v & 0xff;
    }
  }
  return { w, h, at: (x, y) => { const o = y * stride + x * bpp; return [px[o], px[o + 1], px[o + 2]]; } };
}

// The median color of a 9x9 patch around (x, y), so a stray glyph or
// cursor pixel doesn't decide it.
function sample(img, x, y) {
  const c = [[], [], []];
  for (let dy = -4; dy <= 4; dy++) for (let dx = -4; dx <= 4; dx++) {
    const v = img.at(Math.min(img.w - 1, Math.max(0, Math.round(x + dx))), Math.min(img.h - 1, Math.max(0, Math.round(y + dy))));
    for (let i = 0; i < 3; i++) c[i].push(v[i]);
  }
  return c.map((a) => a.sort((m, n) => m - n)[40]);
}

// How much of the backdrop shows through: 0 when the pixel is the
// surface's own color, 1 when it is the backdrop's. The surface color is
// what the page paints there at opacity 1. Also how far the pixel is from
// that mix (0-255): a gray or blurred material behind the page instead of
// what is really behind the window lightens the pixel, but not toward
// magenta, and shows up here.
function seen(px, surface, backdrop) {
  let num = 0, den = 0;
  for (let i = 0; i < 3; i++) { num += (px[i] - surface[i]) * (backdrop[i] - surface[i]); den += (backdrop[i] - surface[i]) ** 2; }
  const a = den ? num / den : 0;
  const off = Math.sqrt([0, 1, 2].reduce((t, i) => t + (px[i] - (surface[i] + a * (backdrop[i] - surface[i]))) ** 2, 0));
  return { a, off };
}
const mixed = (c, b, o) => c.map((v, i) => Math.round(v * o + b[i] * (1 - o)));
const hex = (c) => '#' + c.map((v) => v.toString(16).padStart(2, '0')).join('');

// ---------------------------------------------------------------- screen

function screenshot(name) {
  const file = path.join(out, `${label}-${name}.png`);
  if (mac) execFileSync('screencapture', ['-x', file]);
  else if (win32) {
    const ps = `Add-Type -AssemblyName System.Windows.Forms, System.Drawing
[void][System.Windows.Forms.SystemInformation]::VirtualScreen
$b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
$bmp.Save('${file.replace(/'/g, "''")}', [System.Drawing.Imaging.ImageFormat]::Png)`;
    execFileSync('powershell', ['-NoProfile', '-Command', ps]);
  } else execFileSync('import', ['-window', 'root', `PNG24:${file}`]);
  return file;
}

// ---------------------------------------------------------------- DevTools

let ws, seq = 0;
const pending = new Map();
async function connect() {
  for (let i = 0; i < 120; i++) {
    try {
      const list = await (await fetch(`http://127.0.0.1:${CDP}/json/list`)).json();
      const page = list.find((t) => t.type === 'page' && t.url.startsWith(`http://127.0.0.1:${PORT}/`));
      if (page) {
        ws = new WebSocket(page.webSocketDebuggerUrl);
        await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; });
        ws.onmessage = (m) => {
          const d = JSON.parse(m.data);
          if (d.id && pending.has(d.id)) { pending.get(d.id)(d); pending.delete(d.id); }
        };
        return;
      }
    } catch {}
    await sleep(500);
  }
  throw new Error('no Gecko page on DevTools');
}
async function evaluate(expr) {
  const id = ++seq;
  ws.send(JSON.stringify({ id, method: 'Runtime.evaluate', params: { expression: expr, awaitPromise: true, returnByValue: true } }));
  const d = await new Promise((r) => pending.set(id, r));
  if (d.result?.exceptionDetails) throw new Error(JSON.stringify(d.result.exceptionDetails));
  return d.result?.result?.value;
}

// ---------------------------------------------------------------- Windows

// A see-through window on Windows has no native frame (desktop/main.js):
// check that the caption buttons have room and that the mouse still moves
// and resizes it, with real mouse input.
function mouseDrag(x0, y0, x1, y1) {
  const r = Math.round;
  const ps = `Add-Type -Namespace W -Name U -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y); [DllImport("user32.dll")] public static extern void mouse_event(int f, int dx, int dy, int d, int e);'
[void][W.U]::SetCursorPos(${r(x0)}, ${r(y0)}); Start-Sleep -m 300; [W.U]::mouse_event(2, 0, 0, 0, 0); Start-Sleep -m 200
for ($i = 1; $i -le 10; $i++) { [void][W.U]::SetCursorPos(${r(x0)} + ${r(x1 - x0)} * $i / 10, ${r(y0)} + ${r(y1 - y0)} * $i / 10); Start-Sleep -m 40 }
[W.U]::mouse_event(4, 0, 0, 0, 0); Start-Sleep -m 600`;
  execFileSync('powershell', ['-NoProfile', '-Command', ps]);
}
async function windowsFrame(check) {
  const ok = (name, pass, detail) => check(name, pass, detail);
  const geo = () => evaluate(`({ x: screenX, y: screenY, w: outerWidth, h: outerHeight, iw: innerWidth,
    plus: document.querySelector('#new-tab').getBoundingClientRect().right,
    wco: navigator.windowControlsOverlay ? { visible: navigator.windowControlsOverlay.visible, rect: navigator.windowControlsOverlay.getTitlebarAreaRect().toJSON() } : null })`);
  let g = await geo();
  log('window', JSON.stringify(g));
  ok('windows: the caption buttons are drawn over the page', !!g.wco?.visible, JSON.stringify(g.wco));
  ok('windows: the tab bar ends before the caption buttons', g.wco && g.plus <= g.wco.rect.x + g.wco.rect.width, `new tab button ends at ${g.plus}, title bar area ends at ${g.wco && g.wco.rect.x + g.wco.rect.width}`);
  const before = g;
  mouseDrag(g.x + g.iw * 0.6, g.y + 16, g.x + g.iw * 0.6 + 50, g.y + 16 + 30);
  g = await geo();
  ok('windows: dragging the tab bar moves the window', g.x === before.x + 50 && g.y === before.y + 30, `from ${before.x},${before.y} to ${g.x},${g.y}`);
  const b2 = g;
  mouseDrag(g.x + g.w - 3, g.y + g.h - 3, g.x + g.w + 77, g.y + g.h + 37);
  g = await geo();
  ok('windows: dragging the corner resizes the window', g.w > b2.w + 40 && g.h > b2.h + 20, `from ${b2.w}x${b2.h} to ${g.w}x${g.h}`);
  await evaluate(`window.geckoDesktop.setBounds({ x: ${before.x}, y: ${before.y}, width: ${before.w}, height: ${before.h} })`);
  await sleep(800);
}

// ---------------------------------------------------------------- run

async function main() {
  const exe = path.join(repo, 'bin', win32 ? 'gecko.exe' : 'gecko');
  const electron = require(path.join(repo, 'desktop', 'node_modules', 'electron'));
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'gecko-transparency-'));
  const env = { ...process.env, GECKO_HOME: path.join(home, '.gecko-terminal') };
  fs.mkdirSync(path.join(env.GECKO_HOME, 'state'), { recursive: true, mode: 0o700 });
  fs.writeFileSync(path.join(env.GECKO_HOME, 'config.json'), JSON.stringify({ listen: `127.0.0.1:${PORT}`, name: 'transparency-ci' }));
  // Demos use Tokyo Night. Start solid: the opacity is changed live.
  fs.writeFileSync(path.join(env.GECKO_HOME, 'settings.json'), JSON.stringify({ theme: 'tokyo-night', opacity: 1, startupSound: false }));
  fs.writeFileSync(path.join(env.GECKO_HOME, 'no-login-service'), '');

  const results = { label, platform: process.platform, electron: require(path.join(repo, 'desktop', 'node_modules', 'electron', 'package.json')).version, checks: [] };
  const procs = [];
  const run = (cmd, args, extra = {}) => {
    const p = spawn(cmd, args, { env, stdio: ['ignore', fs.openSync(path.join(out, `${label}-${path.basename(args[0] || cmd)}.log`), 'a'), 'pipe'], ...extra });
    p.stderr.on('data', (d) => fs.appendFileSync(path.join(out, `${label}-electron-stderr.log`), d));
    procs.push(p);
    return p;
  };

  try {
    // The backdrop first, so Gecko opens over it.
    const info = path.join(home, 'screen.json');
    run(electron, [path.join(__dirname, 'backdrop.cjs'), `--info=${info}`, ...noSandbox]);
    for (let i = 0; i < 60 && !fs.existsSync(info); i++) await sleep(500);
    const screen = JSON.parse(fs.readFileSync(info, 'utf8'));
    log('screen', JSON.stringify(screen));
    results.screen = screen;
    await sleep(1500);

    const url = execFileSync(exe, ['url'], { env, cwd: home, timeout: 30000 }).toString().split(/\r?\n/)[0].trim();
    execFileSync(exe, ['new', '-n', 'check'], { env, cwd: home, timeout: 30000 });
    const [W, H] = [820, 560];
    const X = screen.workArea.x + 80, Y = screen.workArea.y + 40;
    run(electron, [process.env.CHECK_DESKTOP || path.join(repo, 'desktop'), `--url=${url}`, `--size=${W},${H}`, `--position=${X},${Y}`, `--remote-debugging-port=${CDP}`, ...gpu, ...noSandbox]);
    await connect();
    for (let i = 0; i < 60; i++) {
      if (await evaluate(`!!(window.gecko && document.querySelector('.pane .xterm-screen'))`).catch(() => false)) break;
      await sleep(500);
    }
    await sleep(3000);
    results.page = await evaluate(`({ desktop: !!window.geckoDesktop, transparent: window.geckoDesktop?.transparent, platform: window.geckoDesktop?.platform, classes: document.documentElement.className, renderer: document.querySelector('.pane canvas') ? 'webgl' : 'dom' })`);
    log('page', JSON.stringify(results.page));

    // Where to look, in screen pixels: the middle of the terminal's empty
    // lower half, and the sidebar's empty middle.
    const where = async () => {
      const g = await evaluate(`(() => {
        const r = document.querySelector('.pane .xterm-screen').getBoundingClientRect();
        const s = document.querySelector('#side-body').getBoundingClientRect();
        const bx = (outerWidth - innerWidth) / 2;
        return { cx: screenX + bx, cy: screenY + (outerHeight - innerHeight) - bx, dpr: devicePixelRatio,
          term: [r.left + r.width / 2, r.top + r.height * 0.75], side: [s.left + s.width / 2, s.top + s.height * 0.7] };
      })()`);
      const scale = g.dpr;
      const pt = ([x, y]) => [(g.cx + x) * scale - screen.bounds.x * scale, (g.cy + y) * scale - screen.bounds.y * scale];
      return { term: pt(g.term), side: pt(g.side), scale };
    };
    const shoot = async (name, theme, opacity) => {
      await evaluate(`(() => { const g = window.gecko; g.setSetting('theme', ${JSON.stringify(theme)}); g.setSetting('opacity', ${opacity}); })()`);
      await sleep(2000);
      const pts = await where();
      const file = screenshot(name);
      const img = readPNG(file);
      const sx = img.w / (screen.bounds.width * pts.scale); // a screenshot in another scale than the page's
      const at = (p) => sample(img, p[0] * sx, p[1] * sx);
      // What the page paints there, as the page sees it (at opacity 1 that
      // is the color on screen).
      const paint = await evaluate(`['#main', '#side'].map((q) => getComputedStyle(document.querySelector(q)).backgroundColor.match(/[\\d.]+/g).slice(0, 3).map(Number))`);
      // The backdrop as it came out on this screen (color management can
      // shift it a little): below the window, on the left.
      const back = sample(img, (screen.workArea.x + 30) * pts.scale * sx, (Y + H + 40) * pts.scale * sx);
      return { file, term: at(pts.term), side: at(pts.side), back, paint: { term: paint[0], side: paint[1] } };
    };

    const solid = await shoot('opacity-100', 'tokyo-night', 1);
    const half = await shoot('opacity-50', 'tokyo-night', 0.5);
    const light = await shoot('light-70', 'github-light', 0.7);
    const lightSolid = await shoot('light-100', 'github-light', 1);
    const check = (name, r, lo, hi, detail) => {
      if (typeof r === 'boolean') { // a yes/no check: check(name, pass, detail)
        results.checks.push({ name, ok: r, detail: lo });
        return log(`${r ? 'PASS' : 'FAIL'} ${name}: ${lo}`);
      }
      const ok = r.a >= lo && r.a <= hi && r.off <= 24;
      results.checks.push({ name, seen: Number(r.a.toFixed(3)), off: Math.round(r.off), ok, detail });
      log(`${ok ? 'PASS' : 'FAIL'} ${name}: ${r.a.toFixed(3)} of the backdrop (want ${lo}-${hi}), ${Math.round(r.off)} off the mix (want <= 24); ${detail}`);
    };
    const B = half.back;
    log('backdrop on screen', hex(B));
    results.backdrop = hex(B);
    check('the backdrop is on screen, below the window', B[0] > 190 && B[1] < 90 && B[2] > 190, `${hex(B)}, want about #ff00ff`);
    for (const part of ['term', 'side']) {
      const p = solid.paint[part], lp = lightSolid.paint[part];
      check(`${part}: nothing shows through at opacity 1`, seen(solid[part], p, B), -0.05, 0.05, `painted ${hex(p)}, on screen ${hex(solid[part])}`);
      check(`${part}: half the backdrop shows through at opacity 0.5`, seen(half[part], p, B), 0.35, 0.65, `${hex(p)} at 0.5 is ${hex(half[part])} on screen, ideal ${hex(mixed(p, B, 0.5))}`);
      check(`${part}: light theme at 0.7 shows the backdrop`, seen(light[part], lp, B), 0.15, 0.45, `${hex(lp)} at 0.7 is ${hex(light[part])} on screen, ideal ${hex(mixed(lp, B, 0.7))}`);
    }
    if (win32) await windowsFrame(check);
    await evaluate(`window.gecko.setSetting('opacity', 0.7); window.gecko.setSetting('theme', 'tokyo-night')`);
    await sleep(1500);
    screenshot('tokyo-70');
    await evaluate(`window.gecko.setSetting('theme', 'mt-fuji')`);
    await sleep(2000);
    screenshot('picture-70');
  } catch (e) {
    log('ERROR', e.stack || e.message);
    results.error = String(e.message || e);
  } finally {
    try { ws?.close(); } catch {}
    for (const p of procs.reverse()) try { p.kill(); } catch {}
    try { execFileSync(exe, ['stop'], { env, cwd: home, timeout: 20000 }); } catch {}
    try { fs.copyFileSync(path.join(env.GECKO_HOME, 'state', 'daemon.log'), path.join(out, `${label}-daemon.log`)); } catch {}
  }
  fs.writeFileSync(path.join(out, `${label}-results.json`), JSON.stringify(results, null, 2));
  const failed = results.error || results.checks.some((c) => !c.ok);
  log(failed ? 'FAILED' : 'ALL PASS');
  process.exit(failed ? 1 : 0);
}

main();
