package hub_test

import (
	"net"
	"testing"
	"time"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/server"
	"github.com/gecko-term/gecko/internal/session"
)

func newHub(t *testing.T, name string, hosts ...config.Host) *hub.Hub {
	m := session.NewManager(session.Options{Host: name, Shell: "/bin/sh", ScriptsDir: t.TempDir()})
	h := hub.New(&config.Config{Name: name, Hosts: hosts}, m)
	t.Cleanup(h.Close)
	return h
}

// pipeTo is a dial function that reaches h in process, as the relay would.
func pipeTo(h *hub.Hub) hub.DialFunc {
	return func() (*hub.Link, error) {
		a, b := net.Pipe()
		go server.Serve(h, proto.NewStreamConn(a, a, a))
		return &hub.Link{Conn: proto.NewStreamConn(b, b, b)}, nil
	}
}

func hosts(h *hub.Hub) map[string]proto.HostInfo {
	out := map[string]proto.HostInfo{}
	for _, i := range h.State(hub.ScopeAll).Hosts {
		out[i.Name] = i
	}
	return out
}

func waitHost(t *testing.T, h *hub.Hub, name string, ok func(proto.HostInfo) bool) proto.HostInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		i, found := hosts(h)[name]
		if found && ok(i) {
			return i
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %+v", name, hosts(h))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A machine of the account gets its own Remote; one that is also an SSH
// host shows once, as that host, and uses the relay only when SSH fails.
func TestCloudMachines(t *testing.T) {
	t.Setenv("GECKO_HOME", t.TempDir())
	gpu := newHub(t, "gpu-box")
	// "devbox" is an SSH host whose ssh never works here, and that we
	// already know is machine "m-devbox" in the account.
	laptop := newHub(t, "laptop", config.Host{Name: "devbox", Command: []string{"sh", "-c", "exit 3"}, Machine: "m-devbox"})
	dev := newHub(t, "devbox")

	laptop.SetCloudMachines([]hub.CloudMachine{
		{ID: "m-gpu", Name: "gpu-box", OS: "linux", Online: true, Dial: pipeTo(gpu)},
		{ID: "m-devbox", Name: "devbox", OS: "linux", Online: true, Dial: pipeTo(dev)},
	})
	i := waitHost(t, laptop, "gpu-box", func(i proto.HostInfo) bool { return i.Status == "connected" })
	if i.Kind != hub.KindCloud || i.Via != hub.ViaRelay || i.Machine != "m-gpu" {
		t.Fatalf("gpu-box: %+v", i)
	}
	d := waitHost(t, laptop, "devbox", func(i proto.HostInfo) bool { return i.Status == "connected" })
	if d.Kind != hub.KindSSH || d.Via != hub.ViaRelay {
		t.Fatalf("devbox should be the SSH host, over the relay: %+v", d)
	}
	if n := len(hosts(laptop)); n != 3 {
		t.Fatalf("want laptop, devbox, gpu-box once each: %+v", hosts(laptop))
	}

	// Revoked: the cloud Remote goes away, and devbox loses its fallback.
	laptop.SetCloudMachines(nil)
	waitHost(t, laptop, "devbox", func(i proto.HostInfo) bool { return i.Status != "connected" })
	if _, ok := hosts(laptop)["gpu-box"]; ok {
		t.Fatal("gpu-box is still listed")
	}

	// Offline cloud machines say so, with when they were last seen.
	laptop.SetCloudMachines([]hub.CloudMachine{{ID: "m-gpu", Name: "gpu-box", LastSeen: 1234, Dial: func() (*hub.Link, error) {
		return nil, net.ErrClosed
	}}})
	o := waitHost(t, laptop, "gpu-box", func(i proto.HostInfo) bool { return i.Status == "offline" })
	if o.LastSeen != 1234 {
		t.Fatalf("offline gpu-box: %+v", o)
	}
}
