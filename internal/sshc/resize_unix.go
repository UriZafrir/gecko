//go:build !windows

package sshc

import (
	"os"
	"os/signal"
	"syscall"
)

// watchResize calls f when the terminal's size changes (SIGWINCH).
func watchResize(f func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				f()
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}
