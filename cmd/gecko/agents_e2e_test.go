package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/agentmock"
	"github.com/gecko-term/gecko/internal/proto"
)

// The agents end to end: a Gecko daemon of its own (a fresh GECKO_HOME and
// port) runs the real Codex and OpenCode in a tab, against the stand-in
// model of internal/agentmock, and `gecko agents --json` must say what
// each does at every step: at its prompt, working, stopped with Esc,
// asking to run a command, asking a question. Skipped when the agent
// isn't installed (GECKO_TEST_CODEX and GECKO_TEST_OPENCODE name the
// executables when they aren't on PATH) and with -short.

func TestCodexInGecko(t *testing.T) {
	bin := agentBinary(t, "codex", "GECKO_TEST_CODEX")
	g := startGecko(t)
	m := startMock(t)
	home := t.TempDir()
	// Codex starts an app-server of its own from its home, which outlives it.
	t.Cleanup(func() { _ = exec.Command("pkill", "-f", home).Run() })
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"mock-model\"\nmodel_provider = \"mock\"\napproval_policy = \"on-request\"\nsandbox_mode = \"read-only\"\n"), 0o644)
	id := g.newTab(t, demoDir(t), "env", "CODEX_HOME="+home, bin, "-c", `model_providers.mock={name="Mock",base_url="`+m.URL+`",wire_api="responses"}`)

	a := g.wait(t, id, "needs-input", "trusting the folder", 60*time.Second)
	if a.Detail != "Trust this folder?" || choices(a) != "1=Trust and continue | 2=Quit" {
		t.Fatalf("trust: %+v", a)
	}
	g.send(t, id, "enter")
	g.wait(t, id, "idle", "at its prompt", 30*time.Second)

	g.prompt(t, id, "say hi")
	g.waitDetail(t, id, "idle", agentmock.Greeting, 30*time.Second)

	g.prompt(t, id, "count slow please")
	g.waitDetail(t, id, "working", "Counting slowly:", 30*time.Second)
	g.send(t, id, "esc")
	g.waitDetail(t, id, "idle", "Conversation interrupted", 15*time.Second)
	g.stays(t, id, "idle", 3*time.Second)

	g.prompt(t, id, "list files here")
	a = g.wait(t, id, "needs-input", "asking to run ls", 30*time.Second)
	if a.Detail != "Would you like to run the following command?" || !strings.HasPrefix(choices(a), "1=Yes, proceed | 2=Yes, and don't ask again") {
		t.Fatalf("approval: %+v", a)
	}
	// Its title blinks all the while: still waiting, not working.
	g.stays(t, id, "needs-input", 3*time.Second)
	g.send(t, id, "1")
	g.waitDetail(t, id, "idle", agentmock.ListDone, 30*time.Second)

	g.prompt(t, id, "/plan")
	g.prompt(t, id, "ask me something")
	a = g.wait(t, id, "needs-input", "asking a question", 30*time.Second)
	if a.Detail != agentmock.Question || choices(a) != "1=Blue | 2=Green | 3=None of the above" {
		t.Fatalf("question: %+v", a)
	}
	g.stays(t, id, "needs-input", 2*time.Second)
	g.send(t, id, "2")
	g.waitDetail(t, id, "idle", agentmock.Answered, 30*time.Second)
}

