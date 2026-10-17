# Architecture

[Docs](README.md)

```
 browser / PWA (xterm.js, WebGL)        gecko attach (any terminal)
        │  WebTransport (QUIC/UDP)               │ unix socket
        │  or WebSocket (TCP)                    │
        └───────────────┬────────────────────────┘
                 gecko daemon  (this machine)
        ┌───────────────┼──────────────────────────────┐
   local PTYs     hub ── ssh host gecko bridge ──► gecko daemon (devbox)
   + headless VT        ├ ssh … gecko bridge ────► gecko daemon (gpu-box)
   + ring buffer        └ relay + Noise ─────────► gecko daemon (vm, signed in)
   + process / tmux / agent inspection (of its own sessions)
```

- **One protocol everywhere.** JSON control messages and binary data frames
  (`kind | id | offset | bytes`) run over WebTransport or WebSockets, the
  local socket, and SSH stdio. A remote daemon looks exactly like the local
  one.
- **The relay is one more transport** for the same protocol: a signed-in
  daemon dials out to it, and the protocol runs inside a Noise session the
  relay can't read ([details](cloud.md)).
- **Every session has a 4 MiB ring buffer** with absolute byte offsets.
  Attaching replays it, and reattaching sends only the missing bytes. If a
  client fell too far behind, it gets a reset plus the terminal modes it needs
  (alt screen, mouse, bracketed paste).
- **A headless VT emulator** (`internal/vt`) tracks what is on screen. That
  drives agent status, the text view, and mode restoration. It also handles
  OSC 0/2/7/9/52/133/633/777/1337 and bells.
- **Slow clients never stall a session.** Each one gets a bounded queue. A
  client that can't keep up is disconnected and resumes from its offset.

## Connection and transport

Browsers and the desktop app reach the daemon in one of two ways, with the
same protocol on top:

- **WebTransport** (HTTP/3 over QUIC, so UDP) on the same address and port
  number as the web UI, UDP instead of TCP. The session has one
  bidirectional stream, framed like the local socket
  (`u32 length | u8 type | payload`, type 0 JSON, 1 data frame). Terminal
  output must arrive complete and in order, so datagrams are not used: a
  lost packet is resent by QUIC for that stream only, without a TCP
  connection's long stalls, and a dead network is noticed quickly.
- **WebSocket** (TCP), as before. It is the fallback.

The page picks WebTransport when the browser has it and the page is a
secure context: `http://localhost`, `http://127.0.0.1` and `https://` count,
a LAN address over plain `http://` does not, and there the API doesn't even
exist. Older Safari has no WebTransport at all. The page then fetches
`/api/wt` (with its cookie) for the certificate hash and a one-time ticket,
and opens `https://host:port/wt?ticket=...`. If that fails twice while plain
HTTP works (UDP blocked, a proxy in between), it stays on the WebSocket for
the rest of the page's life. If QUIC has worked before and only failed once
(the service restarting, say), it uses the WebSocket for now and moves back
to QUIC after 30 seconds. `localStorage['gecko.transport'] = 'ws'` forces
the WebSocket; `GECKO_WEBTRANSPORT=off` turns QUIC off in the daemon.

**Certificates.** A daemon on localhost can't have a certificate from a
public CA, and browsers accept a self-signed one for WebTransport only
through `serverCertificateHashes`, which requires ECDSA and at most 14 days
of validity. The daemon makes an ECDSA P-256 certificate in memory, valid
for 13 days (from an hour ago, for clock skew), replaces it after 6, and
serves its SHA-256 hash through `/api/wt`. Sessions already open keep
working across a rotation; new ones fetch the new hash.

**Auth.** WebTransport requests carry no cookies, and the token cookie is
HttpOnly, so the page trades its cookie for a ticket on `/api/wt` (single
use, valid 30 seconds). Non-browser clients can pass the token itself
(`?token=` or `Authorization: Bearer`). The Origin must name the same host
as the request (ports may differ; `localhost` and `127.0.0.1` match each
other), and the bind address is the web UI's, loopback only by default.

**Liveness.** The client sends `{"t":"ping","rid":N}` every 2 seconds and the
daemon replies right away (services before protocol 8 answer with an error
reply, which counts just the same). That gives the round trip in the status
line. If nothing at all has arrived for 3 seconds while a ping waits, the
connection is stale: the client closes it and reconnects at once, then every
0.25 to 1.5 seconds while the network is down, and immediately on the
`online`, `visibilitychange` and network `change` events (where a ping must
then answer within 1.5 seconds). Every attempt has a 3 second limit, so a
half-open TCP or QUIC connection can't hang the UI. The daemon drops a
WebSocket that sent nothing for 60 seconds, and a QUIC session after 15
seconds without packets. Panes resume from the last byte they saw.

**Connection migration.** QUIC can keep a session alive when the client's
address changes (Wi-Fi to Ethernet, a new DHCP lease). The quic-go release
Gecko can use with its Go version (1.22) does not support migration yet, so
an address change is handled by the fast reconnect above instead, which
takes about as long as the ping timeout. Newer quic-go (Go 1.24+) supports
it.

**Between machines** the transport is unchanged: the hub reaches other
machines with `gecko bridge` over SSH (TCP), or through the relay for
machines in the same account ([details](cloud.md)), and resumes every
stream after a drop. The hub pings each link the same way every 2 seconds, shows the round trip on
the machine in the sidebar, and redials a link that hasn't answered for 15
seconds. A mosh-style UDP link between machines is a possible next step.

Code map: `cmd/gecko` (CLI), `internal/session` (PTYs, inspection, agent
status), `internal/hub` (federation, remote reconnect), `internal/server`
(protocol, HTTP/WebSocket, WebTransport), `internal/vt`, `internal/inspect`,
`internal/shellint`, `internal/tmux`, `internal/cloud` (accounts, keys,
Noise links), `internal/relay` (`gecko relay`), `web/` (client).

