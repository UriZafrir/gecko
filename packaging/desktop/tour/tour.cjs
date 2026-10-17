// Feature tour of the Gecko desktop app, for the demo videos. It drives the
// running app over the Chrome DevTools protocol (start the app with
// --remote-debugging-port=$TOUR_CDP) while the screen is being recorded,
// and writes a caption timeline ("unix-seconds|text" lines) to
// $TOUR_CAPTIONS. Chapters: pairing, another machine, agents, Gecko driven
// from outside, themes, scrolling. A chapter that fails is logged and the
// tour goes on.
//
// Environment:
//   TOUR_CDP            DevTools port of the Gecko window
//   TOUR_PLATFORM       linux | mac | windows
//   TOUR_CAPTIONS       caption timeline file
//   TOUR_BOUNDS         screen area for the window, "x,y,w,h"
//   TOUR_PHONE_BROWSER  Chrome/Chromium for the second device (optional)
//   TOUR_PHONE_LABEL    what the second device is, for its caption
//   TOUR_REMOTE         ssh destination to add as a machine (optional)
//   TOUR_REMOTE_NAME    its name in Gecko
//   TOUR_REMOTE_LABEL   what that machine is, for the caption
//   TOUR_REMOTE_ADDED   1: the machine is already in the config
//   TOUR_AGENTS         0: skip the agents chapter (no agent detection)
//   TOUR_AGENTS_WHY     caption when it is skipped
//   TOUR_TMUX           0: no tmux part in the scrolling chapter
//   TOUR_SKIP           comma-separated chapter numbers to skip
const fs = require('fs');
let pw;
try { pw = require('playwright-core'); } catch { pw = require(process.env.TOUR_PLAYWRIGHT || 'playwright-core'); }

const env = process.env;
const platform = env.TOUR_PLATFORM || 'linux';
const mod = platform === 'mac' ? 'Meta' : 'Control';
const win = platform === 'windows';
const skip = new Set((env.TOUR_SKIP || '').split(',').filter(Boolean).map(Number));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log(`[${new Date().toISOString().slice(11, 19)}]`, ...a);

function caption(text) {
  log('caption:', text);
  if (env.TOUR_CAPTIONS) fs.appendFileSync(env.TOUR_CAPTIONS, `${Date.now() / 1000}|${text}\n`);
}

let page;

// A visible pointer, since clicks sent over DevTools don't move the real one.
async function pointer() {
  await page.evaluate(() => {
    if (document.getElementById('tour-pointer')) return;
    const p = document.createElement('div');
    p.id = 'tour-pointer';
    p.innerHTML = '<svg width="22" height="22" viewBox="0 0 22 22"><path d="M3 2l14 9-6 1.2L8 19z" fill="#fff" stroke="#000" stroke-width="1.4" stroke-linejoin="round"/></svg>';
    p.style.cssText = 'position:fixed;left:0;top:0;z-index:2147483647;pointer-events:none;transition:transform .55s cubic-bezier(.3,.7,.3,1);transform:translate(60vw,60vh);filter:drop-shadow(0 1px 2px rgb(0 0 0/.5))';
    document.documentElement.appendChild(p);
    const st = document.createElement('style');
    st.textContent = '@keyframes tour-ring{from{opacity:.9;transform:translate(-50%,-50%) scale(.3)}to{opacity:0;transform:translate(-50%,-50%) scale(1.6)}}.tour-ring{position:fixed;z-index:2147483646;width:34px;height:34px;border-radius:50%;border:3px solid #7aa2f7;pointer-events:none;animation:tour-ring .6s ease-out forwards}';
    document.head.appendChild(st);
  });
}

async function moveTo(x, y, ms = 650) {
  await page.evaluate(([x, y, ms]) => {
    const p = document.getElementById('tour-pointer');
    if (!p) return;
    p.style.transitionDuration = Math.max(ms - 100, 0) + 'ms';
    p.style.transform = `translate(${x}px,${y}px)`;
  }, [x, y, ms]);
  await sleep(ms);
}

async function ring(x, y) {
  await page.evaluate(([x, y]) => {
    const r = document.createElement('div');
    r.className = 'tour-ring';
    r.style.left = x + 'px';
    r.style.top = y + 'px';
    document.documentElement.appendChild(r);
    setTimeout(() => r.remove(), 700);
  }, [x, y]);
}

