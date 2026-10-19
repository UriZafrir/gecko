package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/proto"
)

// Make anchor, end to end: real gecko daemons (this checkout, built once),
// each with its own GECKO_HOME and a port from 7940 up, never the user's
// service. A "phone" (Android's This phone, or any Gecko a page opens)
// reaches "ubuntu" by its Gecko link or over SSH (a throwaway sshd), and a
// page connected to the phone makes ubuntu its anchor.

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

func geckoBin(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("starts real daemons")
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gecko-anchor-bin-")
		if err != nil {
			buildErr = err
			return
		}
		builtBin = filepath.Join(dir, "gecko")
		out, err := exec.Command("go", "build", "-o", builtBin, ".").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBin
}

type daemonProc struct {
	name, home, base, token string
}

// link is the daemon's Gecko link, as `gecko url` prints it.
func (d *daemonProc) link() string { return d.base + "/?token=" + d.token }

func startDaemon(t *testing.T, name string, port int, hosts []config.Host) *daemonProc {
	t.Helper()
	bin := geckoBin(t)
	home, err := os.MkdirTemp("", "gk-"+name+"-") // short: the socket path has a length limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	cfg, _ := json.Marshal(map[string]any{"name": name, "hosts": hosts})
	if err := os.WriteFile(filepath.Join(home, "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"theme":"tokyo-night"}`), 0o600)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cmd := exec.Command(bin, "daemon", "--listen", addr)
	cmd.Env = append(os.Environ(), "GECKO_HOME="+home, "SHELL=/bin/sh", "PS1=$ ")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
	d := &daemonProc{name: name, home: home, base: "http://" + addr}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(home, "token")); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
			d.token = strings.TrimSpace(string(b))
			if r, err := http.Get(d.base + "/"); err == nil {
				r.Body.Close()
				return d
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("daemon %s didn't start on %s", name, addr)
	return nil
}

// page is a web client of a daemon (the web UI, minus the drawing).
type page struct {
	t     *testing.T
	ws    *websocket.Conn
	wmu   sync.Mutex
	rid   int
	msgs  chan proto.Msg
	hello proto.Msg

	mu       sync.Mutex
	hosts    []proto.HostInfo
	sessions map[string]proto.SessionInfo
}

func openPage(t *testing.T, d *daemonProc, anchor ...string) *page {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(d.base, "http")+"/ws", http.Header{"Authorization": {"Bearer " + d.token}})
	if err != nil {
		t.Fatal(err)
	}
	p := &page{t: t, ws: ws, msgs: make(chan proto.Msg, 4096), sessions: map[string]proto.SessionInfo{}}
	t.Cleanup(func() { ws.Close() })
	go p.read()
	p.send(&proto.Msg{T: proto.THello, Client: "web", Version: proto.Version, Anchor: anchor})
	p.hello = p.wait("hello", func(m proto.Msg) bool { return m.T == proto.THello })
	return p
}

func (p *page) read() {
	for {
		typ, b, err := p.ws.ReadMessage()
		if err != nil {
			close(p.msgs)
			return
		}
		var m proto.Msg
		if typ != websocket.TextMessage || json.Unmarshal(b, &m) != nil {
			continue
		}
		p.mu.Lock()
		switch m.T {
		case proto.TState:
			p.hosts = m.Hosts
		case proto.TSessions:
			if m.Host == "" {
				p.sessions = map[string]proto.SessionInfo{}
			}
			for _, s := range m.Sessions {
				p.sessions[s.ID] = s
			}
		case proto.TSession:
			p.sessions[m.Session.ID] = *m.Session
		}
		p.mu.Unlock()
		p.msgs <- m
	}
}

func (p *page) send(m *proto.Msg) {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	b, _ := json.Marshal(m)
	_ = p.ws.WriteMessage(websocket.TextMessage, b)
}

func (p *page) wait(what string, ok func(proto.Msg) bool) proto.Msg {
	p.t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case m, open := <-p.msgs:
			if !open {
				p.t.Fatalf("connection closed waiting for %s", what)
			}
			if ok(m) {
				return m
			}
		case <-timeout:
			p.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (p *page) call(m proto.Msg) proto.Msg {
	p.t.Helper()
	p.wmu.Lock()
	p.rid++
	m.Rid = p.rid
	p.wmu.Unlock()
	p.send(&m)
	return p.wait(string(m.T)+" reply", func(r proto.Msg) bool { return r.T == proto.TReply && r.Rid == m.Rid })
}

// eventually waits until ok holds for the hosts and sessions this page has.
func (p *page) eventually(what string, ok func(hosts []proto.HostInfo, ss map[string]proto.SessionInfo) bool) {
	p.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		good := ok(p.hosts, p.sessions)
		p.mu.Unlock()
		if good {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.t.Fatalf("never: %s (hosts %+v)", what, p.hosts)
}

func hostNamed(hs []proto.HostInfo, name string) *proto.HostInfo {
	for i := range hs {
		if hs[i].Name == name {
			return &hs[i]
		}
	}
	return nil
}

func tabNamed(ss map[string]proto.SessionInfo, host, name string) bool {
	for _, s := range ss {
		if s.Host == host && s.Name == name {
			return true
		}
	}
	return false
}

// checkSwitch makes ubuntu the anchor of a page on phone and back, and
// checks what each side sees.
func writeSettings(t *testing.T, d *daemonProc, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(d.home, "settings.json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func settingsOf(t *testing.T, text string) map[string]any {
	t.Helper()
	var s map[string]any
	if err := json.Unmarshal([]byte(text), &s); err != nil {
		t.Fatalf("settings %q: %v", text, err)
	}
	return s
}

func waitSettings(t *testing.T, d *daemonProc, ok func(map[string]any) bool) {
	t.Helper()
	var b []byte
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		b, _ = os.ReadFile(filepath.Join(d.home, "settings.json"))
		var s map[string]any
		if json.Unmarshal(b, &s) == nil && ok(s) {
			return
		}
	}
	t.Fatalf("%s's settings: %s", d.name, b)
}

func checkSwitch(t *testing.T, phone, ubuntu *daemonProc) {
	t.Helper()
	// The phone's own page: the phone is the anchor, ubuntu one of its machines.
	home := openPage(t, phone)
	if home.hello.Host != "phone" || home.hello.Door != "" || home.hello.Version != proto.Version {
		t.Fatalf("phone's hello: %+v", home.hello)
	}
	home.eventually("ubuntu connected on the phone", func(hs []proto.HostInfo, _ map[string]proto.SessionInfo) bool {
		u := hostNamed(hs, "ubuntu")
		return u != nil && u.Status == "connected"
	})
	if r := home.call(proto.Msg{T: proto.TCreate, Create: &proto.CreateReq{Name: "phone-tab", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24}}); r.Error != "" {
		t.Fatal(r.Error)
	}
	// Different looks on each: the window's settings are the phone's.
	writeSettings(t, phone, `{"theme":"gecko-light","fontSize":17,"order":["phone-order"]}`)
	writeSettings(t, ubuntu, `{"theme":"tokyo-night","fontSize":12,"order":["ubuntu-order"]}`)
	// ubuntu has a machine of its own the phone doesn't (by its link).
	// Make anchor on ubuntu: the page says hello to the phone with
	// anchor [ubuntu] and lands on ubuntu.
	p := openPage(t, phone, "ubuntu")
	if p.hello.Host != "ubuntu" || p.hello.Door != "phone" || p.hello.Scope != "all" || p.hello.Error != "" {
		t.Fatalf("hello through the phone: %+v", p.hello)
	}
	p.eventually("ubuntu is the anchor, with its own machines", func(hs []proto.HostInfo, ss map[string]proto.SessionInfo) bool {
		u := hostNamed(hs, "ubuntu")
		return u != nil && u.Local && hostNamed(hs, "gpu") != nil && hostNamed(hs, "phone") == nil && !tabNamed(ss, "phone", "phone-tab")
	})
	// The window keeps the phone's theme and font size; the tab order is ubuntu's.
	set := p.wait("settings", func(m proto.Msg) bool { return m.T == proto.TSettings })
	if got := settingsOf(t, set.Text); got["theme"] != "gecko-light" || got["fontSize"] != 17.0 || fmt.Sprint(got["order"]) != "[ubuntu-order]" {
		t.Fatalf("settings through the new anchor: %s", set.Text)
	}
	// Changing them: the look is saved on the phone, the order on ubuntu.
	p.send(&proto.Msg{T: proto.TSetSettings, Text: `{"theme":"dracula","fontSize":17,"order":["new-order"]}`})
	waitSettings(t, phone, func(s map[string]any) bool { return s["theme"] == "dracula" && fmt.Sprint(s["order"]) == "[phone-order]" })
	waitSettings(t, ubuntu, func(s map[string]any) bool {
		return s["theme"] == "tokyo-night" && s["fontSize"] == 12.0 && fmt.Sprint(s["order"]) == "[new-order]"
	})
	// New tabs open on ubuntu now, and it manages its machines (owner).
	r := p.call(proto.Msg{T: proto.TCreate, Create: &proto.CreateReq{Name: "made-on-ubuntu", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24}})
	if r.Error != "" || r.Session.Host != "ubuntu" {
		t.Fatalf("create through the new anchor: %+v", r)
	}
	if r := p.call(proto.Msg{T: proto.TAnchor, Name: "account"}); r.Error != "" || r.Text != "on" {
		t.Fatalf("an owner request on the new anchor: %+v", r)
	}
	direct := openPage(t, ubuntu)
	direct.eventually("the tab is on ubuntu itself", func(_ []proto.HostInfo, ss map[string]proto.SessionInfo) bool {
		return tabNamed(ss, "ubuntu", "made-on-ubuntu")
	})
	// Typing reaches it through the phone, and its output comes back.
	var id string
	p.mu.Lock()
	for _, s := range p.sessions {
		if s.Name == "made-on-ubuntu" {
			id = s.ID
		}
	}
	p.mu.Unlock()
	p.send(&proto.Msg{T: proto.TAttach, ID: id, Sub: "s1"})
	p.send(&proto.Msg{T: proto.TInput, ID: id, Data: "echo anchored-$((6*7))\n"})
	deadline := time.Now().Add(15 * time.Second)
	for {
		r := p.call(proto.Msg{T: proto.TCapture, ID: id})
		if strings.Contains(r.Text, "anchored-42") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no output from ubuntu through the phone: %q", r.Text)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Switching back: a page with no anchor gets the phone and its list again.
	back := openPage(t, phone)
	if back.hello.Host != "phone" || back.hello.Door != "" {
		t.Fatalf("back on the phone: %+v", back.hello)
	}
	back.eventually("the phone's own list again", func(hs []proto.HostInfo, ss map[string]proto.SessionInfo) bool {
		return hostNamed(hs, "phone") != nil && hostNamed(hs, "ubuntu") != nil && hostNamed(hs, "gpu") == nil &&
			tabNamed(ss, "phone", "phone-tab") && tabNamed(ss, "ubuntu", "made-on-ubuntu")
	})
	// An unknown machine: the page stays on the phone and hears why.
	lost := openPage(t, phone, "nowhere")
	if lost.hello.Host != "phone" || lost.hello.Kind != "anchor" || !strings.Contains(lost.hello.Error, "nowhere") {
		t.Fatalf("hello for an unknown anchor: %+v", lost.hello)
	}
}

func TestMakeAnchorOverLink(t *testing.T) {
	gpu := startDaemon(t, "gpu", 7942, nil)
	ubuntu := startDaemon(t, "ubuntu", 7941, []config.Host{{Name: "gpu", URL: gpu.base + "/", Token: gpu.token}})
	phone := startDaemon(t, "phone", 7940, []config.Host{{Name: "ubuntu", URL: ubuntu.base + "/", Token: ubuntu.token}})
	checkSwitch(t, phone, ubuntu)
}

// sshd starts a throwaway OpenSSH server on 127.0.0.1:port that lets this
// user in with a key of its own, and returns an ssh client config for it
// (host "ubuntu").
func sshd(t *testing.T, port int) string {
	t.Helper()
	bin, err := exec.LookPath("sshd")
	if err != nil {
		if _, e := os.Stat("/usr/sbin/sshd"); e != nil {
			t.Skip("no sshd")
		}
		bin = "/usr/sbin/sshd"
	}
	dir, err := os.MkdirTemp("", "gk-sshd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for _, k := range []string{"host", "client"} {
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(dir, k)).CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen: %v %s", err, out)
		}
	}
	pub, _ := os.ReadFile(filepath.Join(dir, "client.pub"))
	os.WriteFile(filepath.Join(dir, "authorized_keys"), pub, 0o600)
	u, _ := user.Current()
	conf := fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s/host
AuthorizedKeysFile %s/authorized_keys
PidFile %s/sshd.pid
StrictModes no
UsePAM no
PasswordAuthentication no
KbdInteractiveAuthentication no
AllowUsers %s
`, port, dir, dir, dir, u.Username)
	os.WriteFile(filepath.Join(dir, "sshd_config"), []byte(conf), 0o600)
	cmd := exec.Command(bin, "-D", "-e", "-f", filepath.Join(dir, "sshd_config"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	client := fmt.Sprintf(`Host ubuntu
  HostName 127.0.0.1
  Port %d
  User %s
  IdentityFile %s/client
  IdentitiesOnly yes
  UserKnownHostsFile /dev/null
  StrictHostKeyChecking no
  BatchMode yes
  LogLevel ERROR
`, port, u.Username, dir)
	p := filepath.Join(dir, "ssh_config")
	os.WriteFile(p, []byte(client), 0o600)
	return p
}

func TestMakeAnchorOverSSH(t *testing.T) {
	gpu := startDaemon(t, "gpu", 7945, nil)
	ubuntu := startDaemon(t, "ubuntu", 7944, []config.Host{{Name: "gpu", URL: gpu.base + "/", Token: gpu.token}})
	conf := sshd(t, 7946)
	// The phone reaches ubuntu over SSH and runs `gecko bridge` there, as
	// for any SSH machine (the command only adds this test's ssh config and
	// ubuntu's GECKO_HOME).
	bridge := []string{"ssh", "-F", conf, "-T", "ubuntu", "env GECKO_HOME=" + ubuntu.home + " " + geckoBin(t) + " bridge"}
	phone := startDaemon(t, "phone", 7943, []config.Host{{Name: "ubuntu", SSH: "ubuntu", Command: bridge}})
	checkSwitch(t, phone, ubuntu)
}

// An older Gecko (protocol 10) can't be the anchor: the page stays on the
// phone and is told to update it.
func TestMakeAnchorOnOlderGecko(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:7947")
	if err != nil {
		t.Fatal(err)
	}
	const tok = "old-gecko-token-0123456789"
	up := websocket.Upgrader{}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, b, err := c.ReadMessage()
			if err != nil {
				return
			}
			var m proto.Msg
			if json.Unmarshal(b, &m) == nil && m.T == proto.THello {
				out, _ := json.Marshal(proto.Msg{T: proto.THello, Host: "oldbox", Version: 10, Scope: m.Scope, Node: "old-node"})
				_ = c.WriteMessage(websocket.TextMessage, out)
			} else if m.Rid != 0 {
				out, _ := json.Marshal(proto.Msg{T: proto.TReply, Rid: m.Rid, OK: true})
				_ = c.WriteMessage(websocket.TextMessage, out)
			}
		}
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	phone := startDaemon(t, "phone", 7948, []config.Host{{Name: "oldbox", URL: "http://127.0.0.1:7947/", Token: tok}})
	home := openPage(t, phone)
	home.eventually("oldbox connected", func(hs []proto.HostInfo, _ map[string]proto.SessionInfo) bool {
		o := hostNamed(hs, "oldbox")
		return o != nil && o.Status == "connected"
	})
	p := openPage(t, phone, "oldbox")
	if p.hello.Host != "phone" || p.hello.Kind != "anchor" || p.hello.Error != "Update Gecko on oldbox to make it the anchor" {
		t.Fatalf("hello: %+v", p.hello)
	}
}
