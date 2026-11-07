package recording

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

func testHeader() Header {
	return Header{
		Version: Version,
		Cols:    10,
		Rows:    2,
		Command: []string{"ls", "-l"},
		Started: time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC),
	}
}

func testFrame(at time.Duration, lines []string, cur snapshot.Cursor, title string) snapshot.Frame {
	return snapshot.Frame{
		At: at,
		Snapshot: snapshot.Snapshot{
			Cols: 10, Rows: 2, Lines: lines, Cursor: cur, Title: title,
		},
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		header Header
		frames []snapshot.Frame
	}{
		{
			name:   "no frames",
			header: testHeader(),
		},
		{
			name:   "single frame",
			header: testHeader(),
			frames: []snapshot.Frame{
				testFrame(0, []string{"hello", ""}, snapshot.Cursor{X: 5, Y: 0, Visible: true}, ""),
			},
		},
		{
			name:   "several frames with unicode, html and title",
			header: testHeader(),
			frames: []snapshot.Frame{
				testFrame(0, []string{"<a&b>", "héllo ✓"}, snapshot.Cursor{}, "t\"itle"),
				testFrame(1500*time.Millisecond, []string{"x", "y"}, snapshot.Cursor{X: 1, Y: 1, Visible: true}, ""),
			},
		},
		{
			name: "nil command",
			header: Header{
				Version: Version, Cols: 10, Rows: 2,
				Started: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, tt.header)
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}
			for _, f := range tt.frames {
				if err := w.WriteFrame(f); err != nil {
					t.Fatalf("WriteFrame: %v", err)
				}
			}

			r, err := NewReader(&buf)
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}
			got := r.Header()
			if got.Version != tt.header.Version || got.Cols != tt.header.Cols || got.Rows != tt.header.Rows ||
				!got.Started.Equal(tt.header.Started) || len(got.Command) != len(tt.header.Command) ||
				(len(got.Command) > 0 && !reflect.DeepEqual(got.Command, tt.header.Command)) {
				t.Errorf("Header() = %+v, want %+v", got, tt.header)
			}
			for i, want := range tt.frames {
				f, err := r.Next()
				if err != nil {
					t.Fatalf("Next %d: %v", i, err)
				}
				if f.At != want.At || !f.Snapshot.Equal(want.Snapshot) {
					t.Errorf("frame %d = %+v, want %+v", i, f, want)
				}
			}
			if _, err := r.Next(); err != io.EOF {
				t.Errorf("Next after last frame = %v, want io.EOF", err)
			}
		})
	}
}

func TestWriterFormat(t *testing.T) {
	var buf bytes.Buffer
	h := Header{Cols: 3, Rows: 1, Command: []string{"ls"}, Started: time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)}
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	f := snapshot.Frame{At: 1234 * time.Millisecond, Snapshot: snapshot.Snapshot{
		Cols: 3, Rows: 1, Lines: []string{"a<"}, Cursor: snapshot.Cursor{X: 2, Y: 0, Visible: true}, Title: "T",
	}}
	if err := w.WriteFrame(f); err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"cols":3,"rows":1,"command":["ls"],"started":"2024-05-06T07:08:09Z"}` + "\n" +
		`{"t":1234,"lines":["a<"],"cx":2,"cy":0,"cv":true,"title":"T"}` + "\n"
	if buf.String() != want {
		t.Errorf("output =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestNewWriterErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Header)
		wantErr error
	}{
		{"unknown version", func(h *Header) { h.Version = 2 }, ErrVersion},
		{"zero cols", func(h *Header) { h.Cols = 0 }, ErrMalformed},
		{"negative rows", func(h *Header) { h.Rows = -1 }, ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := testHeader()
			tt.mutate(&h)
			var buf bytes.Buffer
			if _, err := NewWriter(&buf, h); !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if buf.Len() != 0 {
				t.Errorf("wrote %q despite error", buf.String())
			}
		})
	}
}

func TestWriteFrameLineCountMismatch(t *testing.T) {
	w, err := NewWriter(io.Discard, testHeader())
	if err != nil {
		t.Fatal(err)
	}
	err = w.WriteFrame(testFrame(0, []string{"only one"}, snapshot.Cursor{}, ""))
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("err = %v, want ErrMalformed", err)
	}
}

var errWrite = errors.New("disk full")

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestWriterIOErrors(t *testing.T) {
	if _, err := NewWriter(failWriter{}, testHeader()); !errors.Is(err, errWrite) {
		t.Errorf("NewWriter err = %v, want wrapped errWrite", err)
	}
}

func TestNewReaderErrors(t *testing.T) {
	const okHeader = `{"version":1,"cols":10,"rows":2,"command":["ls"],"started":"2024-05-06T07:08:09Z"}`
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"empty input", "", ErrMalformed},
		{"not json", "hello\n", ErrMalformed},
		{"wrong field type", `{"version":"1"}` + "\n", ErrMalformed},
		{"unknown version", `{"version":99,"cols":10,"rows":2,"started":"2024-05-06T07:08:09Z"}` + "\n", ErrVersion},
		{"missing version", `{"cols":10,"rows":2}` + "\n", ErrVersion},
		{"zero size", `{"version":1,"cols":0,"rows":0}` + "\n", ErrMalformed},
		{"bad timestamp", `{"version":1,"cols":10,"rows":2,"started":"yesterday"}` + "\n", ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewReader(strings.NewReader(tt.input)); !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}

	t.Run("header without trailing newline", func(t *testing.T) {
		r, err := NewReader(strings.NewReader(okHeader))
		if err != nil {
			t.Fatalf("NewReader: %v", err)
		}
		if _, err := r.Next(); err != io.EOF {
			t.Errorf("Next = %v, want io.EOF", err)
		}
	})
}

func TestReaderNextErrors(t *testing.T) {
	const header = `{"version":1,"cols":10,"rows":2,"command":["ls"],"started":"2024-05-06T07:08:09Z"}` + "\n"
	tests := []struct {
		name  string
		frame string
	}{
		{"not json", "garbage\n"},
		{"blank line", "\n"},
		{"truncated json", `{"t":1,"lines":["a",` + "\n"},
		{"wrong type", `{"t":"soon","lines":["a","b"]}` + "\n"},
		{"negative time", `{"t":-1,"lines":["a","b"],"cx":0,"cy":0,"cv":false,"title":""}` + "\n"},
		{"too few lines", `{"t":1,"lines":["a"],"cx":0,"cy":0,"cv":false,"title":""}` + "\n"},
		{"missing lines", `{"t":1}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewReader(strings.NewReader(header + tt.frame))
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}
			if _, err := r.Next(); !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v, want ErrMalformed", err)
			}
		})
	}

	t.Run("last frame without trailing newline", func(t *testing.T) {
		in := header + `{"t":7,"lines":["a","b"],"cx":1,"cy":1,"cv":true,"title":"x"}`
		r, err := NewReader(strings.NewReader(in))
		if err != nil {
			t.Fatal(err)
		}
		f, err := r.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if f.At != 7*time.Millisecond || f.Snapshot.Cols != 10 || f.Snapshot.Rows != 2 {
			t.Errorf("frame = %+v", f)
		}
		if _, err := r.Next(); err != io.EOF {
			t.Errorf("Next = %v, want io.EOF", err)
		}
	})
}
