# Android app

[Docs](README.md)

The Android app is Gecko's own web UI in a native shell, with the things a
browser tab on a phone can't do: a row of terminal keys above the keyboard,
notifications that arrive while the app is closed, the real clipboard, and
"share to Gecko". It connects to the Gecko daemon on your computer, the same
way [the phone browser](mobile.md) does, and it carries Gecko itself too, so
**This phone** gives you a terminal on the phone, which your computer can
open as well. Android 8.0 (API 26) or newer, 64-bit (arm64 or x86_64).

## Install

There is no store listing yet. Build a debug APK and install it:

```sh
make android          # needs JDK 17 and the Android SDK, see below
adb install android/app/build/outputs/apk/debug/app-debug.apk
```

`make android` first builds the web UI and cross-compiles Gecko for the
phone (`make android-gecko`: a static `GOOS=linux` build for arm64 and
x86_64, put in `android/app/src/main/jniLibs/<abi>/libgecko.so`), then runs
`./gradlew assembleDebug` in `android/` (Gradle Kotlin DSL, Android Gradle
Plugin 8.13, Kotlin 2.2, compile and target SDK 35). It needs Go and Node
like the rest of Gecko, `JAVA_HOME` pointing at a JDK 17, and `ANDROID_HOME`
at an SDK with `platforms;android-35` and `build-tools;35.0.0`; `sdkmanager`
from Google's command-line tools installs both. `make android-test` runs the unit tests,
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
- **Touch gestures** (in the Android app and any phone browser): one finger
  up or down scrolls the scrollback by the pixel, the text staying under the
  finger. A flick coasts and slows like a web page in Chrome (a fast flick
  goes much further than a slow one, and flicks in a row add up). Past the top
  or bottom the terminal stretches, and the scrollbar squeezes, then both
  spring back; a fling that runs into the end bounces the same way. A
  fast-scroll thumb shows on the right while scrolling (or when you touch near
  the right edge): drag it to jump through the scrollback, or tap the strip to
  jump there. In programs that use the mouse or the full screen (tmux, vim,
  less, Claude Code) a drag or flick scrolls them the way a mouse wheel does,
  line by line, and a flick sends its wheel steps over time on the same
  slowing curve (at most 60 a second and 200 in all, so a hard flick over
  ssh doesn't flood the connection). This works the same over ssh, in tmux
  with `mouse on` (a swipe scrolls its copy mode), and in a machine's tabs.
  In tmux with the mouse off a swipe sends nothing (the arrow keys it would
  become go to the shell's history) and says how to turn the mouse on. With
  animations removed (Remove animations, animator scale 0, which the page
  sees as reduced motion) flicks still coast, as Android's own lists do;
  only the stretch at the ends and the scrollbar's fade go, and a working agent's
  logo still fades in and out, as on the desktop. Pinch to change the font size (kept per
  device). Two fingers sideways, or a swipe on the tab bar when its tabs fit,
  change tabs. A swipe in from the left edge opens the drawer, a swipe left
  closes it. Tap for the keyboard, long-press for the selectable text view.
  Android's back gesture is kept off a 200dp strip in the middle of the left
  edge (the most Android allows), so the swipe works there; the rest of the
  edge still goes back.
- **One finger sideways moves the cursor**: an arrow key for each character
  cell the finger moves, and a flick adds more (up to 80), in the shell and
  in full-screen programs alike. Up and down still scroll; which one a drag
  is gets decided after its first 10 px.
- **The app's own web UI on every machine.** The app ships Gecko's web UI
  and shows it for every machine, so gestures, scrolling and the agent
  animations work even where the machine runs an older Gecko (its tabs work;
  newer features that need the machine wait for an update there). See
  [Security](security.md).
- **⋮ > Touch diagnostics** shows what the last gesture on a terminal looked
  like (how many touch samples, their time steps, the speed at release, the
  fling it made, or why it made none, and its last few samples), whether
  reduced motion is on, with the screen's refresh rate and the WebView version, to
  copy into a bug report. It starts with the build ids.
- **Build id.** Each APK says which build it is, e.g. `a1b2c3d · Oct 17 02:15`
  (the commit, `-dirty` if it had uncommitted changes, and when it was built,
  in the phone's time): at the bottom of the **⋮** menu and of the machine
  list, tap to copy. Touch diagnostics shows it, the web UI's (the same, as
  the app brings its own) and, when it differs, the build of the Gecko on the
  machine, which `/api/info` reports. `make android-gecko` sets it, for the
  web UI, gecko and the app at once.
- **Back** closes what is open, innermost first: a menu, a dialog, the
  palette, the text view, find, the drawer, compose. With nothing open it goes
  back to the machine list.
- **Clipboard.** Copies go to the Android clipboard, including what programs
  copy with OSC 52 (tmux, nvim, Claude Code), even over plain http where a
  browser would refuse. **paste** pastes the Android clipboard into the tab.
- **Share to Gecko.** Share text from any app and pick **Paste in Gecko**: it
  is pasted (not run) in the tab you have open, on the machine you used last.
- **Keep screen on**, in the **⋮** menu.

## A terminal on this phone

**This phone** is always first in the machine list. Tap it and the app
starts Gecko's own daemon on the phone, the same program as on a computer,
and opens it: tabs, workspaces, the agent dock and the extra keys all work
the same. The shell is Android's `/system/bin/sh` (mksh) with the toybox
commands (`ls`, `ps`, `top`, `grep`, `getprop`, `am`, `pm`...), running as
the Gecko app in its own sandbox. Its home is the app's private storage,
with a `.mkshrc` you can edit.

Gecko keeps running while you use other apps, with a "Gecko on this phone"
notification: Android ends an app's processes along with it otherwise. Stop
it from that notification, from **⋮** next to **This phone**, or from **⋮**
in the extra keys. Stopping ends every shell on the phone.

### SSH from the phone

Android has no ssh, so Gecko brings its own: `ssh`, `ssh-keygen` and `scp`
are on the phone shell's PATH (links to Gecko, which acts as each), and
Gecko uses the same ssh to reach machines you add from the phone.

```sh
ssh-keygen                      # once: ~/.ssh/id_ed25519 and .pub
ssh me@devbox.lan               # password or key; asks about a new host key
ssh -p 2222 me@10.0.0.5 uptime  # a command, with its exit code
scp notes.txt me@devbox.lan:    # and back: scp me@devbox.lan:file .
```

- Names resolve with the phone's own DNS, search domains included, so
  Tailscale short names (`ssh me@devbox`) work like on a computer.
- Logins: keys (`~/.ssh/id_ed25519`, `id_ecdsa`, `id_rsa`, or `-i`;
  passphrase-protected ones ask), keyboard-interactive and password.
- Host keys: `~/.ssh/known_hosts`, with OpenSSH's questions and wording on
  first contact, and a refusal when a known host's key changed
  (`ssh-keygen -R host` forgets one). `StrictHostKeyChecking`,
  `UserKnownHostsFile` and `BatchMode` work with `-o`.
- A terminal for interactive logins (`-t`, `-tt`, `-T` as usual) that
  follows the tab's size; `vim`, `top` and the like work. The remote
  command's exit code is ssh's, 255 for ssh's own errors.
- `~/.ssh/config`: `Host` blocks with patterns, `HostName`, `User`, `Port`,
  `IdentityFile`, `ServerAliveInterval`, and the options above. `Match` and
  `Include` are skipped.
- `ssh-keygen` makes ed25519 keys; `-y` prints a public key, `-l` a
  fingerprint, `-R` removes a host from known_hosts.
- `scp` copies files (and folders with `-r`) between the phone and one other
  machine, with the classic scp protocol.
- Not there: an ssh agent, port forwarding (`-L`, `-R`, `-D`), jump hosts
  (`-J`), X11, `~.` escapes, sftp.

**SSH key** (in **⋮** on the machine list, or in the extra keys while on This
phone) shows the phone's public key, making it on first use, with Copy and
Share.

To add a computer from the phone, use **Machines +** in the sidebar, or
**Make it a Gecko machine** under a tab where you ran `ssh`. Gecko asks you to
confirm a new machine's fingerprint and asks for its password if it has no key
for it. It then puts the phone's key in `~/.ssh/authorized_keys` there, so
it can stay connected without a password (see [More machines](machines.md)).
If Gecko isn't installed there, the phone installs it: the app carries Gecko
for Linux on x86_64 and arm64 (the same files it runs itself), so Linux
machines on those CPUs work. For a Mac or another system, install Gecko there
yourself first.

### Allow other devices

Off by default. Turn it on (**⋮** next to **This phone**, or in the extra
keys while on it, or **Open on another device** in the palette) and the
phone's Gecko also listens on its Wi-Fi, Ethernet and Tailscale addresses,
port 7682. The app shows the link and QR code for each address, the same
kind Gecko shows on a computer: open it in a browser, or add it as a machine
in another Gecko app.

What that means:

- Anyone with the link can use this phone's terminal: run commands as the
  Gecko app, read and change its files, use the network. Not other apps'
  data, not your photos and documents (the app has no storage permission),
  and nothing that needs root.
- The link has its own token, separate from the one the app uses. Turning it
  off closes the listener, disconnects everyone who used the link, and makes
  the link useless; turning it on again makes a new one. Stopping Gecko on
  the phone also turns it off.
- Only peers with private addresses get an answer: LAN (10/8, 172.16/12,
  192.168/16, link-local) and Tailscale (100.64/10). The listener is IPv4
  only, since phones often have public IPv6 addresses. Mobile networks can
  use 100.64/10 too, so the token is what keeps others out there; turn it
  off when you aren't using it.
- It is plain http, like Gecko on a LAN: use it on networks you trust, or
  over Tailscale, which encrypts.

On a computer, the same listener is `gecko share on [addr]` and
`gecko share off`.

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

On the phone's own terminal:

- It is Android's shell and toybox, not a Linux distribution: no package
  manager, no git, tmux, ssh or compilers (unless you bring static
  binaries). mksh has no shell integration, so Gecko knows the directory and
  the running program from the process table, but not command boundaries or
  exit codes.
- No storage permission: `ls /sdcard` shows the folders, not your files. The
  app's own folder there, `/sdcard/Android/data/io.github.geckoterm.android`,
  is usable.
- Android 12 and newer may kill the shells' processes ("phantom process"
  limit: 32 child processes across all apps, and heavy background CPU use).
  On Android 14 and newer, Developer options has **Disable child process
  restrictions**; with adb, `adb shell settings put global
  settings_enable_monitor_phantom_procs false`.
- Updating or reinstalling the app ends the shells on the phone.
- Gecko on Android avoids Go's `exec.LookPath`: Go checks files there with the
  `faccessat2` system call, which Android's seccomp filter answers by killing
  the process. Builds with a newer Go must keep that in mind (newer Go also
  uses `pidfd` calls Android may block).
- The Digital Watch theme makes the Android emulator itself crash (its
  software GPU) on the test machine; it renders correctly in Chromium at
  phone size, and wasn't tried on a real phone.
- 64-bit phones only (arm64-v8a, x86_64). The binary is a static linux build;
  it runs as the app, from the app's native library folder, the only place
  Android 10 and newer let an app execute files.

In general:

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
