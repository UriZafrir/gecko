package server

// WebTransport (HTTP/3 over QUIC, so UDP) for browsers, next to the
// WebSocket. The page asks /api/wt for the certificate hash and a one-time
// ticket, then opens https://<host>:<port>/wt?ticket=... with
// serverCertificateHashes, since a self-signed certificate is the only kind
// a daemon on localhost can have. The session carries one bidirectional
// stream, framed exactly like the local socket (proto.StreamConn), so the
// protocol on top is the same as over the WebSocket.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"

	"github.com/gecko-term/gecko/internal/proto"
)

// Browsers accept a certificate by hash only if it is ECDSA (we use P-256)
// and valid for at most 14 days in total. Ours lasts 13 and is replaced
// after 6, so one is always valid with a week to spare.
const (
	certValidity = 13 * 24 * time.Hour
	certRotate   = 6 * 24 * time.Hour
	ticketTTL    = 30 * time.Second
)

// certManager holds the WebTransport certificate and replaces it when it
// gets old. Sessions already open keep working across a rotation; new ones
// fetch the new hash first.
type certManager struct {
	mu   sync.Mutex
	now  func() time.Time
	cert *tls.Certificate
	hash [32]byte
	born time.Time
}

func newCertManager() *certManager { return &certManager{now: time.Now} }

// current returns the certificate to present, making a new one if there is
// none yet or it is due for rotation.
func (m *certManager) current() (*tls.Certificate, [32]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if m.cert == nil || now.Sub(m.born) >= certRotate {
		c, h, err := generateCert(now)
		if err != nil {
			return nil, [32]byte{}, err
		}
		m.cert, m.hash, m.born = c, h, now
	}
	return m.cert, m.hash, nil
}

// generateCert makes a self-signed ECDSA P-256 certificate valid from an
// hour ago (clock skew) for certValidity, and its SHA-256 hash, which is
// what the browser checks instead of a chain.
func generateCert(now time.Time) (*tls.Certificate, [32]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, [32]byte{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, [32]byte{}, err
	}
	from := now.Add(-time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "gecko"},
		NotBefore:             from,
		NotAfter:              from.Add(certValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, [32]byte{}, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, [32]byte{}, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, sha256.Sum256(der), nil
}

// tickets are one-time WebTransport credentials. Browsers send no cookies
// on a WebTransport request and the token cookie is HttpOnly, so the page
// trades its cookie for a ticket over HTTP and passes it in the URL.
type tickets struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (t *tickets) issue() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	s := hex.EncodeToString(b)
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]time.Time{}
	}
	for k, exp := range t.m {
		if now.After(exp) {
			delete(t.m, k)
		}
	}
	t.m[s] = now.Add(ticketTTL)
	return s
}

func (t *tickets) redeem(s string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	exp, ok := t.m[s]
	delete(t.m, s)
	return ok && time.Now().Before(exp)
}

// wtOrigin is the Origin check for WebTransport. The page and the session
// share a host but not always a port (a page behind an HTTPS proxy), so
// compare host names only. A foreign page can't get a ticket anyway: that
// takes the SameSite cookie and reading a same-origin response.
func wtOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true // non-browser client
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if loopback(u.Hostname()) && loopback(host) {
		return true // a page on localhost connecting to 127.0.0.1
	}
	return strings.EqualFold(u.Hostname(), host)
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// wtAuthed accepts a ticket from /api/wt, or the token itself (query or
// Bearer header) for clients that have it.
func (s *HTTP) wtAuthed(r *http.Request) bool {
	q := r.URL.Query()
	if t := q.Get("ticket"); t != "" && s.tickets.redeem(t) {
		return true
	}
	if t := q.Get("token"); t != "" && s.tokenOK(t) {
		return true
	}
	return s.authed(r)
}

// wtInfo tells the page how to open a WebTransport session: the UDP port,
// the certificate hash and a fresh ticket. 404 when WebTransport is off.
func (s *HTTP) wtInfo(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.wtMu.Lock()
	ws, started := s.wt, s.wtStarted
	s.wtMu.Unlock()
	if ws == nil {
		if started {
			// Between CloseWT and a restart (upgrading in place).
			http.Error(w, "webtransport is restarting", http.StatusServiceUnavailable)
		} else {
			http.Error(w, "webtransport is off", http.StatusNotFound)
		}
		return
	}
	_, hash, err := s.certs.current()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		// Where to connect when it isn't the page's own host name: a
		// daemon on 127.0.0.1 only, opened as http://localhost, where the
		// browser might try ::1 for QUIC.
		"host":   ws.host,
		"port":   ws.port,
		"hash":   base64.StdEncoding.EncodeToString(hash[:]),
		"ticket": s.tickets.issue(),
	})
}

