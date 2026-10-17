# iOS app

[Docs](README.md)

The iOS app is Gecko's own web UI in a native shell for iPhone and iPad, the
counterpart of the [Android app](android.md): a row of terminal keys above
the keyboard, the real clipboard, the share sheet, and notifications when an
agent needs you. It connects to the Gecko daemon on your computer, the same
way [the phone browser](mobile.md) does. iOS 16 or newer.

For now it is built for the iOS Simulator only (no certificate, no App
Store or TestFlight yet), and checked there by CI on a matrix of iPhones and
iPads.

## Build

On a Mac with Xcode 16 and [XcodeGen](https://github.com/yonaskolb/XcodeGen):

```sh
make ios          # web UI, then ios/Gecko.xcodeproj from ios/project.yml, then the app
make ios-test     # the unit tests on a simulator (IOS_SIM="iPhone SE (3rd generation)")
```

`make ios-web` (any OS) puts the web UI into the app as one page
(`ios/scripts/bundle-web.mjs`: the script, styles and fonts inlined, the
theme photos as files) with the build id. The Xcode project is not checked
in; `ios/project.yml` is the project. Builds are signed ad hoc ("Sign to
Run Locally", no certificate or team): the Keychain refuses an app without
entitlements even on the Simulator (`CODE_SIGNING_ALLOWED=NO` builds run,
but can't keep tokens). CI (`.github/workflows/ios.yml`, on
GitHub's macOS runners) builds the app (ad hoc, and unsigned), runs the unit tests, then
the UI tests on iPhone SE (3rd generation), iPhone 16, iPhone 16 Pro Max,
iPad mini, iPad Air 11-inch and iPad Pro 13-inch against Gecko daemons it
starts in the job (this version, and an older one from `main`), and keeps
the app, videos, screenshots, a contact sheet per device and the test
results as artifacts.

## Connect

As with Android: start Gecko on your network (`gecko daemon --listen
0.0.0.0:7681`, see [Phone and tablet](mobile.md)), open **Open on another
device** in the palette on the computer, then in the app:

- **Scan QR code** with the camera,
- **Paste** (the system paste button, so iOS doesn't ask about pasting), or
  **Type** the link `gecko url` prints,
- **Picture**: a screenshot of the QR code from Photos.

The app checks the link with the daemon's `/api/info`, names the machine
after it, and opens it. It remembers machines: tap one to open it, or touch
and hold it to rename it, paste a new link (a new token) or remove it.
Scanning a machine you already have just updates its token.

Tokens are kept in the iOS Keychain (this device only, readable after the
first unlock, never in backups or a transfer to a new phone). The token is
sent over plain http only inside a private network, the same rule as on
Android (`NetPolicy.swift`): LAN and link-local addresses, Tailscale,
single-label and `.local`, `.lan`, `.home.arpa`, `.internal`, `.ts.net`
names, or a name that resolves only to such addresses. Anywhere else needs
https (`tailscale serve 7681`).

## Using it

- **The full Gecko UI**, between the notch or Dynamic Island and the home
  indicator: workspaces, the agent dock, the palette, every theme. Phones in
  portrait (and windows narrower than 720 points, such as iPad Split View
  and Slide Over) get the drawer; iPads and large phones in landscape keep
  the sidebar beside the terminal. Rotating keeps the session and the tab.
- **The keyboard never covers the terminal**: the page ends above it and the
  terminal refits.
- **Extra keys** right above the keyboard, and at the bottom while it is
  down: `esc`, `tab`, sticky `ctrl` and `alt`, arrows that repeat while
  held, `|` `~` `/` `-`, `^C`, home, end, page up and down, **paste**,
  **select** and **compose**. Scroll the row sideways for the rest. Then a
  key to hide the keyboard, and **⋯**: search and commands, share screen
  text, keep screen on, notify in background, reload, touch diagnostics,
  machines, and the build id. The keys get larger with larger text sizes.
- **Touch gestures** are the web UI's (see the [Android app](android.md)):
  pixel scrolling, flicks that coast, the stretch at the ends, the
  fast-scroll thumb, pinch for the font size, swipes on the tab bar or with
  two fingers for tabs, the drawer from the left edge, one finger sideways
  for the cursor.
- **iPad with a keyboard**: keys and shortcuts go to Gecko as on a Mac
  (Cmd shortcuts included, except the ones iPadOS keeps for itself, like
  Cmd+Tab and Cmd+Space). Cmd+. is Escape, for keyboards without one. The
  pointer and trackpad work as a mouse: select, click links, scroll.
- **Clipboard**: copies go to the iOS clipboard, including OSC 52 from
  tmux, nvim or Claude Code, even over plain http. **paste** pastes it (iOS
  may ask once whether Gecko may paste).
- **Share**: **Share screen text** in the menu, and **Share** in the text
  view (select), send the text through the share sheet.
- **The app's own web UI on every machine**: the app brings Gecko's web UI
  and shows it for every machine, so the phone features work even where the
  machine runs an older Gecko (protocol 6 and newer were tried: its tabs
  work, newer features wait for an update there). See [Security](security.md).
- **Build id**: the same format as the Android app, e.g. `a1b2c3d · Oct 17
  02:15` (the commit, `-dirty` for uncommitted changes, and when it was
  built, in the phone's time), under the machine list and at the bottom of
  the **⋯** menu; tap to copy. Touch diagnostics shows it, the web UI's and
  the machine's.

## Notifications

When an agent in any tab needs you, or finishes, you get a notification
with what it asks; tap it to open that tab. If the agent shows choices (a
numbered menu, y/n), touch and hold the notification for buttons with up to
three of them, which answer without opening the app. iOS asks for
permission the first time a machine opens (or from **Notify me when agents
need input** in the palette).

What iOS allows, honestly:

- **While Gecko is open**: always, as a banner, for tabs that aren't on
  screen.
- **Right after you leave it**: the app keeps one small connection to each
  saved machine (it reads only the tab list and the agents' state) for as
  long as iOS lets an app finish its work, usually about 30 seconds.
- **After that**: iOS suspends the app. With **Notify in background** on
  (the default), it asks iOS for a background refresh, which runs when iOS
  decides (at best every 15 minutes or so, less often on battery or when the
  app is rarely used); each one looks at every machine once and notifies
  about agents that changed since. This is not real time.
- Real-time notifications while the app is closed need a push server (APNs)
  that the daemon or a relay talks to. That isn't built yet.

## Limits

- Simulator only for now: signed only ad hoc, not on the App Store or TestFlight.
  Runs on iPhone and iPad simulators (iOS 18 in CI); not tried on a real
  device yet. The camera scan can't be tried on the Simulator (it has no
  camera); reading a QR code from a picture is tested.
- No terminal on the phone itself: iOS doesn't let an app start a shell or
  other programs, so there is no **This phone** as on Android, and no
  `ssh` from the phone. Gecko on your computer reaches other machines over
  ssh as usual (see [More machines](machines.md)). An ssh client inside the
  app (Gecko's Go ssh through gomobile) would be possible but isn't built.
- No "share to Gecko" from other apps yet: that needs a share extension and
  an app group, so a signed build.
- Split View and Slide Over resize the page like any window; the CI walk
  tries Split View on the iPads, where the simulator allows it. Stage Manager
  wasn't tried.
- Dictation and autocorrect go through WebKit and xterm.js; the Android
  app's word-by-word edit handling (`ime.js`) is Android only.
