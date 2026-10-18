package cloud_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/cloud"
	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/relay"
	"github.com/gecko-term/gecko/internal/server"
	"github.com/gecko-term/gecko/internal/session"
)

// These tests run a real relay and several daemons (hub, sessions, agent)
// in one process, each machine with its own key directory.

type machine struct {
	name  string
	dir   string
	hub   *hub.Hub
	agent *cloud.Agent
}

type fleet struct {
	t     *testing.T
	relay *relay.Server
	url   string

	mu     sync.Mutex
	frames [][]byte // everything the relay forwarded between machines
}

func newFleet(t *testing.T) *fleet {
	t.Setenv("GECKO_HOME", t.TempDir())
	rs, err := relay.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fleet{t: t, relay: rs}
	rs.Tap = func(b []byte) {
		f.mu.Lock()
		f.frames = append(f.frames, append([]byte(nil), b...))
		f.mu.Unlock()
	}
	srv := httptest.NewServer(rs.Handler())
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

func (f *fleet) machine(name string) *machine {
	m := session.NewManager(session.Options{Host: name, Shell: "/bin/sh", ScriptsDir: f.t.TempDir()})
	h := hub.New(&config.Config{Name: name}, m)
	a := cloud.NewAgent(h, true, func(c proto.Conn) { server.ServePeer(h, c) }) // as the daemon does
	a.Dir = f.t.TempDir()
	h.SetCloud(a)
	a.Start()
	// The shells end with the test binary. (Manager.Shutdown here would
	// trip a known race in ptyx between Close and WaitReadable.)
	f.t.Cleanup(func() {
		_ = a.Do("logout", "")
		h.Close()
	})
	return &machine{name: name, dir: a.Dir, hub: h, agent: a}
}

// create signs m in as the first machine of a new account.
func (f *fleet) create(m *machine) {
	f.t.Helper()
	var out bytes.Buffer
	if _, err := cloud.Login(cloud.LoginOptions{Relay: f.url, Account: "roy", Name: m.name, Dir: m.dir, Out: &out}); err != nil {
		f.t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Recovery code") {
		f.t.Fatalf("no recovery code shown:\n%s", out.String())
	}
	if err := m.agent.Do("reload", ""); err != nil {
		f.t.Fatal(err)
	}
}

// enroll starts m's login and returns its pairing code and a channel with
// the login's result.
func (f *fleet) enroll(m *machine) (string, chan error) {
	f.t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := cloud.Login(cloud.LoginOptions{Relay: f.url, Account: "roy", Name: m.name, Dir: m.dir, Out: io.Discard, Poll: 50 * time.Millisecond})
		done <- err
	}()
	var keys *cloud.Keys
	waitFor(f.t, "keys of "+m.name, func() bool {
		k, err := cloud.LoadKeys(m.dir, false)
		keys = k
		return err == nil
	})
	d := keys.Device(m.name, 0)
	return cloud.PairingCode(&d), done
}

