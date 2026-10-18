// Connection to the Gecko daemon, carrying JSON control messages and binary
// terminal frames. Two transports, same protocol on top:
//   - WebTransport (HTTP/3 over QUIC, so UDP) when the browser can use it:
//     one bidirectional stream, framed like the daemon's local socket
//     (u32 length | u8 type | payload);
//   - a WebSocket otherwise, or when QUIC doesn't get through.
// A ping every 2 s measures the round trip and notices a connection that
// stopped answering; then it is dropped and redialed right away, quickly
// and repeatedly while the network is down. Panes resume their streams
// from the last byte they saw.

const KIND_OUTPUT = 1;
const KIND_INPUT = 2;
const enc = new TextEncoder();
const dec = new TextDecoder();

const PING_EVERY = 2000;
const STALE_AFTER = 3000; // nothing received for this long while a ping waits
const PROBE_WAIT = 1500; // after a network change, a ping must answer this fast
const OPEN_TIMEOUT = 3000; // a transport that hasn't opened by then won't
const RETRY = [0, 250, 500, 1000]; // then every RETRY_MAX while it's down
const RETRY_MAX = 1500;

const timeout = (ms, what) => new Promise((_, rej) => setTimeout(() => rej(new Error(what)), ms));

// WebTransport needs the API (not in older Safari), a secure context
// (https, or http on localhost / 127.0.0.1; a LAN address over plain
// http is not one) and certificate pinning by hash.
export function webTransportUsable() {
  try {
    if (localStorage.getItem('gecko.transport') === 'ws') return false;
  } catch { /* storage blocked */ }
  return typeof WebTransport === 'function' && window.isSecureContext;
}

class WSTransport {
  constructor(t) { this.t = t; this.kind = 'ws'; }

  open() {
    return new Promise((resolve, reject) => {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const ws = new WebSocket(`${proto}//${location.host}/ws`);
      ws.binaryType = 'arraybuffer';
      this.ws = ws;
      let opened = false;
      const timer = setTimeout(() => { if (!opened) { ws.close(); reject(new Error('timeout')); } }, OPEN_TIMEOUT);
      ws.onopen = () => { opened = true; clearTimeout(timer); resolve(); };
      ws.onmessage = (ev) => {
        if (typeof ev.data === 'string') this.t.onText(ev.data);
        else this.t.onBinary(new Uint8Array(ev.data));
      };
      ws.onclose = (ev) => {
        clearTimeout(timer);
        const unauthorized = ev.code === 1008 || ev.code === 4001;
        if (!opened) reject(Object.assign(new Error('closed'), { unauthorized }));
        else this.t.onClose(unauthorized);
      };
    });
  }

  sendText(s) { this.ws.send(s); }
  sendBinary(b) { this.ws.send(b); }
  close() {
    this.ws.onclose = this.ws.onmessage = null;
    try { this.ws.close(); } catch { /* already gone */ }
  }
}

class WTTransport {
  constructor(t) { this.t = t; this.kind = 'quic'; this.buf = new Uint8Array(64 << 10); this.len = 0; }

  async open() {
    const ac = new AbortController();
    const at = setTimeout(() => ac.abort(), OPEN_TIMEOUT);
    let info;
    try {
      const r = await fetch('/api/wt', { cache: 'no-store', credentials: 'same-origin', signal: ac.signal });
      if (r.status === 401) throw Object.assign(new Error('unauthorized'), { unauthorized: true });
      // 404: this service has no WebTransport (turned off, or too old).
      // 503: it is restarting; use the WebSocket this time.
      if (r.status === 404) throw Object.assign(new Error('no webtransport'), { unsupported: true });
      if (!r.ok) throw Object.assign(new Error('webtransport unavailable'), { skip: true });
      info = await r.json();
    } catch (e) {
      // The HTTP side didn't answer: the network (or daemon) is down, so
      // there is no point trying the WebSocket either.
      if (!e.unauthorized && !e.unsupported && !e.skip) e.down = true;
      throw e;
    } finally {
      clearTimeout(at);
    }
    const hash = Uint8Array.from(atob(info.hash), (c) => c.charCodeAt(0));
    const host = info.host || location.hostname;
    const wt = new WebTransport(`https://${host}:${info.port}/wt?ticket=${encodeURIComponent(info.ticket)}`, {
      serverCertificateHashes: [{ algorithm: 'sha-256', value: hash }],
    });
    this.wt = wt;
    wt.closed.then(() => this.gone(), () => this.gone());
    try {
      await Promise.race([wt.ready, timeout(OPEN_TIMEOUT, 'timeout')]);
      const s = await Promise.race([wt.createBidirectionalStream(), timeout(OPEN_TIMEOUT, 'timeout')]);
      this.w = s.writable.getWriter();
      this.readLoop(s.readable.getReader());
    } catch (e) {
      this.close();
      throw e;
    }
  }

  gone() {
    if (this.notified) return;
    this.notified = true;
    this.t.onClose(false);
  }

