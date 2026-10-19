// Package hostsetup checks that a machine is reachable over SSH and has
// gecko, and installs gecko there. Output is streamed line by line so a UI
// can show exactly what happened.
package hostsetup

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/execx"
)

// Result is what a successful check found.
type Result struct {
	OS, Arch string // gecko naming: linux/amd64, darwin/arm64, ...
	Gecko    string // `gecko version` output; "" when Missing
	Path     string // where that gecko is (the one `gecko bridge` runs there)
	Missing  bool   // reachable, but gecko isn't installed
}

// sshOpts are the options gecko always uses: no prompts (nobody is there to
// answer), trust a new host key on first use but refuse a changed one.
var sshOpts = []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new"}

func sshCmd(ctx context.Context, target string, remote string) *exec.Cmd {
	return sshCmdWith(ctx, target, remote, Options{})
}

// Options change how a check or install logs in.
type Options struct {
	GeckoPath string
	// Secret is the password to log in with, handed to ssh by the askpass
	// helper (see askpass.go): never on a command line or in a file.
	Secret string
	// InstallKey adds this device's public key to ~/.ssh/authorized_keys
	// over the same login, so later connections need no password.
	InstallKey bool
	// Strict refuses a host whose key isn't known yet, so a person can
	// confirm its fingerprint (the Add machine dialog) instead of it being
	// trusted silently.
	Strict bool
}

func sshCmdWith(ctx context.Context, target, remote string, o Options) *exec.Cmd {
	dest, port := config.SplitTarget(target)
	opts := sshOpts
	if o.Strict {
		opts = []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=yes"}
	}
	if o.Secret != "" {
		opts = append(withoutBatch(opts), "-o", "BatchMode=no", "-o", "NumberOfPasswordPrompts=1")
	}
	args := append([]string{"-T"}, opts...)
	if port != "" {
		args = append(args, "-p", port)
	}
	cmd := execx.CommandContext(ctx, "ssh", append(args, dest, remote)...)
	if o.Secret != "" {
		cmd.Env = append(os.Environ(), askpassEnv(o.Secret)...)
	}
	return cmd
}

func withoutBatch(opts []string) []string {
	var out []string
	for i := 0; i < len(opts); i++ {
		if opts[i] == "-o" && i+1 < len(opts) && strings.HasPrefix(opts[i+1], "BatchMode=") {
			i++
			continue
		}
		out = append(out, opts[i])
	}
	return out
}

// shQuote quotes s for a POSIX shell.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

var remotePath = config.RemotePath

// run executes cmd, sending each output line to log, and returns the last
// line of output (useful as an error message).
func run(cmd *exec.Cmd, log func(string)) (last string, err error) {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			last = line
			log(line)
		}
	}()
	err = cmd.Wait()
	pw.Close()
	wg.Wait()
	return last, err
}

// Probe connects to target and reports the platform and gecko version.
func Probe(ctx context.Context, target, geckoPath string, log func(string)) (Result, error) {
	return ProbeWith(ctx, target, Options{GeckoPath: geckoPath}, log)
}

