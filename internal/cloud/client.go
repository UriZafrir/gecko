package cloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// CheckURL validates a relay address. It must be https (wss), except on
// this machine (127.0.0.1, ::1, localhost), where plain http is allowed
// for tests and local development.
func CheckURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("no relay: pass --relay https://relay.example (run your own with `gecko relay`)")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("bad relay address %q", raw)
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	if u.Scheme == "http" && !loopback(u.Hostname()) {
		return nil, fmt.Errorf("relay %s must use https (plain http only works for a relay on this machine)", u.Host)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("bad relay address %q", raw)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// relayClient makes signed requests to a relay. HTTPS_PROXY is honored for
// both plain requests and WebSockets.
type relayClient struct {
	base   *url.URL
	signer Signer
}

// The default transport already takes its proxy from the environment.
var httpClient = &http.Client{Timeout: 20 * time.Second}

var wsDialer = &websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 15 * time.Second,
	ReadBufferSize: 64 << 10, WriteBufferSize: 64 << 10}

func newRelayClient(relay string, s Signer) (*relayClient, error) {
	u, err := CheckURL(relay)
	if err != nil {
		return nil, err
	}
	return &relayClient{base: u, signer: s}, nil
}

// apiError is an error reply from the relay.
type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

func isStatus(err error, code int) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == code
}

// do sends a signed JSON request (in may be nil) and decodes the reply into
// out (may be nil).
func (c *relayClient) do(method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, c.base.String()+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.signer.Authorize(req, body)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach the relay: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return &apiError{Status: resp.StatusCode, Msg: "relay: " + msg}
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// ws opens a signed WebSocket to path.
func (c *relayClient) ws(path string) (*websocket.Conn, error) {
	u := *c.base
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	p, q, _ := strings.Cut(path, "?")
	u.Path += p
	u.RawQuery = q
	h := http.Header{}
	h.Set("Authorization", c.signer.header("GET", u.RequestURI(), nil))
	ws, resp, err := wsDialer.Dial(u.String(), h)
	if err != nil {
		if resp != nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if msg := strings.TrimSpace(string(b)); msg != "" {
				return nil, &apiError{Status: resp.StatusCode, Msg: "relay: " + msg}
			}
		}
		return nil, fmt.Errorf("can't reach the relay: %w", err)
	}
	return ws, nil
}

// Wire types shared with the relay (internal/relay).

// EnrollReq asks to join (or create) an account.
type EnrollReq struct {
	Account string `json:"account"`
	Device  Device `json:"device"`
}

// EnrollReply says what happens next.
type EnrollReply struct {
	Status  string `json:"status"` // new | pending | approved | gone
	Code    string `json:"code,omitempty"`
	Expires int64  `json:"expires,omitempty"`
}

// Pending is a machine waiting for approval.
type Pending struct {
	Device  Device `json:"device"`
	IP      string `json:"ip"`
	Expires int64  `json:"expires"` // unix ms
}

// Presence is what the relay knows about a device's connection.
type Presence struct {
	ID       string `json:"id"`
	Online   bool   `json:"online"`
	LastSeen int64  `json:"lastSeen,omitempty"` // unix ms
	IP       string `json:"ip,omitempty"`
}

// CtrlMsg is a message on a daemon's control socket (relay to daemon).
type CtrlMsg struct {
	T        string     `json:"t"`                  // list | presence | pending | incoming
	Lists    []List     `json:"lists,omitempty"`    // list: versions after the one the daemon has
	Presence []Presence `json:"presence,omitempty"` // presence: every device of the account
	Pending  []Pending  `json:"pending,omitempty"`  // pending: the whole current set
	Session  string     `json:"session,omitempty"`  // incoming: pairing id for /v1/accept
	From     string     `json:"from,omitempty"`     // incoming: device id the relay says is calling
}
