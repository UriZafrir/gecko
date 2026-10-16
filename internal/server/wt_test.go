package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/session"
)

func TestCertGenerationAndRotation(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	m := &certManager{now: func() time.Time { return now }}
	c1, h1, err := m.current()
	if err != nil {
		t.Fatal(err)
	}
	leaf := c1.Leaf
	if d := leaf.NotAfter.Sub(leaf.NotBefore); d > 14*24*time.Hour {
		t.Fatalf("validity %v: browsers refuse more than 14 days", d)
	}
	if !leaf.NotBefore.Before(now) || !leaf.NotAfter.After(now.Add(certRotate)) {
		t.Fatalf("not valid across its rotation period: %v .. %v", leaf.NotBefore, leaf.NotAfter)
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		t.Fatalf("want ECDSA P-256, got %T", leaf.PublicKey)
	}
	if h1 != sha256.Sum256(c1.Certificate[0]) {
		t.Fatal("hash is not the SHA-256 of the certificate")
	}

	now = now.Add(certRotate - time.Minute)
	if c, h, _ := m.current(); c != c1 || h != h1 {
		t.Fatal("rotated too early")
	}
	now = now.Add(2 * time.Minute)
	c2, h2, _ := m.current()
	if c2 == c1 || h2 == h1 {
		t.Fatal("not rotated after certRotate")
	}
	if h2 != sha256.Sum256(c2.Certificate[0]) || !c2.Leaf.NotAfter.After(now.Add(certRotate)) {
		t.Fatal("rotated certificate is wrong")
	}
}

func TestTicketsAreSingleUse(t *testing.T) {
	var tk tickets
	s := tk.issue()
	if !tk.redeem(s) {
		t.Fatal("fresh ticket refused")
	}
	if tk.redeem(s) {
		t.Fatal("ticket worked twice")
	}
	if tk.redeem("nope") {
		t.Fatal("made-up ticket accepted")
	}
}

func TestWTOrigin(t *testing.T) {
	for _, c := range []struct {
		origin, host string
		ok           bool
	}{
		{"", "127.0.0.1:7681", true},
		{"http://127.0.0.1:7681", "127.0.0.1:7681", true},
		{"https://box.example.ts.net", "box.example.ts.net:7681", true},
		{"http://[::1]:7681", "[::1]:7681", true},
		{"https://evil.example", "127.0.0.1:7681", false},
	} {
		r := httptest.NewRequest("GET", "/wt", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := wtOrigin(r); got != c.ok {
			t.Errorf("origin %q host %q: got %v", c.origin, c.host, got)
		}
	}
}

// TestWebTransport opens real WebTransport sessions against the daemon's
// HTTP + QUIC servers: no credentials is refused, a ticket from /api/wt
// works once, and JSON and binary frames flow both ways.
func TestWebTransport(t *testing.T) {
	m := session.NewManager(session.Options{Host: "test", Shell: "/bin/sh", ScriptsDir: t.TempDir()})
	h := hub.New(&config.Config{Name: "test"}, m)
	defer m.Shutdown(time.Second)
	web := &HTTP{Hub: h, Token: "secret"}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := web.StartWT(pc); err != nil {
		t.Fatal(err)
	}
	defer web.CloseWT()
	hs := httptest.NewServer(web.Handler())
	defer hs.Close()

	getInfo := func(auth bool) (int, map[string]any) {
		req, _ := http.NewRequest("GET", hs.URL+"/api/wt", nil)
		if auth {
			req.Header.Set("Authorization", "Bearer secret")
		}
		rsp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer rsp.Body.Close()
		var v map[string]any
		_ = json.NewDecoder(rsp.Body).Decode(&v)
		return rsp.StatusCode, v
	}
	if code, _ := getInfo(false); code != http.StatusUnauthorized {
		t.Fatalf("/api/wt without auth: %d", code)
	}
	_, info := getInfo(true)
	hash, _ := base64.StdEncoding.DecodeString(info["hash"].(string))
	port := int(info["port"].(float64))
	if port != pc.LocalAddr().(*net.UDPAddr).Port {
		t.Fatalf("port %d", port)
	}

	// Trust the certificate by hash, the way a browser does with
	// serverCertificateHashes.
	d := &webtransport.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				if sum := sha256.Sum256(raw[0]); !bytes.Equal(sum[:], hash) {
					return errors.New("certificate hash mismatch")
				}
				return nil
			},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
	}
	defer d.Close()
	base := fmt.Sprintf("https://127.0.0.1:%d/wt", port)
	dial := func(url string) (*http.Response, *webtransport.Session, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return d.Dial(ctx, url, http.Header{"Origin": {fmt.Sprintf("http://127.0.0.1:%d", port)}})
	}

	if rsp, _, err := dial(base); err == nil || rsp == nil || rsp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no credentials: rsp %v err %v", rsp, err)
	}
	if rsp, _, err := dial(base + "?token=wrong"); err == nil || rsp == nil || rsp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: rsp %v err %v", rsp, err)
	}
	ticket := info["ticket"].(string)
	_, sess, err := dial(base + "?ticket=" + ticket)
	if err != nil {
		t.Fatalf("with ticket: %v", err)
	}
	defer sess.CloseWithError(0, "")
	if rsp, _, err := dial(base + "?ticket=" + ticket); err == nil || rsp == nil || rsp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused ticket: rsp %v err %v", rsp, err)
	}

	str, err := sess.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := proto.NewStreamConn(str, str, str)
	type frame struct {
		bin bool
		b   []byte
	}
	in := make(chan frame, 256)
	go func() {
		for {
			bin, b, err := c.Read()
			if err != nil {
				close(in)
				return
			}
			in <- frame{bin, b}
		}
	}()
	wait := func(what string, pred func(frame) bool) {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case f, ok := <-in:
				if !ok {
					t.Fatalf("closed waiting for %s", what)
				}
				if pred(f) {
					return
				}
			case <-timeout:
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	msg := func(f frame) proto.Msg {
		var m proto.Msg
		if !f.bin {
			_ = json.Unmarshal(f.b, &m)
		}
		return m
	}

	_ = proto.WriteJSON(c, &proto.Msg{T: proto.THello, Client: "test"})
	wait("hello", func(f frame) bool { return msg(f).T == proto.THello })
	_ = proto.WriteJSON(c, &proto.Msg{T: proto.TPing, Rid: 7})
	wait("ping reply", func(f frame) bool { m := msg(f); return m.T == proto.TReply && m.Rid == 7 && m.OK })

	_ = proto.WriteJSON(c, &proto.Msg{T: proto.TCreate, Rid: 8, Create: &proto.CreateReq{Argv: []string{"/bin/sh"}, Workspace: "w"}})
	var id string
	wait("create reply", func(f frame) bool {
		m := msg(f)
		if m.T == proto.TReply && m.Rid == 8 && m.Session != nil {
			id = m.Session.ID
			return true
		}
		return false
	})
	_ = proto.WriteJSON(c, &proto.Msg{T: proto.TAttach, ID: id, Sub: "p1"})
	_ = c.Write(true, proto.EncodeData(proto.KindInput, id, 0, []byte("echo over-$((6*7))-quic\n")))
	var out strings.Builder
	wait("output", func(f frame) bool {
		if !f.bin {
			return false
		}
		kind, sub, _, data, err := proto.DecodeData(f.b)
		if err == nil && kind == proto.KindOutput && sub == "p1" {
			out.Write(data)
		}
		return strings.Contains(out.String(), "over-42-quic")
	})
}
