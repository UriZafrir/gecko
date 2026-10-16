package sshc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

var errGotKey = errors.New("got the host key")

// HostKey connects to dest (as ssh would: ~/.ssh/config, the app's DNS)
// just far enough to read the server's host key, for a person to confirm.
// It returns the key and where known_hosts keeps it.
func HostKey(dest string, port int) (ssh.PublicKey, *Resolved, error) {
	args := []string{}
	if port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	o, err := ParseArgs(append(args, dest))
	if err != nil {
		return nil, nil, err
	}
	r, err := Resolve(o)
	if err != nil {
		return nil, nil, err
	}
	conn, err := dialTCP(r.HostName, strconv.Itoa(r.Port), 10*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to %s port %d: %s", r.HostName, r.Port, plainNetError(err))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	var key ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User:              r.User,
		HostKeyAlgorithms: hostKeyOrder(nil),
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			key = k
			return errGotKey
		},
	}
	_, _, _, err = ssh.NewClientConn(conn, net.JoinHostPort(r.HostName, strconv.Itoa(r.Port)), cfg)
	if key == nil {
		return nil, nil, fmt.Errorf("no host key from %s: %v", r.HostName, err)
	}
	return key, r, nil
}

// Fingerprint is how a host key is shown: "ED25519 SHA256:...".
func Fingerprint(k ssh.PublicKey) string { return keyName(k) + " " + ssh.FingerprintSHA256(k) }

// Trust adds key to the known_hosts r names, as accepting it at ssh's
// question would.
func Trust(r *Resolved, key ssh.PublicKey) error {
	if err := os.MkdirAll(filepath.Dir(r.KnownHosts), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(r.KnownHosts, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(knownName(r.HostName, r.Port) + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + "\n")
	return err
}

// askpass runs $SSH_ASKPASS with the prompt, as OpenSSH does when there is
// no terminal to ask on (or SSH_ASKPASS_REQUIRE=force), and returns its
// answer.
func askpass(prompt string) (string, bool, error) {
	prog := os.Getenv("SSH_ASKPASS")
	if prog == "" {
		return "", false, nil
	}
	req := os.Getenv("SSH_ASKPASS_REQUIRE")
	if req == "never" {
		return "", false, nil
	}
	if req != "force" {
		if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			tty.Close()
			return "", false, nil
		}
	}
	out, err := execAskpass(prog, prompt)
	if err != nil {
		return "", true, err
	}
	return strings.TrimRight(out, "\r\n"), true, nil
}
