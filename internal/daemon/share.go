package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/server"
)

// DefaultShareListen is where "Allow other devices" listens: every IPv4
// interface (Wi-Fi, Tailscale), with peers outside private networks refused.
const DefaultShareListen = "0.0.0.0:7682"

// sharer is a second web listener for other devices, with a token of its
// own that is made new each time it is turned on, so turning it off makes
// every link handed out before useless. The Android app turns it on and
// off ("Allow other devices"); `gecko share` does the same.
type sharer struct {
	hub *hub.Hub

	mu    sync.Mutex
	srv   *http.Server
	web   *server.HTTP
	asked string // the address it was turned on with ("0.0.0.0:7682")
	state proto.ShareState
}

func (s *sharer) set(action, addr string) (proto.ShareState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch action {
	case "":
	case "off":
		s.stopLocked()
	case "on":
		if addr == "" {
			addr = DefaultShareListen
		}
		if s.srv != nil && (s.asked == addr || s.state.Listen == addr) {
			break
		}
		s.stopLocked()
		if err := s.startLocked(addr); err != nil {
			return proto.ShareState{}, err
		}
	default:
		return proto.ShareState{}, errors.New(`share: say "on" or "off"`)
	}
	return s.state, nil
}

func (s *sharer) startLocked(addr string) error {
	// 0.0.0.0 means IPv4 only: Go would otherwise open IPv6 too, where a
	// phone usually has a public address.
	network := "tcp"
	if host, _, err := net.SplitHostPort(addr); err == nil && host == "0.0.0.0" {
		network = "tcp4"
	}
	ln, err := net.Listen(network, addr)
	if err != nil {
		return err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		ln.Close()
		return err
	}
	token := hex.EncodeToString(buf)
	listen := ln.Addr().String()
	s.web = &server.HTTP{Hub: s.hub, Token: token, Listen: listen, PrivateOnly: true}
	s.srv = &http.Server{Handler: s.web.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func(srv *http.Server) {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("sharing: %v", err)
		}
	}(s.srv)
	s.asked = addr
	s.state = proto.ShareState{On: true, Listen: listen, Token: token, URLs: server.ShareURLs(listen, token)}
	log.Printf("sharing with other devices on %s", listen)
	return nil
}

func (s *sharer) stopLocked() {
	if s.srv == nil {
		return
	}
	_ = s.srv.Close() // the listener and plain HTTP connections
	s.web.CloseClients()
	s.srv, s.web, s.asked = nil, nil, ""
	s.state = proto.ShareState{}
	log.Printf("stopped sharing with other devices")
}

func (s *sharer) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}
