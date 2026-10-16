// Package tmux queries the local tmux server.
package tmux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gecko-term/gecko/internal/execx"
)

func run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := execx.CommandContext(ctx, "tmux", args...)
	cmd.Env = cleanEnv()
	out, err := cmd.Output()
	return string(out), err
}

// sep separates fields in tmux -F formats. It must be printable: without a
// UTF-8 locale (e.g. a daemon started over a non-interactive ssh) tmux
// prints control characters such as tab as "_".
const sep = "|~|"

// cleanEnv drops TMUX so queries hit the default server even when the
// daemon itself was started inside tmux, and makes sure tmux runs with a
// UTF-8 locale so non-ASCII session and window names survive.
func cleanEnv() []string {
	var env []string
	utf8 := false
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TMUX=") || strings.HasPrefix(e, "TMUX_PANE=") {
			continue
		}
		k, v, _ := strings.Cut(e, "=")
		if (k == "LC_ALL" || k == "LC_CTYPE" || k == "LANG") && strings.Contains(strings.ReplaceAll(strings.ToUpper(v), "-", ""), "UTF8") {
			utf8 = true
		}
		env = append(env, e)
	}
	if !utf8 {
		env = append(env, "LC_CTYPE=C.UTF-8")
	}
	return env
}

// fields splits one line of tmux -F output.
func fields(line string) []string { return strings.Split(line, sep) }

// fieldsN splits a line into at most n fields; the last one (a title, say)
// keeps any separator it contains.
func fieldsN(line string, n int) []string { return strings.SplitN(line, sep, n) }

// format joins tmux format variables with sep.
func format(vars ...string) string { return strings.Join(vars, sep) }

// Available reports whether tmux is installed.
func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// Session is a tmux session.
type Session struct {
	Name     string
	Windows  int
	Attached int
}

// Window is a tmux window.
type Window struct {
	Session string
	Index   int
	Name    string
	Active  bool
	Panes   int
	Command string // what runs in its active pane
	PanePid int    // first process of its active pane
	Title   string // title of its active pane (set by OSC 0/2 inside it)
}

// Windows lists the windows of every session.
func Windows() []Window {
	out, err := run("list-windows", "-a", "-F",
		format("#{session_name}", "#{window_index}", "#{window_name}", "#{window_active}", "#{window_panes}", "#{pane_current_command}", "#{pane_pid}", "#{pane_title}"))
	if err != nil {
		return nil
	}
	var ws []Window
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := fieldsN(l, 8)
		if len(f) < 6 {
			continue
		}
		idx, _ := strconv.Atoi(f[1])
		panes, _ := strconv.Atoi(f[4])
		w := Window{Session: f[0], Index: idx, Name: f[2], Active: f[3] == "1", Panes: panes, Command: f[5]}
		if len(f) == 8 { // older tmux may lack pane_title
			w.PanePid, _ = strconv.Atoi(f[6])
			w.Title = f[7]
		}
		ws = append(ws, w)
	}
	return ws
}

// Do runs one of the actions Gecko offers on tmux. target is
// "session" or "session:window"; arg is a name where one is needed;
// client is the tty of the tmux client to act on (switch-client).
func Do(action, target, arg, client string) error {
	var args []string
	switch action {
	case "select-window":
		args = []string{"select-window", "-t", target}
	case "new-window":
		args = []string{"new-window", "-t", target + ":"}
		if arg != "" {
			args = append(args, "-n", arg)
		}
	case "rename-window":
		args = []string{"rename-window", "-t", target, arg}
	case "rename-session":
		args = []string{"rename-session", "-t", target, arg}
	case "kill-window":
		args = []string{"kill-window", "-t", target}
	case "switch-client":
		args = []string{"switch-client", "-c", client, "-t", target}
	case "new-session":
		args = []string{"new-session", "-d", "-s", target}
	default:
		return errors.New("unknown tmux action " + action)
	}
	out, err := runErr(args...)
	if err != nil {
		if msg := strings.TrimSpace(out); msg != "" {
			return errors.New("tmux: " + msg)
		}
		return err
	}
	return nil
}

// runErr is run with stderr included in the output.
func runErr(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := execx.CommandContext(ctx, "tmux", args...)
	cmd.Env = cleanEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Sessions lists tmux sessions (nil if no server is running).
func Sessions() []Session {
	out, err := run("list-sessions", "-F", format("#{session_name}", "#{session_windows}", "#{session_attached}"))
	if err != nil {
		return nil
	}
	var ss []Session
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := fields(l)
		if len(f) != 3 {
			continue
		}
		w, _ := strconv.Atoi(f[1])
		a, _ := strconv.Atoi(f[2])
		ss = append(ss, Session{Name: f[0], Windows: w, Attached: a})
	}
	return ss
}

// Pane is a tmux pane.
type Pane struct {
	ID      string // %3
	Target  string // session:window.pane
	TTY     string
	Pid     int
	Command string
	Cwd     string
}

// Panes lists all panes on the server.
func Panes() []Pane {
	out, err := run("list-panes", "-a", "-F",
		format("#{pane_id}", "#{session_name}:#{window_index}.#{pane_index}", "#{pane_tty}", "#{pane_pid}", "#{pane_current_command}", "#{pane_current_path}"))
	if err != nil {
		return nil
	}
	var ps []Pane
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := fields(l)
		if len(f) != 6 {
			continue
		}
		pid, _ := strconv.Atoi(f[3])
		ps = append(ps, Pane{ID: f[0], Target: f[1], TTY: f[2], Pid: pid, Command: f[4], Cwd: f[5]})
	}
	return ps
}

// Client is a tmux client and the pane it shows.
type Client struct {
	TTY     string
	Session string
	PanePid int    // first process of the pane it shows
	Title   string // that pane's title
}

// Clients lists the clients attached to the server.
func Clients() []Client {
	out, err := run("list-clients", "-F", format("#{client_tty}", "#{session_name}", "#{pane_pid}", "#{pane_title}"))
	if err != nil {
		return nil
	}
	var cs []Client
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := fieldsN(l, 4)
		if len(f) < 2 || f[0] == "" {
			continue
		}
		c := Client{TTY: f[0], Session: f[1]}
		if len(f) == 4 {
			c.PanePid, _ = strconv.Atoi(f[2])
			c.Title = f[3]
		}
		cs = append(cs, c)
	}
	return cs
}

// ClientOn returns the client on tty, if any.
func ClientOn(cs []Client, tty string) (Client, bool) {
	for _, c := range cs {
		if c.TTY == tty {
			return c, true
		}
	}
	return Client{}, false
}

// ClientSession returns the tmux session shown by the client on tty.
func ClientSession(tty string) string {
	c, _ := ClientOn(Clients(), tty)
	return c.Session
}

// Capture returns the text of a pane (target may be a session name, which
// means its active pane) including up to history lines of scrollback.
func Capture(target string, history int) (string, error) {
	return run("capture-pane", "-p", "-J", "-t", target, "-S", strconv.Itoa(-history))
}
