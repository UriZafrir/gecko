package sshc

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// testServer is an in-process SSH server: password "secret" for alice,
// keys from authorized, a few commands, a PTY shell, and an scp sink and
// source.
type testServer struct {
	addr       string
	port       int
	hostKey    ssh.Signer
	authorized []ssh.PublicKey
	files      map[string][]byte // what scp -t received / scp -f sends

	mu      sync.Mutex
	ptyReq  string
	resizes []string
	ln      net.Listener
}

func newServer(t *testing.T, hostKey ssh.Signer) *testServer {
	t.Helper()
	if hostKey == nil {
		_, sk, _ := ed25519.GenerateKey(rand.Reader)
		hostKey, _ = ssh.NewSignerFromKey(sk)
	}
	s := &testServer{hostKey: hostKey, files: map[string][]byte{"remote.txt": []byte("from the server\n")}}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "alice" && string(pass) == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("bad password")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, k := range s.authorized {
				if bytes.Equal(k.Marshal(), key.Marshal()) {
					return nil, nil
				}
			}
			return nil, fmt.Errorf("unknown key")
		},
	}
	cfg.AddHostKey(hostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	s.addr = ln.Addr().String()
	s.port = ln.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c, cfg)
		}
	}()
	return s
}

func (s *testServer) serve(c net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		c.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, creqs, _ := nc.Accept()
		go s.session(ch, creqs)
	}
}

func exit(ch ssh.Channel, code int) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(code))
	ch.SendRequest("exit-status", false, b)
	ch.Close()
}

func (s *testServer) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	for r := range reqs {
		switch r.Type {
		case "pty-req":
			var p struct {
				Term   string
				W, H   uint32
				PW, PH uint32
				Modes  string
			}
			ssh.Unmarshal(r.Payload, &p)
			s.mu.Lock()
			s.ptyReq = fmt.Sprintf("%s %dx%d", p.Term, p.W, p.H)
			s.mu.Unlock()
			r.Reply(true, nil)
		case "window-change":
			var p struct{ W, H, PW, PH uint32 }
			ssh.Unmarshal(r.Payload, &p)
			s.mu.Lock()
			s.resizes = append(s.resizes, fmt.Sprintf("%dx%d", p.W, p.H))
			s.mu.Unlock()
		case "env":
			r.Reply(false, nil)
		case "shell":
			r.Reply(true, nil)
			go func() {
				s.mu.Lock()
				fmt.Fprintf(ch, "shell with %s\r\n$ ", s.ptyReq)
				s.mu.Unlock()
				br := bufio.NewReader(ch)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						exit(ch, 0)
						return
					}
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "exit") {
						n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "exit")))
						exit(ch, n)
						return
					}
					fmt.Fprintf(ch, "you said %s\r\n$ ", line)
				}
			}()
		case "exec":
			var p struct{ Cmd string }
			ssh.Unmarshal(r.Payload, &p)
			r.Reply(true, nil)
			go s.exec(ch, p.Cmd)
		default:
			if r.WantReply {
				r.Reply(false, nil)
			}
		}
	}
}

func (s *testServer) exec(ch ssh.Channel, cmd string) {
	switch {
	case cmd == "echo hello":
		io.WriteString(ch, "hello\n")
		exit(ch, 0)
	case strings.HasPrefix(cmd, "exit "):
		n, _ := strconv.Atoi(cmd[5:])
		exit(ch, n)
	case cmd == "cat":
		io.Copy(ch, ch)
		exit(ch, 0)
	case strings.HasPrefix(cmd, "scp -t "):
		br := bufio.NewReader(ch)
		ch.Write([]byte{0})
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				exit(ch, 0)
				return
			}
			f := strings.SplitN(strings.TrimSpace(line[1:]), " ", 3)
			size, _ := strconv.Atoi(f[1])
			ch.Write([]byte{0})
			data := make([]byte, size)
			io.ReadFull(br, data)
			br.ReadByte()
			s.mu.Lock()
			s.files[f[2]] = data
			s.mu.Unlock()
			ch.Write([]byte{0})
		}
	case strings.HasPrefix(cmd, "scp -f "):
		name := strings.Trim(strings.TrimPrefix(cmd, "scp -f "), "'")
		br := bufio.NewReader(ch)
		br.ReadByte()
		data := s.files[name]
		fmt.Fprintf(ch, "C0644 %d %s\n", len(data), name)
		br.ReadByte()
		ch.Write(data)
		ch.Write([]byte{0})
		br.ReadByte()
		exit(ch, 0)
	default:
		fmt.Fprintf(ch.Stderr(), "sh: %s: not found\n", cmd)
		exit(ch, 127)
	}
}

