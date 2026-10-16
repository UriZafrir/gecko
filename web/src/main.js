import '@xterm/xterm/css/xterm.css';
import '@fontsource-variable/jetbrains-mono';
import '@fontsource-variable/instrument-sans';
import '@fontsource/barlow/400.css'; // Mission Control skin (DIN-style); only downloaded when used
import '@fontsource/barlow/500.css';
import '@fontsource/barlow/600.css';
// Theme fonts (Latin only; a font downloads only when its theme is in use).
import '@fontsource/zen-maru-gothic/latin-400.css';
import '@fontsource/zen-maru-gothic/latin-700.css';
import '@fontsource/rajdhani/latin-500.css';
import '@fontsource/rajdhani/latin-700.css';
import '@fontsource/orbitron/latin-600.css';
import '@fontsource/share-tech-mono/latin-400.css';
import '@fontsource/zen-kaku-gothic-new/latin-400.css';
import '@fontsource/zen-kaku-gothic-new/latin-700.css';
import '@fontsource/dela-gothic-one/latin-400.css';
import '@fontsource/bebas-neue/latin-400.css';
import '@fontsource/barlow-semi-condensed/latin-500.css';
import '@fontsource/barlow-semi-condensed/latin-700.css';
import '@fontsource/josefin-sans/latin-400.css';
import '@fontsource/josefin-sans/latin-600.css';
import '@fontsource/zilla-slab/latin-500.css';
import '@fontsource/zilla-slab/latin-700.css';
import '@fontsource/ibm-plex-mono/latin-400.css';
import '@fontsource/ibm-plex-mono/latin-600.css';
import '@fontsource/m-plus-rounded-1c/latin-500.css';
import '@fontsource/m-plus-rounded-1c/latin-700.css';
import '@fontsource/mochiy-pop-one/latin-400.css';
import '@fontsource/vt323/latin-400.css'; // Digital Watch LCD
import '@fontsource/michroma/latin-400.css'; // Digital Watch face printing
import './style.css';
import qrcode from 'qrcode-generator';

const splash = window.__splash || { set() {}, done() {} };
splash.set(0.4); // the app's code is here
import { Conn } from './conn.js';
import { Terminal } from '@xterm/xterm';
import { WebglAddon } from '@xterm/addon-webgl';
import { Pane, FONT } from './pane.js';
import { THEMES, AUTO, resolveTheme, paintTheme, swatch } from './themes.js';
import { Keys, prettyCombo, comboOf, ACTIONS, DEFAULTS, DEFAULT_TAB_MODIFIER, TAB_MODIFIERS } from './keys.js';
import { startupChime } from './sound.js';
import { attachPane, attachPage } from './touch.js';
import { BUILD, buildId } from './build.js';
import { siClaude, siGooglegemini, siCursor, siGithubcopilot, siQwen, siOpencode } from 'simple-icons';
import { $, esc, ago, shortPath, copyText, readClipboard, fuzzy, store, save, isTouch, isMac, android } from './util.js';

const MAX_LIVE_PANES = 12;

// Turns the mouse wheel over el into next (+1) / previous (-1) steps for
// elements matching sel. A wheel notch is one step; a trackpad has to
// travel a bit, and its momentum can't fly through a long list.
function onWheelStep(el, sel, step, when = () => true) {
  let acc = 0, lastEvent = 0, lastStep = 0;
  el.addEventListener('wheel', (ev) => {
    const target = ev.target.closest(sel);
    if (!target || ev.ctrlKey || !when()) return; // ctrl+wheel is pinch zoom
    ev.preventDefault();
    const d = Math.abs(ev.deltaY) >= Math.abs(ev.deltaX) ? ev.deltaY : ev.deltaX;
    const now = performance.now();
    if (now - lastEvent > 250) acc = 0;
    lastEvent = now;
    acc += ev.deltaMode ? d * 40 : d; // lines or pages, not pixels
    if (Math.abs(acc) < 40 || now - lastStep < 120) return;
    lastStep = now;
    step(acc > 0 ? 1 : -1, target);
    acc = 0;
  }, { passive: false });
}

// The theme this browser last used. "theme" once meant system/dark/light
// and the theme id was stored as "scheme"; both still load.
function initialTheme() {
  const old = { system: AUTO, dark: 'gecko-dark', light: 'gecko-light' };
  const t = store('theme', null);
  if (t && !old[t]) return t;
  return store('scheme', old[t] || AUTO);
}