func TestOpenCodeInGecko(t *testing.T) {
	bin := agentBinary(t, "opencode", "GECKO_TEST_OPENCODE")
	g := startGecko(t)
	m := startMock(t)
	dirs := t.TempDir()
	// OpenCode fetches the provider package for the stand-in once; keep it.
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = dirs
	}
	config := `{"provider":{"mock":{"npm":"@ai-sdk/openai-compatible","name":"Mock","options":{"baseURL":"` + m.URL + `"},"models":{"mock-model":{"name":"Mock Model"}}}},` +
		`"model":"mock/mock-model","small_model":"mock/mock-model","permission":{"bash":"ask"},"autoupdate":false,"share":"disabled"}`
	id := g.newTab(t, demoDir(t), "env", "OPENCODE_CONFIG_CONTENT="+config,
		"XDG_DATA_HOME="+filepath.Join(dirs, "data"), "XDG_STATE_HOME="+filepath.Join(dirs, "state"),
		"XDG_CONFIG_HOME="+filepath.Join(dirs, "config"), "XDG_CACHE_HOME="+filepath.Join(cache, "gecko-test", "opencode"), bin)

	g.waitScreen(t, id, "Ask anything", 60*time.Second)
	// Its probe for notifications on start isn't a question.
	g.wait(t, id, "idle", "at its prompt", 10*time.Second)
	g.stays(t, id, "idle", 2*time.Second)

	g.prompt(t, id, "say hi")
	g.waitDetail(t, id, "idle", agentmock.Greeting, 60*time.Second)

	g.prompt(t, id, "count slow please")
	g.waitDetail(t, id, "working", "Counting slowly:", 30*time.Second)
	g.send(t, id, "esc")
	g.stays(t, id, "working", time.Second) // one Esc only asks for another
	g.send(t, id, "esc")
	g.waitDetail(t, id, "idle", "Interrupted", 15*time.Second)
	g.stays(t, id, "idle", 3*time.Second)

	g.prompt(t, id, "list files here")
	a := g.wait(t, id, "needs-input", "asking to run ls", 30*time.Second)
	if a.Detail != "Permission required: $ ls" || choices(a) != "enter=Allow once | \x1b[C\r=Allow always | \x1b[C\x1b[C\r=Reject" {
		t.Fatalf("permission: %+v", a)
	}
	g.stays(t, id, "needs-input", 2*time.Second)
	g.send(t, id, a.Choices[0].Key)
	g.waitDetail(t, id, "idle", agentmock.ListDone, 30*time.Second)

	g.prompt(t, id, "ask me something")
	a = g.wait(t, id, "needs-input", "asking a question", 30*time.Second)
	if a.Detail != agentmock.Question || choices(a) != "1=Blue | 2=Green | 3=Type your own answer" {
		t.Fatalf("question: %+v", a)
	}
	g.send(t, id, "2")
	g.waitDetail(t, id, "idle", agentmock.Answered, 30*time.Second)
}

// agentBinary finds an agent's executable or skips the test.
func agentBinary(t *testing.T, name, env string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("starts a Gecko daemon and runs " + name + "; not with -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the daemon's tabs run under a Unix pty here")
	}
	if p := os.Getenv(env); p != "" {
		return p
	}
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed (not on PATH, and %s is not set)", name, env)
	}
	return p
}

func startMock(t *testing.T) *agentmock.Server {
	m, err := agentmock.Start("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

// demoDir is where the agent runs: a git repository with one file, a.txt.
func demoDir(t *testing.T) string {
	d := filepath.Join(t.TempDir(), "gecko-agent-demo")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "a.txt"), []byte("hello\n"), 0o644)
	_ = exec.Command("git", "-C", d, "init", "-q").Run()
	return d
}

type gecko struct {
	bin, home string
}

// startGecko builds this gecko and starts a daemon of its own.
func startGecko(t *testing.T) *gecko {
	t.Helper()
	dir := t.TempDir()
	g := &gecko{bin: filepath.Join(dir, "gecko"), home: filepath.Join(dir, "home")}
	os.MkdirAll(g.home, 0o700)
	if out, err := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", g.bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	d := exec.Command(g.bin, "daemon", "--listen", addr)
	d.Env = append(os.Environ(), "GECKO_HOME="+g.home, "SHELL=/bin/sh", "PS1=$ ")
	log, _ := os.Create(filepath.Join(dir, "daemon.log"))
	d.Stdout, d.Stderr = log, log
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = g.run("stop")
		done := make(chan struct{})
		go func() { d.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			d.Process.Kill()
		}
		log.Close()
	})
	for end := time.Now().Add(15 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if _, err := g.run("ls"); err == nil {
			return g
		}
		if time.Now().After(end) {
			b, _ := os.ReadFile(log.Name())
			t.Fatalf("the daemon did not start:\n%s", b)
		}
	}
}