// Point at an element, then click it.
async function click(sel, { wait = 700, timeout = 15000 } = {}) {
  const el = typeof sel === 'string' ? page.locator(sel).first() : sel;
  await el.waitFor({ state: 'visible', timeout });
  const b = await el.boundingBox();
  const x = b.x + Math.min(b.width / 2, 40), y = b.y + b.height / 2;
  await pointer();
  await moveTo(x, y);
  await ring(x, y);
  await page.mouse.click(x, y);
  await page.mouse.move(BX + BW - 40, BY + BH / 2);
  await sleep(wait);
}

async function hover(sel) {
  const el = page.locator(sel).first();
  const b = await el.boundingBox();
  if (!b) return;
  await pointer();
  await moveTo(b.x + b.width / 2, b.y + b.height / 2);
}

async function typeSlow(text, delay = 40) {
  await page.keyboard.type(text, { delay });
}

// Type a command in the active terminal and run it.
async function run(cmd, after = 1200) {
  await page.evaluate(() => window.gecko.activePane()?.focus());
  await typeSlow(cmd);
  await sleep(250);
  await page.keyboard.press('Enter');
  await sleep(after);
}

async function palette(query, { enter = true, pause = 900 } = {}) {
  await page.evaluate(() => window.gecko.activePane()?.focus());
  await page.keyboard.press(`${mod}+k`);
  await sleep(500);
  await typeSlow(query, 70);
  await sleep(pause);
  if (enter) await page.keyboard.press('Enter');
  await sleep(600);
}

const sessions = () => page.evaluate(() => [...window.gecko.sessions.values()].map((s) => ({ id: s.id, name: s.name, host: s.host, title: s.title, agent: s.agent || null, workspace: s.workspace })));

async function waitFor(fn, timeout = 20000, every = 250) {
  const end = Date.now() + timeout;
  for (;;) {
    const v = await fn();
    if (v) return v;
    if (Date.now() > end) throw new Error('timed out waiting');
    await sleep(every);
  }
}

// Open a session's tab by clicking it in the sidebar.
async function openTab(id) {
  const row = page.locator(`#side [data-drag="tab"][data-id="${id}"]`).first();
  if (await row.count()) await click(row);
  else await page.evaluate((id) => window.gecko.focusSession(id), id);
  await sleep(500);
}

async function newTabOn(host) {
  const before = new Set((await sessions()).map((s) => s.id));
  await click(`#side button.host-main[data-host="${host}"]`);
  const s = await waitFor(async () => (await sessions()).find((s) => !before.has(s.id) && s.host === host));
  await sleep(1500);
  return s;
}

async function setBounds(x, y, width, height) {
  await page.evaluate((b) => window.geckoDesktop?.setBounds(b), { x, y, width, height });
  await sleep(700);
}

const [BX, BY, BW, BH] = (env.TOUR_BOUNDS || '0,0,1280,800').split(',').map(Number);

// ------------------------------------------------------------------ chapters

async function intro() {
  caption(`Gecko desktop app on ${{ linux: 'Linux (AppImage)', mac: 'macOS', windows: 'Windows' }[platform]}: a feature tour`);
  await setBounds(BX, BY, BW, BH);
  await sleep(2500);
}

