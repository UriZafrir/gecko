package cloud

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/flynn/noise"

	"github.com/gecko-term/gecko/internal/config"
)

// Files in ~/.gecko-terminal/cloud (0700; files 0600):
//
//	key.json      this machine's key pairs (X25519 for Noise, Ed25519 for signing)
//	account.json  relay, account, this device's id and the verified device list
//
// Signed out means account.json doesn't exist; then nothing here touches
// the network.

// Dir is where the cloud files live.
func Dir() string { return filepath.Join(config.Home(), "cloud") }

// Keys are a machine's long-term key pairs. They never leave the machine.
type Keys struct {
	NoisePriv []byte `json:"noisePrivate"`
	NoisePub  []byte `json:"noisePublic"`
	SignSeed  []byte `json:"signSeed"`
}

// Signer returns the signing identity.
func (k *Keys) Signer() Signer {
	key := ed25519.NewKeyFromSeed(k.SignSeed)
	return Signer{ID: DeviceID(key.Public().(ed25519.PublicKey)), Key: key}
}

// ID is this machine's device id.
func (k *Keys) ID() string { return k.Signer().ID }

func (k *Keys) dh() noise.DHKey { return noise.DHKey{Private: k.NoisePriv, Public: k.NoisePub} }

// Device describes this machine for a device list.
func (k *Keys) Device(name string, now int64) Device {
	s := k.Signer()
	return Device{ID: s.ID, Name: name, OS: runtime.GOOS, Kind: KindMachine, Noise: k.NoisePub,
		Sign: s.Key.Public().(ed25519.PublicKey), Added: now}
}

// NewKeys generates fresh key pairs.
func NewKeys() (*Keys, error) {
	kp, err := noise.DH25519.GenerateKeypair(rand.Reader)
	if err != nil {
		return nil, err
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	return &Keys{NoisePriv: kp.Private, NoisePub: kp.Public, SignSeed: seed}, nil
}

// LoadKeys reads this machine's keys from dir, creating them if create is
// set.
func LoadKeys(dir string, create bool) (*Keys, error) {
	p := filepath.Join(dir, "key.json")
	var k Keys
	if err := readJSON(p, &k); err == nil {
		if len(k.NoisePriv) != 32 || len(k.NoisePub) != 32 || len(k.SignSeed) != ed25519.SeedSize {
			return nil, errors.New(p + " is damaged; remove it and sign in again")
		}
		return &k, nil
	} else if !errors.Is(err, os.ErrNotExist) || !create {
		return nil, err
	}
	nk, err := NewKeys()
	if err != nil {
		return nil, err
	}
	return nk, writeJSON(p, nk)
}

// Account is what a signed-in machine remembers about its account.
type Account struct {
	Relay       string `json:"relay"`
	Name        string `json:"account"`
	Device      string `json:"device"`      // this machine's id
	Fingerprint string `json:"fingerprint"` // of the account's first device list
	List        *List  `json:"list"`        // the latest verified device list

	dir string
}

// LoadAccount returns the account this machine is signed in to (state in
// dir), or nil.
func LoadAccount(dir string) (*Account, error) {
	a := Account{dir: dir}
	if err := readJSON(filepath.Join(dir, "account.json"), &a); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if a.List == nil || a.Relay == "" {
		return nil, errors.New("cloud/account.json is incomplete; run gecko logout and sign in again")
	}
	return &a, nil
}

// SignedIn reports whether the machine with state in dir is signed in,
// without reading more than a file's existence.
func SignedIn(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "account.json"))
	return err == nil
}

func (a *Account) save() error { return writeJSON(filepath.Join(a.dir, "account.json"), a) }

// forget removes the account and the keys (a new sign-in makes new ones).
func forget(dir string) error {
	err := os.Remove(filepath.Join(dir, "account.json"))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	_ = os.Remove(filepath.Join(dir, "key.json"))
	return err
}

func readJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return errors.New(p + ": " + err.Error())
	}
	return nil
}

func writeJSON(p string, v any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