// ProbeWith is Probe with a password, key install, or strict host keys.
// Failures the UI can do something about are an *Error with a Kind.
func ProbeWith(ctx context.Context, target string, o Options, log func(string)) (Result, error) {
	geckoPath := o.GeckoPath
	defer forgetSecret(o.Secret)
	dest, port := config.SplitTarget(target)
	if dest == "" {
		return Result{}, errors.New("enter an SSH destination, like me@devbox or me@devbox:2222")
	}
	where := dest
	if port != "" {
		where += " (port " + port + ")"
	}
	log("$ ssh " + where)
	gecko := "gecko"
	if geckoPath != "" {
		gecko = shQuote(geckoPath)
	}
	keyStep := ""
	if o.InstallKey {
		pub, err := PublicKey(true)
		if err != nil {
			return Result{}, err
		}
		// Like ssh-copy-id, over this same login.
		keyStep = `umask 077; mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && { grep -qxF ` + shQuote(pub) + ` ~/.ssh/authorized_keys || printf '%s\n' ` + shQuote(pub) + ` >> ~/.ssh/authorized_keys; } && echo GECKO_KEY_ADDED; `
	}
	script := keyStep + `uname -sm; ` + remotePath + `; if command -v ` + gecko + ` >/dev/null 2>&1; then echo "GECKO_PATH: $(command -v ` + gecko + `)"; echo "GECKO: $(` + gecko + ` version)"; else echo GECKO_MISSING; fi`
	var res Result
	var all []string
	last, err := run(sshCmdWith(ctx, target, "sh -c "+shQuote(script), o), func(line string) {
		all = append(all, line)
		switch {
		case line == "GECKO_KEY_ADDED":
			log("added this device's key to ~/.ssh/authorized_keys there")
		case strings.HasPrefix(line, "GECKO_PATH: "):
			res.Path = strings.TrimPrefix(line, "GECKO_PATH: ")
		case strings.HasPrefix(line, "GECKO: "):
			res.Gecko = strings.TrimPrefix(line, "GECKO: ")
			log("found " + res.Gecko)
		case line == "GECKO_MISSING":
			res.Missing = true
			log("gecko is not installed there")
		default:
			if f := strings.Fields(line); res.OS == "" && len(f) == 2 && platform(f[0], f[1]) != "" {
				res.OS, res.Arch = splitPlatform(platform(f[0], f[1]))
			}
			log(line)
		}
	})
	if err != nil {
		if ctx.Err() != nil {
			return res, errors.New("timed out")
		}
		return res, classify(target, strings.Join(all, "\n"), last)
	}
	if res.OS == "" {
		return res, errors.New("connected, but couldn't tell what system this is")
	}
	return res, nil
}

func platform(sys, machine string) string {
	goos := map[string]string{"Linux": "linux", "Darwin": "darwin", "FreeBSD": "freebsd"}[sys]
	goarch := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[machine]
	if goos == "" || goarch == "" {
		return ""
	}
	return goos + "/" + goarch
}

func splitPlatform(p string) (string, string) {
	a, b, _ := strings.Cut(p, "/")
	return a, b
}

