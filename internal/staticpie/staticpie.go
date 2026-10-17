//go:build staticpie

// Package staticpie is the start code of the static-PIE Linux build (make
// dist, tag staticpie): Termux from Google Play starts programs through
// Android's linker (/system/bin/linker64 gecko ...), which loads only
// position-independent programs and leaves them in a state musl's own
// start code doesn't expect. start.c, the program's entry point, puts that
// right; a program the kernel starts is untouched.
package staticpie

// #cgo CFLAGS: -fno-stack-protector
import "C"
