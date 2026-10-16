package sshc

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	geckoconfig "github.com/gecko-term/gecko/internal/config"
)

// Android has no /etc/resolv.conf, so Go's own resolver (in a static
// build) has no DNS server to ask. The Gecko app writes the phone's current
// DNS servers and search domains to the file in $GECKO_RESOLV_CONF, in
// resolv.conf format, and names are looked up there.
type resolvConf struct {
	servers []string
	search  []string
}

func readResolvConf(path string) *resolvConf {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	rc := &resolvConf{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "nameserver":
			// An address, maybe IPv6 with a zone (fe80::1%wlan0) or in brackets.
			if ns := strings.Trim(fields[1], "[]"); net.ParseIP(strings.SplitN(ns, "%", 2)[0]) != nil {
				rc.servers = append(rc.servers, ns)
			}
		case "search", "domain":
			rc.search = append(rc.search, fields[1:]...)
		}
	}
	if len(rc.servers) == 0 {
		return nil
	}
	return rc
}

// appResolver is the resolver for the app's DNS file, or nil to use the
// system's (anywhere with a /etc/resolv.conf).
func appResolver() (*net.Resolver, []string) {
	if systemResolver() {
		return nil, nil
	}
	file := os.Getenv("GECKO_RESOLV_CONF")
	if file == "" {
		file = filepath.Join(geckoconfig.Home(), "resolv.conf") // where the app writes it
	}
	rc := readResolvConf(file)
	if rc == nil {
		return nil, nil
	}
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		var last error
		for _, s := range rc.servers {
			c, err := d.DialContext(ctx, network, net.JoinHostPort(s, dnsServerPort))
			if err == nil {
				return c, nil
			}
			last = err
		}
		return nil, last
	}}
	return r, rc.search
}

// dnsServerPort is 53 (a variable for tests).
var dnsServerPort = "53"

// systemResolver: Go's resolver has a configuration to use.
var systemResolver = func() bool {
	_, err := os.Stat("/etc/resolv.conf")
	return err == nil
}

// candidates are the names to look up for host, in order: like the C
// library with ndots:1, a name without a dot is tried in the search
// domains first (Tailscale's MagicDNS names are short), then as it is.
func candidates(host string, search []string) []string {
	h := strings.TrimSuffix(host, ".")
	if strings.HasSuffix(host, ".") || strings.Contains(h, ".") || len(search) == 0 {
		return []string{h}
	}
	var out []string
	for _, s := range search {
		out = append(out, h+"."+strings.Trim(s, "."))
	}
	return append(out, h)
}

// dialTCP connects to host:port, looking the name up as above.
func dialTCP(host, port string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	res, search := appResolver()
	if res == nil || net.ParseIP(host) != nil {
		return d.Dial("tcp", net.JoinHostPort(host, port))
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var last error
	for _, n := range candidates(host, search) {
		addrs, err := res.LookupIPAddr(ctx, n)
		if err != nil {
			last = err
			continue
		}
		for _, a := range addrs {
			c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(a.IP.String(), port))
			if err == nil {
				return c, nil
			}
			last = err
		}
	}
	if last == nil {
		last = errors.New("no such host")
	}
	return nil, last
}
