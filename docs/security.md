# Security

[Docs](README.md)

- The web UI needs a random token (`~/.gecko-terminal/token`). Pass it once via
  `?token=`, after which it is kept as an HttpOnly, SameSite=Strict cookie.
- WebSocket upgrades check the Origin. The daemon socket is `0600` in a `0700`
  directory.
- Programs can *write* the clipboard with OSC 52, but can never read it.
- Exposing the port beyond loopback is a choice you make. Prefer Tailscale
  over opening it to the internet.

- Signing in to an account (`gecko login`) is optional. Machines in an
  account talk through a relay with end-to-end encryption, and each one only
  accepts machines on its own copy of the signed device list. Keys live in
  `~/.gecko-terminal/cloud/` (`0600`). Details and the threat model are in
  [Accounts and the relay](cloud.md).
