# Android app

[Docs](README.md)

The Android app is Gecko's own web UI in a native shell, with the things a
browser tab on a phone can't do: a row of terminal keys above the keyboard,
notifications that arrive while the app is closed, the real clipboard, and
"share to Gecko". It connects to the Gecko daemon on your computer, the same
way [the phone browser](mobile.md) does. Android 8.0 (API 26) or newer.

## Install

There is no store listing yet. Build a debug APK and install it:

```sh
make android          # needs JDK 17 and the Android SDK, see below
adb install android/app/build/outputs/apk/debug/app-debug.apk
```

`make android` runs `./gradlew assembleDebug` in `android/` (Gradle Kotlin
DSL, Android Gradle Plugin 8.13, Kotlin 2.2, compile and target SDK 35). The
build needs `JAVA_HOME` pointing at a JDK 17 and `ANDROID_HOME` at an SDK with
`platforms;android-35` and `build-tools;35.0.0`; `sdkmanager` from Google's
command-line tools installs both. `make android-test` runs the unit tests,
`cd android && ./gradlew connectedDebugAndroidTest` the device tests (add
`-Pandroid.testInstrumentationRunnerArguments.geckoUrl=<link>` to include the
part that talks to a live Gecko).

## Connect

The phone has to reach the computer: the same Wi-Fi, or
[Tailscale](https://tailscale.com). Gecko listens only on `127.0.0.1` by
default, so start it on your network first (see [Phone and tablet](mobile.md)):

```sh
gecko stop && gecko daemon --listen 0.0.0.0:7681    # or "listen" in the config
```

Then on the computer open the palette (Ctrl+K), pick **Open on another
device**, and in the app either:

- tap **Scan QR code** and point the camera at it, or
- copy the link (`gecko url` prints it too) and tap **Paste link**, or
- take a screenshot of the QR code and use **Scan QR code from a picture** in
  the menu.

The app checks the link with the daemon, names the machine after it, and
opens it. It remembers several machines: tap one to open it, or use its **⋮**
menu to rename it, paste a new link (a new token), or remove it. Scanning a
machine you already have just updates its token.

The token is stored encrypted with a key kept in the Android Keystore, which
never leaves the phone. Android backups don't include the app's data, since
that key can't be restored on another phone; after a restore, scan the QR code
again.

### http, https and your network

Gecko's link carries its access token, so the app sends it over plain http
only inside a private network: this phone, LAN addresses (10/8, 172.16/12,
192.168/16, link-local), Tailscale (100.64/10, `fd7a:115c:a1e0::/48`), IPv6
unique local addresses, single-label names and `.local`, `.lan`,
`.home.arpa`, `.internal` and `.ts.net` names, or any name that resolves only
to such addresses. Anything else needs https, for example `tailscale serve
7681`. Android's network security config can only list names and single
addresses, not ranges, so the app allows cleartext there and enforces this
rule itself before it connects (`NetPolicy.kt`).

## Using it

- **The full Gecko UI**, edge to edge: workspaces in the drawer, the agent
  dock, the palette, every theme. The system bars take the theme's colors.
- **The keyboard never covers the terminal.** The page shrinks above the
  keyboard and the terminal refits.
- **Extra keys** above the keyboard: `esc`, `tab`, sticky `ctrl` and `alt`
  (tap once, then type: ctrl, then `c` on the keyboard is Ctrl+C), arrows
  that repeat while held, `|` `~` `/` `-`, `^C`, home, end, page up and down,
  **paste**, **select** (the selectable text view) and **compose** (a text box
  with autocorrect for prompts). Scroll the row sideways for the rest. **⋮**
  at its end has search, **Keep screen on**, **Notify in background**, reload,
  and the machine list.
- **Back** closes what is open, innermost first: a menu, a dialog, the
  palette, the text view, find, the drawer, compose. With nothing open it goes
  back to the machine list.
- **Clipboard.** Copies go to the Android clipboard, including what programs
  copy with OSC 52 (tmux, nvim, Claude Code), even over plain http where a
  browser would refuse. **paste** pastes the Android clipboard into the tab.
- **Share to Gecko.** Share text from any app and pick **Paste in Gecko**: it
  is pasted (not run) in the tab you have open, on the machine you used last.
- **Keep screen on**, in the **⋮** menu.

## Notifications

When an agent in any tab stops for you, or finishes, you get a notification
with what it asks. Tap it to open that tab. If the agent shows choices (a
numbered menu, y/n), the notification has buttons for up to three of them, so
you can answer without opening the app.

Android 13 and newer ask for permission the first time (from **Notify me when
agents need input** in the palette, or **Notify in background**).

Android stops the network of apps that aren't on screen (Android 15 cuts it
off as soon as the app is in the background). So notifications while the app
is closed need **Notify in background** (in the **⋮** menu of the extra keys
or of the machine list): a foreground service keeps one small connection to
each saved machine, which reads only the list of tabs and their agents' state,
and shows a silent "Gecko is watching your agents" notification with a
**Stop** button. Without it you are notified only while Gecko is open, and
see what you missed in the agent dock when you come back.

## Limits

- Not on the Play Store, and only debug builds are made: there is no release
  signing yet.
- The Android app is the web UI, served by your Gecko: it needs a Gecko built
  from the same version or newer for the native keys, clipboard, back button
  and theme colors. With an older Gecko it still works, but the web key bar
  shows next to the native one and copying uses the browser's clipboard.
- Notifications in the background need the foreground service, and some
  phone makers stop even those unless Gecko is exempt from battery
  optimisation.
- The device tests run on the Android emulator (Pixel 8, Android 15); it has
  not been tried on many real phones yet. The camera QR scan was tested with
  the emulator's camera showing a picture of the code.
