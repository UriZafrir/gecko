package hub

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestParseLink(t *testing.T) {
	tok := "0123456789abcdef0123"
	for in, want := range map[string]string{
		"http://127.0.0.1:7683/?token=" + tok:                         "http://127.0.0.1:7683/",
		"  http://127.0.0.1:7683/?token=" + tok + "  (other devices)": "http://127.0.0.1:7683/",
		"https://Box.tail1234.ts.net/?token=" + tok:                   "https://box.tail1234.ts.net/",
		"http://[fd7a:115c:a1e0::1]:7681?token=" + tok:                "http://[fd7a:115c:a1e0::1]:7681/",
		"https://example.com/gecko?token=" + tok + "#x":               "https://example.com/gecko/",
	} {
		l, err := ParseLink(in)
		if err != nil || l.Base != want || l.Token != tok {
			t.Errorf("%q: %+v %v, want %s", in, l, err, want)
		}
		if strings.Contains(l.String(), tok) {
			t.Errorf("%q: String shows the token", in)
		}
	}
	for _, bad := range []string{"", "http://127.0.0.1:7683/", "ssh://me@box", "http://me@127.0.0.1/?token=" + tok, "http://127.0.0.1/?token=short", "not a link"} {
		if _, err := ParseLink(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	l, _ := ParseLink("https://box/x/?token=" + tok)
	if l.wsURL() != "wss://box/x/ws" {
		t.Error(l.wsURL())
	}
	l, _ = ParseLink("http://127.0.0.1:7683/?token=" + tok)
	if l.wsURL() != "ws://127.0.0.1:7683/ws" {
		t.Error(l.wsURL())
	}
}

// Plain http only where the token can't leave private networks.
func TestLinkPolicy(t *testing.T) {
	old := lookupIP
	defer func() { lookupIP = old }()
	lookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "devbox":
			return []net.IP{net.ParseIP("192.168.1.20")}, nil
		case "mixed":
			return []net.IP{net.ParseIP("10.0.0.2"), net.ParseIP("93.184.216.34")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	tok := "?token=0123456789abcdef"
	ok := []string{"http://127.0.0.1:7683/", "http://localhost:7681/", "http://[::1]:7681/", "http://10.1.2.3/", "http://172.20.0.1/",
		"http://192.168.0.5/", "http://100.101.102.103/", "http://169.254.1.1/", "http://[fd7a:115c:a1e0::1]/", "http://devbox:7681/",
		"https://93.184.216.34/", "https://example.com/"}
	bad := []string{"http://93.184.216.34/", "http://example.com/", "http://mixed/", "http://100.128.0.1/", "http://[2001:db8::1]/"}
	for _, u := range ok {
		l, err := ParseLink(u + tok)
		if err != nil {
			t.Fatal(u, err)
		}
		if err := CheckLinkPolicy(l); err != nil {
			t.Errorf("%s refused: %v", u, err)
		}
	}
	for _, u := range bad {
		l, err := ParseLink(u + tok)
		if err != nil {
			t.Fatal(u, err)
		}
		if err := CheckLinkPolicy(l); err == nil || !strings.Contains(err.Error(), "needs https") {
			t.Errorf("%s allowed: %v", u, err)
		}
	}
}
