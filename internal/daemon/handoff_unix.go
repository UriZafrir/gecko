//go:build !windows

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/session"
)

// handoff is what an upgrading daemon passes to its replacement. File
// descriptors stay open across exec; everything else is in this file.
type handoff struct {
	UnixFd   uintptr         `json:"unixFd"`
	TCPFd    uintptr         `json:"tcpFd,omitempty"`
	Sessions []session.State `json:"sessions"`
	Reap     []int           `json:"reap,omitempty"` // helper processes to wait for
}

const handoffEnv = "GECKO_HANDOFF"

func handoffDir() string { return filepath.Join(config.StateDir(), "handoff") }

// executable is the binary this daemon was started from, which an install
// may have replaced since.
func executable() (string, error) {
	exe, err := config.Executable()
	if err != nil {
		return "", err
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	if _, err := os.Stat(exe); err != nil {
		return "", fmt.Errorf("can't find the gecko binary to restart into (%s)", exe)
	}
	return exe, nil
}

// upgradeTarget is the binary to restart into: want, the gecko that asked
// (`gecko upgrade` sends its own path, so the service restarts into the
// one just installed even when it was started from another place), or
// this daemon's own path when want is "". It must start, which is checked
// here, before anything is handed over: a binary still being written
// (text file busy), a truncated one or another system's build fails now
// with that reason, and the daemon carries on as it was.
func upgradeTarget(want string) (string, error) {
	exe := want
	if exe == "" {
		var err error
		if exe, err = executable(); err != nil {
			return "", err
		}
	} else {
		if !filepath.IsAbs(exe) {
			return "", fmt.Errorf("can't restart into %s: not an absolute path", exe)
		}
		st, err := os.Stat(exe)
		if err != nil {
			return "", fmt.Errorf("can't restart into %s: %v", exe, err)
		}
		if !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
			return "", fmt.Errorf("can't restart into %s: not an executable file", exe)
		}
	}
	return exe, checkRuns(exe)
}

// busyWait is how long a binary that is still being written gets to be
// finished before a restart into it gives up.
var busyWait = 5 * time.Second

