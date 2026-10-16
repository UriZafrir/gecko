package sshc

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const keygenUsage = `usage: ssh-keygen [-q] [-t ed25519] [-C comment] [-f output_keyfile] [-N new_passphrase]
       ssh-keygen -y [-f input_keyfile]
       ssh-keygen -l [-f input_keyfile]
       ssh-keygen -R hostname [-f known_hosts_file]`

// KeygenMain runs `ssh-keygen args...`: makes ed25519 keys (the only kind
// this one makes), prints a public key or fingerprint, or removes a host
// from known_hosts.
func KeygenMain(args []string, s *IO) int {
	var typ, file, comment, pass, remove string
	var printPub, fingerprint, quiet, passSet bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || len(a) < 2 {
			s.errf("%s\n", keygenUsage)
			return 1
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if strings.IndexByte("tfCNRb", c) >= 0 {
				var v string
				if j+1 < len(a) {
					v = a[j+1:]
				} else if i+1 < len(args) {
					i++
					v = args[i]
				} else {
					s.errf("option requires an argument -- %c\n%s\n", c, keygenUsage)
					return 1
				}
				switch c {
				case 't':
					typ = strings.ToLower(v)
				case 'f':
					file = v
				case 'C':
					comment = v
				case 'N':
					pass, passSet = v, true
				case 'R':
					remove = v
				case 'b': // bits: ed25519 has one size
				}
				break
			}
			switch c {
			case 'y':
				printPub = true
			case 'l':
				fingerprint = true
			case 'q':
				quiet = true
			default:
				s.errf("unknown option -- %c\n%s\n", c, keygenUsage)
				return 1
			}
		}
	}
	if remove != "" {
		return removeKnownHost(remove, file, s)
	}
	if printPub || fingerprint {
		if file == "" {
			f, err := s.Ask(fmt.Sprintf("Enter file in which the key is (%s): ", defaultKeyFile()), true)
			if err != nil {
				return 1
			}
			file = orDefault(f, defaultKeyFile())
		}
		return showKey(file, printPub, s)
	}
	if typ != "" && typ != "ed25519" {
		s.errf("This ssh-keygen (gecko) makes ed25519 keys only; use -t ed25519.\n")
		return 1
	}
	if !quiet {
		s.errf("Generating public/private ed25519 key pair.\n")
	}
	if file == "" {
		f, err := s.Ask(fmt.Sprintf("Enter file in which to save the key (%s): ", defaultKeyFile()), true)
		if err != nil {
			return 1
		}
		file = orDefault(f, defaultKeyFile())
	}
	file = expandHome(file)
	if _, err := os.Stat(file); err == nil {
		ans, err := s.Ask(fmt.Sprintf("%s already exists.\nOverwrite (y/n)? ", file), true)
		if err != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
			return 1
		}
	}
	if !passSet {
		for {
			p1, err := s.Ask("Enter passphrase for \""+file+"\" (empty for no passphrase): ", false)
			if err != nil {
				return 1
			}
			p2, err := s.Ask("Enter same passphrase again: ", false)
			if err != nil {
				return 1
			}
			if p1 == p2 {
				pass = p1
				break
			}
			s.errf("Passphrases do not match.  Try again.\n")
		}
	}
	if comment == "" {
		host, _ := os.Hostname()
		comment = localUser() + "@" + host
	}
	pub, err := WriteKey(file, comment, pass)
	if err != nil {
		s.errf("Saving key \"%s\" failed: %v\n", file, err)
		return 1
	}
	if !quiet {
		s.errf("Your identification has been saved in %s\nYour public key has been saved in %s.pub\nThe key fingerprint is:\n%s %s\n",
			file, file, ssh.FingerprintSHA256(pub), comment)
	}
	return 0
}

// WriteKey makes an ed25519 key pair: file (0600) and file.pub.
func WriteKey(file, comment, passphrase string) (ssh.PublicKey, error) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	var block *pem.Block
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(sk, comment, []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(sk, comment)
	}
	if err != nil {
		return nil, err
	}
	pub, err := ssh.NewPublicKey(pk)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(file, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " " + comment + "\n"
	return pub, os.WriteFile(file+".pub", []byte(line), 0o644)
}

func defaultKeyFile() string { return filepath.Join(Home(), ".ssh", "id_ed25519") }

func orDefault(s, d string) string {
	if s = strings.TrimSpace(s); s == "" {
		return d
	}
	return expandHome(s)
}

func expandHome(s string) string {
	if strings.HasPrefix(s, "~/") {
		return filepath.Join(Home(), s[2:])
	}
	return s
}

func showKey(file string, pub bool, s *IO) int {
	file = expandHome(file)
	b, err := os.ReadFile(file)
	if err != nil {
		s.errf("%s: %s\n", file, plainFileError(err))
		return 1
	}
	var key ssh.PublicKey
	comment := ""
	if k, c, _, _, err := ssh.ParseAuthorizedKey(b); err == nil {
		key, comment = k, c
	} else {
		sg, err := ssh.ParsePrivateKey(b)
		var pm *ssh.PassphraseMissingError
		if errors.As(err, &pm) {
			p, aerr := s.Ask("Enter passphrase: ", false)
			if aerr != nil {
				return 1
			}
			sg, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(p))
		}
		if err != nil {
			s.errf("Load key \"%s\": invalid format\n", file)
			return 1
		}
		key = sg.PublicKey()
		if pb, err := os.ReadFile(file + ".pub"); err == nil {
			if _, c, _, _, err := ssh.ParseAuthorizedKey(pb); err == nil {
				comment = c
			}
		}
	}
	if pub {
		line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
		if comment != "" {
			line += " " + comment
		}
		fmt.Fprintln(s.Out, line)
		return 0
	}
	bits := 256
	fmt.Fprintf(s.Out, "%d %s %s (%s)\n", bits, ssh.FingerprintSHA256(key), comment, keyName(key))
	return 0
}

// removeKnownHost drops every line for host from known_hosts, keeping a .old copy.
func removeKnownHost(host, file string, s *IO) int {
	if file == "" {
		file = filepath.Join(Home(), ".ssh", "known_hosts")
	}
	file = expandHome(file)
	// host, or [host]:port as known_hosts writes it
	h, port := host, 22
	if strings.HasPrefix(host, "[") {
		if i := strings.LastIndex(host, "]:"); i > 0 {
			h = host[1:i]
			if _, err := fmt.Sscanf(host[i+2:], "%d", &port); err != nil {
				s.errf("bad port in %s\n", host)
				return 1
			}
		} else {
			h = strings.Trim(host, "[]")
		}
	}
	name := knownName(h, port)
	b, err := os.ReadFile(file)
	if err != nil {
		s.errf("%s: %s\n", file, plainFileError(err))
		return 1
	}
	var keep []string
	found := false
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		_, hosts, _, _, _, err := ssh.ParseKnownHosts([]byte(line))
		match := false
		if err == nil {
			for _, x := range hosts {
				if hostMatches(x, name) {
					match = true
				}
			}
		}
		if match {
			found = true
			continue
		}
		keep = append(keep, line)
	}
	if !found {
		return 0
	}
	_ = os.WriteFile(file+".old", b, 0o600)
	if err := os.WriteFile(file, []byte(strings.Join(keep, "\n")+"\n"), 0o600); err != nil {
		s.errf("%v\n", err)
		return 1
	}
	fmt.Fprintf(s.Out, "# Host %s found\n%s updated.\nOriginal contents retained as %s.old\n", name, file, file)
	return 0
}
