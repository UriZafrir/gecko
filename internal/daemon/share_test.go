package daemon

import (
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/session"
)

func TestSharerOnOffRotates(t *testing.T) {
	h := hub.New(&config.Config{Name: "test"}, session.NewManager(session.Options{Host: "test", Shell: "/bin/sh", ScriptsDir: t.TempDir()}))
	s := &sharer{hub: h}
	defer s.stop()
	get := func(listen, token string) (int, error) {
		req, _ := http.NewRequest("GET", "http://"+listen+"/api/info", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err
		}
		r.Body.Close()
		return r.StatusCode, nil
	}

	st, err := s.set("on", "127.0.0.1:0")
	if err != nil || !st.On || len(st.Token) < 32 {
		t.Fatalf("on: %+v %v", st, err)
	}
	if code, err := get(st.Listen, st.Token); err != nil || code != 200 {
		t.Fatalf("shared listener: %d %v", code, err)
	}
	if code, _ := get(st.Listen, "wrong-token-0000000000"); code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", code)
	}
	// Asking again for what is on keeps it (and its link).
	if same, _ := s.set("on", "127.0.0.1:0"); same.Token != st.Token {
		t.Errorf("turning on what is on changed the token")
	}
	if same, _ := s.set("on", st.Listen); same.Token != st.Token {
		t.Errorf("turning on what is on changed the token")
	}
	// Off, then on: a new token, and the old link is dead.
	s.set("off", "")
	again, _ := s.set("on", "127.0.0.1:0")
	if again.Token == st.Token {
		t.Errorf("token not renewed")
	}
	if code, _ := get(again.Listen, st.Token); code != http.StatusUnauthorized {
		t.Errorf("old token after off/on: %d", code)
	}
	wild, err := s.set("on", "0.0.0.0:0")
	if err != nil || wild.Listen[:8] != "0.0.0.0:" {
		t.Errorf("0.0.0.0 should stay IPv4: %+v %v", wild, err)
	}
	if same, _ := s.set("on", "0.0.0.0:0"); same.Token != wild.Token {
		t.Errorf("asking again with the same address changed the token")
	}
	again = wild
	// A browser connected through the shared link is cut off too.
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+again.Listen+"/ws", http.Header{"Authorization": {"Bearer " + again.Token}})
	if err != nil {
		t.Fatalf("websocket: %v", err)
	}
	defer ws.Close()
	off, err := s.set("off", "")
	if err != nil || off.On {
		t.Fatalf("off: %+v %v", off, err)
	}
	if _, err := get(again.Listen, again.Token); err == nil {
		t.Errorf("still listening after off")
	}
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				t.Errorf("websocket still open after off")
			}
			break
		}
	}
	if st, _ := s.set("", ""); st.On {
		t.Errorf("status after off: %+v", st)
	}
	if _, err := s.set("maybe", ""); err == nil {
		t.Errorf("bad action accepted")
	}
}
