package hostsetup

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gecko-term/gecko/internal/config"
)

// A password typed into the Add machine dialog reaches ssh like this: the
// daemon keeps it in memory under a random one-time id, and runs ssh with
// SSH_ASKPASS pointing at gecko-askpass (a link to gecko) and that id in
// GECKO_ASKPASS. When ssh asks for the password, gecko-askpass trades the
// id for it over the daemon's own socket, which only this user can open.
// So it is never on a command line, in an environment (just the id), in a
// file or in a log; and it is forgotten when the check or install ends.

var (
	secretsMu sync.Mutex
	secrets   = map[string]string{} // id -> secret
)

// askpassEnv registers secret (or reuses its id) and returns what ssh needs.
func askpassEnv(secret string) []string {
	secretsMu.Lock()
	id := ""
	for k, v := range secrets {
		if v == secret {
			id = k
		}
	}
	if id == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		id = hex.EncodeToString(b)
		secrets[id] = secret
	}
	secretsMu.Unlock()
	env := []string{"SSH_ASKPASS=" + askpassPath(), "SSH_ASKPASS_REQUIRE=force", "GECKO_ASKPASS=" + id}
	if os.Getenv("DISPLAY") == "" {
		env = append(env, "DISPLAY=gecko") // OpenSSH before 8.4 uses askpass only with a DISPLAY
	}
	return env
}

func forgetSecret(secret string) {
	if secret == "" {
		return
	}
	secretsMu.Lock()
	for k, v := range secrets {
		if v == secret {
			delete(secrets, k)
		}
	}
	secretsMu.Unlock()
}

// AskpassSecret is the daemon's answer to gecko-askpass: the secret for id,
// for a password or passphrase prompt only (never "yes" to a host key).
func AskpassSecret(id, prompt string) (string, bool) {
	p := strings.ToLower(prompt)
	if !strings.Contains(p, "password") && !strings.Contains(p, "passphrase") && !strings.Contains(p, "pin") {
		return "", false
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	s, ok := secrets[id]
	return s, ok
}

// AskpassProgram replaces the gecko-askpass link (tests run gecko itself).
var AskpassProgram string

// askpassPath is a link named gecko-askpass to this gecko, which acts as
// the askpass program under that name.
func askpassPath() string {
	if AskpassProgram != "" {
		return AskpassProgram
	}
	p := filepath.Join(config.StateDir(), "gecko-askpass")
	exe, err := config.Executable()
	if err != nil {
		return p
	}
	if cur, err := os.Readlink(p); err != nil || cur != exe {
		_ = os.Remove(p)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.Symlink(exe, p)
	}
	return p
}