// Binary finds a gecko binary for goos/goarch: this one if it matches, else
// one built by `make dist` next to it or in ./dist.
func Binary(goos, goarch string) (string, error) {
	exe, err := config.Executable()
	if err != nil {
		return "", err
	}
	if goos == runtime.GOOS && goarch == runtime.GOARCH {
		return exe, nil
	}
	if config.IsAndroid() && goos == "linux" && os.Getenv("GECKO_APK") != "" {
		// The Android app carries Gecko for both phone CPUs, which are the
		// Linux builds for those CPUs too: take the other one out of the APK.
		return fromAPK(os.Getenv("GECKO_APK"), goarch, filepath.Join(config.StateDir(), "dist"))
	}
	for _, dir := range []string{filepath.Dir(exe), "dist"} {
		p := filepath.Join(dir, "gecko-"+goos+"-"+goarch)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no gecko build for %s/%s here; run `make dist` in the gecko repo", goos, goarch)
}

// Install copies a matching gecko to target and returns where it went
// there: over the gecko `gecko bridge` runs there when this login may
// replace it, else ~/.local/bin/gecko, which comes first for the bridge.
// So a machine keeps one gecko, the one its connections run.
func Install(ctx context.Context, target string, res Result, log func(string)) (string, error) {
	return InstallWith(ctx, target, res, Options{}, log)
}

// installDir picks the folder to install into, on the machine (see
// Install), and prints it: the folder of the gecko the bridge runs if it is
// a file this login may replace, else ~/.local/bin (made if needed). Any
// other gecko it would leave there is listed after it.
var installDir = remotePath + `; d="$HOME/.local/bin"; p=$(command -v gecko 2>/dev/null); ` +
	`case "$p" in /*) if [ -f "$p" ] && [ ! -L "$p" ] && [ -w "${p%/*}" ] && [ -w "$p" ]; then d=${p%/*}; fi ;; esac; ` +
	`mkdir -p "$d" && echo "GECKO_DIR: $d" && for x in ` + strings.Join(config.BinDirs, " ") + `; do ` +
	`[ -f "$x/gecko" ] && [ "$x" != "$d" ] && echo "GECKO_OTHER: $x/gecko"; done; true`

// InstallWith is Install, logging in with o.Secret if there is one.
func InstallWith(ctx context.Context, target string, res Result, o Options, log func(string)) (string, error) {
	defer forgetSecret(o.Secret)
	bin, err := Binary(res.OS, res.Arch)
	if err != nil {
		return "", err
	}
	dest, port := config.SplitTarget(target)
	dir := ""
	var others []string
	last, err := run(sshCmdWith(ctx, target, "sh -c "+shQuote(installDir), o), func(line string) {
		switch {
		case strings.HasPrefix(line, "GECKO_DIR: "):
			dir = strings.TrimPrefix(line, "GECKO_DIR: ")
		case strings.HasPrefix(line, "GECKO_OTHER: "):
			others = append(others, strings.TrimPrefix(line, "GECKO_OTHER: "))
		default:
			log(line)
		}
	})
	if err != nil {
		return "", errors.New(Explain(last, ""))
	}
	if !strings.HasPrefix(dir, "/") {
		return "", errors.New("couldn't find a folder to install gecko into there")
	}
	user, host := "", dest
	if i := strings.LastIndexByte(dest, '@'); i >= 0 {
		user, host = dest[:i+1], dest[i+1:]
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	opts := sshOpts
	if o.Secret != "" {
		opts = append(withoutBatch(opts), "-o", "BatchMode=no", "-o", "NumberOfPasswordPrompts=1")
	}
	args := append([]string{"-q"}, opts...)
	if port != "" {
		args = append(args, "-P", port)
	}
	// Copied under another name and renamed over the old one: the gecko
	// running there keeps its file, so this never fails with "text file
	// busy", and nothing there ever starts a half-copied binary.
	exe, tmp := dir+"/gecko", dir+"/.gecko.new"
	args = append(args, bin, user+host+":"+tmp)
	log("$ scp gecko-" + res.OS + "-" + res.Arch + " " + dest + ":" + exe)
	scp := execx.CommandContext(ctx, "scp", args...)
	if o.Secret != "" {
		scp.Env = append(os.Environ(), askpassEnv(o.Secret)...)
	}
	if last, err := run(scp, log); err != nil {
		return "", errors.New(Explain(last, ""))
	}
	// And it must run there (a noexec home, another CPU) before it
	// replaces anything.
	finish := "chmod 755 " + shQuote(tmp) + " && { " + shQuote(tmp) + " version >/dev/null || { rm -f " + shQuote(tmp) + "; echo 'the copied gecko does not run there'; exit 1; }; } && mv -f " + shQuote(tmp) + " " + shQuote(exe)
	if last, err := run(sshCmdWith(ctx, target, "sh -c "+shQuote(finish), o), log); err != nil {
		return "", errors.New(Explain(last, ""))
	}
	log("installed " + exe)
	for _, x := range others {
		log("note: " + x + " is another gecko there; " + exe + " comes first for Gecko's connections, but a shell may still run the other one. Remove it, or run `" + exe + " upgrade --installed` there")
	}
	return exe, nil
}

// Explain turns common ssh / shell failures into what to do about them.
func Explain(msg, host string) string {
	install := "gecko host install"
	if host != "" {
		install += " " + host
	}
	switch {
	case msg == "":
		return "the connection failed without saying why"
	case strings.Contains(msg, "Permission denied") && config.IsAndroid():
		return msg + ". Gecko connects without a password prompt: add this phone's key (in the Gecko app, ⋮ then SSH key) to ~/.ssh/authorized_keys there"
	case strings.Contains(msg, "Permission denied"):
		return msg + ". Gecko connects without a password prompt; set up key login (ssh-copy-id) for this machine"
	case strings.Contains(msg, "gecko: not found"), strings.Contains(msg, "gecko: command not found"), strings.Contains(msg, "exec: gecko"):
		return "Gecko isn't installed there. Run: " + install
	case strings.Contains(msg, "Host key verification failed"), strings.Contains(msg, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
		return msg + ". The host key changed; check it, then fix ~/.ssh/known_hosts"
	case strings.Contains(msg, "Could not resolve hostname"):
		return msg + ". Check the name (use host:port or user@host:port)"
	case strings.Contains(msg, "Connection refused"):
		return msg + ". Nothing is listening for SSH there (wrong port?)"
	case strings.Contains(msg, "timed out"):
		return msg + ". The machine didn't answer; is it on and reachable from here?"
	}
	return msg
}
