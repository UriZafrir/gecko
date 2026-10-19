# Install and run

[Docs](README.md)

Requires Go 1.22+ and Node 18+.

```sh
make            # builds web/dist, then bin/gecko with the client embedded
make test
make install    # ~/.local/bin/gecko, plus an app-menu launcher on Linux
make dist       # cross-compiles linux/darwin/windows/freebsd × amd64/arm64
```

`make install PREFIX=/usr/local` installs elsewhere, and `DESTDIR=/tmp/stage`
stages the files for a package without touching this machine.

## Where gecko lives

Keep one gecko per machine. Three things look for it, each its own way:

- **`gecko bridge`**, which another Gecko runs over SSH, looks in
  `~/.local/bin`, `~/bin`, `~/go/bin`, `/usr/local/bin`, `/opt/homebrew/bin`,
  then the login's PATH, and takes the first gecko it finds. The PATH of
  a non-interactive SSH login is often shorter than your shell's, which is
  why Gecko puts these folders first.
- **Your shell** takes the first gecko on its own PATH.
- **The service** (the daemon) runs the binary it was started from, and
  the login service (`gecko service install`) the one it names.

`make install` puts gecko in `~/.local/bin/gecko` (or `$PREFIX/bin/gecko`),
under a temporary name renamed over the old file, so a running gecko is never
overwritten in place ("text file busy") and nothing starts a half-copied
binary. It then runs `gecko upgrade --installed`, which:

- makes every other gecko in your home folder (`~/go/bin/gecko` from
  `go install`, an older install with another PREFIX, a copy another
  machine's Gecko put in `~/.local/bin`) a link to the new one, and names
  the ones outside it (a package's `/usr/bin/gecko`, say) for you to remove;
- points the login service at the new binary;
- restarts a running service in place with the new binary, wherever the
  running one was started from, keeping every session, and checks that the
  restarted service really runs the new build.

`gecko upgrade` on its own does the restart, and lists the other geckos
without changing them. A service from before Gecko 0.3 restarts only into
the binary it was started from; `gecko upgrade` says so, and
`gecko stop && gecko` once fixes it (that closes its sessions; later
upgrades keep them).

When another machine's Gecko installs or updates gecko here (Add machine,
**Update Gecko**, `gecko host install`), it replaces the gecko `gecko bridge`
runs when your login may, else installs to `~/.local/bin/gecko`, which comes
first, and restarts the service with it.

`make dist` needs [zig](https://ziglang.org/download/) 0.13.0 on PATH (or
`ZIG=/path/to/zig`): it links the Linux binaries as static PIEs, which also
start where a program has to go through Android's linker (Termux from
Google Play). The GitHub releases carry them as `gecko-linux-amd64` and
`gecko-linux-arm64`.

## Installation packages

```sh
make packages                    # all of them, into dist/packages
make packages PLATFORMS=linux    # or macos, windows (several are fine)
```

| Platform | Package | Installs |
| --- | --- | --- |
| Linux | `gecko_<ver>_<arch>.deb`, `gecko-<ver>.<arch>.rpm` (amd64, arm64) | `/usr/bin/gecko`, an app-menu launcher and the icon |
| macOS | `Gecko-<ver>-macos.zip` | `Gecko.app` for Apple silicon and Intel; drag it to Applications |
| Windows | `Gecko-<ver>-setup-<arch>.exe` (amd64, arm64) | Gecko for the current user (no admin), a Start menu entry, and optionally `gecko` on PATH and a desktop shortcut |

The version is the latest `v*` tag (`0.0.0` without one); set
`PKG_VERSION=1.2.3` to override it. Linux packages are made with
[nfpm](https://nfpm.goreleaser.com), which `go run` fetches when it isn't
installed. Windows installers need NSIS (`apt install nsis`,
`brew install makensis`). Everything builds on any OS.

Notes:

- **macOS:** the app isn't signed or notarized, so the first launch is
  blocked. Open it from **System Settings > Privacy & Security > Open
  Anyway**, or run `xattr -dr com.apple.quarantine /Applications/Gecko.app`.
  For the command line:
  `sudo ln -s /Applications/Gecko.app/Contents/MacOS/gecko /usr/local/bin/gecko`.
- **Windows:** installing over a running daemon keeps it and its sessions
  going on the old version; the next start uses the new one. Uninstalling
  stops the daemon.
- **Linux:** a package upgrade doesn't touch a running daemon; run
  `gecko upgrade --installed` to switch to the new binary, keep your
  sessions and turn any other gecko in your home folder (an earlier
  `make install`) into a link to `/usr/bin/gecko`, so SSH connections from
  other machines run the package's gecko too.

For client development, run `make dev` (rebuilds on change) and start the
daemon with `GECKO_WEB_DIR=$PWD/web/dist gecko daemon`. It then serves the
client from disk, so a page reload picks up changes without restarting the
daemon (and its sessions).

```sh
gecko                 # start the daemon if needed and open the app window
gecko new -w api -n claude -- claude    # new tab in workspace "api", typing `claude`
gecko ls              # all sessions on all machines
gecko agents          # what every coding agent is doing
gecko attach api      # use a session from any terminal (Ctrl-\ detaches)
gecko send claude "run the tests"       # type into a session (scriptable)
gecko capture claude  # its text, tmux-aware
gecko service uninstall # stop starting at login
```

The first time `gecko` runs it sets the daemon to start at login (a systemd
user unit, a launchd agent, or a script in the Windows Startup folder), so a
reboot doesn't leave you without one. `gecko service uninstall` removes it
and stops `gecko` from adding it back; `gecko service install` adds it again.
Only your installed Gecko does this: a build inside a source checkout, a run
with its own `GECKO_HOME`, or a copy other than the `gecko` on your PATH
leaves the service alone (`gecko service install` still works from any of
them).

`gecko open` opens Gecko in its own window. It starts at exactly 80×24,
centered, and remembers the right size after the first time.

- **Desktop app (`make desktop`):** a small native Electron window with a
  see-through background. **Background opacity…** in the palette sets how
  see-through it is: what is behind the window shows through on macOS,
  Windows and Linux (Linux needs a compositor). Text, menus and dialogs
  stay solid. On Ubuntu 23.10+ Chromium's sandbox needs an AppArmor profile;
  run `make desktop-sandbox` once (it uses sudo). Until then `gecko open`
  falls back to a browser window. `GECKO_DESKTOP_NO_SANDBOX=1` runs without
  the sandbox instead.
- **Otherwise:** a chromeless browser app window (Chrome, Edge, Brave or
  Chromium). You can also install the page as an app from the browser menu.

