// Package hub federates sessions from this machine and from remote hosts
// behind one API. Session ids are "<host>/<local id>".
package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hostsetup"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/session"
)

// Backend is a machine that runs sessions. Ids passed in and out are local
// (without the host prefix).
type Backend interface {
	Info() proto.HostInfo
	Sessions() []proto.SessionInfo
	Create(req proto.CreateReq) (proto.SessionInfo, error)
	Attach(id string, offset int64, sink session.Sink) (detach func(), err error)
	Input(id string, data []byte) error
	Resize(id string, cols, rows int) error
	Kill(id string) error
	Patch(id string, p proto.Patch) error
	Capture(id string) (string, error)
	Search(q string) ([]proto.Match, error)
	ListDir(dir string) (string, []proto.DirEntry, string, error)
	TmuxDo(action, target, arg, tabID string) error
	Tmux() []proto.TmuxSession
	Close()
}

// Scopes for subscribers.
const (
	ScopeAll   = "all"
	ScopeLocal = "local" // only this machine (used by bridges, to avoid loops)
)

type subscriber struct {
	scope string
	fn    func(*proto.Msg)
}

// Hub is the federation point.
type Hub struct {
	// Upgrade, if set, checks that the daemon can restart in place with
	// the gecko binary at exe ("" for the one it runs) and returns what
	// does it, keeping sessions (see daemon.upgrade). That only returns on
	// failure.
	Upgrade func(exe string) (func() error, error)
	// Share, if set, turns the listener for other devices on or off (on
	// with addr), or reports it (action ""). See daemon.sharer.
	Share func(action, addr string) (proto.ShareState, error)

	cfg   *config.Config
	name  string
	node  string // random id of this running hub (see Node)
	local *Local

	mu          sync.Mutex
	backends    map[string]Backend
	order       []string
	subs        map[int]*subscriber
	nextSub     int
	stateDue    *time.Timer
	settingsMod time.Time
	notified    map[string]bool
	nodes       map[string]*Remote // node id -> the Remote connected to it
	helpers     map[int]bool       // helper processes of anchor connections (OpenAnchor)

	cloud         Cloud
	cloudMachines []CloudMachine
}

// New creates a hub around the local session manager.
func New(cfg *config.Config, m *session.Manager) *Hub {
	h := &Hub{cfg: cfg, name: m.Host(), node: newNode(), backends: map[string]Backend{}, subs: map[int]*subscriber{},
		notified: map[string]bool{}, nodes: map[string]*Remote{}}
	h.local = &Local{m: m}
	h.backends[h.name] = h.local
	h.order = []string{h.name}
	m.Listen(func(info *proto.SessionInfo, closed string) { h.sessionEvent(h.name, info, closed) })
	m.OnState(h.stateChanged)
	for _, host := range cfg.Hosts {
		h.addRemote(host)
	}
	if st, err := os.Stat(config.SettingsPath()); err == nil {
		h.settingsMod = st.ModTime()
	}
	go h.watchSettings()
	return h
}

// Name is this machine's name.
func (h *Hub) Name() string { return h.name }

// Node is a random id of this running hub, new every time it starts. The
// hello reply carries it, so a hub can tell that a link leads back to
// itself, or that two of its machines are the same Gecko.
func (h *Hub) Node() string { return h.node }

func newNode() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// claimNode records that r reached the Gecko with node id node. It fails
// when that is this hub itself (a loop) or a Gecko another machine of this
// hub already reaches (a duplicate that would list every tab twice). A
// machine of the account is left to reconcileCloud, which merges by
// machine id.
func (h *Hub) claimNode(node string, r *Remote) error {
	if node == "" {
		return nil // before protocol 10
	}
	if node == h.node {
		return errors.New("this leads back to " + h.name + " itself")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if o := h.nodes[node]; o != nil && o != r && h.backends[o.name] == o {
		if o.kind != KindCloud && r.kind != KindCloud {
			return errors.New("this is the same Gecko as " + o.name + ", which is already connected; remove one of them")
		}
	}
	h.nodes[node] = r
	return nil
}

func (h *Hub) releaseNode(node string, r *Remote) {
	h.mu.Lock()
	if h.nodes[node] == r {
		delete(h.nodes, node)
	}
	h.mu.Unlock()
}

func (h *Hub) addRemote(host config.Host) {
	if host.Name == "" || host.Name == h.name {
		return
	}
	var r *Remote
	if host.URL != "" {
		l := GeckoLink{Base: host.URL, Token: host.Token}
		r = NewRemote(host.Name, host.URL, KindLink, LinkDial(l), h)
	} else {
		r = NewRemote(host.Name, host.SSH, KindSSH, SSHDial(host.Name, host.BridgeCommand()), h)
	}
	r.machine = host.Machine
	h.mu.Lock()
	if old := h.backends[host.Name]; old != nil {
		old.Close()
	} else {
		h.order = append(h.order, host.Name)
	}
	h.backends[host.Name] = r
	h.mu.Unlock()
	go r.Run()
	if r.machine != "" {
		h.reconcileCloud()
	}
}

// Subscribe registers fn for pushes (session changes, state). It returns an
// unsubscribe function.
func (h *Hub) Subscribe(scope string, fn func(*proto.Msg)) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextSub++
	id := h.nextSub
	h.subs[id] = &subscriber{scope: scope, fn: fn}
	return func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
	}
}

