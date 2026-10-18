package hub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"github.com/gecko-term/gecko/internal/proto"
)

// A link machine is another Gecko reached directly at its web address with
// its access token (what `gecko url` prints, or the QR code under Open on
// another device), over the same WebSocket the web client uses. It is how
// the Android app puts Gecko in Termux next to This phone, and works for
// any Gecko on the LAN or a tailnet. The token is a secret: it lives only
// in config.json (mode 0600), goes out only in the Authorization header,
// and never appears in HostInfo, errors or logs.

// KindLink is a machine reached by its Gecko link; ViaLink its link path.
const (
	KindLink = "link"
	ViaLink  = "link"
)

// GeckoLink is a parsed Gecko link: the web address and the token.
type GeckoLink struct {
	Base  string // scheme://host[:port]/path/ (no query, no token)
	Token string
}

// ParseLink reads a Gecko link: http(s)://host:port/?token=..., also as
// `gecko url` prints it (with words after it). Plain http is allowed only
// to this device and private networks (see CheckLinkPolicy).
func ParseLink(s string) (GeckoLink, error) {
	s = strings.TrimSpace(s)
	if f := strings.Fields(s); len(f) > 0 {
		s = f[0]
	}
	if s == "" {
		return GeckoLink{}, errors.New("paste the link Gecko prints with `gecko url`, with its token")
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return GeckoLink{}, errors.New("that doesn't look like a Gecko link (http://host:port/?token=...)")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return GeckoLink{}, errors.New("only http and https Gecko links work")
	}
	if u.User != nil {
		return GeckoLink{}, errors.New("links with a user name in them aren't Gecko links")
	}
	tok := u.Query().Get("token")
	if tok == "" {
		return GeckoLink{}, errors.New("the link has no token; copy the whole line `gecko url` prints")
	}
	if len(tok) < 8 || len(tok) > 512 || strings.ContainsAny(tok, " \t\r\n") {
		return GeckoLink{}, errors.New("the token in that link looks damaged; copy it again")
	}
	path := u.EscapedPath()
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	base := u.Scheme + "://" + strings.ToLower(u.Host) + path
	return GeckoLink{Base: base, Token: tok}, nil
}

// String is the link without its token, to show.
func (l GeckoLink) String() string { return l.Base }

// wsURL is the WebSocket address for the link.
func (l GeckoLink) wsURL() string {
	if strings.HasPrefix(l.Base, "https:") {
		return "wss:" + strings.TrimPrefix(l.Base, "https:") + "ws"
	}
	return "ws:" + strings.TrimPrefix(l.Base, "http:") + "ws"
}

// PrivateIP reports whether ip is this device, a LAN (RFC 1918, IPv6 ULA),
// link-local or Tailscale's 100.64.0.0/10: where plain http may carry a
// token, the same rule as the Android app's.
func PrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		if v4[0] == 100 && v4[1]&0xc0 == 64 {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// lookupIP is net.DefaultResolver.LookupIP, replaced in tests.
var lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// CheckLinkPolicy refuses plain http outside private networks: the token
// would cross the internet in the clear. https may go anywhere. Names must
// resolve to private addresses only (the dialer checks again on connect).
func CheckLinkPolicy(l GeckoLink) error {
	if strings.HasPrefix(l.Base, "https:") {
		return nil
	}
	u, err := url.Parse(l.Base)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !PrivateIP(ip) {
			return cleartextErr(host)
		}
		return nil
	}
	if host == "localhost" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := lookupIP(ctx, host)
	if err != nil {
		return fmt.Errorf("can't find %s: %v", host, err)
	}
	for _, ip := range ips {
		if !PrivateIP(ip) {
			return cleartextErr(host)
		}
	}
	return nil
}

func cleartextErr(host string) error {
	return fmt.Errorf("%s isn't on this device or a private network, so its link needs https (an http link would send the token unencrypted)", host)
}

// errCleartext is what the dialer's check returns for a public address.
var errCleartext = errors.New("refusing plain http to a public address")

// LinkDial connects to the Gecko at l over its WebSocket API. With plain
// http, the connection itself is refused unless the address it goes to is
// private, whatever the name resolved to.
func LinkDial(l GeckoLink) DialFunc {
	return func() (*Link, error) {
		nd := &net.Dialer{Timeout: 10 * time.Second}
		if !strings.HasPrefix(l.Base, "https:") {
			nd.Control = func(_, address string, _ syscall.RawConn) error {
				host, _, _ := net.SplitHostPort(address)
				if !PrivateIP(net.ParseIP(host)) {
					return errCleartext
				}
				return nil
			}
		}
		d := websocket.Dialer{
			NetDialContext:    nd.DialContext,
			HandshakeTimeout:  15 * time.Second,
			Proxy:             nil, // a token for a private address doesn't go through a proxy
			EnableCompression: true,
			ReadBufferSize:    64 << 10,
			WriteBufferSize:   32 << 10,
		}
		hdr := http.Header{"Authorization": {"Bearer " + l.Token}}
		c, resp, err := d.Dial(l.wsURL(), hdr)
		if err != nil {
			return nil, explainLinkErr(l, resp, err)
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		c.SetReadLimit(16 << 20)
		return &Link{Conn: &linkConn{c: c}, Explain: func(err error) error {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, net.ErrClosed) {
				return errors.New("connection closed")
			}
			var ce *websocket.CloseError
			if errors.As(err, &ce) {
				return errors.New("connection closed")
			}
			return err
		}}, nil
	}
}

// explainLinkErr turns a failed dial into words, without the token.
func explainLinkErr(l GeckoLink, resp *http.Response, err error) error {
	if resp != nil {
		if resp.Body != nil {
			resp.Body.Close()
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return errors.New("the Gecko at " + l.Base + " refused the token in its link (it changed, or sharing was turned off); paste its current link")
		case http.StatusNotFound:
			return errors.New(l.Base + " answers, but it isn't a Gecko (no /ws there)")
		}
		return fmt.Errorf("%s answered HTTP %d instead of opening Gecko's connection", l.Base, resp.StatusCode)
	}
	if errors.Is(err, errCleartext) {
		u, _ := url.Parse(l.Base)
		return cleartextErr(u.Hostname())
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return errors.New("nothing answers at " + l.Base + " (is Gecko running there?)")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errors.New(l.Base + " didn't answer in time")
	}
	return err
}

// linkConn adapts a client WebSocket to proto.Conn.
type linkConn struct {
	c  *websocket.Conn
	mu sync.Mutex
}

func (w *linkConn) Read() (bool, []byte, error) {
	t, b, err := w.c.ReadMessage()
	return t == websocket.BinaryMessage, b, err
}

func (w *linkConn) Write(bin bool, data []byte) error {
	t := websocket.TextMessage
	if bin {
		t = websocket.BinaryMessage
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return w.c.WriteMessage(t, data)
}

func (w *linkConn) Close() error { return w.c.Close() }

var _ proto.Conn = (*linkConn)(nil)
