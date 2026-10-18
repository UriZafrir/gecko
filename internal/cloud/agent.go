package cloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
)

// Agent is a signed-in daemon's side of the account. It keeps one outbound
// WebSocket to the relay (reconnecting with backoff), applies device list
// updates only after checking their signatures, serves Gecko's protocol to
// trusted machines that connect through the relay, and gives the hub a
// Remote for every other machine of the account.
//
// When the machine isn't signed in (or the config turns cloud off) the
// agent does nothing at all: no files written, no network.
type Agent struct {
	// Dir holds the keys and account (default Dir(); tests run several
	// machines in one process).
	Dir string

	hub     *hub.Hub
	serve   func(proto.Conn)
	enabled bool

	mu        sync.Mutex
	acct      *Account
	keys      *Keys
	rc        *relayClient
	presence  map[string]Presence
	pending   []Pending
	connected bool
	errMsg    string
	notice    string
	links     map[*SecureConn]string // live encrypted links -> the other machine's id
	stop      chan struct{}          // ends the current connection loop
}

// NewAgent creates the agent; serve runs the Gecko protocol on a link from
// another machine (server.Serve). Call Start.
func NewAgent(h *hub.Hub, enabled bool, serve func(proto.Conn)) *Agent {
	return &Agent{Dir: Dir(), hub: h, serve: serve, enabled: enabled, links: map[*SecureConn]string{}}
}

// Start signs in from the saved account, if there is one.
func (a *Agent) Start() {
	if err := a.load(); err != nil {
		log.Printf("cloud: %v", err)
	}
}

// load (re)reads the account from disk and (re)starts the relay connection.
func (a *Agent) load() error {
	a.mu.Lock()
	was := a.acct != nil
	if a.stop != nil {
		close(a.stop)
		a.stop = nil
	}
	a.acct, a.keys, a.rc, a.connected, a.errMsg = nil, nil, nil, false, ""
	a.presence, a.pending = map[string]Presence{}, nil
	drop := a.takeLinks()
	a.mu.Unlock()
	closeAll(drop)

	var err error
	if a.enabled && SignedIn(a.Dir) {
		err = a.signIn()
	}
	if was || a.MachineID() != "" {
		a.syncHub()
		a.hub.PublishCloud()
	}
	return err
}

func (a *Agent) signIn() error {
	acct, err := LoadAccount(a.Dir)
	if err != nil || acct == nil {
		return err
	}
	keys, err := LoadKeys(a.Dir, false)
	if err != nil {
		return fmt.Errorf("signed in, but this machine's keys are missing: %w", err)
	}
	if keys.ID() != acct.Device {
		return errors.New("this machine's keys don't match its account; run gecko logout and sign in again")
	}
	rc, err := newRelayClient(acct.Relay, keys.Signer())
	if err != nil {
		return err
	}
	stop := make(chan struct{})
	a.mu.Lock()
	a.acct, a.keys, a.rc, a.stop, a.notice = acct, keys, rc, stop, ""
	a.mu.Unlock()
	go a.run(stop)
	return nil
}

func (a *Agent) takeLinks() []*SecureConn {
	var out []*SecureConn
	for c := range a.links {
		out = append(out, c)
	}
	a.links = map[*SecureConn]string{}
	return out
}

func closeAll(cs []*SecureConn) {
	for _, c := range cs {
		c.Close()
	}
}

