package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Termux from Google Play may not execute its own files (newer Android
// forbids it for apps that target it), so its termux-exec starts every
// program through Android's linker instead: /system/bin/linker64 <program>
// <args>. The static-PIE Linux build of Gecko starts that way too (see
// internal/staticpie). Started like that, /proc/self/exe is the linker, not
// Gecko, and the programs Gecko starts itself (shells, tmux, git, Gecko
// again) have to go through the linker as well.

// startArgv0 is the program as it was started (made absolute before
// anything can change the working directory).
var startArgv0 = func() string {
	if len(os.Args) == 0 {
		return ""
	}
	p, err := filepath.Abs(os.Args[0])
	if err != nil {
		return os.Args[0]
	}
	return p
}()

var linker = sync.OnceValue(func() string {
	if !IsAndroid() {
		return ""
	}
	exe, err := os.Readlink("/proc/self/exe")
	if err != nil || !isLinker(exe) {
		return ""
	}
	return "/system/bin/" + filepath.Base(exe)
})

// isLinker: /system/bin/linker64, which links to the runtime APEX's
// (/apex/com.android.runtime/bin/linker64), or the 32-bit linker.
func isLinker(p string) bool {
	return (strings.HasPrefix(p, "/system/") || strings.HasPrefix(p, "/apex/")) &&
		(filepath.Base(p) == "linker64" || filepath.Base(p) == "linker")
}

// Linker is Android's linker when Gecko was started through it, else "".
func Linker() string { return linker() }

// Executable is os.Executable, except when Gecko was started through
// Android's linker: then it is Gecko's own file, not the linker.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err == nil && isLinker(exe) && Linker() != "" && startArgv0 != "" {
		return startArgv0, nil
	}
	return exe, err
}
