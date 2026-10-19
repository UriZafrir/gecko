package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"sync"

	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
)

// Making another machine the anchor (protocol 11, see hub/anchor.go and
// docs/anchor.md): a hello with "anchor" asks this Gecko (the door) to
// forward the client to a machine it reaches. Only owner clients may ask:
// a relay peer, or a client that asked for the local scope, can't.

// maxAnchorPath bounds how many Geckos a client may go through.
const maxAnchorPath = 4

// elevate is the one way a relay peer gets owner rights: it says it
// forwards an owner's page that makes this machine its anchor, and this
// machine's owner hasn't turned off "Can be the anchor through my
// account" (on by default: every relay peer is one of the owner's own
// approved machines). A peer that doesn't say so stays a peer.
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
	// The window's settings stay the client's: the door the page connects
	// to (the outermost one, which no other Gecko forwards) keeps them,
	// and only what belongs to the anchor's tabs comes from the anchor.
	var st *splitSettings
	if m.Door == "" {
		st = &splitSettings{door: c.hub}
	}
	go func() {
		for {
			bin, b, err := link.Conn.Read()
			if err != nil {
				c.close()
				return
			}
			if st != nil && !bin {
				b = st.fromAnchor(b)
			}
			c.enqueue(bin, b)
		}
	}()
	for {
		bin, b, err := c.conn.Read()
		if err != nil {
			return true
		}
		if st != nil && !bin {
			if b = st.toAnchor(b); b == nil {
				continue
			}
		}
		if link.Conn.Write(bin, b) != nil {
			return true
		}
	}
}

// machineSettings are the settings that belong to a machine's tabs rather
// than to the window (the tab order): after Make anchor they are the
// anchor's. Everything else (theme, font size, keys, ...) stays the
// client's.
var machineSettings = []string{"order"}

// splitSettings keeps a switched page's interface settings on the door
// and the tab order on the anchor.
type splitSettings struct {
	door   *hub.Hub
	mu     sync.Mutex
	anchor map[string]any // the anchor's settings, as it last sent them
}

func settingsObj(text string) map[string]any {
	obj := map[string]any{}
	_ = json.Unmarshal([]byte(text), &obj)
	if obj == nil {
		obj = map[string]any{}
	}
	return obj
}

// fromAnchor turns the anchor's settings push into the door's settings
// with the anchor's tab order.
func (s *splitSettings) fromAnchor(b []byte) []byte {
	if !bytes.Contains(b, []byte(`"t":"settings"`)) {
		return b
	}
	var m proto.Msg
	if json.Unmarshal(b, &m) != nil || m.T != proto.TSettings {
		return b
	}
	a := settingsObj(m.Text)
	s.mu.Lock()
	s.anchor = a
	s.mu.Unlock()
	mine := settingsObj(s.door.Settings())
	for _, k := range machineSettings {
		if v, ok := a[k]; ok {
			mine[k] = v
		} else {
			delete(mine, k)
		}
	}
	text, _ := json.Marshal(mine)
	m.Text = string(text)
	out, err := json.Marshal(&m)
	if err != nil {
		return b
	}
	return out
}

// toAnchor saves a switched page's settings on the door, and passes only
// the tab order on to the anchor.
func (s *splitSettings) toAnchor(b []byte) []byte {
	if !bytes.Contains(b, []byte(`"t":"setsettings"`)) {
		return b
	}
	var m proto.Msg
	if json.Unmarshal(b, &m) != nil || m.T != proto.TSetSettings {
		return b
	}
	in := settingsObj(m.Text)
	mine := settingsObj(s.door.Settings())
	for k, v := range in {
		mine[k] = v
	}
	s.mu.Lock()
	known := s.anchor != nil
	a := map[string]any{}
	for k, v := range s.anchor {
		a[k] = v
	}
	s.mu.Unlock()
	for _, k := range machineSettings {
		if v, ok := in[k]; ok {
			a[k] = v
		} else {
			delete(a, k)
		}
	}
	// The door keeps its own tab order.
	door := settingsObj(s.door.Settings())
	for _, k := range machineSettings {
		if v, ok := door[k]; ok {
			mine[k] = v
		} else {
			delete(mine, k)
		}
	}
	if text, err := json.Marshal(mine); err == nil {
		if err := s.door.SetSettings(string(text)); err != nil {
			log.Printf("anchor: keeping a switched window's settings: %v", err)
		}
	}
	if !known {
		// The anchor hasn't said what it has: don't overwrite it.
		return nil
	}
	text, _ := json.Marshal(a)
	m.Text = string(text)
	out, err := json.Marshal(&m)
	if err != nil {
		return b
	}
	return out
}
