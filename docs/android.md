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

For git, tmux, Node, Python, compilers and the rest, use [Termux](#termux)
instead: the app opens Gecko running inside it the same way.

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

## Termux

[Termux](https://termux.dev) is a Linux environment for Android with its
own package manager: `pkg install git tmux nodejs python golang openssh`,
and the coding agents that run on those (Claude Code's npm package now
installs a native build, and it has none for Android, so it doesn't run
there; agents in Node or Python do). Gecko runs
inside it, and the app shows it as **Termux** in the machine list, right
after **This phone**, whenever Termux is installed. Tap it and the app
starts Gecko in Termux and opens it like any machine: tabs, workspaces, the
agent dock and its notifications, the extra keys and touch all work the
same, with Termux's shell (bash, or what `chsh` picked) and packages.

### Set up

The first tap walks you through what's missing, one screen at a time, and
checks again when you come back from Termux:

1. **Termux.** Install it from [F-Droid](https://f-droid.org/packages/com.termux/)
   or its [GitHub releases](https://github.com/termux/termux-app/releases),
   and open it once (it sets itself up on first start).
2. **Permission.** Allow Gecko to "Run commands in Termux environment" when
   Android asks. If you said no twice, the app opens its settings, where it is
   under Permissions, Additional permissions.
3. **Allow apps, install Gecko.** Termux takes commands from other apps only
   when `~/.termux/termux.properties` says so. Run this line in Termux (the
   app copies it for you and opens Termux): it installs Gecko and sets that.

   ```sh
   curl -fsSL https://raw.githubusercontent.com/eternaleclipse/gecko/main/packaging/termux/install.sh | sh -s -- --app
   ```

   By hand, the line for `~/.termux/termux.properties` is
   `allow-external-apps=true`; then run `termux-reload-settings`. When apps
   are already allowed but Gecko isn't installed, **Install in Termux** runs
   the installer in a Termux session for you.

Then the app starts `gecko daemon --listen 127.0.0.1:7683` in Termux, gets
its link with `gecko url`, and connects. **⋮** next to Termux stops it
(`gecko stop`, which ends its tabs), opens Termux, or shows the setup steps
again.

How it works: Termux's [RUN_COMMAND intent](https://github.com/termux/termux-app/wiki/RUN_COMMAND-Intent)
runs `gecko` with Termux's `sh`, in the background (Termux's notification
counts it as a task, which also keeps Termux running), and sends the output
of `gecko url` back to the app. From then on it is a plain connection to
`127.0.0.1:7683` with Gecko's token, as with a computer.

### Without the app

The installer works on its own (no root needed): it puts the Linux build for
the phone's CPU from the latest GitHub release in `$PREFIX/bin/gecko`
(`--source` builds it with Termux's Go and Node instead). Run it again to
update; a running Gecko is upgraded in place, keeping its tabs. Then:

```sh
gecko               # starts the daemon, prints the link (open it in a browser, or Paste link in the app)
gecko attach <tab>  # or use a tab right here in Termux
gecko service install   # start at boot, with the Termux:Boot app
```

Inside Termux, Gecko takes Termux's shell, its `$PREFIX` (there is no
`/bin` or `/usr`), its `resolv.conf` for names, and starts scripts with
`#!/usr/bin/env node` or `#!/bin/sh` lines from `$PREFIX/bin` the way
Termux's own programs do. There is no systemd, so plain `gecko` doesn't set
up start at login; `gecko service install` writes a
[Termux:Boot](https://f-droid.org/packages/com.termux.boot/) script instead
(`~/.termux/boot/gecko`). A [termux-packages](https://github.com/termux/termux-packages)
recipe is drafted in `packaging/termux/build.sh`.

### Keeping it running

Android stops background work on its own, and Termux is no exception:

- **Phantom process killer** (Android 12 and newer): Android kills
  processes that apps start beyond 32 across all apps, and ones that use
  much CPU in the background, which can end Gecko and its tabs. On Android
  14 and newer, turn on **Disable child process restrictions** in Developer
  options. On Android 12 and 13, with adb from a computer:

  ```sh
  adb shell "/system/bin/device_config set_sync_disabled_for_tests persistent"
  adb shell "/system/bin/device_config put activity_manager max_phantom_processes 2147483647"
  adb shell settings put global settings_enable_monitor_phantom_procs false
  ```

- **Battery optimisation**: set Termux's battery use to unrestricted (App
  info, Battery), or Android pauses it when the screen is off. Phone makers'
  extra limits apply too (see [Notifications](#notifications) and
  [dontkillmyapp.com](https://dontkillmyapp.com)).
- **`termux-wake-lock`** (or **Acquire wakelock** in Termux's
  notification) keeps the CPU awake for Termux while agents work with the
  screen off; `termux-wake-unlock` lets go.
- **Exit** in Termux's notification ends everything in Termux, Gecko
  included. The app then starts it again the next time you tap Termux.

### F-Droid, GitHub and Google Play

Use Termux from F-Droid or GitHub. The Google Play listing is a separate
build, made and signed differently, and Termux's add-ons (Termux:Boot,
Termux:API) have to come from the same place as Termux itself. It also
targets a newer Android, where Termux may have to start programs through
Android's own linker, which can't start Gecko's Linux build (a static
program); on the F-Droid and GitHub builds it starts directly. Gecko is
tested with the GitHub build, and the app warns when its Termux came from
Play.

### Separate sandboxes

Termux and the Gecko app are separate apps, and Android keeps their files
apart: Gecko in Termux has Termux's home (`/data/data/com.termux/files/home`),
its own `~/.gecko-terminal` (token, settings, tabs) and its own ssh keys,
apart from **This phone**'s. The tabs of one aren't the other's, and Termux
can't read the app's files or the other way round. `termux-setup-storage`
gives Termux your shared storage (`~/storage`); the Gecko app has no storage
permission. Both can reach each other only over `127.0.0.1`, with the token.

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
each saved machine, and to Gecko on this phone while it runs (so the machines
added there are watched too), which reads only the list of tabs and their
agents' state. Its silent notification says what it reaches right now
("Watching agents on ubunty", "on 2 machines", or "Not connected, retrying"),
with a second line while Gecko runs on this phone, and **Stop watching**. Without it you are notified only while Gecko is open, and
see what you missed in the agent dock when you come back.

Some phone makers stop apps on their own, background services included:
Xiaomi (MIUI, HyperOS), OnePlus, Oppo, Realme, Vivo, Huawei and Honor, and
Samsung puts apps it thinks unused to sleep. When you turn on **Notify in
background**, Gecko asks once to be left out of battery optimization, and on
those phones says which switches to flip (autostart, "no restrictions",
Samsung's sleeping apps), with a button to that screen where the phone has
one and **More help** for [dontkillmyapp.com](https://dontkillmyapp.com).
While it still helps, **Background notifications** in the **⋮** menu shows it
again.

## Screens, folds and windows

The app keeps one page through everything that changes the screen:
rotating, folding or unfolding, split screen and desktop windows, dark
mode, the system text size. The session, the tab and the line you were
reading stay; the terminal refits.

- Narrow or short windows get the compact layout (drawer, slim agent
  chips, no status line): phones in either orientation, a folded
  foldable, half of a split screen. Wider and taller ones (a tablet, an
  unfolded foldable in landscape, a desktop window) keep the sidebar.
- The drawer opens with a swipe in from the left edge, from the screen
  edge too when a camera cutout sits there (landscape); the back gesture
  still works from the rest of that edge.
- The extra keys follow the system text size (up to 1.5x). With a
  keyboard plugged in or paired, only the menu stays.
- Android 10 (API 29) and newer are tested; the app installs from Android 8
  (minSdk 26). The page needs an Android System WebView 111 or newer; Play
  updates it on its own, and an older one gets a screen that says so with an
  **Update WebView** button.

## Limits

On the phone's own terminal:

- It is Android's shell and toybox, not a Linux distribution: no package
  manager, no git, tmux, ssh or compilers (unless you bring static
  binaries). [Termux](#termux) has all of those. mksh has no shell integration, so Gecko knows the directory and
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
