// Package relay is `gecko relay`, the server that signed-in machines dial
// out to. It knows accounts, their signed device lists and who is online,
// and it pairs a machine that wants to reach another with that machine's
// connection, then forwards the Noise ciphertext between them without
// being able to read it. See docs/cloud.md.
//
// All state is one JSON file in the data directory; there is no database.
package relay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gecko-term/gecko/internal/cloud"
)

const (
	enrollTTL  = 10 * time.Minute // how long a device code stays valid
	maxPending = 5                // machines waiting for approval per account
	pairWait   = 10 * time.Second // for the target machine to pick up a connection
)

// Server is a relay.
type Server struct {
	// Tap, if set, sees every frame the relay forwards between two paired
	// machines (tests use it to check the relay only ever sees ciphertext).
	Tap func(b []byte)
	// TrustProxy takes the client address from X-Forwarded-For (set it when
	// the relay runs behind a reverse proxy that terminates TLS).
	TrustProxy bool
	// NoKick leaves a removed device's sockets open, as a relay that
	// ignores revocations would. Tests use it to show that the daemons
	// enforce revocation themselves.
	NoKick bool

	file string

	mu    sync.Mutex
	st    state
	ctrl  map[string]*ctrlConn // device id -> its daemon's control socket
	pairs map[string]*pair     // pairing id -> waiting initiator
	data  map[string]map[*websocket.Conn]bool
}

type state struct {
	Accounts map[string]*account `json:"accounts"`
}

type account struct {
	Name    string                    `json:"name"`
	Created int64                     `json:"created"`
	Chain   []cloud.List              `json:"chain"`
	Pending []cloud.Pending           `json:"pending,omitempty"`
	Seen    map[string]cloud.Presence `json:"seen,omitempty"` // last seen time and address per device
}

func (a *account) latest() *cloud.List { return &a.Chain[len(a.Chain)-1] }

type pair struct {
	from, to string
	ch       chan *websocket.Conn
}

// ctrlConn is a daemon's control socket. Sends never block the relay: a
// daemon too slow to take its messages is disconnected and reconnects.
type ctrlConn struct {
	ws   *websocket.Conn
	ip   string
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func newCtrl(ws *websocket.Conn, ip string) *ctrlConn {
	c := &ctrlConn{ws: ws, ip: ip, out: make(chan []byte, 64), done: make(chan struct{})}
	go c.writer()
	return c
}

func (c *ctrlConn) send(m *cloud.CtrlMsg) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	select {
	case c.out <- b:
	case <-c.done:
	default:
		c.close()
	}
}

func (c *ctrlConn) close() {
	c.once.Do(func() { close(c.done) })
	c.ws.Close()
}

func (c *ctrlConn) writer() {
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		var err error
		select {
		case b := <-c.out:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err = c.ws.WriteMessage(websocket.TextMessage, b)
		case <-t.C:
			err = c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		case <-c.done:
			return
		}
		if err != nil {
			c.close()
			return
		}
	}
}

// New opens (or creates) the relay state in dir.
func New(dir string) (*Server, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Server{file: filepath.Join(dir, "relay.json"), st: state{Accounts: map[string]*account{}},
		ctrl: map[string]*ctrlConn{}, pairs: map[string]*pair{}, data: map[string]map[*websocket.Conn]bool{}}
	b, err := os.ReadFile(s.file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, errors.New(s.file + ": " + err.Error())
		}
	}
	if s.st.Accounts == nil {
		s.st.Accounts = map[string]*account{}
	}
	// Nobody is connected after a restart.
	for _, a := range s.st.Accounts {
		for id, p := range a.Seen {
			p.Online = false
			a.Seen[id] = p
		}
	}
	return s, nil
}

// save writes the state; callers hold s.mu.
func (s *Server) save() {
	b, err := json.MarshalIndent(&s.st, "", "  ")
	if err != nil {
		return
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		err = os.Rename(tmp, s.file)
	}
	if err != nil {
		log.Printf("relay: saving state: %v", err)
	}
}