  async readLoop(rd) {
    try {
      for (;;) {
        const { value, done } = await rd.read();
        if (done) break;
        this.feed(value);
      }
    } catch { /* stream reset */ }
    this.close();
    this.gone();
  }

  feed(chunk) {
    if (this.len + chunk.length > this.buf.length) {
      const nb = new Uint8Array(Math.max(this.buf.length * 2, this.len + chunk.length));
      nb.set(this.buf.subarray(0, this.len));
      this.buf = nb;
    }
    this.buf.set(chunk, this.len);
    this.len += chunk.length;
    const dv = new DataView(this.buf.buffer);
    let p = 0;
    while (this.len - p >= 5) {
      const n = dv.getUint32(p);
      if (this.len - p - 5 < n) break;
      const body = this.buf.slice(p + 5, p + 5 + n); // a copy: buf is reused
      if (this.buf[p + 4] === 1) this.t.onBinary(body);
      else this.t.onText(dec.decode(body));
      p += 5 + n;
    }
    if (p) {
      this.buf.copyWithin(0, p, this.len);
      this.len -= p;
    }
  }

  write(type, body) {
    const f = new Uint8Array(5 + body.length);
    new DataView(f.buffer).setUint32(0, body.length);
    f[4] = type;
    f.set(body, 5);
    // The writer queues in order; a failed write means the session died.
    this.w.write(f).catch(() => { this.close(); this.gone(); });
  }

  sendText(s) { this.write(0, enc.encode(s)); }
  sendBinary(b) { this.write(1, b); }
  close() {
    try { this.wt?.close(); } catch { /* already closed */ }
  }
}