// run keeps the control connection up until stop.
func (a *Agent) run(stop chan struct{}) {
	backoff := time.Second
	for {
		start := time.Now()
		err := a.control(stop)
		select {
		case <-stop:
			return
		default:
		}
		a.mu.Lock()
		a.connected = false
		if err != nil {
			a.errMsg = err.Error()
		}
		for id, p := range a.presence {
			p.Online = false
			a.presence[id] = p
		}
		a.mu.Unlock()
		a.syncHub()
		a.hub.PublishCloud()
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
		}
		wait := backoff + time.Duration(rand.Int64N(int64(backoff/2)+1))
		select {
		case <-time.After(wait):
		case <-stop:
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// control runs one control connection: the relay pushes device lists,
// presence, waiting machines and incoming links down it.
func (a *Agent) control(stop chan struct{}) error {
	a.mu.Lock()
	rc, v := a.rc, a.acct.List.Version
	a.mu.Unlock()
	ws, err := rc.ws("/v1/connect?since=" + strconv.Itoa(v))
	if err != nil {
		if isStatus(err, 401) {
			// The relay no longer knows this machine: most likely it was
			// removed while this daemon was offline. The signed list will
			// say for sure once we can read it; until then keep trying.
			return errors.New("the relay doesn't accept this machine (was it removed from the account?)")
		}
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-stop:
			ws.Close()
		case <-done:
		}
	}()
	ws.SetReadLimit(8 << 20)
	a.mu.Lock()
	a.connected, a.errMsg = true, ""
	a.mu.Unlock()
	a.hub.PublishCloud()
	for {
		_, b, err := ws.ReadMessage()
		if err != nil {
			ws.Close()
			return errors.New("lost the connection to the relay")
		}
		var m CtrlMsg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		switch m.T {
		case "list":
			a.applyLists(m.Lists)
		case "presence":
			a.mu.Lock()
			a.presence = map[string]Presence{}
			for _, p := range m.Presence {
				a.presence[p.ID] = p
			}
			a.mu.Unlock()
			a.syncHub()
			a.hub.PublishCloud()
		case "pending":
			a.mu.Lock()
			a.pending = m.Pending
			a.mu.Unlock()
			a.hub.PublishCloud()
		case "incoming":
			go a.accept(m.Session)
		}
	}
}

// applyLists applies new versions of the device list, each checked against
// the one before it. A version that doesn't verify stops the update: the
// relay can deliver lists but can't make them.
func (a *Agent) applyLists(ls []List) {
	a.mu.Lock()
	acct := a.acct
	if acct == nil {
		a.mu.Unlock()
		return
	}
	changed := false
	for i := range ls {
		l := ls[i]
		if l.Version <= acct.List.Version {
			continue
		}
		if err := Verify(acct.List, &l); err != nil {
			a.errMsg = "ignored a device list update that doesn't verify: " + err.Error()
			log.Printf("cloud: %s", a.errMsg)
			break
		}
		acct.List, changed = &l, true
	}
	if !changed {
		a.mu.Unlock()
		return
	}
	if err := acct.save(); err != nil {
		log.Printf("cloud: saving the device list: %v", err)
	}
	removed := acct.List.Find(acct.Device) == nil
	var drop []*SecureConn
	for c, id := range a.links {
		if d := acct.List.Find(id); removed || d == nil || string(d.Noise) != string(c.Peer()) {
			drop = append(drop, c)
			delete(a.links, c)
		}
	}
	a.mu.Unlock()
	// Revoked machines lose their live links right away.
	closeAll(drop)
	if removed {
		a.removed()
		return
	}
	a.syncHub()
	a.hub.PublishCloud()
}

// removed: another machine took this one out of the account.
func (a *Agent) removed() {
	a.mu.Lock()
	name := ""
	if a.acct != nil {
		name = a.acct.Name
	}
	a.mu.Unlock()
	log.Printf("cloud: this machine was removed from account %s; signing out", name)
	_ = forget(a.Dir)
	_ = a.load()
	a.mu.Lock()
	a.notice = "This machine was removed from account " + name + " and is signed out."
	a.mu.Unlock()
	a.hub.PublishCloud()
}

// trusted finds the machine with Noise key peer in our own copy of the list.
func (a *Agent) trusted(peer []byte) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.acct == nil {
		return "", false
	}
	d := a.acct.List.Machine(peer)
	if d == nil || d.ID == a.acct.Device {
		return "", false
	}
	return d.ID, true
}

// accept picks up a link another machine asked the relay for.
func (a *Agent) accept(sid string) {
	a.mu.Lock()
	rc, keys := a.rc, a.keys
	a.mu.Unlock()
	if rc == nil {
		return
	}
	ws, err := rc.ws("/v1/accept?session=" + sid)
	if err != nil {
		return
	}
	var peerID string
	sc, err := Accept(ws, keys, func(peer []byte) bool {
		id, ok := a.trusted(peer)
		peerID = id
		return ok
	})
	if err != nil {
		log.Printf("cloud: incoming link: %v", err)
		ws.Close()
		return
	}
	if !a.track(sc, peerID) {
		sc.Close()
		return
	}
	a.serve(sc)
	a.untrack(sc)
}