func (h *Hub) publish(host string, m *proto.Msg) {
	h.mu.Lock()
	var fns []func(*proto.Msg)
	for _, s := range h.subs {
		if s.scope == ScopeLocal && host != h.name {
			continue
		}
		fns = append(fns, s.fn)
	}
	h.mu.Unlock()
	for _, fn := range fns {
		fn(m)
	}
}

func (h *Hub) sessionEvent(host string, info *proto.SessionInfo, closed string) {
	if info != nil {
		i := h.globalize(host, *info)
		h.publish(host, &proto.Msg{T: proto.TSession, Session: &i})
		h.watchAgent(i)
		return
	}
	h.publish(host, &proto.Msg{T: proto.TClosed, ID: host + "/" + closed})
}

// hostReset is called by a remote that (re)connected: clients replace all
// sessions of that host.
func (h *Hub) hostReset(host string, infos []proto.SessionInfo) {
	out := make([]proto.SessionInfo, len(infos))
	for i, s := range infos {
		out[i] = h.globalize(host, s)
	}
	h.publish(host, &proto.Msg{T: proto.TSessions, Host: host, Sessions: out})
}

func (h *Hub) globalize(host string, s proto.SessionInfo) proto.SessionInfo {
	s.ID = host + "/" + localID(s.ID)
	s.Host = host
	return s
}

func localID(id string) string {
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		return id[i+1:]
	}
	return id
}

// stateChanged coalesces host/agent/tmux updates.
func (h *Hub) stateChanged() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stateDue != nil {
		return
	}
	h.stateDue = time.AfterFunc(100*time.Millisecond, func() {
		h.mu.Lock()
		h.stateDue = nil
		h.mu.Unlock()
		h.publishState()
	})
}

func (h *Hub) publishState() {
	h.mu.Lock()
	subs := make([]*subscriber, 0, len(h.subs))
	for _, s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	all, local := h.State(ScopeAll), h.State(ScopeLocal)
	for _, s := range subs {
		if s.scope == ScopeLocal {
			s.fn(local)
		} else {
			s.fn(all)
		}
	}
}

func (h *Hub) backendList(scope string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if scope == ScopeLocal {
		return []string{h.name}
	}
	return append([]string(nil), h.order...)
}

// Backend returns the machine named name (nil if unknown).
func (h *Hub) Backend(name string) Backend { return h.backend(name) }

func (h *Hub) backend(name string) Backend {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.backends[name]
}

// State returns hosts, tmux sessions and workspace templates.
func (h *Hub) State(scope string) *proto.Msg {
	m := &proto.Msg{T: proto.TState}
	for _, name := range h.backendList(scope) {
		b := h.backend(name)
		if b == nil {
			continue
		}
		info := b.Info()
		info.Name = name
		if info.Local {
			info.Kind, info.Machine = KindLocal, h.MachineID()
		}
		m.Hosts = append(m.Hosts, info)
		tm := b.Tmux()
		for _, t := range tm {
			t.Host = name
			m.Tmux = append(m.Tmux, t)
		}
	}
	if scope != ScopeLocal {
		m.Templates = append(m.Templates, h.cfg.Workspaces...)
	}
	return m
}

