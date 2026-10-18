package server

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/session"
)

// Trust comes from how a client connected, not from what it says: a relay
// peer gets this machine's own tabs and nothing else, whatever scope it
// asks for, and any client that asks for the local scope gets exactly
// that. These tests give a hub ("anchor") a machine of its own ("gpu", by
// its link) and check what each kind of client can reach.

type pipeClient struct {
	t    *testing.T
	c    proto.Conn
	rid  int
	msgs chan proto.Msg
	data chan []byte
}

func connect(t *testing.T, h *hub.Hub, serve func(*hub.Hub, proto.Conn), hello proto.Msg) (*pipeClient, proto.Msg) {
	t.Helper()
	a, b := net.Pipe()
	go serve(h, proto.NewStreamConn(a, a, a))
	pc := &pipeClient{t: t, c: proto.NewStreamConn(b, b, b), msgs: make(chan proto.Msg, 1000), data: make(chan []byte, 1000)}
	t.Cleanup(func() { pc.c.Close() })
	go func() {
		for {
			bin, raw, err := pc.c.Read()
			if err != nil {
				close(pc.msgs)
				return
			}
			if bin {
				_, _, _, d, _ := proto.DecodeData(raw)
				pc.data <- append([]byte(nil), d...)
				continue
			}
			var m proto.Msg
			if json.Unmarshal(raw, &m) == nil {
				pc.msgs <- m
			}
		}
	}()
	hello.T = proto.THello
	_ = proto.WriteJSON(pc.c, &hello)
	r := pc.wait(func(m proto.Msg) bool { return m.T == proto.THello })
	return pc, r
}

func (p *pipeClient) wait(ok func(proto.Msg) bool) proto.Msg {
	p.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case m, open := <-p.msgs:
			if !open {
				p.t.Fatal("connection closed")
			}
			if ok(m) {
				return m
			}
		case <-timeout:
			p.t.Fatal("timed out")
		}
	}
}

func (p *pipeClient) call(m proto.Msg) proto.Msg {
	p.t.Helper()
	p.rid++
	m.Rid = p.rid
	_ = proto.WriteJSON(p.c, &m)
	return p.wait(func(r proto.Msg) bool { return r.T == proto.TReply && r.Rid == m.Rid })
}