// run runs ssh with args against s, answering prompts from answers.
func run(t *testing.T, args []string, stdin string, answers ...string) (code int, out, errOut string, asked []string) {
	t.Helper()
	var o, e bytes.Buffer
	io := &IO{In: strings.NewReader(stdin), Out: &o, Err: &e, AskFunc: func(prompt string, echo bool) (string, error) {
		asked = append(asked, prompt)
		if len(answers) == 0 {
			return "", fmt.Errorf("unexpected question %q", prompt)
		}
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}}
	code = Main(args, io)
	return code, o.String(), e.String(), asked
}

func home(t *testing.T) string {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USER", "alice")
	t.Setenv("TERM", "xterm-256color")
	return h
}

func TestPasswordAndTrustOnFirstUse(t *testing.T) {
	h := home(t)
	s := newServer(t, nil)
	code, out, errOut, asked := run(t, []string{"-p", strconv.Itoa(s.port), "alice@127.0.0.1", "echo", "hello"}, "", "yes", "secret")
	if code != 0 || out != "hello\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if len(asked) != 2 || !strings.Contains(asked[0], "Are you sure you want to continue connecting (yes/no/[fingerprint])?") || asked[1] != "alice@127.0.0.1's password: " {
		t.Errorf("questions: %q", asked)
	}
	if !strings.Contains(errOut, "The authenticity of host '[127.0.0.1]:"+strconv.Itoa(s.port)+"' can't be established.") ||
		!strings.Contains(errOut, "ED25519 key fingerprint is "+ssh.FingerprintSHA256(s.hostKey.PublicKey())) ||
		!strings.Contains(errOut, "Warning: Permanently added") {
		t.Errorf("stderr: %s", errOut)
	}
	kh, _ := os.ReadFile(filepath.Join(h, ".ssh", "known_hosts"))
	if !strings.HasPrefix(string(kh), "[127.0.0.1]:"+strconv.Itoa(s.port)+" ssh-ed25519 ") {
		t.Errorf("known_hosts: %q", kh)
	}
	// Known now: no question about the host.
	code, _, _, asked = run(t, []string{"-p", strconv.Itoa(s.port), "alice@127.0.0.1", "exit 3"}, "", "secret")
	if code != 3 || len(asked) != 1 {
		t.Errorf("second login: code %d, asked %q", code, asked)
	}
	// Wrong password, three times.
	code, _, errOut, _ = run(t, []string{"-p", strconv.Itoa(s.port), "alice@127.0.0.1", "echo hello"}, "", "a", "b", "c")
	if code != 255 || !strings.Contains(errOut, "alice@127.0.0.1: Permission denied (") {
		t.Errorf("bad password: %d %q", code, errOut)
	}
	// Unknown command: its exit code.
	code, _, errOut, _ = run(t, []string{"-p", strconv.Itoa(s.port), "alice@127.0.0.1", "nope"}, "", "secret")
	if code != 127 || !strings.Contains(errOut, "not found") {
		t.Errorf("exit code: %d %q", code, errOut)
	}
}

