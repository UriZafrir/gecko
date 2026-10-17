package execx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gecko-term/gecko/internal/config"
)

// LookPath is exec.LookPath, except on Android: there Go's LookPath checks
// the file with the faccessat2 system call, which Android's seccomp filter
// answers by killing the process (SIGSYS) instead of saying "not
// supported". So on Android the PATH is searched by looking at the files'
// mode bits instead.
func LookPath(name string) (string, error) {
	if !config.IsAndroid() {
		return exec.LookPath(name)
	}
	if strings.Contains(name, "/") {
		if executable(name) {
			return name, nil
		}
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		p := filepath.Join(dir, name)
		if executable(p) {
			return p, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func executable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// resolve gives exec.Command a path, so it doesn't look the name up itself.
func resolve(name string) string {
	if !config.IsAndroid() || strings.Contains(name, "/") {
		return name
	}
	if p, err := LookPath(name); err == nil {
		return p
	}
	return name
}
