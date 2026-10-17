package execx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookPath(t *testing.T) {
	// Off Android it is exec.LookPath; the manual search must agree with it.
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "tool"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(d, "data"), []byte("x"), 0o644)
	t.Setenv("PATH", d)
	if p, err := LookPath("tool"); err != nil || p != filepath.Join(d, "tool") {
		t.Errorf("tool: %q %v", p, err)
	}
	if _, err := LookPath("data"); err == nil {
		t.Error("found a file that isn't executable")
	}
	if !executable(filepath.Join(d, "tool")) || executable(filepath.Join(d, "data")) || executable(d) {
		t.Error("executable()")
	}
}
