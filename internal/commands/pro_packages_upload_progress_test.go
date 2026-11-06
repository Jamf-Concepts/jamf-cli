// Copyright 2026, Jamf Software LLC

package commands

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func progressOver(total int, interactive bool, now func() time.Time) (*uploadProgress, *bytes.Buffer) {
	var out bytes.Buffer
	p := &uploadProgress{
		r: bytes.NewReader(make([]byte, total)), w: &out, total: int64(total),
		interactive: interactive, now: now, nextStep: 25,
	}
	return p, &out
}

func TestUploadProgress_PlainLogGetsOneLinePerQuarter(t *testing.T) {
	p, out := progressOver(1000, false, time.Now)
	buf := make([]byte, 10) // many small reads must not repeat a quarter
	for {
		if _, err := p.Read(buf); err == io.EOF {
			break
		}
	}
	p.finish()
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "  25%") || !strings.HasPrefix(lines[3], "  100%") {
		t.Fatalf("%q", out.String())
	}
	if strings.ContainsAny(out.String(), "\r\x1b") {
		t.Fatal("a log got terminal control characters")
	}
}

func TestUploadProgress_TerminalRedrawsInPlaceAndIsRateLimited(t *testing.T) {
	clock := time.Unix(0, 0)
	p, out := progressOver(1000, true, func() time.Time { return clock })
	buf := make([]byte, 100)
	for i := 0; i < 9; i++ { // 900 bytes with a frozen clock
		_, _ = p.Read(buf)
	}
	if n := strings.Count(out.String(), "\r"); n != 1 {
		t.Fatalf("%d redraws inside one interval, want 1: %q", n, out.String())
	}
	clock = clock.Add(redrawInterval)
	_, _ = p.Read(buf) // reaches 100%, always drawn
	if !strings.Contains(out.String(), "(100%)") {
		t.Fatalf("final state not drawn: %q", out.String())
	}
	p.finish()
	if !strings.HasSuffix(out.String(), "\r\x1b[K") {
		t.Fatalf("line not cleared: %q", out.String())
	}
}

func TestUploadProgress_SilentAndUnknownSizePrintNothing(t *testing.T) {
	for name, mk := range map[string]func() (*uploadProgress, *bytes.Buffer){
		"quiet": func() (*uploadProgress, *bytes.Buffer) {
			p, o := progressOver(100, false, time.Now)
			p.silent = true
			return p, o
		},
		"unknown size": func() (*uploadProgress, *bytes.Buffer) {
			p, o := progressOver(100, false, time.Now)
			p.total = 0
			return p, o
		},
		"unknown tty": func() (*uploadProgress, *bytes.Buffer) {
			p, o := progressOver(100, true, time.Now)
			p.total = 0
			return p, o
		},
	} {
		p, out := mk()
		_, _ = io.Copy(io.Discard, p)
		p.finish()
		if out.Len() != 0 {
			t.Errorf("%s printed %q", name, out.String())
		}
	}
}

func TestUploadProgress_SeekToStartResetsTheCount(t *testing.T) {
	p, out := progressOver(1000, false, time.Now)
	p.r = bytes.NewReader(make([]byte, 1000))
	_, _ = io.CopyN(io.Discard, p, 800)
	if _, err := p.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_, _ = io.Copy(io.Discard, p)
	// A retried body counts from 0 again, so its quarters print again.
	if n := strings.Count(out.String(), "%"); n != 4 {
		t.Fatalf("after a rewind: %d quarter lines, want 4: %q", n, out.String())
	}
}

func TestUploadProgress_CountIsHeldAtTheTotal(t *testing.T) {
	p, out := progressOver(100, false, time.Now)
	p.add(70)
	p.add(70) // the same part read again after a failed attempt
	if p.read != 100 || strings.Contains(out.String(), "140") || strings.Contains(out.String(), "(1") && strings.Contains(out.String(), "of 100 bytes") && p.read > 100 {
		t.Fatalf("read=%d out=%q", p.read, out.String())
	}
}

func TestProgressFile_KeepsTheFileSeekableAndSafeToReadConcurrently(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "p")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(make([]byte, 4000)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	p := &uploadProgress{r: f, w: &out, total: 4000, now: time.Now, nextStep: 25}
	var body io.Reader = progressFile{File: f, p: p}

	// The JCDS transfer sizes parts from Stat and reads them through Seek and ReadAt.
	if _, ok := body.(io.Seeker); !ok {
		t.Fatal("progressFile is not a Seeker")
	}
	if _, ok := body.(io.ReaderAt); !ok {
		t.Fatal("progressFile is not a ReaderAt")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(off int64) {
			defer wg.Done()
			_, _ = body.(io.ReaderAt).ReadAt(make([]byte, 1000), off)
		}(int64(i) * 1000)
	}
	wg.Wait()
	if p.read != 4000 {
		t.Fatalf("read = %d, want 4000", p.read)
	}
}
