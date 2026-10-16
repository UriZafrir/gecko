package sshc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// IO is where a command reads, writes and asks. Questions (passwords, host
// keys) go to the terminal, like OpenSSH's /dev/tty, so they work while
// stdin and stdout are a pipe.
type IO struct {
	In       io.Reader
	Out, Err io.Writer
	// Ask asks a question; echo is false for passwords. Nil uses the terminal.
	AskFunc func(prompt string, echo bool) (string, error)
	quiet   bool
}

// StdIO is the process's own stdin, stdout and stderr.
func StdIO() *IO { return &IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr} }

func (s *IO) errf(format string, a ...any) { fmt.Fprintf(s.Err, format, a...) }

// Ask prompts on the terminal.
func (s *IO) Ask(prompt string, echo bool) (string, error) {
	if s.AskFunc != nil {
		return s.AskFunc(prompt, echo)
	}
	if ans, used, err := askpass(prompt); used {
		return ans, err
	}
	return askTTY(prompt, echo)
}

func askTTY(prompt string, echo bool) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		tty = nil
	}
	in, out := os.Stdin, os.Stderr
	if tty != nil {
		defer tty.Close()
		in, out = tty, tty
	}
	fmt.Fprint(out, prompt)
	if !echo && term.IsTerminal(int(in.Fd())) {
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		return string(b), err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
