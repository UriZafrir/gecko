package session

import (
	"testing"

	"github.com/gecko-term/gecko/internal/inspect"
	"github.com/gecko-term/gecko/internal/vt"
)

// Title sequences recorded from Claude Code 2.1.302 in a pty: at start, while
// it works on a prompt, back at its prompt, and on /exit.
const (
	claudeStart   = "\x1b]7501;?\x07\x1b]0;✳ Claude Code\x07\x1b]7501;?\x07"
	claudeWorking = "\x1b]0;◐ Claude Code\x07\x1b]0;◐ Terminal poem and gecko echo\x07\x1b]0;◑ Terminal poem and gecko echo\x07"
	claudeIdle    = "\x1b]0;✳ Terminal poem and gecko echo\x07"
	claudeExit    = "\x1b]0;\x07"
)

func titleSession() *Session {
	s := &Session{vt: vt.New(80, 24, 100)}
	s.vt.OnOSC = s.osc
	return s
}

func TestTitleAgentOverSSH(t *testing.T) {
	s := titleSession()
	ssh := inspect.Info{Kind: "ssh", Proc: "ssh", Pid: 42}
	check := func(step, wantName, wantHint string) {
		t.Helper()
		if n, h := s.agentOf(ssh, "", ""); n != wantName || h != wantHint {
			t.Fatalf("%s: agentOf = %q %q, want %q %q", step, n, h, wantName, wantHint)
		}
	}
	check("before", "", "")
	s.vt.Write([]byte("user@box:~$ claude\r\n" + claudeStart))
	check("start", "claude", "idle")
	s.vt.Write([]byte(claudeWorking))
	check("working", "claude", "working")
	s.vt.Write([]byte(claudeIdle))
	check("idle", "claude", "idle")
	s.vt.Write([]byte(claudeExit))
	check("exit", "", "")

	// Gone when the process it ran under is.
	s.vt.Write([]byte(claudeStart))
	check("again", "claude", "idle")
	if n, _ := s.agentOf(inspect.Info{Kind: "ssh", Pid: 43}, "", ""); n != "" {
		t.Fatalf("another ssh: %q", n)
	}
	check("stays gone", "", "")

	// ... when the local shell is back at its prompt ...
	s.vt.Write([]byte(claudeStart))
	if n, _ := s.agentOf(inspect.Info{Kind: "shell"}, "", ""); n != "" {
		t.Fatalf("shell: %q", n)
	}
	// ... or when a shell prompt (OSC 133) shows.
	s.vt.Write([]byte(claudeStart))
	check("again", "claude", "idle")
	s.vt.Write([]byte("\x1b]133;A\x07"))
	check("prompt", "", "")
}

func TestTitleAgentFalsePositives(t *testing.T) {
	s := titleSession()
	ssh := inspect.Info{Kind: "ssh", Pid: 7}
	for _, title := range []string{"user@box: ~", "vim main.go", "◐ building", "⠋ npm install", "✦ thinking (x)", "Claude Codex"} {
		s.vt.Write([]byte("\x1b]0;" + title + "\x07"))
		if n, _ := s.agentOf(ssh, "", ""); n != "" {
			t.Errorf("title %q: agent %q", title, n)
		}
	}
	// A spinner title only keeps an agent already seen.
	s.vt.Write([]byte(claudeStart + "\x1b]2;◐ Fixing the tests\x07"))
	if n, h := s.agentOf(ssh, "", ""); n != "claude" || h != "working" {
		t.Errorf("spinner after start: %q %q", n, h)
	}
	s.vt.Write([]byte("\x1b]2;user@box: ~\x07"))
	if n, _ := s.agentOf(ssh, "", ""); n != "" {
		t.Errorf("after the shell's title: %q", n)
	}
}

func TestAgentOfPrecedence(t *testing.T) {
	s := titleSession()
	s.vt.Write([]byte(claudeWorking[:len("\x1b]0;◐ Claude Code\x07")]))
	// A local claude process wins and takes its status hint from the title.
	if n, h := s.agentOf(inspect.Info{Kind: "agent", Agent: "claude", Pid: 5}, "", ""); n != "claude" || h != "working" {
		t.Errorf("process: %q %q", n, h)
	}
	// A tmux client: only the pane counts, not the outer title.
	if n, _ := s.agentOf(inspect.Info{Kind: "tmux", Proc: "tmux", Pid: 6}, "", ""); n != "" {
		t.Errorf("tmux, no pane agent: %q", n)
	}
	if n, h := s.agentOf(inspect.Info{Kind: "tmux", Proc: "tmux", Pid: 6}, "codex", ""); n != "codex" || h != "" {
		t.Errorf("tmux pane: %q %q", n, h)
	}
}

func TestWithHint(t *testing.T) {
	for _, c := range [][3]string{{"idle", "working", "working"}, {"needs-input", "working", "needs-input"}, {"idle", "needs-input", "needs-input"}, {"working", "idle", "working"}, {"idle", "", "idle"}} {
		if got := withHint(c[0], c[1]); got != c[2] {
			t.Errorf("withHint(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestWithoutTmuxStatus(t *testing.T) {
	got := withoutTmuxStatus([]string{"❯ hi", "", "[main] 0:claude* 1:bash-", ""})
	if len(got) != 2 || got[0] != "❯ hi" {
		t.Fatalf("%q", got)
	}
}
