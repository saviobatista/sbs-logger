// Command resplit restores one SBS message per line in a log file written by
// sbs-logger before the newline fix, where each day's messages were
// concatenated on a single line ("...,0,0MSG,4,333,...").
//
// Usage:
//
//	resplit [-o output] [input]
//
// It reads input (or stdin), plain or gzip (detected by content: the old
// ".log.gz" files are plain text), and writes the messages, one per line
// terminated by "\n", to output (or stdout). Lines that are already split
// pass through unchanged, so a file that mixes both formats (the day of the
// deploy) is handled too. Run it on a copy, for example:
//
//	resplit sbs_2026-09-23.log.gz | gzip > sbs_2026-09-23.split.log.gz
//
// How it finds the boundaries: every SBS/BaseStation message starts with its
// type (MSG, SEL, ID, AIR, STA, CLK) followed by five fields (transmission
// type, session, aircraft, hex ident, flight) and the generated date
// (YYYY/MM/DD). That pattern cannot occur inside a message (the only free
// text field, the callsign, is not followed by a date five fields later),
// so a match marks the start of a message even when it is glued to the end
// of the previous one.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
)

var messageStart = regexp.MustCompile(`(?:MSG|SEL|ID|AIR|STA|CLK),[^,\r\n]*,[^,\r\n]*,[^,\r\n]*,[^,\r\n]*,[^,\r\n]*,\d{4}/\d{2}/\d{2},`)

// Split copies r to w with one SBS message per line and returns the number
// of lines written.
func Split(r io.Reader, w io.Writer) (int, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	bw := bufio.NewWriterSize(w, 64*1024)
	chunk := make([]byte, 64*1024)
	var buf []byte
	lines := 0

	emit := func(b []byte) error {
		b = bytes.TrimSpace(b)
		if len(b) == 0 {
			return nil
		}
		lines++
		if _, err := bw.Write(b); err != nil {
			return err
		}
		return bw.WriteByte('\n')
	}

	// flush writes every complete message in buf and keeps the last one,
	// which may continue in the next chunk.
	flush := func(final bool) error {
		for {
			// A newline always ends a message.
			if i := bytes.IndexByte(buf, '\n'); i >= 0 {
				if err := emitSegment(buf[:i], emit); err != nil {
					return err
				}
				buf = buf[i+1:]
				continue
			}
			break
		}
		// No newline left: split at message starts, keep the tail.
		starts := messageStart.FindAllIndex(buf, -1)
		cut := 0
		for _, s := range starts {
			if s[0] > cut {
				if err := emit(buf[cut:s[0]]); err != nil {
					return err
				}
				cut = s[0]
			}
		}
		if final {
			if err := emit(buf[cut:]); err != nil {
				return err
			}
			buf = nil
			return nil
		}
		buf = append([]byte(nil), buf[cut:]...)
		return nil
	}

	for {
		n, err := br.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if err == io.EOF {
			if ferr := flush(true); ferr != nil {
				return lines, ferr
			}
			return lines, bw.Flush()
		}
		if err != nil {
			return lines, err
		}
		if ferr := flush(false); ferr != nil {
			return lines, ferr
		}
	}
}

// emitSegment emits a newline-terminated segment, splitting it at message
// starts in case it holds several concatenated messages.
func emitSegment(seg []byte, emit func([]byte) error) error {
	cut := 0
	for _, s := range messageStart.FindAllIndex(seg, -1) {
		if s[0] > cut {
			if err := emit(seg[cut:s[0]]); err != nil {
				return err
			}
			cut = s[0]
		}
	}
	return emit(seg[cut:])
}

// openInput returns a reader for path ("" or "-" is stdin), transparently
// decompressing gzip content.
func openInput(path string) (io.Reader, func() error, error) {
	var f *os.File
	if path == "" || path == "-" {
		f = os.Stdin
	} else {
		var err error
		//nolint:gosec // the path is the operator's input
		f, err = os.Open(path)
		if err != nil {
			return nil, nil, err
		}
	}
	br := bufio.NewReader(f)
	magic, _ := br.Peek(2)
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			_ = f.Close()
			return nil, nil, err
		}
		return zr, f.Close, nil
	}
	return br, f.Close, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("resplit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "output file (default stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: resplit [-o output] [input]")
		return 2
	}

	in, closeIn, err := openInput(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "resplit: %v\n", err)
		return 1
	}
	defer func() { _ = closeIn() }()

	w := stdout
	if *out != "" {
		//nolint:gosec // the path is the operator's input
		f, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			fmt.Fprintf(stderr, "resplit: %v\n", err)
			return 1
		}
		defer func() { _ = f.Close() }()
		w = f
	}

	n, err := Split(in, w)
	if err != nil {
		fmt.Fprintf(stderr, "resplit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "resplit: %d messages\n", n)
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
