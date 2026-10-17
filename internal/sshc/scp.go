package sshc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const scpUsage = `usage: scp [-pqr] [-i identity_file] [-o ssh_option] [-P port] source ... target`

// remoteSpec splits [user@]host:path; ok is false for a local path.
func remoteSpec(s string) (dest, p string, ok bool) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return "", "", false
	}
	if slash := strings.IndexByte(s, '/'); slash >= 0 && slash < i {
		return "", "", false // ./a:b is a local file
	}
	if strings.HasPrefix(s, "[") { // [v6]:path
		if end := strings.Index(s, "]:"); end > 0 {
			return s[:end+1], s[end+2:], true
		}
	}
	return s[:i], s[i+1:], true
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ScpMain runs `scp args...` with the classic scp protocol (the server's
// `scp -t` / `scp -f`), between this machine and one remote one.
func ScpMain(args []string, s *IO) int {
	var sshArgs []string
	recursive, preserve := false, false
	var paths []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(paths) > 0 || !strings.HasPrefix(a, "-") || a == "-" {
			paths = append(paths, a)
			continue
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if strings.IndexByte("PioFlcS", c) >= 0 {
				var v string
				if j+1 < len(a) {
					v = a[j+1:]
				} else if i+1 < len(args) {
					i++
					v = args[i]
				} else {
					s.errf("%s\n", scpUsage)
					return 1
				}
				switch c {
				case 'P':
					sshArgs = append(sshArgs, "-p", v)
				case 'i', 'o', 'F':
					sshArgs = append(sshArgs, "-"+string(c), v)
				}
				break
			}
			switch c {
			case 'r':
				recursive = true
			case 'p':
				preserve = true
			case 'q':
				sshArgs = append(sshArgs, "-q")
			case 'O', 'T', 'C', '4', '6', 'B', 'v':
			default:
				s.errf("unknown option -- %c\n%s\n", c, scpUsage)
				return 1
			}
		}
	}
	if len(paths) < 2 {
		s.errf("%s\n", scpUsage)
		return 1
	}
	target := paths[len(paths)-1]
	sources := paths[:len(paths)-1]
	flags := ""
	if recursive {
		flags += " -r"
	}
	if preserve {
		flags += " -p"
	}
	if dest, rpath, ok := remoteSpec(target); ok {
		for _, src := range sources {
			if _, _, r := remoteSpec(src); r {
				s.errf("scp: copying between two remote machines is not supported (gecko)\n")
				return 1
			}
		}
		if rpath == "" {
			rpath = "."
		}
		return scpRun(append(sshArgs, dest), "scp -t"+flags+" "+shellQuote(rpath), s, func(r *bufio.Reader, w io.Writer) error {
			if err := ack(r); err != nil {
				return err
			}
			for _, src := range sources {
				if err := sendPath(r, w, src, recursive, s); err != nil {
					return err
				}
			}
			return nil
		})
	}
	code := 0
	for _, src := range sources {
		dest, rpath, ok := remoteSpec(src)
		if !ok {
			s.errf("scp: copying between two local paths: use cp\n")
			return 1
		}
		c := scpRun(append(append([]string{}, sshArgs...), dest), "scp -f"+flags+" "+shellQuote(rpath), s, func(r *bufio.Reader, w io.Writer) error {
			return receive(r, w, target, s)
		})
		if c != 0 {
			code = c
		}
	}
	return code
}

