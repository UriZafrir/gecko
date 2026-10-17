package config

import "testing"

func TestTermuxPrefix(t *testing.T) {
	const usr = "/data/data/com.termux/files/usr"
	dirs := map[string]bool{usr + "/bin": true}
	isDir := func(p string) bool { return dirs[p] }
	for _, c := range []struct{ prefix, exe, want string }{
		{usr, usr + "/bin/gecko", usr},
		{usr + "/", "/somewhere/gecko", usr},
		// RUN_COMMAND from another app without Termux's environment.
		{"", usr + "/bin/gecko", usr},
		{"", "/data/data/com.termux/files/home/.local/bin/gecko", usr},
		// The Gecko app's own daemon, a computer.
		{"", "/data/app/~~x/io.github.geckoterm.android-1/lib/arm64/libgecko.so", ""},
		{"/usr", "/usr/local/bin/gecko", ""},
		// Termux's folders gone (bootstrap not installed).
		{"/data/data/com.termux/files/usr2", "/x", ""},
	} {
		if got := termuxPrefix(c.prefix, c.exe, isDir); got != c.want {
			t.Errorf("termuxPrefix(%q, %q) = %q, want %q", c.prefix, c.exe, got, c.want)
		}
	}
	if IsTermux() {
		t.Error("IsTermux on a computer")
	}
}
