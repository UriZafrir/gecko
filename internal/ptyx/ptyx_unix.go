//go:build !windows

package ptyx

import (
	"errors"
	"github.com/gecko-term/gecko/internal/execx"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type unixPty struct {
	f      *os.File
	cmd    *exec.Cmd // nil for a pty adopted from a previous gecko process
	pid    int
	tty    string
	exited chan struct{}
	once   sync.Once

	// A pipe Close writes to, polled next to the master: a reader waiting
	// in WaitReadable wakes even when the hangup alone wouldn't wake it
	// (a closed tab then stayed listed, its shell a zombie, and came back).
	wakeR, wakeW int
	wmu          sync.Mutex
}

func newUnixPty(f *os.File, cmd *exec.Cmd, pid int, tty string) (*unixPty, error) {
	var w [2]int
	syscall.ForkLock.RLock() // no child may inherit the pipe before it is close-on-exec
	err := unix.Pipe(w[:])
	if err == nil {
		unix.CloseOnExec(w[0])
		unix.CloseOnExec(w[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	return &unixPty{f: f, cmd: cmd, pid: pid, tty: tty, exited: make(chan struct{}), wakeR: w[0], wakeW: w[1]}, nil
}

// Start launches opts.Argv attached to a new pty.
func Start(opts Options) (Pty, error) {
	if len(opts.Argv) == 0 {
		return nil, errors.New("empty argv")
	}
	name := opts.Argv[0]
	if p, err := execx.LookPath(name); err == nil {
		name = p // not exec's own lookup: it can crash on Android (see execx.LookPath)
	}
	argv := execx.Shebang(append([]string{name}, opts.Argv[1:]...)) // scripts in Termux
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	execx.ThroughLinker(cmd) // Termux from Google Play
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	defer tty.Close()
	_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(opts.Cols), Rows: uint16(opts.Rows)})
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		ptmx.Close()
		return nil, err
	}
	ptmx.Fd() // put the master in blocking mode; reads are gated by WaitReadable
	p, err := newUnixPty(ptmx, cmd, cmd.Process.Pid, tty.Name())
	if err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		ptmx.Close()
		_ = cmd.Wait()
		return nil, err
	}
	return p, nil
}

// Adopt wraps a pty master inherited from a previous gecko process (see
// `gecko upgrade`). pid must be a child of this process: exec keeps the PID,
// so the shell started by the old image is still ours to wait for.
func Adopt(fd uintptr, pid int, tty string) (Pty, error) {
	f := os.NewFile(fd, "ptmx")
	if f == nil {
		return nil, errors.New("bad pty fd")
	}
	unix.CloseOnExec(int(fd))
	return newUnixPty(f, nil, pid, tty)
}

func (p *unixPty) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixPty) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *unixPty) Pid() int                    { return p.pid }
func (p *unixPty) TTY() string                 { return p.tty }

// Fd is the master's file descriptor (handed to the next gecko on upgrade).
func (p *unixPty) Fd() uintptr { return p.f.Fd() }

// WaitReadable blocks until a Read would not block (data, hangup or error).
// Readers wait here without holding any locks and then read under the
// session lock, so a session can be paused at an exact byte boundary.
// After Close it returns os.ErrClosed.
func (p *unixPty) WaitReadable() error {
	fds := []unix.PollFd{
		{Fd: int32(p.f.Fd()), Events: unix.POLLIN},
		{Fd: int32(p.wakeR), Events: unix.POLLIN},
	}
	for {
		_, err := unix.Poll(fds, -1)
		if err == unix.EINTR {
			continue
		}
		if err == nil && fds[1].Revents != 0 {
			return os.ErrClosed
		}
		return err
	}
}

func (p *unixPty) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Close hangs up the terminal like a closing window: the shell's group and
// the foreground job get SIGHUP. Whatever survives 3s is killed.
func (p *unixPty) Close() error {
	var err error
	p.once.Do(func() {
		pid := p.pid
		_ = syscall.Kill(-pid, syscall.SIGHUP)
		p.wmu.Lock()
		if p.wakeW >= 0 {
			_, _ = unix.Write(p.wakeW, []byte{1}) // before the master's fd number can be reused
		}
		p.wmu.Unlock()
		err = p.f.Close() // the kernel hangs up the foreground job too
		go func() {
			select {
			case <-p.exited:
			case <-time.After(3 * time.Second):
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	})
	return err
}

// Wait is called once reading has stopped: it also closes the wake pipe.
func (p *unixPty) Wait() (int, error) {
	defer close(p.exited)
	defer p.closeWake()
	if p.cmd != nil {
		err := p.cmd.Wait()
		if p.cmd.ProcessState != nil {
			return p.cmd.ProcessState.ExitCode(), nil
		}
		return -1, err
	}
	var ws syscall.WaitStatus
	for {
		_, err := syscall.Wait4(p.pid, &ws, 0, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return -1, err
		}
		break
	}
	if ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return ws.ExitStatus(), nil
}

func (p *unixPty) closeWake() {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if p.wakeW >= 0 {
		unix.Close(p.wakeR)
		unix.Close(p.wakeW)
		p.wakeR, p.wakeW = -1, -1
	}
}