func (a *Agent) track(c *SecureConn, id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.acct == nil || a.acct.List.Find(id) == nil {
		return false // revoked during the handshake
	}
	a.links[c] = id
	return true
}

func (a *Agent) untrack(c *SecureConn) {
	a.mu.Lock()
	delete(a.links, c)
	a.mu.Unlock()
	c.Close()
}

// dialer reaches machine id through the relay. Its Noise key comes from
// our verified list, never from the relay.
func (a *Agent) dialer(id string) hub.DialFunc {
	return func() (*hub.Link, error) {
		a.mu.Lock()
		rc, keys := a.rc, a.keys
		var d *Device
		if a.acct != nil {
			d = a.acct.List.Find(id)
		}
		a.mu.Unlock()
		if rc == nil || d == nil {
			return nil, errors.New("no longer in the account")
		}
		ws, err := rc.ws("/v1/dial?to=" + id)
		if err != nil {
			if isStatus(err, 404) {
				return nil, errors.New(d.Name + " is offline")
			}
			return nil, err
		}
		sc, err := Initiate(ws, keys, d.Noise)
		if err != nil {
			ws.Close()
			return nil, err
		}
		if !a.track(sc, id) {
			sc.Close()
			return nil, errors.New("no longer in the account")
		}
		return &hub.Link{Conn: &trackedConn{sc, a}}, nil
	}
}

// Dialer reaches machine id through the relay, as this machine's hub does
// (for tests and tools; the hub gets it through SetCloudMachines).
func (a *Agent) Dialer(id string) hub.DialFunc { return a.dialer(id) }

// trackedConn forgets the link when the hub closes it.
type trackedConn struct {
	*SecureConn
	a *Agent
}

func (t *trackedConn) Close() error {
	t.a.untrack(t.SecureConn)
	return nil
}

// syncHub tells the hub which machines the account has and which are online.
func (a *Agent) syncHub() {
	a.mu.Lock()
	var ms []hub.CloudMachine
	if a.acct != nil {
		for _, d := range a.acct.List.Devices {
			if d.Kind != KindMachine || d.ID == a.acct.Device {
				continue
			}
			p := a.presence[d.ID]
			ms = append(ms, hub.CloudMachine{ID: d.ID, Name: d.Name, OS: d.OS, Online: p.Online, LastSeen: p.LastSeen, Dial: a.dialer(d.ID)})
		}
	}
	a.mu.Unlock()
	a.hub.SetCloudMachines(ms)
}

// MachineID is this machine's id in the account ("" when signed out).
func (a *Agent) MachineID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.acct == nil {
		return ""
	}
	return a.acct.Device
}

// State is the account as the interface shows it.
func (a *Agent) State() *proto.CloudState {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := &proto.CloudState{Notice: a.notice}
	if a.acct == nil {
		return st
	}
	l := a.acct.List
	st.SignedIn, st.Account, st.Fingerprint, st.Relay, st.Self = true, a.acct.Name, a.acct.Fingerprint, a.acct.Relay, a.acct.Device
	st.Connected, st.Error = a.connected, a.errMsg
	for _, d := range l.Devices {
		p := a.presence[d.ID]
		cd := proto.CloudDevice{ID: d.ID, Name: d.Name, OS: d.OS, Kind: d.Kind, Online: p.Online, LastSeen: p.LastSeen,
			IP: p.IP, Added: d.Added, Self: d.ID == a.acct.Device}
		if cd.Self {
			cd.Online = a.connected
		}
		if b := l.Find(d.By); b != nil {
			cd.By = b.Name
		}
		st.Devices = append(st.Devices, cd)
	}
	now := time.Now().UnixMilli()
	for _, p := range a.pending {
		if p.Expires > now && l.Find(p.Device.ID) == nil {
			st.Pending = append(st.Pending, proto.CloudPending{ID: p.Device.ID, Name: p.Device.Name, OS: p.Device.OS, IP: p.IP, Expires: p.Expires})
		}
	}
	return st
}

// Do runs an account action for the CLI or the interface.
func (a *Agent) Do(action, arg string) error {
	switch action {
	case "status":
		return nil
	case "reload":
		return a.load()
	case "approve":
		return a.approve(arg)
	case "deny":
		return a.deny(arg)
	case "revoke":
		return a.revoke(arg)
	case "logout":
		return a.logout()
	}
	return fmt.Errorf("unknown account action %q", action)
}