func TestRefusesAChangedHostKey(t *testing.T) {
	h := home(t)
	s := newServer(t, nil)
	if code, _, e, _ := run(t, []string{"-o", "StrictHostKeyChecking=accept-new", "-p", strconv.Itoa(s.port), "alice@127.0.0.1", "echo hello"}, "", "secret"); code != 0 {
		t.Fatalf("first: %d %s", code, e)
	}
	// Same address, another key.
	s.ln.Close()
	_, sk, _ := ed25519.GenerateKey(rand.Reader)
	other, _ := ssh.NewSignerFromKey(sk)
	s2 := newServerAt(t, other, s.addr)
	code, _, errOut, asked := run(t, []string{"-p", strconv.Itoa(s2.port), "alice@127.0.0.1", "echo hello"}, "")
	if code != 255 || len(asked) != 0 {
		t.Fatalf("changed key: code %d asked %q", code, asked)
	}
	for _, want := range []string{"WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!", ssh.FingerprintSHA256(other.PublicKey()), "Offending key in " + filepath.Join(h, ".ssh", "known_hosts") + ":1", "Host key verification failed."} {
		if !strings.Contains(errOut, want) {
			t.Errorf("missing %q in:\n%s", want, errOut)
		}
	}
	// "no" to an unknown host.
	os.Remove(filepath.Join(h, ".ssh", "known_hosts"))
	code, _, errOut, _ = run(t, []string{"-p", strconv.Itoa(s2.port), "alice@127.0.0.1", "echo hello"}, "", "no")
	if code != 255 || !strings.Contains(errOut, "Host key verification failed.") {
		t.Errorf("answer no: %d %q", code, errOut)
	}
	// Batch mode never asks.
	code, _, _, asked = run(t, []string{"-o", "BatchMode=yes", "-p", strconv.Itoa(s2.port), "alice@127.0.0.1", "echo hello"}, "")
	if code != 255 || len(asked) != 0 {
		t.Errorf("batch: %d %q", code, asked)
	}
}

func newServerAt(t *testing.T, key ssh.Signer, addr string) *testServer {
	t.Helper()
	for i := 0; i < 50; i++ {
		s := newServer(t, key)
		if s.addr == addr {
			return s
		}
		s.ln.Close()
		// listen on exactly addr
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		ln.Close()
	}
	s := &testServer{}
	_ = s
	// Fall back to a server bound at addr directly.
	return bindAt(t, key, addr)
}

func bindAt(t *testing.T, key ssh.Signer, addr string) *testServer {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	ln.Close()
	s := newServer(t, key)
	s.ln.Close()
	ln, err = net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	s.ln, s.addr, s.port = ln, addr, ln.Addr().(*net.TCPAddr).Port
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
		if string(pass) == "secret" {
			return nil, nil
		}
		return nil, fmt.Errorf("no")
	}}
	cfg.AddHostKey(key)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c, cfg)
		}
	}()
	return s
}

func TestKeygenAndKeyLogin(t *testing.T) {
	h := home(t)
	var o, e bytes.Buffer
	io := &IO{Out: &o, Err: &e, AskFunc: func(string, bool) (string, error) { return "", fmt.Errorf("no questions") }}
	if code := KeygenMain([]string{"-t", "ed25519", "-N", "", "-C", "phone", "-f", filepath.Join(h, ".ssh", "id_ed25519")}, io); code != 0 {
		t.Fatalf("keygen: %s", e.String())
	}
	if !strings.Contains(e.String(), "Your public key has been saved in "+filepath.Join(h, ".ssh", "id_ed25519.pub")) {
		t.Errorf("keygen output: %s", e.String())
	}
	st, _ := os.Stat(filepath.Join(h, ".ssh", "id_ed25519"))
	if st.Mode().Perm() != 0o600 {
		t.Errorf("private key mode %v", st.Mode())
	}
	pubLine, _ := os.ReadFile(filepath.Join(h, ".ssh", "id_ed25519.pub"))
	pub, comment, _, _, err := ssh.ParseAuthorizedKey(pubLine)
	if err != nil || comment != "phone" || pub.Type() != "ssh-ed25519" {
		t.Fatalf("pub: %q %v", pubLine, err)
	}
	o.Reset()
	if KeygenMain([]string{"-y", "-f", filepath.Join(h, ".ssh", "id_ed25519")}, io); strings.TrimSpace(o.String()) != strings.TrimSpace(string(pubLine)) {
		t.Errorf("-y: %q vs %q", o.String(), pubLine)
	}

	s := newServer(t, nil)
	s.authorized = []ssh.PublicKey{pub}
	// The default key logs in; no password asked.
	code, out, errOut, asked := run(t, []string{"-o", "StrictHostKeyChecking=accept-new", "-p", strconv.Itoa(s.port), "bob@127.0.0.1", "echo hello"}, "")
	if code != 0 || out != "hello\n" || len(asked) != 0 {
		t.Fatalf("key login: %d %q %q %q", code, out, errOut, asked)
	}
	// A passphrase-protected key, given with -i.
	KeygenMain([]string{"-q", "-N", "hunter2", "-f", filepath.Join(h, "k2")}, io)
	pb, _ := os.ReadFile(filepath.Join(h, "k2.pub"))
	pub2, _, _, _, _ := ssh.ParseAuthorizedKey(pb)
	s.authorized = []ssh.PublicKey{pub2}
	code, _, errOut, asked = run(t, []string{"-i", filepath.Join(h, "k2"), "-p", strconv.Itoa(s.port), "bob@127.0.0.1", "echo hello"}, "", "hunter2")
	if code != 0 || len(asked) != 1 || !strings.HasPrefix(asked[0], "Enter passphrase for key") {
		t.Errorf("passphrase key: %d %q %q", code, errOut, asked)
	}
	// ssh-keygen -R forgets the host.
	o.Reset()
	if code := KeygenMain([]string{"-R", "[127.0.0.1]:" + strconv.Itoa(s.port)}, io); code != 0 || !strings.Contains(o.String(), "found") {
		t.Errorf("-R: %d %q %q", code, o.String(), e.String())
	}
}

