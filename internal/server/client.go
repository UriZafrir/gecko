// Package server exposes the hub to clients: browsers over WebSockets and
// the CLI / remote bridges over the daemon's local socket.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hostsetup"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/session"
)

// startID identifies this process image; it changes when the daemon
// restarts in place (same PID), so `gecko upgrade` can tell it worked.
var startID = strconv.FormatInt(time.Now().UnixNano(), 36)

type outMsg struct {
	bin  bool
	data []byte
}

// client is one connected client.
type client struct {
	hub   *hub.Hub
	conn  proto.Conn
	scope string
	local bool   // on the daemon's own socket (the CLI, the Android app), not the web
	kind  string // what it said it is in hello: web, cli, hub (another Gecko, through a bridge)
	// peer: another machine of the account, through the relay. Trust comes
	// from how it connected, not from what it says: a peer always gets the
	// local scope, whatever its hello asks for (see restricted).
	peer bool

	out   chan outMsg
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	subs  map[string]func()
	unsub func()
}

// Serve runs the protocol on conn until it closes. It is for clients
// holding this machine's web token (main or share): the owner's browser,
// desktop app, phone, or a Gecko that added this one by its link.
func Serve(h *hub.Hub, conn proto.Conn) { serve(h, conn, false, false) }

// ServeLocal is Serve for a client on the daemon's own socket, which only
// this user can open; a few requests are accepted only there.
func ServeLocal(h *hub.Hub, conn proto.Conn) { serve(h, conn, true, false) }

// ServePeer is Serve for another machine of the account, through the
// relay: it gets this machine's own sessions and nothing else, whatever
// scope it asks for. It can't reach this hub's other machines, change the
// machine list, settings or the account, or restart the service.
func ServePeer(h *hub.Hub, conn proto.Conn) { serve(h, conn, false, true) }

func serve(h *hub.Hub, conn proto.Conn, local, peer bool) {
	scope := hub.ScopeAll
	if peer {
		scope = hub.ScopeLocal
	}
	c := &client{hub: h, conn: conn, scope: scope, out: make(chan outMsg, 8192),
		done: make(chan struct{}), subs: map[string]func(){}, local: local, peer: peer}
	go c.writer()
	defer c.close()
	for {
		bin, b, err := conn.Read()
		if err != nil {
			return
		}
		if bin {
			kind, id, _, data, err := proto.DecodeData(b)
			if err == nil && kind == proto.KindInput {
				if be, lid, err := h.ResolveScope(c.scope, id); err == nil {
					_ = be.Input(lid, data)
				}
			}
			continue
		}
		m := new(proto.Msg)
		if err := json.Unmarshal(b, m); err != nil {
			continue
		}
		switch m.T {
		case proto.TCreate, proto.TCapture, proto.TOpenWS, proto.TAddHost, proto.TRmHost, proto.TList, proto.TSearch, proto.TProbeHost, proto.TInstall, proto.TListDir, proto.TUpdateHost, proto.TTmux, proto.TTrustHost, proto.TCloud:
			// May wait on a remote host; don't stall input for other tabs.
			go c.handle(m)
		default:
			c.handle(m)
		}
	}
}

func (c *client) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
		if c.unsub != nil {
			c.unsub()
		}
		c.mu.Lock()
		for _, d := range c.subs {
			d()
		}
		c.subs = map[string]func(){}
		c.mu.Unlock()
	})
}

func (c *client) writer() {
	for {
		select {
		case m := <-c.out:
			if err := c.conn.Write(m.bin, m.data); err != nil {
				c.close()
				return
			}
		case <-c.done:
			return
		}
	}
}

// enqueue never blocks: a client too slow to keep up is disconnected and
// will resume from its last offset when it reconnects.
func (c *client) enqueue(bin bool, data []byte) {
	select {
	case c.out <- outMsg{bin, data}:
	case <-c.done:
	default:
		go c.close()
	}
}

func (c *client) send(m *proto.Msg) {
	b, err := json.Marshal(m)
	if err == nil {
		c.enqueue(false, b)
	}
}

func (c *client) reply(req *proto.Msg, m *proto.Msg, err error) {
	if req.Rid == 0 {
		if err != nil {
			c.send(&proto.Msg{T: proto.TReply, Error: err.Error()})
		}
		return
	}
	if m == nil {
		m = &proto.Msg{}
	}
	m.T, m.Rid, m.OK = proto.TReply, req.Rid, err == nil
	if err != nil {
		m.Error = err.Error()
	}
	c.send(m)
}

