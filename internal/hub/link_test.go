package hub_test

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/server"
)

const linkToken = "s3cret-link-token-0123456789abcdef"

// geckoAt serves h's web API (the WebSocket the web client and link
// machines use) on a loopback port, as `gecko daemon --listen` does.
func geckoAt(t *testing.T, h *hub.Hub) (*httptest.Server, *server.HTTP) {
	s := &server.HTTP{Hub: h, Token: linkToken}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, s
}

// lockedBuf collects log output from every goroutine.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Another Gecko added by its link: its tabs show up here with its name as
// host, tabs open there from here, the round trip is measured, and the
// token stays in config.json (0600) and nowhere else.
func TestLinkMachine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GECKO_HOME", home)
	logs := &lockedBuf{}
	log.SetOutput(logs)
	defer log.SetOutput(os.Stderr)

	termux := newHub(t, "pixel-termux")
	srv, _ := geckoAt(t, termux)
	if _, err := termux.Create(proto.CreateReq{Name: "old-tab", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	phone := newHub(t, "pixel")
	events := make(chan *proto.Msg, 100)
	defer phone.Subscribe(hub.ScopeAll, func(m *proto.Msg) {
		if m.T == proto.TSession || m.T == proto.TSessions {
			events <- m
		}
	})()

	name, goos, version, err := phone.ProbeLink(srv.URL + "/?token=" + linkToken)
	if err != nil || name != "pixel-termux" || goos == "" || version != proto.Version {
		t.Fatalf("probe: %q %q %d %v", name, goos, version, err)
	}
	// As `gecko url` prints it, with words after the link.
	if err := phone.AddLinkHost("termux", srv.URL+"/?token="+linkToken+"  (this device)", true); err != nil {
		t.Fatal(err)
	}
	i := waitHost(t, phone, "termux", func(i proto.HostInfo) bool { return i.Status == "connected" })
	if i.Kind != hub.KindLink || i.Via != hub.ViaLink || i.Target != srv.URL+"/" || i.Version != proto.Version {
		t.Fatalf("termux: %+v", i)
	}
	// Its existing tab, under its name here.
	deadline := time.Now().Add(5 * time.Second)
	for {
		found := false
		for _, s := range phone.Sessions(hub.ScopeAll) {
			if s.Host == "termux" && s.Name == "old-tab" {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("termux's tab isn't listed: %+v", phone.Sessions(hub.ScopeAll))
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A new tab on it from here runs there, and its events come through.
	s, err := phone.Create(proto.CreateReq{Host: "termux", Name: "new-tab", Cols: 80, Rows: 24})
	if err != nil || !strings.HasPrefix(s.ID, "termux/") {
		t.Fatalf("create on termux: %+v %v", s, err)
	}
	if len(termux.Sessions(hub.ScopeLocal)) != 2 {
		t.Fatalf("the tab didn't open in termux: %+v", termux.Sessions(hub.ScopeLocal))
	}
	b, lid, _ := termux.Resolve("new-tab")
	_ = b.Patch(lid, proto.Patch{Name: strPtr("renamed")})
	timeout := time.After(5 * time.Second)
	for got := false; !got; {
		select {
		case m := <-events:
			got = m.T == proto.TSession && m.Session.ID == s.ID && m.Session.Name == "renamed"
		case <-timeout:
			t.Fatal("no session event from termux")
		}
	}
	// The latency indicator.
	waitHost(t, phone, "termux", func(i proto.HostInfo) bool { return i.RTT > 0 })

	// The token: in config.json, mode 0600; not in host state, not in logs.
	st, _ := json.Marshal(phone.State(hub.ScopeAll))
	if strings.Contains(string(st), linkToken) {
		t.Fatalf("token in host state: %s", st)
	}
	cfg, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil || !strings.Contains(string(cfg), linkToken) {
		t.Fatalf("token not saved: %v", err)
	}
	if fi, _ := os.Stat(filepath.Join(home, "config.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("config.json is %v", fi.Mode().Perm())
	}
	if strings.Contains(logs.String(), linkToken) {
		t.Fatalf("token in the log: %s", logs.String())
	}
	// Loaded again (the daemon restarting), it connects from the config.
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	again := newHub(t, "pixel", c.Hosts...)
	waitHost(t, again, "termux", func(i proto.HostInfo) bool { return i.Status == "connected" && i.Kind == hub.KindLink })
}

func strPtr(s string) *string { return &s }

// A wrong token: refused when checked, and when added anyway it says so,
// without the token in what it says.
func TestLinkMachineBadToken(t *testing.T) {
	t.Setenv("GECKO_HOME", t.TempDir())
	other := newHub(t, "other")
	srv, _ := geckoAt(t, other)
	phone := newHub(t, "phone")
	bad := "wrong-token-abcdefghijklmnop"
	err := phone.AddLinkHost("other", srv.URL+"/?token="+bad, true)
	if err == nil || !strings.Contains(err.Error(), "refused the token") || strings.Contains(err.Error(), bad) {
		t.Fatalf("bad token checked: %v", err)
	}
	if _, ok := hosts(phone)["other"]; ok {
		t.Fatal("added despite the check")
	}
	if err := phone.AddLinkHost("other", srv.URL+"/?token="+bad, false); err != nil {
		t.Fatal(err)
	}
	i := waitHost(t, phone, "other", func(i proto.HostInfo) bool { return i.Status == "error" })
	if !strings.Contains(i.Error, "refused the token") || strings.Contains(i.Error, bad) || strings.Contains(i.Target, bad) {
		t.Fatalf("other: %+v", i)
	}
	// Nothing listening: added unchecked (Termux not started yet), it
	// says so and keeps trying.
	if err := phone.AddLinkHost("gone", "http://127.0.0.1:1/?token="+linkToken, false); err != nil {
		t.Fatal(err)
	}
	g := waitHost(t, phone, "gone", func(i proto.HostInfo) bool { return i.Status == "error" })
	if !strings.Contains(g.Error, "nothing answers") {
		t.Fatalf("gone: %+v", g)
	}
	// Names and links that aren't right.
	for _, c := range []struct{ name, link, want string }{
		{"a b", srv.URL + "/?token=" + linkToken, "without spaces"},
		{"phone", srv.URL + "/?token=" + linkToken, "is this machine"},
		{"x", srv.URL + "/", "no token"},
		{"x", "ftp://host/?token=" + linkToken, "only http and https"},
		{"x", "http://8.8.8.8:7681/?token=" + linkToken, "needs https"},
	} {
		if err := phone.AddLinkHost(c.name, c.link, false); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: %v, want %q", c.name, c.link, err, c.want)
		}
	}
}

// The other Gecko restarting, or the connection dropping: the link comes
// back by itself, with the tabs.
func TestLinkMachineReconnects(t *testing.T) {
	t.Setenv("GECKO_HOME", t.TempDir())
	other := newHub(t, "other")
	srv, s := geckoAt(t, other)
	phone := newHub(t, "phone")
	if err := phone.AddLinkHost("other", srv.URL+"/?token="+linkToken, true); err != nil {
		t.Fatal(err)
	}
	waitHost(t, phone, "other", func(i proto.HostInfo) bool { return i.Status == "connected" })
	// Close every client connection, as a restart or a dropped network would.
	s.CloseClients()
	waitHost(t, phone, "other", func(i proto.HostInfo) bool { return i.Status != "connected" })
	if _, err := other.Create(proto.CreateReq{Name: "while-away", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	waitHost(t, phone, "other", func(i proto.HostInfo) bool { return i.Status == "connected" })
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, x := range phone.Sessions(hub.ScopeAll) {
			if x.Host == "other" && x.Name == "while-away" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("tab made while away isn't listed: %+v", phone.Sessions(hub.ScopeAll))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
