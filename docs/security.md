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

The app shows its own copy of Gecko's web UI (shipped in the APK) for every
machine, so the phone features don't depend on the Gecko version there. It
does this without a new origin: the page still loads from the machine's
address, and the app answers only requests for the UI's own files (the page,
scripts, styles, fonts, images) from the APK, while `/ws`, `/api/` and
anything else go to the machine as before. So the WebSocket Origin check is
unchanged and no other origin is ever allowed. The token goes in as the
daemon's own HttpOnly, SameSite=Strict cookie (set by the app, not by a
`?token=` link), after the app checks it against `/api/info`; it is never
in the page's address.

Gecko on the phone itself listens on 127.0.0.1 only, with its token in the
app's private storage. **Allow other devices** (and `gecko share` on any
machine) opens a second listener with a token of its own, made new every
time it is turned on and invalid once it is off; it answers only private
and Tailscale addresses, and turning it off disconnects its clients. Only
the daemon's own socket (the app, the CLI) can turn it on, not the web UI or
another Gecko.


## iOS app

The [iOS app](ios.md) keeps each machine's token in the iOS Keychain (this
device only, not in backups). Like the Android app it sends a token over
plain http only to private, loopback and Tailscale addresses (or names that
resolve only to those), and needs https anywhere else: App Transport
Security allows http (it can't express address ranges), and the app checks
every machine before it connects (`NetPolicy.swift`).

It shows its own copy of Gecko's web UI for every machine, also without a
new origin and without anything new in the daemon. WKWebView can't answer
http(s) requests itself, so the app loads its page with `loadHTMLString`
and the machine's address as the base URL: the page has the machine's
origin, its WebSocket sends that Origin, and the daemon's same-origin check
applies unchanged. The page carries its script, styles and fonts inside it;
theme photos come from the app's own `gecko-asset:` scheme, which serves
only files of the bundled UI. The token goes in as the daemon's HttpOnly,
SameSite=Strict cookie (set by the app after it checks the token against
`/api/info`), never in the page's address. Reloads and links to the page
load the app's page again; other links open in Safari. The bridge
(`window.GeckoIOS`: clipboard, notifications, keys, share) answers only the
main frame of the machine's origin. The app's own connections (agent
notifications, answers from a notification) authenticate with the token as
a Bearer header and send no Origin, as any non-browser client.
