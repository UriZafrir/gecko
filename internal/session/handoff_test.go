package session

import (
	"testing"

	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/vt"
)

type fakePty struct{ fd uintptr }

func (p *fakePty) Read([]byte) (int, error)    { return 0, nil }
func (p *fakePty) Write(b []byte) (int, error) { return len(b), nil }
func (p *fakePty) Close() error                { return nil }
func (p *fakePty) Resize(int, int) error       { return nil }
func (p *fakePty) Pid() int                    { return 1 }
func (p *fakePty) TTY() string                 { return "" }
func (p *fakePty) Wait() (int, error)          { return 0, nil }
func (p *fakePty) Fd() uintptr                 { return p.fd }

// A tab that was closed but hasn't ended yet doesn't stop an upgrade: its
// closed fd (-1) isn't handed over.
func TestFreezeSkipsClosedPty(t *testing.T) {
	m := &Manager{sessions: map[string]*Session{}}
	for id, fd := range map[string]uintptr{"open": 7, "closed": ^uintptr(0)} {
		m.sessions[id] = &Session{m: m, pty: &fakePty{fd: fd}, ring: NewRing(1024), vt: vt.New(80, 24, 10), info: proto.SessionInfo{ID: id}}
	}
	states, thaw, err := m.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	thaw()
	if len(states) != 1 || states[0].Info.ID != "open" {
		t.Fatalf("handed over %+v, want only the open session", states)
	}
}
