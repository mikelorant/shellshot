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

// reportingProcess is a fakeProcess that can report its foreground state.
type reportingProcess struct {
	fakeProcess
	foreground bool
	err        error
}

func (r reportingProcess) ShellInForeground() (bool, error) { return r.foreground, r.err }

func TestTeeForwardsShellInForegroundToTheWrappedProcess(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		name string
		in   reportingProcess
		want bool
		err  error
	}{
		{"foreground", reportingProcess{foreground: true}, true, nil},
		{"background", reportingProcess{}, false, nil},
		{"error", reportingProcess{err: boom}, false, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := Tee(tc.in, io.Discard).(ForegroundReporter)
			if !ok {
				t.Fatal("Tee result is not a ForegroundReporter")
			}
			got, err := r.ShellInForeground()
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Errorf("ShellInForeground = (%v, %v), want (%v, %v)", got, err, tc.want, tc.err)
			}
		})
	}
}

func TestTeeShellInForegroundWithoutReporterReturnsErrNoForeground(t *testing.T) {
	r, ok := Tee(fakeProcess{}, io.Discard).(ForegroundReporter)
	if !ok {
		t.Fatal("Tee result is not a ForegroundReporter")
	}
	if got, err := r.ShellInForeground(); got || !errors.Is(err, ErrNoForeground) {
		t.Errorf("ShellInForeground = (%v, %v), want (false, ErrNoForeground)", got, err)
	}
}
