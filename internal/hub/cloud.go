package hub

import (
	"errors"
	"strconv"

	"github.com/gecko-term/gecko/internal/proto"
)

// Cloud is this machine's account (internal/cloud): it lists the other
// machines of the account and dials them through the relay. Nil until the
// daemon sets it; a machine that isn't signed in reports SignedIn false and
// never touches the network.
type Cloud interface {
	State() *proto.CloudState
	Do(action, arg string) error
	MachineID() string
}

// CloudMachine is another machine of the account, reached through the relay.
type CloudMachine struct {
	ID, Name, OS string
	Online       bool
	LastSeen     int64 // unix ms
	Dial         DialFunc
}

// SetCloud attaches the account side.
func (h *Hub) SetCloud(c Cloud) {
	h.mu.Lock()
	h.cloud = c
	h.mu.Unlock()
}

func (h *Hub) getCloud() Cloud {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cloud
}

// MachineID is this machine's id in its account ("" when signed out).
func (h *Hub) MachineID() string {
	if c := h.getCloud(); c != nil {
		return c.MachineID()
	}
	return ""
}

// CloudState is the account as the interface shows it (nil: not signed in).
func (h *Hub) CloudState() *proto.CloudState {
	c := h.getCloud()
	if c == nil {
		return nil
	}
	if st := c.State(); st.SignedIn || st.Notice != "" {
		return st
	}
	return nil
}

// CloudDo runs an account action (approve, deny, revoke, ...).
func (h *Hub) CloudDo(action, arg string) (*proto.CloudState, error) {
	c := h.getCloud()
	if c == nil {
		return nil, errors.New("this Gecko has no account support")
	}
	err := c.Do(action, arg)
	return c.State(), err
}

// PublishCloud sends the account state to every window.
func (h *Hub) PublishCloud() {
	c := h.getCloud()
	if c == nil {
		return
	}
	h.publishAll(&proto.Msg{T: proto.TCloud, Cloud: c.State()})
}

// SetCloudMachines replaces the list of the account's other machines.
func (h *Hub) SetCloudMachines(ms []CloudMachine) {
	h.mu.Lock()
	h.cloudMachines = ms
	h.mu.Unlock()
	h.reconcileCloud()
}

// machineLearned records the account id an SSH machine reported, so it is
// shown once (over SSH, with the relay as fallback) even while it's down.
func (h *Hub) machineLearned(name, id string) {
	for i := range h.cfg.Hosts {
		if h.cfg.Hosts[i].Name == name && h.cfg.Hosts[i].Machine != id {
			h.cfg.Hosts[i].Machine = id
			_ = h.cfg.Save()
		}
	}
	h.reconcileCloud()
}

// reconcileCloud makes the backends match the account: one Remote per
// machine. A machine also set up over SSH stays that SSH Remote, with the
// relay as its fallback; the rest get a cloud Remote of their own.
func (h *Hub) reconcileCloud() {
	var (
		closed []Backend
		gone   []string
		starts []*Remote
	)
	h.mu.Lock()
	want := map[string]CloudMachine{}
	for _, m := range h.cloudMachines {
		want[m.ID] = m
	}
	direct := map[string]*Remote{}
	for _, b := range h.backends {
		if r, ok := b.(*Remote); ok && (r.kind == KindSSH || r.kind == KindLink) {
			if id := r.MachineID(); id != "" {
				direct[id] = r
			}
		}
	}
	existing := map[string]*Remote{}
	for name, b := range h.backends {
		r, ok := b.(*Remote)
		if !ok || r.kind != KindCloud {
			continue
		}
		id := r.MachineID()
		if _, ok := want[id]; !ok || direct[id] != nil {
			delete(h.backends, name)
			h.dropOrder(name)
			closed, gone = append(closed, r), append(gone, name)
			continue
		}
		existing[id] = r
	}
	for id, r := range direct {
		if m, ok := want[id]; ok {
			r.setFallback(m.Dial)
		} else {
			r.setFallback(nil)
		}
	}
	for id, m := range want {
		if direct[id] != nil {
			continue
		}
		if r := existing[id]; r != nil {
			r.setPresence(m.Online, m.LastSeen, m.OS, m.Dial)
			continue
		}
		name := m.Name
		for n := 2; name == h.name || h.backends[name] != nil; n++ {
			name = m.Name + "-" + strconv.Itoa(n)
		}
		r := NewRemote(name, "", KindCloud, m.Dial, h)
		r.machine, r.online, r.lastSeen, r.os = id, m.Online, m.LastSeen, m.OS
		h.backends[name] = r
		h.order = append(h.order, name)
		starts = append(starts, r)
	}
	h.mu.Unlock()
	for _, b := range closed {
		b.Close()
	}
	for _, name := range gone {
		h.hostReset(name, nil)
	}
	for _, r := range starts {
		go r.Run()
	}
	h.stateChanged()
}

// dropOrder removes name from the machine order; callers hold h.mu.
func (h *Hub) dropOrder(name string) {
	for i, n := range h.order {
		if n == name {
			h.order = append(h.order[:i], h.order[i+1:]...)
			return
		}
	}
}
