// Make anchor in the desktop app, for real on each platform: two throwaway
// Gecko daemons, "desk" (the one the window opens) and "ubuntu" (one of
// desk's machines, by its Gecko link). Over DevTools it checks that the
// anchor icon is on desk in its own window, makes ubuntu the anchor from
// the machine's menu, checks the icon moved and "via ubuntu" shows, that
// the choice is kept in the app's settings and survives a relaunch, and
// that Back to desk switches back. Screenshots of the window at each step
// and results.json go to the out dir; exits 1 when a check fails.
//
//   node packaging/desktop/anchor/check.cjs <repo> <out>
//
// Needs a built repo: bin/gecko(.exe) and desktop/node_modules (Electron).
// Linux: run it in an X server (Xvfb), as transparency.yml does.
// CHECK_PORT (desk; ubuntu is CHECK_PORT+2) and CHECK_CDP pick the ports.

const { spawn, execFileSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const repo = path.resolve(process.argv[2] || '.');
const out = path.resolve(process.argv[3] || 'out/anchor');
const PORT = Number(process.env.CHECK_PORT || 17708);
const UPORT = PORT + 2;
const CDP = Number(process.env.CHECK_CDP || 17709);
const win32 = process.platform === 'win32';
const noSandbox = process.platform === 'linux' ? ['--no-sandbox'] : [];
const gpu = ['--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'];
fs.mkdirSync(out, { recursive: true });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => {
  const l = `[${new Date().toISOString().slice(11, 19)}] ${a.join(' ')}`;
  console.log(l);
  fs.appendFileSync(path.join(out, 'anchor.log'), l + '\n');
};

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
function cdp(method, params = {}) {
  const id = ++seq;
  ws.send(JSON.stringify({ id, method, params }));
  return new Promise((r) => pending.set(id, r));
}
async function evaluate(expr) {
  const d = await cdp('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
  if (d.result?.exceptionDetails) throw new Error(JSON.stringify(d.result.exceptionDetails).slice(0, 400));
  return d.result?.result?.value;
}
async function until(what, expr, secs = 40) {
  for (let i = 0; i < secs * 4; i++) {
    try { if (await evaluate(`!!(${expr})`)) return true; } catch { /* reloading */ }
    await sleep(250);
  }
  throw new Error('timed out: ' + what);
}
async function shot(name) {
  const d = await cdp('Page.captureScreenshot', { format: 'png' });
  if (d.result?.data) fs.writeFileSync(path.join(out, `${process.platform}-${name}.png`), Buffer.from(d.result.data, 'base64'));
}

// What the page shows about the anchor.
const STATE = `(() => { const g = window.gecko, a = document.querySelector('#side .host.anchor'), v = document.querySelector('#statusline .sl-anchor');
  return { host: g.hostName, proxied: !!g.proxied, door: g.door, anchorRow: a && a.dataset.host, icon: !!(a && a.querySelector('.anchor-badge .anc')),
    edge: a ? getComputedStyle(a.querySelector('.host-row')).boxShadow : '', via: v ? v.textContent : '',
    prev: [...document.querySelectorAll('#side .prev-anchor')].map((e) => e.dataset.prev), desktop: !!window.geckoDesktop }; })()`;

async function main() {
  const exe = path.join(repo, 'bin', win32 ? 'gecko.exe' : 'gecko');
  const electron = require(path.join(repo, 'desktop', 'node_modules', 'electron'));
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'gecko-anchor-'));
  const homeOf = (n) => path.join(root, n, '.gecko-terminal');
  const results = { platform: process.platform, checks: [] };
  const check = (name, ok, detail) => {
    results.checks.push({ name, ok: !!ok, detail });
    log(`${ok ? 'PASS' : 'FAIL'} ${name}${detail ? ': ' + detail : ''}`);
  };
  const procs = [];
  const envOf = (n) => ({ ...process.env, GECKO_HOME: homeOf(n) });
  const setup = (n, cfg) => {
    fs.mkdirSync(path.join(homeOf(n), 'state'), { recursive: true, mode: 0o700 });
    fs.writeFileSync(path.join(homeOf(n), 'config.json'), JSON.stringify(cfg));
    fs.writeFileSync(path.join(homeOf(n), 'settings.json'), JSON.stringify({ theme: 'tokyo-night', opacity: 1, startupSound: false }));
    fs.writeFileSync(path.join(homeOf(n), 'no-login-service'), '');
  };
  let app = null;
  const launch = (u) => {
    app = spawn(electron, [path.join(repo, 'desktop'), `--url=${u}`, '--size=900,600', `--remote-debugging-port=${CDP}`, ...gpu, ...noSandbox],
      { env: envOf('desk'), stdio: ['ignore', 'ignore', fs.openSync(path.join(out, 'electron-stderr.log'), 'a')] });
    procs.push(app);
  };
  const quit = async () => {
    try { ws?.close(); } catch {}
    try { app.kill(); } catch {}
    await sleep(2500);
  };
  try {
    // ubuntu first: desk's config names it by its link.
    setup('ubuntu', { listen: `127.0.0.1:${UPORT}`, name: 'ubuntu' });
    const ubuntuURL = execFileSync(exe, ['url'], { env: envOf('ubuntu'), cwd: root, timeout: 30000 }).toString().split(/\r?\n/)[0].trim();
    execFileSync(exe, ['new', '-n', 'build'], { env: envOf('ubuntu'), cwd: root, timeout: 30000 });
    const tok = new URL(ubuntuURL).searchParams.get('token');
    setup('desk', { listen: `127.0.0.1:${PORT}`, name: 'desk', hosts: [{ name: 'ubuntu', url: `http://127.0.0.1:${UPORT}/`, token: tok }] });
    const url = execFileSync(exe, ['url'], { env: envOf('desk'), cwd: root, timeout: 30000 }).toString().split(/\r?\n/)[0].trim();
    execFileSync(exe, ['new', '-n', 'shell'], { env: envOf('desk'), cwd: root, timeout: 30000 });

    launch(url);
    await connect();
    await until('desk online, ubuntu connected', `window.gecko && gecko.firstSync && gecko.hostName === 'desk' && gecko.hosts.some((h) => h.name === 'ubuntu' && h.status === 'connected')`, 60);
    await sleep(1000);
    let s = await evaluate(STATE);
    log('desk', JSON.stringify(s));
    check('desktop app: the anchor icon is on desk in its own window', s.desktop && s.anchorRow === 'desk' && s.icon && s.edge.includes('inset'), JSON.stringify(s));
    check('desktop app: no "via" on the machine itself', s.via === '', s.via);
    await shot('1-desk');

    // The machine's menu (right-click), Make anchor, confirm.
    await evaluate(`(() => { const b = document.querySelector('#side .host[data-host="ubuntu"] .host-main'), r = b.getBoundingClientRect();
      b.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: r.left + 20, clientY: r.top + 10 })); })()`);
    await until('the menu', `[...document.querySelectorAll('#ctxmenu button')].some((b) => b.textContent.includes('Make anchor'))`);
    await shot('2-menu');
    await evaluate(`[...document.querySelectorAll('#ctxmenu button')].find((b) => b.textContent.includes('Make anchor')).click()`);
    await until('the confirmation', `document.querySelector('#modal [data-yes]')?.textContent === 'Make anchor'`);
    await shot('3-confirm');
    await evaluate(`document.querySelector('#modal [data-yes]').click()`);
    await until('ubuntu as the anchor', `window.gecko && gecko.firstSync && gecko.hostName === 'ubuntu' && gecko.proxied && gecko.hosts.length`, 60);
    await sleep(1200);
    s = await evaluate(STATE);
    log('ubuntu', JSON.stringify(s));
    check('Make anchor: the icon moves to ubuntu', s.anchorRow === 'ubuntu' && s.icon, JSON.stringify(s));
    check('Make anchor: "via ubuntu" in the bottom bar', s.via === 'via ubuntu', s.via);
    check('Make anchor: Back to desk is offered', s.prev.includes('desk'), JSON.stringify(s.prev));
    const saved = path.join(homeOf('desk'), 'state', 'desktop', 'anchor.json');
    let kept = '';
    try { kept = JSON.parse(fs.readFileSync(saved, 'utf8')).path; } catch {}
    check('the choice is in the app\'s settings', kept === '["ubuntu"]', kept);
    await shot('4-ubuntu');

    // Relaunch: the window opens on ubuntu.
    await quit();
    launch(url);
    await connect();
    await until('reopened on ubuntu', `window.gecko && gecko.firstSync && gecko.hostName === 'ubuntu' && gecko.proxied`, 60);
    await sleep(1000);
    s = await evaluate(STATE);
    check('relaunch: the app reopens on ubuntu', s.anchorRow === 'ubuntu' && s.via === 'via ubuntu', JSON.stringify(s));
    await shot('5-relaunched');

    // Back to desk.
    await evaluate(`document.querySelector('#side .prev-anchor[data-prev="desk"] .host-main').click()`);
    await until('back on desk', `window.gecko && gecko.firstSync && gecko.hostName === 'desk' && !gecko.proxied && gecko.hosts.length`, 60);
    await sleep(1200);
    s = await evaluate(STATE);
    check('Back to desk: the icon is on desk again, no via', s.anchorRow === 'desk' && s.via === '' && !s.prev.length, JSON.stringify(s));
    check('Back to desk: the app forgets the choice', !fs.existsSync(saved), saved);
    await shot('6-back');
  } catch (e) {
    log('ERROR', e.stack || e.message);
    results.error = String(e.message || e);
  } finally {
    try { ws?.close(); } catch {}
    for (const p of procs.reverse()) try { p.kill(); } catch {}
    for (const n of ['desk', 'ubuntu']) try { execFileSync(exe, ['stop'], { env: envOf(n), cwd: root, timeout: 20000 }); } catch {}
  }
  fs.writeFileSync(path.join(out, 'anchor-results.json'), JSON.stringify(results, null, 2));
  const failed = results.error || results.checks.some((c) => !c.ok);
  log(failed ? 'FAILED' : 'ALL PASS');
  process.exit(failed ? 1 : 0);
}

main();
