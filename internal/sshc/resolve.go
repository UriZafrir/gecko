package sshc

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"time"
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
			rc.servers = append(rc.servers, fields[1])
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
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		return nil, nil
	}
	rc := readResolvConf(os.Getenv("GECKO_RESOLV_CONF"))
	if rc == nil {
		return nil, nil
	}
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		var last error
		for _, s := range rc.servers {
			c, err := d.DialContext(ctx, network, net.JoinHostPort(s, "53"))
			if err == nil {
				return c, nil
			}
			last = err
		}
		return nil, last
	}}
	return r, rc.search
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
	names := []string{host}
	if !strings.Contains(strings.TrimSuffix(host, "."), ".") {
		for _, s := range search {
			names = append(names, host+"."+s) // short names: the search domains (Tailscale's too)
		}
	}
	var last error
	for _, n := range names {
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
