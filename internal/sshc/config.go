package sshc

import (
	"bufio"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Resolved is a connection after the command line and ~/.ssh/config.
type Resolved struct {
	Alias      string // the host as typed
	HostName   string // what to connect to
	User       string
	Port       int
	Identities []string // explicit (-i, IdentityFile)
	KnownHosts string
	Strict     string // yes, no, accept-new, ask
	Batch      bool
	Timeout    int // seconds, 0 for none
	AliveEvery int // seconds, 0 for never
	AliveMax   int
	RequestTTY string // auto, yes, no, force
	Password   bool   // PasswordAuthentication and keyboard-interactive allowed
}

// Resolve applies the -o options, then the first matching values in the
// config file, then defaults, as OpenSSH does.
func Resolve(o *Options) (*Resolved, error) {
	set := map[string]string{}
	var idents []string
	for k, v := range o.Set {
		set[k] = v
	}
	file := o.ConfigFile
	if file == "" {
		file = filepath.Join(Home(), ".ssh", "config")
	}
	if file != "none" {
		cfg, err := readConfig(file)
		if err != nil && (o.ConfigFile != "" || !os.IsNotExist(err)) {
			return nil, err
		}
		for _, kv := range cfg.lookup(o.Host) {
			if kv.key == "identityfile" {
				idents = append(idents, kv.val)
				continue
			}
			if _, ok := set[kv.key]; !ok {
				set[kv.key] = kv.val
			}
		}
	}
	r := &Resolved{Alias: o.Host, HostName: o.Host, User: o.User, Port: o.Port, Strict: "ask", AliveMax: 3, RequestTTY: "auto", Password: true}
	if v := set["hostname"]; v != "" {
		r.HostName = strings.ReplaceAll(v, "%h", o.Host)
	}
	if r.User == "" {
		r.User = set["user"]
	}
	if r.User == "" {
		r.User = localUser()
	}
	if r.Port == 0 {
		r.Port, _ = strconv.Atoi(set["port"])
	}
	if r.Port == 0 {
		r.Port = 22
	}
	if v := strings.ToLower(set["stricthostkeychecking"]); v != "" {
		switch v {
		case "yes", "no", "accept-new", "ask", "off":
			r.Strict = v
		}
		if v == "off" {
			r.Strict = "no"
		}
	}
	r.Batch = yes(set["batchmode"])
	r.Timeout, _ = strconv.Atoi(set["connecttimeout"])
	r.AliveEvery, _ = strconv.Atoi(set["serveraliveinterval"])
	if n, err := strconv.Atoi(set["serveralivecountmax"]); err == nil {
		r.AliveMax = n
	}
	if v := strings.ToLower(set["requesttty"]); v != "" {
		r.RequestTTY = v
	}
	if v := set["passwordauthentication"]; v != "" {
		r.Password = yes(v)
	}
	r.KnownHosts = filepath.Join(Home(), ".ssh", "known_hosts")
	if v := set["userknownhostsfile"]; v != "" {
		r.KnownHosts = expand(strings.Fields(v)[0], r)
	}
	for _, id := range append(append([]string{}, o.Identities...), idents...) {
		r.Identities = append(r.Identities, expand(id, r))
	}
	if v := set["identityfile"]; v != "" { // from -o
		r.Identities = append(r.Identities, expand(v, r))
	}
	return r, nil
}

func yes(v string) bool {
	v = strings.ToLower(v)
	return v == "yes" || v == "true"
}

// localUser is the name to log in as when none is given.
func localUser() string {
	for _, k := range []string{"USER", "LOGNAME"} {
		if u := os.Getenv(k); u != "" {
			return u
		}
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "root"
}

type kv struct{ key, val string }

type block struct {
	patterns []string
	opts     []kv
}

type config struct{ blocks []block }

// readConfig reads Host blocks; Match blocks and Include are skipped.
func readConfig(file string) (*config, error) {
	f, err := os.Open(file)
	if err != nil {
		return &config{}, err
	}
	defer f.Close()
	c := &config{blocks: []block{{patterns: []string{"*"}}}}
	cur := &c.blocks[0]
	skip := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := cutOption(line)
		if !ok {
			continue
		}
		switch k {
		case "host":
			c.blocks = append(c.blocks, block{patterns: strings.Fields(v)})
			cur = &c.blocks[len(c.blocks)-1]
			skip = false
			continue
		case "match":
			skip = true
			continue
		}
		if !skip {
			cur.opts = append(cur.opts, kv{k, v})
		}
	}
	return c, sc.Err()
}

func (c *config) lookup(host string) []kv {
	var out []kv
	for _, b := range c.blocks {
		if matchHost(b.patterns, host) {
			out = append(out, b.opts...)
		}
	}
	return out
}

// matchHost: any pattern matches and no negated one does.
func matchHost(patterns []string, host string) bool {
	host = strings.ToLower(host)
	hit := false
	for _, p := range patterns {
		neg := strings.HasPrefix(p, "!")
		p = strings.ToLower(strings.TrimPrefix(p, "!"))
		if ok, _ := path.Match(p, host); ok {
			if neg {
				return false
			}
			hit = true
		}
	}
	return hit
}
