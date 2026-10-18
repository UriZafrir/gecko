package server

import (
	"net"
	"strings"
)

// An interface address, for choosing the addresses other devices can reach.
type ifaceAddr struct {
	Name string // interface name: wlan0, tailscale0, rmnet_data1, utun3
	IP   net.IP
}

// cellularIface reports whether an interface name is a mobile data link
// (Android and Linux modems). Carriers hand these addresses from
// 100.64.0.0/10 (carrier-grade NAT), the same range as Tailscale, and no
// other device of yours can reach them.
func cellularIface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"rmnet", "v4-rmnet", "ccmni", "wwan", "pdp", "clat", "seth_", "rev_rmnet", "usb_rmnet", "qmimux", "mhi_hwip"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// tunnelIface reports whether an interface name is a VPN tunnel:
// Tailscale (tailscale0 on Linux, utun on macOS, "Tailscale" on Windows,
// tun0 on Android), WireGuard, other tun devices.
func tunnelIface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"tailscale", "tun", "utun", "wg"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func cgnat(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64
}

// shareHosts picks the IPv4 addresses to put in links for other devices:
// tunnels first (Tailscale reaches your devices anywhere), then the LAN.
// Never a mobile data address, never 100.64.0.0/10 outside a tunnel (that's
// a carrier's NAT, not Tailscale), and nothing a device on a private
// network or tailnet couldn't reach.
func shareHosts(addrs []ifaceAddr) []string {
	var tun, lan []string
	seen := map[string]bool{}
	for _, a := range addrs {
		v4 := a.IP.To4()
		if v4 == nil || v4.IsLoopback() || v4.IsLinkLocalUnicast() || cellularIface(a.Name) || seen[v4.String()] {
			continue
		}
		switch {
		case cgnat(v4) && tunnelIface(a.Name):
			tun = append(tun, v4.String())
		case cgnat(v4):
			continue
		case v4.IsPrivate() && tunnelIface(a.Name):
			tun = append(tun, v4.String())
		case v4.IsPrivate():
			lan = append(lan, v4.String())
		default:
			continue
		}
		seen[v4.String()] = true
	}
	return append(tun, lan...)
}

// interfaceAddrs lists this machine's interface addresses with their
// interface names.
func interfaceAddrs() []ifaceAddr {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []ifaceAddr
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok {
				out = append(out, ifaceAddr{Name: ifi.Name, IP: n.IP})
			}
		}
	}
	return out
}
