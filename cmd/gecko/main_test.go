package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWindowArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GECKO_HOME", home)
	if got := windowArgs(); !reflect.DeepEqual(got, []string{"--window-size=900,500"}) {
		t.Fatalf("first launch: %v", got)
	}
	os.MkdirAll(filepath.Join(home, "state"), 0o700)
	os.WriteFile(filepath.Join(home, "state", "window.json"), []byte(`{"w":848,"h":537,"sx":1920,"sy":0,"sw":2560,"sh":1400}`), 0o600)
	want := []string{"--window-size=848,537", "--window-position=2776,431"} // centered on the second monitor
	if got := windowArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNotTheInstalledGecko(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GECKO_HOME", "")
	t.Setenv("TMPDIR", filepath.Join(home, "tmp"))
	bin := filepath.Join(home, ".local", "bin")
	src := filepath.Join(home, "src", "gecko")
	for _, d := range []string{bin, filepath.Join(src, "bin")} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module github.com/gecko-term/gecko\n\ngo 1.22\n"), 0o644)
	installed, dev := filepath.Join(bin, "gecko"), filepath.Join(src, "bin", "gecko")
	for _, f := range []string{installed, dev} {
		os.WriteFile(f, []byte("#!/bin/sh\n"), 0o755)
	}
	t.Setenv("PATH", bin)
	if notTheInstalledGecko(installed) {
		t.Fatal("the gecko on PATH is the installed one")
	}
	if !notTheInstalledGecko(dev) {
		t.Fatal("a build inside the source tree must not install the service")
	}
	t.Setenv("GECKO_HOME", filepath.Join(home, "throwaway"))
	if !notTheInstalledGecko(installed) {
		t.Fatal("a run with its own GECKO_HOME must not install the service")
	}
	t.Setenv("GECKO_HOME", filepath.Join(home, ".gecko-terminal"))
	if notTheInstalledGecko(installed) {
		t.Fatal("GECKO_HOME set to the default is still the user's Gecko")
	}
	other := filepath.Join(home, "Downloads", "gecko")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755)
	if !notTheInstalledGecko(other) {
		t.Fatal("another copy than the gecko on PATH must not take over the service")
	}
	t.Setenv("PATH", "")
	if notTheInstalledGecko(other) {
		t.Fatal("with no gecko on PATH, the one running is the user's")
	}
}
