package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/session"
)

func TestPrivatePeer(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:5000":         true,
		"[::1]:5000":             true,
		"10.1.2.3:1":             true,
		"172.16.0.9:1":           true,
		"172.32.0.9:1":           false,
		"192.168.1.20:1":         true,
		"169.254.3.4:1":          true,
		"100.64.0.1:1":           true,
		"100.127.255.254:1":      true,
		"100.128.0.1:1":          false,
		"8.8.8.8:53":             false,
		"[fd7a:115c:a1e0::5]:1":  true,
		"[fe80::1]:1":            true,
		"[2001:4860::8888]:1":    false,
		"[::ffff:192.168.0.2]:1": true,
		"[::ffff:8.8.8.8]:1":     false,
		"not an address":         false,
	} {
		if got := PrivatePeer(addr); got != want {
			t.Errorf("PrivatePeer(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestPrivateOnlyRefusesPublicPeers(t *testing.T) {
	h := hub.New(&config.Config{Name: "test"}, session.NewManager(session.Options{Host: "test", Shell: "/bin/sh", ScriptsDir: t.TempDir()}))
	s := &HTTP{Hub: h, Token: "secret-token-123456", PrivateOnly: true}
	for addr, want := range map[string]int{"192.168.1.5:4000": http.StatusOK, "203.0.113.7:4000": http.StatusForbidden} {
		r := httptest.NewRequest("GET", "/api/info", nil)
		r.RemoteAddr = addr
		r.Header.Set("Authorization", "Bearer secret-token-123456")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: got %d, want %d", addr, w.Code, want)
		}
	}
}

// /api/info says which gecko it is (the Android app's touch diagnostics).
func TestInfoHasBuild(t *testing.T) {
	old := Build
	defer func() { Build = old }()
	Build = BuildInfo{Version: "v1.2.3", Commit: "a1b2c3d-dirty", Time: "2026-10-16T23:15:00Z"}
	h := hub.New(&config.Config{Name: "test"}, session.NewManager(session.Options{Host: "test", Shell: "/bin/sh", ScriptsDir: t.TempDir()}))
	s := &HTTP{Hub: h, Token: "secret-token-123456"}
	r := httptest.NewRequest("GET", "/api/info", nil)
	r.Header.Set("Authorization", "Bearer secret-token-123456")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != "v1.2.3" || got["commit"] != "a1b2c3d-dirty" || got["built"] != "2026-10-16T23:15:00Z" {
		t.Errorf("info = %v", got)
	}
}

// Sharing is changed only from the daemon's own socket.
func TestShareOnlyOnLocalSocket(t *testing.T) {
	h := hub.New(&config.Config{Name: "test"}, session.NewManager(session.Options{Host: "test", Shell: "/bin/sh", ScriptsDir: t.TempDir()}))
	h.Share = func(action, addr string) (proto.ShareState, error) {
		return proto.ShareState{On: action == "on", Listen: addr}, nil
	}
	ask := func(serve func(*hub.Hub, proto.Conn), hello string) proto.Msg {
		a, b := net.Pipe()
		go serve(h, proto.NewStreamConn(a, a, a))
		c := proto.NewStreamConn(b, b, b)
		defer c.Close()
		_ = proto.WriteJSON(c, &proto.Msg{T: proto.THello, Client: hello, Rid: 1})
		_ = proto.WriteJSON(c, &proto.Msg{T: proto.TShare, Text: "on", Target: "127.0.0.1:0", Rid: 2})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			bin, raw, err := c.Read()
			if err != nil {
				break
			}
			var m proto.Msg
			if !bin && json.Unmarshal(raw, &m) == nil && m.Rid == 2 {
				return m
			}
		}
		t.Fatal("no reply")
		return proto.Msg{}
	}
	if r := ask(Serve, "web"); r.Error == "" {
		t.Error("a web client could turn sharing on")
	}
	if r := ask(ServeLocal, "hub"); r.Error == "" {
		t.Error("another Gecko (bridge) could turn sharing on")
	}
	r := ask(ServeLocal, "cli")
	var st proto.ShareState
	if r.Error != "" || json.Unmarshal([]byte(r.Text), &st) != nil || !st.On {
		t.Errorf("cli: %+v", r)
	}
}
