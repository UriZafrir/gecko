package sshc

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// errHostKey ends the connection; the reason has been printed.
var errHostKey = errors.New("Host key verification failed.")

// knownName is how known_hosts names host:port ("host", or "[host]:2222").
func knownName(host string, port int) string {
	// As OpenSSH writes it: plain on port 22 (IPv6 too; Normalize would
	// bracket that, and knownhosts can't read it back), else [host]:port.
	if port == 22 {
		return host
	}
	return "[" + host + "]:" + strconv.Itoa(port)
}

// knownKeyTypes lists the key types known_hosts has for name, so the
// server is asked for one of those (and a host with an ed25519 key isn't
// reported as changed when it offers RSA first).
func knownKeyTypes(file, name string) []string {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		marker, hosts, key, _, _, err := ssh.ParseKnownHosts(sc.Bytes())
		if err != nil || marker != "" {
			continue
		}
		for _, h := range hosts {
			if hostMatches(h, name) {
				switch t := key.Type(); t {
				case ssh.KeyAlgoRSA:
					out = append(out, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
				default:
					out = append(out, t)
				}
				break
			}
		}
	}
	return out
}

// hostMatches compares a known_hosts host entry (plain, pattern or hashed
// |1|salt|hash) with a normalized name.
func hostMatches(entry, name string) bool {
	if strings.HasPrefix(entry, "|1|") {
		parts := strings.Split(entry[3:], "|")
		if len(parts) != 2 {
			return false
		}
		salt, err1 := base64.StdEncoding.DecodeString(parts[0])
		want, err2 := base64.StdEncoding.DecodeString(parts[1])
		if err1 != nil || err2 != nil {
			return false
		}
		m := hmac.New(sha1.New, salt)
		m.Write([]byte(name))
		return hmac.Equal(m.Sum(nil), want)
	}
	// [host]:port is literal; only * and ? are wildcards in known_hosts.
	if !strings.ContainsAny(entry, "*?") {
		return strings.EqualFold(entry, name)
	}
	esc := strings.NewReplacer("[", `\[`, "]", `\]`).Replace(entry)
	return matchHost([]string{esc}, name)
}

// keyName is how OpenSSH names a key type in messages: ED25519, RSA, ECDSA.
func keyName(k ssh.PublicKey) string {
	t := k.Type()
	switch {
	case strings.Contains(t, "ed25519"):
		return "ED25519"
	case strings.Contains(t, "ecdsa"):
		return "ECDSA"
	case strings.Contains(t, "rsa"):
		return "RSA"
	}
	return strings.ToUpper(t)
}

// hostKeyCallback checks the server's key against known_hosts, asking (or
// not, per StrictHostKeyChecking) about a host seen for the first time,
// and refusing one whose key changed.
func hostKeyCallback(r *Resolved, io *IO) (ssh.HostKeyCallback, error) {
	if err := os.MkdirAll(filepath.Dir(r.KnownHosts), 0o700); err != nil {
		return nil, err
	}
	if f, err := os.OpenFile(r.KnownHosts, os.O_CREATE|os.O_RDONLY, 0o600); err == nil {
		f.Close()
	}
	if _, err := knownhosts.New(r.KnownHosts); err != nil {
		return nil, fmt.Errorf("%s: %v", r.KnownHosts, err)
	}
	// known_hosts writes port 22 as just the host ("ubunty"), but the check
	// takes host:port, always.
	name := knownName(r.HostName, r.Port)
	address := net.JoinHostPort(r.HostName, strconv.Itoa(r.Port))
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		check, err := knownhosts.New(r.KnownHosts) // as it is now
		if err != nil {
			return fmt.Errorf("%s: %v", r.KnownHosts, err)
		}
		err = check(address, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err // revoked
		}
		fp := ssh.FingerprintSHA256(key)
		if len(ke.Want) > 0 {
			io.errf(changedWarning, keyName(key), fp, r.KnownHosts, ke.Want[0].Filename, ke.Want[0].Line, name)
			return errHostKey
		}
		switch r.Strict {
		case "yes":
			io.errf("No %s host key is known for %s and you have requested strict checking.\n", keyName(key), name)
			return errHostKey
		case "no", "accept-new":
		default:
			if r.Batch {
				return errHostKey
			}
			host := name
			if ip, _, e := net.SplitHostPort(remote.String()); e == nil && ip != r.HostName {
				host = fmt.Sprintf("%s (%s)", name, ip)
			}
			io.errf("The authenticity of host '%s' can't be established.\n%s key fingerprint is %s.\nThis key is not known by any other names.\n", host, keyName(key), fp)
			prompt := "Are you sure you want to continue connecting (yes/no/[fingerprint])? "
			for {
				ans, err := io.Ask(prompt, true)
				if err != nil {
					return errHostKey
				}
				ans = strings.TrimSpace(ans)
				if strings.EqualFold(ans, "yes") || ans == fp {
					break
				}
				if strings.EqualFold(ans, "no") {
					return errHostKey
				}
				prompt = "Please type 'yes', 'no' or the fingerprint: "
			}
		}
		// Not knownhosts.Line: it brackets a plain IPv6 address, which it then can't read.
		line := name + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
		f, err := os.OpenFile(r.KnownHosts, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(line + "\n"); err != nil {
			return err
		}
		if r.Strict != "accept-new" || !io.quiet {
			io.errf("Warning: Permanently added '%s' (%s) to the list of known hosts.\n", name, keyName(key))
		}
		return nil
	}, nil
}

const changedWarning = `@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
Someone could be eavesdropping on you right now (man-in-the-middle attack)!
It is also possible that a host key has just been changed.
The fingerprint for the %s key sent by the remote host is
%s.
Please contact your system administrator.
Add correct host key in %s to get rid of this message.
Offending key in %s:%d
Host key for %s has changed and you have requested strict checking.
`
