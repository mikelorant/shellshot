package live

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeProcess struct {
	io.Reader
	io.Writer
}

func (fakeProcess) Resize(cols, rows int) error { return nil }
func (fakeProcess) Wait() (int, error)          { return 0, nil }
func (fakeProcess) Close() error                { return nil }

type failingWriter struct{ calls int }

func (f *failingWriter) Write(b []byte) (int, error) {
	f.calls++
	return 0, errors.New("boom")
}

func TestTeeCopiesBytesToOutAndReturnsThem(t *testing.T) {
	var out bytes.Buffer
	p := Tee(fakeProcess{Reader: strings.NewReader("hello")}, &out)

	buf := make([]byte, 16)
	n, err := p.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("Read = %q, %v; want hello, nil", buf[:n], err)
	}
	if out.String() != "hello" {
		t.Fatalf("out = %q; want hello", out.String())
	}
	if _, err := p.Read(buf); err != io.EOF {
		t.Fatalf("second Read err = %v; want io.EOF", err)
	}
}

func TestTeeOutWriteErrorDoesNotAffectReadsAndStopsOutWrites(t *testing.T) {
	out := &failingWriter{}
	p := Tee(fakeProcess{Reader: strings.NewReader("abcdef")}, out)

	buf := make([]byte, 3)
	var got []byte
	for {
		n, err := p.Read(buf)
		got = append(got, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read err = %v", err)
		}
	}
	if string(got) != "abcdef" {
		t.Fatalf("read %q; want abcdef", got)
	}
	if out.calls != 1 {
		t.Fatalf("out writes = %d; want 1", out.calls)
	}
}

func TestTeeDelegatesWrite(t *testing.T) {
	var in bytes.Buffer
	p := Tee(fakeProcess{Reader: strings.NewReader(""), Writer: &in}, io.Discard)
	if _, err := p.Write([]byte("x")); err != nil || in.String() != "x" {
		t.Fatalf("Write -> %q, %v", in.String(), err)
	}
}
