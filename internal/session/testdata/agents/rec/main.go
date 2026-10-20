// Command rec records a coding agent's screens as Gecko sees them: it runs
// the agent in a pty at a fixed size, feeds what it draws to Gecko's own vt,
// answers the terminal queries a client would (device attributes, cursor
// position, colors), and follows a script that types keys, waits for the
// screen and saves it as a fixture.
//
//	go run ./internal/session/testdata/agents/rec -out internal/session/testdata/agents/codex \
//	    -agent "codex-cli 0.163.0" -script codex.rec -- codex
//
// Script lines:
//
//	wait <seconds> <regexp>   until the screen matches (fails after seconds)
//	gone <seconds> <regexp>   until the screen no longer matches
//	sleep <ms>
//	keys <Go string literal>  e.g. keys "\x1b" for Esc, keys "\r" for Enter
//	type <text>               the rest of the line, one key at a time
//	activity <ms>             report how much the agent drew in that time
//	note <text>               a line for the next fixture's header
//	dump <name>               save the screen as <out>/<name>.txt (SIZE in
//	                          the name becomes e.g. 50x30)
//	print                     show the screen on stdout
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gecko-term/gecko/internal/agentmock"
	"github.com/gecko-term/gecko/internal/vt"
)

type rec struct {
	mu      sync.Mutex
	term    *vt.Terminal
	f       *os.File
	carry   []byte
	bytes   int
	writes  int
	lastOut time.Time
	raw     *os.File
	// What the agent last said about itself: its terminal title (OSC 0
	// or 2) and its state (OSC 7501, e.g. "state=working:app=codex").
	title, state string
}

func main() {
	cols := flag.Int("cols", 100, "columns")
	rows := flag.Int("rows", 30, "rows")
	out := flag.String("out", ".", "directory for fixtures")
	script := flag.String("script", "", "script file")
	agent := flag.String("agent", "", "agent and version, for fixture headers")
	rawLog := flag.String("raw", "", "also save everything the agent wrote here")
	limit := flag.Duration("timeout", 5*time.Minute, "give up after this long")
	mock := flag.String("mock", "", "run the stand-in model server (internal/agentmock), logging requests to this directory; {{MOCK}} in the command and -env values is its URL")
	var env envFlag
	flag.Var(&env, "env", "KEY=value for the agent (repeatable)")
	flag.Parse()
	if flag.NArg() == 0 || *script == "" {
		fmt.Fprintln(os.Stderr, "usage: rec -script file [-out dir] [-agent name] -- command args...")
		os.Exit(2)
	}
	lines, err := os.ReadFile(*script)
	if err != nil {
		fail("%v", err)
	}
	argv := flag.Args()
	if *mock != "" {
		m, err := agentmock.Start(*mock)
		if err != nil {
			fail("%v", err)
		}
		defer m.Close()
		for i := range argv {
			argv[i] = strings.ReplaceAll(argv[i], "{{MOCK}}", m.URL)
		}
		for i := range env {
			env[i] = strings.ReplaceAll(env[i], "{{MOCK}}", m.URL)
		}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor"), env...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(*cols), Rows: uint16(*rows)})
	if err != nil {
		fail("%v", err)
	}
	r := &rec{term: vt.New(*cols, *rows, 1000), f: f, lastOut: time.Now()}
	r.term.OnOSC = func(num int, payload string) {
		switch {
		case num == 0 || num == 2:
			r.title = payload
		case num == 7501 && payload != "?":
			r.state = payload
		}
	}
	if *rawLog != "" {
		if r.raw, err = os.Create(*rawLog); err != nil {
			fail("%v", err)
		}
	}
	exited := make(chan struct{})
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				r.output(buf[:n])
			}
			if err != nil {
				close(exited)
				return
			}
		}
	}()
	stop := func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
		_ = cmd.Process.Signal(syscall.SIGHUP)
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Process.Kill()
		}
	}
	go func() {
		time.Sleep(*limit)
		fmt.Println("rec: timed out")
		stop()
		os.Exit(1)
	}()
	note := []string{}
	for n, line := range strings.Split(string(lines), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		verb, arg, _ := strings.Cut(line, " ")
		fmt.Printf("rec: %d: %s\n", n+1, line)
		switch verb {
		case "wait", "gone":
			secs, re, _ := strings.Cut(arg, " ")
			d, _ := strconv.ParseFloat(secs, 64)
			if !r.waitFor(regexp.MustCompile(re), verb == "wait", time.Duration(d*float64(time.Second))) {
				fmt.Println(r.screen())
				stop()
				fail("line %d: %s: timed out", n+1, line)
			}
		case "sleep":
			ms, _ := strconv.Atoi(arg)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		case "keys":
			s, err := strconv.Unquote(arg)
			if err != nil {
				fail("line %d: %v", n+1, err)
			}
			r.write([]byte(s))
		case "type":
			for _, c := range arg {
				r.write([]byte(string(c)))
				time.Sleep(15 * time.Millisecond)
			}
		case "activity":
			ms, _ := strconv.Atoi(arg)
			r.mu.Lock()
			b0, w0 := r.bytes, r.writes
			r.mu.Unlock()
			time.Sleep(time.Duration(ms) * time.Millisecond)
			r.mu.Lock()
			fmt.Printf("rec: activity: %d bytes in %d reads over %dms\n", r.bytes-b0, r.writes-w0, ms)
			r.mu.Unlock()
		case "note":
			note = append(note, arg)
		case "print":
			fmt.Println(r.screen())
		case "dump":
			r.mu.Lock()
			c, rw := r.term.Size()
			arg = strings.ReplaceAll(arg, "SIZE", fmt.Sprintf("%dx%d", c, rw))
			screen := strings.Join(r.term.ScreenLines(), "\n")
			title, state := r.title, r.state
			r.mu.Unlock()
			var b strings.Builder
			fmt.Fprintf(&b, "# agent: %s\n# state: %s\n# size: %dx%d, recorded %s with testdata/agents/rec\n", *agent, arg, c, rw, time.Now().Format("2006-01-02"))
			fmt.Fprintf(&b, "# title: %s\n", title)
			if state != "" {
				fmt.Fprintf(&b, "# osc7501: %s\n", state)
			}
			for _, l := range note {
				fmt.Fprintf(&b, "# note: %s\n", l)
			}
			b.WriteString("#---\n" + screen + "\n")
			note = nil
			if err := os.WriteFile(filepath.Join(*out, arg+".txt"), []byte(b.String()), 0o644); err != nil {
				fail("%v", err)
			}
		default:
			fail("line %d: unknown %q", n+1, verb)
		}
	}
	stop()
}

