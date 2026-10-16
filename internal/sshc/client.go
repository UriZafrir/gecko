package sshc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Main runs `ssh args...` and returns its exit code: the remote command's,
// or 255 for an ssh error, like OpenSSH.
func Main(args []string, s *IO) int {
	o, err := ParseArgs(args)
	if err != nil {
		if errors.Is(err, errVersion) {
			s.errf("%s\n", err)
			return 0
		}
		s.errf("%s\n", err)
		return 255
	}
	r, err := Resolve(o)
	if err != nil {
		s.errf("ssh: %v\n", err)
		return 255
	}
	s.quiet = o.Quiet
	c, err := Dial(r, s)
	if err != nil {
		if errors.Is(err, errHostKey) {
			s.errf("%s\n", errHostKey)
		} else {
			s.errf("%s\n", err)
		}
		return 255
	}
	defer c.Close()
	if o.NoCommand {
		err := c.Wait()
		if err != nil {
			return 255
		}
		return 0
	}
	return Session(c, r, o, s)
}

// Dial connects and logs in.
func Dial(r *Resolved, s *IO) (*ssh.Client, error) {
	hostKey, err := hostKeyCallback(r, s)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(r.HostName, strconv.Itoa(r.Port))
	cfg := &ssh.ClientConfig{
		User:              r.User,
		Auth:              authMethods(r, s),
		HostKeyCallback:   hostKey,
		HostKeyAlgorithms: knownKeyTypes(r.KnownHosts, knownName(r.HostName, r.Port)),
		ClientVersion:     "SSH-2.0-OpenSSH_9.6 gecko",
		Timeout:           time.Duration(r.Timeout) * time.Second,
	}
	conn, err := dialTCP(r.HostName, strconv.Itoa(r.Port), cfg.Timeout)
	if err != nil {
		var dns *net.DNSError
		if errors.As(err, &dns) {
			return nil, fmt.Errorf("ssh: Could not resolve hostname %s: Name or service not known", r.HostName)
		}
		return nil, fmt.Errorf("ssh: connect to host %s port %d: %s", r.HostName, r.Port, plainNetError(err))
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		if errors.Is(err, errHostKey) {
			return nil, errHostKey
		}
		msg := err.Error()
		if i := strings.Index(msg, "unable to authenticate"); i >= 0 {
			methods := "publickey,password"
			if a := strings.Index(msg, "attempted methods ["); a >= 0 {
				if b := strings.Index(msg[a:], "]"); b > 0 {
					m := strings.Fields(msg[a+len("attempted methods [") : a+b])
					var keep []string
					for _, x := range m {
						if x != "none" {
							keep = append(keep, x)
						}
					}
					if len(keep) > 0 {
						methods = strings.Join(keep, ",")
					}
				}
			}
			return nil, fmt.Errorf("%s@%s: Permission denied (%s).", r.User, r.HostName, methods)
		}
		return nil, fmt.Errorf("ssh: %s", strings.TrimPrefix(msg, "ssh: "))
	}
	c := ssh.NewClient(cc, chans, reqs)
	if r.AliveEvery > 0 {
		go keepAlive(c, r)
	}
	return c, nil
}

func plainNetError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "Connection refused"
	case strings.Contains(msg, "i/o timeout"):
		return "Connection timed out"
	case strings.Contains(msg, "no such host"):
		return "Name or service not known"
	case strings.Contains(msg, "network is unreachable"):
		return "Network is unreachable"
	}
	return msg
}

func keepAlive(c *ssh.Client, r *Resolved) {
	missed := 0
	for {
		time.Sleep(time.Duration(r.AliveEvery) * time.Second)
		done := make(chan error, 1)
		go func() {
			_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				return
			}
			missed = 0
		case <-time.After(time.Duration(r.AliveEvery) * time.Second):
			missed++
			if missed >= r.AliveMax {
				c.Close()
				return
			}
		}
	}
}

// authMethods: keys, then keyboard-interactive, then password, as OpenSSH
// tries them. No prompts in batch mode.
func authMethods(r *Resolved, s *IO) []ssh.AuthMethod {
	var out []ssh.AuthMethod
	if signers := loadSigners(r, s); len(signers) > 0 {
		out = append(out, ssh.PublicKeys(signers...))
	}
	if r.Batch || !r.Password {
		return out
	}
	out = append(out, ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		if name != "" {
			s.errf("%s\n", name)
		}
		if instruction != "" {
			s.errf("%s\n", instruction)
		}
		answers := make([]string, len(questions))
		for i, q := range questions {
			if strings.EqualFold(strings.TrimSpace(q), "password:") {
				q = fmt.Sprintf("(%s@%s) Password: ", r.User, r.HostName)
			}
			a, err := s.Ask(q, echos[i])
			if err != nil {
				return nil, err
			}
			answers[i] = a
		}
		return answers, nil
	}))
	out = append(out, ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
		return s.Ask(fmt.Sprintf("%s@%s's password: ", r.User, r.HostName), false)
	}), 3))
	return out
}