async function pairing() {
  caption('A first tab on this machine');
  const local = await page.evaluate(() => window.gecko.hostName);
  const s = await newTabOn(local);
  await run(win ? 'echo "hello from the desktop window"' : 'echo "hello from the desktop window on $(hostname)"', 1500);
  caption('1. Pairing: "Open on another device" shows a QR code and a link with the access token');
  await palette('another device');
  await sleep(4500);
  if (!env.TOUR_PHONE_BROWSER) {
    await click('#modal [data-no]');
    return;
  }
  const url = await page.evaluate(async () => ((await (await fetch('/api/info')).json()).urls || [])[0] || location.href);
  caption(`The other side opens the link: ${env.TOUR_PHONE_LABEL || 'a second client'}`);
  await setBounds(BX, BY, Math.round(BW * 0.66), BH);
  await click('#modal [data-no]');
  const pw0 = Math.round(BW * 0.31), ph = Math.min(BH - 40, 760);
  let phone;
  try {
    phone = await pw.chromium.launchPersistentContext(env.TOUR_PHONE_PROFILE || '', {
      executablePath: env.TOUR_PHONE_BROWSER,
      headless: false,
      args: [`--window-position=${BX + BW - pw0 - 8},${BY + 20}`, `--window-size=${pw0},${ph}`, '--no-first-run', '--no-default-browser-check', '--disable-features=Translate'],
      viewport: { width: pw0 - 16, height: ph - 90 },
      hasTouch: true,
      isMobile: true,
      deviceScaleFactor: 1,
    });
    const p2 = phone.pages()[0] || (await phone.newPage());
    await p2.goto(url);
    await sleep(4000);
    caption('Same tabs on both; what you type on either side lands in the same shell');
    await p2.evaluate((id) => window.gecko?.focusSession(id), s.id);
    await sleep(1500);
    await page.evaluate(() => window.gecko.activePane()?.focus());
    await typeSlow('whoami', 90);
    await page.keyboard.press('Enter');
    await sleep(2500);
    await p2.evaluate(() => window.gecko?.activePane()?.focus());
    await p2.keyboard.type('date', { delay: 110 });
    await p2.keyboard.press('Enter');
    await sleep(3500);
  } finally {
    await phone?.close().catch(() => {});
    await setBounds(BX, BY, BW, BH);
    await page.evaluate(() => window.gecko.activePane()?.show());
  }
}

async function machines() {
  const name = env.TOUR_REMOTE_NAME || 'remote';
  caption(`2. Another machine: ${env.TOUR_REMOTE_LABEL || name}`);
  if (env.TOUR_REMOTE_ADDED !== '1') {
    await click('#side [data-act="add-host"]');
    await page.locator('#modal [name=target]').waitFor();
    await click('#modal [name=target]');
    await typeSlow(env.TOUR_REMOTE, 80);
    await page.locator('#modal [name=name]').fill('');
    await click('#modal [name=name]');
    await typeSlow(name, 80);
    await sleep(600);
    caption('Machines → +: Gecko checks the SSH connection and shows its output');
    await click('#modal [data-main]');
    const st = await waitFor(async () => {
      const t = await page.locator('#modal [data-main]').textContent().catch(() => '');
      return /Install Gecko there|Add machine|Retry/.test(t) && t;
    }, 60000);
    await sleep(2500);
    if (/Install/.test(st)) {
      caption('Gecko is not installed there yet, so it copies itself over (~/.local/bin)');
      await click('#modal [data-main]');
      await waitFor(async () => /Add machine|Retry/.test(await page.locator('#modal [data-main]').textContent().catch(() => '')), 120000);
      await sleep(2500);
    }
    caption('Add machine: its background service now owns the sessions there');
    await click('#modal [data-main]');
  }
  await waitFor(() => page.locator(`#side .host.st-connected[data-host="${name}"]`).count(), 60000);
  await sleep(1500);
  caption(`New tab on ${name}: a shell on the other machine, next to the local tabs`);
  const s = await newTabOn(name);
  await run(win ? 'hostname; uname -a 2>$null; $PSVersionTable.OS' : 'hostname; uname -sr; uptime', 2500);
  if (!win) await run('echo "this tab runs on $(hostname)"', 2000);
  else await run('echo "this tab runs on $(hostname)"', 2000);
  caption('Switch between machines from the sidebar: each tab says where it runs');
  const local = (await sessions()).find((x) => x.host !== name);
  if (local) {
    await openTab(local.id);
    await sleep(2200);
  }
  await openTab(s.id);
  await sleep(2200);
  await hover(`#side .host[data-host="${name}"]`);
  await sleep(1500);
}

