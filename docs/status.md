# Status and limits

[Docs](README.md)

Working and tested on Linux: local and remote sessions, resume after network
drops, shell integration, ssh/mosh/tmux/agent detection, CLI attach, and the web UI on desktop and mobile layouts. macOS
and Windows builds compile and use the same code paths. These are not
exercised yet:

- **macOS:** process inspection uses `ps`, and the cwd falls back to `lsof`
  without shell integration.
- **Windows:** sessions use ConPTY. There is no foreground-process inspection
  yet, so tabs show titles, bells and OSC notifications but no ssh/tmux/agent
  detection, and there is no PowerShell integration script yet.

Apps and packages, each built and smoke-tested in CI:

- **iOS:** the [iOS app](ios.md) runs on iPhone and iPad, built for the iOS
  Simulator for now (no App Store yet).
- **Desktop:** [desktop apps](desktop-packages.md) for macOS (dmg), Windows
  (setup or zip) and Linux (AppImage), unsigned.
- **Linux:** [packages](linux-packages.md) for Debian and Ubuntu, Fedora,
  Arch and snap, and static binaries that run on any Linux.
- **Termux:** Gecko runs in [Termux](android.md#termux) on Android, and the
  Android app opens it like any other machine.

New in 0.3.0:

- **Transparency:** background opacity shows the desktop on macOS and
  Windows too, not a gray material, checked in CI on all three systems.
- **The anchor (protocol 10):** the Gecko you connect to manages every other
  machine (see [The anchor](anchor.md)). The server now enforces scope: relay
  peers only get that machine's own tabs, whatever they ask for. v9 machines
  still work with a v10 anchor.
- **Web app on phones:** installable, with Settings on the tab bar, pinch for
  text size, and tabs that switch on every tap. `make test-pwa` runs it in
  WebKit and Chromium, and CI also runs Safari in the iOS Simulator.

Agent status is a heuristic. It looks for screen text such as "esc to
interrupt" and approval prompts, plus output activity, bells and OSC 9/777
notifications. It works well for Claude Code and Codex-style TUIs. Other agents
may need patterns added in `internal/session/session.go`.

Sessions survive client, network and SSH loss, and upgrades.
`gecko upgrade` (which `make install` runs for you, and which is also in the
palette) restarts the daemon in place with the new binary. It hands every
PTY, its buffered output and both listening sockets to the new process via
`exec`, so shells and agents never notice, and clients reconnect and resume
where they were. This works on Linux and macOS. On Windows, `gecko stop`
still ends sessions, and so does a reboot. Ideas for later:

- Split panes.
- A native GPU renderer shell (Tauri or Wails) around the same client.
- tmux control mode (`-CC`), so tmux windows become Gecko tabs.
- Push notifications for the iOS app (APNs), so it notifies in real time
  while closed (see [iOS app](ios.md)), and a signed build of it.