// scpRun connects like ssh and runs remote, with proto speaking the scp
// protocol over its stdin and stdout.
func scpRun(sshArgs []string, remote string, s *IO, proto func(*bufio.Reader, io.Writer) error) int {
	o, err := ParseArgs(append(append([]string{"-T"}, sshArgs...), remote))
	if err != nil {
		s.errf("%v\n", err)
		return 1
	}
	o.Command = []string{remote}
	r, err := Resolve(o)
	if err != nil {
		s.errf("%v\n", err)
		return 1
	}
	c, err := Dial(r, s)
	if err != nil {
		if errors.Is(err, errHostKey) {
			s.errf("%s\n", errHostKey)
		} else {
			s.errf("%s\n", err)
		}
		return 255
	}
	defer c.Close()
	sess, err := c.NewSession()
	if err != nil {
		s.errf("scp: %v\n", err)
		return 1
	}
	defer sess.Close()
	in, _ := sess.StdinPipe()
	out, _ := sess.StdoutPipe()
	sess.Stderr = s.Err
	if err := sess.Start(remote); err != nil {
		s.errf("scp: %v\n", err)
		return 1
	}
	perr := proto(bufio.NewReader(out), in)
	in.Close()
	werr := sess.Wait()
	if perr != nil {
		s.errf("scp: %v\n", perr)
		return 1
	}
	if werr != nil {
		return 1
	}
	return 0
}

// ack reads the other side's 0 (ok), or 1/2 and a message.
func ack(r *bufio.Reader) error {
	b, err := r.ReadByte()
	if err != nil {
		return err
	}
	if b == 0 {
		return nil
	}
	msg, _ := r.ReadString('\n')
	return errors.New(strings.TrimSpace(msg))
}

func sendPath(r *bufio.Reader, w io.Writer, p string, recursive bool, s *IO) error {
	st, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("%s: %s", p, plainFileError(err))
	}
	if st.IsDir() {
		if !recursive {
			return fmt.Errorf("%s: not a regular file", p)
		}
		fmt.Fprintf(w, "D%04o 0 %s\n", st.Mode().Perm(), filepath.Base(p))
		if err := ack(r); err != nil {
			return err
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := sendPath(r, w, filepath.Join(p, e.Name()), recursive, s); err != nil {
				return err
			}
		}
		fmt.Fprint(w, "E\n")
		return ack(r)
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(w, "C%04o %d %s\n", st.Mode().Perm(), st.Size(), filepath.Base(p))
	if err := ack(r); err != nil {
		return err
	}
	if _, err := io.CopyN(w, f, st.Size()); err != nil {
		return err
	}
	if _, err := w.Write([]byte{0}); err != nil {
		return err
	}
	return ack(r)
}

// receive is the sink side: files (and directories, with -r) into target.
func receive(r *bufio.Reader, w io.Writer, target string, s *IO) error {
	ok := func() { _, _ = w.Write([]byte{0}) }
	ok()
	dirs := []string{target}
	targetIsDir := false
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		targetIsDir = true
	}
	first := true
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF && line == "" {
			return nil
		}
		if err != nil {
			return err
		}
		if line == "" {
			continue
		}
		switch line[0] {
		case 1, 2:
			msg := strings.TrimSpace(line[1:])
			if line[0] == 2 {
				return errors.New(msg)
			}
			s.errf("scp: %s\n", msg)
			continue
		case 'T':
			ok()
			continue
		case 'E':
			if len(dirs) > 1 {
				dirs = dirs[:len(dirs)-1]
			}
			ok()
			continue
		case 'C', 'D':
		default:
			return fmt.Errorf("protocol error: %q", strings.TrimSpace(line))
		}
		f := strings.SplitN(strings.TrimSpace(line[1:]), " ", 3)
		if len(f) != 3 {
			return fmt.Errorf("protocol error: %q", line)
		}
		mode, _ := strconv.ParseUint(f[0], 8, 32)
		size, _ := strconv.ParseInt(f[1], 10, 64)
		name := path.Base(f[2])
		if name == "." || name == ".." || name == "/" {
			return fmt.Errorf("refusing a file called %q", f[2])
		}
		cur := dirs[len(dirs)-1]
		dst := filepath.Join(cur, name)
		if first && !targetIsDir {
			dst = target // scp host:file newname
		}
		first = false
		if line[0] == 'D' {
			if err := os.MkdirAll(dst, os.FileMode(mode)|0o700); err != nil {
				return err
			}
			dirs = append(dirs, dst)
			ok()
			continue
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode))
		if err != nil {
			return err
		}
		ok()
		_, err = io.CopyN(out, r, size)
		out.Close()
		if err != nil {
			return err
		}
		if err := ack(r); err != nil {
			return err
		}
		ok()
	}
}