func (a *Agent) session() (*Account, *relayClient, Signer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.enabled {
		return nil, nil, Signer{}, errors.New(`cloud features are off ("cloud": {"enabled": false} in the config)`)
	}
	if a.acct == nil {
		return nil, nil, Signer{}, errors.New("this machine isn't signed in (gecko login)")
	}
	acct := *a.acct
	return &acct, a.rc, a.keys.Signer(), nil
}

// approve adds the waiting machine whose code is code. arg is "CODE" or
// "ID CODE" (the interface names the machine it showed).
func (a *Agent) approve(arg string) error {
	acct, rc, s, err := a.session()
	if err != nil {
		return err
	}
	f := strings.Fields(arg)
	if len(f) == 0 {
		return errors.New("approve needs the code the new machine shows")
	}
	want, id := NormalizeCode(f[len(f)-1]), ""
	if len(f) > 1 {
		id = f[0]
	}
	if len(want) != 8 {
		return errors.New("a code looks like ABCD-EFGH")
	}
	var pr pendingReply
	if err := rc.do("GET", "/v1/pending", nil, &pr); err != nil {
		return err
	}
	var match *Pending
	for i := range pr.Pending {
		p := &pr.Pending[i]
		if p.Device.Kind == KindMachine && p.Device.ID == DeviceID(p.Device.Sign) && NormalizeCode(PairingCode(&p.Device)) == want {
			match = p
		}
	}
	if match == nil || (id != "" && match.Device.ID != id) {
		return errors.New("no machine is waiting with that code; check it on the new machine (codes expire after 10 minutes)")
	}
	dev := match.Device
	dev.By, dev.Added = s.ID, time.Now().UnixMilli()
	lists, err := publish(rc, acct.List, s, func(l *List) error {
		l.Devices = append(without(l.Devices, dev.ID), dev)
		return nil
	})
	if err != nil {
		return err
	}
	a.applyLists(lists)
	log.Printf("cloud: approved %s (%s, %s)", dev.Name, dev.OS, match.IP)
	return nil
}

// deny drops a waiting machine, named by id or code.
func (a *Agent) deny(arg string) error {
	_, rc, _, err := a.session()
	if err != nil {
		return err
	}
	var pr pendingReply
	if err := rc.do("GET", "/v1/pending", nil, &pr); err != nil {
		return err
	}
	for _, p := range pr.Pending {
		if p.Device.ID == arg || NormalizeCode(PairingCode(&p.Device)) == NormalizeCode(arg) {
			return rc.do("POST", "/v1/deny", map[string]string{"id": p.Device.ID}, nil)
		}
	}
	return errors.New("no machine is waiting with that code")
}

// revoke removes a machine (by name or id) from the account. Its links to
// every machine close as soon as they get the new list.
func (a *Agent) revoke(arg string) error {
	acct, rc, s, err := a.session()
	if err != nil {
		return err
	}
	var hit []Device
	for _, d := range acct.List.Devices {
		if d.ID == arg || d.Name == arg {
			hit = append(hit, d)
		}
	}
	switch {
	case len(hit) == 0:
		return fmt.Errorf("no device named %q in account %s", arg, acct.Name)
	case len(hit) > 1:
		return fmt.Errorf("more than one device is named %q; revoke it by id", arg)
	case hit[0].Kind == KindRecovery:
		return errors.New("the recovery code can't be revoked")
	}
	lists, err := publish(rc, acct.List, s, func(l *List) error {
		l.Devices = without(l.Devices, hit[0].ID)
		return nil
	})
	if err != nil {
		return err
	}
	log.Printf("cloud: revoked %s", hit[0].Name)
	a.applyLists(lists)
	return nil
}

// logout takes this machine out of the account and forgets its keys.
func (a *Agent) logout() error {
	a.mu.Lock()
	var acct *Account
	if a.acct != nil {
		c := *a.acct
		acct = &c
	}
	keys := a.keys
	a.mu.Unlock()
	if acct != nil && keys != nil {
		removeSelf(acct, keys)
	}
	if err := forget(a.Dir); err != nil {
		return err
	}
	return a.load()
}
