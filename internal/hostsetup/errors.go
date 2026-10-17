package hostsetup

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/sshc"
)

// Kinds of failures the Add machine dialog handles itself.
const (
	NeedsPassword  = "needs-password"   // the server takes a password (or keyboard-interactive) and there's no key
	HostKeyUnknown = "host-key-unknown" // first contact: confirm the fingerprint
	HostKeyChanged = "host-key-changed"
	NoGeckoBuild   = "no-gecko-build" // nothing to install for that system from here
)

// Error is a failed check with what kind of failure it was.
type Error struct {
	Kind        string
	Msg         string
	Fingerprint string // HostKeyUnknown: "ED25519 SHA256:..."
}

func (e *Error) Error() string { return e.Msg }

var reDenied = regexp.MustCompile(`Permission denied \(([^)]*)\)`)

// Kind sorts ssh's output into what to do next.
func Kind(out string) string {
	switch {
	case strings.Contains(out, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
		return HostKeyChanged
	case strings.Contains(out, "host key is known for") && strings.Contains(out, "requested strict checking"),
		strings.Contains(out, "No ") && strings.Contains(out, "host key is known"):
		return HostKeyUnknown
	}
	if m := reDenied.FindStringSubmatch(out); m != nil {
		for _, meth := range strings.Split(m[1], ",") {
			if meth = strings.TrimSpace(meth); meth == "password" || meth == "keyboard-interactive" {
				return NeedsPassword
			}
		}
	}
	return ""
}

func classify(target, out, last string) error {
	kind := Kind(out)
	switch kind {
	case NeedsPassword:
		return &Error{Kind: kind, Msg: "This machine wants a password."}
	case HostKeyUnknown:
		dest, port := config.SplitTarget(target)
		p, _ := strconv.Atoi(port)
		key, _, err := sshc.HostKey(dest, p)
		if err != nil {
			return &Error{Msg: Explain(last, "") + " (" + err.Error() + ")"}
		}
		return &Error{Kind: kind, Fingerprint: sshc.Fingerprint(key), Msg: "Gecko hasn't connected to this machine before."}
	case HostKeyChanged:
		return &Error{Kind: kind, Msg: Explain("REMOTE HOST IDENTIFICATION HAS CHANGED", "")}
	}
	return &Error{Msg: Explain(last, "")}
}

// TrustHost adds target's host key to known_hosts, if it is still the one
// the user confirmed (fingerprint as shown: "ED25519 SHA256:...").
func TrustHost(target, fingerprint string) error {
	dest, port := config.SplitTarget(target)
	p, _ := strconv.Atoi(port)
	key, r, err := sshc.HostKey(dest, p)
	if err != nil {
		return err
	}
	if sshc.Fingerprint(key) != fingerprint {
		return &Error{Kind: HostKeyChanged, Msg: "The machine's key isn't the one you confirmed; it changed in the meantime. Check again."}
	}
	return sshc.Trust(r, key)
}

// PublicKey is this device's SSH public key line, for authorized_keys. On
// Android the app's key is made if there is none yet; elsewhere only an
// existing ~/.ssh key is offered (Gecko doesn't make one for you).
func PublicKey(create bool) (string, error) {
	dir := filepath.Join(sshc.Home(), ".ssh")
	for _, k := range sshc.DefaultKeys {
		if b, err := os.ReadFile(filepath.Join(dir, k+".pub")); err == nil {
			if line := strings.TrimSpace(string(b)); line != "" {
				return line, nil
			}
		}
	}
	if !create || !config.IsAndroid() {
		return "", os.ErrNotExist
	}
	host := config.DefaultName()
	if _, err := sshc.WriteKey(filepath.Join(dir, "id_ed25519"), "gecko@"+host, ""); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "id_ed25519.pub"))
	return strings.TrimSpace(string(b)), err
}

// CanUseKey says whether a key can be offered for the machine (see PublicKey).
func CanUseKey() bool {
	if config.IsAndroid() {
		return true
	}
	_, err := PublicKey(false)
	return err == nil
}
