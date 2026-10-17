package config

import (
	"os"
	"path/filepath"
	"strings"
)

// TermuxPrefix is Termux's $PREFIX (/data/data/com.termux/files/usr) when
// Gecko runs inside Termux, or "". Termux is a Linux userland on Android
// with its own package manager: programs live under $PREFIX instead of
// /usr and /bin, there is no /tmp ($TMPDIR is $PREFIX/tmp) and no systemd.
func TermuxPrefix() string {
	if !IsAndroid() {
		return ""
	}
	exe, _ := os.Executable()
	return termuxPrefix(os.Getenv("PREFIX"), exe, func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && st.IsDir()
	})
}

func termuxPrefix(prefix, exe string, isDir func(string) bool) string {
	if !strings.Contains(prefix, "/com.termux/") {
		// Started without Termux's environment: tell from where this binary is.
		i := strings.Index(exe, "/com.termux/files/")
		if i < 0 {
			return ""
		}
		prefix = exe[:i] + "/com.termux/files/usr"
	}
	if !isDir(filepath.Join(prefix, "bin")) {
		return ""
	}
	return filepath.Clean(prefix)
}

// IsTermux reports whether Gecko runs inside Termux.
func IsTermux() bool { return TermuxPrefix() != "" }
