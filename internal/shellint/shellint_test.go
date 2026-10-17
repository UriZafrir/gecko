package shellint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellCandidates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prefix := filepath.Join(t.TempDir(), "usr")
	os.MkdirAll(filepath.Join(prefix, "bin"), 0o755)
	os.WriteFile(filepath.Join(prefix, "bin", "fish"), nil, 0o755)
	got := shellCandidates(prefix)
	want := []string{prefix + "/bin/zsh", prefix + "/bin/bash", prefix + "/bin/sh", "/bin/zsh", "/bin/bash", "/bin/sh", "/system/bin/sh"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %s, want %s", i, got[i], want[i])
		}
	}
	// `chsh` in Termux links ~/.termux/shell to the chosen shell: it comes first.
	os.MkdirAll(filepath.Join(home, ".termux"), 0o700)
	os.Symlink(filepath.Join(prefix, "bin", "fish"), filepath.Join(home, ".termux", "shell"))
	if got := shellCandidates(prefix); got[0] != filepath.Join(prefix, "bin", "fish") {
		t.Errorf("chsh choice not first: %v", got)
	}
	// Off Termux: the usual places, Android's mksh last.
	if got := shellCandidates(""); len(got) != 4 || got[3] != "/system/bin/sh" {
		t.Errorf("off Termux: %v", got)
	}
}
