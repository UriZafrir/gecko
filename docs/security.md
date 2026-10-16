# Security

[Docs](README.md)

- The web UI needs a random token (`~/.gecko-terminal/token`). Pass it once via
  `?token=`, after which it is kept as an HttpOnly, SameSite=Strict cookie.
- WebSocket upgrades check the Origin. The daemon socket is `0600` in a `0700`
  directory.
- Programs can *write* the clipboard with OSC 52, but can never read it.
- Exposing the port beyond loopback is a choice you make. Prefer Tailscale
  over opening it to the internet.


## Android app

The [Android app](android.md) keeps each machine's token encrypted with a key
in the Android Keystore and leaves app data out of backups. It sends a token
over plain http only to private, loopback and Tailscale addresses (or names
that resolve only to those), and needs https anywhere else. Its web view
shows only the machine's own origin (other links open in the browser), and
its JavaScript bridge (clipboard, notifications, keys) answers only pages
from that origin.

Gecko on the phone itself listens on 127.0.0.1 only, with its token in the
app's private storage. **Allow other devices** (and `gecko share` on any
machine) opens a second listener with a token of its own, made new every
time it is turned on and invalid once it is off; it answers only private
and Tailscale addresses, and turning it off disconnects its clients. Only
the daemon's own socket (the app, the CLI) can turn it on, not the web UI or
another Gecko.