// sink adapts a client attachment to session.Sink.
type sink struct {
	c   *client
	sub string
}

func (s sink) Data(off int64, data []byte) {
	s.c.enqueue(true, proto.EncodeData(proto.KindOutput, s.sub, off, data))
}

func (s sink) Reset(off int64, prefix string) {
	s.c.send(&proto.Msg{T: proto.TReset, Sub: s.sub, Offset: off, Prefix: prefix})
}

func (s sink) Exit() {
	s.c.mu.Lock()
	delete(s.c.subs, s.sub)
	s.c.mu.Unlock()
	s.c.send(&proto.Msg{T: proto.TExit, Sub: s.sub})
}

// restricted reports whether the client only gets this machine: a relay
// peer always, any other client when it asked for that in hello (link
// hubs and bridges do). It can't reach other machines' sessions or change
// what this hub federates.
func (c *client) restricted() bool { return c.scope == hub.ScopeLocal }

// errRestricted is the answer to a restricted client asking for something
// only the Gecko you use directly may do.
var errRestricted = errors.New("only the Gecko you are using directly can do that, not another Gecko connected to it")

// resolve finds a session within the client's scope.
func (c *client) resolve(id string) (hub.Backend, string, error) {
	return c.hub.ResolveScope(c.scope, id)
}

