package session

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/inspect"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/vt"
)

// A screen recorded from a real agent (see testdata/agents/README.md).
type fixture struct {
	agent, name  string
	cols, rows   int
	title, state string // the title and OSC 7501 state it had set
	lines        []string
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	files, _ := filepath.Glob("testdata/agents/*/*.txt")
	sort.Strings(files)
	var fs []fixture
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		head, body, ok := strings.Cut(string(b), "#---\n")
		if !ok {
			t.Fatalf("%s: no #--- line", f)
		}
		fx := fixture{agent: filepath.Base(filepath.Dir(f)), name: strings.TrimSuffix(filepath.Base(f), ".txt")}
		for _, l := range strings.Split(head, "\n") {
			k, v, _ := strings.Cut(strings.TrimPrefix(l, "# "), ": ")
			switch k {
			case "size":
				size, _, _ := strings.Cut(v, ",")
				c, r, _ := strings.Cut(size, "x")
				fx.cols, _ = strconv.Atoi(c)
				fx.rows, _ = strconv.Atoi(r)
			case "title":
				fx.title = v
			case "osc7501":
				fx.state = v
			}
		}
		fx.lines = strings.Split(strings.TrimSuffix(body, "\n"), "\n")
		fs = append(fs, fx)
	}
	return fs
}

// replay draws a fixture in a session the way the agent left it: its
// title, its state, its screen.
func (fx fixture) replay(t *testing.T) *Session {
	t.Helper()
	s := &Session{vt: vt.New(fx.cols, fx.rows, 100), info: proto.SessionInfo{Agent: &proto.AgentState{Name: fx.agent}}}
	s.vt.OnOSC = s.osc
	s.vt.Write([]byte("\x1b]0;" + fx.title + "\x07"))
	if fx.state != "" {
		s.vt.Write([]byte("\x1b]7501;" + fx.state + "\x07"))
	}
	s.vt.Write([]byte("\x1b[H\x1b[2J" + strings.Join(fx.lines, "\r\n")))
	got := s.vt.ScreenLines()
	if strings.Join(trimBlank(got), "\n") != strings.Join(trimBlank(fx.lines), "\n") {
		t.Fatalf("%s/%s: the screen doesn't replay:\n%s", fx.agent, fx.name, strings.Join(got, "\n"))
	}
	return s
}

// What Gecko reports for each recorded screen: the status (with what the
// agent's title and OSC 7501 state add), the status from the screen alone
// when that differs (inside tmux the state doesn't come through), the
// detail and the answers ("key=label").
var fixtureWant = []struct{ fixture, status, bare, detail, choices string }{
	{"codex/answered", "idle", "", "Got it, thanks.", ""},
	{"codex/approval-160x40", "needs-input", "", "Would you like to run the following command?", "1=Yes, proceed | 2=Yes, and don't ask again for commands that start with `ls` | 3=No, and tell Codex what to do differently"},
	{"codex/approval-50x30", "needs-input", "", "Would you like to run the following command?", "1=Yes, proceed | 2=Yes, and don't ask again for commands that | 3=No, and tell Codex what to do differently"},
	{"codex/approval-canceled", "idle", "", "Conversation interrupted", ""},
	{"codex/approval", "needs-input", "", "Would you like to run the following command?", "1=Yes, proceed | 2=Yes, and don't ask again for commands that start with `ls` | 3=No, and tell Codex what to do differently"},
	{"codex/approved-160x40", "idle", "", "The directory has one file: a.txt.", ""},
	{"codex/approved-50x30", "idle", "", "The directory has one file: a.txt.", ""},
	{"codex/approved", "idle", "", "The directory has one file: a.txt.", ""},
	{"codex/done", "idle", "", "Hi! How can I help you today?", ""},
	{"codex/idle-start-160x40", "idle", "", "", ""},
	{"codex/idle-start-50x30", "idle", "", "", ""},
	{"codex/idle-start", "idle", "", "", ""},
	{"codex/interrupted-160x40", "idle", "", "Conversation interrupted", ""},
	{"codex/interrupted-50x30", "idle", "", "Conversation interrupted", ""},
	{"codex/interrupted", "idle", "", "Conversation interrupted", ""},
	{"codex/login", "needs-input", "", "Sign in with ChatGPT to use Codex as part of your paid plan", "1=Sign in with ChatGPT | 2=Sign in with Device Code | 3=Provide your own API key"},
	{"codex/question", "needs-input", "", "Which color should the button be?", "1=Blue | 2=Green | 3=None of the above"},
	{"codex/trust-160x40", "needs-input", "", "Trust this folder?", "1=Trust and continue | 2=Quit"},
	{"codex/trust-50x30", "needs-input", "", "Trust this folder?", "1=Trust and continue | 2=Quit"},
	{"codex/trust", "needs-input", "", "Trust this folder?", "1=Trust and continue | 2=Quit"},
	{"codex/working-160x40", "working", "", "Working", ""},
	{"codex/working-50x30", "working", "", "Working", ""},
	{"codex/working-streaming", "working", "idle", "Counting slowly: 1 2", ""},
	{"codex/working", "working", "", "Working", ""},
	{"opencode/always-allow", "needs-input", "", "Always allow: This will allow the following patterns until OpenCode is restarted", "enter=Confirm | \x1b[C\r=Cancel"},
	{"opencode/answered", "idle", "", "Got it, thanks.", ""},
	{"opencode/approved-160x40", "idle", "", "The directory has one file: a.txt.", ""},
	{"opencode/approved-50x30", "idle", "", "The directory has one file: a.txt.", ""},
	{"opencode/approved", "idle", "", "The directory has one file: a.txt.", ""},
	{"opencode/done", "idle", "", "Hi! How can I help you today?", ""},
	{"opencode/home-160x40", "idle", "", "", ""},
	{"opencode/home-50x30", "idle", "", "", ""},
	{"opencode/home", "idle", "", "", ""},
	{"opencode/interrupt-pending", "working", "", "Counting slowly: 1 2 3 4 5 6", ""},
	{"opencode/interrupted-160x40", "idle", "", "Interrupted", ""},
	{"opencode/interrupted-50x30", "idle", "", "Interrupted", ""},
	{"opencode/interrupted", "idle", "", "Interrupted", ""},
	{"opencode/permission-160x40", "needs-input", "", "Permission required: $ ls", "enter=Allow once | \x1b[C\r=Allow always | \x1b[C\x1b[C\r=Reject"},
	{"opencode/permission-50x30", "needs-input", "", "Permission required: $ ls", "enter=Allow once | \x1b[C\r=Allow always | \x1b[C\x1b[C\r=Reject"},
	{"opencode/permission", "needs-input", "", "Permission required: $ ls", "enter=Allow once | \x1b[C\r=Allow always | \x1b[C\x1b[C\r=Reject"},
	{"opencode/question-160x40", "needs-input", "", "Which color should the button be?", "1=Blue | 2=Green | 3=Type your own answer"},
	{"opencode/question-50x30", "needs-input", "", "Which color should the button be?", "1=Blue | 2=Green | 3=Type your own answer"},
	{"opencode/question", "needs-input", "", "Which color should the button be?", "1=Blue | 2=Green | 3=Type your own answer"},
	{"opencode/real-approved", "idle", "", "The directory contains one file:", ""},
	{"opencode/real-done", "idle", "", "Hi! 👋 How can I help you today?", ""},
	{"opencode/real-interrupted", "idle", "", "Interrupted", ""},
	{"opencode/real-permission", "needs-input", "", "Permission required: $ ls", "enter=Allow once | \x1b[C\r=Allow always | \x1b[C\x1b[C\r=Reject"},
	{"opencode/real-working", "working", "", "", ""},
	{"opencode/rejected", "idle", "", "$ ls", ""},
	{"opencode/working-160x40", "working", "", "", ""},
	{"opencode/working-50x30", "working", "", "", ""},
	{"opencode/working-streaming", "working", "", "Counting slowly: 1 2 3 4 5 6", ""},
	{"opencode/working", "working", "", "", ""},
}