// Handler serves the relay API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/enroll", s.enroll)
	mux.HandleFunc("GET /v1/enroll", s.enrollStatus)
	mux.HandleFunc("POST /v1/account", s.createAccount)
	mux.HandleFunc("GET /v1/chain", s.chain)
	mux.HandleFunc("POST /v1/list", s.postList)
	mux.HandleFunc("GET /v1/pending", s.pending)
	mux.HandleFunc("POST /v1/deny", s.deny)
	mux.HandleFunc("GET /v1/connect", s.connect)
	mux.HandleFunc("GET /v1/dial", s.dial)
	mux.HandleFunc("GET /v1/accept", s.accept)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "gecko relay\n")
	})
	return mux
}

var upgrader = websocket.Upgrader{ReadBufferSize: 64 << 10, WriteBufferSize: 64 << 10,
	// Only daemons talk to the relay, never a web page.
	CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}

func (s *Server) clientIP(r *http.Request) string {
	if s.TrustProxy {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			ip, _, _ := strings.Cut(f, ",")
			return strings.TrimSpace(ip)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func readBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

func fail(w http.ResponseWriter, code int, msg string) { http.Error(w, msg, code) }

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// member finds the account and latest entry of a device; callers hold s.mu.
func (s *Server) member(id string) (*account, *cloud.Device) {
	for _, a := range s.st.Accounts {
		if d := a.latest().Find(id); d != nil {
			return a, d
		}
	}
	return nil, nil
}

// auth checks that a request is signed by a device in some account.
func (s *Server) auth(w http.ResponseWriter, r *http.Request, body []byte) (*account, cloud.Device, bool) {
	s.mu.Lock()
	a, d := s.member(cloud.AuthID(r))
	var dev cloud.Device
	if d != nil {
		dev = *d
	}
	s.mu.Unlock()
	if a == nil {
		fail(w, http.StatusUnauthorized, "this device isn't in any account")
		return nil, dev, false
	}
	if err := cloud.CheckAuth(r, body, dev.Sign); err != nil {
		fail(w, http.StatusUnauthorized, err.Error())
		return nil, dev, false
	}
	return a, dev, true
}

func prune(a *account) {
	now := time.Now().UnixMilli()
	out := a.Pending[:0]
	for _, p := range a.Pending {
		if p.Expires > now && a.latest().Find(p.Device.ID) == nil {
			out = append(out, p)
		}
	}
	a.Pending = out
}

func validMachine(d *cloud.Device) error {
	if d.ID != cloud.DeviceID(d.Sign) || d.Kind != cloud.KindMachine || len(d.Noise) != 32 {
		return errors.New("malformed device")
	}
	if d.Name == "" || len(d.Name) > 64 || strings.ContainsAny(d.Name, "/ \n") {
		return errors.New("machine name must be 1 to 64 characters without spaces or slashes")
	}
	return nil
}

// enroll: a machine asks to join an account (or learns it doesn't exist).
func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	var req cloud.EnrollReq
	if err != nil || json.Unmarshal(body, &req) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := validMachine(&req.Device); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cloud.CheckAuth(r, body, req.Device.Sign); err != nil {
		fail(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.st.Accounts[req.Account]
	if a == nil {
		reply(w, cloud.EnrollReply{Status: "new"})
		return
	}
	if a.latest().Find(req.Device.ID) != nil {
		reply(w, cloud.EnrollReply{Status: "approved"})
		return
	}
	if o, _ := s.member(req.Device.ID); o != nil {
		fail(w, http.StatusConflict, "this machine is in another account")
		return
	}
	prune(a)
	p := cloud.Pending{Device: req.Device, IP: s.clientIP(r), Expires: time.Now().Add(enrollTTL).UnixMilli()}
	found := false
	for i := range a.Pending {
		if a.Pending[i].Device.ID == req.Device.ID {
			a.Pending[i], found = p, true
		}
	}
	if !found {
		if len(a.Pending) >= maxPending {
			fail(w, http.StatusTooManyRequests, "too many machines are already waiting for approval in this account")
			return
		}
		a.Pending = append(a.Pending, p)
	}
	s.save()
	s.pushPending(a)
	reply(w, cloud.EnrollReply{Status: "pending", Code: cloud.PairingCode(&p.Device), Expires: p.Expires})
}

// enrollStatus: a waiting machine asks whether it was approved.
func (s *Server) enrollStatus(w http.ResponseWriter, r *http.Request) {
	id := cloud.AuthID(r)
	s.mu.Lock()
	var key []byte
	status := "gone"
	if _, d := s.member(id); d != nil {
		key, status = d.Sign, "approved"
	} else {
		for _, a := range s.st.Accounts {
			prune(a)
			for _, p := range a.Pending {
				if p.Device.ID == id {
					key, status = p.Device.Sign, "pending"
				}
			}
		}
	}
	s.mu.Unlock()
	if key != nil {
		if err := cloud.CheckAuth(r, nil, key); err != nil {
			fail(w, http.StatusUnauthorized, err.Error())
			return
		}
	}
	reply(w, cloud.EnrollReply{Status: status})
}

type listReq struct {
	List *cloud.List `json:"list"`
}

// createAccount: the first machine of a new account posts its first list.
func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	var req listReq
	if err != nil || json.Unmarshal(body, &req) != nil || req.List == nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	l := req.List
	if !cloud.ValidAccount(l.Account) {
		fail(w, http.StatusBadRequest, "bad account name")
		return
	}
	if err := cloud.VerifyGenesis(l); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	signer := l.Find(l.Signer)
	if signer.Kind != cloud.KindMachine || validMachine(signer) != nil {
		fail(w, http.StatusBadRequest, "the first device must be a machine")
		return
	}
	if err := cloud.CheckAuth(r, body, signer.Sign); err != nil {
		fail(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Accounts[l.Account] != nil {
		fail(w, http.StatusConflict, "account exists")
		return
	}
	for _, d := range l.Devices {
		if a, _ := s.member(d.ID); a != nil {
			fail(w, http.StatusConflict, "a device in this list is in another account")
			return
		}
	}
	s.st.Accounts[l.Account] = &account{Name: l.Account, Created: time.Now().UnixMilli(), Chain: []cloud.List{*l},
		Seen: map[string]cloud.Presence{}}
	s.save()
	reply(w, struct{}{})
}

// chain: the device list versions after ?since (all of them by default).
func (s *Server) chain(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.auth(w, r, nil)
	if !ok {
		return
	}
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	s.mu.Lock()
	out := s.since(a, since)
	s.mu.Unlock()
	reply(w, map[string]any{"lists": out})
}

func (s *Server) since(a *account, v int) []cloud.List {
	out := []cloud.List{}
	for _, l := range a.Chain {
		if l.Version > v {
			out = append(out, l)
		}
	}
	return out
}

// postList: a trusted device publishes the next version of the list (an
// approval or a revocation). The relay checks the signature too, so a
// stranger can't fill the chain with junk, but daemons don't rely on that.
func (s *Server) postList(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	a, _, ok := s.auth(w, r, body)
	if !ok {
		return
	}
	var req listReq
	if json.Unmarshal(body, &req) != nil || req.List == nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	l := req.List
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := a.latest()
	if l.Version <= cur.Version {
		fail(w, http.StatusConflict, "the device list changed; fetch it again")
		return
	}
	if err := cloud.Verify(cur, l); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, d := range l.Devices {
		if cur.Find(d.ID) != nil {
			continue
		}
		if o, _ := s.member(d.ID); o != nil && o != a {
			fail(w, http.StatusConflict, "a device in this list is in another account")
			return
		}
		if d.Kind == cloud.KindMachine {
			if err := validMachine(&d); err != nil {
				fail(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	var removed []string
	for _, d := range cur.Devices {
		if l.Find(d.ID) == nil {
			removed = append(removed, d.ID)
		}
	}
	a.Chain = append(a.Chain, *l)
	prune(a)
	for _, id := range removed {
		delete(a.Seen, id)
	}
	s.save()
	for _, id := range removed {
		s.kick(id)
	}
	for _, d := range l.Devices {
		if c := s.ctrl[d.ID]; c != nil {
			c.send(&cloud.CtrlMsg{T: "list", Lists: []cloud.List{*l}})
		}
	}
	s.pushPending(a)
	s.pushPresence(a)
	reply(w, struct{}{})
}

// kick closes every socket of a device that was removed; callers hold s.mu.
func (s *Server) kick(id string) {
	if s.NoKick {
		return
	}
	if c := s.ctrl[id]; c != nil {
		c.close()
		delete(s.ctrl, id)
	}
	for c := range s.data[id] {
		c.Close()
	}
}

func (s *Server) pending(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.auth(w, r, nil)
	if !ok {
		return
	}
	s.mu.Lock()
	prune(a)
	out := append([]cloud.Pending{}, a.Pending...)
	s.mu.Unlock()
	reply(w, map[string]any{"pending": out})
}

func (s *Server) deny(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	a, _, ok := s.auth(w, r, body)
	if !ok {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &req)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := a.Pending[:0]
	for _, p := range a.Pending {
		if p.Device.ID != req.ID {
			out = append(out, p)
		}
	}
	a.Pending = out
	s.save()
	s.pushPending(a)
	reply(w, struct{}{})
}

// pushPending sends the account's waiting machines to its online daemons;
// callers hold s.mu.
func (s *Server) pushPending(a *account) {
	prune(a)
	m := &cloud.CtrlMsg{T: "pending", Pending: append([]cloud.Pending{}, a.Pending...)}
	for _, d := range a.latest().Devices {
		if c := s.ctrl[d.ID]; c != nil {
			c.send(m)
		}
	}
}

func (s *Server) presence(a *account) []cloud.Presence {
	var out []cloud.Presence
	for _, d := range a.latest().Devices {
		if d.Kind != cloud.KindMachine {
			continue
		}
		p := a.Seen[d.ID]
		p.ID = d.ID
		p.Online = s.ctrl[d.ID] != nil
		out = append(out, p)
	}
	return out
}

// pushPresence tells the account's daemons who is online; callers hold s.mu.
func (s *Server) pushPresence(a *account) {
	m := &cloud.CtrlMsg{T: "presence", Presence: s.presence(a)}
	for _, d := range a.latest().Devices {
		if c := s.ctrl[d.ID]; c != nil {
			c.send(m)
		}
	}
}

func (s *Server) machine(w http.ResponseWriter, r *http.Request) (*account, cloud.Device, bool) {
	a, d, ok := s.auth(w, r, nil)
	if ok && d.Kind != cloud.KindMachine {
		fail(w, http.StatusForbidden, "only machines connect")
		return nil, d, false
	}
	return a, d, ok
}

// connect is a daemon's control socket: the relay pushes list updates,
// presence, waiting machines and incoming connections down it.
func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.machine(w, r)
	if !ok {
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(4096)
	c := newCtrl(ws, s.clientIP(r))
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	s.mu.Lock()
	if old := s.ctrl[d.ID]; old != nil {
		old.close()
	}
	s.ctrl[d.ID] = c
	if a.Seen == nil {
		a.Seen = map[string]cloud.Presence{}
	}
	a.Seen[d.ID] = cloud.Presence{ID: d.ID, LastSeen: time.Now().UnixMilli(), IP: c.ip}
	if l := s.since(a, since); len(l) > 0 {
		c.send(&cloud.CtrlMsg{T: "list", Lists: l})
	}
	prune(a)
	c.send(&cloud.CtrlMsg{T: "pending", Pending: append([]cloud.Pending{}, a.Pending...)})
	s.pushPresence(a)
	s.mu.Unlock()

	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			break
		}
	}
	c.close()
	s.mu.Lock()
	if s.ctrl[d.ID] == c {
		delete(s.ctrl, d.ID)
		if a.latest().Find(d.ID) != nil {
			a.Seen[d.ID] = cloud.Presence{ID: d.ID, LastSeen: time.Now().UnixMilli(), IP: c.ip}
			s.save()
		}
		s.pushPresence(a)
	}
	s.mu.Unlock()
}

// dial: a machine asks for a link to another machine of its account.
func (s *Server) dial(w http.ResponseWriter, r *http.Request) {
	a, from, ok := s.machine(w, r)
	if !ok {
		return
	}
	to := r.URL.Query().Get("to")
	s.mu.Lock()
	target := a.latest().Find(to)
	ctrl := s.ctrl[to]
	s.mu.Unlock()
	if target == nil || target.Kind != cloud.KindMachine {
		fail(w, http.StatusNotFound, "no such machine in this account")
		return
	}
	if ctrl == nil {
		fail(w, http.StatusNotFound, target.Name+" is offline")
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	sid := hex.EncodeToString(b)
	p := &pair{from: from.ID, to: to, ch: make(chan *websocket.Conn, 1)}
	s.mu.Lock()
	s.pairs[sid] = p
	s.mu.Unlock()
	ctrl.send(&cloud.CtrlMsg{T: "incoming", Session: sid, From: from.ID})
	select {
	case peer := <-p.ch:
		s.pipe(ws, peer, from.ID, to)
	case <-time.After(pairWait):
		s.mu.Lock()
		delete(s.pairs, sid)
		s.mu.Unlock()
		// accept may have taken the pairing just before it was dropped.
		go func() {
			select {
			case peer := <-p.ch:
				peer.Close()
			case <-time.After(pairWait):
			}
		}()
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "machine didn't answer"), time.Now().Add(time.Second))
		ws.Close()
	}
}

// accept: the target machine picks up a connection it was told about.
func (s *Server) accept(w http.ResponseWriter, r *http.Request) {
	_, d, ok := s.machine(w, r)
	if !ok {
		return
	}
	sid := r.URL.Query().Get("session")
	s.mu.Lock()
	p := s.pairs[sid]
	if p != nil && p.to == d.ID {
		delete(s.pairs, sid)
	} else {
		p = nil
	}
	s.mu.Unlock()
	if p == nil {
		fail(w, http.StatusNotFound, "no such connection")
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	p.ch <- ws
}

// pipe forwards frames both ways until either side closes. The frames are
// Noise messages; the relay has no key to open them.
func (s *Server) pipe(a, b *websocket.Conn, aID, bID string) {
	a.SetReadLimit(1 << 17)
	b.SetReadLimit(1 << 17)
	s.mu.Lock()
	for _, x := range []struct {
		id string
		c  *websocket.Conn
	}{{aID, a}, {aID, b}, {bID, a}, {bID, b}} {
		if s.data[x.id] == nil {
			s.data[x.id] = map[*websocket.Conn]bool{}
		}
		s.data[x.id][x.c] = true
	}
	s.mu.Unlock()
	copyWS := func(dst, src *websocket.Conn, done chan<- struct{}) {
		defer func() { done <- struct{}{} }()
		for {
			t, msg, err := src.ReadMessage()
			if err != nil {
				return
			}
			if s.Tap != nil {
				s.Tap(msg)
			}
			_ = dst.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if dst.WriteMessage(t, msg) != nil {
				return
			}
		}
	}
	done := make(chan struct{}, 2)
	go copyWS(a, b, done)
	go copyWS(b, a, done)
	<-done
	a.Close()
	b.Close()
	<-done
	s.mu.Lock()
	for _, id := range []string{aID, bID} {
		delete(s.data[id], a)
		delete(s.data[id], b)
		if len(s.data[id]) == 0 {
			delete(s.data, id)
		}
	}
	s.mu.Unlock()
}

// ListenAndServe runs a relay on addr, with TLS when cert and key are
// given (otherwise put it behind a TLS-terminating proxy).
func ListenAndServe(s *Server, addr, cert, key string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	host, _, _ := net.SplitHostPort(addr)
	if cert != "" {
		log.Printf("gecko relay on https://%s", addr)
		return srv.ListenAndServeTLS(cert, key)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		log.Printf("gecko relay: serving plain http on %s; clients only accept https for a relay on another machine, so put a TLS proxy in front (or pass --tls-cert/--tls-key)", addr)
	} else {
		log.Printf("gecko relay on http://%s (this machine only)", addr)
	}
	return srv.ListenAndServe()
}