func (c *client) handle(m *proto.Msg) {
	h := c.hub
	switch m.T {
	case proto.THello:
		c.kind = m.Client
		// A client may ask for less (local), never for more than its
		// connection grants.
		if m.Scope == hub.ScopeLocal {
			c.scope = hub.ScopeLocal
		}
		c.send(&proto.Msg{T: proto.THello, Rid: m.Rid, Host: h.Name(), Version: proto.Version, OS: runtime.GOOS, Text: startID, Machine: h.MachineID(), Scope: c.scope, Node: h.Node()})
		if c.unsub == nil {
			c.unsub = h.Subscribe(c.scope, c.send)
		}
		if m.Client != "hub" && m.Client != "cli" {
			c.send(&proto.Msg{T: proto.TSessions, Sessions: h.Sessions(c.scope)})
			c.send(h.State(c.scope))
			if c.scope != hub.ScopeLocal {
				c.send(&proto.Msg{T: proto.TSettings, Text: h.Settings()})
				if st := h.CloudState(); st != nil {
					c.send(&proto.Msg{T: proto.TCloud, Cloud: st})
				}
			}
		} else if m.Client == "hub" {
			// A Gecko connecting to this machine: send the current tmux list
			// right away; otherwise it only arrives when something changes.
			c.send(h.State(c.scope))
		}
	case proto.TPing:
		c.reply(m, nil, nil)
	case proto.TList:
		c.reply(m, &proto.Msg{Sessions: h.Sessions(c.scope)}, nil)
	case proto.TCreate:
		if m.Create == nil {
			c.reply(m, nil, errors.New("create: missing what to create"))
			return
		}
		if c.scope == hub.ScopeLocal {
			m.Create.Host = ""
		}
		s, err := h.Create(*m.Create)
		c.reply(m, &proto.Msg{Session: &s}, err)
	case proto.TAttach:
		sub := m.Sub
		if sub == "" {
			sub = m.ID
		}
		be, lid, err := c.resolve(m.ID)
		if err != nil {
			c.send(&proto.Msg{T: proto.TExit, Sub: sub, Error: err.Error()})
			return
		}
		c.mu.Lock()
		if old := c.subs[sub]; old != nil {
			old()
		}
		c.mu.Unlock()
		detach, err := be.Attach(lid, m.Offset, sink{c, sub})
		if err != nil {
			c.send(&proto.Msg{T: proto.TExit, Sub: sub, Error: err.Error()})
			return
		}
		c.mu.Lock()
		c.subs[sub] = detach
		c.mu.Unlock()
	case proto.TDetach:
		c.mu.Lock()
		d := c.subs[m.Sub]
		delete(c.subs, m.Sub)
		c.mu.Unlock()
		if d != nil {
			d()
		}
	case proto.TResize, proto.TKill, proto.TPatch, proto.TCapture, proto.TInput:
		be, lid, err := c.resolve(m.ID)
		if err != nil {
			c.reply(m, nil, err)
			return
		}
		switch m.T {
		case proto.TResize:
			err = be.Resize(lid, m.Cols, m.Rows)
		case proto.TKill:
			err = be.Kill(lid)
		case proto.TPatch:
			if m.Patch == nil {
				err = errors.New("patch: missing the changes")
			} else {
				err = be.Patch(lid, *m.Patch)
			}
		case proto.TInput:
			err = be.Input(lid, []byte(m.Data))
		case proto.TCapture:
			var text string
			text, err = be.Capture(lid)
			c.reply(m, &proto.Msg{Text: text}, err)
			return
		}
		if m.T != proto.TResize {
			c.reply(m, nil, err)
		}
	case proto.TSearch:
		if len(m.Text) < 2 {
			c.reply(m, &proto.Msg{}, nil)
			return
		}
		c.reply(m, &proto.Msg{Matches: h.Search(c.scope, m.Text)}, nil)
	case proto.TUpdateHost:
		if c.scope == hub.ScopeLocal {
			c.reply(m, nil, errors.New("machines can only be updated from the Gecko you are using directly"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		log := func(line string) { c.send(&proto.Msg{T: proto.THostLog, Sub: m.Sub, Text: line}) }
		c.reply(m, nil, h.UpdateHost(ctx, m.Name, log))
	case proto.TListDir:
		// Folders on the machine of session id (this machine when no id,
		// which is how a remote hub asks its own daemon).
		var be hub.Backend
		dir := m.Dir
		if m.ID != "" {
			b, lid, err := c.resolve(m.ID)
			if err != nil {
				c.reply(m, nil, err)
				return
			}
			be = b
			if dir == "" {
				for _, s := range b.Sessions() {
					if s.ID == lid || strings.HasSuffix(s.ID, "/"+lid) {
						dir = s.Cwd
					}
				}
			}
		}
		var (
			abs  string
			ents []proto.DirEntry
			home string
			err  error
		)
		if be != nil {
			abs, ents, home, err = be.ListDir(dir)
		} else {
			abs, ents, home, err = session.ListDir(dir)
		}
		c.reply(m, &proto.Msg{Dir: abs, Entries: ents, Text: home}, err)
	case proto.TTmux:
		// On the host given (this machine when empty, as a remote hub asks).
		host := m.Host
		if c.scope == hub.ScopeLocal || host == "" {
			host = h.Name()
		}
		be := h.Backend(host)
		if be == nil {
			c.reply(m, nil, errors.New("unknown host: "+host))
			return
		}
		tab := m.ID
		if i := strings.LastIndexByte(tab, '/'); i >= 0 {
			tab = tab[i+1:]
		}
		c.reply(m, nil, be.TmuxDo(m.Name, m.Target, m.Data, tab))
	case proto.TSetSettings:
		if c.scope == hub.ScopeLocal {
			c.reply(m, nil, errors.New("settings belong to the Gecko you are using directly"))
			return
		}
		c.reply(m, nil, h.SetSettings(m.Text))
	case proto.TWindow:
		if c.restricted() {
			c.reply(m, nil, errRestricted)
			return
		}
		// Remember where a fitted app window ended up; `gecko open` starts
		// the next one there, already centered and sized.
		var b struct{ W, H, SX, SY, SW, SH int }
		if json.Unmarshal([]byte(m.Text), &b) == nil && b.W > 100 && b.H > 100 && b.SW > 0 && b.SH > 0 {
			_ = os.WriteFile(filepath.Join(config.StateDir(), "window.json"), []byte(m.Text), 0o600)
		}
	case proto.TUpgrade:
		// A bridge asks for the local scope but is this user over ssh, and
		// "Update Gecko" restarts the daemon through it; a relay peer can't.
		if c.peer {
			c.reply(m, nil, errRestricted)
			return
		}
		if h.Upgrade == nil {
			c.reply(m, nil, errors.New("this Gecko service can't restart itself in place"))
			return
		}
		c.reply(m, nil, nil)
		go func() {
			time.Sleep(200 * time.Millisecond) // let the reply go out first
			if err := h.Upgrade(); err != nil {
				log.Printf("upgrade failed: %v", err)
			}
		}()
	case proto.TShare:
		if !c.local || c.kind == "hub" {
			c.reply(m, nil, errors.New("sharing can only be changed on the device itself"))
			return
		}
		if h.Share == nil {
			c.reply(m, nil, errors.New("this Gecko can't share itself with other devices"))
			return
		}
		st, err := h.Share(m.Text, m.Target)
		if err != nil {
			c.reply(m, nil, err)
			return
		}
		b, _ := json.Marshal(st)
		c.reply(m, &proto.Msg{Text: string(b)}, nil)
	case proto.TOpenWS:
		// Templates may open tabs on other machines.
		if c.restricted() {
			c.reply(m, nil, errRestricted)
			return
		}
		ss, err := h.OpenWorkspace(m.Name, m.Cols, m.Rows)
		c.reply(m, &proto.Msg{Sessions: ss}, err)
	case proto.TAddHost:
		if c.restricted() {
			c.reply(m, nil, errors.New("machines can only be set up from the Gecko you are using directly, not through another one"))
			return
		}
		if m.Kind == hub.KindLink {
			// A machine at its Gecko link; the link (with its token) comes
			// in Secret, so nothing that logs targets sees it.
			c.reply(m, nil, h.AddLinkHost(m.Name, m.Secret, m.Data != "nocheck"))
			return
		}
		c.reply(m, nil, h.AddHost(m.Name, m.Target, m.Data))
	case proto.TProbeHost, proto.TInstall:
		if c.scope == hub.ScopeLocal {
			c.reply(m, nil, errors.New("machines can only be set up from the Gecko you are using directly, not through another one"))
			return
		}
		if m.T == proto.TProbeHost && m.Kind == hub.KindLink {
			name, goos, version, err := h.ProbeLink(m.Secret)
			c.reply(m, &proto.Msg{Host: name, OS: goos, Version: version}, err)
			return
		}
		log := func(line string) { c.send(&proto.Msg{T: proto.THostLog, Sub: m.Sub, Text: line}) }
		if m.T == proto.TProbeHost {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			// The dialog confirms new host keys and asks for passwords itself.
			res, err := hostsetup.ProbeWith(ctx, m.Target, hostsetup.Options{GeckoPath: m.Data, Secret: m.Secret, InstallKey: m.InstallKey, Strict: true}, log)
			r := &proto.Msg{OS: res.OS + "/" + res.Arch, Text: res.Gecko, Missing: res.Missing, CanUseKey: hostsetup.CanUseKey()}
			var se *hostsetup.Error
			if errors.As(err, &se) {
				r.Kind, r.Data = se.Kind, se.Fingerprint
			}
			c.reply(m, r, err)
			return
		}
		goos, goarch, _ := strings.Cut(m.OS, "/")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		c.reply(m, nil, hostsetup.InstallWith(ctx, m.Target, hostsetup.Result{OS: goos, Arch: goarch}, hostsetup.Options{Secret: m.Secret}, log))
	case proto.TTrustHost:
		if c.scope == hub.ScopeLocal {
			c.reply(m, nil, errors.New("machines can only be set up from the Gecko you are using directly, not through another one"))
			return
		}
		c.reply(m, nil, hostsetup.TrustHost(m.Target, m.Data))
	case proto.TAskpass:
		// gecko-askpass, run by ssh for a check or install the daemon started.
		if !c.local || c.kind != "cli" {
			c.reply(m, nil, errors.New("not here"))
			return
		}
		secret, ok := hostsetup.AskpassSecret(m.Text, m.Data)
		if !ok {
			c.reply(m, nil, errors.New("nothing to answer"))
			return
		}
		c.reply(m, &proto.Msg{Secret: secret}, nil)
	case proto.TRmHost:
		if c.restricted() {
			c.reply(m, nil, errors.New("machines can only be removed from the Gecko you are using directly, not through another one"))
			return
		}
		c.reply(m, nil, h.RemoveHost(m.Name))
	case proto.TCloud:
		if c.scope == hub.ScopeLocal {
			c.reply(m, nil, errors.New("the account belongs to the Gecko you are using directly"))
			return
		}
		st, err := h.CloudDo(m.Name, m.Data)
		c.reply(m, &proto.Msg{Cloud: st}, err)
	default:
		c.reply(m, nil, fmt.Errorf("this Gecko service doesn't know %q; it is older than the app talking to it. Restart it: gecko stop && gecko", m.T))
	}
}