func TestPTYSessionAndStdin(t *testing.T) {
	home(t)
	s := newServer(t, nil)
	p := strconv.Itoa(s.port)
	// -tt forces a terminal even without one here; the shell sees the type and size.
	code, out, errOut, _ := run(t, []string{"-tt", "-o", "StrictHostKeyChecking=no", "-p", p, "alice@127.0.0.1"}, "hi\nexit 4\n", "secret")
	if code != 4 || !strings.Contains(out, "shell with xterm-256color 80x24") || !strings.Contains(out, "you said hi") {
		t.Fatalf("pty: %d %q %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "Connection to 127.0.0.1 closed.") {
		t.Errorf("no closing line: %q", errOut)
	}
	// A command reads stdin; no terminal.
	code, out, _, _ = run(t, []string{"-o", "StrictHostKeyChecking=no", "-p", p, "alice@127.0.0.1", "cat"}, "piped through\n", "secret")
	if code != 0 || out != "piped through\n" {
		t.Errorf("stdin: %d %q", code, out)
	}
}

func TestScp(t *testing.T) {
	h := home(t)
	s := newServer(t, nil)
	p := strconv.Itoa(s.port)
	local := filepath.Join(h, "up.txt")
	os.WriteFile(local, []byte("to the server\n"), 0o644)
	answers := []string{"secret"}
	ask := func(string, bool) (string, error) { return answers[0], nil }
	var e bytes.Buffer
	if code := ScpMain([]string{"-o", "StrictHostKeyChecking=no", "-P", p, local, "alice@127.0.0.1:"}, &IO{Out: io.Discard, Err: &e, AskFunc: ask}); code != 0 {
		t.Fatalf("upload: %s", e.String())
	}
	if string(s.files["up.txt"]) != "to the server\n" {
		t.Errorf("server got %q", s.files["up.txt"])
	}
	down := filepath.Join(h, "down.txt")
	if code := ScpMain([]string{"-P", p, "alice@127.0.0.1:remote.txt", down}, &IO{Out: io.Discard, Err: &e, AskFunc: ask}); code != 0 {
		t.Fatalf("download: %s", e.String())
	}
	if b, _ := os.ReadFile(down); string(b) != "from the server\n" {
		t.Errorf("downloaded %q", b)
	}
}

func TestParseArgs(t *testing.T) {
	o, err := ParseArgs([]string{"-tt", "-p2222", "-i", "k", "-l", "bob", "-oStrictHostKeyChecking=no", "-o", "User carol", "host", "ls", "-la"})
	if err != nil {
		t.Fatal(err)
	}
	if o.TTY != 2 || o.Port != 2222 || o.User != "bob" || o.Host != "host" || strings.Join(o.Command, " ") != "ls -la" || o.Identities[0] != "k" || o.Set["stricthostkeychecking"] != "no" || o.Set["user"] != "carol" {
		t.Errorf("%+v", o)
	}
	o, _ = ParseArgs([]string{"ssh://dave@[::1]:2200"})
	if o.User != "dave" || o.Host != "::1" || o.Port != 2200 {
		t.Errorf("url: %+v", o)
	}
	if _, err := ParseArgs([]string{"-L", "80:x:80", "h"}); err == nil {
		t.Error("-L accepted")
	}
	if _, err := ParseArgs([]string{"-p"}); err == nil {
		t.Error("missing argument accepted")
	}
}

func TestConfigFile(t *testing.T) {
	h := home(t)
	os.MkdirAll(filepath.Join(h, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(h, ".ssh", "config"), []byte(`
# a comment
Host devbox
    HostName 10.0.0.5
    User erin
    Port 2022
    IdentityFile ~/.ssh/devbox_key

Host *.lan !nas.lan
    User lanuser

Match host foo
    User nobody

Host *
    User fallback
    ServerAliveInterval 15
`), 0o600)
	o, _ := ParseArgs([]string{"devbox"})
	r, err := Resolve(o)
	if err != nil {
		t.Fatal(err)
	}
	if r.HostName != "10.0.0.5" || r.User != "erin" || r.Port != 2022 || r.Identities[0] != filepath.Join(h, ".ssh", "devbox_key") || r.AliveEvery != 15 {
		t.Errorf("devbox: %+v", r)
	}
	o, _ = ParseArgs([]string{"-p", "23", "frank@devbox"})
	r, _ = Resolve(o)
	if r.User != "frank" || r.Port != 23 {
		t.Errorf("command line first: %+v", r)
	}
	for host, user := range map[string]string{"pc.lan": "lanuser", "nas.lan": "fallback", "foo": "fallback"} {
		o, _ = ParseArgs([]string{host})
		r, _ = Resolve(o)
		if r.User != user {
			t.Errorf("%s: user %q, want %q", host, r.User, user)
		}
	}
}

func TestResolvConf(t *testing.T) {
	f := filepath.Join(t.TempDir(), "resolv.conf")
	os.WriteFile(f, []byte("# from the app\nnameserver 192.168.1.1\nnameserver 100.100.100.100\nsearch tail1234.ts.net lan\n"), 0o600)
	rc := readResolvConf(f)
	if rc == nil || strings.Join(rc.servers, ",") != "192.168.1.1,100.100.100.100" || strings.Join(rc.search, ",") != "tail1234.ts.net,lan" {
		t.Errorf("%+v", rc)
	}
	if readResolvConf(filepath.Join(t.TempDir(), "none")) != nil {
		t.Error("missing file")
	}
}

// The default port: known_hosts names the host without a port ("ubunty"),
// but the check needs host:port. A Tailscale short name on port 22 failed
// with "knownhosts: SplitHostPort(ubunty): missing port in address".
func TestHostKeyOnPort22(t *testing.T) {
	h := home(t)
	_, sk, _ := ed25519.GenerateKey(rand.Reader)
	key, _ := ssh.NewSignerFromKey(sk)
	remote := &net.TCPAddr{IP: net.ParseIP("100.117.62.82"), Port: 22}
	for _, c := range []struct {
		host string
		port int
		line string
	}{
		{"ubunty", 22, "ubunty "},
		{"ubunty.tail3d2bf5.ts.net", 22, "ubunty.tail3d2bf5.ts.net "},
		{"100.117.62.82", 22, "100.117.62.82 "},
		{"fd7a:115c:a1e0::5", 22, "fd7a:115c:a1e0::5 "},
		{"ubunty", 2222, "[ubunty]:2222 "},
	} {
		os.Remove(filepath.Join(h, ".ssh", "known_hosts"))
		var e bytes.Buffer
		r := &Resolved{HostName: c.host, Port: c.port, Strict: "accept-new", KnownHosts: filepath.Join(h, ".ssh", "known_hosts")}
		cb, err := hostKeyCallback(r, &IO{Err: &e})
		if err != nil {
			t.Fatal(err)
		}
		if err := cb(net.JoinHostPort(c.host, strconv.Itoa(c.port)), remote, key.PublicKey()); err != nil {
			t.Errorf("%s:%d first contact: %v", c.host, c.port, err)
			continue
		}
		kh, _ := os.ReadFile(r.KnownHosts)
		if !strings.HasPrefix(string(kh), c.line) {
			t.Errorf("%s:%d known_hosts %q", c.host, c.port, kh)
		}
		// Known now, and refused with another key.
		if err := cb("", remote, key.PublicKey()); err != nil {
			t.Errorf("%s:%d known: %v", c.host, c.port, err)
		}
		_, sk2, _ := ed25519.GenerateKey(rand.Reader)
		other, _ := ssh.NewSignerFromKey(sk2)
		if err := cb("", remote, other.PublicKey()); err != errHostKey {
			t.Errorf("%s:%d changed key: %v", c.host, c.port, err)
		}
	}
}
