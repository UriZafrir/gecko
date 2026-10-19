package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gecko-term/gecko/internal/config"
)

// One gecko per machine. `gecko bridge`, which every SSH connection from
// another Gecko runs, takes the first gecko in config.BinDirs and then on
// PATH; a shell takes the first on its own PATH, and the service runs the
// binary it was started from. A second copy anywhere there (an older
// `make install` with another PREFIX, `go install`, a copy another
// machine's Gecko installed) is a gecko some of them keep running after
// an update. So installers end with `gecko upgrade --installed`, which
// makes every other copy a link to the one just installed and restarts the
// service with it.

// geckoCopy is a gecko binary other than this one.
type geckoCopy struct {
	path    string
	version string // what `gecko version` says
}

// searchDirs are the folders a gecko is looked up in, in the bridge's order.
func searchDirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" || !filepath.IsAbs(d) {
			return
		}
		d = filepath.Clean(d)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	for _, d := range config.BinDirs {
		if strings.HasPrefix(d, "$HOME/") {
			if home == "" {
				continue
			}
			d = filepath.Join(home, strings.TrimPrefix(d, "$HOME/"))
		}
		add(d)
	}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		add(d)
	}
	return dirs
}

// otherCopies finds the gecko binaries in searchDirs that aren't exe.
func otherCopies(exe string) []geckoCopy {
	self, err := os.Stat(exe)
	if err != nil {
		return nil
	}
	name := "gecko"
	if runtime.GOOS == "windows" {
		name = "gecko.exe"
	}
	var out []geckoCopy
	for _, d := range searchDirs() {
		p := filepath.Join(d, name)
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || os.SameFile(st, self) {
			continue
		}
		if v := geckoVersion(p); v != "" {
			out = append(out, geckoCopy{p, v})
		}
	}
	return out
}

// geckoVersion is what p says to `version`, or "" if it isn't gecko.
func geckoVersion(p string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, "version")
	cmd.Stdin = nil
	out, err := cmd.Output()
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if err != nil || !strings.HasPrefix(line, "gecko ") {
		return ""
	}
	return line
}

// warnOtherCopies tells about the other geckos (plain `gecko upgrade`).
func warnOtherCopies(exe string, w io.Writer) {
	for _, c := range otherCopies(exe) {
		fmt.Fprintf(w, "note: %s is another gecko (%s), which Gecko's SSH connections or a shell may run instead of %s; `%s upgrade --installed` makes it a link to this one\n", c.path, c.version, exe, exe)
	}
}

// tidyInstall makes every other gecko in the home folder a link to exe,
// says how to remove the others (a package's, say, which is the package
// manager's to change), and points the login service at exe.
func tidyInstall(exe string, w io.Writer) {
	home, _ := os.UserHomeDir()
	for _, c := range otherCopies(exe) {
		if home == "" || !strings.HasPrefix(c.path, filepath.Clean(home)+string(filepath.Separator)) {
			fmt.Fprintf(w, "note: %s is another gecko (%s), which Gecko's SSH connections or a shell may run instead of %s. Remove it (or its package) unless you want both\n", c.path, c.version, exe)
			continue
		}
		if err := linkOver(exe, c.path); err != nil {
			fmt.Fprintf(w, "note: %s is another gecko (%s), which Gecko's SSH connections or a shell may run instead of %s, and it can't be replaced from here (%v). Remove it: rm %s\n", c.path, c.version, exe, err, c.path)
			continue
		}
		fmt.Fprintf(w, "%s (%s) is now a link to %s\n", c.path, c.version, exe)
	}
	refreshService(exe, w)
}

// linkOver replaces path with a symlink to target, in one rename.
func linkOver(target, path string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("links need administrator rights on Windows")
	}
	tmp := filepath.Join(filepath.Dir(path), ".gecko.link")
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// refreshService points an installed login service at exe, so the next
// login doesn't start an older gecko again.
func refreshService(exe string, w io.Writer) {
	if config.IsAndroid() || os.Getenv("GECKO_HOME") != "" {
		return // the service runs the default home, with a gecko of its own choosing
	}
	p := servicePath()
	b, err := os.ReadFile(p)
	if p == "" || err != nil {
		return
	}
	real, _ := filepath.EvalSymlinks(exe)
	if strings.Contains(string(b), real) {
		return
	}
	if _, err := installService(false); err != nil {
		fmt.Fprintf(w, "note: the login service %s still starts another gecko: %v\n", p, err)
		return
	}
	fmt.Fprintf(w, "the login service now starts %s\n", real)
}

func orUnknown(s string) string {
	if s == "" {
		return "an older build"
	}
	return s
}
