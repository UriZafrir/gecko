package server

import (
	"net"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func ia(name, ip string) ifaceAddr { return ifaceAddr{Name: name, IP: net.ParseIP(ip)} }

// The owner's phone on mobile data with Tailscale: the carrier's NAT
// address (100.64.0.0/10 on rmnet) looked like Tailscale and came first.
func TestShareHosts(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []ifaceAddr
		want []string
	}{
		{"phone on mobile data, Tailscale, Wi-Fi", []ifaceAddr{
			ia("rmnet_data1", "100.119.45.155"), ia("rmnet_data1", "2607:fb90::1"),
			ia("wlan0", "192.168.1.244"), ia("tun0", "100.76.235.47"), ia("lo", "127.0.0.1"),
		}, []string{"100.76.235.47", "192.168.1.244"}},
		{"phone on mobile data only", []ifaceAddr{ia("rmnet_data0", "100.119.45.155"), ia("v4-rmnet_data0", "192.0.0.4")}, nil},
		{"MediaTek modem", []ifaceAddr{ia("ccmni1", "10.45.3.2"), ia("wlan0", "10.0.0.7")}, []string{"10.0.0.7"}},
		{"Linux desktop", []ifaceAddr{ia("enp3s0", "192.168.0.10"), ia("docker0", "172.17.0.1"), ia("tailscale0", "100.101.102.103"), ia("enp3s0", "203.0.113.9")},
			[]string{"100.101.102.103", "192.168.0.10", "172.17.0.1"}},
		{"macOS", []ifaceAddr{ia("en0", "192.168.1.5"), ia("utun4", "100.70.1.2"), ia("en0", "169.254.3.3")}, []string{"100.70.1.2", "192.168.1.5"}},
		{"Windows", []ifaceAddr{ia("Ethernet", "10.1.1.4"), ia("Tailscale", "100.88.0.5")}, []string{"100.88.0.5", "10.1.1.4"}},
		{"CGNAT on a plain interface is not Tailscale", []ifaceAddr{ia("eth0", "100.64.12.1"), ia("eth0", "192.168.5.2")}, []string{"192.168.5.2"}},
		{"WireGuard with a private address", []ifaceAddr{ia("wg0", "10.8.0.2"), ia("wlan0", "192.168.1.9")}, []string{"10.8.0.2", "192.168.1.9"}},
	} {
		if got := shareHosts(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestShareURLsListenAddress(t *testing.T) {
	if u := ShareURLs("127.0.0.1:7681", "tok"); u != nil {
		t.Fatal(u)
	}
	if u := ShareURLs("192.168.1.4:7682", "tok"); !reflect.DeepEqual(u, []string{"http://192.168.1.4:7682/?token=tok"}) {
		t.Fatal(u)
	}
}

// The kernel's address table agrees with the interface table where both
// can be read.
func TestLabeledAddrs(t *testing.T) {
	want := map[string]string{}
	for _, a := range interfaceAddrs() {
		if a.IP.To4() != nil {
			want[a.IP.String()] = a.Name
		}
	}
	got := labeledAddrs()
	if runtime.GOOS != "linux" {
		return
	}
	for _, a := range got {
		if n, ok := want[a.IP.String()]; ok && n != a.Name && !strings.HasPrefix(a.Name, n+":") {
			t.Errorf("%s: label %q, interface %q", a.IP, a.Name, n)
		}
	}
	if len(want) > 0 && len(got) == 0 {
		t.Errorf("no addresses from RTM_GETADDR, want %v", want)
	}
}
