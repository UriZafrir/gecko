//go:build ignore

// Lipo joins single-architecture Mach-O binaries into one universal binary,
// like Apple's lipo, so macOS packages build on any OS.
//
//	go run packaging/macos/lipo.go <out> <in>...
package main

import (
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
)

const align = 14 // 16 KiB slices, what arm64 pages need

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: lipo <out> <in>...")
		os.Exit(2)
	}
	if err := lipo(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "lipo:", err)
		os.Exit(1)
	}
}

func lipo(out string, ins []string) error {
	type slice struct {
		cpu, sub uint32
		data     []byte
	}
	var slices []slice
	for _, in := range ins {
		f, err := macho.Open(in)
		if err != nil {
			return fmt.Errorf("%s: %w", in, err)
		}
		cpu, sub := uint32(f.Cpu), f.SubCpu&0x00ffffff
		f.Close()
		data, err := os.ReadFile(in)
		if err != nil {
			return err
		}
		slices = append(slices, slice{cpu, sub, data})
	}
	head := make([]byte, 8+20*len(slices))
	binary.BigEndian.PutUint32(head[0:], macho.MagicFat)
	binary.BigEndian.PutUint32(head[4:], uint32(len(slices)))
	off := uint32(len(head))
	var body []byte
	for i, s := range slices {
		pad := (1<<align - off%(1<<align)) % (1 << align)
		body = append(body, make([]byte, pad)...)
		off += pad
		e := head[8+20*i:]
		binary.BigEndian.PutUint32(e[0:], s.cpu)
		binary.BigEndian.PutUint32(e[4:], s.sub)
		binary.BigEndian.PutUint32(e[8:], off)
		binary.BigEndian.PutUint32(e[12:], uint32(len(s.data)))
		binary.BigEndian.PutUint32(e[16:], align)
		body = append(body, s.data...)
		off += uint32(len(s.data))
	}
	return os.WriteFile(out, append(head, body...), 0o755)
}
