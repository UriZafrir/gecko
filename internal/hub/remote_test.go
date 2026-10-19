package hub

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/session"
)

// A fake `gecko bridge`: this test binary started with GECKO_FAKE_BRIDGE
// set speaks just enough protocol on stdio. It answers pings after
// GECKO_FAKE_DELAY ms, and stops answering them after GECKO_FAKE_PINGS.
func TestMain(m *testing.M) {
	if os.Getenv("GECKO_FAKE_BRIDGE") != "" {
		fakeBridge()
		return
	}
	os.Exit(m.Run())
}

func fakeBridge() {
	delay, _ := strconv.Atoi(os.Getenv("GECKO_FAKE_DELAY"))
	limit, _ := strconv.Atoi(os.Getenv("GECKO_FAKE_PINGS"))
	c := proto.NewStreamConn(os.Stdin, os.Stdout, nil)
	pings := 0
	for {
		_, b, err := c.Read()
		if err != nil {
			return
		}
		var m proto.Msg
		_ = json.Unmarshal(b, &m)
		switch m.T {
		case proto.THello:
			_ = proto.WriteJSON(c, &proto.Msg{T: proto.THello, Host: "fake", Version: proto.Version, OS: "linux"})
		case proto.TList:
			_ = proto.WriteJSON(c, &proto.Msg{T: proto.TReply, Rid: m.Rid, OK: true})
		case "fake-exit":
			// Gone, like a bridge whose daemon restarted, but a helper of
			// ssh's (a ProxyCommand, say) keeps the stderr pipe open.
			sleep := exec.Command("sleep", "3")
			sleep.Stderr = os.Stderr
			_ = sleep.Start()
			os.Exit(0)
		case proto.TPing:
			pings++
			if limit > 0 && pings > limit {
				continue // the link went quiet, like a dead SSH connection
			}
			time.Sleep(time.Duration(delay) * time.Millisecond)
			_ = proto.WriteJSON(c, &proto.Msg{T: proto.TReply, Rid: m.Rid, OK: true})
		}
	}
}

func fakeRemote(t *testing.T, delay, limit int) (*Remote, func()) {
	t.Setenv("GECKO_HOME", t.TempDir())
	t.Setenv("GECKO_FAKE_BRIDGE", "1")
	t.Setenv("GECKO_FAKE_DELAY", strconv.Itoa(delay))
	t.Setenv("GECKO_FAKE_PINGS", strconv.Itoa(limit))
	h := New(&config.Config{Name: "here"}, session.NewManager(session.Options{Host: "here", Shell: "/bin/sh", ScriptsDir: t.TempDir()}))
	r := NewRemote("there", "", KindSSH, SSHDial("there", []string{os.Args[0], "-test.run=^$"}), h)
	go r.Run()
	return r, func() { r.Close(); h.Close() }
}

func waitFor(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for !ok() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRemoteRTT(t *testing.T) {
	old := pingEvery
	pingEvery = 100 * time.Millisecond
	defer func() { pingEvery = old }()
	r, done := fakeRemote(t, 40, 0)
	defer done()
	waitFor(t, "an rtt", 5*time.Second, func() bool { return r.Info().RTT > 0 })
	if rtt := r.Info().RTT; rtt < 40 || rtt > 1000 {
		t.Fatalf("rtt %d ms, want about 40", rtt)
	}
}

func TestRemoteDeadLinkRedials(t *testing.T) {
	oe, ot, od := pingEvery, pingTimeout, linkDead
	pingEvery, pingTimeout, linkDead = 50*time.Millisecond, 100*time.Millisecond, 400*time.Millisecond
	defer func() { pingEvery, pingTimeout, linkDead = oe, ot, od }()
	r, done := fakeRemote(t, 0, 3)
	defer done()
	waitFor(t, "connected", 5*time.Second, func() bool { return r.Info().Status == "connected" })
	// After 3 pongs the fake goes quiet; the link must be dropped (and
	// its rtt forgotten) instead of hanging on.
	waitFor(t, "the quiet link dropped", 5*time.Second, func() bool { return r.Info().Status != "connected" })
	if r.Info().RTT != 0 {
		t.Fatal("rtt kept after the link dropped")
	}
}

// hubWithMachine is a hub with one SSH machine, "there", reached through
// the fake bridge.
func hubWithMachine(t *testing.T) (*Hub, *Remote) {
	t.Setenv("GECKO_HOME", t.TempDir())
	t.Setenv("GECKO_FAKE_BRIDGE", "1")
	cfg := &config.Config{Name: "here", Hosts: []config.Host{{Name: "there", SSH: "there", Command: []string{os.Args[0], "-test.run=^$"}}}}
	h := New(cfg, session.NewManager(session.Options{Host: "here", Shell: "/bin/sh", ScriptsDir: t.TempDir()}))
	t.Cleanup(h.Close)
	r := h.backend("there").(*Remote)
	waitFor(t, "connected", 5*time.Second, func() bool { return r.Info().Status == "connected" })
	return h, r
}

// A request on a machine whose link was closed (as an in-place restart
// used to do with every link) must say the machine is offline, not write
// to the closed pipe: "write |1: file already closed" is what Update
// Gecko showed after a restart failed.
func TestClosedRemoteSaysOffline(t *testing.T) {
	_, r := hubWithMachine(t)
	r.Close()
	_, err := r.request(&proto.Msg{T: proto.TList})
	if err == nil || strings.Contains(err.Error(), "file already closed") || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("request on a closed machine: %v", err)
	}
}

// A restart in place takes the links down (Suspend). When it fails, the
// daemon carries on, and so must its machines (Resume), instead of
// staying "connected" over dead links.
func TestFailedRestartKeepsMachines(t *testing.T) {
	h, r := hubWithMachine(t)
	h.Suspend()
	if _, err := r.request(&proto.Msg{T: proto.TList}); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("request while suspended: %v", err)
	}
	time.Sleep(1500 * time.Millisecond) // past the first redial
	if st := r.Info().Status; st == "connected" {
		t.Fatal("redialed while suspended")
	}
	h.Resume()
	waitFor(t, "connected again", 5*time.Second, func() bool { return r.Info().Status == "connected" })
	if _, err := r.request(&proto.Msg{T: proto.TList}); err != nil {
		t.Fatalf("request after resume: %v", err)
	}
}

// The link drops while ssh's stderr is still held open by a helper, so
// reaping ssh takes a while: requests meanwhile must say the machine is
// offline, not hit the closed stdin pipe.
func TestDroppedLinkSaysOffline(t *testing.T) {
	_, r := hubWithMachine(t)
	if err := r.send(&proto.Msg{T: "fake-exit"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	_, err := r.request(&proto.Msg{T: proto.TList})
	if err == nil || strings.Contains(err.Error(), "file already closed") {
		t.Fatalf("request on a dropped link: %v", err)
	}
	// And the link writer itself never says "file already closed".
	w := &linkWriter{w: nopCloser{}}
	w.close()
	if _, err := w.Write([]byte("x")); err != errLinkClosed {
		t.Fatalf("write on a closed link: %v", err)
	}
}

type nopCloser struct{}

func (nopCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopCloser) Close() error                { return nil }