// Sessions lists sessions across hosts.
func (h *Hub) Sessions(scope string) []proto.SessionInfo {
	var out []proto.SessionInfo
	for _, name := range h.backendList(scope) {
		if b := h.backend(name); b != nil {
			for _, s := range b.Sessions() {
				out = append(out, h.globalize(name, s))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out
}

// Resolve splits a global id into its backend and local id. Bare local ids
// and unique name / id-prefix matches are accepted for CLI convenience.
func (h *Hub) Resolve(id string) (Backend, string, error) {
	if host, lid, ok := strings.Cut(id, "/"); ok {
		if b := h.backend(host); b != nil {
			return b, lid, nil
		}
		return nil, "", errors.New("unknown host: " + host)
	}
	var match []proto.SessionInfo
	for _, s := range h.Sessions(ScopeAll) {
		if localID(s.ID) == id || s.Name == id || strings.HasPrefix(localID(s.ID), id) {
			match = append(match, s)
		}
	}
	if len(match) != 1 {
		return nil, "", errors.New("no unique session matches " + id)
	}
	return h.backend(match[0].Host), localID(match[0].ID), nil
}

// ResolveScope is Resolve for a client with that scope: with the local
// scope, only sessions of this machine resolve, so a Gecko connected to
// this one can't reach the machines this hub federates.
func (h *Hub) ResolveScope(scope, id string) (Backend, string, error) {
	if scope != ScopeLocal {
		return h.Resolve(id)
	}
	if host, lid, ok := strings.Cut(id, "/"); ok {
		if host != h.name {
			return nil, "", errors.New("unknown host: " + host)
		}
		return h.local, lid, nil
	}
	var match []proto.SessionInfo
	for _, s := range h.Sessions(ScopeLocal) {
		if localID(s.ID) == id || s.Name == id || strings.HasPrefix(localID(s.ID), id) {
			match = append(match, s)
		}
	}
	if len(match) != 1 {
		return nil, "", errors.New("no unique session matches " + id)
	}
	return h.local, localID(match[0].ID), nil
}

// Search looks for q in the text of every session on every host.
func (h *Hub) Search(scope, q string) []proto.Match {
	var (
		mu  sync.Mutex
		out []proto.Match
		wg  sync.WaitGroup
	)
	for _, name := range h.backendList(scope) {
		b := h.backend(name)
		if b == nil {
			continue
		}
		wg.Add(1)
		go func(name string, b Backend) {
			defer wg.Done()
			ms, err := b.Search(q)
			if err != nil {
				return // e.g. an offline host or an older gecko there
			}
			mu.Lock()
			for _, m := range ms {
				m.ID = name + "/" + localID(m.ID)
				out = append(out, m)
			}
			mu.Unlock()
		}(name, b)
	}
	wg.Wait()
	return out
}

// Create starts a session on req.Host (default: this machine).
func (h *Hub) Create(req proto.CreateReq) (proto.SessionInfo, error) {
	host := req.Host
	if host == "" {
		host = h.name
	}
	b := h.backend(host)
	if b == nil {
		return proto.SessionInfo{}, errors.New("unknown host: " + host)
	}
	req.Host = ""
	s, err := b.Create(req)
	if err != nil {
		return s, err
	}
	return h.globalize(host, s), nil
}

// OpenWorkspace creates the tabs of a workspace template.
func (h *Hub) OpenWorkspace(name string, cols, rows int) ([]proto.SessionInfo, error) {
	for _, w := range h.cfg.Workspaces {
		if w.Name != name {
			continue
		}
		tabs := w.Tabs
		if len(tabs) == 0 {
			tabs = []proto.TabSpec{{}}
		}
		var out []proto.SessionInfo
		for _, t := range tabs {
			host, cwd := w.Host, w.Cwd
			if t.Host != "" {
				host = t.Host
			}
			if t.Cwd != "" {
				cwd = t.Cwd
			}
			s, err := h.Create(proto.CreateReq{Host: host, Workspace: w.Name, Name: t.Name, Cwd: cwd, Command: t.Command, Cols: cols, Rows: rows})
			if err != nil {
				return out, err
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, errors.New("no workspace template named " + name)
}

// UpdateHost copies this gecko to a remote machine and restarts the gecko
// there in place, so its sessions keep running. log gets progress lines.
func (h *Hub) UpdateHost(ctx context.Context, name string, log func(string)) error {
	r, ok := h.backend(name).(*Remote)
	if !ok {
		return errors.New("no machine named " + name)
	}
	target := ""
	for _, x := range h.cfg.Hosts {
		if x.Name == name {
			target = x.SSH
		}
	}
	if target == "" {
		return errors.New(name + " isn't reached over ssh, so Gecko can't update it from here")
	}
	if v := r.Version(); v > 0 && v < 3 {
		return fmt.Errorf("the Gecko on %s is too old to restart itself; on %s run: gecko stop && gecko (this ends its sessions), then update again", name, name)
	}
	res, err := hostsetup.Probe(ctx, target, "", log)
	if err != nil {
		return err
	}
	exe, err := hostsetup.Install(ctx, target, res, log)
	if err != nil {
		return err
	}
	_, before := r.Build()
	log("restarting Gecko on " + name + " with " + exe + " (its tabs keep running)")
	if err := r.RestartInPlace(exe); err != nil {
		return fmt.Errorf("installed %s on %s, but it didn't restart: %v", exe, name, err)
	}
	// The link drops and comes back from the restarted gecko.
	dropped := false
	for i := 0; i < 150; i++ {
		time.Sleep(200 * time.Millisecond)
		build, start := r.Build()
		if r.Info().Status != "connected" {
			dropped = true
			continue
		}
		if before != "" && start == before {
			if dropped {
				return errors.New("Gecko on " + name + " couldn't restart; see its log there (~/.gecko-terminal/state/daemon.log)")
			}
			continue
		}
		if before == "" && !dropped {
			continue
		}
		if build == "" {
			// A Gecko before 0.3 restarts only into the binary it was
			// started from, and that wasn't the one just installed.
			return fmt.Errorf("%s restarted, but still runs the older Gecko it was started from, not the one installed at %s. On %s run once: gecko stop && %s (this closes its tabs; later updates keep them)", name, exe, name, exe)
		}
		if r.Version() < proto.Version || Build != "" && build != Build {
			return fmt.Errorf("%s restarted, but runs Gecko %s, not %s from %s. On %s run: %s upgrade", name, build, Build, exe, name, exe)
		}
		log("updated: " + name + " now runs this Gecko")
		return nil
	}
	return errors.New(name + " didn't come back after restarting; check gecko's log there (~/.gecko-terminal/state/daemon.log)")
}

// AddHost adds (or replaces) a remote host and saves the config.
func (h *Hub) AddHost(name, target, gecko string) error {
	name = strings.TrimSpace(name)
	if target == "" {
		return errors.New("a machine needs an ssh destination")
	}
	if err := h.checkHostName(name); err != nil {
		return err
	}
	return h.saveHost(config.Host{Name: name, SSH: target, Gecko: gecko})
}

// checkHostName validates the name of a machine to add.
func (h *Hub) checkHostName(name string) error {
	if name == "" || strings.ContainsAny(name, "/ \t") {
		return errors.New("a machine needs a name, without spaces or slashes")
	}
	if name == h.name {
		return errors.New(name + " is this machine")
	}
	if b, ok := h.backend(name).(*Remote); ok && b.kind == KindCloud {
		return errors.New(name + " is already a machine in your account")
	}
	return nil
}

// ProbeLink connects once to the Gecko at a link and reports its name, OS
// and protocol version, so a machine is added only once it works.
func (h *Hub) ProbeLink(link string) (name, goos string, version int, err error) {
	l, err := ParseLink(link)
	if err != nil {
		return "", "", 0, err
	}
	if err := CheckLinkPolicy(l); err != nil {
		return "", "", 0, err
	}
	r := NewRemote("probe", l.Base, KindLink, LinkDial(l), h)
	lk, hello, err := r.open(r.dial)
	if err != nil {
		return "", "", 0, err
	}
	lk.close()
	if hello.Node != "" {
		if hello.Node == h.node {
			return "", "", 0, errors.New("that link leads to " + h.name + " itself; paste the link of another machine")
		}
		h.mu.Lock()
		o := h.nodes[hello.Node]
		dup := o != nil && h.backends[o.name] == o && o.kind != KindCloud
		h.mu.Unlock()
		if dup {
			return "", "", 0, errors.New("that is the same Gecko as " + o.name + ", which is already one of the machines here")
		}
	}
	return hello.Host, hello.OS, hello.Version, nil
}

// AddLinkHost adds (or replaces) a machine reached at its Gecko link and
// saves it. With check, it is added only if it answers with that token.
func (h *Hub) AddLinkHost(name, link string, check bool) error {
	name = strings.TrimSpace(name)
	if err := h.checkHostName(name); err != nil {
		return err
	}
	l, err := ParseLink(link)
	if err != nil {
		return err
	}
	if err := CheckLinkPolicy(l); err != nil {
		return err
	}
	if check {
		if _, _, _, err := h.ProbeLink(link); err != nil {
			return err
		}
	}
	return h.saveHost(config.Host{Name: name, URL: l.Base, Token: l.Token})
}

func (h *Hub) saveHost(host config.Host) error {
	name := host.Name
	hosts := h.cfg.Hosts[:0:0]
	for _, x := range h.cfg.Hosts {
		if x.Name != name {
			hosts = append(hosts, x)
		}
	}
	h.cfg.Hosts = append(hosts, host)
	if err := h.cfg.Save(); err != nil {
		return err
	}
	h.addRemote(host)
	h.stateChanged()
	return nil
}

// RemoveHost disconnects and forgets a host. Its sessions keep running there.
func (h *Hub) RemoveHost(name string) error {
	h.mu.Lock()
	b := h.backends[name]
	if b == nil || name == h.name {
		h.mu.Unlock()
		return errors.New("unknown host: " + name)
	}
	if r, ok := b.(*Remote); ok && r.kind == KindCloud {
		h.mu.Unlock()
		return errors.New(name + " is in your account; revoke it to remove it")
	}
	delete(h.backends, name)
	for i, n := range h.order {
		if n == name {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
	b.Close()
	hosts := h.cfg.Hosts[:0:0]
	for _, x := range h.cfg.Hosts {
		if x.Name != name {
			hosts = append(hosts, x)
		}
	}
	h.cfg.Hosts = hosts
	h.hostReset(name, nil)
	h.stateChanged()
	return h.cfg.Save()
}

// watchAgent sends the optional webhook notification when an agent starts
// waiting for the user.
func (h *Hub) watchAgent(s proto.SessionInfo) {
	hook := h.cfg.Notify.Webhook
	if hook == "" {
		return
	}
	waiting := s.Agent != nil && s.Agent.Status == "needs-input"
	h.mu.Lock()
	was := h.notified[s.ID]
	h.notified[s.ID] = waiting
	h.mu.Unlock()
	if !waiting || was {
		return
	}
	name := s.Name
	if name == "" {
		name = s.Workspace
	}
	msg := s.Agent.Name + " in " + name + " (" + s.Host + ") needs your input"
	if s.Agent.Detail != "" {
		msg += ": " + s.Agent.Detail
	}
	go func() {
		body, _ := json.Marshal(map[string]string{"title": "Gecko", "message": msg, "session": s.ID})
		ct := "application/json"
		if strings.Contains(hook, "ntfy") {
			body, ct = []byte(msg), "text/plain"
		}
		c := &http.Client{Timeout: 10 * time.Second}
		if resp, err := c.Post(hook, ct, bytes.NewReader(body)); err == nil {
			resp.Body.Close()
		}
	}()
}

// Children returns the PIDs of helper processes (ssh bridges) so a
// replacement process can reap them.
func (h *Hub) Children() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	var pids []int
	for pid := range h.helpers {
		pids = append(pids, pid)
	}
	for _, b := range h.backends {
		if r, ok := b.(*Remote); ok {
			r.mu.Lock()
			if r.pid != 0 {
				pids = append(pids, r.pid)
			}
			r.mu.Unlock()
		}
	}
	return pids
}

// Build is this gecko's build ("v0.2.0"), set by main, to tell whether a
// machine runs the same one.
var Build string

// Suspend takes every remote link down while this daemon restarts in
// place, and keeps them down: the new process connects again. Resume
// brings them back when the restart failed.
func (h *Hub) Suspend() {
	for _, r := range h.remotes() {
		r.suspend()
	}
}

// Resume reconnects the links Suspend took down.
func (h *Hub) Resume() {
	for _, r := range h.remotes() {
		r.resume()
	}
}

func (h *Hub) remotes() []*Remote {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*Remote
	for _, b := range h.backends {
		if r, ok := b.(*Remote); ok {
			out = append(out, r)
		}
	}
	return out
}

// Close shuts down remote connections.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, b := range h.backends {
		b.Close()
	}
}
