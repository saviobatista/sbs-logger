package main

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real lines from sbs_2026-09-24.log (concatenated by the old logger).
var sample = []string{
	"MSG,5,333,8006,E49BFC,8106,2026/09/24,00:00:00.075,2026/09/24,00:00:00.075,,14875,,,,,,,0,,0,0",
	"MSG,4,333,7933,E49329,8033,2026/09/24,00:00:00.077,2026/09/24,00:00:00.077,,,233.0,272.5,,,64,,,,,",
	"AIR,,333,8022,E49C05,8122,2026/09/24,00:00:00.083,2026/09/24,00:00:00.083",
	"MSG,1,333,8022,E49C05,8122,2026/09/24,00:00:00.090,2026/09/24,00:00:00.090,AIRMSG1 ,,,,,,,,,,,0",
	"ID,,333,8022,E49C05,8122,2026/09/24,00:00:00.091,2026/09/24,00:00:00.091,GOLID",
	"STA,,333,8022,E49C05,8122,2026/09/24,00:00:00.092,2026/09/24,00:00:00.092,RM",
	"MSG,3,333,2524,E48053,2624,2026/09/24,00:00:00.018,2026/09/24,00:00:00.018,,8500,,,-23.22957,-46.49073,,,0,0,0,0",
}

func split(t *testing.T, in string) []string {
	t.Helper()
	var out bytes.Buffer
	n, err := Split(strings.NewReader(in), &out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if n != len(lines) {
		t.Errorf("reported %d lines, wrote %d", n, len(lines))
	}
	if !strings.HasSuffix(out.String(), "\n") {
		t.Error("output does not end with a newline")
	}
	return lines
}

func TestSplitConcatenated(t *testing.T) {
	got := split(t, strings.Join(sample, ""))
	if strings.Join(got, "\n") != strings.Join(sample, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(sample, "\n"))
	}
}

func TestSplitMixedAndAlreadySplit(t *testing.T) {
	// The deploy day: concatenated part, then proper lines (with \r\n too).
	in := strings.Join(sample[:3], "") + strings.Join(sample[3:], "\r\n") + "\r\n"
	if got := split(t, in); strings.Join(got, "\n") != strings.Join(sample, "\n") {
		t.Errorf("mixed input: got\n%s", strings.Join(got, "\n"))
	}
	already := strings.Join(sample, "\n") + "\n"
	if got := split(t, already); strings.Join(got, "\n")+"\n" != already {
		t.Error("already split input changed")
	}
}

// Messages straddling the internal read chunk boundary.
func TestSplitLargeInput(t *testing.T) {
	var want []string
	for len(strings.Join(want, "")) < 300*1024 {
		want = append(want, sample...)
	}
	got := split(t, strings.Join(want, ""))
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRunGzipAndPlain(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "sbs_2026-09-23.log.gz") // plain text despite the name, like the old files
	if err := os.WriteFile(plain, []byte(strings.Join(sample, "")), 0o600); err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(strings.Join(sample, "")))
	_ = zw.Close()
	real := filepath.Join(dir, "real.log.gz")
	if err := os.WriteFile(real, gz.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, in := range []string{plain, real} {
		out := filepath.Join(dir, filepath.Base(in)+".out")
		var stderr bytes.Buffer
		if code := run([]string{"-o", out, in}, &bytes.Buffer{}, &stderr); code != 0 {
			t.Fatalf("run(%s) = %d: %s", in, code, stderr.String())
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != strings.Join(sample, "\n")+"\n" {
			t.Errorf("%s: output\n%s", in, data)
		}
		// Never overwrites an existing file.
		if code := run([]string{"-o", out, in}, &bytes.Buffer{}, &bytes.Buffer{}); code == 0 {
			t.Error("overwrote an existing output file")
		}
	}
}