func (g *gecko) run(args ...string) (string, error) {
	c := exec.Command(g.bin, args...)
	c.Env = append(os.Environ(), "GECKO_HOME="+g.home)
	out, err := c.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("gecko %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func (g *gecko) newTab(t *testing.T, dir string, argv ...string) string {
	t.Helper()
	out, err := g.run(append([]string{"new", "-C", dir, "--exec", "--"}, argv...)...)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(out)
	t.Cleanup(func() { g.run("kill", id) })
	return id
}

// send presses an answer's key, as the agent dock does.
func (g *gecko) send(t *testing.T, id, key string) {
	t.Helper()
	keys := map[string]string{"enter": "\r", "esc": "\x1b"}
	if k, ok := keys[key]; ok {
		key = k
	}
	if _, err := g.run("send", "--no-enter", id, key); err != nil {
		t.Fatal(err)
	}
}

// prompt types a prompt, then Enter on its own: typed at once, Codex
// takes it for a paste and the Enter for a new line.
func (g *gecko) prompt(t *testing.T, id, text string) {
	t.Helper()
	g.send(t, id, text)
	time.Sleep(300 * time.Millisecond)
	g.send(t, id, "enter")
}

func (g *gecko) agent(id string) (proto.AgentState, bool) {
	out, err := g.run("agents", "--json")
	if err != nil {
		return proto.AgentState{}, false
	}
	var rows []struct {
		Where string           `json:"where"`
		Agent proto.AgentState `json:"agent"`
	}
	if json.Unmarshal([]byte(out), &rows) != nil {
		return proto.AgentState{}, false
	}
	for _, r := range rows {
		if r.Where == id {
			return r.Agent, true
		}
	}
	return proto.AgentState{}, false
}

func (g *gecko) screen(id string) string {
	out, _ := g.run("capture", id)
	lines := strings.Split(out, "\n")
	return strings.Join(lines[max(0, len(lines)-30):], "\n")
}

func (g *gecko) poll(t *testing.T, id, what string, d time.Duration, ok func(proto.AgentState) bool) proto.AgentState {
	t.Helper()
	var last proto.AgentState
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if a, found := g.agent(id); found {
			last = a
			if ok(a) {
				return a
			}
		}
	}
	t.Fatalf("%s: still %s %q %v after %v; the screen:\n%s", what, last.Status, last.Detail, last.Choices, d, g.screen(id))
	return last
}

func (g *gecko) wait(t *testing.T, id, status, what string, d time.Duration) proto.AgentState {
	t.Helper()
	return g.poll(t, id, what+" ("+status+")", d, func(a proto.AgentState) bool { return a.Status == status })
}

func (g *gecko) waitDetail(t *testing.T, id, status, detail string, d time.Duration) proto.AgentState {
	t.Helper()
	return g.poll(t, id, status+" with "+detail, d, func(a proto.AgentState) bool {
		return a.Status == status && strings.Contains(a.Detail, detail)
	})
}

// stays checks the status holds for a while, the agent redrawing or not.
func (g *gecko) stays(t *testing.T, id, status string, d time.Duration) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if a, _ := g.agent(id); a.Status != status {
			t.Fatalf("was %s, then %s %q; the screen:\n%s", status, a.Status, a.Detail, g.screen(id))
		}
	}
}

func (g *gecko) waitScreen(t *testing.T, id, text string, d time.Duration) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if strings.Contains(g.screen(id), text) {
			return
		}
	}
	t.Fatalf("no %q on screen after %v:\n%s", text, d, g.screen(id))
}

func choices(a proto.AgentState) string {
	var out []string
	for _, c := range a.Choices {
		out = append(out, c.Key+"="+c.Label)
	}
	return strings.Join(out, " | ")
}
