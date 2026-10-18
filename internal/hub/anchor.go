package hub

import (
	"errors"
	"fmt"
	"log"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/proto"
)

type configAnchor = config.AnchorConfig

// Making another machine the anchor (protocol 11, docs/anchor.md).
//
// A client that connected to this Gecko (the door) can make any machine
// in its sidebar the anchor. The door then opens a new connection to that
// machine over the same path it already uses for it (an SSH bridge, its
// Gecko link, or the relay), says hello there for the client, and from
// then on only forwards bytes both ways. The machine serves the client as
// it serves its own window, so the client sees that machine's tabs and
// machines, and adds machines with that machine's keys.
//
// Owner rights come from the connection, as always: an SSH bridge reaches
// the machine's local socket (owner), a link holds its token (owner). A
// relay connection is a peer and stays one, unless that machine was told
// to accept anchor requests from the account (SetAnchorFromAccount).

// errAnchorOld is the reason a machine can't become the anchor because its
// Gecko predates protocol 11.
func errAnchorOld(name string) error {
	return fmt.Errorf("Update Gecko on %s to make it the anchor", name)
}

// OpenAnchor opens an owner connection to the machine called name, for a
// client of this hub that makes it the anchor, and runs the hello
// handshake there for it. path is the rest of the machines to go through
// after name (empty: name is the anchor). The caller forwards the
// client's traffic over the returned link and closes it when done.
func (h *Hub) OpenAnchor(name string, client string, path []string) (*Link, *proto.Msg, error) {
	if name == "" || name == h.name {
		return nil, nil, errors.New(h.name + " is already this Gecko")
	}
	r, ok := h.backend(name).(*Remote)
	if !ok {
		return nil, nil, fmt.Errorf("there is no machine called %s on %s", name, h.name)
	}
	if v := r.Version(); v > 0 && v < proto.AnchorVersion {
		return nil, nil, errAnchorOld(name)
	}
	r.mu.Lock()
	dial, fallback := r.dial, r.fallback
	// Over the relay right now (its direct path failed): start there.
	if r.conn != nil && r.via == ViaRelay && fallback != nil {
		dial, fallback = fallback, dial
	}
	r.mu.Unlock()
	if client == "" {
		client = "web"
	}
	hello := &proto.Msg{T: proto.THello, Client: client, Version: proto.Version, Scope: ScopeAll, Door: h.name, Anchor: path}
	try := func(d DialFunc) (*Link, *proto.Msg, error) {
		link, err := d()
		if err != nil {
			return nil, nil, err
		}
		h.trackHelper(link.PID, true)
		reply, err := handshake(link, hello)
		if err != nil {
			h.trackHelper(link.PID, false)
			return nil, nil, err
		}
		return link, reply, nil
	}
	link, reply, err := try(dial)
	if err != nil && fallback != nil {
		var ferr error
		if link, reply, ferr = try(fallback); ferr != nil {
			return nil, nil, fmt.Errorf("can't reach %s: %v (relay: %v)", name, err, ferr)
		}
		err = nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("can't reach %s: %v", name, err)
	}
	fail := func(err error) (*Link, *proto.Msg, error) {
		h.ReleaseAnchor(link)
		return nil, nil, err
	}
	switch {
	case reply.Kind == KindAnchor && reply.Error != "":
		// A machine further along the path refused or couldn't be reached.
		return fail(errors.New(reply.Error))
	case reply.Version < proto.AnchorVersion:
		return fail(errAnchorOld(name))
	case reply.Scope == ScopeLocal:
		return fail(fmt.Errorf("%s is reached through your account, which only lets %s open %s's own tabs. To make %s the anchor through the account, turn on \"Can be the anchor through my account\" on %s once (its Settings, or gecko anchor account on), or add %s by SSH or its Gecko link", name, h.name, name, name, name, name))
	}
	log.Printf("anchor: a client of %s made %s its anchor", h.name, name)
	return link, reply, nil
}

// ReleaseAnchor closes a link OpenAnchor returned.
func (h *Hub) ReleaseAnchor(l *Link) {
	h.trackHelper(l.PID, false)
	l.close()
}

// KindAnchor marks a hello reply whose anchor switch failed: Error says
// why, and the Gecko that answered serves the client itself.
const KindAnchor = "anchor"

// trackHelper keeps the helper processes (ssh) of anchor connections, so
// an upgraded daemon reaps them too (see Children).
func (h *Hub) trackHelper(pid int, on bool) {
	if pid == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.helpers == nil {
		h.helpers = map[int]bool{}
	}
	if on {
		h.helpers[pid] = true
	} else {
		delete(h.helpers, pid)
	}
}

// AnchorFromAccount reports whether the account's other machines may make
// this one the anchor through the relay.
func (h *Hub) AnchorFromAccount() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.Anchor != nil && h.cfg.Anchor.FromAccount
}

// SetAnchorFromAccount turns that on or off and saves it.
func (h *Hub) SetAnchorFromAccount(on bool) error {
	h.mu.Lock()
	if on {
		if h.cfg.Anchor == nil {
			h.cfg.Anchor = new(configAnchor)
		}
		h.cfg.Anchor.FromAccount = true
	} else if h.cfg.Anchor != nil {
		h.cfg.Anchor.FromAccount = false
		if *h.cfg.Anchor == (configAnchor{}) {
			h.cfg.Anchor = nil
		}
	}
	h.mu.Unlock()
	log.Printf("anchor: through the account %s", map[bool]string{true: "on", false: "off"}[on])
	return h.cfg.Save()
}
