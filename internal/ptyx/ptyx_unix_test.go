//go:build !windows

package ptyx

import (
	"errors"
	"os"
	"testing"
	"time"
)

// A reader waiting for output wakes as soon as the session is closed, not
// only once the program lets go of the terminal. A reader that never woke
// kept a closed tab listed, and it came back.
func TestCloseWakesWaitReadable(t *testing.T) {
	p, err := Start(Options{Argv: []string{"/bin/sh", "-c", "trap '' HUP; exec sleep 30"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	up := p.(*unixPty)
	woke := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			if err := up.WaitReadable(); err != nil {
				woke <- err
				return
			}
			if _, err := up.Read(buf); err != nil {
				woke <- err
				return
			}
		}
	}()
	time.Sleep(200 * time.Millisecond)
	p.Close()
	select {
	case err := <-woke:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("WaitReadable after Close: %v, want os.ErrClosed", err)
		}
	case <-time.After(time.Second): // sleep holds the terminal until the 3s SIGKILL
		t.Fatal("the reader still waits 1s after Close")
	}
	p.Wait()
}