// join enrolls m and has approver approve it.
func (f *fleet) join(m, approver *machine) {
	f.t.Helper()
	code, done := f.enroll(m)
	waitFor(f.t, "pending "+m.name, func() bool { return len(approver.agent.State().Pending) == 1 })
	if err := approver.agent.Do("approve", code); err != nil {
		f.t.Fatal(err)
	}
	if err := <-done; err != nil {
		f.t.Fatal(err)
	}
	if err := m.agent.Do("reload", ""); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fleet) sawPlaintext(marker string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.frames) == 0 {
		f.t.Fatal("the relay forwarded nothing")
	}
	for _, b := range f.frames {
		if bytes.Contains(b, []byte(marker)) {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func connected(m *machine, host string) bool {
	b := m.hub.Backend(host)
	if b == nil {
		return false
	}
	i := b.Info()
	return i.Status == "connected" && i.Kind == hub.KindCloud && i.Via == hub.ViaRelay
}

type sink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *sink) Data(_ int64, b []byte) { s.mu.Lock(); s.buf.Write(b); s.mu.Unlock() }
func (s *sink) Reset(int64, string)    {}
func (s *sink) Exit()                  {}
func (s *sink) has(x string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Contains(s.buf.String(), x)
}

// TestRelaySession: enroll a second machine, then create a session on it
// from the first and type into it, all through the relay. The relay must
// only ever see ciphertext.
func TestRelaySession(t *testing.T) {
	f := newFleet(t)
	laptop, gpu := f.machine("laptop"), f.machine("gpu-box")
	f.create(laptop)
	f.join(gpu, laptop)
	waitFor(t, "gpu-box connected over the relay", func() bool { return connected(laptop, "gpu-box") })

	s, err := laptop.hub.Create(proto.CreateReq{Host: "gpu-box", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.ID, "gpu-box/") {
		t.Fatalf("session id %s", s.ID)
	}
	be, lid, err := laptop.hub.Resolve(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	out := &sink{}
	detach, err := be.Attach(lid, 0, out)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	const marker = "PLAINTEXT-MARKER-4f2a"
	if err := be.Input(lid, []byte("echo "+marker+"-$((6*7))\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the command's output", func() bool { return out.has(marker + "-42") })
	if f.sawPlaintext(marker) {
		t.Fatal("the relay forwarded a frame with terminal text in the clear")
	}

	// The fleet view: gpu-box knows the laptop too, both ways.
	waitFor(t, "laptop seen from gpu-box", func() bool { return connected(gpu, "laptop") })
	st := laptop.agent.State()
	if !st.SignedIn || len(st.Devices) != 3 || st.Fingerprint != gpu.agent.State().Fingerprint {
		t.Fatalf("account state: %+v", st)
	}

	// Revoked, gpu-box drops off the laptop and signs itself out.
	if err := laptop.agent.Do("revoke", "gpu-box"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "gpu-box gone from the laptop", func() bool { return laptop.hub.Backend("gpu-box") == nil })
	waitFor(t, "gpu-box signed out", func() bool { st := gpu.agent.State(); return !st.SignedIn && st.Notice != "" })
	if cloud.SignedIn(gpu.dir) {
		t.Fatal("gpu-box kept its account file")
	}
}

// TestEnrollNeedsApproval: a machine that asks to join gets nothing until a
// trusted machine approves it with the right code.
func TestEnrollNeedsApproval(t *testing.T) {
	f := newFleet(t)
	laptop, gpu := f.machine("laptop"), f.machine("gpu-box")
	f.create(laptop)
	code, done := f.enroll(gpu)
	waitFor(t, "the approval prompt", func() bool { return len(laptop.agent.State().Pending) == 1 })
	p := laptop.agent.State().Pending[0]
	if p.Name != "gpu-box" || p.IP == "" || p.OS == "" {
		t.Fatalf("prompt shows %+v", p)
	}

	// Waiting doesn't let it reach anything.
	keys, _ := cloud.LoadKeys(gpu.dir, false)
	if _, err := cloud.DialRelay(f.url, keys.Signer(), "/v1/dial?to="+laptop.agent.MachineID()); err == nil {
		t.Fatal("a machine waiting for approval could dial")
	}
	select {
	case err := <-done:
		t.Fatalf("login finished without approval: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	// The wrong code approves nothing.
	if err := laptop.agent.Do("approve", "AAAA-AAAA"); err == nil {
		t.Fatal("a wrong code was accepted")
	}
	if err := laptop.agent.Do("approve", code); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(laptop.agent.State().Devices) != 3 { // laptop, recovery code, gpu-box
		t.Fatalf("devices: %+v", laptop.agent.State().Devices)
	}
}

// TestUntrustedRefused: someone the relay lets through (stolen relay
// credentials, or a compromised relay) but whose key isn't in the
// daemon's own signed list is refused in the handshake.
func TestUntrustedRefused(t *testing.T) {
	f := newFleet(t)
	laptop, gpu := f.machine("laptop"), f.machine("gpu-box")
	f.create(laptop)
	f.join(gpu, laptop)
	waitFor(t, "gpu-box connected", func() bool { return connected(laptop, "gpu-box") })

	// The relay accepts gpu-box's signing key, so it pairs this socket with
	// the laptop. The Noise key is the attacker's own.
	gk, _ := cloud.LoadKeys(gpu.dir, false)
	evil, _ := cloud.NewKeys()
	evil.SignSeed = gk.SignSeed
	ws, err := cloud.DialRelay(f.url, evil.Signer(), "/v1/dial?to="+laptop.agent.MachineID())
	if err != nil {
		t.Fatalf("the relay should pair this socket: %v", err)
	}
	var lk *cloud.Keys
	lk, _ = cloud.LoadKeys(laptop.dir, false)
	if _, err := cloud.Initiate(ws, evil, lk.NoisePub); err == nil {
		t.Fatal("the laptop completed a handshake with a key it doesn't trust")
	}
	ws.Close()

	// The real gpu-box still gets in.
	if !connected(laptop, "gpu-box") {
		t.Fatal("the trusted link dropped")
	}
}

// TestRevokeDropsLink: revoking a machine closes its live links within 5
// seconds. The relay here ignores revocations (NoKick), so what closes the
// link is the other daemon applying the signed list.
func TestRevokeDropsLink(t *testing.T) {
	f := newFleet(t)
	f.relay.NoKick = true
	laptop, gpu, pi := f.machine("laptop"), f.machine("gpu-box"), f.machine("pi")
	f.create(laptop)
	f.join(gpu, laptop)
	f.join(pi, laptop)
	waitFor(t, "gpu-box connected", func() bool { return connected(laptop, "gpu-box") })

	// pi revokes the laptop; gpu-box hears of it from the relay.
	start := time.Now()
	if err := pi.agent.Do("revoke", "laptop"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the laptop's link to drop", func() bool { return !connected(laptop, "gpu-box") })
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("link dropped after %v", d)
	}
	// It stays out: gpu-box refuses the laptop's key even though the relay
	// would still pair it.
	time.Sleep(1500 * time.Millisecond)
	if connected(laptop, "gpu-box") {
		t.Fatal("a revoked machine reconnected")
	}
	for _, d := range gpu.agent.State().Devices {
		if d.Name == "laptop" {
			t.Fatal("gpu-box still lists the laptop")
		}
	}
}

// TestRecoveryCode: the recovery code adds a machine without an approval.
func TestRecoveryCode(t *testing.T) {
	f := newFleet(t)
	laptop, gpu := f.machine("laptop"), f.machine("gpu-box")
	var out bytes.Buffer
	if _, err := cloud.Login(cloud.LoginOptions{Relay: f.url, Account: "roy", Name: "laptop", Dir: laptop.dir, Out: &out}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	code := ""
	for i, l := range lines {
		if strings.HasPrefix(l, "Recovery code") && i+2 < len(lines) {
			code = strings.TrimSpace(lines[i+2])
		}
	}
	if _, err := cloud.Login(cloud.LoginOptions{Relay: f.url, Account: "roy", Name: "gpu-box", Dir: gpu.dir, Recovery: "WRONG-CODE-0000-0000-0000"}); err == nil {
		t.Fatal("a wrong recovery code worked")
	}
	a, err := cloud.Login(cloud.LoginOptions{Relay: f.url, Account: "roy", Name: "gpu-box", Dir: gpu.dir, Recovery: code})
	if err != nil {
		t.Fatalf("recovery %q: %v", code, err)
	}
	if a.List.Find(a.Device) == nil || a.List.Version != 2 {
		t.Fatalf("list after recovery: %+v", a.List)
	}
}

// TestRelayPeerOnlyGetsItsOwnTabs: a machine of the account that reaches
// another one through the relay gets that machine's own tabs, whatever
// scope it asks for. It can't reach the machines the other one federates,
// change its machine list or approve devices through it. (One compromised
// machine of the account no longer reaches everything behind the others.)
func TestRelayPeerOnlyGetsItsOwnTabs(t *testing.T) {
	f := newFleet(t)
	laptop, devbox := f.machine("laptop"), f.machine("devbox")
	f.create(devbox)
	f.join(laptop, devbox)
	// devbox has a machine of its own, by its link: the anchor's reach.
	box := hub.New(&config.Config{Name: "box"}, session.NewManager(session.Options{Host: "box", Shell: "/bin/sh", ScriptsDir: t.TempDir()}))
	t.Cleanup(box.Close)
	bs, err := box.Create(proto.CreateReq{Name: "secret-tab", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	const tok = "box-token-0123456789abcdef"
	srv := httptest.NewServer((&server.HTTP{Hub: box, Token: tok}).Handler())
	t.Cleanup(srv.Close)
	if err := devbox.hub.AddLinkHost("box", srv.URL+"/?token="+tok, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "devbox connected over the relay", func() bool { return connected(laptop, "devbox") })

	// The laptop opens its own link to devbox and asks for everything.
	link, err := laptop.agent.Dialer(devbox.agent.MachineID())()
	if err != nil {
		t.Fatal(err)
	}
	defer link.Conn.Close()
	c := link.Conn
	msgs := make(chan proto.Msg, 100)
	go func() {
		for {
			bin, b, err := c.Read()
			if err != nil {
				close(msgs)
				return
			}
			var m proto.Msg
			if !bin && json.Unmarshal(b, &m) == nil {
				msgs <- m
			}
		}
	}()
	next := func(ok func(proto.Msg) bool) proto.Msg {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for {
			select {
			case m, open := <-msgs:
				if !open {
					t.Fatal("link closed")
				}
				if ok(m) {
					return m
				}
			case <-timeout:
				t.Fatal("timed out")
			}
		}
	}
	rid := 0
	call := func(m proto.Msg) proto.Msg {
		t.Helper()
		rid++
		m.Rid = rid
		_ = proto.WriteJSON(c, &m)
		return next(func(r proto.Msg) bool { return r.T == proto.TReply && r.Rid == m.Rid })
	}
	_ = proto.WriteJSON(c, &proto.Msg{T: proto.THello, Client: "web", Scope: hub.ScopeAll, Version: proto.Version})
	if h := next(func(m proto.Msg) bool { return m.T == proto.THello }); h.Scope != hub.ScopeLocal || h.Host != "devbox" {
		t.Fatalf("hello: %+v", h)
	}
	if st := next(func(m proto.Msg) bool { return m.T == proto.TState }); len(st.Hosts) != 1 || st.Hosts[0].Name != "devbox" {
		t.Fatalf("state: %+v", st.Hosts)
	}
	for _, s := range call(proto.Msg{T: proto.TList}).Sessions {
		if s.Host != "devbox" {
			t.Fatalf("a relay peer listed %s", s.ID)
		}
	}
	boxTab := "box/" + bs.ID[strings.LastIndexByte(bs.ID, '/')+1:]
	if r := call(proto.Msg{T: proto.TCapture, ID: boxTab}); !strings.Contains(r.Error, "unknown host") {
		t.Fatalf("a relay peer read a tab on devbox's machine: %+v", r)
	}
	for _, m := range []proto.Msg{
		{T: proto.TRmHost, Name: "box"},
		{T: proto.TAddHost, Name: "evil", Target: "me@evil"},
		{T: proto.TCloud, Name: "revoke", Data: "laptop"},
		{T: proto.TSetSettings, Text: "{}"},
		{T: proto.TUpgrade},
	} {
		if r := call(m); r.Error == "" {
			t.Fatalf("a relay peer was allowed %s", m.T)
		}
	}
	if devbox.hub.Backend("box") == nil {
		t.Fatal("box was removed by a relay peer")
	}
	// What it may do: its own tabs on devbox.
	if r := call(proto.Msg{T: proto.TCreate, Create: &proto.CreateReq{Host: "box", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24}}); r.Error != "" || r.Session.Host != "devbox" {
		t.Fatalf("create: %+v", r)
	}
}