async function agents() {
  if (env.TOUR_AGENTS === '0') {
    caption(`3. Managing agents: ${env.TOUR_AGENTS_WHY || 'not available here'}`);
    await sleep(4000);
    return;
  }
  const local = await page.evaluate(() => window.gecko.hostName);
  caption('3. Managing agents: Claude Code in a tab (a stand-in script that draws Claude\'s screens)');
  const s = await newTabOn(env.TOUR_AGENT_HOST || local);
  await run('claude', 2500);
  await typeSlow('fix the failing login test', 60);
  await sleep(400);
  await page.keyboard.press('Enter');
  caption('Working: the tab shows Claude\'s logo and state, the agent dock shows what it is doing');
  await sleep(1500);
  await hover('#dock .agent');
  await sleep(1500);
  // Look at another tab while it works, so its question arrives as a notification.
  const other = (await sessions()).find((x) => x.id !== s.id && !x.agent);
  if (other) await openTab(other.id);
  caption('Meanwhile, in another tab...');
  await waitFor(async () => (await sessions()).find((x) => x.id === s.id && x.agent?.status === 'needs-input'), 30000);
  caption('Claude needs you: a notification, and its choices as buttons in the dock');
  await sleep(1200);
  await hover('#toasts .toast');
  await sleep(2500);
  await hover(`#dock .agent [data-reply="1"]`);
  await sleep(1200);
  caption('Answer from the dock without leaving this tab: 1. Yes');
  await click(`#dock .agent [data-reply="1"]`);
  await waitFor(async () => (await sessions()).find((x) => x.id === s.id && x.agent?.status === 'working'), 15000).catch(() => {});
  await sleep(1500);
  await openTab(s.id);
  await waitFor(async () => (await sessions()).find((x) => x.id === s.id && x.agent?.status === 'idle'), 30000);
  caption('Idle again: quick replies offer the next step');
  await sleep(1500);
  const commit = page.locator('#dock .agent [data-prompt]').filter({ hasText: /commit/i }).first();
  await hover('#dock .agent .replies');
  await sleep(1200);
  if (await commit.count()) {
    caption('Quick reply "Commit + push"');
    await click(commit);
  } else {
    await click('#dock .agent [data-prompt]');
  }
  await waitFor(async () => (await sessions()).find((x) => x.id === s.id && x.agent?.status === 'working'), 15000).catch(() => {});
  await sleep(2000);
  await waitFor(async () => (await sessions()).find((x) => x.id === s.id && x.agent?.status === 'idle'), 30000).catch(() => {});
}

async function managed() {
  const local = await page.evaluate(() => window.gecko.hostName);
  caption('4. Driven from outside: an agent (or a script) runs the gecko CLI and the window follows live');
  const ops = await newTabOn(local);
  await run('gecko new -n build', 2500);
  const build = await waitFor(async () => (await sessions()).find((x) => x.name === 'build'), 15000);
  caption('gecko new: a tab named build appeared in the sidebar');
  await hover(`#side [data-drag="tab"][data-id="${build.id}"]`);
  await sleep(2500);
  const job = win
    ? "gecko send build 'foreach ($i in 1..5) { Write-Output step-$i-of-5; Start-Sleep -Milliseconds 700 }'"
    : `gecko send build 'for i in 1 2 3 4 5; do echo "step $i of 5"; sleep 0.7; done'`;
  await run(job, 400);
  caption('gecko send types into that tab; watch it run there');
  await openTab(build.id);
  await sleep(5000);
  await openTab(ops.id);
  caption('gecko capture reads its screen back');
  await run(win ? 'gecko capture build | Select-Object -Last 4' : 'gecko capture build | tail -n 4', 2500);
  if (env.TOUR_AGENTS !== '0') {
    caption('An outside agent can start agents too: gecko new -n review -- claude, then gecko send');
    await run('gecko new -n review -- claude', 1500);
    await run("gecko send review 'review the login fix'", 2500);
  }
  caption('gecko agents --json: every agent on every machine and what it is doing');
  await run(win ? 'gecko agents --json' : "gecko agents --json | jq -c '.[] | [.where, .agent.status, .agent.detail]'", 5000);
  if (env.TOUR_AGENTS !== '0') {
    caption('The dock lists every agent; the palette finds them too (type claude)');
    await hover('#dock');
    await sleep(2000);
    await palette('claude', { enter: false, pause: 3500 });
    await page.keyboard.press('Escape');
    await sleep(800);
  }
}