type envFlag []string

func (e *envFlag) String() string     { return strings.Join(*e, " ") }
func (e *envFlag) Set(v string) error { *e = append(*e, v); return nil }

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "rec: "+format+"\n", a...)
	os.Exit(1)
}

func (r *rec) screen() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.term.ScreenLines(), "\n")
}

func (r *rec) write(b []byte) {
	_, _ = r.f.Write(b)
}

func (r *rec) waitFor(re *regexp.Regexp, want bool, d time.Duration) bool {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if re.MatchString(r.screen()) == want {
			return true
		}
	}
	return false
}

func (r *rec) output(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bytes += len(p)
	r.writes++
	r.lastOut = time.Now()
	if r.raw != nil {
		_, _ = r.raw.Write(p)
	}
	// Answer queries in order with the screen as it is when each comes.
	buf := append(r.carry, p...)
	r.carry = nil
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] != 0x1b {
			continue
		}
		seq, ok := escape(buf[i:])
		if !ok {
			// Unfinished: keep it for the next read.
			r.term.Write(buf[start:i])
			r.carry = append([]byte(nil), buf[i:]...)
			return
		}
		r.term.Write(buf[start : i+len(seq)])
		start = i + len(seq)
		if reply := r.answer(seq); reply != "" {
			_, _ = r.f.WriteString(reply)
		}
		i += len(seq) - 1
	}
	r.term.Write(buf[start:])
}

// escape returns the escape sequence at the start of b, and false when b
// ends before it does.
func escape(b []byte) ([]byte, bool) {
	if len(b) < 2 {
		return nil, len(b) > 64
	}
	switch b[1] {
	case '[':
		for i := 2; i < len(b); i++ {
			if b[i] >= 0x40 && b[i] <= 0x7e {
				return b[:i+1], true
			}
		}
		return nil, len(b) > 64
	case ']', 'P', '_', '^':
		for i := 2; i < len(b); i++ {
			if b[i] == 7 {
				return b[:i+1], true
			}
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return b[:i+2], true
			}
		}
		return nil, len(b) > 4096
	}
	return b[:2], true
}

var reDECRQM = regexp.MustCompile(`^\x1b\[\?(\d+)\$p$`)

// answer replies to a terminal query the way xterm.js in Gecko's app does.
func (r *rec) answer(seq []byte) string {
	s := string(seq)
	switch {
	case s == "\x1b[c" || s == "\x1b[0c":
		return "\x1b[?62;22c"
	case s == "\x1b[>c" || s == "\x1b[>0c":
		return "\x1b[>0;276;0c"
	case s == "\x1b[5n":
		return "\x1b[0n"
	case s == "\x1b[6n":
		x, y := r.term.Cursor()
		return fmt.Sprintf("\x1b[%d;%dR", y+1, x+1)
	case s == "\x1b[18t":
		c, rw := r.term.Size()
		return fmt.Sprintf("\x1b[8;%d;%dt", rw, c)
	case bytes.HasPrefix(seq, []byte("\x1b]10;?")):
		return "\x1b]10;rgb:c0c0/caca/f5f5\x1b\\"
	case bytes.HasPrefix(seq, []byte("\x1b]11;?")):
		return "\x1b]11;rgb:1a1a/1b1b/2626\x1b\\"
	}
	if m := reDECRQM.FindStringSubmatch(s); m != nil {
		return "\x1b[?" + m[1] + ";2$y"
	}
	return ""
}
