<p align="center">
  <img src="packaging/linux/gecko.svg" width="88" alt="Gecko logo">
</p>

<h1 align="center">Gecko</h1>

<p align="center">
  <b>The terminal for running coding agents across all your machines.</b><br>
  Sessions that survive anything. Agents you can see at a glance. From your desktop or your phone.
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white">
  <img alt="Platforms" src="https://img.shields.io/badge/Linux%20%C2%B7%20macOS%20%C2%B7%20Windows-a3be8c">
  <img alt="Mobile" src="https://img.shields.io/badge/phone-PWA%20%C2%B7%20Android-d08770">
  <img alt="Agents" src="https://img.shields.io/badge/agents-Claude%20Code%20%C2%B7%20Codex%20%C2%B7%20Gemini%20%C2%B7%20Aider-88c0d0">
</p>

<p align="center">
  <a href="docs/features.md">Features</a> ·
  <a href="docs/install.md">Install</a> ·
  <a href="docs/machines.md">Machines</a> ·
  <a href="docs/mobile.md">Phone</a> ·
  <a href="docs/android.md">Android</a> ·
  <a href="docs/ios.md">iOS</a> ·
  <a href="docs/themes.md">Themes</a> ·
  <a href="docs/keyboard.md">Keyboard</a> ·
  <a href="docs/config.md">Config</a> ·
  <a href="docs/architecture.md">Architecture</a>
</p>

<p align="center">
  <img src="docs/img/hero.png" alt="Gecko in the Tokyo Night theme with workspaces on two machines, Claude Code working and Codex waiting for approval in the agent dock">
</p>

## What it does

- **Sessions outlive everything.** A small daemon owns every shell and agent.
  Close the window, lose Wi-Fi or upgrade Gecko, and tabs keep running and
  resume from the last byte. The daemon starts at login.
- **Agents at a glance.** Claude Code, Codex, Gemini, Aider and others are
  recognised in a tab, inside tmux panes, and over ssh or mosh from their
  terminal titles. The dock shows which are working and which need you.
- **Every machine.** Add hosts by their SSH destination. Or turn on sticky
  sessions: sign in to one account on each machine and they reach each other
  through a relay you run, end-to-end encrypted, with no open ports.
  See [Machines](docs/machines.md) and [Accounts and the relay](docs/cloud.md).
- **Latency you can see.** The status line shows the round trip and the
  transport (WebTransport over QUIC, or WebSocket), and each machine shows its
  own hop. A dead connection is noticed in about 3 seconds and redialed at
  once. See [Connection and transport](docs/architecture.md#connection-and-transport).
- **[Android app](docs/android.md).** The full UI on phones, foldables and
  tablets: terminal keys, touch scrolling and selection, notifications you can
  answer when an agent needs you, a built-in ssh client, and a terminal on the
  phone itself.

Coming: an iOS app, more desktop packages (macOS dmg, AppImage, snap, Arch)
and Termux integration.

[All features](docs/features.md)

## Quick Start

```sh
git clone https://github.com/eternaleclipse/gecko && cd gecko
make install      # needs Go 1.22+ and Node 18+
gecko             # starts the daemon and opens the app
```
