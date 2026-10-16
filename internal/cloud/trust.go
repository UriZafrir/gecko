// Package cloud signs a machine in to a Gecko account on a relay (see
// docs/cloud.md): its keys, the account's signed list of trusted devices,
// the end-to-end encrypted (Noise IK) link to another machine through the
// relay, and the daemon side that keeps one outbound connection to it.
package cloud

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Device kinds.
const (
	KindMachine  = "machine"  // a computer running the Gecko daemon
	KindRecovery = "recovery" // the account's recovery code: can sign lists, can't connect
)

// Device is one entry of an account's trusted device list.
type Device struct {
	ID    string `json:"id"` // fingerprint of Sign (see DeviceID)
	Name  string `json:"name"`
	OS    string `json:"os,omitempty"`
	Kind  string `json:"kind"`
	Noise []byte `json:"noise,omitempty"` // X25519 static public key (machines)
	Sign  []byte `json:"sign"`            // Ed25519 public key
	Added int64  `json:"added"`           // unix ms
	By    string `json:"by,omitempty"`    // id of the device that approved it
}

// List is one version of an account's trusted devices. Every version after
// the first is signed by a device of the version before it, so a daemon
// that holds version n can check n+1 without trusting whoever delivered it
// (the relay).
type List struct {
	Account string   `json:"account"`
	Version int      `json:"version"`
	Devices []Device `json:"devices"`
	Signer  string   `json:"signer"`
	Sig     []byte   `json:"sig,omitempty"`
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// DeviceID names a device by its signing key: 16 base32 characters.
func DeviceID(sign ed25519.PublicKey) string {
	sum := sha256.Sum256(sign)
	return strings.ToLower(b32.EncodeToString(sum[:10]))
}

// signedBytes is what a list's signature covers: the list as JSON without
// its signature, behind a domain separator.
func (l *List) signedBytes() []byte {
	c := *l
	c.Sig = nil
	b, _ := json.Marshal(&c)
	return append([]byte("gecko-device-list-v1\n"), b...)
}

// Sign signs l as the device id with key.
func (l *List) Sign(id string, key ed25519.PrivateKey) {
	l.Signer = id
	l.Sig = ed25519.Sign(key, l.signedBytes())
}

// Find returns the device with id (nil if not listed).
func (l *List) Find(id string) *Device {
	if l == nil {
		return nil
	}
	for i := range l.Devices {
		if l.Devices[i].ID == id {
			return &l.Devices[i]
		}
	}
	return nil
}

// Machine returns the machine whose Noise key is noise (nil if none).
func (l *List) Machine(noise []byte) *Device {
	if l == nil {
		return nil
	}
	for i := range l.Devices {
		d := &l.Devices[i]
		if d.Kind == KindMachine && len(d.Noise) == 32 && string(d.Noise) == string(noise) {
			return d
		}
	}
	return nil
}

// Next copies l as the unsigned next version.
func (l *List) Next() *List {
	n := &List{Account: l.Account, Version: l.Version + 1}
	n.Devices = append([]Device(nil), l.Devices...)
	return n
}

func (l *List) checkSig(signers *List) error {
	s := signers.Find(l.Signer)
	if s == nil {
		return fmt.Errorf("device list v%d is signed by %s, who isn't trusted", l.Version, l.Signer)
	}
	if len(s.Sign) != ed25519.PublicKeySize || !ed25519.Verify(s.Sign, l.signedBytes(), l.Sig) {
		return fmt.Errorf("device list v%d has a bad signature", l.Version)
	}
	return nil
}

func (l *List) wellFormed() error {
	seen := map[string]bool{}
	for _, d := range l.Devices {
		if d.ID == "" || d.ID != DeviceID(d.Sign) {
			return fmt.Errorf("device list v%d: device %q has an id that doesn't match its key", l.Version, d.Name)
		}
		if seen[d.ID] {
			return fmt.Errorf("device list v%d lists %s twice", l.Version, d.ID)
		}
		seen[d.ID] = true
		if d.Kind == KindMachine && len(d.Noise) != 32 {
			return fmt.Errorf("device list v%d: machine %q has no encryption key", l.Version, d.Name)
		}
	}
	return nil
}

// VerifyGenesis checks the first version of an account's list: signed by
// a device in it.
func VerifyGenesis(l *List) error {
	if l.Version != 1 {
		return errors.New("the first device list must be version 1")
	}
	if err := l.wellFormed(); err != nil {
		return err
	}
	return l.checkSig(l)
}

// Verify checks that next may follow prev: same account, the next version,
// and signed by a device trusted in prev.
func Verify(prev, next *List) error {
	if next.Account != prev.Account {
		return errors.New("device list is for another account")
	}
	if next.Version != prev.Version+1 {
		return fmt.Errorf("device list v%d can't follow v%d", next.Version, prev.Version)
	}
	if err := next.wellFormed(); err != nil {
		return err
	}
	return next.checkSig(prev)
}

// VerifyChain checks a chain of lists starting at the first version and
// returns the last one.
func VerifyChain(chain []List) (*List, error) {
	if len(chain) == 0 {
		return nil, errors.New("empty device list")
	}
	if err := VerifyGenesis(&chain[0]); err != nil {
		return nil, err
	}
	cur := &chain[0]
	for i := 1; i < len(chain); i++ {
		if err := Verify(cur, &chain[i]); err != nil {
			return nil, err
		}
		cur = &chain[i]
	}
	return cur, nil
}

// Fingerprint names an account by its first device list, as
// XXXX-XXXX-XXXX. Two machines showing the same fingerprint are in the
// same account; a relay can't make two different accounts look alike.
func Fingerprint(genesis *List) string {
	b := append(genesis.signedBytes(), genesis.Sig...)
	sum := sha256.Sum256(b)
	return group(b32.EncodeToString(sum[:8])[:12])
}

// PairingCode is the code a new machine shows while it waits for approval:
// 40 bits of the hash of its keys, as XXXX-XXXX. The approving machine
// computes it from the keys the relay hands it and compares, so a relay
// can't swap in its own keys without the codes differing.
func PairingCode(d *Device) string {
	h := sha256.New()
	h.Write([]byte("gecko-pairing-v1\n"))
	h.Write(d.Noise)
	h.Write(d.Sign)
	return group(b32.EncodeToString(h.Sum(nil)[:5]))
}

// NormalizeCode upper-cases a typed code and drops dashes and spaces.
func NormalizeCode(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ' || r == '\t':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}
		return r
	}, s)
}

func group(s string) string {
	var parts []string
	for len(s) > 4 {
		parts = append(parts, s[:4])
		s = s[4:]
	}
	return strings.Join(append(parts, s), "-")
}

// NewRecoveryCode returns a random 100-bit recovery code (20 characters).
func NewRecoveryCode() string {
	b := make([]byte, 13)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return group(b32.EncodeToString(b)[:20])
}

// RecoveryKey derives the signing key a recovery code stands for. Only its
// public half is ever stored (as the account's "recovery" device).
func RecoveryKey(code string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("gecko-recovery-v1\n" + NormalizeCode(code)))
	return ed25519.NewKeyFromSeed(seed[:])
}

// RecoveryDevice is the list entry for a recovery code.
func RecoveryDevice(code string, now int64) Device {
	pub := RecoveryKey(code).Public().(ed25519.PublicKey)
	return Device{ID: DeviceID(pub), Name: "recovery code", Kind: KindRecovery, Sign: pub, Added: now}
}
