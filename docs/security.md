# Security

[Docs](README.md)

- The web UI needs a random token (`~/.gecko-terminal/token`). Pass it once via
  `?token=`, after which it is kept as an HttpOnly, SameSite=Strict cookie.
- WebSocket upgrades check the Origin. The daemon socket is `0600` in a `0700`
  directory.
- WebTransport (QUIC, UDP on the web UI's port and address) needs a one-time
  ticket the page gets from `/api/wt` with its cookie, or the token itself.
  Its certificate is self-signed and short-lived; the page pins it by hash.
  See [Connection and transport](architecture.md#connection-and-transport).
- Programs can *write* the clipboard with OSC 52, but can never read it.
- Exposing the port beyond loopback is a choice you make. Prefer Tailscale
  over opening it to the internet.

