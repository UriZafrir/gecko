package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gecko-term/gecko/internal/config"
)

// cmdService installs a login service so the daemon (and every session,
// agent included) is always there, independent of terminals and SSH.
// Plain `gecko` installs it on first run (see autoService); uninstall
// removes it and stops that from happening again.
func cmdService(args []string) error {
	if len(args) != 1 || (args[0] != "install" && args[0] != "uninstall") {
		return fmt.Errorf("usage: gecko service install|uninstall")
	}
	if args[0] == "uninstall" {
		return uninstallService()
	}
	_ = os.Remove(serviceOptOut())
	p, err := installService(true)
	if err != nil {
		return err
	}
	fmt.Println("installed", p)
	if runtime.GOOS == "linux" {
		fmt.Println("to keep sessions alive after you log out everywhere, run: loginctl enable-linger $USER")
		fmt.Println("(stop a running daemon with `gecko stop`, then `systemctl --user start gecko`)")
	}
	return nil
}

// autoService installs the login service the first time `gecko` runs, so
// a reboot doesn't leave the machine without a daemon until someone
// remembers `gecko service install`. It never fails the command.
func autoService() {
	p := servicePath()
	if p == "" || fileExists(p) || fileExists(serviceOptOut()) {
		return
	}
	exe, err := os.Executable()
	if err != nil || strings.HasPrefix(exe, os.TempDir()) { // go run
		return
	}
	// Linux without a systemd user manager (containers, WSL without
	// systemd) has nothing to install into.
	if runtime.GOOS == "linux" && exec.Command("systemctl", "--user", "show-environment").Run() != nil {
		return
	}
	// The daemon is already running, so don't start a second one now; the
	// service takes over at the next login.
	if _, err := installService(false); err != nil {
		fmt.Fprintln(os.Stderr, "gecko: could not set up start at login:", err)
		return
	}
	fmt.Println("gecko now starts at login (undo: gecko service uninstall)")
}

// serviceOptOut marks that the user removed the service on purpose.
func serviceOptOut() string { return filepath.Join(config.Home(), "no-login-service") }

// servicePath is the file that starts the daemon at login, or "" where
// Gecko can't install one.
func servicePath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", "gecko.service")
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", "dev.gecko.daemon.plist")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "gecko.vbs")
	}
	return ""
}

// installService writes the login service for this binary. With start,
// launchd also starts it right away (systemd and Windows only do at login).
func installService(start bool) (string, error) {
	p := servicePath()
	if p == "" {
		return "", fmt.Errorf("not supported on %s; run `gecko daemon` from your init system", runtime.GOOS)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	home, _ := os.UserHomeDir()
	var body string
	switch runtime.GOOS {
	case "linux":
		body = fmt.Sprintf(`[Unit]
Description=Gecko terminal daemon

[Service]
ExecStart=%s daemon
Restart=on-failure
KillMode=process

[Install]
WantedBy=default.target
`, exe)
	case "darwin":
		body = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>dev.gecko.daemon</string>
  <key>ProgramArguments</key><array><string>%s</string><string>daemon</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>StandardErrorPath</key><string>%s/Library/Logs/gecko.log</string>
</dict></plist>
`, exe, home)
	case "windows":
		body = fmt.Sprintf("CreateObject(\"WScript.Shell\").Run \"\"\"%s\"\" daemon\", 0, False\r\n", exe)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		if out, err := exec.Command("systemctl", "--user", "enable", "gecko.service").CombinedOutput(); err != nil {
			os.Remove(p)
			return "", fmt.Errorf("systemctl: %s", out)
		}
	case "darwin":
		if start {
			_ = exec.Command("launchctl", "load", "-w", p).Run()
		}
	}
	return p, nil
}

// uninstallService removes the login service (leaving a running daemon
// alone) and keeps `gecko` from installing it again.
func uninstallService() error {
	p := servicePath()
	if p == "" {
		return fmt.Errorf("not supported on %s", runtime.GOOS)
	}
	// Not `launchctl unload`: that would stop the daemon and its sessions.
	if runtime.GOOS == "linux" {
		_ = exec.Command("systemctl", "--user", "disable", "gecko.service").Run()
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(config.Home(), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(serviceOptOut(), nil, 0o600); err != nil {
		return err
	}
	fmt.Println("removed", p)
	return nil
}
