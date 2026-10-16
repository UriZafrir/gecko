package cloud

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Requests to the relay carry no bearer token. Each one is signed with the
// device's Ed25519 key over its method, path, time and body, and the relay
// accepts it for two minutes. Nothing long-lived can leak from a log or a
// proxy, and removing a device from the list cuts it off at once.

// AuthWindow is how long a signed request stays valid.
const AuthWindow = 2 * time.Minute

// Signer is a key that signs relay requests: a machine's, or the recovery
// code's.
type Signer struct {
	ID  string
	Key ed25519.PrivateKey
}

func authMessage(method, uri string, ts int64, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte("gecko-relay-v1\n" + method + "\n" + uri + "\n" + strconv.FormatInt(ts, 10) + "\n" + hex.EncodeToString(sum[:]))
}

// Authorize signs a request.
func (s Signer) Authorize(r *http.Request, body []byte) {
	r.Header.Set("Authorization", s.header(r.Method, r.URL.RequestURI(), body))
}

func (s Signer) header(method, uri string, body []byte) string {
	ts := time.Now().Unix()
	sig := ed25519.Sign(s.Key, authMessage(method, uri, ts, body))
	return "Gecko id=" + s.ID + ",ts=" + strconv.FormatInt(ts, 10) + ",sig=" + base64.RawURLEncoding.EncodeToString(sig)
}

// AuthID returns the device id a request claims to come from.
func AuthID(r *http.Request) string {
	f := authFields(r)
	return f["id"]
}

func authFields(r *http.Request) map[string]string {
	a, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Gecko ")
	if !ok {
		return nil
	}
	f := map[string]string{}
	for _, kv := range strings.Split(a, ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(kv), "="); ok {
			f[k] = v
		}
	}
	return f
}

// CheckAuth verifies a request's signature against key.
func CheckAuth(r *http.Request, body []byte, key ed25519.PublicKey) error {
	f := authFields(r)
	ts, err := strconv.ParseInt(f["ts"], 10, 64)
	if err != nil {
		return errors.New("unsigned request")
	}
	if d := time.Since(time.Unix(ts, 0)); d > AuthWindow || d < -AuthWindow {
		return errors.New("request signed too long ago (check this machine's clock)")
	}
	sig, err := base64.RawURLEncoding.DecodeString(f["sig"])
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, authMessage(r.Method, r.URL.RequestURI(), ts, body), sig) {
		return errors.New("bad request signature")
	}
	return nil
}
