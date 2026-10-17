# Desktop apps

Self-contained Gecko apps you can double-click: the native window (Electron,
`desktop/`) with the `gecko` binary for that OS and architecture inside.

| Platform | File | Architectures |
| --- | --- | --- |
| macOS 12+ | `Gecko-<ver>-mac-universal.dmg` | Apple silicon and Intel (universal) |
| Linux | `Gecko-<ver>-linux-x86_64.AppImage` | x86_64 (arm64: `ARCHS="x64 arm64"`) |
| Windows 10 1809+ / 11 | `Gecko-<ver>-windows-x64-setup.exe`, `Gecko-<ver>-windows-x64.zip` | x64 |

## Building

```sh
make package-mac        # on macOS (needs hdiutil)
make package-appimage   # on Linux
make package-windows    # on Windows (Git Bash), or Linux with wine
```

Output goes to `dist/desktop/`. The targets build the web client, cross-compile
`gecko` into `dist/build/desktop/<os>-<arch>/` and run
[electron-builder](https://www.electron.build) (pinned in
`packaging/desktop/package.json`, kept out of `desktop/` so `make desktop`
stays small). The config is `packaging/desktop/electron-builder.config.cjs`.

CI builds all three on every push to `feature/desktop-apps`
(`.github/workflows/desktop.yml`) and smoke-tests each on its own runner:
fresh `GECKO_HOME`, bundled daemon up, `/api/info` answers, the UI loads, a
session outlives the window. The installers, screenshots and video frames are
the run's artifacts.

## What the app does when opened

Started without `--url` (double-clicked, from the dock or Start menu), the app
runs the bundled `gecko url`. That is what `gecko open` does: it connects to
this user's daemon (the socket, token and web port under `GECKO_HOME`, by
default `~/.gecko-terminal`), starts it detached when none is running, and
prints the URL with the token. The window opens on that URL at the size and
place the last one had.

So sessions survive closing the window, as they always have, and an already
running daemon (from `make install`, a package, or another copy of the app)
is used as it is. `gecko open` and `gecko-desktop --url=...` work as before.

The daemon is never run from somewhere that can disappear under it: the
AppImage's mount, a dmg, a translocated app or the Windows install folder (an
update would have to stop it to replace `gecko.exe`). There the app runs a
copy in `GECKO_HOME/state/bin`, one per build.

With `GECKO_HOME` set, the window also keeps its own browser profile and
single-instance lock under `GECKO_HOME/state/desktop`, so a second Gecko
(another home, another port) gets its own window.

## Build id

The About panel (macOS: Gecko > About Gecko) and the tooltip on the Gecko
title in the sidebar show the build: short commit and build time in local
time, the same format as the Android app, e.g. `a1b2c3d · Oct 17 02:15`
(`-dirty` when built from uncommitted changes).

## Command line tool

- **macOS**: Gecko > Install command line tool links `/usr/local/bin/gecko`
  to the `gecko` inside the app (it asks for an administrator password). Move
  the app to Applications first. By hand:
  `sudo ln -sf /Applications/Gecko.app/Contents/Resources/gecko /usr/local/bin/gecko`.
- **Linux**: `./Gecko-x86_64.AppImage gecko <args>` runs the CLI, e.g.
  `./Gecko-x86_64.AppImage gecko ls`. A symlink named `gecko` does the same:
  `ln -s ~/Applications/Gecko-x86_64.AppImage ~/.local/bin/gecko`.
- **Windows**: the CLI is `resources\gecko.exe` in the install folder
  (`%LOCALAPPDATA%\Programs\gecko` by default); add that folder to `PATH` to
  use it from a terminal.

## Unsigned builds

The apps are not signed with a paid certificate, so the first start needs
one extra step.

- **macOS** (ad-hoc signed, not notarized): Gatekeeper says Gecko "cannot be
  opened" or "is damaged". Drag Gecko to Applications, then right-click (or
  Control-click) it, choose Open, and confirm. On macOS 15 and later, open it
  once, then go to System Settings > Privacy & Security and click "Open
  Anyway". Or clear the quarantine flag in a terminal:
  `xattr -dr com.apple.quarantine /Applications/Gecko.app`.
- **Windows**: SmartScreen shows "Windows protected your PC". Click "More
  info", then "Run anyway". The installer is per user and needs no admin
  rights.
- **Linux**: `chmod +x Gecko-*.AppImage`, then run it. AppImages need FUSE 2
  or 3 (`libfuse2` on older distros); without it, run with
  `--appimage-extract-and-run`.

## Linux sandbox

Chromium's sandbox needs unprivileged user namespaces, which Ubuntu 23.10 and
later restrict with AppArmor, and an AppImage cannot carry the setuid helper
that is the other way in. The AppImage's launcher tries `unshare -Ur true`;
where that fails, it starts the window with `--no-sandbox`, and the app says
so once (in a dialog and on stderr; the note is remembered in
`GECKO_HOME/state/desktop-sandbox-note`). The window only ever shows your own
Gecko, so this is the same trade-off as `GECKO_DESKTOP_NO_SANDBOX=1` with
`make desktop`; setting that variable forces it in the AppImage too.

To keep the sandbox on, allow user namespaces for the AppImage the way `make
desktop-sandbox` does for `gecko-desktop` (needs sudo once). The AppImage
mounts itself at a new `/tmp/.mount_Gecko*` path each run, so the profile
matches that pattern:

```sh
sudo tee /etc/apparmor.d/gecko-appimage >/dev/null <<'EOF'
abi <abi/4.0>,
include <tunables/global>
profile gecko-appimage /tmp/.mount_Gecko*/gecko-desktop.bin flags=(unconfined) {
  userns,
}
EOF
sudo apparmor_parser -r /etc/apparmor.d/gecko-appimage
```

## Windows notes

The Windows daemon uses ConPTY (Windows 10 1809 or newer) and starts
PowerShell 7 if installed, else Windows PowerShell. What is not there yet on
Windows: `gecko upgrade` (restart in place keeping sessions); use `gecko stop`
and reopen Gecko instead, which ends local sessions. The window uses Windows
11 acrylic for its see-through background.