// A CSS color (as getComputedStyle gives it, or #rgb/#rrggbb) as [r, g, b].
function toRgb(c) {
  c = c.trim();
  const m = c.match(/^#([\da-f]{3}|[\da-f]{6})$/i);
  if (m) {
    const h = m[1].length === 3 ? m[1].replace(/./g, '$&$&') : m[1];
    return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
  }
  const n = c.match(/[\d.]+/g);
  return n ? n.slice(0, 3).map(Number) : [128, 128, 128];
}

// #rrggbb between two colors (t = 0 is a, 1 is b).
function hexMix(a, b, t) {
  return '#' + a.map((v, i) => Math.round(v + (b[i] - v) * t).toString(16).padStart(2, '0')).join('');
}

// Sort by position in a dragged order; whatever isn't in it yet (new
// tabs, workspaces, machines) follows, sorted by fallback.
function byOrder(list, order, key, fallback) {
  const pos = new Map((order || []).map((k, i) => [k, i]));
  return list.sort((a, b) => {
    const pa = pos.get(key(a)), pb = pos.get(key(b));
    if (pa != null && pb != null) return pa - pb;
    if (pa != null || pb != null) return pa != null ? -1 : 1;
    return fallback(a, b);
  });
}

// A new dragged order: what's shown now, plus what isn't (a machine that's
// offline keeps the place of its tabs), capped so it can't grow forever.
function newOrder(shown, old) {
  const seen = new Set(shown);
  return [...shown, ...(old || []).filter((k) => !seen.has(k))].slice(0, 500);
}

// Settings saved in ~/.gecko-terminal/settings.json (the rest of what the
// browser stores is per-window state, like which tab is open).
const SYNCED = ['theme', 'opacity', 'fontSize', 'mobileFontSize', 'copyOnSelect', 'smartCopy', 'optionIsMeta', 'renderer', 'sidebarHidden', 'keys', 'startupSound', 'order', 'quickReplies'];

// Protocol version this client speaks; see internal/proto.
const PROTOCOL = 7;
const WATCH_MIN_COLS = 20, WATCH_MIN_ROWS = 3; // the Digital Watch LCD
const OUTDATED = 'The Gecko background service is older than this window, so some features are missing. Click to restart it.';

// Next steps offered on an idle agent's card; each sends its text as the
// agent's next prompt. Editable (setting "quickReplies"), one per line:
// "Label | text to send", or just the text.
// The everyday nudges as typed; the rest short names for full prompts.
// One line each, since a newline would send early.
const QUICK_REPLIES = [
  'Go | go',
  'Commit + push | commit and push',
  'Try harder | try harder',
  'Dig deeper | dig deeper',
  'Review | Review everything you changed in this session as a strict senior reviewer: correctness bugs, edge cases, error handling, security, performance, and anything left half done. List the findings most severe first with file and line references, then fix the ones that are clearly worth fixing.',
  'Test | Test what you built end to end, the way a user would, not only with unit tests: run it, try the main paths and the edge cases, and tell me exactly what you ran and what happened. Fix whatever fails and test again.',
  'Explain | Explain what you just did in plain language for someone new to this code: the problem, your approach, how the pieces fit together, and anything surprising. Keep it short and concrete, with file references.',
  'Summarize | Summarize this session in well structured Markdown: the goal, what was done and why, the key decisions, the files changed, how it was tested, and open issues or next steps. Save it as SUMMARY.md, convert it to a nicely styled PDF (with pandoc or another tool you have), and tell me where both files are.',
];

function parseQuickReplies(lines) {
  return (Array.isArray(lines) ? lines : QUICK_REPLIES).map((l) => {
    // "Label | text": only the first | separates; the text may have more.
    l = String(l);
    const cut = l.indexOf('|');
    const a = (cut < 0 ? l : l.slice(0, cut)).trim(), b = cut < 0 ? '' : l.slice(cut + 1).trim();
    const text = b || a;
    return text ? { label: b ? a || text : text[0].toUpperCase() + text.slice(1), text } : null;
  }).filter(Boolean).slice(0, 8);
}

const STATUS_TEXT = { working: 'Working', 'needs-input': 'Needs you', idle: 'Idle', running: 'Running' };

function basename(p) {
  if (!p) return '';
  const parts = p.replace(/\/+$/, '').split('/');
  return parts[parts.length - 1] || '/';
}

export function tabLabel(s) {
  // tmux tabs are titled by the active tmux window, unless named by hand
  // (older Gecko named tmux tabs after their session automatically).
  if (s.kind === 'tmux' && s.tmuxWindow && (!s.name || s.name === s.tmuxSession || /^tmux /.test(s.name))) return s.tmuxWindow;
  if (s.name) return s.name;
  if (s.agent) return s.agent.name;
  if ((s.kind === 'ssh' || s.kind === 'mosh') && s.remote) return s.remote;
  if (s.kind === 'tmux' && s.tmuxSession) return s.tmuxSession; // the status line says it's tmux
  if (s.kind !== 'shell' && s.proc) return s.proc;
  return s.repo || basename(s.cwd) || s.shell || 'shell';
}

// Git info drawn by Gecko itself, so prompts don't need powerline or a
// patched font: branch icon, branch, and working-tree state.
// A robot face: agents, counted next to it in a workspace's title.
const AGENTS_ICON = '<svg class="agi" viewBox="0 0 16 16" aria-hidden="true"><rect x="2.5" y="3.75" width="11" height="8.5" rx="2.2"/><circle cx="5.9" cy="8" r="1.05" class="f"/><circle cx="10.1" cy="8" r="1.05" class="f"/></svg>';
const BRANCH_ICON = '<svg class="gi" viewBox="0 0 16 16" aria-hidden="true"><circle cx="4.5" cy="3.5" r="1.7"/><circle cx="4.5" cy="12.5" r="1.7"/><circle cx="11.5" cy="5" r="1.7"/><path d="M4.5 5.2v5.6M11.5 6.7c0 3-6.2 2.2-6.8 4.1"/></svg>';

function gitStateText(g) {
  if (!g) return '';
  const bits = [];
  if (g.changed) bits.push(`${g.changed} changed`);
  if (g.untracked) bits.push(`${g.untracked} untracked`);
  if (g.ahead) bits.push(`${g.ahead} ahead`);
  if (g.behind) bits.push(`${g.behind} behind`);
  return bits.length ? bits.join(', ') : 'clean';
}

// full: also show counts and ahead/behind (status line). Otherwise just a
// dot when there are uncommitted changes (tabs).
function gitChip(s, full = false) {
  if (!s.repo) return '';
  const g = s.git;
  const prefix = tabLabel(s) === s.repo && !full ? '' : `<span class="repo-name">${esc(s.repo)}</span>`;
  let state = '';
  if (g) {
    if (full) {
      if (g.changed) state += `<b class="dirty">●${g.changed}</b>`;
      if (g.untracked) state += `<b class="untracked">?${g.untracked}</b>`;
      if (g.ahead) state += `<b class="ab">↑${g.ahead}</b>`;
      if (g.behind) state += `<b class="ab">↓${g.behind}</b>`;
    } else if (g.changed || g.untracked) {
      state = '<b class="dirty">●</b>';
    }
  }
  const tip = `${s.repo} on ${s.branch || '?'}${g ? ' · ' + gitStateText(g) : ''}`;
  return `<span class="git" title="${esc(tip)}">${prefix}${s.branch ? BRANCH_ICON + `<span class="br">${esc(s.branch)}</span>` : ''}${state}</span>`;
}

// Where in the repo the tab is, e.g. "src/auth" (empty at the root).
function repoPath(s) {
  if (!s.repoRoot || !s.cwd || !s.cwd.startsWith(s.repoRoot)) return '';
  return s.cwd.slice(s.repoRoot.length).replace(/^\/+/, '');
}

// Text with `code` spans, as in Markdown: hostnames, commands and names in
// messages stand out from the sentence around them.
function md(text) {
  return esc(text).replace(/`([^`]+)`/g, '<code>$1</code>');
}

// Compact condensed tags (SSH, MOSH, TMUX) for the status line, in the
// theme's accent. The tab title stays just the name.
const tag = (text) => `<span class="ctag" aria-hidden="true">${text}</span>`;
const TMUX_ICON = tag('TMUX');

// Tabs show their number (the Alt/⌘+N shortcut) in a square whose color
// carries the state: pink needs you, ochre working, sage idle agent.
// Logos of the coding agents Gecko recognizes (internal/inspect), from
// Simple Icons. Agents without one there get a terminal prompt.
const AGENT_LOGOS = {
  claude: siClaude.path, gemini: siGooglegemini.path, cursor: siCursor.path,
  copilot: siGithubcopilot.path, qwen: siQwen.path, opencode: siOpencode.path,
};
const AGENT_PROMPT = 'M2.6 5.4 4.2 3.8 12.4 12l-8.2 8.2-1.6-1.6L9.2 12zM12.5 18h9v2.4h-9z';

// A tab's agent, as its logo in the color of what it's doing (working,
// needs you, idle). Where it's shown, the tab number keeps out of it.
function agentMark(s) {
  if (!s.agent) return '';
  const st = s.agent.status;
  // The working fade keeps time with the clock, so the frequent re-renders
  // of the sidebar and tab bar don't restart it (cycle as in style.css).
  const phase = st === 'working' ? ` style="animation-delay:-${Date.now() % 2000}ms"` : '';
  return `<svg class="alogo s-${esc(st)} a-${esc(s.agent.name)}"${phase} viewBox="0 0 24 24" aria-hidden="true"><title>${esc(`${s.agent.name}: ${STATUS_TEXT[st] || st}`)}</title><path d="${AGENT_LOGOS[s.agent.name] || AGENT_PROMPT}"/></svg>`;
}

// The icon of a workspace's agents in one state: their logo when they are
// all the same agent, else the robot face; it moves like a tab's logo
// while they work (same cycle and phase as agentMark).
function groupAgentMark(agents, status) {
  const names = [...new Set(agents.map((t) => t.agent.name))];
  const phase = status === 'working' ? ` style="animation-delay:-${Date.now() % 2000}ms"` : '';
  if (names.length !== 1) return AGENTS_ICON.replace('class="agi"', `class="agi s-${esc(status)}"${phase}`);
  return `<svg class="alogo s-${esc(status)} a-${esc(names[0])}"${phase} viewBox="0 0 24 24" aria-hidden="true"><path d="${AGENT_LOGOS[names[0]] || AGENT_PROMPT}"/></svg>`;
}

// The logo of the agent in a tmux window (its status isn't tracked there).
function windowAgentMark(w) {
  if (!w.agent) return '';
  return `<svg class="alogo w-agent a-${esc(w.agent)}" viewBox="0 0 24 24" aria-hidden="true"><title>${esc(w.agent + ' runs here')}</title><path d="${AGENT_LOGOS[w.agent] || AGENT_PROMPT}"/></svg>`;
}

function numGlyph(s, n, plain = false) {
  if (plain && s.agent) s = { ...s, agent: null, running: true };
  const st = s.agent ? s.agent.status : s.attention ? 'bell' : s.running || (s.kind && s.kind !== 'shell') ? 'busy' : '';
  const label = s.agent ? STATUS_TEXT[s.agent.status] || s.agent.status : st === 'bell' ? 'Bell' : '';
  return `<i class="num ${st ? 's-' + st : ''}"${label ? ` title="${esc(label)}"` : ''}>${n}</i>`;
}

function glyph(s) {
  if (s.agent) return `<i class="dot s-${s.agent.status}" title="${esc(STATUS_TEXT[s.agent.status] || s.agent.status)}"></i>`;
  if (s.attention) return '<i class="dot s-bell" title="Bell"></i>';
  if (s.running || (s.kind !== 'shell' && s.kind)) return '<i class="dot s-busy"></i>';
  return '<i class="dot"></i>';
}

class App {
  constructor() {
    this.sessions = new Map();
    this.hosts = [];
    this.tmux = [];
    this.templates = [];
    this.hostName = '';
    this.panes = new Map();
    this.exited = new Map(); // id -> info of sessions that failed while shown
    this.closing = new Set(); // ids the user closed
    this.touch = isTouch;
    // The Android app serves this UI from the APK for every machine.
    this.bundled = (() => { try { return !!android?.bundled?.(); } catch { return false; } })();
    // Running inside the Gecko desktop app (Electron) rather than a browser.
    this.desktop = window.geckoDesktop || null;
    this.settings = {
      fontSize: store('fontSize', 13), // desktop
      mobileFontSize: store('mobileFontSize', 12), // phones and tablets
      copyOnSelect: store('copyOnSelect', false),
      smartCopy: store('smartCopy', true),
      optionIsMeta: store('optionIsMeta', true),
      theme: initialTheme(),
      renderer: store('renderer', 'auto'), // auto | webgl | dom
      opacity: store('opacity', 0.9), // desktop app background opacity
      sidebarHidden: store('sidebarHidden', false),
      keys: store('keys', {}), // key binding overrides
      startupSound: store('startupSound', true),
      quickReplies: store('quickReplies', null), // null: QUICK_REPLIES
      order: store('order', {}), // dragged order of workspaces, tabs and machines: { ws, tabs, hosts }
    };
    // `gecko open` asks for a window that fits a terminal of this size,
    // centered; done once, then dropped from the URL.
    const url = new URL(location.href);
    const fit = /^(\d+)x(\d+)$/.exec(url.searchParams.get('fit') || '');
    if (fit) {
      this.fitRequest = { cols: +fit[1], rows: +fit[2] };
      url.searchParams.delete('fit');
      history.replaceState(null, '', url);
    }
    this.ws = store('workspace', 'default');
    this.activeByWs = store('activeByWs', {});
    this.collapsed = store('collapsed', {});
    this.tmuxOpen = store('tmuxOpen', {}); // tmux sessions expanded in the sidebar
    this.mods = { ctrl: false, alt: false };
    this.agentSeen = new Map();
    this.pendingFocus = null;
    this.applyTheme();

    this.conn = new Conn({
      onOpen: () => {
        splash.set(0.85);
        for (const p of this.panes.values()) if (p.attached) p.attach();
        const a = this.activePane();
        if (a) a.sendSize(true);
      },
      onStatus: () => this.renderSoon(),
      onMessage: (m) => this.onMessage(m),
      onData: (sub, off, data) => this.panes.get(sub)?.write(off, data),
    });
    this.keys = new Keys(this);
    this.bindUI();
    this.conn.connect();
    setInterval(() => this.renderSoon(), 5000); // relative times
  }

  // ---------------------------------------------------------------- state

  onMessage(m) {
    switch (m.t) {
      case 'hello':
        this.hostName = m.host;
        // A different start id means the service restarted; finish a
        // "Restart Gecko" by reloading this window into the new version.
        if (this.restarting && m.text && m.text !== this.startId) location.reload();
        this.startId = m.text;
        this.daemonVersion = m.version || 1;
        this.outdated = this.daemonVersion < PROTOCOL;
        // In the Android app this UI comes with the app, whatever Gecko the
        // machine runs: an older one there is fine (its tabs work, newer
        // features wait for an update), and restarting it wouldn't help.
        if (this.outdated && this.bundled) {
          this.outdated = false;
          if (!this.olderNoted) this.toast(`Gecko on ${m.host} is an older version: tabs work, and updating it there brings the newest features.`);
          this.olderNoted = true;
        }
        if (this.outdated) this.toast(OUTDATED);
        break;
      case 'sessions': {
        const fresh = new Map((m.sessions || []).map((s) => [s.id, s]));
        for (const [id, s] of this.sessions) {
          if (m.host && s.host !== m.host) fresh.set(id, s);
        }
        for (const id of this.sessions.keys()) if (!fresh.has(id)) this.sessionGone(id);
        this.sessions = fresh;
        if (!this.firstSync && this.settings.startupSound) startupChime();
        this.firstSync = true;
        splash.done(); // tabs are in: ready
        this.ensureWorkspace();
        break;
      }
      case 'session': {
        const s = m.session;
        if (this.closing.has(s.id)) break;
        this.sessions.set(s.id, s);
        this.exited.delete(s.id);
        this.watchAgent(s);
        if (this.pendingFocus === s.id) {
          this.pendingFocus = null;
          this.focusSession(s.id);
        }
        break;
      }
      case 'closed':
        this.sessionGone(m.id);
        this.closing.delete(m.id);
        break;
      case 'state':
        this.hosts = m.hosts || [];
        this.tmux = m.tmux || [];
        this.templates = m.templates || [];
        break;
      case 'settings':
        this.onSettings(m.text);
        return;
      case 'hostlog':
        this.hostSetup?.log(m.sub, m.text);
        return;
      case 'reset':
        this.panes.get(m.sub)?.reset(m.offset, m.prefix);
        break;
      case 'exit': {
        const p = this.panes.get(m.sub);
        if (p && m.error && !this.sessions.has(m.sub)) this.sessionGone(m.sub);
        break;
      }
    }
    this.renderSoon();
  }

  // A session ended. Its tab closes, unless the process failed while you
  // were looking at it: then it stays so you can read why.
  sessionGone(id) {
    const info = this.sessions.get(id);
    const p = this.panes.get(id);
    const failed = info?.exited && (info.exitCode ?? 0) !== 0 && !this.closing.has(id);
    if (p && p.visible && failed) {
      this.sessions.delete(id);
      this.exited.set(id, info);
      p.markExited(info.exitCode);
      this.renderSoon();
      return;
    }
    this.selectNeighbor(id);
    this.sessions.delete(id);
    if (p) {
      p.dispose();
      this.panes.delete(id);
    }
    this.renderSoon();
  }

  // Before a tab disappears, make the tab next to it active.
  selectNeighbor(id) {
    const s = this.sessions.get(id) || this.exited.get(id);
    if (!s || this.activeByWs[s.workspace] !== id) return;
    const tabs = this.tabsOf(s.workspace);
    const i = tabs.findIndex((t) => t.id === id);
    const next = tabs[i + 1] || tabs[i - 1];
    if (next) this.activeByWs[s.workspace] = next.id;
    else delete this.activeByWs[s.workspace];
    save('activeByWs', this.activeByWs);
  }

  // Every tab in a workspace, including ones whose process just exited.
  tabsOf(ws) {
    const out = [];
    for (const s of this.sessions.values()) if (s.workspace === ws) out.push(s);
    for (const s of this.exited.values()) if (s.workspace === ws) out.push({ ...s, exited: true });
    return byOrder(out, this.settings.order?.tabs, (t) => t.id, (a, b) => (a.created || 0) - (b.created || 0));
  }

  workspaces() {
    const names = new Map();
    for (const s of this.sessions.values()) names.set(s.workspace, true);
    for (const s of this.exited.values()) names.set(s.workspace, true);
    names.set(this.ws, true);
    return byOrder([...names.keys()], this.settings.order?.ws, (w) => w, (a, b) => (a === 'default' ? -1 : b === 'default' ? 1 : a.localeCompare(b)));
  }

  // Machines in the order they were dragged into.
  sortedHosts() {
    return byOrder([...this.hosts], this.settings.order?.hosts, (h) => h.name, () => 0);
  }

  ensureWorkspace() {
    if (this.tabsOf(this.ws).length) return;
    const first = [...this.sessions.values()][0];
    if (first) this.ws = first.workspace;
  }

  activeId() {
    const tabs = this.tabsOf(this.ws);
    const want = this.activeByWs[this.ws];
    if (tabs.some((t) => t.id === want)) return want;
    return tabs.length ? tabs[tabs.length - 1].id : null;
  }

  activeSession() {
    const id = this.activeId();
    return id ? this.sessions.get(id) || this.exited.get(id) : null;
  }

  activePane() {
    const id = this.activeId();
    return id ? this.panes.get(id) : null;
  }

  // ---------------------------------------------------------------- agents

  allAgents() {
    const out = [];
    for (const s of this.sessions.values()) if (s.agent) out.push({ key: s.id, session: s, state: s.agent, host: s.host });
    const rank = { 'needs-input': 0, working: 1, idle: 2, running: 3 };
    return out.sort((a, b) => (rank[a.state.status] ?? 9) - (rank[b.state.status] ?? 9) || (b.state.since || 0) - (a.state.since || 0));
  }

  watchAgent(s) {
    const prev = this.agentSeen.get(s.id);
    const now = s.agent?.status;
    this.agentSeen.set(s.id, now);
    if (!prev || !now || prev === now) return;
    const finished = prev === 'working' && now === 'idle';
    if (now !== 'needs-input' && !finished) return;
    const visible = !document.hidden && this.activeId() === s.id;
    if (visible) return;
    const title = `${s.agent.name} ${finished ? 'finished' : 'needs you'} · ${tabLabel(s)}`;
    this.notify(title, s.agent.detail || '', s.id);
  }

  notify(title, body, id) {
    if (android && document.hidden) {
      // The Android app posts it; tapping it opens this tab.
      try { android.notify(title, body, id || ''); return; } catch {}
    }
    if ('Notification' in window && Notification.permission === 'granted' && document.hidden) {
      const n = new Notification(title, { body, tag: id || title, icon: '/icon.svg' });
      n.onclick = () => { window.focus(); if (id) this.focusSession(id); n.close(); };
    } else {
      this.toast(title, id ? () => this.focusSession(id) : null);
    }
  }

  // ---------------------------------------------------------------- panes

  ensurePane(id) {
    let p = this.panes.get(id);
    if (!p) {
      p = new Pane(this, id);
      this.panes.set(id, p);
      $('#panes').appendChild(p.el);
      this.setupTouch(p);
      if (this.sessions.has(id)) p.attach();
      this.trimPanes();
    }
    p.lastUsed = Date.now();
    return p;
  }

  // Keep memory bounded: far-away tabs drop their terminal and re-attach
  // (replaying the server-side buffer) when visited again.
  trimPanes() {
    if (this.panes.size <= MAX_LIVE_PANES) return;
    const victims = [...this.panes.values()].filter((p) => !p.visible).sort((a, b) => a.lastUsed - b.lastUsed);
    while (this.panes.size > MAX_LIVE_PANES && victims.length) {
      const v = victims.shift();
      v.dispose();
      this.panes.delete(v.id);
    }
  }

  showActive() {
    const id = this.activeId();
    for (const p of this.panes.values()) {
      const on = p.id === id;
      if (on !== p.visible) p.el.classList.toggle('active', on);
    }
    $('#empty').hidden = !!id;
    if (!id) {
      this.shownId = null;
      return;
    }
    const p = this.ensurePane(id);
    if (!p.visible) p.el.classList.add('active');
    if (this.shownId === id) return;
    this.shownId = id;
    requestAnimationFrame(() => {
      p.show();
      if (this.fitRequest) {
        const { cols, rows } = this.fitRequest;
        this.fitRequest = null;
        this.fitWindow(cols, rows);
      }
      if (!this.paletteOpen && !this.textViewOpen && !(this.touch && document.activeElement?.closest('#compose'))) p.focus();
    });
    const s = this.sessions.get(id);
    if (s?.attention) this.conn.send({ t: 'patch', id, patch: { seen: true } });
  }

  focusSession(id) {
    const s = this.sessions.get(id) || this.exited.get(id);
    if (!s) return;
    this.ws = s.workspace;
    this.activeByWs[this.ws] = id;
    save('workspace', this.ws);
    save('activeByWs', this.activeByWs);
    this.closeDrawer();
    this.render();
  }

  // All tabs in sidebar order. Tab numbers (and Alt/⌘+N) run across
  // workspaces, so every tab has one stable number.
  allTabs() {
    return this.workspaces().flatMap((ws) => this.tabsOf(ws));
  }

  tabNumbers() {
    return new Map(this.allTabs().map((t, i) => [t.id, i + 1]));
  }

  selectTabIndex(i) {
    const t = this.allTabs()[i];
    if (t) this.focusSession(t.id);
  }

  cycleTab(d) {
    const tabs = this.tabsOf(this.ws);
    if (!tabs.length) return;
    const i = tabs.findIndex((t) => t.id === this.activeId());
    this.focusSession(tabs[(i + d + tabs.length) % tabs.length].id);
  }

  cycleTmuxWindow(d) {
    const s = this.activeSession();
    const wins = this.tmux.find((t) => t.host === s?.host && t.name === s?.tmuxSession)?.wins;
    if (!wins?.length) return;
    const i = wins.findIndex((w) => w.active);
    const w = wins[(i + d + wins.length) % wins.length];
    this.tmuxDo(s.host, 'select-window', `${s.tmuxSession}:${w.index}`);
  }

  // Through every tab in sidebar order, crossing workspaces.
  cycleAllTabs(d) {
    const tabs = this.allTabs();
    if (!tabs.length) return;
    const i = tabs.findIndex((t) => t.id === this.activeId());
    this.focusSession(tabs[(i + d + tabs.length) % tabs.length].id);
  }

  switchWorkspace(ws) {
    this.ws = ws;
    save('workspace', ws);
    this.closeDrawer();
    this.render();
  }

  cycleWorkspace(d) {
    const all = this.workspaces();
    const i = all.indexOf(this.ws);
    this.switchWorkspace(all[(i + d + all.length) % all.length]);
  }

  sendInput(id, data) {
    if (this.exited.has(id)) {
      this.closeTab(id, true);
      return;
    }
    if (this.mods.ctrl || this.mods.alt) {
      if (this.mods.ctrl && data.length === 1) {
        const c = data.toUpperCase().charCodeAt(0);
        if (c >= 64 && c <= 95) data = String.fromCharCode(c - 64);
        else if (data === ' ') data = '\x00';
      }
      if (this.mods.alt) data = '\x1b' + data;
      if (!this.mods.locked) this.mods = { ctrl: false, alt: false };
      this.renderKeybar();
    }
    this.conn.input(id, data);
  }

  // ---------------------------------------------------------------- actions

  // The machine a new tab opens on: the current tab's, or this one.
  defaultHost() {
    const cur = this.activeSession();
    return cur && !cur.exited ? cur.host : this.hostName;
  }

  async newTab(opts = {}) {
    const cur = this.activeSession();
    const p = this.activePane();
    const req = {
      host: opts.host ?? this.defaultHost(),
      workspace: opts.workspace ?? this.ws,
      cwd: opts.cwd ?? (cur && (opts.host ?? cur.host) === cur.host ? cur.cwd : ''),
      name: opts.name || '',
      command: opts.command || '',
      argv: opts.argv,
      cols: p?.term.cols || 80,
      rows: p?.term.rows || 24,
    };
    try {
      const r = await this.conn.request({ t: 'create', create: req });
      this.sessions.set(r.session.id, r.session);
      this.focusSession(r.session.id);
    } catch (e) {
      this.toast(`Couldn't open a tab on ${req.host}: ${e.message}`);
    }
  }

  async closeTab(id = this.activeId(), force = false) {
    if (!id) return;
    if (this.exited.has(id)) {
      this.selectNeighbor(id);
      this.exited.delete(id);
      this.panes.get(id)?.dispose();
      this.panes.delete(id);
      this.render();
      return;
    }
    const s = this.sessions.get(id);
    if (!force && s && (s.agent || (s.kind && s.kind !== 'shell') || s.running)) {
      const what = s.agent ? `${s.agent.name} (agent)` : s.command || s.proc || s.kind;
      const ok = await this.confirm(`Close \`${tabLabel(s)}\`? This ends \`${what}\`.`, 'Close tab');
      if (!ok) return;
    }
    // Close right away; don't wait for the process to finish exiting.
    this.closing.add(id);
    this.conn.send({ t: 'kill', id });
    this.sessionGone(id);
  }

  // The Gecko tab already showing tmux session `name` on `host`, if any.
  tmuxTab(host, name) {
    return [...this.sessions.values()].find((s) => s.host === host && s.tmuxSession === name) || null;
  }

  // Show tmux session `name` (optionally window `index`): go to the tab that
  // already shows it, or open one. Never a second tab for the same session.
  async openTmux(host, name, index = null) {
    const tab = this.tmuxTab(host, name);
    if (tab) {
      this.focusSession(tab.id);
      if (index != null) await this.tmuxDo(host, 'select-window', `${name}:${index}`);
      return;
    }
    await this.attachTmux(host, index == null ? name : `${name}:${index}`);
  }

  // Digital Watch theme: the window becomes a wristwatch. The terminal
  // area moves into the watch's wide, short LCD sized to the window; the 7-segment rows show
  // the time, the tabs and an alarm when an agent needs you; the pushers
  // work: LIGHT (backlight), MODE (next tab), START·STOP (search), ALARM.
  bindWatch() {
    const root = document.documentElement;
    const days = ['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'];
    const w = $('#watchface');
    const pad = (n) => String(n).padStart(2, '0');
    const tick = () => {
      if (!this.watchMode) return;
      const d = new Date();
      w.querySelector('.w-day').textContent = days[d.getDay()];
      w.querySelector('.w-date').textContent = String(d.getDate());
      w.querySelector('.w-time').textContent = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
      w.querySelector('.w-sec').textContent = pad(d.getSeconds());
    };
    setInterval(tick, 1000);
    this.watchTick = tick;
    let lightTimer = 0;
    w.addEventListener('click', (ev) => {
      const b = ev.target.closest('[data-watch]');
      if (!b) return;
      const act = b.dataset.watch;
      if (act === 'light') {
        const lit = (on) => {
          root.classList.toggle('lcd-light', on);
          const t = on ? { ...this.termTheme, background: '#8fe3cf' } : this.termTheme;
          for (const p of this.panes.values()) p.setTheme(t);
        };
        lit(true);
        clearTimeout(lightTimer);
        lightTimer = setTimeout(() => lit(false), 3000);
      }
      if (act === 'mode') this.cycleTab(1);
      if (act === 'search') this.openPalette();
      if (act === 'alarm') this.nextAgentNeedingYou();
    });
    window.addEventListener('resize', () => { if (this.watchMode) this.fitWatchSoon(); });
  }

  setWatchLayout(on) {
    if (!!this.watchMode === on) return;
    const root = document.documentElement;
    const stage = $('#stage');
    if (on) {
      this.stageHome = { parent: stage.parentNode, next: stage.nextSibling };
      $('#watchface .lcd-screen').appendChild(stage);
      $('#watchface').hidden = false;
      root.classList.add('layout-watch');
      this.watchMode = true;
      this.watchTick?.();
      this.renderWatch();
      this.fitWatchSoon();
    } else {
      this.watchMode = false;
      root.classList.remove('layout-watch');
      $('#watchface').hidden = true;
      const h = this.stageHome;
      if (h) h.parent.insertBefore(stage, h.next);
      $('#watchface .lcd-screen').style.cssText = '';
      requestAnimationFrame(() => this.activePane()?.layout(true));
    }
  }

  fitWatchSoon() {
    cancelAnimationFrame(this.watchFitFrame);
    this.watchFitFrame = requestAnimationFrame(() => this.fitWatch());
  }

  // Size the LCD to the window: as many whole cells as fit at the person's
  // font size, so resizing the window resizes the terminal and the text
  // always fills the LCD edge to edge.
  async fitWatch() {
    if (!this.watchMode) return;
    const screen = $('#watchface .lcd-screen');
    const caseEl = $('#watchface .case');
    const pane = this.activePane();
    const c = pane?.term._core?._renderService?.dimensions?.css?.cell;
    if (!c?.width) {
      // No tab drawn yet (e.g. starting in this theme): try again shortly.
      clearTimeout(this.watchRetry);
      this.watchRetry = setTimeout(() => this.fitWatchSoon(), 100);
      return;
    }
    const chromeW = caseEl.offsetWidth - screen.offsetWidth;
    const chromeH = caseEl.offsetHeight - screen.offsetHeight;
    const cs = getComputedStyle(pane.term.element);
    const padX = parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight);
    const padY = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom);
    const availW = window.innerWidth - 2 * 24 - chromeW; // room for the pushers
    const availH = window.innerHeight - 2 * 28 - chromeH; // a bit of strap shows
    const cols = Math.max(WATCH_MIN_COLS, Math.floor((availW - padX) / c.width));
    const rows = Math.max(WATCH_MIN_ROWS, Math.floor((availH - padY) / c.height));
    // The pane reaches 14px past the LCD's right edge (hidden), where xterm
    // keeps its scrollbar gutter, so the text itself fills the LCD.
    screen.style.width = Math.ceil(cols * c.width + padX + 0.5) + 'px';
    screen.style.height = Math.ceil(rows * c.height + padY + 0.5) + 'px';
    requestAnimationFrame(() => { for (const p of this.panes.values()) if (p.visible) { p.layout(true); p.sendSize(true); } });
  }

  // The LCD's segment rows: the tabs of this workspace (current one lit),
  // the workspace, the machine, and ALM when an agent needs you.
  renderWatch() {
    if (!this.watchMode) return;
    if (!$('#watchface .lcd-screen').style.width) this.fitWatchSoon(); // first tab just arrived
    const w = $('#watchface');
    const tabs = this.tabsOf(this.ws);
    const nums = this.tabNumbers();
    const act = this.activeId();
    w.querySelector('.w-tabs').innerHTML = tabs.slice(0, 9).map((t) => `<b class="${t.id === act ? 'on' : ''} ${t.agent?.status === 'needs-input' ? 'need' : ''}">${nums.get(t.id)}</b>`).join('');
    const s = this.activeSession();
    w.querySelector('.w-ws').textContent = (this.ws || '').toUpperCase().slice(0, 10);
    w.querySelector('.w-host').textContent = (s?.host || this.hostName || '').toUpperCase().slice(0, 10);
    w.querySelector('.w-alm').classList.toggle('on', this.allAgents().some((a) => a.state.status === 'needs-input'));
  }

  // Single click acts after a short wait; a second click on the same thing
  // within it is a double click instead (so renaming never opens a tab).
  clickOrDouble(key, single, double) {
    if (this.pendingClick?.key === key) {
      clearTimeout(this.pendingClick.timer);
      this.pendingClick = null;
      return double();
    }
    clearTimeout(this.pendingClick?.timer);
    this.pendingClick = { key, timer: setTimeout(() => { this.pendingClick = null; single(); }, 250) };
  }

  async renameTmuxSession(host, name) {
    const n = (await this.prompt('Rename tmux session', name, ''))?.trim();
    if (!n || n === name) return;
    await this.tmuxDo(host, 'rename-session', name, n);
    const k = host + '/' + name;
    if (k in this.tmuxOpen) {
      this.tmuxOpen[host + '/' + n] = this.tmuxOpen[k];
      delete this.tmuxOpen[k];
      save('tmuxOpen', this.tmuxOpen);
    }
  }

  async renameTmuxWindow(host, session, index) {
    const w = this.tmux.find((t) => t.host === host && t.name === session)?.wins?.find((x) => x.index === index);
    const n = (await this.prompt('Rename tmux window', w?.name || '', `${session}:${index}`))?.trim();
    if (n && n !== w?.name) await this.tmuxDo(host, 'rename-window', `${session}:${index}`, n);
  }

  // Run a tmux action on the tmux server of `host`.
  async tmuxDo(host, action, target, arg = '', id = '') {
    try {
      await this.conn.request({ t: 'tmux', host, name: action, target, data: arg, id });
    } catch (e) {
      if (/too old|bad request|doesn.t know/.test(e.message)) this.needsNewerGecko(host, 'control tmux');
      else this.toast(e.message);
    }
  }

  async attachTmux(host, target) {
    await this.newTab({ host, cwd: '', argv: ['tmux', 'attach', '-t', target] }); // titled by the tmux window
  }

  openAgent(a) {
    this.focusSession(a.session.id);
  }

  nextAgentNeedingYou() {
    const list = this.allAgents().filter((a) => a.state.status === 'needs-input');
    const target = list.find((a) => a.session.id !== this.activeId()) || list[0];
    if (target) this.focusSession(target.session.id);
    else this.toast('No agent is waiting for you.');
  }

  copySelection(p = this.activePane()) {
    if (!p || !p.term.hasSelection()) return false;
    copyText(p.term.getSelection()).then((ok) => this.toast(ok ? 'Copied' : 'Copy failed: the browser blocked clipboard access'));
    p.term.clearSelection();
    return true;
  }

  async paste() {
    const p = this.activePane();
    if (!p) return;
    const text = await readClipboard();
    if (text == null) {
      this.toast(isTouch ? 'Long-press the compose box to paste, then send.' : `Use ${isMac ? '⌘V' : 'Ctrl+Shift+V'} to paste.`);
      return;
    }
    p.term.paste(text);
    p.focus();
  }

  setFont(n) {
    this.setSetting(this.touch ? 'mobileFontSize' : 'fontSize', Math.max(8, Math.min(32, n)));
  }

  // Settings live in ~/.gecko-terminal/settings.json on the machine running
  // Gecko and sync to every window; localStorage only caches them so the
  // first paint already has the right colors.
  setSetting(k, v) {
    this.applySetting(k, v);
    this.pushSettings();
  }

  applySetting(k, v) {
    if (JSON.stringify(this.settings[k]) === JSON.stringify(v)) return;
    this.settings[k] = v;
    save(k, v);
    if (k === 'theme' || k === 'opacity') this.applyTheme();
    if (k === 'optionIsMeta') for (const p of this.panes.values()) p.term.options.macOptionIsMeta = v;
    if (k === 'fontSize' || k === 'mobileFontSize') {
      for (const p of this.panes.values()) p.setFontSize(this.termFontSize());
      if (this.watchMode) this.fitWatchSoon();
    }
    if (k === 'sidebarHidden') {
      document.body.classList.toggle('side-hidden', !!v);
      this.activePane()?.layout(true);
    }
    if (k === 'keys') this.keys?.rebuild(v);
  }

  pushSettings() {
    const out = { ...(this.serverSettings || {}) };
    for (const k of SYNCED) out[k] = this.settings[k];
    this.serverSettings = out;
    this.conn.send({ t: 'setsettings', text: JSON.stringify(out) });
  }

  // Settings arrived from the service (on connect, or changed elsewhere).
  onSettings(text) {
    let obj;
    try { obj = JSON.parse(text || '{}'); } catch { return; }
    if (!Object.keys(obj).length && !this.serverSettings) {
      // First run with settings.json: keep what this browser had.
      this.serverSettings = {};
      return this.pushSettings();
    }
    if ('scheme' in obj) {
      // settings.json from before themes were called themes
      if (!('theme' in obj)) obj.theme = obj.scheme;
      delete obj.scheme;
      this.serverSettings = obj;
      this.pushSettings();
    }
    this.serverSettings = obj;
    for (const k of SYNCED) if (k in obj) this.applySetting(k, obj[k]);
    this.renderSoon();
  }

  fontSize() {
    return this.touch ? this.settings.mobileFontSize : this.settings.fontSize;
  }

  // The terminal's font size: the person's size, scaled for themes whose
  // terminal font draws small (e.g. a pixel font).
  termFontSize() {
    return Math.round(this.fontSize() * (this.termScale || 1));
  }

  applyTheme(id = this.settings.theme, opacity = this.opacity()) {
    const theme = resolveTheme(id);
    this.termTheme = paintTheme(theme, opacity);
    const scale = theme.termSize || 1;
    if (scale !== (this.termScale || 1)) {
      this.termScale = scale;
      for (const p of this.panes?.values() || []) p.setFontSize(this.termFontSize());
    }
    this.setTermFont(theme.termFont || FONT);
    this.termContrast = theme.contrast || 1.1;
    this.noDim = !!theme.noDim;
    for (const p of this.panes?.values() || []) { p.setTheme(this.termTheme); p.term.options.minimumContrastRatio = this.termContrast; }
    if (this.panes) this.setWatchLayout(theme.layout === 'watch');
    if (this.watchMode) this.fitWatchSoon();
  }

  // Keyboard shortcuts editor: every action with its keys; add, remove,
  // reset. Saved to settings.json as overrides of the defaults.
  openShortcuts() {
    const m = $('#modal');
    const overrides = () => ({ ...(this.settings.keys || {}) });
    const save2 = (o) => {
      // Keep only real changes, so new defaults still reach people later.
      for (const a of Object.keys(DEFAULTS)) {
        if (o[a] && JSON.stringify(o[a]) === JSON.stringify(DEFAULTS[a])) delete o[a];
      }
      if (o.tabNumbers === DEFAULT_TAB_MODIFIER) delete o.tabNumbers;
      this.setSetting('keys', o);
      render();
    };
    let capture = null; // { action } while waiting for keys
    let conflict = null; // { action, combo, owner }
    let filter = '';
    const chip = (action, c) => `<span class="keychip"><kbd>${esc(prettyCombo(c))}</kbd><button data-rm="${esc(action)}" data-combo="${esc(c)}" aria-label="Remove ${esc(prettyCombo(c))}" title="Remove">×</button></span>`;
    const render = () => {
      const b = this.keys.bindings;
      const o = this.settings.keys || {};
      const rows = Object.entries(ACTIONS)
        .filter(([, label]) => !filter || label.toLowerCase().includes(filter))
        .map(([a, label]) => {
          const changed = !!o[a];
          const keysHtml = capture?.action === a
            ? '<span class="capture">Press the new keys… (Esc cancels)</span>'
            : (b[a].length ? b[a].map((c) => chip(a, c)).join('') : '<span class="none">No shortcut</span>');
          const conf = conflict?.action === a
            ? `<div class="conflict">${esc(prettyCombo(conflict.combo))} already does “${esc(ACTIONS[conflict.owner] || conflict.owner)}”. <button data-take>Use it here instead</button> <button data-cancel-conflict>Cancel</button></div>`
            : '';
          return `<li class="${changed ? 'changed' : ''}"><span class="act">${esc(label)}</span><span class="keys">${keysHtml}</span>
            <span class="tools"><button data-add="${esc(a)}" title="Add a shortcut" aria-label="Add a shortcut for ${esc(label)}">+</button>${changed ? `<button data-reset="${esc(a)}" title="Back to the default">reset</button>` : ''}</span>${conf}</li>`;
        }).join('');
      const tm = this.keys.tabModifier;
      const tabRow = !filter || 'go to tab'.includes(filter)
        ? `<li class="${tm !== DEFAULT_TAB_MODIFIER ? 'changed' : ''}"><span class="act">Go to tab 1-9</span><span class="keys">${TAB_MODIFIERS.map((mod) => `<button class="mod ${mod === tm ? 'on' : ''}" data-tabmod="${mod}">${esc(prettyCombo(mod + '+1'))}…9</button>`).join('')}</span><span class="tools"></span></li>`
        : '';
      m.querySelector('ul').innerHTML = tabRow + rows || '<li class="none">No action matches.</li>';
    };
    m.innerHTML = `<div class="card shortcuts" role="dialog" aria-modal="true" aria-labelledby="sc-title">
      <h2 id="sc-title">Keyboard shortcuts</h2>
      <input type="search" placeholder="Filter actions" aria-label="Filter actions">
      <ul></ul>
      <div class="row"><span class="where">Saved in ~/.gecko-terminal/settings.json</span><button data-reset-all>Reset all</button><button class="primary" data-done>Done</button></div>
    </div>`;
    m.hidden = false;
    render();
    // Keys are caught on the window while the editor is open: re-rendering
    // the list moves focus out of the dialog.
    const onKey = (ev) => keydown(ev);
    window.addEventListener('keydown', onKey, true);
    const close = () => {
      window.removeEventListener('keydown', onKey, true);
      this.capturingKeys = false;
      m.hidden = true;
      m.innerHTML = '';
      this.activePane()?.focus();
    };
    const search = m.querySelector('input');
    search.addEventListener('input', () => { filter = search.value.trim().toLowerCase(); render(); });
    search.focus();
    m.onclick = (ev) => {
      const t = ev.target.closest('button');
      if (!t) return;
      const o = overrides();
      const d = t.dataset;
      if ('done' in d) return close();
      if ('resetAll' in d) return save2({});
      if (d.add) { capture = { action: d.add }; conflict = null; this.capturingKeys = true; render(); return; }
      if (d.reset) { delete o[d.reset]; return save2(o); }
      if (d.rm) { o[d.rm] = this.keys.bindings[d.rm].filter((c) => c !== d.combo); return save2(o); }
      if (d.tabmod) { o.tabNumbers = d.tabmod; return save2(o); }
      if ('take' in d && conflict) {
        o[conflict.owner] = this.keys.bindings[conflict.owner].filter((c) => c !== conflict.combo);
        o[conflict.action] = [...this.keys.bindings[conflict.action], conflict.combo];
        conflict = null;
        return save2(o);
      }
      if ('cancelConflict' in d) { conflict = null; render(); }
    };
    const keydown = (ev) => {
      if (!capture) {
        if (ev.key === 'Escape') { ev.preventDefault(); ev.stopPropagation(); close(); }
        return;
      }
      ev.preventDefault();
      ev.stopPropagation();
      if (ev.key === 'Escape') { capture = null; this.capturingKeys = false; render(); return; }
      if (['Control', 'Shift', 'Alt', 'Meta', 'AltGraph'].includes(ev.key)) return; // wait for the real key
      const combo = comboOf(ev);
      const action = capture.action;
      capture = null;
      this.capturingKeys = false;
      const owner = Object.keys(this.keys.bindings).find((a) => a !== action && this.keys.bindings[a].includes(combo));
      if (owner) { conflict = { action, combo, owner }; render(); return; }
      const o = overrides();
      if (!this.keys.bindings[action].includes(combo)) o[action] = [...this.keys.bindings[action], combo];
      save2(o);
    };
  }

  // Some themes bring their own terminal font. Terminals measure glyphs, so
  // switch once the font has loaded, then refit.
  async setTermFont(f) {
    if (this.termFontWanted === f) return;
    this.termFontWanted = f;
    const first = f.split(',')[0].trim();
    try { await document.fonts.load(`${this.termFontSize()}px ${first}`); } catch {}
    if (this.termFontWanted !== f) return; // changed again meanwhile
    this.termFont = f; // new terminals use it from now on
    for (const p of this.panes?.values() || []) p.setFontFamily(f);
    if (this.watchMode) this.fitWatchSoon();
  }

  // Background opacity applies in the desktop app only.
  opacity() {
    return this.desktop?.transparent ? this.settings.opacity : 1;
  }

  // Opacity of the whole window over the desktop (desktop app only): the
  // background, picture themes' photo included. Text stays solid.
  openOpacityPicker() {
    const before = this.settings.opacity;
    const items = [1, 0.95, 0.9, 0.85, 0.8, 0.75, 0.7, 0.6, 0.5].map((o) => ({
      kind: Math.abs(o - before) < 0.001 ? 'Current' : 'Opacity',
      label: o === 1 ? 'Solid' : `${Math.round(o * 100)}%`,
      hint: o === 1 ? 'No transparency' : o >= 0.85 ? 'A hint of the desktop behind the window' : o >= 0.7 ? 'The desktop clearly shows through' : 'Very see-through',
      value: o,
      run: () => this.setSetting('opacity', o),
    }));
    this.openPalette('', {
      placeholder: 'Window opacity over the desktop',
      items,
      select: items.findIndex((i) => Math.abs(i.value - before) < 0.001),
      onSelect: (item) => item && this.applyTheme(this.settings.theme, item.value),
      onCancel: () => this.applyTheme(),
    });
  }

  // Pick a theme; the app previews each one as you move through
  // the list. Enter keeps it, Esc goes back.
  openThemePicker() {
    const before = this.settings.theme;
    const items = [{ id: AUTO, name: 'Gecko', hint: 'Dark or light, following your system', s: resolveTheme(AUTO) }]
      .concat(THEMES.map((s) => ({ id: s.id, name: s.name, hint: (s.dark ? 'Dark' : 'Light') + (s.image ? ', with a picture' : ''), s })))
      .map((x) => ({
        kind: x.id === before ? 'Current' : 'Theme',
        label: x.name,
        hint: x.hint,
        glyph: swatch(x.s),
        theme: x.id,
        run: () => this.setSetting('theme', x.id),
      }));
    this.openPalette('', {
      placeholder: 'Theme',
      items,
      select: items.findIndex((i) => i.theme === before),
      onSelect: (item) => item && this.applyTheme(item.theme),
      onCancel: () => this.applyTheme(before),
    });
  }

  action(name) {
    switch (name) {
      case 'palette': return this.openPalette();
      case 'toggleSidebar': return this.toggleSidebar();
      case 'fullscreen': return this.toggleFullscreen();
      case 'newTab': return this.newTab();
      case 'closeTab': return this.closeTab();
      case 'nextTab': return this.cycleTab(1);
      case 'prevTab': return this.cycleTab(-1);
      case 'nextWs': return this.cycleWorkspace(1);
      case 'prevWs': return this.cycleWorkspace(-1);
      case 'copy': return this.copySelection() ? undefined : false;
      case 'find': return this.openFind();
      case 'selectText': return this.openTextView();
      case 'nextAgent': return this.nextAgentNeedingYou();
      case 'fontUp': return this.setFont(this.fontSize() + 1);
      case 'fontDown': return this.setFont(this.fontSize() - 1);
      case 'fontReset': return this.setFont(this.touch ? 12 : 13);
      case 'rename': return this.renameTab();
      case 'help': return this.openShortcuts();
    }
    return false;
  }

  async renameTab(id = this.activeId()) {
    const s = this.sessions.get(id);
    if (!s) return;
    const name = await this.prompt('Rename tab', tabLabel(s), 'Leave empty to name it after what runs in it');
    if (name == null) return;
    this.conn.send({ t: 'patch', id, patch: { name } });
  }

  // Pick the workspace to move a tab to: the others, filtered as you type,
  // and "Create workspace …" for a name that isn't one yet.
  moveTab(id = this.activeId()) {
    const s = this.sessions.get(id);
    if (!s) return;
    const move = (ws) => {
      this.conn.send({ t: 'patch', id, patch: { workspace: ws } });
      this.activeByWs[ws] = id;
      this.switchWorkspace(ws);
    };
    const count = (n) => `${n} ${n === 1 ? 'tab' : 'tabs'}`;
    this.openPalette('', {
      placeholder: `Move “${tabLabel(s)}” to a workspace, or type a new name`,
      items: (q) => {
        let list = this.workspaces().filter((w) => w !== s.workspace)
          .map((w) => ({ kind: 'Workspace', label: w, hint: count(this.tabsOf(w).length), run: () => move(w) }));
        if (q) {
          list = list.map((i) => ({ i, sc: fuzzy(q, i.label) })).filter((x) => x.sc >= 0)
            .sort((a, b) => b.sc - a.sc).map((x) => x.i);
        }
        if (q && !this.workspaces().includes(q)) {
          list.push({ kind: 'New', label: `Create workspace “${q}”`, hint: 'and move the tab there', run: () => move(q) });
        } else if (q === s.workspace) {
          list.push({ kind: 'Workspace', label: q, hint: 'Already here', run: () => {} });
        }
        return list;
      },
    });
  }

  async newWorkspace() {
    const ws = await this.prompt('New workspace', '', 'A workspace groups tabs for one project, across machines');
    if (!ws) return;
    await this.newTab({ workspace: ws, cwd: '' });
  }

  // Browse folders on a tab's machine and make one its working folder.
  async openFolderPicker(id, dir = '') {
    const s = this.sessions.get(id);
    if (!s) return;
    let r;
    try {
      r = await this.conn.request({ t: 'listdir', id, dir });
    } catch (e) {
      if (/too old|bad request|doesn.t know/.test(e.message)) return this.needsNewerGecko(s.host, 'browse folders');
      return this.toast(e.message);
    }
    const here = r.dir, home = r.text || '';
    const pretty = (p) => (home && (p === home || p.startsWith(home + '/')) ? '~' + p.slice(home.length) : p);
    const join = (a, b) => (a.endsWith('/') ? a + b : a + '/' + b);
    const parent = here === '/' ? null : here.replace(/\/[^/]+\/?$/, '') || '/';
    const go = (p) => this.openFolderPicker(id, p);
    const use = (p) => this.useFolder(id, p);
    const top = [{ kind: 'Use', label: 'Use this folder', hint: pretty(here), run: () => use(here) }];
    if (parent) top.push({ kind: 'Up', label: '..', hint: pretty(parent), keepOpen: true, run: () => go(parent), commit: () => use(parent) });
    const folders = (r.entries || []).map((e) => {
      const p = join(here, e.name);
      return {
        kind: e.git ? 'Repo' : 'Folder', label: e.name + '/', hint: e.git ? 'git repository' : '',
        glyph: e.git ? BRANCH_ICON : '', keepOpen: true, run: () => go(p), commit: () => use(p),
      };
    });
    if (!folders.length) top.push({ kind: '', label: 'No folders here', hint: '', run: () => {} });
    this.openPalette('', {
      placeholder: `${pretty(here)} on ${s.host}: Enter opens a folder, Ctrl+Enter uses it, or type a path`,
      items: (q) => {
        if (/^[~/]/.test(q)) {
          // A typed path: open it, or use it right away with Ctrl+Enter.
          const resolveThen = async (f) => {
            try {
              const t = await this.conn.request({ t: 'listdir', id, dir: q });
              f(t.dir);
            } catch (e) {
              this.toast(e.message);
            }
          };
          return [{ kind: 'Go', label: `Go to ${q}`, hint: 'Enter opens it, Ctrl+Enter uses it', keepOpen: true, run: () => go(q), commit: () => resolveThen(use) }];
        }
        if (!q) return [...top, ...folders];
        return folders
          .map((i) => ({ i, s: fuzzy(q, i.label) }))
          .filter((x) => x.s >= 0)
          .sort((a, b) => b.s - a.s)
          .map((x) => x.i);
      },
    });
  }

  // A feature needs a newer Gecko on some machine: say which, and offer
  // the fix (update that machine, or restart this one).
  needsNewerGecko(host, what) {
    if (host && host !== this.hostName) {
      this.toast(`\`${host}\` runs an older Gecko that can't ${what}. Click to update it there (its tabs keep running).`, () => this.updateHost(host));
    } else {
      this.toast(`This Gecko is too old to ${what}. Click to restart it with the installed version.`, () => this.restartService());
    }
  }

  // Copy this Gecko to a machine and restart it there in place.
  async updateHost(name) {
    const m = $('#modal');
    m.innerHTML = `<div class="card setup" role="dialog" aria-modal="true" aria-labelledby="upd-title">
      <h2 id="upd-title">Update Gecko on ${esc(name)}</h2>
      <p class="sub">Copies this version of Gecko there and restarts it in place. Tabs on ${esc(name)} keep running.</p>
      <div class="setup-status" role="status">Working…</div>
      <pre class="setup-log"></pre>
      <div class="row"><button class="primary" data-close disabled>Close</button></div>
    </div>`;
    m.hidden = false;
    const job = 'update' + Date.now();
    const log = m.querySelector('.setup-log');
    const status = m.querySelector('.setup-status');
    const close = m.querySelector('[data-close]');
    const done = () => { this.hostSetup = null; m.hidden = true; m.innerHTML = ''; this.activePane()?.focus(); };
    close.onclick = done;
    m.onkeydown = (ev) => { if (ev.key === 'Escape' && !close.disabled) done(); };
    this.hostSetup = { log: (sub, text) => { if (sub === job) { log.textContent += text + '\n'; log.scrollTop = log.scrollHeight; } } };
    try {
      await this.conn.request({ t: 'updatehost', name, sub: job });
      status.textContent = `${name} is up to date.`;
      status.className = 'setup-status good';
    } catch (e) {
      status.textContent = /too old|bad request|doesn.t know/.test(e.message) ? 'This Gecko is too old to update other machines. Restart Gecko (Ctrl+K) first.' : e.message;
      status.className = 'setup-status bad';
    }
    close.disabled = false;
    close.focus();
  }

  // Make path the tab's working folder: `cd` at a shell prompt, otherwise
  // (something is running) a new tab in that folder.
  useFolder(id, path) {
    const s = this.sessions.get(id);
    if (!s) return;
    if (s.kind === 'shell' && !s.running) {
      const q = "'" + path.replace(/'/g, "'\\''") + "'";
      this.focusSession(id);
      this.conn.input(id, '\x05\x15cd ' + q + '\r'); // Ctrl+E Ctrl+U: clear anything half-typed first
    } else {
      this.newTab({ host: s.host, cwd: path, workspace: s.workspace });
      this.toast(`Something is running in \`${tabLabel(s)}\`, so \`${path}\` opened in a new tab.`);
    }
  }

  // The Gecko machine an ssh destination refers to, if one is set up.
  hostForTarget(target) {
    if (!target) return null;
    const norm = (t) => (t || '').replace(/^ssh:\/\//, '').replace(/\/$/, '').replace(/:22$/, '').toLowerCase();
    const want = norm(target);
    const bare = (t) => norm(t).replace(/^[^@]*@/, '');
    return this.hosts.find((h) => !h.local && h.target && (norm(h.target) === want || (bare(h.target) === bare(want) && !want.includes('@'))))
      || this.hosts.find((h) => !h.local && h.name === bare(want).split(/[.:]/)[0]) || null;
  }

  // Add (or fix) a machine: check the SSH connection with live output,
  // offer to install gecko there, and only then add it.
  addHost(prefill = {}) {
    const guessName = (t) => {
      const host = t.replace(/^ssh:\/\//, '').replace(/^.*@/, '').replace(/^\[|\]?(:\d+)?\/?$/g, '').toLowerCase();
      return /^[\d.]+$/.test(host) ? host : host.split('.')[0]; // an IP address whole
    };
    const m = $('#modal');
    m.innerHTML = `<div class="card setup" role="dialog" aria-modal="true" aria-labelledby="setup-title">
      <h2 id="setup-title">${prefill.name ? `Fix ${esc(prefill.name)}` : 'Add a machine'}</h2>
      <p class="sub">Gecko connects with your SSH keys and runs its own background service there, so sessions keep running when you disconnect.</p>
      <label>SSH destination<input name="target" placeholder="me@devbox or me@devbox:2222" autocomplete="off" autocapitalize="off" spellcheck="false"></label>
      <label>Name in Gecko<input name="name" placeholder="devbox" autocomplete="off" autocapitalize="off" spellcheck="false"></label>
      <details><summary>Advanced</summary>
        <label>Path to gecko on that machine<input name="gecko" placeholder="found on PATH or in ~/.local/bin" autocomplete="off" spellcheck="false"></label>
      </details>
      <div class="setup-status" role="status"></div>
      <div class="setup-auth" hidden>
        <label>Password<input name="secret" type="password" autocomplete="off" autocapitalize="off" spellcheck="false"></label>
        <label class="check" data-key-row><input name="usekey" type="checkbox" checked> <span data-key-label></span></label>
      </div>
      <pre class="setup-log" hidden></pre>
      <div class="row">
        <button data-cancel>Cancel</button>
        <button data-recheck hidden>Check again</button>
        <button class="primary" data-main>Check connection</button>
      </div>
    </div>`;
    m.hidden = false;
    const $m = (sel) => m.querySelector(sel);
    const f = { target: $m('[name=target]'), name: $m('[name=name]'), gecko: $m('[name=gecko]'), secret: $m('[name=secret]'), usekey: $m('[name=usekey]') };
    const auth = $m('.setup-auth');
    $m('[data-key-label]').textContent = android ? "Use this phone's SSH key from now on" : 'Use my SSH key from now on (adds it to ~/.ssh/authorized_keys there)';
    // The password lives only in this dialog and the request it goes with.
    let secret = '';
    let fingerprint = '';
    f.target.value = prefill.target || '';
    f.name.value = prefill.name || (prefill.target ? guessName(prefill.target) : '');
    f.gecko.value = prefill.gecko || '';
    let nameEdited = !!prefill.name;
    let state = 'idle'; // idle | busy | ok | missing | failed | password | hostkey
    let found = null;
    const job = 'setup' + Date.now();
    const log = $m('.setup-log');
    const status = $m('.setup-status');
    const main = $m('[data-main]');
    const recheck = $m('[data-recheck]');

    const setState = (st, msg = '', kind = '') => {
      state = st;
      status.innerHTML = md(msg);
      status.className = 'setup-status ' + kind;
      const busy = st === 'busy';
      for (const el of [main, recheck, f.target, f.name, f.gecko, f.secret, f.usekey]) el.disabled = busy;
      main.textContent = { idle: 'Check connection', busy: 'Working…', ok: 'Add machine', missing: 'Install Gecko there', failed: 'Retry', password: 'Log in', hostkey: 'Trust and connect' }[st];
      if (st !== 'busy') auth.hidden = st !== 'password';
      recheck.hidden = !(st === 'ok' || st === 'missing');
    };
    const close = () => {
      secret = '';
      this.hostSetup = null;
      m.hidden = true;
      m.innerHTML = '';
      this.activePane()?.focus();
    };
    this.hostSetup = {
      log: (sub, text) => {
        if (sub !== job) return;
        log.hidden = false;
        log.textContent += text + '\n';
        log.scrollTop = log.scrollHeight;
      },
    };
    const check = async (keepLog = false) => {
      const target = f.target.value.trim();
      if (!target) return setState('failed', 'Enter the SSH destination, for example me@devbox.', 'bad');
      if (keepLog !== true) log.textContent = '';
      setState('busy', 'Connecting…');
      const installKey = !!secret && f.usekey.checked && !$m('[data-key-row]').hidden;
      try {
        const r = await this.conn.request({ t: 'probehost', sub: job, target, data: f.gecko.value.trim(), secret: secret || undefined, installKey: installKey || undefined });
        if (!this.hostSetup) return;
        found = r;
        if (installKey) secret = ''; // the key logs in from now on
        if (secret && !installKey) {
          // Gecko's own connection there logs in with a key, never a password.
          return setState('failed', r.canUseKey
            ? 'The password works, but Gecko stays connected with a key: tick the box to use yours, then log in again.'
            : 'The password works, but Gecko stays connected with a key and this computer has none. Make one with `ssh-keygen`, then check again.', 'warn');
        }
        if (r.missing) setState('missing', `Reachable (${r.os}), but Gecko isn't installed there. Gecko can copy itself over to ~/.local/bin.`, 'warn');
        else setState('ok', `Connected: ${r.os}, ${r.text}.`, 'good');
      } catch (e) {
        if (!this.hostSetup) return;
        const r = e.reply || {};
        if (r.kind === 'needs-password') {
          const again = !!secret;
          secret = '';
          f.secret.value = '';
          $m('[data-key-row]').hidden = !r.canUseKey;
          setState('password', again ? 'That password didn\'t work. Try again.' : `${target.replace(/:\d+$/, '')} wants a password. Gecko uses it for this setup only and doesn't keep it.`, again ? 'bad' : '');
          f.secret.focus();
        } else if (r.kind === 'host-key-unknown') {
          fingerprint = r.data;
          setState('hostkey', `First connection to this machine. Its key fingerprint is \`${r.data}\`. If you aren't sure it's the right machine, compare it with what \`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub\` shows there.`, 'warn');
        } else setState('failed', e.message, 'bad');
      }
    };
    const login = () => {
      if (!f.secret.value) return f.secret.focus();
      secret = f.secret.value;
      f.secret.value = '';
      check(true);
    };
    const trust = async () => {
      setState('busy', 'Trusting the machine…');
      try {
        await this.conn.request({ t: 'trusthost', target: f.target.value.trim(), data: fingerprint });
        if (this.hostSetup) check(true);
      } catch (e) {
        if (this.hostSetup) setState('failed', e.message, 'bad');
      }
    };
    const install = async () => {
      setState('busy', 'Installing Gecko…');
      try {
        await this.conn.request({ t: 'installhost', sub: job, target: f.target.value.trim(), os: found.os, secret: secret || undefined });
        if (this.hostSetup) await check(true);
      } catch (e) {
        if (this.hostSetup) setState('missing', e.message, 'bad');
      }
    };
    const add = async () => {
      const name = f.name.value.trim();
      if (!name || /[\s/]/.test(name)) return setState('ok', 'Give the machine a name without spaces or slashes.', 'bad');
      setState('busy', 'Adding…');
      try {
        await this.conn.request({ t: 'addhost', name, target: f.target.value.trim(), data: f.gecko.value.trim() });
        close();
        this.toast(`Added \`${name}\`. Connecting…`);
      } catch (e) {
        setState('ok', e.message, 'bad');
      }
    };
    const primary = () => {
      if (state === 'busy') return;
      if (state === 'ok') return add();
      if (state === 'missing') return install();
      if (state === 'password') return login();
      if (state === 'hostkey') return trust();
      return check();
    };
    main.onclick = primary;
    recheck.onclick = () => check();
    $m('[data-cancel]').onclick = close;
    f.target.addEventListener('input', () => {
      if (!nameEdited) f.name.value = guessName(f.target.value);
      secret = '';
      if (state !== 'busy') setState('idle');
    });
    f.gecko.addEventListener('input', () => state !== 'busy' && setState('idle'));
    f.name.addEventListener('input', () => { nameEdited = true; });
    m.onkeydown = (ev) => {
      if (ev.key === 'Escape') { ev.preventDefault(); close(); }
      if (ev.key === 'Enter' && ev.target.tagName === 'INPUT') { ev.preventDefault(); primary(); }
    };
    setState('idle');
    if (prefill.error) setState('failed', prefill.error, 'bad');
    (f.target.value ? main : f.target).focus();
    if (prefill.check) check();
  }

  async removeHost(name) {
    if (!(await this.confirm(`Remove \`${name}\` from Gecko? Sessions running there keep running, and you can add it back any time.`, 'Remove'))) return;
    try { await this.conn.request({ t: 'rmhost', name }); } catch (e) { this.toast(e.message); }
  }

  async openTemplate(name) {
    const p = this.activePane();
    try {
      const r = await this.conn.request({ t: 'openws', name, cols: p?.term.cols || 80, rows: p?.term.rows || 24 });
      if (r.sessions?.length) {
        for (const s of r.sessions) this.sessions.set(s.id, s);
        this.focusSession(r.sessions[0].id);
      }
    } catch (e) {
      this.toast(e.message);
    }
  }

  async enableNotifications() {
    if (android) return android.enableNotifications();
    if (!('Notification' in window)) return this.toast('This browser has no notifications.');
    const r = await Notification.requestPermission();
    this.toast(r === 'granted' ? 'Gecko will notify you when an agent needs you.' : 'Notifications are blocked for this site.');
  }

  // ---------------------------------------------------------------- UI wiring

  bindUI() {
    $('#new-tab').onclick = () => this.newTab();
    $('#menu-btn').onclick = () => this.toggleSidebar();
    $('#menu-btn').title = `Show or hide workspaces (${prettyCombo(this.keys.first('toggleSidebar'))})`;
    document.body.classList.toggle('side-hidden', !!this.settings.sidebarHidden);
    $('#scrim').onclick = () => this.closeDrawer();
    if (this.touch) attachPage(this);
    $('#palette-btn').onclick = () => this.openPalette();
    $('#conn').onclick = () => { if (this.outdated) this.restartService(); };
    $('#palette-btn kbd').textContent = prettyCombo(this.keys.first('palette'));
    $('#tabs').addEventListener('click', (ev) => {
      const close = ev.target.closest('[data-close]');
      if (close) return this.closeTab(close.dataset.close);
      const t = ev.target.closest('[data-tab]');
      if (!t) return;
      if (this.isDouble('tab:' + t.dataset.tab)) return this.renameTab(t.dataset.tab);
      this.focusSession(t.dataset.tab);
    });
    onWheelStep($('#tabbar'), '#tabs', (d) => this.cycleTab(d));
    onWheelStep($('#tabbar'), '#ws-title', (d) => this.cycleWorkspace(d));
    // Only when the sidebar fits; otherwise the wheel scrolls it as usual.
    const sideFits = () => { const b = $('#side-body'); return b.scrollHeight <= b.clientHeight; };
    onWheelStep($('#side'), '.workspaces', (d) => this.cycleAllTabs(d), sideFits);
    onWheelStep($('#statusline'), '.tmux-wins-bar', (d) => this.cycleTmuxWindow(d));
    onWheelStep($('#watchface'), '.w-tabs', (d) => this.cycleTab(d));
    $('#tabs').addEventListener('auxclick', (ev) => {
      const t = ev.target.closest('[data-tab]');
      if (t && ev.button === 1) this.closeTab(t.dataset.tab);
    });
    $('#side').addEventListener('click', (ev) => {
      const el = ev.target.closest('[data-act]');
      if (!el) return;
      const d = el.dataset;
      switch (d.act) {
        case 'ws':
          // Click folds/unfolds; double-click renames (and undoes the fold).
          if (this.isDouble('ws:' + d.ws)) {
            this.toggleFold(d.ws); // undo the first click's fold
            return this.renameWorkspace(d.ws);
          }
          return this.toggleFold(d.ws);
        case 'tab':
          if (this.isDouble('tab:' + d.id)) return this.renameTab(d.id);
          return this.focusSession(d.id);
        case 'tab-close':
          return this.closeTab(d.id);
        case 'ws-close':
          return this.closeWorkspace(d.ws);
        case 'template': return this.openTemplate(d.name);
        case 'host-new': return this.newTab({ host: d.host, cwd: '' });
        case 'adopt-ssh': return this.addHost({ target: d.target, check: true });
        case 'host-rm': return this.removeHost(d.host);
        case 'host-update': return this.updateHost(d.host);
        case 'host-fix': {
          const h = this.hosts.find((x) => x.name === d.host);
          return this.addHost({ name: d.host, target: h?.target, error: h?.error });
        }
        case 'tmux':
          return this.clickOrDouble(`tmux:${d.host}/${d.name}`, () => this.openTmux(d.host, d.name), () => this.renameTmuxSession(d.host, d.name));
        case 'tmux-win':
          return this.clickOrDouble(`tmuxw:${d.host}/${d.name}:${d.idx}`, () => this.openTmux(d.host, d.name, +d.idx), () => this.renameTmuxWindow(d.host, d.name, +d.idx));
        case 'tmux-fold': {
          const k = d.host + '/' + d.name;
          this.tmuxOpen[k] = !this.tmuxOpen[k];
          save('tmuxOpen', this.tmuxOpen);
          return this.renderSidebar();
        }
        case 'add-host': return this.addHost();
        case 'new-ws': return this.newWorkspace();
      }
    });
    $('#side').addEventListener('auxclick', (ev) => {
      if (ev.button !== 1) return;
      const row = ev.target.closest('.tab-row');
      if (row) return this.closeTab(row.dataset.id);
      const ws = ev.target.closest('.ws-row [data-ws]');
      if (ws) this.closeWorkspace(ws.dataset.ws);
    });
    $('#statusline').addEventListener('click', (ev) => {
      const el = ev.target.closest('[data-act]');
      if (!el) return;
      if (el.dataset.act === 'adopt-ssh') this.addHost({ target: el.dataset.target, check: true });
      if (el.dataset.act === 'pick-cwd') this.openFolderPicker(this.activeId());
      const cur = this.activeSession();
      if (el.dataset.act === 'tmux-sel' && cur?.tmuxSession) {
        const idx = +el.dataset.idx;
        this.clickOrDouble(`chip:${cur.host}/${cur.tmuxSession}:${idx}`,
          () => this.tmuxDo(cur.host, 'select-window', `${cur.tmuxSession}:${idx}`),
          () => this.renameTmuxWindow(cur.host, cur.tmuxSession, idx));
      }
      if (el.dataset.act === 'tmux-rename' && cur?.tmuxSession) {
        this.clickOrDouble(`sess:${cur.host}/${cur.tmuxSession}`, () => {}, () => this.renameTmuxSession(cur.host, cur.tmuxSession));
      }
      if (el.dataset.act === 'tmux-new' && cur?.tmuxSession) this.tmuxDo(cur.host, 'new-window', cur.tmuxSession);
      if (el.dataset.act === 'host-new') this.newTab({ host: el.dataset.host, cwd: '' });
    });
    $('#dock').addEventListener('click', (ev) => {
      const prompt = ev.target.closest('[data-prompt]');
      if (prompt) {
        // Typed, then Enter on its own, as if typed by hand; a long prompt
        // gets a moment to land first.
        const text = prompt.dataset.prompt;
        this.conn.input(prompt.dataset.id, text);
        setTimeout(() => this.conn.input(prompt.dataset.id, '\r'), Math.min(400, 50 + text.length));
        return;
      }
      const reply = ev.target.closest('[data-reply]');
      if (reply) {
        const id = reply.dataset.id;
        const keys = { enter: '\r', esc: '\x1b', up: '\x1b[A', down: '\x1b[B' };
        this.conn.input(id, keys[reply.dataset.reply] ?? reply.dataset.reply);
        return;
      }
      const chip = ev.target.closest('[data-agent]');
      if (chip) {
        const a = this.allAgents().find((x) => x.key === chip.dataset.agent);
        if (a) this.openAgent(a);
      }
    });
    $('#empty').addEventListener('click', (ev) => {
      const el = ev.target.closest('[data-act]');
      if (!el) return;
      if (el.dataset.act === 'host-new') this.newTab({ host: el.dataset.host, cwd: '' });
      if (el.dataset.act === 'template') this.openTemplate(el.dataset.name);
    });
    this.bindReorder();
    this.bindContextMenu();
    this.bindNumberReveal();
    this.keepTerminalFocus();
    this.bindWatch();
    this.bindKeybar();
    this.bindCompose();
    this.bindPalette();
    this.bindTextView();
    this.bindFind();
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (this.settings.theme === AUTO) this.applyTheme();
    });
    window.addEventListener('focus', () => {
      const p = this.activePane();
      if (p) p.sendSize(true);
    });
    document.addEventListener('visibilitychange', () => this.updateTitle());
  }

  // Tab numbers stay hidden until they're useful: while dragging (see
  // .reordering) and while the go-to-tab modifier (Alt, or ⌘ on macOS) is
  // held.
  bindNumberReveal() {
    const prop = { alt: 'altKey', ctrl: 'ctrlKey', cmd: 'metaKey' };
    const name = { alt: 'Alt', ctrl: 'Control', cmd: 'Meta' };
    const hide = () => document.body.classList.remove('show-nums');
    window.addEventListener('keydown', (ev) => {
      if (ev.key === name[this.keys.tabModifier]) document.body.classList.add('show-nums');
    }, true);
    window.addEventListener('keyup', (ev) => {
      if (!ev[prop[this.keys.tabModifier]]) hide();
    }, true);
    window.addEventListener('blur', hide);
  }

  // Right-click (or long-press) a tab, workspace, machine or tmux session
  // for what can be done with it.
  bindContextMenu() {
    const open = (ev) => {
      const items = this.menuFor(ev.target);
      if (!items) return;
      ev.preventDefault();
      this.showMenu(ev.clientX, ev.clientY, items);
    };
    $('#side').addEventListener('contextmenu', open);
    $('#tabs').addEventListener('contextmenu', open);
    // In a terminal, right-click is always Gecko's menu, whatever runs
    // there: it neither selects a word nor reaches an app that reads the
    // mouse (Claude, vim, htop), so it behaves the same everywhere.
    const paneAt = (t) => [...this.panes.values()].find((x) => x.el.contains(t));
    for (const type of ['mousedown', 'mouseup']) {
      $('#panes').addEventListener(type, (ev) => {
        if (ev.button === 2 && paneAt(ev.target)) ev.stopPropagation();
      }, true);
    }
    $('#panes').addEventListener('contextmenu', (ev) => {
      const p = paneAt(ev.target);
      if (!p) return;
      ev.preventDefault();
      this.showMenu(ev.clientX, ev.clientY, this.terminalMenu(p, ev.clientX, ev.clientY));
    });
  }

  // The web address under a point of a terminal, if any.
  linkAt(p, x, y) {
    const t = p.term, r = t.element.querySelector('.xterm-screen')?.getBoundingClientRect();
    if (!r) return null;
    const col = Math.floor((x - r.left) / (r.width / t.cols)), row = Math.floor((y - r.top) / (r.height / t.rows));
    const line = t.buffer.active.getLine(t.buffer.active.viewportY + row)?.translateToString(true) || '';
    for (const m of line.matchAll(/https?:\/\/[^\s"'<>()]+/g)) {
      if (col >= m.index && col < m.index + m[0].length) return m[0].replace(/[.,;:!?]+$/, '');
    }
    return null;
  }

  terminalMenu(p, x, y) {
    const key = (a) => prettyCombo(this.keys.first(a));
    const own = p.term.hasSelection() ? p.term.getSelection() : '';
    const mouseApp = p.term.modes.mouseTrackingMode !== 'none';
    // Apps that read the mouse (Claude) select by themselves and copy what
    // you select; that is the selection then.
    const appSel = !own && mouseApp && p.appCopy && p.appCopy.at > p.lastDown ? p.appCopy.text : '';
    const sel = own || appSel;
    const trimmed = sel.trim();
    const link = this.linkAt(p, x, y) || (/^https?:\/\/\S+$/.test(trimmed) ? trimmed : null);
    const short = sel && !sel.includes('\n') && sel.length <= 40;
    const copied = (ok) => this.toast(ok ? 'Copied' : 'Copy failed: the browser blocked clipboard access');
    return [
      own && { label: 'Copy', hint: key('copy'), run: () => this.copySelection(p) },
      appSel && { label: 'Copy', run: () => copyText(appSel).then(copied) },
      link && { label: 'Open link', run: () => window.open(link, '_blank', 'noopener') },
      link && { label: 'Copy link', run: () => copyText(link).then(copied) },
      { label: 'Paste', hint: isMac ? '⌘V' : 'Ctrl+Shift+V', run: () => this.paste() },
      '-',
      { label: short ? `Find “${sel}”` : 'Find…', hint: key('find'), run: () => this.openFind(short ? sel : '') },
      { label: 'Select all', run: () => p.term.selectAll() },
      { label: 'Select text…', hint: key('selectText'), run: () => this.openTextView() },
      // In an app that reads the mouse but doesn't select by itself (vim,
      // htop), how to select anyway.
      !sel && mouseApp && !p.appCopy && { note: `${isMac ? '⌥' : 'Shift'}-drag to select here` },
    ];
  }

  menuFor(target) {
    const sep = '-';
    const tab = target.closest('[data-drag="tab"]');
    if (tab) {
      const id = tab.dataset.key;
      const s = this.sessions.get(id);
      const shortcut = id === this.activeId() ? prettyCombo(this.keys.first('closeTab')) : '';
      if (!s) return [{ label: 'Close tab', run: () => this.closeTab(id), hint: shortcut, danger: true }];
      const others = this.tabsOf(s.workspace).filter((t) => t.id !== id);
      return [
        { label: 'New tab here', hint: 'Same machine and folder', run: () => this.newTab({ host: s.host, cwd: s.cwd, workspace: s.workspace }) },
        sep,
        { label: 'Rename…', run: () => this.renameTab(id) },
        { label: 'Move to workspace…', run: () => this.moveTab(id) },
        s.kind === 'shell' && { label: 'Change folder…', run: () => this.openFolderPicker(id) },
        sep,
        others.length && { label: 'Close other tabs', run: async () => { for (const t of others) await this.closeTab(t.id); } },
        { label: 'Close tab', hint: shortcut, danger: true, run: () => this.closeTab(id) },
      ];
    }
    const ws = target.closest('.ws-row')?.dataset.key;
    if (ws) {
      const folded = this.collapsed[ws];
      return [
        { label: 'New tab in workspace', run: () => this.newTab({ workspace: ws, cwd: '' }) },
        sep,
        { label: 'Rename…', run: () => this.renameWorkspace(ws) },
        { label: folded ? 'Expand' : 'Collapse', run: () => this.toggleFold(ws) },
        sep,
        { label: 'Close workspace', danger: true, run: () => this.closeWorkspace(ws) },
      ];
    }
    const tmuxWin = target.closest('.tmux-win');
    if (tmuxWin) {
      const { host, name, idx } = tmuxWin.dataset;
      const w = this.tmux.find((t) => t.host === host && t.name === name)?.wins?.find((x) => x.index === +idx);
      return [
        { label: 'Open', run: () => this.openTmux(host, name, +idx) },
        { label: 'Rename…', run: () => this.renameTmuxWindow(host, name, +idx) },
        sep,
        { label: 'Close window', danger: true, run: async () => {
          if (await this.confirm(`Close tmux window \`${idx}:${w?.name || ''}\` in \`${name}\`? This ends what runs in it.`, 'Close window')) this.tmuxDo(host, 'kill-window', `${name}:${idx}`);
        } },
      ];
    }
    const tmux = target.closest('.tmux-row');
    if (tmux) {
      const { host, name } = tmux.querySelector('.tmux').dataset;
      const tab = this.tmuxTab(host, name);
      return [
        { label: tab ? 'Go to its tab' : 'Open in a new tab', run: () => this.openTmux(host, name) },
        { label: 'New window', run: () => this.tmuxDo(host, 'new-window', name) },
        { label: 'Rename…', run: () => this.renameTmuxSession(host, name) },
      ];
    }
    const hostName = target.closest('.host')?.dataset.host;
    if (hostName) {
      const h = this.hosts.find((x) => x.name === hostName);
      if (!h) return null;
      const outdated = !h.local && h.status === 'connected' && (h.version || 0) < PROTOCOL;
      return [
        { label: `New tab on ${h.name}`, disabled: h.status !== 'connected', run: () => this.newTab({ host: h.name, cwd: '' }) },
        !h.local && h.status === 'error' && { label: 'Fix connection…', run: () => this.addHost({ name: h.name, target: h.target, error: h.error }) },
        !h.local && h.status === 'connected' && { label: outdated ? 'Update Gecko' : 'Reinstall Gecko', hint: 'Its tabs keep running', run: () => this.updateHost(h.name) },
        !h.local && sep,
        !h.local && { label: 'Remove machine', hint: 'Its sessions keep running', danger: true, run: () => this.removeHost(h.name) },
      ];
    }
    const tpl = target.closest('.ws.template [data-name]');
    if (tpl) return [{ label: `Open ${tpl.dataset.name}`, run: () => this.openTemplate(tpl.dataset.name) }];
    return null;
  }

  // A small menu at the pointer: arrow keys and Enter, or click; Escape,
  // a click elsewhere or scrolling closes it.
  showMenu(x, y, items) {
    this.closeMenu?.();
    // Drop empty entries and separators that would start, end or double up.
    const list = items.filter(Boolean).filter((it, i, a) => it !== '-' || (i > 0 && a[i - 1] !== '-' && i < a.length - 1 && !a[i + 1].note));
    const el = document.createElement('div');
    el.id = 'ctxmenu';
    el.setAttribute('role', 'menu');
    el.innerHTML = list.map((it, i) => it === '-' ? '<hr>'
      : it.note ? `<p class="note">${esc(it.note)}</p>`
      : `<button role="menuitem" data-i="${i}" class="${it.danger ? 'danger' : ''}" ${it.disabled ? 'disabled' : ''}><span>${esc(it.label)}</span>${it.hint ? `<kbd>${esc(it.hint)}</kbd>` : ''}</button>`).join('');
    document.body.appendChild(el);
    const r = el.getBoundingClientRect();
    el.style.left = Math.max(4, Math.min(x, innerWidth - r.width - 4)) + 'px';
    el.style.top = Math.max(4, Math.min(y, innerHeight - r.height - 4)) + 'px';
    const buttons = [...el.querySelectorAll('button:not(:disabled)')];
    let on = -1;
    const mark = (i) => {
      on = (i + buttons.length) % buttons.length;
      buttons.forEach((b, j) => b.classList.toggle('on', j === on));
    };
    const close = () => {
      el.remove();
      this.closeMenu = null;
      // Unless what was chosen opened something to type in, the keyboard
      // goes back to the shell.
      requestAnimationFrame(() => {
        if (!document.activeElement?.closest?.('input, textarea, #palette, #modal, #textview, #find, #compose')) this.activePane()?.focus();
      });
      window.removeEventListener('pointerdown', outside, true);
      window.removeEventListener('keydown', key, true);
      window.removeEventListener('blur', close);
      window.removeEventListener('resize', close);
      window.removeEventListener('wheel', close, true);
    };
    const run = (b) => {
      close();
      list[+b.dataset.i].run();
    };
    const outside = (e) => { if (!el.contains(e.target)) close(); };
    const key = (e) => {
      const k = { ArrowDown: 1, ArrowUp: -1 }[e.key];
      if (k) mark(on < 0 && k < 0 ? -1 : on + k);
      else if (e.key === 'Enter' && on >= 0) run(buttons[on]);
      else if (e.key === 'Escape') close();
      else return;
      e.preventDefault();
      e.stopPropagation();
    };
    el.addEventListener('click', (e) => { const b = e.target.closest('button'); if (b && !b.disabled) run(b); });
    el.addEventListener('pointermove', (e) => { const i = buttons.indexOf(e.target.closest('button')); if (i >= 0) mark(i); });
    window.addEventListener('pointerdown', outside, true);
    window.addEventListener('keydown', key, true);
    window.addEventListener('blur', close);
    window.addEventListener('resize', close);
    window.addEventListener('wheel', close, true);
    this.closeMenu = close;
  }

  // Drag workspaces, tabs (in the sidebar or the tab bar) and machines to
  // reorder them; drop a tab on another workspace to move it there. Done
  // with pointer events: the sidebar keeps focus in the terminal by
  // cancelling mousedown, which rules out native drag and drop.
  bindReorder() {
    const onDown = (ev) => {
      if (ev.button !== 0 || ev.pointerType === 'touch') return;
      const src = ev.target.closest('[data-drag]');
      if (!src || ev.target.closest('.x, .fold[data-act], input')) return;
      const start = { x: ev.clientX, y: ev.clientY };
      const move = (e) => {
        if (!this.drag) {
          if (Math.hypot(e.clientX - start.x, e.clientY - start.y) < 6) return;
          // Numbers show first: in the tab bar they change the tabs' size.
          document.body.classList.add('reordering');
          this.drag = { kind: src.dataset.drag, key: src.dataset.key, src, ghost: this.dragGhost(src, start) };
          this.drag.ghost.item.classList.add('dragging');
        }
        const at = this.drag.ghost.move(e.clientX, e.clientY);
        this.autoScroll(at.x, e.clientY);
        this.dragOver(at.x, at.y);
      };
      const end = (e) => {
        window.removeEventListener('pointermove', move, true);
        window.removeEventListener('pointerup', end, true);
        window.removeEventListener('keydown', esc, true);
        const d = this.drag;
        if (!d) return;
        this.drag = null;
        clearTimeout(d.recheck);
        d.ghost.item.classList.remove('dragging');
        document.body.classList.remove('reordering');
        this.markDrop(null);
        // The click that ends a drag isn't a click on the row.
        const swallow = (c) => { c.stopPropagation(); c.preventDefault(); };
        window.addEventListener('click', swallow, { capture: true, once: true });
        setTimeout(() => window.removeEventListener('click', swallow, true), 0);
        if (e && d.at) this.dropAt(d, d.at);
        this.render();
        d.ghost.land(d.kind, d.key);
      };
      const esc = (e) => {
        if (e.key !== 'Escape' || !this.drag) return;
        e.preventDefault();
        e.stopPropagation();
        this.drag.at = null;
        end(null);
      };
      window.addEventListener('pointermove', move, true);
      window.addEventListener('pointerup', end, true);
      window.addEventListener('keydown', esc, true);
    };
    $('#side').addEventListener('pointerdown', onDown);
    $('#tabs').addEventListener('pointerdown', onDown);
  }

  // A copy of the dragged item that follows the pointer, kept in its lane:
  // along the tab bar, or up and down the sidebar. A workspace
  // brings its tabs along. It lives next to the original (inside the
  // sidebar or tab bar) so the theme's styles still apply to it. The
  // original stays in the list as an empty slot, so the item is never
  // seen twice; the copy lifts off when picked up and settles into its
  // place when let go.
  dragGhost(src, start) {
    const item = src.dataset.drag === 'ws' ? src.closest('.ws') : src.dataset.drag === 'host' ? src.closest('.host') : src;
    const r = item.getBoundingClientRect();
    const box = src.closest('#tabbar') || $('#side');
    const el = document.createElement(item.tagName === 'LI' ? 'ul' : 'div');
    el.className = 'drag-ghost' + (item.classList.contains('host') ? ' hostlist' : '');
    el.style.width = r.width + 'px';
    el.style.height = r.height + 'px';
    el.appendChild(item.cloneNode(true));
    box.appendChild(el);
    const off = { x: start.x - r.left, y: start.y - r.top };
    // Correct for an ancestor that shifts fixed positioning (the phone
    // drawer is transformed).
    el.style.left = '0px';
    el.style.top = '0px';
    const o = el.getBoundingClientRect();
    const ease = 'cubic-bezier(.2, .8, .3, 1)';
    const fast = matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 1;
    el.animate([{ opacity: 1, transform: 'none', boxShadow: 'none' }, {}], { duration: 140 * fast, easing: ease });
    return {
      el,
      item,
      // Glide onto the item's place in the freshly rendered list (or back
      // to where it was, when the drag was cancelled).
      land: (kind, key) => {
        const lane = box.id === 'tabbar' ? $('#tabs') : $('#side-body');
        const row = [...lane.querySelectorAll(`[data-drag="${kind}"]`)].find((e) => e.dataset.key === key);
        const to = row && (kind === 'ws' ? row.closest('.ws') : kind === 'host' ? row.closest('.host') : row);
        const left = parseFloat(el.style.left) + o.left, top = parseFloat(el.style.top) + o.top;
        const t = to?.getBoundingClientRect();
        if (to) to.style.visibility = 'hidden';
        const anim = el.animate(t
          ? [{}, { opacity: 1, transform: `translate(${t.left - left}px, ${t.top - top}px)`, boxShadow: 'none' }]
          : [{}, { opacity: 0 }], { duration: 160 * fast, easing: ease, fill: 'forwards' });
        anim.finished.then(() => {
          el.remove();
          if (to) to.style.visibility = '';
        });
      },
      // Returns the point the item is held at.
      move: (x, y) => {
        const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));
        if (box.id === 'tabbar') {
          const lane = $('#tabs').getBoundingClientRect();
          x = clamp(x, lane.left + off.x, lane.right - r.width + off.x);
          y = start.y;
        } else {
          // Its own section of the sidebar (workspaces or machines), as
          // much of it as is scrolled into view.
          const body = $('#side-body').getBoundingClientRect();
          const sec = (src.closest('.group') || $('#side-body')).getBoundingClientRect();
          const top = Math.max(body.top, sec.top), bottom = Math.min(body.bottom, sec.bottom);
          x = start.x;
          // The whole copy when it fits; a tall workspace keeps just the
          // point it's held by inside.
          y = r.height <= bottom - top ? clamp(y, top + off.y, bottom - r.height + off.y) : clamp(y, top, bottom);
        }
        el.style.left = x - off.x - o.left + 'px';
        el.style.top = y - off.y - o.top + 'px';
        return { x, y };
      },
    };
  }

  // Where the dragged item would land: { el, key, pos: before|after|into }.
  // Within a list the other items slide out of the way as soon as the
  // pointer crosses into one of them, so what you see is where it lands.
  // Where that can't be shown (a tab bar tab over the sidebar, a folded
  // workspace) a line or outline marks the spot instead.
  dragOver(x, y) {
    const d = this.drag;
    d.last = { x, y };
    // Rows still sliding are not where they look; wait for them.
    if (performance.now() < d.busyUntil) return;
    const hit = document.elementFromPoint(x, y);
    const unit = d.src.dataset.drag === 'tab' ? (d.src.classList.contains('tab') ? d.src : d.src.parentElement) : d.src.closest('.ws, .host');
    const half = (el) => {
      const r = el.getBoundingClientRect();
      return el.classList.contains('tab') ? (x < r.left + r.width / 2 ? 'before' : 'after') : (y < r.top + r.height / 2 ? 'before' : 'after');
    };
    let at = null, target = null;
    const row = d.kind === 'tab' ? hit?.closest('[data-drag="tab"]') : hit?.closest(d.kind === 'ws' ? '.ws[data-ws]' : '.host[data-host]');
    const wsRow = d.kind === 'tab' && !row && hit?.closest('.ws-row');
    if (row) {
      target = row.classList.contains('tab-row') ? row.parentElement : row;
      if (target === unit) return;
      const key = row.dataset.key || row.dataset.ws || row.dataset.host;
      const pos = target.parentElement === unit.parentElement
        ? (unit.compareDocumentPosition(target) & Node.DOCUMENT_POSITION_FOLLOWING ? 'after' : 'before')
        : half(target);
      at = { el: row, key, pos };
      if (unit.classList.contains('tab') !== target.classList.contains('tab')) target = null; // tab bar <-> sidebar
    } else if (wsRow) {
      // A workspace's name: first in that workspace.
      const ul = wsRow.parentElement.querySelector('ul');
      const first = ul && [...ul.children].find((li) => li !== unit);
      if (first && unit.tagName === 'LI') {
        if (first.previousElementSibling === unit) return;
        target = first;
        at = { el: first, key: first.firstElementChild.dataset.key, pos: 'before' };
      } else {
        at = { el: wsRow, key: wsRow.dataset.key, pos: 'into' };
        if (ul && unit.tagName === 'LI') { this.slide(() => ul.append(unit)); d.at = at; return this.markDrop(null); }
      }
    }
    if (target) {
      this.slide(() => target[at.pos](unit));
      this.markDrop(null);
      d.at = at;
    } else if (at) {
      this.markDrop(at);
      d.at = at;
    } else if (d.marked) {
      // Off the list after a marked spot: nothing to drop on.
      this.markDrop(null);
      d.at = null;
    }
    d.marked = !!at && !target && !(wsRow && unit.tagName === 'LI');
  }

  // Change the order of rows, then slide each from where it was (FLIP).
  slide(change) {
    const els = [...document.querySelectorAll('#side .ws-row, #side .ws li, #side .ws.template, #side .host, #side .group > h3, #tabs .tab')]
      .filter((e) => !e.closest('.drag-ghost'));
    const was = new Map(els.map((e) => [e, e.getBoundingClientRect()]));
    for (const e of els) for (const a of e.getAnimations()) a.cancel();
    change();
    this.mirror();
    const ms = matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 150;
    this.renumber(ms);
    if (!ms) return;
    for (const e of els) {
      const a = was.get(e), b = e.getBoundingClientRect();
      const dx = a.left - b.left, dy = a.top - b.top;
      if (dx || dy) e.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: 'none' }], { duration: ms, easing: 'cubic-bezier(.2, .8, .3, 1)' });
    }
    const d = this.drag;
    d.busyUntil = performance.now() + ms;
    clearTimeout(d.recheck);
    d.recheck = setTimeout(() => this.drag === d && this.dragOver(d.last.x, d.last.y), ms + 10);
  }

  // The current workspace's tabs are in both the tab bar and the sidebar;
  // whichever one is being dragged in, the other follows. A tab dragged
  // off to another workspace leaves the tab bar.
  mirror() {
    const ul = [...document.querySelectorAll('#side-body .ws[data-ws]')].find((b) => b.dataset.ws === this.ws)?.querySelector('ul');
    if (!ul) return;
    const bar = $('#tabs');
    const tabs = new Map([...bar.querySelectorAll('.tab')].map((e) => [e.dataset.key, e]));
    const rows = new Map([...ul.querySelectorAll('.tab-row')].map((e) => [e.dataset.key, e.parentElement]));
    if (this.drag.src.closest('#tabs')) {
      for (const k of tabs.keys()) if (rows.has(k)) ul.append(rows.get(k));
    } else {
      for (const [k, e] of tabs) e.style.display = rows.has(k) ? '' : 'none';
      for (const k of rows.keys()) if (tabs.has(k)) bar.append(tabs.get(k));
    }
  }

  // Tab numbers as the order now on screen would make them, so they change
  // while dragging, not after. Numbers run across workspaces; a folded
  // workspace counts its tabs without showing them.
  renumber(ms) {
    const ids = [];
    const inBar = !!this.drag.src.closest('#tabs');
    for (const block of document.querySelectorAll('#side-body .ws[data-ws]')) {
      const ws = block.dataset.ws;
      const rows = ws === this.ws && inBar ? $('#tabs').querySelectorAll('.tab') : block.querySelector('ul')?.querySelectorAll('.tab-row');
      ids.push(...(rows ? [...rows].map((r) => r.dataset.key) : this.tabsOf(ws).map((t) => t.id)));
    }
    const nums = new Map(ids.map((id, i) => [id, String(i + 1)]));
    for (const row of document.querySelectorAll('[data-drag="tab"]')) {
      const num = row.querySelector('.num');
      const n = nums.get(row.dataset.key);
      if (!num || !n || num.textContent === n) continue;
      num.textContent = n;
      if (ms) num.animate([{ transform: 'scale(0.5)', opacity: 0 }, { transform: 'none', opacity: 1 }], { duration: ms + 50, easing: 'cubic-bezier(.3, 1.4, .5, 1)' });
    }
  }

  // Near the sidebar's edges, scroll it.
  autoScroll(x, y) {
    const body = $('#side-body');
    const r = body.getBoundingClientRect();
    if (x < r.left || x > r.right) return;
    if (y < r.top + 24) body.scrollTop -= 12;
    else if (y > r.bottom - 24) body.scrollTop += 12;
  }

  markDrop(at) {
    for (const el of document.querySelectorAll('.drop-before, .drop-after, .drop-into')) el.classList.remove('drop-before', 'drop-after', 'drop-into');
    if (at) at.el.classList.add('drop-' + at.pos);
  }

  dropAt(d, at) {
    const place = (list, key, target, pos) => {
      const out = list.filter((k) => k !== key);
      const i = out.indexOf(target);
      out.splice(i < 0 ? out.length : pos === 'after' ? i + 1 : i, 0, key);
      return out;
    };
    const order = { ...this.settings.order };
    if (d.kind === 'ws') {
      order.ws = newOrder(place(this.workspaces(), d.key, at.key, at.pos), order.ws);
    } else if (d.kind === 'host') {
      order.hosts = newOrder(place(this.hosts.map((h) => h.name), d.key, at.key, at.pos), order.hosts);
    } else if (d.kind === 'tab') {
      const tab = this.sessions.get(d.key) || this.exited.get(d.key);
      if (!tab) return;
      let ws, ids;
      if (at.pos === 'into') {
        // Onto a workspace: the end of its tabs.
        ws = at.key;
        const last = this.tabsOf(ws).filter((t) => t.id !== d.key).pop();
        ids = last ? place(this.allTabs().map((t) => t.id), d.key, last.id, 'after') : this.allTabs().map((t) => t.id);
      } else {
        ws = (this.sessions.get(at.key) || this.exited.get(at.key))?.workspace;
        ids = place(this.allTabs().map((t) => t.id), d.key, at.key, at.pos);
      }
      if (!ws) return;
      order.tabs = newOrder(ids, order.tabs);
      if (ws !== tab.workspace) {
        if (this.sessions.has(d.key)) this.conn.send({ t: 'patch', id: d.key, patch: { workspace: ws } });
        else tab.workspace = ws;
        // Follow the tab when it was the one being looked at.
        if (this.activeId() === d.key) {
          this.selectNeighbor(d.key);
          this.activeByWs[ws] = d.key;
          save('activeByWs', this.activeByWs);
          this.ws = ws;
          save('workspace', ws);
        }
      }
    }
    this.setSetting('order', order);
  }

  // Double-click detection that survives the sidebar re-rendering between
  // the two clicks (which swallows native dblclick events).
  isDouble(key) {
    const now = Date.now();
    const dbl = this.lastClick && this.lastClick.key === key && now - this.lastClick.at < 400;
    this.lastClick = dbl ? null : { key, at: now };
    return dbl;
  }

  toggleFold(ws) {
    this.collapsed[ws] = !this.collapsed[ws];
    save('collapsed', this.collapsed);
    this.renderSidebar();
  }

  // Close a workspace: all its tabs, after one confirmation.
  async closeWorkspace(ws) {
    const tabs = this.tabsOf(ws);
    if (tabs.length) {
      const busy = tabs.filter((t) => !t.exited && (t.agent || (t.kind && t.kind !== 'shell') || t.running))
        .map((t) => '`' + (t.agent ? t.agent.name : t.command || t.proc || t.kind) + '`' + (t.agent ? ' (agent)' : ''));
      const what = busy.length ? ` This ends ${busy.join(', ')}.` : '';
      const n = tabs.length === 1 ? '1 tab' : `${tabs.length} tabs`;
      if (!(await this.confirm(`Close \`${ws}\` and its ${n}?${what}`, 'Close workspace'))) return;
    }
    for (const t of tabs) this.closeTab(t.id, true);
    delete this.activeByWs[ws];
    delete this.collapsed[ws];
    save('activeByWs', this.activeByWs);
    save('collapsed', this.collapsed);
    if (this.ws === ws) {
      const next = this.workspaces().find((w) => w !== ws && this.tabsOf(w).length);
      this.switchWorkspace(next || 'default');
    } else {
      this.render();
    }
  }

  async renameWorkspace(ws) {
    const name = (await this.prompt('Rename workspace', ws, 'Using the name of another workspace merges the two'))?.trim();
    if (!name || name === ws) return;
    for (const s of this.sessions.values()) {
      if (s.workspace === ws) this.conn.send({ t: 'patch', id: s.id, patch: { workspace: name } });
    }
    for (const s of this.exited.values()) if (s.workspace === ws) s.workspace = name;
    if (this.activeByWs[ws]) this.activeByWs[name] = this.activeByWs[ws];
    delete this.activeByWs[ws];
    if (this.collapsed[ws] !== undefined) this.collapsed[name] = this.collapsed[ws];
    delete this.collapsed[ws];
    save('activeByWs', this.activeByWs);
    save('collapsed', this.collapsed);
    if (this.ws === ws) this.switchWorkspace(name);
  }

  // The shell is what has the keyboard. Clicking chrome (sidebar, tabs,
  // dock, margins) must not take focus away from it; only text fields and
  // overlays (palette, prompts, find, compose, text view) get focus.
  keepTerminalFocus() {
    const ownsFocus = 'input, textarea, select, [contenteditable], #palette, #modal, #textview, #find, #compose, #ctxmenu';
    document.addEventListener('mousedown', (ev) => {
      if (this.touch || ev.button !== 0) return;
      const t = ev.target;
      if (t.closest?.('.xterm') || t.closest?.(ownsFocus)) return;
      // Leave scrollbar drags alone.
      if (t.clientWidth && ev.offsetX > t.clientWidth) return;
      ev.preventDefault(); // keeps focus where it is
      if (!document.activeElement?.closest?.('.xterm') && !this.paletteOpen && !this.textViewOpen) {
        requestAnimationFrame(() => this.activePane()?.focus());
      }
    }, true);
    // Coming back to the window lands in the shell too.
    window.addEventListener('focus', () => {
      if (!this.touch && !this.paletteOpen && !this.textViewOpen && !document.activeElement?.closest?.(ownsFocus)) {
        this.activePane()?.focus();
      }
    });
  }

  // Space the terminal gets and its cell size, computed exactly like
  // xterm's fit addon (parent size minus padding and a 14px scrollbar).
  terminalArea() {
    const p = this.activePane();
    const dims = p?.term._core?._renderService?.dimensions?.css?.cell;
    if (!p || !dims?.width) return this.estimatedArea();
    const parent = getComputedStyle(p.el);
    const own = getComputedStyle(p.term.element);
    const padX = parseInt(own.paddingLeft) + parseInt(own.paddingRight);
    const padY = parseInt(own.paddingTop) + parseInt(own.paddingBottom);
    return {
      cw: dims.width, ch: dims.height,
      w: parseInt(parent.width) - padX - 14,
      h: parseInt(parent.height) - padY,
    };
  }

  // No terminal open yet: measure with a hidden terminal laid out exactly
  // like a real tab, then throw it away.
  estimatedArea() {
    const el = document.createElement('div');
    el.className = 'pane';
    el.style.visibility = 'hidden';
    $('#panes').appendChild(el);
    const t = new Terminal({ fontFamily: this.termFont || FONT, fontSize: this.termFontSize(), lineHeight: 1.15, scrollback: 1 });
    try {
      t.open(el);
      // Same renderer as real tabs: WebGL snaps cells to whole pixels.
      if (this.settings.renderer === 'webgl' || (this.settings.renderer === 'auto' && !this.touch)) {
        try { t.loadAddon(new WebglAddon()); } catch {}
      }
      const cell = t._core?._renderService?.dimensions?.css?.cell;
      if (!cell?.width) return null;
      const parent = getComputedStyle(el);
      // The status line is hidden while there are no tabs and takes its
      // place once the first one opens.
      const statusline = $('#statusline').offsetHeight ? 0 : 24;
      return { cw: cell.width, ch: cell.height, w: parseInt(parent.width) - 14, h: parseInt(parent.height) - statusline };
    } finally {
      t.dispose();
      el.remove();
    }
  }

  // Resize this window so the terminal is exactly cols x rows, then center
  // it. Works in app windows (gecko open, installed app); browsers ignore
  // it for normal tabs, which is fine.
  async fitWindow(cols, rows) {
    const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
    for (let i = 0; i < 6; i++) {
      const a = this.terminalArea();
      if (!a) break;
      // Aim for the middle of a cell so rounding lands on exactly cols x rows.
      const dw = Math.round((cols + 0.5) * a.cw - a.w);
      const dh = Math.round((rows + 0.5) * a.ch - a.h);
      if (Math.abs(dw) < a.cw / 2 && Math.abs(dh) < a.ch / 2) break;
      const before = [window.outerWidth, window.outerHeight];
      if (this.desktop) await this.desktop.setBounds({ width: window.outerWidth + dw, height: window.outerHeight + dh });
      else window.resizeTo(window.outerWidth + dw, window.outerHeight + dh);
      await sleep(150);
      if (window.outerWidth === before[0] && window.outerHeight === before[1]) break; // not allowed here
    }
    const x = Math.round((screen.availLeft || 0) + Math.max(0, (screen.availWidth - window.outerWidth) / 2));
    const y = Math.round((screen.availTop || 0) + Math.max(0, (screen.availHeight - window.outerHeight) / 2));
    if (Math.abs(window.screenX - x) > 1 || Math.abs(window.screenY - y) > 1) {
      if (this.desktop) await this.desktop.setBounds({ width: window.outerWidth, height: window.outerHeight, x, y });
      else window.moveTo(x, y);
    }
    this.activePane()?.layout(true);
    // Tell gecko where this window belongs, so the next `gecko open`
    // starts there instead of moving into place.
    const a = this.terminalArea();
    if (a && Math.floor(a.w / a.cw) === cols && Math.floor(a.h / a.ch) === rows) {
      this.conn.send({ t: 'window', text: JSON.stringify({
        w: window.outerWidth, h: window.outerHeight,
        sx: screen.availLeft || 0, sy: screen.availTop || 0, sw: screen.availWidth, sh: screen.availHeight,
      }) });
    }
  }

  // Restart Gecko: the background service restarts in place with the
  // installed binary (tabs keep running), then this window reloads into the
  // new version once the service is back.
  async restartService() {
    const reloadSoon = (ms) => setTimeout(() => location.reload(), ms);
    try {
      this.restarting = true;
      await this.conn.request({ t: 'upgrade' });
      this.toast('Restarting Gecko. Your tabs keep running.');
      reloadSoon(15000); // in case the reconnect is never noticed
    } catch (e) {
      this.restarting = false;
      const old = /too old|bad request|doesn.t know|can.t restart/.test(e.message);
      this.toast(old
        ? 'Reloading this window. The background service can\'t restart itself here; run gecko stop && gecko to update it (closes its tabs).'
        : e.message);
      if (old) reloadSoon(2500);
    }
  }

  // Desktop: hide or show the sidebar (remembered). Phones: open the drawer.
  toggleSidebar() {
    if (matchMedia('(max-width: 760px)').matches) {
      document.body.classList.toggle('drawer');
      return;
    }
    const hidden = document.body.classList.toggle('side-hidden');
    this.setSetting('sidebarHidden', hidden);
    this.activePane()?.layout(true);
  }

  // Fullscreen. Where the browser supports it, the keyboard is captured too,
  // so Esc (vim!) and shortcuts like Ctrl+W reach the terminal; holding Esc
  // leaves fullscreen.
  async toggleFullscreen() {
    if (document.fullscreenElement) {
      navigator.keyboard?.unlock?.();
      return document.exitFullscreen();
    }
    try {
      await document.documentElement.requestFullscreen({ navigationUI: 'hide' });
    } catch {
      return this.toast('This browser does not allow fullscreen here.');
    }
    let locked = false;
    try {
      await navigator.keyboard.lock();
      locked = true;
    } catch {}
    const leave = prettyCombo(this.keys.first('fullscreen'));
    this.toast(locked ? `Fullscreen. Hold Esc or press ${leave} to leave.` : `Fullscreen. Press ${leave} to leave.`);
  }

  closeDrawer() {
    document.body.classList.remove('drawer');
  }

  setupTouch(p) {
    if (this.touch) p.untouch = attachPane(this, p);
  }

  bindKeybar() {
    const bar = $('#keybar');
    bar.addEventListener('pointerdown', (ev) => {
      if (ev.target.closest('button')) ev.preventDefault(); // keep the keyboard up
    });
    bar.addEventListener('click', (ev) => {
      const b = ev.target.closest('button');
      if (b) this.pressKey(b.dataset.k, b.dataset.text ?? b.textContent);
    });
  }

  // A key of the key bar (or the Android app's extra keys row), by name:
  // esc, tab, ctrl, alt, arrows, ctrl-c, pgup... or text to type.
  pressKey(k, text) {
    const seq = {
      esc: '\x1b', tab: '\t', up: '\x1b[A', down: '\x1b[B', left: '\x1b[D', right: '\x1b[C',
      pgup: '\x1b[5~', pgdn: '\x1b[6~', home: '\x1b[H', end: '\x1b[F', 'ctrl-c': '\x03', 'ctrl-d': '\x04',
      pipe: '|', slash: '/', dash: '-', tilde: '~',
    };
    const id = this.activeId();
    if (k === 'ctrl' || k === 'alt') {
      this.mods[k] = !this.mods[k];
      this.renderKeybar();
      this.activePane()?.focus();
      return;
    }
    if (k === 'paste') return this.paste();
    if (k === 'select') return this.openTextView();
    if (k === 'compose') return this.toggleCompose();
    if (!id) return;
    const data = seq[k] ?? text;
    if (data) this.sendInput(id, data);
  }

  // The Android back button: closes what is open on top, innermost first.
  // False when nothing was, so the app can leave this machine.
  back() {
    if (this.closeMenu) { this.closeMenu(); return true; }
    const m = $('#modal');
    if (!m.hidden) {
      // Dialogs close on Escape; the rest have a cancel or done button.
      const at = m.contains(document.activeElement) ? document.activeElement : m.querySelector('.card') || m;
      at.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
      if (!m.hidden) m.querySelector('[data-no], [data-cancel], [data-close], [data-done]')?.click();
      if (!m.hidden) { m.hidden = true; m.innerHTML = ''; this.capturingKeys = false; }
      return true;
    }
    if (this.paletteOpen) { this.closePalette(); return true; }
    if (this.textViewOpen) { this.closeTextView(); return true; }
    if (!$('#find').hidden) { $('#find [data-close]').click(); return true; }
    if (document.body.classList.contains('drawer')) { this.closeDrawer(); return true; }
    if (!$('#compose').hidden) { this.toggleCompose(false); return true; }
    return false;
  }

  renderKeybar() {
    for (const b of document.querySelectorAll('#keybar [data-k=ctrl], #keybar [data-k=alt]')) {
      b.classList.toggle('on', !!this.mods[b.dataset.k]);
    }
    try { android?.mods(!!this.mods.ctrl, !!this.mods.alt); } catch {}
  }

  bindCompose() {
    const ta = $('#compose textarea');
    const send = () => {
      const id = this.activeId();
      if (!id || !ta.value) return;
      let text = ta.value;
      // Multi-line prompts go in as one bracketed paste so agents don't
      // submit each line separately.
      if (text.includes('\n')) text = `\x1b[200~${text}\x1b[201~`;
      this.conn.input(id, text);
      setTimeout(() => this.conn.input(id, '\r'), 30);
      ta.value = '';
      ta.style.height = '';
    };
    $('#compose [data-send]').onclick = send;
    ta.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.shiftKey && !ev.isComposing) {
        ev.preventDefault();
        send();
      }
      if (ev.key === 'Escape') this.toggleCompose(false);
    });
    ta.addEventListener('input', () => {
      ta.style.height = '';
      ta.style.height = Math.min(ta.scrollHeight, 160) + 'px';
    });
  }

  toggleCompose(on) {
    const c = $('#compose');
    c.hidden = on === undefined ? !c.hidden : !on;
    if (!c.hidden) $('#compose textarea').focus();
    else this.activePane()?.focus();
    requestAnimationFrame(() => this.activePane()?.layout(true));
  }

  // ---------------------------------------------------------------- palette

  bindPalette() {
    const input = $('#palette input');
    input.addEventListener('input', () => this.renderPalette());
    input.addEventListener('keydown', (ev) => {
      const items = this.paletteItems || [];
      if (ev.key === 'ArrowDown' || (ev.key === 'n' && ev.ctrlKey)) {
        this.paletteSel = Math.min(this.paletteSel + 1, items.length - 1);
        this.renderPaletteSel();
        ev.preventDefault();
      } else if (ev.key === 'ArrowUp' || (ev.key === 'p' && ev.ctrlKey)) {
        this.paletteSel = Math.max(this.paletteSel - 1, 0);
        this.renderPaletteSel();
        ev.preventDefault();
      } else if ((ev.key === 'PageDown' || ev.key === 'PageUp') && items.length) {
        // Move by one visible page of results.
        const ul = $('#palette ul');
        const li = ul.querySelector('li[data-i]');
        const page = Math.max(1, Math.floor(ul.clientHeight / (li?.offsetHeight || 34)) - 1);
        const d = ev.key === 'PageDown' ? page : -page;
        this.paletteSel = Math.max(0, Math.min(items.length - 1, this.paletteSel + d));
        this.renderPaletteSel();
        ev.preventDefault();
      } else if (ev.key === 'Enter') {
        ev.preventDefault();
        if (this.promptState) return this.finishPrompt(input.value);
        const item = items[this.paletteSel];
        // Ctrl/Cmd+Enter: an item's alternate action (folder browser: use it).
        if ((ev.ctrlKey || ev.metaKey) && item?.commit) return this.choose({ run: item.commit });
        this.choose(item);
      } else if (ev.key === 'Escape') {
        ev.preventDefault();
        this.closePalette();
      }
    });
    $('#palette').addEventListener('mousedown', (ev) => {
      if (ev.target.id === 'palette') this.closePalette();
    });
    $('#palette ul').addEventListener('click', (ev) => {
      const li = ev.target.closest('li[data-i]');
      if (li) this.choose(this.paletteItems[+li.dataset.i]);
    });
  }

  commands() {
    const cur = this.activeSession();
    const items = [];
    const add = (label, hint, run, kind = 'Command', combo, keywords = '') => items.push({ label, hint, run, kind, combo, keywords });
    const nums = this.tabNumbers();
    for (const s of this.allTabs().filter((t) => !t.exited)) {
      const n = nums.get(s.id);
      const where = s.repo ? `${s.repo} on ${s.branch || '?'}${s.git && (s.git.changed || s.git.untracked) ? ' (uncommitted changes)' : ''}` : shortPath(s.cwd);
      const what = s.agent ? `${s.agent.name} · ${STATUS_TEXT[s.agent.status] || s.agent.status} · ${where}` : s.kind === 'shell' ? where : s.command || s.kind;
      items.push({ kind: 'Tab', num: n, label: tabLabel(s), hint: `${s.workspace} · ${s.host} · ${what}`, run: () => this.focusSession(s.id), glyph: numGlyph(s, n) });
    }
    for (const ws of this.workspaces()) if (ws !== this.ws) add(`Switch to ${ws}`, 'Workspace', () => this.switchWorkspace(ws), 'Workspace');
    for (const t of this.templates) add(`Open ${t.name}`, `Workspace template on ${t.host || this.hostName}`, () => this.openTemplate(t.name), 'Workspace');
    for (const h of this.hosts) {
      add(`New tab on ${h.name}`, h.local ? 'This machine' : `${h.target || ''} · ${h.status}`, () => this.newTab({ host: h.name, cwd: '' }), 'Host');
    }
    for (const t of this.tmux) {
      const tab = this.tmuxTab(t.host, t.name);
      const where = tab ? `in tab ${nums.get(tab.id)}` : 'opens a new tab';
      add(`tmux ${t.name}`, `${t.host} · ${t.windows} ${t.windows === 1 ? 'window' : 'windows'} · ${where}`, () => this.openTmux(t.host, t.name), 'tmux', undefined, 'tmux session');
      for (const w of t.wins || []) {
        add(`${t.name} › ${w.index}:${w.name}`, `${t.host}${w.command && w.command !== w.name ? ' · ' + w.command : ''} · ${where}`, () => this.openTmux(t.host, t.name, w.index), 'tmux', undefined, 'tmux window');
      }
    }
    if (cur?.tmuxSession) {
      const ts = this.tmux.find((t) => t.host === cur.host && t.name === cur.tmuxSession);
      const active = ts?.wins?.find((w) => w.active);
      add('Rename tmux session…', cur.tmuxSession, () => this.renameTmuxSession(cur.host, cur.tmuxSession), 'tmux', undefined, 'tmux session rename');
      add('New tmux window', `In ${cur.tmuxSession}`, () => this.tmuxDo(cur.host, 'new-window', cur.tmuxSession), 'tmux', undefined, 'tmux window create');
      if (active) {
        add('Rename tmux window…', `${active.index}:${active.name}`, () => this.renameTmuxWindow(cur.host, cur.tmuxSession, active.index), 'tmux', undefined, 'tmux window rename');
        add('Close tmux window', `${active.index}:${active.name}`, async () => {
          if (await this.confirm(`Close tmux window \`${active.index}:${active.name}\` in \`${cur.tmuxSession}\`? This ends what runs in it.`, 'Close window')) {
            this.tmuxDo(cur.host, 'kill-window', `${cur.tmuxSession}:${active.index}`);
          }
        }, 'tmux', undefined, 'tmux window kill');
      }
      for (const t of this.tmux) {
        if (t.host === cur.host && t.name !== cur.tmuxSession) {
          add(`Switch this tab to tmux ${t.name}`, `${t.windows} ${t.windows === 1 ? 'window' : 'windows'}`, () => this.tmuxDo(cur.host, 'switch-client', t.name, '', cur.id), 'tmux', undefined, 'tmux session switch');
        }
      }
    }
    for (const a of this.allAgents()) {
      const where = tabLabel(a.session);
      add(`${a.state.name}: ${STATUS_TEXT[a.state.status] || a.state.status}`, `${a.host} · ${where} · ${a.state.detail || ''}`, () => this.openAgent(a), 'Agent');
    }
    const b = (a) => this.keys.first(a);
    add('New tab', 'Same machine and folder as this tab', () => this.newTab(), 'Command', b('newTab'));
    add('New workspace…', '', () => this.newWorkspace(), 'Command');
    if (cur && !cur.exited) {
      add('Rename tab…', '', () => this.renameTab(), 'Command', b('rename'));
      add('Move tab to workspace…', '', () => this.moveTab(), 'Command');
      add('Close tab', '', () => this.closeTab(), 'Command', b('closeTab'));
      add('Select text', 'Full scrollback as selectable text (uses tmux history inside tmux)', () => this.openTextView(), 'Command', b('selectText'));
      add('Find in terminal', '', () => this.openFind(), 'Command', b('find'));
      add('Change folder…', `Now: ${shortPath(cur.cwd)} on ${cur.host}`, () => this.openFolderPicker(cur.id), 'Command', undefined, 'cd cwd directory folder path browse');
      add('Copy all text', '', () => this.copyAll(), 'Command');
    }
    add(document.body.classList.contains('side-hidden') ? 'Show workspaces sidebar' : 'Hide workspaces sidebar', '', () => this.toggleSidebar(), 'Command', b('toggleSidebar'), 'panel sidebar workspaces hide show collapse expand');
    add(document.fullscreenElement ? 'Leave fullscreen' : 'Fullscreen', '', () => this.toggleFullscreen(), 'Command', b('fullscreen'), 'full screen maximize zen focus');
    add('Keyboard shortcuts…', 'See and change every shortcut', () => this.openShortcuts(), 'Setting', b('help'), 'keys keybindings hotkeys remap bindings shortcuts keyboard');
    add('Next tab', '', () => this.cycleTab(1), 'Command', b('nextTab'));
    add('Previous tab', '', () => this.cycleTab(-1), 'Command', b('prevTab'));
    add('Next workspace', '', () => this.cycleWorkspace(1), 'Command', b('nextWs'));
    add('Previous workspace', '', () => this.cycleWorkspace(-1), 'Command', b('prevWs'));
    add('Go to the agent that needs you', '', () => this.nextAgentNeedingYou(), 'Command', b('nextAgent'));
    if (cur && (cur.kind === 'ssh' || cur.kind === 'mosh') && cur.remote) {
      const known = this.hostForTarget(cur.remote);
      if (known) add(`New Gecko tab on ${known.name}`, 'Tabs there keep running when the connection drops', () => this.newTab({ host: known.name, cwd: '' }), 'Command', undefined, 'ssh remote');
      else add(`Make ${cur.remote} a Gecko machine…`, 'Install Gecko there; its tabs survive disconnects and show up here', () => this.addHost({ target: cur.remote, check: true }), 'Command', undefined, 'ssh install remote host server adopt');
    }
    add('Add a machine…', 'Run sessions on another computer over SSH', () => this.addHost(), 'Command', undefined, 'ssh host server remote connect computer');
    for (const h of this.hosts) {
      if (!h.local && h.status === 'connected') add(`Update Gecko on ${h.name}`, (h.version || 0) < PROTOCOL ? 'It runs an older version; its tabs keep running' : 'Reinstall this version there; its tabs keep running', () => this.updateHost(h.name), 'Command', undefined, 'upgrade install remote machine host');
    }
    for (const h of this.hosts) if (!h.local) add(`Remove machine ${h.name}`, 'Its sessions keep running there', () => this.removeHost(h.name), 'Command');
    add('Restart Gecko', 'Restarts the background service with the installed version; your tabs and what runs in them keep going', () => this.restartService(), 'Command', undefined, 'restart upgrade update reload daemon service');
    add('Open on another device', 'Phone or tablet: shows a link and QR code', () => this.openShare(), 'Command', undefined, 'phone mobile tablet qr share link');
    add('Notify me when agents need input', '', () => this.enableNotifications(), 'Setting', undefined, 'notifications alerts');
    add('Edit quick replies…', 'The next steps offered on an idle agent', () => this.editQuickReplies(), 'Setting', undefined, 'agent claude buttons prompts go commit push');
    add(`Startup sound: ${this.settings.startupSound ? 'on' : 'off'}`, 'A short chime when Gecko opens', () => this.setSetting('startupSound', !this.settings.startupSound), 'Setting', undefined, 'sound audio chime startup launch mute');
    add(`Copy on select: ${this.settings.copyOnSelect ? 'on' : 'off'}`, 'Toggle', () => this.setSetting('copyOnSelect', !this.settings.copyOnSelect), 'Setting', undefined, 'clipboard selection');
    if (!isMac) add(`Ctrl+C copies selected text: ${this.settings.smartCopy ? 'on' : 'off'}`, 'Without a selection it still interrupts', () => this.setSetting('smartCopy', !this.settings.smartCopy), 'Setting');
    if (isMac) add(`Option key as Meta: ${this.settings.optionIsMeta ? 'on' : 'off'}`, 'Toggle', () => this.setSetting('optionIsMeta', !this.settings.optionIsMeta), 'Setting');
    const cur0 = this.settings.theme === AUTO ? 'Gecko (follows system)' : resolveTheme(this.settings.theme).name;
    add('Change Theme', `Now: ${cur0}`, () => this.openThemePicker(), 'Setting', undefined, 'switch change theme themes colors colours color scheme palette appearance dark light mode background wallpaper skin');
    if (this.desktop?.transparent) {
      add('Background opacity…', `Now: ${this.settings.opacity >= 1 ? 'solid' : Math.round(this.settings.opacity * 100) + '%'} over the desktop`, () => this.openOpacityPicker(), 'Setting', undefined, 'transparency transparent see-through blur glass alpha window opacity');
    }
    const gl = this.settings.renderer === 'webgl' || (this.settings.renderer === 'auto' && !this.touch);
    add(`Renderer: ${gl ? 'WebGL' : 'DOM'}`, `Switch to ${gl ? 'DOM (most compatible)' : 'WebGL (fastest)'}; reloads the page`, () => {
      this.setSetting('renderer', gl ? 'dom' : 'webgl');
      location.reload();
    }, 'Setting', undefined, 'gpu webgl dom rendering performance');
    add('Bigger text', '', () => this.setFont(this.fontSize() + 1), 'Setting', b('fontUp'), 'font size zoom in larger');
    add('Smaller text', '', () => this.setFont(this.fontSize() - 1), 'Setting', b('fontDown'), 'font size zoom out');
    add('Toggle compose box', 'Type prompts with your keyboard\'s autocorrect, send with Enter', () => this.toggleCompose(), 'Command', undefined, 'prompt message write input');
    return items;
  }

  // picker: { placeholder, items, select, onSelect(item), onCancel() } turns
  // the palette into a chooser over its own items.
  openPalette(initial = '', picker = null) {
    this.promptState = null;
    this.content = null;
    this.paletteOpen = true;
    this.picker = picker;
    const el = $('#palette');
    el.hidden = false;
    el.classList.remove('prompt');
    const input = $('#palette input');
    input.placeholder = picker?.placeholder || 'Jump to a tab, agent, workspace or machine. Or run a command.';
    input.value = initial;
    $('#palette .note').textContent = '';
    this.allCommands = picker ? picker.items : this.commands();
    this.renderPalette();
    if (picker?.select > 0) {
      this.paletteSel = picker.select;
      this.renderPaletteSel();
    }
    input.focus();
  }

  // Run a palette item. The palette closes first, so items that open a
  // prompt or another picker can reuse it.
  choose(item) {
    if (!item) return;
    if (item.keepOpen) return item.run(); // e.g. going into a folder
    this.picker = null; // chosen, not cancelled
    this.closePalette();
    item.run();
  }

  closePalette(result) {
    $('#palette').hidden = true;
    this.paletteOpen = false;
    const pk = this.picker;
    this.picker = null;
    if (pk) pk.onCancel?.();
    const ps = this.promptState;
    this.promptState = null;
    if (ps) ps.resolve(result ?? null);
    if (!this.textViewOpen) this.activePane()?.focus();
  }

  renderPalette(keepSel = false) {
    const q = $('#palette input').value.trim();
    if (this.promptState) return;
    let items = this.allCommands;
    if (typeof items === 'function') {
      items = items(q); // the picker filters its own items
    } else if (q === '?') {
      items = this.allCommands.filter((i) => i.combo || i.label === 'Keyboard shortcuts…');
    } else if (/^\d+$/.test(q)) {
      // A number jumps to that tab; other matches follow.
      const n = +q;
      items = [...items.filter((i) => i.num === n), ...items.filter((i) => i.num !== n && `${i.label} ${i.hint}`.includes(q))];
    } else if (q) {
      items = items
        .map((i) => {
          // Exact keyword ("colors" -> Change Theme) beats a loose fuzzy match.
          const kw = i.keywords && ` ${i.keywords} `.includes(` ${q.toLowerCase()}`) ? 60 : -1;
          return { i, s: Math.max(kw, fuzzy(q, i.label) * 2, fuzzy(q, `${i.num ?? ''} ${i.label} ${i.hint} ${i.kind} ${i.keywords || ''}`)) };
        })
        .filter((x) => x.s >= 0)
        .sort((a, b) => b.s - a.s)
        .map((x) => x.i);
    }
    items = items.slice(0, 60);
    if (!this.picker && q.length >= 2 && !/^\d+$/.test(q)) {
      items = items.concat(this.contentItems(q));
      this.searchContent(q);
    }
    this.paletteItems = items;
    this.paletteSel = keepSel ? Math.min(this.paletteSel, Math.max(items.length - 1, 0)) : 0;
    $('#palette ul').innerHTML = this.paletteItems.map((i, n) => `
      <li data-i="${n}">
        <span class="k">${esc(i.kind)}</span>
        <span class="l${i.mono ? ' mono' : ''}">${i.glyph || ''}<span class="t">${esc(i.label)}</span></span>
        <span class="h">${esc(i.hint || '')}</span>
        ${i.combo ? `<kbd>${esc(prettyCombo(i.combo))}</kbd>` : ''}
      </li>`).join('') || `<li class="none">Nothing matches “${esc(q)}”.</li>`;
    this.renderPaletteSel();
  }

  // Ask every host to search its tabs' text (debounced); results arrive
  // later and are merged into the open palette.
  searchContent(q) {
    if (this.content?.q === q) return;
    clearTimeout(this.contentTimer);
    this.contentTimer = setTimeout(async () => {
      try {
        const r = await this.conn.request({ t: 'search', text: q });
        this.content = { q, matches: r.matches || [] };
      } catch {
        this.content = { q, matches: [] };
      }
      if (this.paletteOpen && !this.picker && !this.promptState && $('#palette input').value.trim() === q) this.renderPalette(true);
    }, 150);
  }

  contentItems(q) {
    if (this.content?.q !== q) return [];
    const nums = this.tabNumbers();
    return this.content.matches
      .filter((m) => this.sessions.has(m.id))
      .map((m) => {
        const s = this.sessions.get(m.id);
        return {
          kind: 'In tab',
          label: m.line,
          hint: `${tabLabel(s)} · ${s.workspace}${m.count > 1 ? ` · ${m.count} matches` : ''}`,
          glyph: numGlyph(s, nums.get(m.id)),
          mono: true,
          run: () => this.revealMatch(m.id, q),
        };
      });
  }

  // Switch to a tab and highlight the most recent occurrence of q.
  revealMatch(id, q) {
    this.focusSession(id);
    const p = this.panes.get(id);
    if (!p) return;
    const find = () => p.search.findPrevious(q, { caseSensitive: false });
    // A freshly opened tab is still replaying its history.
    setTimeout(() => { if (!find()) setTimeout(find, 400); }, 150);
  }

  renderPaletteSel() {
    const lis = document.querySelectorAll('#palette li[data-i]');
    lis.forEach((li, n) => li.classList.toggle('sel', n === this.paletteSel));
    lis[this.paletteSel]?.scrollIntoView({ block: 'nearest' });
    this.picker?.onSelect?.(this.paletteItems?.[this.paletteSel]);
  }

  // A one-line text prompt in the palette's place.
  prompt(title, value = '', note = '') {
    return new Promise((resolve) => {
      if (this.promptState) this.promptState.resolve(null);
      this.paletteOpen = true;
      this.promptState = { resolve };
      const el = $('#palette');
      el.hidden = false;
      el.classList.add('prompt');
      const input = $('#palette input');
      input.placeholder = title;
      input.value = value;
      $('#palette .note').textContent = note ? `${title}. ${note}` : title;
      $('#palette ul').innerHTML = '';
      input.focus();
      input.select();
    });
  }

  finishPrompt(v) {
    this.closePalette(v.trim());
  }

  // The quick replies on an idle agent's card: a button label and the
  // text it sends, per row; drag the handle to reorder.
  editQuickReplies() {
    const m = $('#modal');
    const MAX = 8;
    let rows = parseQuickReplies(this.settings.quickReplies).map((q) => ({ label: q.label, text: q.text }));
    m.innerHTML = `<div class="card quick" role="dialog" aria-modal="true" aria-labelledby="qr-title">
      <h2 id="qr-title">Quick replies</h2>
      <p>Buttons on an idle agent's card. Each one sends its text as the agent's next message.</p>
      <div class="qr-head" aria-hidden="true"><span></span><span>Button</span><span>Sends</span><span></span></div>
      <ol class="qr-list"></ol>
      <button class="qr-add" data-add>+ Add reply</button>
      <div class="qr-preview" aria-label="Preview"></div>
      <div class="row"><button data-reset>Reset to defaults</button><span></span><button data-no>Cancel</button><button class="primary" data-yes>Save</button></div></div>`;
    m.hidden = false;
    const list = m.querySelector('.qr-list');
    const auto = (t) => (t ? t[0].toUpperCase() + t.slice(1) : '');
    // The prompt box fits its text (a prompt is one line when sent).
    const grow = (ta) => { ta.style.height = 'auto'; ta.style.height = ta.scrollHeight + 2 + 'px'; };
    const preview = () => {
      const shown = rows.filter((r) => r.text.trim());
      m.querySelector('.qr-preview').innerHTML = shown.length
        ? `<span class="replies">${shown.map((r) => `<button tabindex="-1"><span>${esc(r.label.trim() || auto(r.text.trim()))}</span></button>`).join('')}</span>`
        : '<em>No quick replies: idle agents get no buttons.</em>';
      m.querySelector('[data-add]').disabled = rows.length >= MAX;
    };
    const draw = (focus) => {
      list.innerHTML = rows.map((r, i) => `<li data-i="${i}">
        <span class="grip" title="Drag to reorder" aria-hidden="true">⠿</span>
        <input type="text" data-f="label" value="${esc(r.label)}" placeholder="${esc(auto(r.text) || 'Label')}" aria-label="Button label ${i + 1}" maxlength="30">
        <textarea data-f="text" rows="1" placeholder="text to send" aria-label="Text sent ${i + 1}">${esc(r.text)}</textarea>
        <button class="x" data-del="${i}" aria-label="Remove reply ${i + 1}" title="Remove">×</button></li>`).join('');
      list.querySelectorAll('textarea').forEach(grow);
      preview();
      if (focus != null) list.querySelector(`li[data-i="${focus}"] [data-f="text"]`)?.focus();
    };
    list.addEventListener('input', (ev) => {
      const li = ev.target.closest('li');
      if (ev.target.tagName === 'TEXTAREA') {
        if (/\n/.test(ev.target.value)) ev.target.value = ev.target.value.replace(/\s*\n\s*/g, ' ');
        grow(ev.target);
      }
      rows[+li.dataset.i][ev.target.dataset.f] = ev.target.value;
      if (ev.target.dataset.f === 'text') li.querySelector('[data-f="label"]').placeholder = auto(ev.target.value) || 'Label';
      preview();
    });
    list.addEventListener('click', (ev) => {
      const del = ev.target.closest('[data-del]');
      if (del) { rows.splice(+del.dataset.del, 1); draw(); }
    });
    // Reorder by dragging a row's handle.
    let from = null;
    list.addEventListener('pointerdown', (ev) => { const li = ev.target.closest('.grip')?.closest('li'); if (li) li.draggable = true; });
    list.addEventListener('dragstart', (ev) => { from = +ev.target.dataset.i; ev.target.classList.add('dragging-row'); ev.dataTransfer.effectAllowed = 'move'; });
    list.addEventListener('dragover', (ev) => {
      const li = ev.target.closest('li');
      if (from == null || !li) return;
      ev.preventDefault();
      const to = +li.dataset.i;
      if (to === from) return;
      rows.splice(to, 0, rows.splice(from, 1)[0]);
      from = to;
      draw();
      list.querySelector(`li[data-i="${to}"]`).classList.add('dragging-row');
    });
    list.addEventListener('dragend', () => { from = null; draw(); });
    m.querySelector('[data-add]').onclick = () => { if (rows.length < MAX) { rows.push({ label: '', text: '' }); draw(rows.length - 1); } };
    m.querySelector('[data-reset]').onclick = () => { rows = parseQuickReplies(null).map((q) => ({ label: q.label, text: q.text })); draw(); };
    const done = () => { m.hidden = true; m.innerHTML = ''; this.activePane()?.focus(); };
    const save = () => {
      const lines = rows.map((r) => ({ label: r.label.trim(), text: r.text.trim() })).filter((r) => r.text)
        // Just the text when the label is the automatic one (and the text has no
        // | that would read as one).
        .map((r) => (r.label && r.label !== auto(r.text)) || r.text.includes('|') ? `${(r.label || auto(r.text)).replace(/\|/g, '/')} | ${r.text}` : r.text);
      const defaults = parseQuickReplies(null).map((q) => `${q.label} | ${q.text}`);
      const same = lines.length === defaults.length && parseQuickReplies(lines).every((q, i) => `${q.label} | ${q.text}` === defaults[i]);
      this.setSetting('quickReplies', same ? null : lines);
      this.renderDock();
      done();
    };
    m.querySelector('[data-yes]').onclick = save;
    m.querySelector('[data-no]').onclick = done;
    m.onkeydown = (ev) => {
      if (ev.key === 'Escape') done();
      if (ev.key === 'Enter' && ev.target.matches('input, textarea')) { ev.preventDefault(); save(); }
    };
    draw();
    (list.querySelector('input[data-f="label"]') || m.querySelector('[data-add]')).focus();
  }

  confirm(text, verb) {
    return new Promise((resolve) => {
      const m = $('#modal');
      m.innerHTML = `<div class="card" role="alertdialog" aria-modal="true">
        <p>${md(text)}</p>
        <div class="row"><button data-no>Cancel</button><button class="primary" data-yes>${esc(verb)}</button></div></div>`;
      m.hidden = false;
      const done = (v) => { m.hidden = true; m.innerHTML = ''; resolve(v); this.activePane()?.focus(); };
      m.querySelector('[data-yes]').onclick = () => done(true);
      m.querySelector('[data-no]').onclick = () => done(false);
      m.onkeydown = (ev) => { if (ev.key === 'Escape') done(false); };
      m.querySelector('[data-yes]').focus();
    });
  }

  async openShare() {
    // The Android app running Gecko on the phone shows its own link and QR code.
    try { if (android?.shareDevice?.()) return; } catch {}
    const m = $('#modal');
    let urls = [];
    try {
      const r = await fetch('/api/info');
      urls = (await r.json()).urls || [];
    } catch {}
    const here = location.origin + '/';
    let body;
    if (location.hostname !== '127.0.0.1' && location.hostname !== 'localhost') {
      urls = [here];
      body = '<p>Open this address on your other device. You may need the access token: run <code>gecko url</code> on the machine running Gecko.</p>';
    } else if (!urls.length) {
      body = `<p>Gecko only accepts connections from this computer right now.</p>
        <p>To use it from your phone, restart the daemon listening on your network or tailnet, for example <code>gecko stop && gecko daemon --listen 0.0.0.0:7681</code>, or set <code>"listen"</code> in the config. Over the internet, put it behind Tailscale (<code>tailscale serve 7681</code>) rather than exposing the port.</p>`;
    } else {
      body = '<p>Scan with your phone (same network or tailnet). The link contains your access token, so treat it like a password.</p>';
    }
    const qrs = urls.map((u) => {
      const q = qrcode(0, 'M');
      q.addData(u);
      q.make();
      return `<figure>${q.createSvgTag({ cellSize: 4, margin: 2, scalable: true })}<figcaption><code>${esc(u)}</code></figcaption></figure>`;
    }).join('');
    m.innerHTML = `<div class="card share" role="dialog" aria-modal="true"><h2>Open on another device</h2>${body}<div class="qrs">${qrs}</div>
      <div class="row"><button class="primary" data-no>Done</button></div></div>`;
    m.hidden = false;
    m.querySelector('[data-no]').onclick = () => { m.hidden = true; m.innerHTML = ''; };
    m.querySelector('[data-no]').focus();
  }

  // ---------------------------------------------------------------- text view

  bindTextView() {
    const tv = $('#textview');
    tv.querySelector('[data-close]').onclick = () => this.closeTextView();
    tv.querySelector('[data-copy-sel]').onclick = async () => {
      const sel = window.getSelection().toString();
      if (!sel) return this.toast('Select some text first.');
      this.toast((await copyText(sel)) ? 'Copied' : 'Copy failed');
    };
    tv.querySelector('[data-copy-all]').onclick = async () => {
      this.toast((await copyText(tv.querySelector('pre').textContent)) ? 'Copied everything' : 'Copy failed');
    };
    tv.addEventListener('keydown', (ev) => {
      if (ev.key === 'Escape') this.closeTextView();
    });
    tv.querySelector('input').addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter') {
        ev.preventDefault();
        if (!window.find?.(ev.target.value, false, ev.shiftKey, true)) this.toast('No more matches');
      }
    });
  }

  async openTextView() {
    const id = this.activeId();
    const p = this.activePane();
    if (!id || !p) return;
    let text;
    try {
      text = (await this.conn.request({ t: 'capture', id })).text;
    } catch {
      text = null;
    }
    if (text == null || text === '') {
      const buf = p.term.buffer.active;
      const lines = [];
      for (let i = 0; i < buf.length; i++) lines.push(buf.getLine(i)?.translateToString(true) ?? '');
      while (lines.length && !lines[lines.length - 1]) lines.pop();
      text = lines.join('\n');
    }
    const tv = $('#textview');
    const s = this.sessions.get(id);
    tv.querySelector('h2').textContent = s ? tabLabel(s) : 'Text';
    tv.querySelector('pre').textContent = text;
    tv.hidden = false;
    this.textViewOpen = true;
    const pre = tv.querySelector('pre');
    pre.scrollTop = pre.scrollHeight;
    pre.focus();
  }

  closeTextView() {
    $('#textview').hidden = true;
    this.textViewOpen = false;
    this.activePane()?.focus();
  }

  async copyAll() {
    const id = this.activeId();
    if (!id) return;
    try {
      const { text } = await this.conn.request({ t: 'capture', id });
      this.toast((await copyText(text)) ? 'Copied everything' : 'Copy failed');
    } catch (e) {
      this.toast(e.message);
    }
  }

  // ---------------------------------------------------------------- find

  // Find in the terminal: every match highlighted, the current one
  // stronger, "3 of 7". It searches again as the terminal changes, so in a
  // full-screen app that redraws (Claude) the highlights follow the text,
  // and scrolling in the app finds more.
  bindFind() {
    const box = $('#find');
    const input = box.querySelector('input');
    const count = box.querySelector('.count');
    const note = box.querySelector('.note');
    let pane = null, watch = null, results = null, timer = 0;
    const decorations = () => {
      const css = getComputedStyle(document.documentElement);
      const bg = toRgb(css.getPropertyValue('--surface-term') || css.getPropertyValue('--bg'));
      const accent = toRgb(css.getPropertyValue('--accent'));
      return {
        matchBackground: hexMix(bg, accent, 0.35), matchOverviewRuler: hexMix(bg, accent, 0.6),
        activeMatchBackground: hexMix(bg, accent, 0.8), activeMatchBorder: hexMix(bg, accent, 1), activeMatchColorOverviewRuler: hexMix(bg, accent, 1),
      };
    };
    const detach = () => {
      watch?.dispose();
      results?.dispose();
      watch = results = null;
      clearTimeout(timer);
      pane?.search.clearDecorations();
      pane = null;
    };
    // Follow the shown tab, and its changes.
    const attach = () => {
      const p = this.activePane();
      if (p === pane) return p;
      detach();
      pane = p;
      if (!p) return null;
      results = p.search.onDidChangeResults(({ resultIndex, resultCount }) => {
        count.textContent = !input.value ? '' : !resultCount ? 'No results' : resultIndex < 0 ? `${resultCount}+` : `${resultIndex + 1} of ${resultCount}`;
        box.classList.toggle('miss', !!input.value && !resultCount);
      });
      watch = p.term.onWriteParsed(() => {
        clearTimeout(timer);
        timer = setTimeout(() => go(false, true), 150);
      });
      return p;
    };
    const go = (back, again = false) => {
      const p = attach();
      if (!p) return;
      const s = this.sessions.get(p.id);
      const full = p.term.buffer.active.type === 'alternate';
      note.hidden = !full;
      if (full) note.textContent = `${s ? tabLabel(s) : 'This app'} is full screen, so only what's on screen can be searched. Scroll in it to search further.`;
      if (!input.value) {
        p.search.clearDecorations();
        count.textContent = '';
        box.classList.remove('miss');
        return;
      }
      const opts = { decorations: decorations(), incremental: again };
      if (back) p.search.findPrevious(input.value, opts);
      else p.search.findNext(input.value, opts);
    };
    const close = () => {
      box.hidden = true;
      detach();
      this.activePane()?.focus();
    };
    input.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter') { ev.preventDefault(); go(ev.shiftKey); }
      if (ev.key === 'Escape') close();
    });
    input.addEventListener('input', () => go(false, true));
    box.querySelector('[data-prev]').onclick = () => go(true);
    box.querySelector('[data-next]').onclick = () => go(false);
    box.querySelector('[data-close]').onclick = close;
    this.findGo = go;
    // Another tab shown while Find is open: search that one.
    this.findFollow = () => { if (!box.hidden && this.activePane() !== pane) go(false, true); };
  }

  openFind(text = '') {
    const box = $('#find');
    box.hidden = false;
    const input = box.querySelector('input');
    const sel = text || this.activePane()?.term.getSelection();
    if (sel && !sel.includes('\n')) input.value = sel;
    input.focus();
    input.select();
    this.findGo(false, true);
  }

  // ---------------------------------------------------------------- render

  // This web UI's build id, e.g. "a1b2c3d · Oct 17 02:15" (see build.js).
  buildId() {
    return buildId(BUILD.commit, BUILD.time);
  }

  // ⋮ > Touch diagnostics in the Android app: what the last gesture on a
  // terminal looked like, to tell what a real phone sends.
  touchReport() {
    const t = this.touchStats;
    const lines = [
      `Web UI build: ${this.buildId()}`,
      `UI: ${this.bundled ? 'from the app' : 'from the machine'}, protocol ${PROTOCOL}; Gecko there: ${this.daemonVersion || '?'}`,
      `Screen: ${innerWidth}x${innerHeight} CSS px at ${devicePixelRatio}x; touch: ${this.touch}`,
      `Reduced motion: ${matchMedia('(prefers-reduced-motion: reduce)').matches ? 'yes (flings still coast; no stretch at the ends)' : 'no'}`,
    ];
    if (!t) lines.push('No gesture yet: scroll, flick or swipe on a terminal, then look again.');
    else {
      lines.push(`Last gesture (${t.at}): ${t.kind || 'tap'}`);
      lines.push(`Samples: ${t.n} over ${Math.round(t.span)} ms, steps ${t.dtMin.toFixed(1)} to ${t.dtMax.toFixed(1)} ms`);
      lines.push(`Moves: ${t.moves}, taken over by the browser: ${t.late}`);
      lines.push(`Speed at release: ${t.v} px/ms; fling distance: ${t.fling} px${t.boosted ? ` (boosted from ${t.boosted})` : ''}`);
      if (t.keys !== undefined) lines.push(`Cursor: ${t.keys} steps dragged, ${t.flingKeys} flung`);
      if (t.flung !== undefined) lines.push(t.flung ? `Fling: yes${t.why ? ` (${t.why})` : ''}` : `Why no fling: ${t.why}`);
      if (t.lift) lines.push(`Lifted ${t.lift[0]} ms after the last move${t.lift[1] !== null ? ` (${t.lift[1]} ms as received)` : ''}`);
      if (t.steps?.length) lines.push(`Last samples (dt ms, d px): ${t.steps.map((q) => `${q.dt}, ${q.d}`).join(' | ')}`);
    }
    return lines.join('\n');
  }

  toast(text, onClick) {
    const t = document.createElement('div');
    t.className = 'toast';
    t.innerHTML = md(text);
    t.setAttribute('role', 'status');
    if (onClick) {
      t.classList.add('clickable');
      t.onclick = () => { onClick(); t.remove(); };
    }
    $('#toasts').appendChild(t);
    setTimeout(() => t.remove(), onClick ? 8000 : 3500);
  }

  renderSoon() {
    if (this.raf) return;
    this.raf = requestAnimationFrame(() => {
      this.raf = 0;
      this.render();
    });
  }

  render() {
    // Re-rendering mid-drag would pull the rows out from under the pointer.
    if (this.drag) { this.drag.stale = true; return; }
    this.renderWatch?.();
    this.renderSidebar();
    this.renderTabs();
    this.showActive();
    this.findFollow?.();
    this.renderEmpty();
    this.renderDock();
    this.renderStatus();
    this.updateTitle();
  }

  renderSidebar() {
    const parts = [];
    parts.push('<div class="group workspaces"><h3>Workspaces <button class="mini" data-act="new-ws" title="New workspace" aria-label="New workspace">+</button></h3>');
    const nums = this.tabNumbers();
    for (const ws of this.workspaces()) {
      const tabs = this.tabsOf(ws);
      const agents = tabs.filter((t) => t.agent);
      const needing = agents.filter((t) => t.agent.status === 'needs-input');
      const workers = agents.filter((t) => t.agent.status === 'working');
      const need = needing.length, working = workers.length;
      const hosts = [...new Set(tabs.map((t) => t.host))].filter((h) => h && h !== this.hostName);
      const folded = this.collapsed[ws];
      parts.push(`<div class="ws ${ws === this.ws ? 'cur' : ''}" data-ws="${esc(ws)}">
        <div class="ws-row" data-drag="ws" data-key="${esc(ws)}">
          <button class="ws-name" data-act="ws" data-ws="${esc(ws)}" aria-expanded="${!folded}" title="Click to ${folded ? 'expand' : 'collapse'}, double-click to rename"><span class="fold" aria-hidden="true">${folded ? '▸' : '▾'}</span>${esc(ws)}</button>
          ${need ? `<span class="agents need" title="Agents waiting for you">${groupAgentMark(needing, 'needs-input')}${need}</span>` : ''}
          ${working ? `<span class="agents work" title="Agents working">${groupAgentMark(workers, 'working')}${working}</span>` : ''}
          ${hosts.length ? `<span class="hosts">${esc(hosts.join(', '))}</span>` : ''}
          <button class="x" data-act="ws-close" data-ws="${esc(ws)}" aria-label="Close workspace ${esc(ws)}" title="Close workspace and its tabs">×</button>
        </div>`);
      if (!folded) {
        parts.push('<ul>');
        for (const t of tabs) {
          const active = ws === this.ws && t.id === this.activeId();
          const sub = t.agent?.detail || (t.kind === 'shell' ? shortPath(t.cwd) : t.command) || '';
          const tip = [t.host, t.repo && `${t.repo} on ${t.branch || '?'}${t.git ? ' (' + gitStateText(t.git) + ')' : ''}`, sub].filter(Boolean).join(' · ');
          parts.push(`<li><div class="tab-row ${active ? 'cur' : ''} ${t.exited ? 'gone' : ''}" role="button" data-act="tab" data-id="${esc(t.id)}" data-drag="tab" data-key="${esc(t.id)}" title="${esc(tip)}">
            ${numGlyph(t, nums.get(t.id), true)}<span class="nm">${agentMark(t)}${esc(tabLabel(t))} ${gitChip(t)}</span>${t.host !== this.hostName ? `<span class="where">${esc(t.host)}</span>` : '<span></span>'}<button class="x" data-act="tab-close" data-id="${esc(t.id)}" aria-label="Close tab" title="Close tab">×</button></div></li>`);
        }
        parts.push('</ul>');
      }
      parts.push('</div>');
    }
    const open = new Set(this.workspaces());
    for (const t of this.templates) {
      if (open.has(t.name)) continue;
      parts.push(`<div class="ws template"><button class="ws-name" data-act="template" data-name="${esc(t.name)}">${esc(t.name)} <span class="hosts">open</span></button></div>`);
    }
    parts.push('</div><div class="group"><h3>Machines <button class="mini" data-act="add-host" title="Add a machine" aria-label="Add a machine">+</button></h3><ul class="hostlist">');
    const next = this.defaultHost();
    for (const h of this.sortedHosts()) {
      const tm = this.tmux.filter((t) => t.host === h.name);
      const broken = !h.local && h.status === 'error';
      const outdated = !h.local && h.status === 'connected' && (h.version || 0) < PROTOCOL;
      parts.push(`<li class="host st-${esc(h.status)}${h.name === next ? ' next' : ''}" data-host="${esc(h.name)}">
        <div class="host-row" data-drag="host" data-key="${esc(h.name)}">
          <button class="host-main" data-act="${broken ? 'host-fix' : outdated ? 'host-update' : 'host-new'}" data-host="${esc(h.name)}" title="${esc(broken ? 'Fix the connection to ' + h.name : outdated ? h.name + ' runs an older Gecko; click to update it (its tabs keep running)' : 'New tab on ' + h.name + (h.name === next ? `. New tabs (${prettyCombo(this.keys.first('newTab'))}) open here, on the current tab's machine` : ''))}">
            <i class="hdot"></i><span class="nm">${esc(h.name)}</span><span class="where">${esc(h.local ? 'this machine' : h.status === 'connected' ? h.os || '' : h.status)}</span><span class="plus${outdated ? ' upd' : ''}">${broken ? 'fix' : outdated ? 'update' : '+'}</span>
          </button>
          ${h.local ? '' : `<button class="x" data-act="host-rm" data-host="${esc(h.name)}" aria-label="Remove ${esc(h.name)}" title="Remove ${esc(h.name)}">×</button>`}
        </div>
        ${h.error && h.status !== 'connected' ? `<div class="err">${esc(h.error)}</div>` : ''}
        ${tm.map((t) => {
          const tab = this.tmuxTab(h.name, t.name);
          const open = !!this.tmuxOpen[h.name + '/' + t.name];
          const wins = (t.wins || []).map((w) => `<button class="tmux-win ${w.active ? 'on' : ''}" data-act="tmux-win" data-host="${esc(h.name)}" data-name="${esc(t.name)}" data-idx="${w.index}" title="${esc((tab ? 'Show this window in tab ' + nums.get(tab.id) : 'Open in a new tab') + '. Double-click to rename.')}"><span class="wi">${w.index}</span>${windowAgentMark(w)}${esc(w.name)}${w.command && w.command !== w.name ? `<span class="where">${esc(w.command)}</span>` : ''}</button>`).join('');
          return `<div class="tmux-row">
            <button class="fold" data-act="tmux-fold" data-host="${esc(h.name)}" data-name="${esc(t.name)}" aria-label="${open ? 'Hide' : 'Show'} windows">${open ? '▾' : '▸'}</button>
            <button class="tmux" data-act="tmux" data-host="${esc(h.name)}" data-name="${esc(t.name)}" title="${esc((tab ? 'Go to tab ' + nums.get(tab.id) + ', which shows this session' : 'Open in a new tab') + '. Double-click to rename.')}">${TMUX_ICON}<b>${esc(t.name)}</b><span class="where">${t.windows} ${t.windows === 1 ? 'window' : 'windows'}</span>${tab ? numGlyph(tab, nums.get(tab.id)) : ''}</button>
          </div>${open ? `<div class="tmux-wins">${wins}</div>` : ''}`;
        }).join('')}
      </li>`);
    }
    parts.push('</ul></div>');
    $('#side-body').innerHTML = parts.join('');
    const st = this.conn.status;
    const conn = $('#conn');
    conn.className = 'conn ' + (st === 'online' && this.outdated ? 'outdated' : st);
    conn.textContent = st === 'online' ? (this.outdated ? 'Restart needed' : '') : st === 'unauthorized' ? 'Not signed in' : st === 'offline' ? 'Reconnecting…' : 'Connecting…';
    conn.title = st === 'online' && this.outdated ? OUTDATED : '';
  }

  renderTabs() {
    const tabs = this.tabsOf(this.ws);
    const act = this.activeId();
    const nums = this.tabNumbers();
    $('#ws-title').textContent = this.ws;
    // + says where a new tab opens, once there's more than one machine.
    const next = this.defaultHost();
    const nt = $('#new-tab');
    nt.innerHTML = '+' + (this.hosts.length > 1 && next ? `<span class="where">${esc(next)}</span>` : '');
    nt.title = `New tab on ${next || 'this machine'} (${prettyCombo(this.keys.first('newTab'))})`;
    $('#tabs').innerHTML = tabs.map((t) => {
      const n = nums.get(t.id);
      return `
      <div class="tab ${t.id === act ? 'cur' : ''} ${t.exited ? 'gone' : ''} ${t.attention ? 'bell' : ''}" data-tab="${esc(t.id)}" data-drag="tab" data-key="${esc(t.id)}" role="tab" aria-selected="${t.id === act}" title="${esc(`${tabLabel(t)} - ${t.host}${t.cwd ? ' · ' + t.cwd : ''}${n <= 9 ? ` (${prettyCombo((isMac ? 'cmd+' : 'alt+') + n)})` : ''}`)}">
        ${numGlyph(t, n, true)}<span class="nm">${agentMark(t)}${esc(tabLabel(t))}</span>${t.repo && t.repo !== tabLabel(t) ? `<span class="git"><span class="repo-name">${esc(t.repo)}</span></span>` : ''}${t.host !== this.hostName ? `<span class="where">${esc(t.host)}</span>` : ''}
        <button class="x" data-close="${esc(t.id)}" aria-label="Close tab">×</button>
      </div>`;
    }).join('');
    $('#tabs .cur')?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  renderEmpty() {
    const el = $('#empty');
    if (el.hidden) return;
    if (this.fitRequest && this.firstSync) {
      // Opened with no tabs: size the window for the first one now.
      const { cols, rows } = this.fitRequest;
      this.fitRequest = null;
      requestAnimationFrame(() => this.fitWindow(cols, rows));
    }
    const hosts = this.hosts.length ? this.sortedHosts() : [{ name: this.hostName || 'this machine', local: true, status: 'connected' }];
    const offline = this.conn.status !== 'online';
    el.innerHTML = offline
      ? `<div class="hello"><h1>${this.conn.status === 'unauthorized' ? 'Gecko needs your access link' : 'Connecting to Gecko…'}</h1>
         <p>${this.conn.status === 'unauthorized'
          ? 'Open the link printed by <code>gecko url</code> on the machine running Gecko.'
          : 'If this takes long, start the daemon with <code>gecko</code> on that machine.'}</p></div>`
      : `<div class="hello">
        <h1>${this.sessions.size ? `${esc(this.ws)} has no tabs` : 'No sessions yet'}</h1>
        <p>Open a shell on one of your machines. Sessions keep running when you close this window or lose the connection.</p>
        <div class="choices">${hosts.map((h) => `<button class="primary" data-act="host-new" data-host="${esc(h.name)}" ${h.status !== 'connected' ? 'disabled' : ''}>New tab on ${esc(h.name)}</button>`).join('')}
        ${this.templates.map((t) => `<button data-act="template" data-name="${esc(t.name)}">Open ${esc(t.name)}</button>`).join('')}</div>
        <p class="keys">${prettyCombo(this.keys.first('palette'))} search everything · ${prettyCombo(this.keys.first('newTab'))} new tab · ${prettyCombo(this.keys.first('nextAgent'))} agent that needs you</p>
      </div>`;
  }

  renderDock() {
    const agents = this.allAgents();
    const dock = $('#dock');
    dock.hidden = !agents.length;
    if (!agents.length) {
      dock.innerHTML = '';
      return;
    }
    const act = this.activeId();
    dock.innerHTML = agents.map((a) => {
      const s = a.session;
      const where = tabLabel(s) === a.state.name ? s.workspace : tabLabel(s);
      // Waiting on a question: the answers it offers on screen (read by the
      // service) and Esc to back out. Otherwise, at its prompt: next steps.
      const choices = a.state.status === 'needs-input' ? a.state.choices || [] : [];
      const button = (attrs, label, title) => `<button ${attrs} data-id="${esc(s.id)}" title="${esc(title)}">${label}</button>`;
      const replies = choices.length
        ? `<span class="replies">${choices.map((c) => button(`data-reply="${esc(c.key)}"`, `${/^\d$/.test(c.key) ? `<b>${esc(c.key)}</b>` : ''}<span>${esc(c.label)}</span>`, c.label)).join('')}${button('class="esc" data-reply="esc"', 'Esc', 'Cancel (Esc)')}</span>`
        : a.state.status !== 'working'
          ? `<span class="replies">${parseQuickReplies(this.settings.quickReplies).map((q) => button(`data-prompt="${esc(q.text)}"`, `<span>${esc(q.label)}</span>`, `Send “${q.text}”`)).join('')}</span>`
          : '';
      return `<div class="agent s-${esc(a.state.status)} ${s && s.id === act ? 'cur' : ''}" data-agent="${esc(a.key)}" role="button" tabindex="0"
          title="${esc(`${a.state.name} on ${a.host}: ${STATUS_TEXT[a.state.status] || a.state.status}${a.state.detail ? ' - ' + a.state.detail : ''}`)}">
        ${agentMark({ agent: a.state })}
        <span class="who"><b>${esc(a.state.name)}</b> ${esc(where)}${a.host !== this.hostName ? ` <span class="where">${esc(a.host)}</span>` : ''}</span>
        <span class="what">${esc(a.state.detail || STATUS_TEXT[a.state.status] || '')}</span>
        <span class="since">${esc(ago(a.state.since))}</span>
        ${replies}
      </div>`;
    }).join('');
  }

  renderStatus() {
    const s = this.activeSession();
    const el = $('#statusline');
    if (!s) {
      el.innerHTML = '';
      return;
    }
    const bits = [`<span class="host">${esc(s.host)}</span>`];
    if (s.kind === 'ssh' || s.kind === 'mosh') {
      bits.push(`<span class="tmux-on" title="${s.kind} to ${esc(s.remote || '?')}">${tag(s.kind.toUpperCase())}${esc(s.remote || '?')}</span>`);
      if (s.remoteTmux) bits.push(`<span class="tmux-on" title="tmux session ${esc(s.remoteTmux)} on ${esc(s.remote || 'the remote machine')}">${TMUX_ICON}${esc(s.remoteTmux)}</span>`);
      const known = this.hostForTarget(s.remote);
      if (known) bits.push(`<button class="sl-action" data-act="host-new" data-host="${esc(known.name)}" title="Gecko tabs on ${esc(known.name)} keep running when the connection drops">New Gecko tab on ${esc(known.name)}</button>`);
      else if (s.remote) bits.push(`<button class="sl-action" data-act="adopt-ssh" data-target="${esc(s.remote)}" title="Install Gecko there so tabs on that machine survive disconnects and show up here">Make it a Gecko machine</button>`);
    }
    if (s.tmuxSession) {
      bits.push(`<span class="tmux-on" title="tmux session ${esc(s.tmuxSession)} (double-click to rename)" data-act="tmux-rename">${TMUX_ICON}${esc(s.tmuxSession)}</span>`);
      const ts = this.tmux.find((t) => t.host === s.host && t.name === s.tmuxSession);
      if (ts?.wins?.length) {
        bits.push(`<span class="tmux-wins-bar">${ts.wins.map((w) => `<button class="${w.active ? 'on' : ''}" data-act="tmux-sel" data-idx="${w.index}" title="${esc(w.command || w.name)} (double-click to rename)">${w.index}:${esc(w.name)}</button>`).join('')}<button data-act="tmux-new" title="New tmux window" aria-label="New tmux window">+</button></span>`);
      }
    }
    // ssh/mosh/tmux tabs: the local folder and client command say nothing
    // useful (the remote shell or tmux pane has its own folder).
    const remoteTab = s.kind === 'ssh' || s.kind === 'mosh' || s.kind === 'tmux';
    if (remoteTab) {
      // nothing more
    } else if (s.repo) {
      const sub = repoPath(s);
      bits.push(`<span class="repo">${gitChip(s, true)}<button class="cwd pick" data-act="pick-cwd" title="${esc(s.cwd)} (click to change folder)">${esc(sub || shortPath(s.cwd))}</button></span>`);
    } else if (s.cwd) {
      bits.push(`<button class="cwd pick" data-act="pick-cwd" title="${esc(s.cwd)} (click to change folder)">${esc(shortPath(s.cwd))}</button>`);
    }
    if (s.command && s.kind !== 'shell' && !remoteTab) bits.push(`<span class="cmd" title="${esc(s.command)}">${esc(s.command)}</span>`);
    if (s.lastExit != null && s.lastExit !== 0 && !s.running) bits.push(`<span class="bad">exit ${s.lastExit}</span>`);
    const p = this.panes.get(s.id);
    const right = [];
    if (s.notice) right.push(`<span class="notice" title="${esc(s.notice)}">${esc(s.notice)}</span>`);
    if (p) right.push(`<span>${p.term.cols}×${p.term.rows}</span>`);
    right.push(`<span class="conn-dot ${this.conn.status}" title="Connection: ${this.conn.status}"></span>`);
    el.innerHTML = `<div class="l">${bits.join('')}</div><div class="r">${right.join('')}</div>`;
  }

  updateTitle() {
    const need = this.allAgents().filter((a) => a.state.status === 'needs-input').length;
    const s = this.activeSession();
    document.title = `${s ? tabLabel(s) + ' - ' : ''}Gecko`;
    const icon = document.querySelector('link[rel=icon]');
    const want = need ? '/icon-alert.svg' : '/icon.svg';
    if (icon && icon.getAttribute('href') !== want) icon.setAttribute('href', want);
  }
}

// Colors first, so the page never flashes the default theme.
if (window.geckoDesktop) document.documentElement.classList.add('desktop', 'desktop-' + window.geckoDesktop.platform);
if (window.GeckoAndroid) document.documentElement.classList.add('android'); // its own extra keys row
paintTheme(
  resolveTheme(initialTheme()),
  window.geckoDesktop?.transparent ? store('opacity', 0.9) : 1,
);

// xterm measures glyphs when a terminal opens, so wait for the font.
Promise.race([
  document.fonts.load('13px "JetBrains Mono Variable"'),
  new Promise((r) => setTimeout(r, 1500)),
]).finally(() => {
  splash.set(0.65); // font ready
  window.gecko = new App();
});

if ('serviceWorker' in navigator && window.isSecureContext) {
  navigator.serviceWorker.register('/sw.js').catch(() => {});
}
