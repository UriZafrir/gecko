package cloud

import "github.com/gorilla/websocket"

// DialRelay opens a signed WebSocket to the relay as s; tests use it to
// act as a device that bypasses the agent.
func DialRelay(relay string, s Signer, path string) (*websocket.Conn, error) {
	rc, err := newRelayClient(relay, s)
	if err != nil {
		return nil, err
	}
	return rc.ws(path)
}
