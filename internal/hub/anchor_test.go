package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/server"
)

// The anchor: the Gecko you connect to from another device (a phone's web
// app through a share link or Tailscale Serve) manages every other machine.
// These tests run several Geckos in one process, each on its own loopback
// port, and drive the anchor the way the phone does: over its WebSocket,
// with its token.

const anchorToken = "anchor-token-0123456789abcdef0123"

// client is a phone's web app connected to a Gecko.
type client struct {
	t    *testing.T
	ws   *websocket.Conn
	wmu  sync.Mutex
	rid  int
	msgs chan proto.Msg
	data chan []byte

	mu       sync.Mutex
	hello    proto.Msg
	sessions map[string]proto.SessionInfo
	hosts    []proto.HostInfo
}

func dialClient(t *testing.T, base, token string) *client {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/ws", http.Header{"Authorization": {"Bearer " + token}})
	if err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, ws: ws, msgs: make(chan proto.Msg, 1000), data: make(chan []byte, 1000), sessions: map[string]proto.SessionInfo{}}
	t.Cleanup(func() { ws.Close() })
	go c.read()
	c.send(&proto.Msg{T: proto.THello, Client: "web", Version: proto.Version})
	c.wait("hello", func(m proto.Msg) bool { return m.T == proto.THello })
	return c
}

func (c *client) read() {
	for {
		typ, b, err := c.ws.ReadMessage()
		if err != nil {
			close(c.msgs)
			return
		}
		if typ == websocket.BinaryMessage {
			_, _, _, d, _ := proto.DecodeData(b)
			c.data <- append([]byte(nil), d...)
			continue
		}
		var m proto.Msg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		c.mu.Lock()
		switch m.T {
		case proto.THello:
			c.hello = m
		case proto.TSessions:
			if m.Host == "" {
				c.sessions = map[string]proto.SessionInfo{}
			}
			for id, s := range c.sessions {
				if m.Host != "" && s.Host == m.Host {
					delete(c.sessions, id)
				}
			}
			for _, s := range m.Sessions {
				c.sessions[s.ID] = s
			}
		case proto.TSession:
			c.sessions[m.Session.ID] = *m.Session
		case proto.TClosed:
			delete(c.sessions, m.ID)
		case proto.TState:
			c.hosts = m.Hosts
		}
		c.mu.Unlock()
		c.msgs <- m
	}
}

func (c *client) send(m *proto.Msg) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	b, _ := json.Marshal(m)
	_ = c.ws.WriteMessage(websocket.TextMessage, b)
}

