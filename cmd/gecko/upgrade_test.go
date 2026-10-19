//go:build linux

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/client"
	"github.com/gecko-term/gecko/internal/proto"
)

// Installing over a running Gecko, end to end: real binaries of this
// checkout built as two versions, a daemon with its own HOME and
// GECKO_HOME (no web listener), never the user's.

func buildVersion(t *testing.T, dir, version string) string {
	t.Helper()
	p := filepath.Join(dir, "gecko-"+version)
	out, err := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", p, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return p
}

type machine struct {
	t    *testing.T
	home string // HOME
	env  []string
	pid  int
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	if testing.Short() {
		t.Skip("starts a real daemon")
	}
	home, err := os.MkdirTemp("", "gk-up-") // short: the socket path has a length limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	gh := filepath.Join(home, ".gecko-terminal")
	os.MkdirAll(gh, 0o700)
	os.WriteFile(filepath.Join(gh, "config.json"), []byte(`{"name":"box","listen":"off"}`), 0o600)
	m := &machine{t: t, home: home}
	m.env = append(os.Environ(), "HOME="+home, "GECKO_HOME="+gh, "PATH=/usr/bin:/bin", "SHELL=/bin/sh")
	t.Setenv("GECKO_HOME", gh) // for this test's own client
	return m
}

func (m *machine) put(src, rel string) string {
	m.t.Helper()
	p := filepath.Join(m.home, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	b, err := os.ReadFile(src)
	if err != nil {
		m.t.Fatal(err)
	}
	// Like make install: a new file renamed over the old one.
	if err := os.WriteFile(p+".new", b, 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.Rename(p+".new", p); err != nil {
		m.t.Fatal(err)
	}
	return p
}

func (m *machine) startDaemon(bin string) {
	m.t.Helper()
	cmd := exec.Command(bin, "daemon")
	cmd.Env = m.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		m.t.Fatal(err)
	}
	m.pid = cmd.Process.Pid
	m.t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if c, err := client.Connect(false); err == nil {
			c.Close()
			return
		}
	}
	m.t.Fatal("the daemon didn't start")
}

func (m *machine) run(bin string, args ...string) string {
	m.t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = m.env
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// runs is the binary the daemon process runs now.
func (m *machine) runs() string {
	p, _ := os.Readlink("/proc/" + strconv.Itoa(m.pid) + "/exe")
	return strings.TrimSuffix(p, " (deleted)")
}

// The daemon was started from one gecko (here ~/go/bin/gecko, as from `go
// install`, or an older install anywhere) and `make install` puts the new
// one in ~/.local/bin, which `gecko bridge` runs. The restart that make
// install asks for must take the service to the new binary, and the old
// copy must not stay around for a shell or the bridge to run. It used to
// say "restarted with" the new version and exec the old binary again.
func TestUpgradeRestartsIntoTheInstalledGecko(t *testing.T) {
	m := newMachine(t)
	dir := t.TempDir()
	oldBin, newBin := buildVersion(t, dir, "v-old"), buildVersion(t, dir, "v-new")
	stale := m.put(oldBin, "go/bin/gecko")
	m.startDaemon(stale)

	installed := m.put(newBin, ".local/bin/gecko")
	out := m.run(installed, "upgrade", "--installed")
	if !strings.Contains(out, "restarted with v-new") {
		t.Fatalf("gecko upgrade --installed:\n%s", out)
	}
	if got := m.runs(); got != installed {
		t.Fatalf("the service runs %s after the install, want %s\n%s", got, installed, out)
	}
	c, err := client.Connect(false)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if c.Build != "v-new" {
		t.Fatalf("the service says it is %q", c.Build)
	}
	// The other copy is now the same gecko.
	if target, err := os.Readlink(stale); err != nil || target != installed {
		t.Fatalf("%s: %q, %v; want a link to %s", stale, target, err, installed)
	}
	if v := m.run(stale, "version"); !strings.HasPrefix(v, "gecko v-new") {
		t.Fatalf("%s version: %s", stale, v)
	}
}

// A restart into a binary that is still being written ("text file busy")
// fails up front with that reason, and the service carries on as it was.
// It used to say yes, and then the daemon, having closed every machine's
// link to restart, failed to exec and stayed up with dead links.
func TestUpgradeIntoBusyBinaryFails(t *testing.T) {
	m := newMachine(t)
	dir := t.TempDir()
	running := m.put(buildVersion(t, dir, "v-old"), ".local/bin/gecko")
	m.startDaemon(running)

	// Copied in place, not renamed: open for writing while the restart runs.
	busy := filepath.Join(m.home, "bin", "gecko")
	os.MkdirAll(filepath.Dir(busy), 0o755)
	f, err := os.OpenFile(busy, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	src, _ := os.Open(running)
	_, _ = io.Copy(f, src)
	src.Close()

	c, err := client.Connect(false)
	if err != nil {
		t.Fatal(err)
	}
	start := c.Start
	_, err = c.Call(&proto.Msg{T: proto.TUpgrade, Data: busy})
	c.Close()
	if err == nil || !strings.Contains(err.Error(), "text file busy") {
		t.Fatalf("restart into a file being written: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	c, err = client.Connect(false)
	if err != nil {
		t.Fatalf("the service is gone: %v", err)
	}
	c.Close()
	if c.Start != start || m.runs() != running {
		t.Fatalf("the service restarted anyway: %s", m.runs())
	}
}
