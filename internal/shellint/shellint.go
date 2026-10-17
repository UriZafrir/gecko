// Package shellint launches shells with Gecko's shell integration, which
// reports prompt boundaries, command lines, exit codes and the working
// directory via OSC 133 / 633 / 7 escape sequences.
package shellint

import (
	"embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gecko-term/gecko/internal/config"
)

//go:embed scripts
var scripts embed.FS

// Install writes the integration scripts into dir.
func Install(dir string) error {
	files := map[string]string{
		"scripts/gecko.bash": "gecko.bash",
		"scripts/gecko.fish": "gecko.fish",
		"scripts/zshenv":     "zsh/.zshenv",
		"scripts/zprofile":   "zsh/.zprofile",
		"scripts/zshrc":      "zsh/.zshrc",
	}
	for src, dst := range files {
		b, err := scripts.ReadFile(src)
		if err != nil {
			return err
		}
		p := filepath.Join(dir, dst)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// DefaultShell returns the user's preferred shell.
func DefaultShell() string {
	if runtime.GOOS == "windows" {
		for _, s := range []string{"pwsh.exe", "powershell.exe"} {
			if p, err := exec.LookPath(s); err == nil {
				return p
			}
		}
		if c := os.Getenv("COMSPEC"); c != "" {
			return c
		}
		return "cmd.exe"
	}
	prefix := config.TermuxPrefix()
	// Termux sets SHELL to its login program, which only starts the real
	// shell (without Gecko's integration): look for that one instead.
	if s := os.Getenv("SHELL"); s != "" && !(prefix != "" && filepath.Base(s) == "login") {
		return s
	}
	for _, s := range shellCandidates(prefix) {
		if st, err := os.Stat(s); err == nil && !st.IsDir() {
			return s
		}
	}
	return "/bin/sh"
}

// shellCandidates are the shells to look for, in order, when $SHELL isn't
// set. Inside Termux (prefix set) that is the one `chsh` picked
// (~/.termux/shell), then Termux's own: Termux has no /bin, and Android's
// /system/bin/sh is a bare mksh without Termux's environment. Android
// outside Termux has only /system/bin/sh (mksh).
func shellCandidates(prefix string) []string {
	var out []string
	if prefix != "" {
		if home, err := os.UserHomeDir(); err == nil {
			if p, err := filepath.EvalSymlinks(filepath.Join(home, ".termux", "shell")); err == nil {
				out = append(out, p)
			}
		}
		for _, s := range []string{"zsh", "bash", "sh"} {
			out = append(out, filepath.Join(prefix, "bin", s))
		}
	}
	return append(out, "/bin/zsh", "/bin/bash", "/bin/sh", "/system/bin/sh")
}

// Command returns argv and extra environment for an interactive, integrated
// instance of shell. dir is where Install put the scripts.
func Command(shell, dir string) (argv, env []string) {
	login := runtime.GOOS == "darwin" // macOS terminals start login shells
	switch strings.TrimSuffix(filepath.Base(shell), ".exe") {
	case "bash":
		if login {
			env = append(env, "GECKO_LOGIN=1")
		}
		return []string{shell, "--rcfile", filepath.Join(dir, "gecko.bash"), "-i"}, env
	case "zsh":
		user := os.Getenv("ZDOTDIR")
		if user == "" {
			user, _ = os.UserHomeDir()
		}
		env = append(env, "ZDOTDIR="+filepath.Join(dir, "zsh"), "GECKO_USER_ZDOTDIR="+user)
		if login {
			return []string{shell, "-l"}, env
		}
		return []string{shell, "-i"}, env
	case "fish":
		argv = []string{shell, "--init-command", "source " + quoteFish(filepath.Join(dir, "gecko.fish"))}
		if login {
			argv = append(argv, "-l")
		}
		return argv, env
	}
	if login && runtime.GOOS != "windows" {
		return []string{shell, "-l"}, nil
	}
	return []string{shell}, nil
}

func quoteFish(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}
