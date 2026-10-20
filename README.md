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
  <img alt="Mobile" src="https://img.shields.io/badge/phone-PWA%20%C2%B7%20Android%20%C2%B7%20iOS-d08770">
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
- **[The anchor](docs/anchor.md).** Open one Gecko from your phone or anywhere
  else and manage every machine through it. Peers that reach it over the
  relay only ever see that machine's own tabs. Any machine in the sidebar
  can become the anchor with **Make anchor**, and the choice is remembered.
- **Latency you can see.** The status line shows the round trip and the
  transport (WebTransport over QUIC, or WebSocket), and each machine shows its
  own hop. A dead connection is noticed in about 3 seconds and redialed at
  once. See [Connection and transport](docs/architecture.md#connection-and-transport).
- **[Android app](docs/android.md).** The full UI on phones, foldables and
  tablets: terminal keys, touch scrolling and selection, notifications you can
  answer when an agent needs you, a built-in ssh client, and a terminal on the
  phone itself.

- **[iOS app](docs/ios.md).** The same UI on iPhone and iPad, built for the
  iOS Simulator for now (no App Store yet).
- **[Desktop apps](docs/desktop-packages.md).** A macOS dmg, a Windows setup
  or zip, and a Linux AppImage, each with the daemon inside, with windows you
  can see through on all three.
- **[Web app](docs/mobile.md#as-an-app-on-the-home-screen-pwa).** Add Gecko to
  your phone's home screen: Settings on the tab bar, pinch for text size,
  tabs that switch on every tap.
- **[Linux packages](docs/linux-packages.md).** deb, rpm, snap and Arch, plus
  static Linux binaries that run anywhere, Termux included.
- **[Termux](docs/android.md#termux).** Gecko runs in Termux on the phone, and
  the Android app opens it like any other machine.

[All features](docs/features.md)

## Quick Start

```sh
git clone https://github.com/eternaleclipse/gecko && cd gecko
make install      # needs Go 1.22+ and Node 18+
gecko             # starts the daemon and opens the app
```