func TestAgentFixtures(t *testing.T) {
	want := map[string]int{}
	for i, w := range fixtureWant {
		want[w.fixture] = i
	}
	seen := map[string]bool{}
	for _, fx := range loadFixtures(t) {
		key := fx.agent + "/" + fx.name
		i, ok := want[key]
		if !ok {
			t.Errorf("%s: no expectation in fixtureWant", key)
			continue
		}
		seen[key] = true
		w := fixtureWant[i]
		if w.bare == "" {
			w.bare = w.status
		}
		s := fx.replay(t)
		name, hint := s.agentOf(inspect.Info{Kind: "agent", Agent: fx.agent, Pid: 1}, "", "")
		if name != fx.agent {
			t.Fatalf("%s: agent %q", key, name)
		}
		// The same whether the agent just drew (a blinking title, a
		// timer) or has been quiet: output alone says nothing.
		for _, since := range []time.Duration{100 * time.Millisecond, 10 * time.Second} {
			st := agentStatusOf(name, s.vt.ScreenLines(), since, time.Hour, false)
			bare := st.Status
			st.Status = withHint(st.Status, hint)
			if st.Status != w.status || bare != w.bare || st.Detail != w.detail || choiceList(st.Choices) != w.choices {
				t.Errorf("%s, output %v ago:\n got %s (screen %s) %q [%s]\nwant %s (screen %s) %q [%s]",
					key, since, st.Status, bare, st.Detail, choiceList(st.Choices), w.status, w.bare, w.detail, w.choices)
			}
		}
	}
	for _, w := range fixtureWant {
		if !seen[w.fixture] {
			t.Errorf("%s: no such fixture", w.fixture)
		}
	}
}

func choiceList(cs []proto.Choice) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Key+"="+c.Label)
	}
	return strings.Join(out, " | ")
}

// The bug: Codex and OpenCode waiting for an answer, or stopped with Esc,
// read as working, because the general rules took a hint like "esc to
// cancel" for a working marker and took output (Codex blinks its title
// while it waits) for work. They don't now, however recent the output.
func TestAgentPausedWhileRedrawing(t *testing.T) {
	paused := map[string]string{
		"codex/approval":       "needs-input",
		"codex/question":       "needs-input",
		"codex/interrupted":    "idle",
		"opencode/permission":  "needs-input",
		"opencode/question":    "needs-input",
		"opencode/interrupted": "idle",
		"opencode/home":        "idle",
	}
	for _, fx := range loadFixtures(t) {
		want, ok := paused[fx.agent+"/"+fx.name]
		if !ok {
			continue
		}
		delete(paused, fx.agent+"/"+fx.name)
		screen := fx.replay(t).vt.ScreenLines()
		old := agentStatus(screen, 0, time.Hour, false).Status
		if got := agentStatusOf(fx.agent, screen, 0, time.Hour, false).Status; got != want {
			t.Errorf("%s/%s, drawing now: %s, want %s (the general rules say %s)", fx.agent, fx.name, got, want, old)
		}
	}
	for k := range paused {
		t.Errorf("%s: no such fixture", k)
	}
}