// DefaultKeys are the identities tried when none is given.
var DefaultKeys = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

func loadSigners(r *Resolved, s *IO) []ssh.Signer {
	files := r.Identities
	explicit := len(files) > 0
	if !explicit {
		for _, k := range DefaultKeys {
			files = append(files, filepath.Join(Home(), ".ssh", k))
		}
	}
	var out []ssh.Signer
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			if explicit {
				s.errf("Warning: Identity file %s not accessible: %s.\n", f, plainFileError(err))
			}
			continue
		}
		sg, err := ssh.ParsePrivateKey(b)
		var pm *ssh.PassphraseMissingError
		if errors.As(err, &pm) {
			if r.Batch {
				continue
			}
			for tries := 0; tries < 3; tries++ {
				pass, aerr := s.Ask(fmt.Sprintf("Enter passphrase for key '%s': ", f), false)
				if aerr != nil || pass == "" {
					break
				}
				if sg, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(pass)); err == nil {
					break
				}
			}
		}
		if err != nil || sg == nil {
			continue
		}
		out = append(out, sg)
	}
	return out
}

func plainFileError(err error) string {
	if os.IsNotExist(err) {
		return "No such file or directory"
	}
	if os.IsPermission(err) {
		return "Permission denied"
	}
	return err.Error()
}

// Session runs the command (or a login shell) and returns its exit code.
func Session(c *ssh.Client, r *Resolved, o *Options, s *IO) int {
	sess, err := c.NewSession()
	if err != nil {
		s.errf("ssh: %v\n", err)
		return 255
	}
	defer sess.Close()
	inFile, _ := s.In.(*os.File)
	outFile, _ := s.Out.(*os.File)
	stdinTTY := inFile != nil && term.IsTerminal(int(inFile.Fd()))
	wantTTY := false
	restore := func() {}
	switch {
	case o.TTY < 0 || r.RequestTTY == "no":
		wantTTY = false
	case o.TTY >= 2 || r.RequestTTY == "force":
		wantTTY = true
	case o.TTY == 1 || r.RequestTTY == "yes":
		wantTTY = stdinTTY
		if !stdinTTY {
			s.errf("Pseudo-terminal will not be allocated because stdin is not a terminal.\n")
		}
	default:
		wantTTY = len(o.Command) == 0 && stdinTTY
	}
	for _, k := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		if v := os.Getenv(k); v != "" {
			_ = sess.Setenv(k, v)
		}
	}
	if wantTTY {
		termName := os.Getenv("TERM")
		if termName == "" {
			termName = "xterm-256color"
		}
		w, h := 80, 24
		if outFile != nil {
			if cw, ch, err := term.GetSize(int(outFile.Fd())); err == nil {
				w, h = cw, ch
			}
		} else if inFile != nil {
			if cw, ch, err := term.GetSize(int(inFile.Fd())); err == nil {
				w, h = cw, ch
			}
		}
		modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
		if err := sess.RequestPty(termName, h, w, modes); err != nil {
			s.errf("PTY allocation request failed\n")
		} else if stdinTTY {
			if old, err := term.MakeRaw(int(inFile.Fd())); err == nil {
				restore = func() { term.Restore(int(inFile.Fd()), old) }
				defer restore()
			}
			stop := watchResize(func() {
				fd := int(inFile.Fd())
				if outFile != nil {
					fd = int(outFile.Fd())
				}
				if cw, ch, err := term.GetSize(fd); err == nil {
					_ = sess.WindowChange(ch, cw)
				}
			})
			defer stop()
		}
	}
	sess.Stdout, sess.Stderr = s.Out, s.Err
	if !o.NoStdin {
		in, err := sess.StdinPipe()
		if err == nil {
			go func() {
				_, _ = io.Copy(in, s.In)
				in.Close()
			}()
		}
	}
	if len(o.Command) > 0 {
		err = sess.Start(strings.Join(o.Command, " "))
	} else {
		err = sess.Shell()
	}
	if err != nil {
		s.errf("ssh: %v\n", err)
		return 255
	}
	// Signals for the remote side, not this process (the terminal is raw anyway).
	signal.Ignore(os.Interrupt)
	err = sess.Wait()
	code := 0
	var ee *ssh.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitStatus()
		if ee.Signal() != "" {
			code = 255
		}
	default:
		code = 255
	}
	restore() // before the message, so it ends with a proper newline
	if wantTTY && !o.Quiet {
		s.errf("Connection to %s closed.\n", r.HostName)
	}
	return code
}