// checkRuns runs `exe version` and expects gecko to answer.
func checkRuns(exe string) error {
	deadline := time.Now().Add(busyWait)
	for {
		argv := []string{exe, "version"}
		if l := config.Linker(); l != "" {
			argv = append([]string{l}, argv...)
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = withoutEnv(os.Environ(), handoffEnv)
		cmd.WaitDelay = time.Second
		timer := time.AfterFunc(10*time.Second, func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
		out, err := cmd.Output()
		timer.Stop()
		switch {
		case err == nil && strings.HasPrefix(string(out), "gecko "):
			return nil
		case err == nil:
			return fmt.Errorf("can't restart into %s: it isn't gecko (it says %q)", exe, firstLine(out))
		case errors.Is(err, syscall.ETXTBSY) && time.Now().Before(deadline):
			time.Sleep(200 * time.Millisecond) // still being copied
			continue
		case errors.Is(err, syscall.ETXTBSY):
			return fmt.Errorf("can't restart into %s: text file busy (it is still being written; install it under another name and rename it over the old one)", exe)
		}
		return fmt.Errorf("can't restart into %s: it doesn't run here (%v)", exe, err)
	}
}

func firstLine(b []byte) string {
	s, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return s
}

func withoutEnv(env []string, key string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return out
}

// upgrade replaces this process with exe (see upgradeTarget). On success it
// never returns; sessions, their programs and both listening sockets carry
// over, and clients reconnect and resume where they were. On failure the
// daemon carries on with everything it had, remote links included.
func upgrade(exe string, m *session.Manager, h *hub.Hub, ln, web net.Listener) error {
	var err error
	dir := handoffDir()
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	hand := handoff{Reap: h.Children()}
	var keep []*os.File // duplicated listener fds, kept open until exec
	dup := func(l net.Listener) (uintptr, error) {
		fl, ok := l.(interface{ File() (*os.File, error) })
		if !ok {
			return 0, errors.New("listener can't be handed over")
		}
		f, err := fl.File()
		if err != nil {
			return 0, err
		}
		keep = append(keep, f)
		return f.Fd(), nil
	}
	if hand.UnixFd, err = dup(ln); err != nil {
		return err
	}
	if web != nil {
		if hand.TCPFd, err = dup(web); err != nil {
			return err
		}
	}
	// Remote links reconnect from the new process, or from this one again
	// if the restart fails: never closed for good, which left every machine
	// "connected" over a dead link ("write |1: file already closed").
	h.Suspend()

	states, thaw, err := m.Freeze()
	if err != nil {
		h.Resume()
		return err
	}
	fail := func(err error) error {
		thaw()
		for _, f := range keep {
			f.Close()
		}
		h.Resume()
		return err
	}
	for _, st := range states {
		if err := os.WriteFile(filepath.Join(dir, st.Info.ID+".ring"), st.Ring, 0o600); err != nil {
			return fail(err)
		}
	}
	hand.Sessions = states
	fds := []uintptr{hand.UnixFd}
	if hand.TCPFd != 0 {
		fds = append(fds, hand.TCPFd)
	}
	for _, st := range states {
		fds = append(fds, st.Fd)
	}
	for _, fd := range fds {
		if _, err := unix.FcntlInt(fd, unix.F_SETFD, 0); err != nil { // survive exec
			return fail(err)
		}
	}
	b, err := json.Marshal(hand)
	if err != nil {
		return fail(err)
	}
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, b, 0o600); err != nil {
		return fail(err)
	}
	env := append([]string{handoffEnv + "=" + statePath}, withoutEnv(os.Environ(), handoffEnv)...)
	log.Printf("upgrading in place: %s (%d sessions)", exe, len(states))
	argv0, argv := exe, append([]string{exe}, os.Args[1:]...)
	if l := config.Linker(); l != "" {
		// Started through Android's linker (Termux from Google Play): so again.
		argv0, argv = l, append([]string{exe}, argv...)
	}
	for i := 0; ; i++ {
		err = syscall.Exec(argv0, argv, env)
		if !errors.Is(err, syscall.ETXTBSY) || i >= 10 {
			break
		}
		time.Sleep(200 * time.Millisecond) // written to again since the check
	}
	return fail(fmt.Errorf("exec %s: %w", exe, err)) // only reached if exec failed
}

// readHandoff loads the state a previous process left for us, if any.
func readHandoff() (*handoff, error) {
	path := os.Getenv(handoffEnv)
	if path == "" {
		return nil, nil
	}
	os.Unsetenv(handoffEnv)
	defer os.RemoveAll(filepath.Dir(path))
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var hand handoff
	if err := json.Unmarshal(b, &hand); err != nil {
		return nil, err
	}
	for i := range hand.Sessions {
		st := &hand.Sessions[i]
		st.Ring, _ = os.ReadFile(filepath.Join(filepath.Dir(path), st.Info.ID+".ring"))
	}
	for _, fd := range append([]uintptr{hand.UnixFd, hand.TCPFd}, fdsOf(hand.Sessions)...) {
		if fd != 0 {
			unix.CloseOnExec(int(fd)) // don't leak them into shells we start
		}
	}
	return &hand, nil
}

func fdsOf(ss []session.State) []uintptr {
	out := make([]uintptr, len(ss))
	for i, s := range ss {
		out[i] = s.Fd
	}
	return out
}

// reap waits for helper processes the previous image started (ssh bridges),
// so they don't linger as zombies. They exit once their pipes close.
func reap(pids []int) {
	for _, pid := range pids {
		go func(pid int) {
			var ws syscall.WaitStatus
			for i := 0; i < 600; i++ {
				if p, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil); p == pid || err != nil {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
		}(pid)
	}
}
