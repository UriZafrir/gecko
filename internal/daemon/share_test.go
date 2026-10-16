package daemon

import (
	"net/http"
	"testing"

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
	again, _ := s.set("on", "127.0.0.1:0")
	if again.Token == st.Token {
		// a different address is a new listener with a new token
		t.Errorf("token not renewed for a new listener")
	}
	if same, _ := s.set("on", again.Listen); same.Token != again.Token {
		t.Errorf("turning on what is on changed the token")
	}
	off, err := s.set("off", "")
	if err != nil || off.On {
		t.Fatalf("off: %+v %v", off, err)
	}
	if _, err := get(again.Listen, again.Token); err == nil {
		t.Errorf("still listening after off")
	}
	if st, _ := s.set("", ""); st.On {
		t.Errorf("status after off: %+v", st)
	}
	if _, err := s.set("maybe", ""); err == nil {
		t.Errorf("bad action accepted")
	}
}