func (c *client) wait(what string, ok func(proto.Msg) bool) proto.Msg {
	c.t.Helper()
	timeout := time.After(20 * time.Second)
	for {
		select {
		case m, open := <-c.msgs:
			if !open {
				c.t.Fatalf("connection closed waiting for %s", what)
			}
			if ok(m) {
				return m
			}
		case <-timeout:
			c.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// call sends a request and returns its reply.
func (c *client) call(m *proto.Msg) proto.Msg {
	c.t.Helper()
	c.wmu.Lock()
	c.rid++
	m.Rid = c.rid
	c.wmu.Unlock()
	c.send(m)
	return c.wait(m.T+" reply", func(r proto.Msg) bool { return r.T == proto.TReply && r.Rid == m.Rid })
}

// eventually waits until the client's view satisfies ok.
func (c *client) eventually(what string, ok func(sessions map[string]proto.SessionInfo, hosts []proto.HostInfo) bool) {
	c.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		c.mu.Lock()
		good := ok(c.sessions, c.hosts)
		c.mu.Unlock()
		if good {
			return
		}
		if time.Now().After(deadline) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.t.Fatalf("timed out waiting for %s; sessions %v hosts %v", what, keys(c.sessions), names(c.hosts))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func keys(m map[string]proto.SessionInfo) []string {
	var out []string
	for k, s := range m {
		out = append(out, k+"("+s.Name+")")
	}
	return out
}

func names(hs []proto.HostInfo) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Name+":"+h.Status+":"+h.Error)
	}
	return out
}

func hasTab(ss map[string]proto.SessionInfo, host, name string) bool {
	for _, s := range ss {
		if s.Host == host && s.Name == name {
			return true
		}
	}
	return false
}

func countHost(ss map[string]proto.SessionInfo, host string) int {
	n := 0
	for _, s := range ss {
		if s.Host == host {
			n++
		}
	}
	return n
}

// serveAt serves h's web UI with token on a loopback port.
func serveAt(t *testing.T, h *hub.Hub, token string) (*httptest.Server, *server.HTTP) {
	s := &server.HTTP{Hub: h, Token: token}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, s
}

// From the phone, connected to the anchor: add the laptop, see its tabs,
// open and type in one there, update the list, remove it. The laptop's tabs
// keep running on the laptop.
func TestAnchorManagesMachinesFromPhone(t *testing.T) {
	t.Setenv("GECKO_HOME", t.TempDir())
	laptop := newHub(t, "laptop")
	if _, err := laptop.Create(proto.CreateReq{Name: "laptop-notes", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	laptopSrv, _ := geckoAt(t, laptop)
	gpu := newHub(t, "gpu")
	if _, err := gpu.Create(proto.CreateReq{Name: "train", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	gpuSrv, _ := geckoAt(t, gpu)

	devbox := newHub(t, "devbox")
	if _, err := devbox.Create(proto.CreateReq{Name: "build", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	if err := devbox.AddLinkHost("gpu", gpuSrv.URL+"/?token="+linkToken, true); err != nil {
		t.Fatal(err)
	}
	devSrv, _ := serveAt(t, devbox, anchorToken)

	// The phone: a full client of the anchor (it holds the anchor's token).
	phone := dialClient(t, devSrv.URL, anchorToken)
	if phone.hello.Host != "devbox" || phone.hello.Scope != hub.ScopeAll || phone.hello.Node == "" || phone.hello.Version != proto.Version {
		t.Fatalf("hello: %+v", phone.hello)
	}
	phone.eventually("devbox's and gpu's tabs", func(ss map[string]proto.SessionInfo, _ []proto.HostInfo) bool {
		return hasTab(ss, "devbox", "build") && hasTab(ss, "gpu", "train")
	})
	if hasTab(phone.sessions, "laptop", "laptop-notes") {
		t.Fatal("laptop's tab before laptop was added")
	}

	// Add the laptop by its link, from the phone (probe first, as the dialog does).
	link := laptopSrv.URL + "/?token=" + linkToken
	if r := phone.call(&proto.Msg{T: proto.TProbeHost, Kind: hub.KindLink, Secret: link}); r.Error != "" || r.Host != "laptop" {
		t.Fatalf("probe from the phone: %+v", r)
	}
	if r := phone.call(&proto.Msg{T: proto.TAddHost, Kind: hub.KindLink, Name: "laptop", Secret: link}); r.Error != "" {
		t.Fatalf("add from the phone: %+v", r)
	}
	phone.eventually("laptop's tab through the anchor", func(ss map[string]proto.SessionInfo, hs []proto.HostInfo) bool {
		return hasTab(ss, "laptop", "laptop-notes")
	})

	// A new tab on the laptop, from the phone: it runs on the laptop.
	r := phone.call(&proto.Msg{T: proto.TCreate, Create: &proto.CreateReq{Host: "laptop", Name: "from-phone", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24}})
	if r.Error != "" || r.Session == nil || !strings.HasPrefix(r.Session.ID, "laptop/") {
		t.Fatalf("create on laptop: %+v", r)
	}
	found := false
	for _, s := range laptop.Sessions(hub.ScopeLocal) {
		found = found || s.Name == "from-phone"
	}
	if !found {
		t.Fatal("the tab didn't open on the laptop")
	}
	phone.send(&proto.Msg{T: proto.TAttach, ID: r.Session.ID, Sub: "p1"})
	phone.wmu.Lock()
	_ = phone.ws.WriteMessage(websocket.BinaryMessage, proto.EncodeData(proto.KindInput, r.Session.ID, 0, []byte("echo ANCHOR-$((6*7))\n")))
	phone.wmu.Unlock()
	var out strings.Builder
	timeout := time.After(10 * time.Second)
	for !strings.Contains(out.String(), "ANCHOR-42") {
		select {
		case d := <-phone.data:
			out.Write(d)
		case <-timeout:
			t.Fatalf("no output from the laptop's tab: %q", out.String())
		}
	}

	// Remove it from the phone: its tabs leave the phone's view at once,
	// and keep running on the laptop.
	if r := phone.call(&proto.Msg{T: proto.TRmHost, Name: "laptop"}); r.Error != "" {
		t.Fatalf("remove: %+v", r)
	}
	phone.eventually("laptop's tabs gone from the phone", func(ss map[string]proto.SessionInfo, hs []proto.HostInfo) bool {
		return countHost(ss, "laptop") == 0
	})
	if len(laptop.Sessions(hub.ScopeLocal)) != 2 {
		t.Fatalf("removing the machine ended its tabs: %+v", laptop.Sessions(hub.ScopeLocal))
	}
}

// The same machine through two links, and a link back to the anchor
// itself: no tab is listed twice and nothing loops.
func TestAnchorNoLoopsNoDuplicates(t *testing.T) {
	t.Setenv("GECKO_HOME", t.TempDir())
	laptop := newHub(t, "laptop")
	if _, err := laptop.Create(proto.CreateReq{Name: "laptop-notes", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	laptopSrv, _ := geckoAt(t, laptop)
	devbox := newHub(t, "devbox")
	devSrv, _ := serveAt(t, devbox, anchorToken)
	phone := dialClient(t, devSrv.URL, anchorToken)

	link := laptopSrv.URL + "/?token=" + linkToken
	if r := phone.call(&proto.Msg{T: proto.TAddHost, Kind: hub.KindLink, Name: "laptop", Secret: link}); r.Error != "" {
		t.Fatal(r.Error)
	}
	waitHost(t, devbox, "laptop", func(i proto.HostInfo) bool { return i.Status == "connected" })

	// The same Gecko again under another name: refused when checked...
	if r := phone.call(&proto.Msg{T: proto.TAddHost, Kind: hub.KindLink, Name: "laptop2", Secret: link}); !strings.Contains(r.Error, "same Gecko as laptop") {
		t.Fatalf("duplicate added: %+v", r)
	}
	// ...and when added unchecked, it doesn't connect and lists nothing.
	if r := phone.call(&proto.Msg{T: proto.TAddHost, Kind: hub.KindLink, Name: "laptop2", Secret: link, Data: "nocheck"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	i := waitHost(t, devbox, "laptop2", func(i proto.HostInfo) bool { return i.Status == "error" })
	if !strings.Contains(i.Error, "same Gecko as laptop") {
		t.Fatalf("laptop2: %+v", i)
	}

	// A link back to the anchor itself.
	self := devSrv.URL + "/?token=" + anchorToken
	if r := phone.call(&proto.Msg{T: proto.TAddHost, Kind: hub.KindLink, Name: "loop", Secret: self}); !strings.Contains(r.Error, "devbox itself") {
		t.Fatalf("loop added: %+v", r)
	}
	if r := phone.call(&proto.Msg{T: proto.TAddHost, Kind: hub.KindLink, Name: "loop", Secret: self, Data: "nocheck"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	l := waitHost(t, devbox, "loop", func(i proto.HostInfo) bool { return i.Status == "error" })
	if !strings.Contains(l.Error, "devbox itself") {
		t.Fatalf("loop: %+v", l)
	}

	// Two Geckos that have each other: each still shows the other's own
	// tabs once, never its own tabs back through the other.
	devLink := devSrv.URL + "/?token=" + anchorToken
	if err := laptop.AddLinkHost("devbox", devLink, true); err != nil {
		t.Fatal(err)
	}
	waitHost(t, laptop, "devbox", func(i proto.HostInfo) bool { return i.Status == "connected" })
	time.Sleep(300 * time.Millisecond)
	phone.eventually("one copy of each tab", func(ss map[string]proto.SessionInfo, hs []proto.HostInfo) bool {
		return countHost(ss, "laptop") == 1 && countHost(ss, "laptop2") == 0 && countHost(ss, "loop") == 0
	})
	for _, s := range devbox.Sessions(hub.ScopeAll) {
		if strings.Count(s.ID, "/") != 1 || s.Host == "devbox" && s.Name == "laptop-notes" {
			t.Fatalf("a tab came back around: %+v", s)
		}
	}

}

// Turning sharing off (the link the phone used) disconnects the phone.
func TestAnchorShareOffCutsThePhone(t *testing.T) {
	t.Setenv("GECKO_HOME", t.TempDir())
	devbox := newHub(t, "devbox")
	srv, share := serveAt(t, devbox, anchorToken)
	phone := dialClient(t, srv.URL, anchorToken)
	share.CloseClients()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, open := <-phone.msgs:
			if !open {
				return
			}
		case <-timeout:
			t.Fatal("the phone is still connected")
		}
	}
}