// StartWT serves WebTransport on pc (UDP, normally the HTTP listener's
// port) in the background until CloseWT. Call it before serving HTTP, so
// /api/wt knows about it.
func (s *HTTP) StartWT(pc net.PacketConn) error {
	s.wtMu.Lock()
	if s.certs == nil {
		s.certs = newCertManager()
	}
	s.wtMu.Unlock()
	// The default UDP buffers are fine for terminal traffic; don't log
	// about them on every start.
	if os.Getenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING") == "" {
		_ = os.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	}
	if _, _, err := s.certs.current(); err != nil {
		return err
	}
	ws := &wtServer{pc: pc, sessions: map[*webtransport.Session]bool{}}
	mux := http.NewServeMux()
	srv := &webtransport.Server{
		H3: http3.Server{
			Handler: mux,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				NextProtos: []string{http3.NextProtoH3},
				GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
					c, _, err := s.certs.current()
					return c, err
				},
			},
			QUICConfig: &quic.Config{
				// A browser that vanished (sleep, network gone) is noticed
				// within MaxIdleTimeout; the client's own pings notice much
				// sooner from its side.
				MaxIdleTimeout:  15 * time.Second,
				KeepAlivePeriod: 5 * time.Second,
				EnableDatagrams: true,
				// Terminal traffic is small. Without PMTU discovery quic-go
				// doesn't set DF on its packets, which also keeps spoofed
				// ICMP "packet too big" messages from hurting a session
				// (fixed in quic-go v0.48.2, which needs a newer Go).
				DisablePathMTUDiscovery: true,
			},
		},
		CheckOrigin: wtOrigin,
	}
	mux.HandleFunc("/wt", func(w http.ResponseWriter, r *http.Request) {
		if !wtOrigin(r) {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		if !s.wtAuthed(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sess, err := srv.Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ws.mu.Lock()
		ws.sessions[sess] = true
		ws.mu.Unlock()
		defer func() {
			ws.mu.Lock()
			delete(ws.sessions, sess)
			ws.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		str, err := sess.AcceptStream(ctx)
		cancel()
		if err != nil {
			_ = sess.CloseWithError(1, "no stream")
			return
		}
		Serve(s.Hub, newWTConn(sess, str))
	})
	ws.srv = srv
	if a, ok := pc.LocalAddr().(*net.UDPAddr); ok {
		ws.port = a.Port
		if a.IP.IsLoopback() {
			ws.host = a.IP.String()
			if a.IP.To4() == nil {
				ws.host = "[" + ws.host + "]"
			}
		}
	}
	s.wtMu.Lock()
	s.wt, s.wtStarted = ws, true
	s.wtMu.Unlock()
	go func() {
		err := srv.Serve(pc)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, quic.ErrServerClosed) {
			log.Printf("webtransport: %v", err)
		}
	}()
	return nil
}

// wtServer is one run of the WebTransport server, StartWT to CloseWT.
type wtServer struct {
	srv      *webtransport.Server
	pc       net.PacketConn
	port     int
	host     string
	mu       sync.Mutex
	sessions map[*webtransport.Session]bool
}

// CloseWT stops WebTransport. Open sessions are closed explicitly, so
// browsers reconnect right away instead of waiting for pings to time out
// (nothing else tells them a UDP socket went away).
func (s *HTTP) CloseWT() {
	s.wtMu.Lock()
	ws := s.wt
	s.wt = nil
	s.wtMu.Unlock()
	if ws == nil {
		return
	}
	ws.mu.Lock()
	n := len(ws.sessions)
	for sess := range ws.sessions {
		_ = sess.CloseWithError(0, "gecko is restarting")
	}
	ws.mu.Unlock()
	if n > 0 {
		time.Sleep(100 * time.Millisecond) // let the close go out
	}
	_ = ws.srv.Close()
	_ = ws.pc.Close()
}

// wtConn is proto.Conn over a WebTransport stream.
type wtConn struct {
	*proto.StreamConn
	sess *webtransport.Session
}

func newWTConn(sess *webtransport.Session, str webtransport.Stream) *wtConn {
	c := &wtConn{sess: sess}
	c.StreamConn = proto.NewStreamConn(str, deadlineWriter{str}, closerFunc(func() error {
		return sess.CloseWithError(0, "")
	}))
	return c
}

// deadlineWriter gives up on a write the peer hasn't let through (flow
// control) in 30 s, like the WebSocket path.
type deadlineWriter struct{ s webtransport.Stream }

func (d deadlineWriter) Write(b []byte) (int, error) {
	_ = d.s.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return d.s.Write(b)
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }
