// Package sshc is a small OpenSSH-compatible ssh client (and ssh-keygen,
// scp) built into gecko, for systems without one: Android has no ssh, so
// the Gecko app links `ssh`, `ssh-keygen` and `scp` to gecko on the phone's
// PATH. It covers interactive use and what Gecko itself needs to reach
// other machines: password, keyboard-interactive and key logins,
// known_hosts with trust on first use, a terminal with resizing, remote
// commands, and the basics of ~/.ssh/config. No agent, no port forwarding,
// no X11, no escape sequences.
package sshc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Options are what one ssh command line asks for, before ~/.ssh/config.
type Options struct {
	User       string
	Host       string
	Port       int
	Identities []string
	Command    []string
	ConfigFile string
	TTY        int  // -t count (2 forces); -1 for -T
	NoStdin    bool // -n
	NoCommand  bool // -N
	Quiet      bool
	Verbose    int
	// -o options, lower-cased keys; the first one given wins, like OpenSSH.
	Set map[string]string
}

// usage is OpenSSH's, with what this client does.
const usage = `usage: ssh [-46NnqTtv] [-F configfile] [-i identity_file] [-l login_name]
           [-o option] [-p port] destination [command [argument ...]]`

// ParseArgs reads an ssh command line.
func ParseArgs(args []string) (*Options, error) {
	o := &Options{Set: map[string]string{}}
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		flags := a[1:]
		for j := 0; j < len(flags); j++ {
			c := flags[j]
			takes := strings.IndexByte("bBcDeEFiIJlLmoOpPQRSwW", c) >= 0
			var val string
			if takes {
				if j+1 < len(flags) {
					val = flags[j+1:]
				} else if i+1 < len(args) {
					i++
					val = args[i]
				} else {
					return nil, fmt.Errorf("option requires an argument -- %c\n%s", c, usage)
				}
				j = len(flags)
			}
			switch c {
			case '4', '6', 'a', 'C', 'x', 'y', 'g', 'k', 'K':
			case 'A', 'X', 'Y', 'f', 'G', 'M', 's', 'V':
				if c == 'V' {
					return nil, errVersion
				}
				return nil, fmt.Errorf("-%c is not supported by this ssh (gecko)", c)
			case 'N':
				o.NoCommand = true
			case 'n':
				o.NoStdin = true
			case 'q':
				o.Quiet = true
			case 'v':
				o.Verbose++
			case 't':
				if o.TTY < 0 {
					o.TTY = 0
				}
				o.TTY++
			case 'T':
				o.TTY = -1
			case 'F':
				o.ConfigFile = val
			case 'i':
				o.Identities = append(o.Identities, val)
			case 'l':
				o.User = val
			case 'p':
				p, err := strconv.Atoi(val)
				if err != nil || p <= 0 || p > 65535 {
					return nil, fmt.Errorf("Bad port '%s'", val)
				}
				o.Port = p
			case 'o':
				k, v, ok := cutOption(val)
				if !ok {
					return nil, fmt.Errorf("no argument after keyword %q", val)
				}
				if _, seen := o.Set[k]; !seen {
					o.Set[k] = v
				}
			case 'c', 'm', 'E', 'b', 'B', 'e':
				// ciphers, MACs, log file, bind address, escape char: defaults
			case 'L', 'R', 'D', 'W', 'w', 'J', 'O', 'S', 'I', 'P', 'Q':
				return nil, fmt.Errorf("-%c is not supported by this ssh (gecko)", c)
			default:
				return nil, fmt.Errorf("unknown option -- %c\n%s", c, usage)
			}
		}
	}
	if i >= len(args) {
		return nil, errors.New(usage)
	}
	user, host, port, err := ParseDestination(args[i])
	if err != nil {
		return nil, err
	}
	if o.User == "" {
		o.User = user
	}
	o.Host = host
	if o.Port == 0 {
		o.Port = port
	}
	o.Command = args[i+1:]
	return o, nil
}

var errVersion = errors.New("OpenSSH-compatible ssh built into gecko (golang.org/x/crypto/ssh)")

// cutOption splits "Key=value" or "Key value" (-o and ssh_config).
func cutOption(s string) (key, val string, ok bool) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, "= \t")
	if i < 0 {
		return "", "", false
	}
	key = strings.ToLower(s[:i])
	val = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s[i:]), "="))
	val = strings.Trim(val, `"`)
	return key, val, val != ""
}

// ParseDestination reads user@host, host, or ssh://user@host:port.
func ParseDestination(d string) (user, host string, port int, err error) {
	if strings.HasPrefix(d, "ssh://") {
		d = strings.TrimSuffix(strings.TrimPrefix(d, "ssh://"), "/")
		if i := strings.LastIndexByte(d, '@'); i >= 0 {
			user, d = d[:i], d[i+1:]
		}
		host = d
		if strings.HasPrefix(d, "[") {
			end := strings.IndexByte(d, ']')
			if end < 0 {
				return "", "", 0, fmt.Errorf("bad destination %q", d)
			}
			host = d[1:end]
			d = d[end+1:]
		} else if j := strings.LastIndexByte(d, ':'); j >= 0 {
			host, d = d[:j], d[j:]
		} else {
			d = ""
		}
		if strings.HasPrefix(d, ":") {
			if port, err = strconv.Atoi(d[1:]); err != nil {
				return "", "", 0, fmt.Errorf("bad port in %q", d)
			}
		}
	} else {
		if i := strings.LastIndexByte(d, '@'); i >= 0 {
			user, d = d[:i], d[i+1:]
		}
		host = strings.TrimSuffix(strings.TrimPrefix(d, "["), "]")
	}
	if host == "" {
		return "", "", 0, fmt.Errorf("bad destination %q", d)
	}
	return user, host, port, nil
}

// Home is the user's home ($HOME; on Android the app's home).
func Home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// expand turns ~/ and the %h %p %r %u %% tokens of ssh_config into text.
func expand(s string, r *Resolved) string {
	if strings.HasPrefix(s, "~/") || s == "~" {
		s = filepath.Join(Home(), s[1:])
	}
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'h':
			b.WriteString(r.HostName)
		case 'n':
			b.WriteString(r.Alias)
		case 'p':
			b.WriteString(strconv.Itoa(r.Port))
		case 'r':
			b.WriteString(r.User)
		case 'u':
			b.WriteString(localUser())
		case 'd':
			b.WriteString(Home())
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
