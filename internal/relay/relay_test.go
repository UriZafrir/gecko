package relay_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gecko-term/gecko/internal/cloud"
	"github.com/gecko-term/gecko/internal/relay"
)

func device(t *testing.T, name string) (cloud.Device, cloud.Signer) {
	k, err := cloud.NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	return k.Device(name, 1), k.Signer()
}

func call(t *testing.T, base, method, path string, s *cloud.Signer, body any) int {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, base+path, bytes.NewReader(b))
	if s != nil {
		s.Authorize(req, b)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// The relay keeps the chain honest too, though daemons don't rely on it.
func TestRelayChecks(t *testing.T) {
	rs, err := relay.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(rs.Handler())
	defer srv.Close()

	a, as := device(t, "laptop")
	x, xs := device(t, "stranger")
	g := &cloud.List{Account: "roy", Version: 1, Devices: []cloud.Device{a}}
	g.Sign(as.ID, as.Key)
	type listReq struct {
		List *cloud.List `json:"list"`
	}
	if c := call(t, srv.URL, "POST", "/v1/account", &xs, listReq{g}); c != http.StatusUnauthorized {
		t.Fatalf("account created by someone else's request: %d", c)
	}
	if c := call(t, srv.URL, "POST", "/v1/account", &as, listReq{g}); c != http.StatusOK {
		t.Fatalf("create: %d", c)
	}
	if c := call(t, srv.URL, "POST", "/v1/account", &as, listReq{g}); c != http.StatusConflict {
		t.Fatalf("created twice: %d", c)
	}
	if c := call(t, srv.URL, "GET", "/v1/chain", nil, nil); c != http.StatusUnauthorized {
		t.Fatalf("unsigned chain request: %d", c)
	}
	if c := call(t, srv.URL, "GET", "/v1/chain", &xs, nil); c != http.StatusUnauthorized {
		t.Fatalf("outsider read the chain: %d", c)
	}

	// A list that adds the stranger, signed by the stranger, posted by a member.
	bad := g.Next()
	bad.Devices = append(bad.Devices, x)
	bad.Sign(xs.ID, xs.Key)
	if c := call(t, srv.URL, "POST", "/v1/list", &as, listReq{bad}); c != http.StatusBadRequest {
		t.Fatalf("list signed by an outsider: %d", c)
	}
	// A machine name that would break session ids.
	y, _ := device(t, "evil/name")
	odd := g.Next()
	odd.Devices = append(odd.Devices, y)
	odd.Sign(as.ID, as.Key)
	if c := call(t, srv.URL, "POST", "/v1/list", &as, listReq{odd}); c != http.StatusBadRequest {
		t.Fatalf("bad machine name: %d", c)
	}
	good := g.Next()
	good.Devices = append(good.Devices, x)
	good.Sign(as.ID, as.Key)
	if c := call(t, srv.URL, "POST", "/v1/list", &as, listReq{good}); c != http.StatusOK {
		t.Fatalf("approval: %d", c)
	}
	if c := call(t, srv.URL, "POST", "/v1/list", &as, listReq{good}); c != http.StatusConflict {
		t.Fatalf("same version twice: %d", c)
	}
	// Dialing a machine that isn't connected.
	if c := call(t, srv.URL, "GET", "/v1/dial?to="+x.ID, &as, nil); c != http.StatusNotFound {
		t.Fatalf("dial offline machine: %d", c)
	}
}
