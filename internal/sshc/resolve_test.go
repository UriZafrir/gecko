package sshc

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	geckoconfig "github.com/gecko-term/gecko/internal/config"
)

func TestResolvConfServers(t *testing.T) {
	f := filepath.Join(t.TempDir(), "resolv.conf")
	os.WriteFile(f, []byte("nameserver 100.100.100.100\nnameserver fe80::1%wlan0\nnameserver [fd7a:115c:a1e0::53]\nnameserver not-an-address\nsearch tail3d2bf5.ts.net\n"), 0o600)
	rc := readResolvConf(f)
	if got := strings.Join(rc.servers, " "); got != "100.100.100.100 fe80::1%wlan0 fd7a:115c:a1e0::53" {
		t.Errorf("servers %q", got)
	}
	for _, s := range rc.servers {
		if _, _, err := net.SplitHostPort(net.JoinHostPort(s, "53")); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
}

func TestCandidates(t *testing.T) {
	ts := []string{"tail3d2bf5.ts.net"}
	for _, c := range []struct {
		host   string
		search []string
		want   string
	}{
		{"ubunty", ts, "ubunty.tail3d2bf5.ts.net ubunty"},
		{"ubunty", nil, "ubunty"},
		{"ubunty.tail3d2bf5.ts.net", ts, "ubunty.tail3d2bf5.ts.net"},
		{"ubunty.", ts, "ubunty"},
		{"devbox.lan", ts, "devbox.lan"},
	} {
		if got := strings.Join(candidates(c.host, c.search), " "); got != c.want {
			t.Errorf("%s: %q, want %q", c.host, got, c.want)
		}
	}
}

// fakeDNS answers A queries for names, NXDOMAIN for others.
func fakeDNS(t *testing.T, names map[string]net.IP) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			q := buf[:n]
			// the question: labels from byte 12, then type and class
			i, labels := 12, []string{}
			for i < len(q) && q[i] != 0 {
				l := int(q[i])
				labels = append(labels, string(q[i+1:i+1+l]))
				i += 1 + l
			}
			qend := i + 5
			qtype := binary.BigEndian.Uint16(q[i+1:])
			name := strings.ToLower(strings.Join(labels, "."))
			resp := append([]byte{}, q[:qend]...)
			resp[2] = 0x81
			resp[3] = 0x80
			binary.BigEndian.PutUint16(resp[6:], 0) // answers
			binary.BigEndian.PutUint16(resp[8:], 0)
			binary.BigEndian.PutUint16(resp[10:], 0)
			ip, ok := names[name]
			switch {
			case !ok:
				resp[3] |= 3 // NXDOMAIN
			case qtype == 1:
				binary.BigEndian.PutUint16(resp[6:], 1)
				resp = append(resp, 0xc0, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4)
				resp = append(resp, ip.To4()...)
			}
			pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

// A short Tailscale name, through the app's DNS file and its search domain.
func TestDialByNameWithAppDNS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hi"))
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	dns := fakeDNS(t, map[string]net.IP{"ubunty.tailtest.ts.net": net.ParseIP("127.0.0.1")})
	_, dnsPort, _ := net.SplitHostPort(dns)
	f := filepath.Join(t.TempDir(), "resolv.conf")
	os.WriteFile(f, []byte("nameserver 127.0.0.1\nsearch tailtest.ts.net\n"), 0o600)
	t.Setenv("GECKO_RESOLV_CONF", f)
	old, oldPort := systemResolver, dnsServerPort
	systemResolver = func() bool { return false }
	dnsServerPort = dnsPort
	defer func() { systemResolver, dnsServerPort = old, oldPort }()

	for _, host := range []string{"ubunty", "ubunty.tailtest.ts.net", "UBUNTY"} {
		c, err := dialTCP(host, port, 5*time.Second)
		if err != nil {
			t.Errorf("%s: %v", host, err)
			continue
		}
		c.Close()
	}
	if _, err := dialTCP("nosuchhost", port, 5*time.Second); err == nil {
		t.Error("nosuchhost resolved")
	}
}

// What Gecko's Add machine runs for "user@ubunty" and "user@ubunty:2222".
func TestBridgeCommandTargets(t *testing.T) {
	home(t)
	for target, want := range map[string]string{
		"user@ubunty":                   "user ubunty 22 ubunty",
		"user@ubunty:2222":              "user ubunty 2222 [ubunty]:2222",
		"user@ubunty.tail3d2bf5.ts.net": "user ubunty.tail3d2bf5.ts.net 22 ubunty.tail3d2bf5.ts.net",
		"user@100.117.62.82":            "user 100.117.62.82 22 100.117.62.82",
		"user@[fd7a:115c:a1e0::5]:2200": "user fd7a:115c:a1e0::5 2200 [fd7a:115c:a1e0::5]:2200",
	} {
		argv := geckoconfig.Host{Name: "x", SSH: target}.BridgeCommand()
		o, err := ParseArgs(argv[1:])
		if err != nil {
			t.Errorf("%s: %v", target, err)
			continue
		}
		r, err := Resolve(o)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join([]string{r.User, r.HostName, strconv.Itoa(r.Port), knownName(r.HostName, r.Port)}, " ")
		if got != want {
			t.Errorf("%s: %q, want %q", target, got, want)
		}
		if r.Strict != "accept-new" || !r.Batch || o.TTY != -1 {
			t.Errorf("%s: options %+v %+v", target, r, o)
		}
	}
}