// anchorWithGPU: a hub with one machine, gpu, reached by its link, with a
// tab on each.
func anchorWithGPU(t *testing.T) (anchor, gpu *hub.Hub, gpuTab string) {
	t.Setenv("GECKO_HOME", t.TempDir())
	newHub := func(name string) *hub.Hub {
		m := session.NewManager(session.Options{Host: name, Shell: "/bin/sh", ScriptsDir: t.TempDir()})
		h := hub.New(&config.Config{Name: name}, m)
		t.Cleanup(h.Close)
		return h
	}
	gpu = newHub("gpu")
	s, err := gpu.Create(proto.CreateReq{Name: "train", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	const tok = "gpu-token-0123456789abcdef"
	srv := httptest.NewServer((&HTTP{Hub: gpu, Token: tok}).Handler())
	t.Cleanup(srv.Close)
	anchor = newHub("anchor")
	if _, err := anchor.Create(proto.CreateReq{Name: "build", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	if err := anchor.AddLinkHost("gpu", srv.URL+"/?token="+tok, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := 0
		for _, x := range anchor.Sessions(hub.ScopeAll) {
			if x.Host == "gpu" {
				n++
			}
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("gpu's tab never showed on the anchor")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return anchor, gpu, "gpu/" + s.ID[strings.LastIndexByte(s.ID, '/')+1:]
}

// restrictedChecks runs every request a restricted client must not get.
func restrictedChecks(t *testing.T, who string, p *pipeClient, gpuTab string) {
	t.Helper()
	// Only this machine's tabs.
	r := p.call(proto.Msg{T: proto.TList})
	for _, s := range r.Sessions {
		if s.Host != "anchor" {
			t.Errorf("%s: listed %s", who, s.ID)
		}
	}
	// Another machine's tab doesn't resolve.
	for _, m := range []proto.Msg{
		{T: proto.TCapture, ID: gpuTab},
		{T: proto.TKill, ID: gpuTab},
		{T: proto.TInput, ID: gpuTab, Data: "echo PWNED\n"},
		{T: proto.TListDir, ID: gpuTab},
		{T: proto.TPatch, ID: gpuTab, Patch: &proto.Patch{Seen: true}},
	} {
		if r := p.call(m); !strings.Contains(r.Error, "unknown host") {
			t.Errorf("%s: %s on gpu's tab: %+v", who, m.T, r)
		}
	}
	_ = proto.WriteJSON(p.c, &proto.Msg{T: proto.TAttach, ID: gpuTab, Sub: "s1"})
	if x := p.wait(func(m proto.Msg) bool { return m.T == proto.TExit && m.Sub == "s1" }); !strings.Contains(x.Error, "unknown host") {
		t.Errorf("%s: attach to gpu's tab: %+v", who, x)
	}
	_ = p.c.Write(true, proto.EncodeData(proto.KindInput, gpuTab, 0, []byte("echo PWNED\n")))
	// A new tab opens here, never on gpu.
	r = p.call(proto.Msg{T: proto.TCreate, Create: &proto.CreateReq{Host: "gpu", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24}})
	if r.Error != "" || r.Session == nil || r.Session.Host != "anchor" {
		t.Errorf("%s: create with host gpu: %+v", who, r)
	}
	// Nothing that changes the hub or the account.
	for _, m := range []proto.Msg{
		{T: proto.TAddHost, Name: "x", Target: "me@evil"},
		{T: proto.TAddHost, Kind: hub.KindLink, Name: "x", Secret: "http://127.0.0.1:1/?token=abcdefghijkl"},
		{T: proto.TRmHost, Name: "gpu"},
		{T: proto.TProbeHost, Target: "me@evil"},
		{T: proto.TInstall, Target: "me@evil", OS: "linux/amd64"},
		{T: proto.TTrustHost, Target: "evil", Data: "SHA256:x"},
		{T: proto.TUpdateHost, Name: "gpu"},
		{T: proto.TSetSettings, Text: "{}"},
		{T: proto.TOpenWS, Name: "w"},
		{T: proto.TCloud, Name: "approve", Data: "ABCD-EFGH"},
		{T: proto.TShare, Text: "on"},
		{T: proto.TWindow, Text: `{"W":800,"H":600,"SW":1,"SH":1}`},
		{T: proto.TAnchor, Name: "account", Data: "on"},
	} {
		if r := p.call(m); r.Error == "" {
			t.Errorf("%s: %s was allowed", who, m.T)
		}
	}
	if r := p.call(proto.Msg{T: proto.TSearch, Text: "zz"}); len(r.Matches) != 0 {
		for _, x := range r.Matches {
			if !strings.HasPrefix(x.ID, "anchor/") {
				t.Errorf("%s: search found %s", who, x.ID)
			}
		}
	}
}

// A relay peer that asks for everything still gets only this machine.
func TestPeerScopeIsEnforced(t *testing.T) {
	anchor, gpu, gpuTab := anchorWithGPU(t)
	p, hello := connect(t, anchor, ServePeer, proto.Msg{Client: "web", Scope: hub.ScopeAll})
	if hello.Scope != hub.ScopeLocal || hello.Node != anchor.Node() || hello.Node == "" {
		t.Fatalf("hello: %+v", hello)
	}
	// Its first state names only this machine.
	st := p.wait(func(m proto.Msg) bool { return m.T == proto.TState })
	if len(st.Hosts) != 1 || st.Hosts[0].Name != "anchor" {
		t.Fatalf("state: %+v", st.Hosts)
	}
	restrictedChecks(t, "relay peer", p, gpuTab)
	if r := p.call(proto.Msg{T: proto.TUpgrade}); !strings.Contains(r.Error, "only the Gecko you are using directly") {
		t.Errorf("relay peer: upgrade: %+v", r)
	}
	// gpu is untouched: still on the anchor, its tab alive and not typed into.
	if anchor.Backend("gpu") == nil {
		t.Fatal("gpu was removed")
	}
	time.Sleep(200 * time.Millisecond)
	be, lid, err := gpu.Resolve(gpuTab[4:])
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := be.Capture(lid); strings.Contains(text, "PWNED") {
		t.Fatalf("a relay peer typed into gpu's tab:\n%s", text)
	}
}

// A link hub or bridge asks for the local scope; that is now enforced too
// (it used to be only a display filter).
func TestLocalScopeIsEnforced(t *testing.T) {
	anchor, _, gpuTab := anchorWithGPU(t)
	p, hello := connect(t, anchor, Serve, proto.Msg{Client: "hub", Scope: hub.ScopeLocal})
	if hello.Scope != hub.ScopeLocal {
		t.Fatalf("hello: %+v", hello)
	}
	restrictedChecks(t, "local-scope client", p, gpuTab)
	b, bh := connect(t, anchor, ServeLocal, proto.Msg{Client: "hub", Scope: hub.ScopeLocal})
	if bh.Scope != hub.ScopeLocal {
		t.Fatalf("bridge hello: %+v", bh)
	}
	restrictedChecks(t, "bridge", b, gpuTab)
	// A bridge may still restart the service ("Update Gecko" does that
	// through it); this hub just has no way to.
	if r := b.call(proto.Msg{T: proto.TUpgrade}); strings.Contains(r.Error, "only the Gecko you are using directly") {
		t.Errorf("bridge: upgrade refused as restricted: %+v", r)
	}
}

// The owner (the browser, the phone with the token, the CLI) is not
// narrowed: it reaches gpu's tabs and manages the machine list.
func TestOwnerReachesEverything(t *testing.T) {
	anchor, _, gpuTab := anchorWithGPU(t)
	p, hello := connect(t, anchor, Serve, proto.Msg{Client: "web"})
	if hello.Scope != hub.ScopeAll {
		t.Fatalf("hello: %+v", hello)
	}
	if r := p.call(proto.Msg{T: proto.TCapture, ID: gpuTab}); r.Error != "" {
		t.Fatalf("capture gpu's tab: %+v", r)
	}
	r := p.call(proto.Msg{T: proto.TCreate, Create: &proto.CreateReq{Host: "gpu", Argv: []string{"/bin/sh"}, Cols: 80, Rows: 24}})
	if r.Error != "" || r.Session.Host != "gpu" {
		t.Fatalf("create on gpu: %+v", r)
	}
	if r := p.call(proto.Msg{T: proto.TRmHost, Name: "gpu"}); r.Error != "" {
		t.Fatalf("remove gpu: %+v", r)
	}
	if anchor.Backend("gpu") != nil {
		t.Fatal("gpu is still there")
	}
}

// Making another machine the anchor is for owner clients only: a relay
// peer, or a client that asked for the local scope, stays where it is,
// with nothing more than it had (protocol 11).
func TestPeerCantChangeTheAnchor(t *testing.T) {
	anchor, _, gpuTab := anchorWithGPU(t)
	p, hello := connect(t, anchor, ServePeer, proto.Msg{Client: "web", Scope: hub.ScopeAll, Anchor: []string{"gpu"}})
	if hello.Host != "anchor" || hello.Kind != hub.KindAnchor || hello.Error == "" || hello.Scope != hub.ScopeLocal {
		t.Fatalf("peer asking for gpu as its anchor: %+v", hello)
	}
	restrictedChecks(t, "peer that asked for an anchor", p, gpuTab)
	l, lh := connect(t, anchor, Serve, proto.Msg{Client: "web", Scope: hub.ScopeLocal, Anchor: []string{"gpu"}})
	if lh.Host != "anchor" || lh.Kind != hub.KindAnchor || lh.Scope != hub.ScopeLocal {
		t.Fatalf("local-scope client asking for an anchor: %+v", lh)
	}
	restrictedChecks(t, "local-scope client that asked for an anchor", l, gpuTab)
	// Saying it forwards for an owner gets a peer nothing while this
	// machine doesn't accept anchor requests from the account (the default).
	d, dh := connect(t, anchor, ServePeer, proto.Msg{Client: "web", Scope: hub.ScopeAll, Door: "laptop"})
	if dh.Scope != hub.ScopeLocal {
		t.Fatalf("peer claiming to be a door: %+v", dh)
	}
	restrictedChecks(t, "peer claiming to be a door", d, gpuTab)
	// Nor does asking to turn that on.
	if r := d.call(proto.Msg{T: proto.TAnchor, Name: "account", Data: "on"}); r.Error == "" || anchor.AnchorFromAccount() {
		t.Fatalf("a peer turned on anchor requests from the account: %+v", r)
	}
}

// With "Can be the anchor through my account" on (the owner's choice on
// this machine), a peer forwarding a client for its owner is served as
// the owner; one that doesn't say so still isn't.
func TestAnchorFromAccountIsOptIn(t *testing.T) {
	anchor, _, gpuTab := anchorWithGPU(t)
	o, _ := connect(t, anchor, Serve, proto.Msg{Client: "web"})
	if r := o.call(proto.Msg{T: proto.TAnchor, Name: "account", Data: "on"}); r.Error != "" || r.Text != "on" || !anchor.AnchorFromAccount() {
		t.Fatalf("owner turning it on: %+v", r)
	}
	plain, ph := connect(t, anchor, ServePeer, proto.Msg{Client: "web", Scope: hub.ScopeAll})
	if ph.Scope != hub.ScopeLocal {
		t.Fatalf("a peer that isn't a door: %+v", ph)
	}
	restrictedChecks(t, "peer with the opt-in on", plain, gpuTab)
	d, dh := connect(t, anchor, ServePeer, proto.Msg{Client: "web", Scope: hub.ScopeAll, Door: "laptop"})
	if dh.Scope != hub.ScopeAll {
		t.Fatalf("door with the opt-in on: %+v", dh)
	}
	if r := d.call(proto.Msg{T: proto.TCapture, ID: gpuTab}); r.Error != "" {
		t.Fatalf("the new anchor's owner can't reach gpu: %+v", r)
	}
	if r := o.call(proto.Msg{T: proto.TAnchor, Name: "account", Data: "off"}); r.Text != "off" {
		t.Fatalf("turning it off: %+v", r)
	}
	if _, dh := connect(t, anchor, ServePeer, proto.Msg{Client: "web", Door: "laptop"}); dh.Scope != hub.ScopeLocal {
		t.Fatalf("door after turning it off: %+v", dh)
	}
}
