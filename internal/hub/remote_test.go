package hub

import (
	"encoding/json"
	"os"
	"strconv"
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
