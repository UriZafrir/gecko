package hostsetup_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/hostsetup"
	"github.com/gecko-term/gecko/internal/hub"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/server"
	"github.com/gecko-term/gecko/internal/session"
	"github.com/gecko-term/gecko/internal/sshc"
)

// shellServer is an SSH server that runs commands with sh, as a real one
// does, for one user: password "secret", or a key in its authorized_keys.
type shellServer struct {
	port int
	home string
}

func newShellServer(t *testing.T) *shellServer {
	s := &shellServer{home: t.TempDir()}
	_, sk, _ := ed25519.GenerateKey(rand.Reader)
	hk, _ := ssh.NewSignerFromKey(sk)
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == "secret" {
				return nil, nil
			}
			return nil, errors.New("no")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			b, _ := os.ReadFile(filepath.Join(s.home, ".ssh", "authorized_keys"))
			for len(b) > 0 {
				k, _, _, rest, err := ssh.ParseAuthorizedKey(b)
				if err != nil {
					break
				}
				if bytes.Equal(k.Marshal(), key.Marshal()) {
					return nil, nil
				}
				b = rest
			}
			return nil, errors.New("unknown key")
		},
	}
	cfg.AddHostKey(hk)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s.port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, creqs, _ := nc.Accept()
					go func() {
						for r := range creqs {
							if r.Type != "exec" {
								r.Reply(false, nil)
								continue
							}
							var p struct{ Cmd string }
							ssh.Unmarshal(r.Payload, &p)
							r.Reply(true, nil)
							cmd := exec.Command("/bin/sh", "-c", p.Cmd)
							cmd.Env = []string{"HOME=" + s.home, "PATH=/usr/bin:/bin"}
							cmd.Stdin, cmd.Stdout, cmd.Stderr = ch, ch, ch.Stderr()
							code := 0
							if err := cmd.Run(); err != nil {
								code = 1
								var ee *exec.ExitError
								if errors.As(err, &ee) {
									code = ee.ExitCode()
								}
							}
							b := make([]byte, 4)
							binary.BigEndian.PutUint32(b, uint32(code))
							ch.SendRequest("exit-status", false, b)
							ch.Close()
						}
					}()
				}
			}()
		}
	}()
	return s
}

// The Add machine dialog's way in, end to end with gecko's own ssh: a new
// host's key is confirmed, the password is asked for and passed through the
// askpass helper over the daemon's socket, and the device's key is
// installed so the next login needs none.
func TestPasswordThenKey(t *testing.T) {
	if testing.Short() {
		t.Skip("builds gecko")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	gecko := filepath.Join(bin, "gecko")
	if out, err := exec.Command("go", "build", "-o", gecko, "github.com/gecko-term/gecko/cmd/gecko").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, n := range []string{"ssh", "scp", "gecko-askpass"} {
		os.Symlink(gecko, filepath.Join(bin, n))
	}
	hostsetup.AskpassProgram = filepath.Join(bin, "gecko-askpass")
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("GECKO_HOME", filepath.Join(dir, "gecko"))
	t.Setenv("USER", "alice")
	// The daemon's socket, for gecko-askpass.
	os.MkdirAll(config.StateDir(), 0o700)
	ln, err := net.Listen("unix", config.Socket())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	h := hub.New(&config.Config{Name: "test"}, session.NewManager(session.Options{Host: "test", Shell: "/bin/sh", ScriptsDir: t.TempDir()}))
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go server.ServeLocal(h, proto.NewStreamConn(c, c, c))
		}
	}()
	pub, err := sshc.WriteKey(filepath.Join(home, ".ssh", "id_ed25519"), "phone", "")
	if err != nil {
		t.Fatal(err)
	}

	s := newShellServer(t)
	target := "alice@127.0.0.1:" + strconv.Itoa(s.port)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var log []string
	logf := func(l string) { log = append(log, l) }
	kind := func(err error) string {
		var e *hostsetup.Error
		if errors.As(err, &e) {
			return e.Kind
		}
		return fmt.Sprintf("not a hostsetup.Error: %v", err)
	}

	// 1. A new host: its fingerprint, to confirm.
	_, err = hostsetup.ProbeWith(ctx, target, hostsetup.Options{Strict: true}, logf)
	var he *hostsetup.Error
	if !errors.As(err, &he) || he.Kind != hostsetup.HostKeyUnknown || !strings.HasPrefix(he.Fingerprint, "ED25519 SHA256:") {
		t.Fatalf("first contact: %v %+v\n%s", err, he, strings.Join(log, "\n"))
	}
	if err := hostsetup.TrustHost(target, "ED25519 SHA256:not-it"); err == nil {
		t.Error("trusted a fingerprint that isn't the host's")
	}
	if err := hostsetup.TrustHost(target, he.Fingerprint); err != nil {
		t.Fatal(err)
	}
	// 2. No key there yet: it wants a password.
	if _, err = hostsetup.ProbeWith(ctx, target, hostsetup.Options{Strict: true}, logf); kind(err) != hostsetup.NeedsPassword {
		t.Fatalf("no key: %v (%s)\n%s", err, kind(err), strings.Join(log, "\n"))
	}
	// 3. A wrong password, then the right one, installing the key.
	if _, err = hostsetup.ProbeWith(ctx, target, hostsetup.Options{Strict: true, Secret: "wrong"}, logf); err == nil {
		t.Fatal("wrong password let in")
	}
	log = nil
	res, err := hostsetup.ProbeWith(ctx, target, hostsetup.Options{Strict: true, Secret: "secret", InstallKey: true}, logf)
	if err != nil || !res.Missing || res.OS == "" {
		t.Fatalf("password: %v %+v\n%s", err, res, strings.Join(log, "\n"))
	}
	ak, _ := os.ReadFile(filepath.Join(s.home, ".ssh", "authorized_keys"))
	if !strings.Contains(string(ak), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))) {
		t.Errorf("authorized_keys: %q", ak)
	}
	if st, _ := os.Stat(filepath.Join(s.home, ".ssh", "authorized_keys")); st.Mode().Perm()&0o077 != 0 {
		t.Errorf("authorized_keys mode %v", st.Mode())
	}
	for _, l := range log {
		if strings.Contains(l, "secret") {
			t.Errorf("password in the log: %q", l)
		}
	}
	// 4. From now on the key: no password, and the key isn't added twice.
	if _, err = hostsetup.ProbeWith(ctx, target, hostsetup.Options{Strict: true, InstallKey: true}, logf); err != nil {
		t.Fatalf("key login: %v\n%s", err, strings.Join(log, "\n"))
	}
	ak2, _ := os.ReadFile(filepath.Join(s.home, ".ssh", "authorized_keys"))
	if !bytes.Equal(ak, ak2) {
		t.Errorf("key added again: %q", ak2)
	}
	// 5. The secret is gone once the check ended.
	if _, ok := hostsetup.AskpassSecret("anything", "Password: "); ok {
		t.Error("a secret is still kept")
	}
}
