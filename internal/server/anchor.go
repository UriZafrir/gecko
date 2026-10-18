package server

import (
	"errors"
	"log"

	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
)

// Making another machine the anchor (protocol 11, see hub/anchor.go and
// docs/anchor.md): a hello with "anchor" asks this Gecko (the door) to
// forward the client to a machine it reaches. Only owner clients may ask:
// a relay peer, or a client that asked for the local scope, can't.

// maxAnchorPath bounds how many Geckos a client may go through.
const maxAnchorPath = 4

// elevate is the one way a relay peer gets owner rights: this machine's
// owner turned on "Can be the anchor through my account", and the peer
// says it forwards a client that makes this machine its anchor.
func (c *client) elevate(m *proto.Msg) {
	if !c.peer || m.Door == "" || !c.hub.AnchorFromAccount() {
		return
	}
	c.peer, c.scope = false, hub.ScopeAll
	log.Printf("anchor: %s made this machine the anchor through the account", m.Door)
}

// mayForward says why this client can't make another machine its anchor.
func (c *client) mayForward(m *proto.Msg) error {
	switch {
	case c.peer:
		return errors.New("another machine of your account can't change the anchor here")
	case m.Scope == hub.ScopeLocal || c.scope == hub.ScopeLocal:
		return errors.New("a client that asked for this machine's own tabs can't change the anchor")
	case m.Client == "hub":
		return errors.New("another Gecko can't change the anchor")
	case len(m.Anchor) > maxAnchorPath:
		return errors.New("too many machines to go through to reach that anchor")
	}
	return nil
}

// forward takes the client to the anchor it asked for in hello m and
// carries its traffic both ways until either side closes. It returns
// false, with the reason in c.anchorErr, when the client stays here.
func (c *client) forward(m *proto.Msg) bool {
	if err := c.mayForward(m); err != nil {
		c.anchorErr = err.Error()
		return false
	}
	link, reply, err := c.hub.OpenAnchor(m.Anchor[0], m.Client, m.Anchor[1:])
	if err != nil {
		c.anchorErr = err.Error()
		return false
	}
	defer c.hub.ReleaseAnchor(link)
	reply.Door, reply.Rid = c.hub.Name(), m.Rid
	c.send(reply)
	go func() {
		for {
			bin, b, err := link.Conn.Read()
			if err != nil {
				c.close()
				return
			}
			c.enqueue(bin, b)
		}
	}()
	for {
		bin, b, err := c.conn.Read()
		if err != nil {
			return true
		}
		if link.Conn.Write(bin, b) != nil {
			return true
		}
	}
}
