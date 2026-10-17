package cloud

import (
	"errors"
	"sync"
	"time"

	"github.com/flynn/noise"
	"github.com/gorilla/websocket"
)

// A machine reached through the relay talks Gecko's usual protocol inside
// a Noise IK session (Noise_IK_25519_ChaChaPoly_BLAKE2s). The connecting
// machine is the initiator and already knows the target's static key from
// the signed device list; the target learns the initiator's key in the
// first message and refuses it unless its own copy of the list trusts it.
// The relay forwards the resulting ciphertext and can't read or alter it.

var suite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)

var prologue = []byte("gecko-cloud-v1")

const (
	maxChunk = 65000    // plaintext per Noise message (limit 65535 with the tag)
	maxMsg   = 16 << 20 // a reassembled protocol message, as proto.StreamConn
	flagBin  = 1        // the message is a binary (data) frame
	flagMore = 2        // more chunks of this message follow
)

// SecureConn is a proto.Conn over a Noise session carried by a relay
// WebSocket. Each protocol message becomes one or more Noise messages of
// flags | payload.
type SecureConn struct {
	ws         *websocket.Conn
	send, recv *noise.CipherState
	peer       []byte // the other side's static key

	wmu  sync.Mutex
	once sync.Once
	done chan struct{}
}

// Peer is the static public key of the other side.
func (c *SecureConn) Peer() []byte { return c.peer }

func newSecureConn(ws *websocket.Conn, send, recv *noise.CipherState, peer []byte) *SecureConn {
	c := &SecureConn{ws: ws, send: send, recv: recv, peer: peer, done: make(chan struct{})}
	ws.SetReadLimit(1 << 17)
	// Keep NATs and proxies from dropping a quiet link.
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					return
				}
			case <-c.done:
				return
			}
		}
	}()
	return c
}

func (c *SecureConn) Read() (bool, []byte, error) {
	var buf []byte
	for {
		_, ct, err := c.ws.ReadMessage()
		if err != nil {
			return false, nil, err
		}
		pt, err := c.recv.Decrypt(nil, nil, ct)
		if err != nil || len(pt) == 0 {
			c.Close()
			return false, nil, errors.New("relay link: message failed authentication")
		}
		if len(buf)+len(pt) > maxMsg {
			c.Close()
			return false, nil, errors.New("relay link: message too large")
		}
		buf = append(buf, pt[1:]...)
		if pt[0]&flagMore == 0 {
			return pt[0]&flagBin != 0, buf, nil
		}
	}
}

func (c *SecureConn) Write(bin bool, data []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
	for first := true; first || len(data) > 0; first = false {
		n := min(len(data), maxChunk)
		var flags byte
		if bin {
			flags |= flagBin
		}
		if n < len(data) {
			flags |= flagMore
		}
		pt := append([]byte{flags}, data[:n]...)
		data = data[n:]
		ct, err := c.send.Encrypt(nil, nil, pt)
		if err != nil {
			return err
		}
		if err := c.ws.WriteMessage(websocket.BinaryMessage, ct); err != nil {
			return err
		}
	}
	return nil
}

func (c *SecureConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.ws.Close()
}

// Initiate runs the initiator side of the handshake over ws: we are local
// and expect the machine whose static key is peer.
func Initiate(ws *websocket.Conn, local *Keys, peer []byte) (*SecureConn, error) {
	hs, err := noise.NewHandshakeState(noise.Config{CipherSuite: suite, Pattern: noise.HandshakeIK,
		Initiator: true, Prologue: prologue, StaticKeypair: local.dh(), PeerStatic: peer})
	if err != nil {
		return nil, err
	}
	_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	msg, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, err
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		return nil, err
	}
	_, reply, err := ws.ReadMessage()
	if err != nil {
		return nil, errors.New("the machine closed the encrypted link (does it still trust this one?)")
	}
	_, send, recv, err := hs.ReadMessage(nil, reply)
	if err != nil || send == nil {
		return nil, errors.New("handshake failed: the other side doesn't hold the key this account lists for it")
	}
	_ = ws.SetReadDeadline(time.Time{})
	return newSecureConn(ws, send, recv, peer), nil
}

// Accept runs the responder side: trusted decides whether the initiator's
// static key may connect, before we answer at all.
func Accept(ws *websocket.Conn, local *Keys, trusted func(peer []byte) bool) (*SecureConn, error) {
	hs, err := noise.NewHandshakeState(noise.Config{CipherSuite: suite, Pattern: noise.HandshakeIK,
		Initiator: false, Prologue: prologue, StaticKeypair: local.dh()})
	if err != nil {
		return nil, err
	}
	_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, first, err := ws.ReadMessage()
	if err != nil {
		return nil, err
	}
	if _, _, _, err := hs.ReadMessage(nil, first); err != nil {
		return nil, errors.New("handshake failed: " + err.Error())
	}
	peer := hs.PeerStatic()
	if !trusted(peer) {
		return nil, errors.New("refused a machine this one doesn't trust")
	}
	msg, recv, send, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, err
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		return nil, err
	}
	_ = ws.SetReadDeadline(time.Time{})
	return newSecureConn(ws, send, recv, append([]byte(nil), peer...)), nil
}