async function themes() {
  caption('5. Themes: Change Theme in the palette previews each one live');
  await palette('change theme');
  await sleep(1200);
  for (const t of ['Mt. Fuji', 'Cyberpunk', 'Mission Control', 'Dracula']) {
    await page.locator('#palette input').fill('');
    await typeSlow(t, 90);
    caption(`Theme: ${t}${t === 'Mt. Fuji' || t === 'Cyberpunk' ? ' (a photo theme)' : ''}`);
    await sleep(3000);
  }
  await page.locator('#palette input').fill('');
  await typeSlow('Tokyo Night', 90);
  caption('And back to Tokyo Night');
  await sleep(1500);
  await page.keyboard.press('Enter');
  await sleep(2000);
}

async function scrolling() {
  caption('6. Scrolling: a long output, then the mouse wheel through the scrollback');
  const local = await page.evaluate(() => window.gecko.hostName);
  const all = await sessions();
  const shell = all.find((x) => x.host === local && x.name !== 'build' && !x.agent);
  if (shell) await openTab(shell.id);
  else await newTabOn(local);
  await run(win ? '1..5000' : 'seq 1 5000', 2500);
  const box = await page.locator('#panes .pane.active').boundingBox();
  const x = box.x + box.width / 2, y = box.y + box.height / 2;
  await pointer();
  await moveTo(x, y);
  await page.mouse.move(x, y);
  for (let i = 0; i < 60; i++) { await page.mouse.wheel(0, -120); await sleep(70); }
  await sleep(1200);
  caption('Or drag the scrollbar: straight to line 1 of 5000');
  const slider = page.locator('#panes .pane.active .scrollbar.vertical .slider').first();
  await page.mouse.move(box.x + box.width - 6, y);
  await sleep(400);
  const sb = await slider.boundingBox().catch(() => null);
  if (sb) {
    const sx = sb.x + sb.width / 2, sy = sb.y + sb.height / 2;
    await moveTo(sx, sy);
    await page.mouse.move(sx, sy);
    await page.mouse.down();
    for (let i = 1; i <= 30; i++) { await page.mouse.move(sx, sy - ((sy - box.y) * i) / 30); await moveTo(sx, sy - ((sy - box.y) * i) / 30, 50); }
    await page.mouse.up();
  } else {
    for (let i = 0; i < 400; i++) { await page.mouse.wheel(0, -2000); await sleep(10); }
  }
  await sleep(2500);
  await moveTo(x, y);
  await page.mouse.move(x, y);
  for (let i = 0; i < 25; i++) { await page.mouse.wheel(0, 120); await sleep(80); }
  await sleep(1500);
  caption('Typing jumps back to the bottom');
  await run(win ? 'echo "back at the bottom"' : 'echo "back at the bottom"', 2500);
  if (!win && env.TOUR_TMUX !== '0') {
    caption('Inside tmux the wheel scrolls tmux\'s own history');
    await run('tmux new -s tour', 2000);
    await run('seq 1 3000', 2000);
    for (let i = 0; i < 16; i++) { await page.mouse.wheel(0, -300); await sleep(160); }
    await sleep(2500);
    await page.keyboard.press('q');
    await sleep(800);
    await run('tmux kill-session -t tour', 1500);
  }
}

(async () => {
  const browser = await pw.chromium.connectOverCDP(`http://127.0.0.1:${env.TOUR_CDP}`);
  const ctx = browser.contexts()[0];
  page = ctx.pages().find((p) => !p.url().startsWith('devtools')) || (await ctx.waitForEvent('page'));
  await page.waitForFunction(() => window.gecko && window.gecko.hostName, null, { timeout: 30000 });
  await page.bringToFront().catch(() => {});
  const chapters = [intro, pairing, machines, agents, managed, themes, scrolling];
  let failed = 0;
  for (const [i, ch] of chapters.entries()) {
    if (skip.has(i)) continue;
    try {
      log('chapter', i, ch.name);
      await ch();
    } catch (e) {
      failed++;
      log(`chapter ${ch.name} failed:`, e.stack || e.message);
      await page.keyboard.press('Escape').catch(() => {});
      await page.evaluate(() => { const m = document.getElementById('modal'); if (m) { m.hidden = true; m.innerHTML = ''; } }).catch(() => {});
    }
  }
  caption('end');
  await page.evaluate(() => document.getElementById('tour-pointer')?.remove()).catch(() => {});
  log(`tour done, ${failed} chapter(s) failed`);
  process.exit(failed ? 1 : 0);
})().catch((e) => {
  console.error(e);
  process.exit(2);
});
