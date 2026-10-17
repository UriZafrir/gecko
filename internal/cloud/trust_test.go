package cloud

import (
	"crypto/ed25519"
	"testing"
)

func testDevice(t *testing.T, name string) (Device, Signer) {
	t.Helper()
	k, err := NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	return k.Device(name, 1), k.Signer()
}

func TestListChain(t *testing.T) {
	a, as := testDevice(t, "laptop")
	b, bs := testDevice(t, "gpu-box")
	c, cs := testDevice(t, "stranger")

	g := &List{Account: "roy", Version: 1, Devices: []Device{a}}
	g.Sign(as.ID, as.Key)
	if err := VerifyGenesis(g); err != nil {
		t.Fatal(err)
	}

	// laptop approves gpu-box.
	v2 := g.Next()
	v2.Devices = append(v2.Devices, b)
	v2.Sign(as.ID, as.Key)
	if err := Verify(g, v2); err != nil {
		t.Fatalf("approval: %v", err)
	}

	// A device that isn't in the list can't add itself, even with a valid
	// signature of its own (what a compromised relay would try).
	bad := v2.Next()
	bad.Devices = append(bad.Devices, c)
	bad.Sign(cs.ID, cs.Key)
	if Verify(v2, bad) == nil {
		t.Fatal("a list signed by an outsider verified")
	}
	// Tampering after signing breaks the signature.
	v3 := v2.Next()
	v3.Sign(bs.ID, bs.Key)
	v3.Devices = append(v3.Devices, c)
	if Verify(v2, v3) == nil {
		t.Fatal("a list changed after signing verified")
	}
	// Versions can't be skipped or replayed.
	skip := v2.Next()
	skip.Version++
	skip.Sign(as.ID, as.Key)
	if Verify(v2, skip) == nil {
		t.Fatal("a skipped version verified")
	}
	if Verify(v2, g) == nil {
		t.Fatal("an old version verified")
	}
	if _, err := VerifyChain([]List{*g, *v2}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryAndCodes(t *testing.T) {
	code := NewRecoveryCode()
	if len(NormalizeCode(code)) != 20 {
		t.Fatalf("recovery code %q", code)
	}
	k1, k2 := RecoveryKey(code), RecoveryKey(" "+NormalizeCode(code)+" ")
	if !k1.Equal(k2) {
		t.Fatal("recovery key depends on formatting")
	}
	d := RecoveryDevice(code, 1)
	if d.ID != DeviceID(k1.Public().(ed25519.PublicKey)) {
		t.Fatal("recovery device id")
	}
	a, _ := testDevice(t, "laptop")
	pc := PairingCode(&a)
	if len(pc) != 9 || pc[4] != '-' {
		t.Fatalf("pairing code %q", pc)
	}
	b := a
	b.Noise = append([]byte(nil), a.Noise...)
	b.Noise[0] ^= 1
	if PairingCode(&b) == pc {
		t.Fatal("pairing code doesn't depend on the encryption key")
	}
}

func TestCheckURL(t *testing.T) {
	for in, ok := range map[string]bool{
		"https://relay.example": true, "relay.example": true, "wss://relay.example/x": true,
		"http://127.0.0.1:7900": true, "ws://localhost:7900": true, "http://[::1]:7900": true,
		"http://relay.example": false, "ws://10.0.0.2:7900": false, "ftp://relay.example": false, "": false,
	} {
		if _, err := CheckURL(in); (err == nil) != ok {
			t.Errorf("CheckURL(%q): %v", in, err)
		}
	}
}