export class Conn {
  constructor(handlers) {
    this.h = handlers;
    this.tr = null; // the open transport
    this.rid = 0;
    this.pending = new Map();
    this.status = 'connecting'; // connecting | online | offline (reconnecting) | unauthorized
    this.transport = ''; // 'quic' | 'ws' while online
    this.rtt = null; // ms, last ping round trip
    this.timer = null;
    this.attempt = 0;
    this.tries = 0;
    this.everOnline = false;
    this.wtOK = false; // WebTransport has worked in this page
    this.wtFails = 0;
    this.wtOff = !webTransportUsable();
    this.ping = null; // { rid, at } outstanding
    this.lastRx = 0;
    window.addEventListener('online', () => this.kick());
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) this.kick();
    });
    navigator.connection?.addEventListener?.('change', () => this.kick());
    setInterval(() => this.tick(), 500);
  }

  async connect() {
    clearTimeout(this.timer);
    this.timer = null;
    const my = ++this.attempt;
    if (!this.everOnline) this.setStatus('connecting');
    let tr = null;
    let err = null;
    if (!this.wtOff) {
      tr = this.make(WTTransport);
      try {
        await tr.open();
        this.wtOK = true;
        this.wtFails = 0;
      } catch (e) {
        tr = null;
        err = e;
        if (e.unsupported) this.wtOff = true;
        else if (!e.down && !e.unauthorized && !e.skip && ++this.wtFails >= 2 && !this.wtOK) {
          // The page is fine over HTTP but QUIC never gets through (UDP
          // blocked, a proxy in between): stay on the WebSocket.
          this.wtOff = true;
        }
      }
    }
    if (my !== this.attempt) { tr?.close(); return; }
    if (!tr && !err?.down && !err?.unauthorized) {
      tr = this.make(WSTransport);
      try {
        await tr.open();
        err = null;
      } catch (e) {
        tr = null;
        err = e;
      }
    }
    if (my !== this.attempt) { tr?.close(); return; }
    if (!tr) {
      this.setStatus(err?.unauthorized ? 'unauthorized' : this.everOnline ? 'offline' : 'connecting');
      this.retry(err?.unauthorized);
      return;
    }
    this.adopt(tr);
  }

  // Each transport reports only while it is the current one.
  make(Kind) {
    const t = new Kind({
      onText: (s) => { if (this.tr === t) { this.lastRx = performance.now(); this.onJSON(JSON.parse(s)); } },
      onBinary: (b) => { if (this.tr === t) { this.lastRx = performance.now(); this.onBinary(b); } },
      onClose: (unauth) => { if (this.tr === t) this.lost(unauth ? 'unauthorized' : 'offline'); },
    });
    return t;
  }

  adopt(tr) {
    this.tr = tr;
    this.transport = tr.kind;
    this.since = performance.now();
    this.tries = 0;
    this.everOnline = true;
    this.lastRx = performance.now();
    this.ping = null;
    // The page's chosen anchor goes in every hello (see anchor.js).
    this.send({ t: 'hello', client: 'web', ...(this.h.hello?.() || {}) });
    this.sendPing();
    this.setStatus('online');
    this.h.onOpen?.(); // panes re-attach from the last byte they have
  }

  // On the WebSocket only because QUIC failed once (a restart, a hiccup)
  // although it has worked in this page: try to move back to QUIC now and
  // then. The swap is a reconnect, so panes resume where they were.
  async upgrade() {
    if (this.upgrading || this.wtOff || !this.wtOK || this.transport !== 'ws' || this.pending.size) return;
    this.upgrading = true;
    this.since = performance.now();
    const tr = this.make(WTTransport);
    try {
      await tr.open();
      if (this.transport === 'ws' && !this.pending.size) {
        const old = this.tr;
        this.tr = null;
        old?.close();
        this.adopt(tr);
      } else tr.close();
    } catch {
      tr.close();
    } finally {
      this.upgrading = false;
    }
  }

  retry(slow) {
    clearTimeout(this.timer);
    const ms = slow ? 5000 : RETRY[this.tries] ?? RETRY_MAX;
    this.tries++;
    this.timer = setTimeout(() => this.connect(), ms);
  }

  // The connection is gone (closed, or stopped answering): reconnect now.
  lost(status) {
    const tr = this.tr;
    this.tr = null;
    this.transport = '';
    this.ping = null;
    tr?.close();
    for (const [, p] of this.pending) p.reject(new Error('disconnected'));
    this.pending.clear();
    this.setStatus(status);
    this.tries = 0;
    this.retry(status === 'unauthorized');
  }

  sendPing(probe) {
    if (!this.tr) return;
    const rid = ++this.rid;
    this.ping = { rid, at: performance.now(), probe };
    this.send({ t: 'ping', rid });
  }

  tick() {
    if (!this.tr) return;
    const now = performance.now();
    const p = this.ping;
    if (p) {
      // Stale: nothing at all received for STALE_AFTER while a ping waits
      // (pongs normally come every 2 s, so that is ~3 s after the line
      // went quiet). Output still arriving proves the connection works,
      // even if the pong is queued behind it.
      const quiet = now - this.lastRx;
      if (p.probe ? now - p.at > PROBE_WAIT && quiet > PROBE_WAIT : now - p.at > 1000 && quiet > STALE_AFTER) {
        this.rtt = null;
        this.lost('offline');
      }
      return;
    }
    if (now - (this.lastPing || 0) >= PING_EVERY - 50) this.sendPing();
    if (now - this.since > 30000) this.upgrade();
  }

  // Network came back, changed, or the tab became visible: check the
  // connection right away, or reconnect now if there is none.
  kick() {
    if (this.tr) {
      if (!this.ping) this.sendPing(true);
      else this.ping.probe = true;
      return;
    }
    if (this.status === 'unauthorized') return;
    this.tries = 0;
    clearTimeout(this.timer);
    this.timer = null;
    this.connect();
  }

  setStatus(s) {
    this.status = s;
    this.h.onStatus?.(s);
  }

  send(msg) {
    this.tr?.sendText(JSON.stringify(msg));
  }

  request(msg) {
    return new Promise((resolve, reject) => {
      if (!this.tr) return reject(new Error('Not connected to the Gecko daemon'));
      msg.rid = ++this.rid;
      this.pending.set(msg.rid, { resolve, reject });
      this.send(msg);
      setTimeout(() => {
        if (this.pending.delete(msg.rid)) reject(new Error('The daemon did not answer in time'));
      }, 30000);
    });
  }

  input(id, data) {
    if (!this.tr) return false;
    const idb = enc.encode(id);
    const body = typeof data === 'string' ? enc.encode(data) : data;
    const frame = new Uint8Array(2 + idb.length + 8 + body.length);
    frame[0] = KIND_INPUT;
    frame[1] = idb.length;
    frame.set(idb, 2);
    frame.set(body, 2 + idb.length + 8);
    this.tr.sendBinary(frame);
    return true;
  }

  onBinary(b) {
    if (b[0] !== KIND_OUTPUT) return;
    const n = b[1];
    const sub = dec.decode(b.subarray(2, 2 + n));
    const dv = new DataView(b.buffer, b.byteOffset + 2 + n, 8);
    const off = Number(dv.getBigUint64(0));
    this.h.onData?.(sub, off, b.subarray(2 + n + 8));
  }

  onJSON(m) {
    if (this.ping && m.rid === this.ping.rid) {
      // Any reply is a pong: services before protocol 8 answer "ping"
      // with an error.
      this.rtt = Math.max(1, Math.round(performance.now() - this.ping.at));
      this.lastPing = this.ping.at;
      this.ping = null;
      this.h.onRTT?.(this.rtt);
      return;
    }
    if (m.t === 'reply' || (m.rid && this.pending.has(m.rid))) {
      const p = this.pending.get(m.rid);
      if (!p) return;
      this.pending.delete(m.rid);
      if (m.error) {
        // Services from before protocol 2 answer unknown requests with a bare
        // "bad request"; say what that actually means.
        const msg = m.error === 'bad request'
          ? 'The Gecko background service is too old for this. Restart it to update: gecko stop && gecko'
          : m.error;
        const e = new Error(msg);
        e.reply = m; // what else it said (e.g. kind, for the Add machine dialog)
        p.reject(e);
      }
      else p.resolve(m);
      return;
    }
    this.h.onMessage?.(m);
  }
}
