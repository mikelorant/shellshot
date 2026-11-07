package recording

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

func styledSnapshot() snapshot.Snapshot {
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	rgb := snapshot.Style{
		Fg: snapshot.Color{Kind: snapshot.ColorRGB, R: 1, G: 2, B: 3},
		Bg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 208},
	}
	attrs := snapshot.Style{Attrs: snapshot.Bold | snapshot.Underline | snapshot.Strike}
	return snapshot.Snapshot{
		Cols: 10, Rows: 3,
		Lines: []string{"héllo wor", "", "x"},
		Spans: [][]snapshot.Span{
			{{Start: 0, End: 3, Style: red}, {Start: 4, End: 9, Style: rgb}},
			nil,
			{{Start: 0, End: 1, Style: attrs}},
		},
		Cursor: snapshot.Cursor{X: 1, Y: 2, Visible: true},
	}
}

func styledHeader() Header {
	h := testHeader()
	h.Rows = 3
	return h
}

func TestStyledRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, styledHeader())
	if err != nil {
		t.Fatal(err)
	}
	want := snapshot.Frame{At: 40 * time.Millisecond, Snapshot: styledSnapshot()}
	if err := w.WriteFrame(want); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	r, err := NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.At != want.At || !got.Snapshot.Equal(want.Snapshot) {
		t.Errorf("frame = %+v, want %+v", got, want)
	}
	if len(got.Snapshot.Spans) != 3 {
		t.Errorf("Spans has %d rows, want 3", len(got.Snapshot.Spans))
	}
}

func TestStyledFrameExactBytes(t *testing.T) {
	var buf bytes.Buffer
	h := Header{Cols: 5, Rows: 3, Command: []string{"ls"}, Started: time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)}
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	f := snapshot.Frame{Snapshot: snapshot.Snapshot{
		Cols: 5, Rows: 3, Lines: []string{"abcd", "", ""},
		Spans: [][]snapshot.Span{{{Start: 0, End: 3, Style: red}}, nil, nil},
	}}
	if err := w.WriteFrame(f); err != nil {
		t.Fatal(err)
	}
	want := `{"version":2,"cols":5,"rows":3,"command":["ls"],"started":"2024-05-06T07:08:09Z"}` + "\n" +
		`{"t":0,"lines":["abcd","",""],"cx":0,"cy":0,"cv":false,"title":"","spans":[[[0,3,"31"]],[],[]]}` + "\n"
	if buf.String() != want {
		t.Errorf("output =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestFrameWithoutSpansOmitsKey(t *testing.T) {
	frames := map[string]snapshot.Snapshot{
		"nil spans":       {Cols: 10, Rows: 2, Lines: []string{"a", "b"}},
		"all-empty spans": {Cols: 10, Rows: 2, Lines: []string{"a", "b"}, Spans: [][]snapshot.Span{nil, {}}},
	}
	for name, snap := range frames {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, testHeader())
			if err != nil {
				t.Fatal(err)
			}
			if err := w.WriteFrame(snapshot.Frame{Snapshot: snap}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(buf.String(), "spans") {
				t.Errorf("output contains spans key: %s", buf.String())
			}
			r, err := NewReader(&buf)
			if err != nil {
				t.Fatal(err)
			}
			got, err := r.Next()
			if err != nil {
				t.Fatal(err)
			}
			if got.Snapshot.Spans != nil {
				t.Errorf("Spans = %v, want nil", got.Snapshot.Spans)
			}
		})
	}
}

func TestReadVersion1(t *testing.T) {
	in := `{"version":1,"cols":10,"rows":2,"command":["ls"],"started":"2024-05-06T07:08:09Z"}` + "\n" +
		`{"t":5,"lines":["a","b"],"cx":1,"cy":0,"cv":true,"title":"x"}` + "\n"
	r, err := NewReader(strings.NewReader(in))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if r.Header().Version != 1 {
		t.Errorf("Version = %d, want 1", r.Header().Version)
	}
	f, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := snapshot.Snapshot{
		Cols: 10, Rows: 2, Lines: []string{"a", "b"},
		Cursor: snapshot.Cursor{X: 1, Visible: true}, Title: "x",
	}
	if !f.Snapshot.Equal(want) || f.Snapshot.Spans != nil || f.At != 5*time.Millisecond {
		t.Errorf("frame = %+v, want %+v", f, want)
	}
	if _, err := r.Next(); err != io.EOF {
		t.Errorf("Next = %v, want io.EOF", err)
	}
}

func TestReadUnknownVersion(t *testing.T) {
	for _, v := range []string{"0", "3", "99"} {
		in := `{"version":` + v + `,"cols":10,"rows":2,"started":"2024-05-06T07:08:09Z"}` + "\n"
		if _, err := NewReader(strings.NewReader(in)); !errors.Is(err, ErrVersion) {
			t.Errorf("version %s: err = %v, want ErrVersion", v, err)
		}
	}
}

func TestReadMalformedSpans(t *testing.T) {
	const header = `{"version":2,"cols":10,"rows":2,"command":["ls"],"started":"2024-05-06T07:08:09Z"}` + "\n"
	frame := func(spans string) string {
		return `{"t":0,"lines":["abc",""],"cx":0,"cy":0,"cv":false,"title":"","spans":` + spans + `}` + "\n"
	}
	tests := []struct {
		name  string
		spans string
	}{
		{"bad SGR", `[[[0,1,"bogus"]],[]]`},
		{"zero style", `[[[0,1,""]],[]]`},
		{"range beyond line", `[[[0,4,"31"]],[]]`},
		{"span on empty line", `[[],[[0,1,"31"]]]`},
		{"negative start", `[[[-1,1,"31"]],[]]`},
		{"empty range", `[[[1,1,"31"]],[]]`},
		{"too few rows", `[[[0,1,"31"]]]`},
		{"too many rows", `[[],[],[]]`},
		{"empty spans array", `[]`},
		{"unsorted", `[[[2,3,"31"],[0,1,"32"]],[]]`},
		{"overlapping", `[[[0,2,"31"],[1,3,"32"]],[]]`},
		{"wrong triple length", `[[[0,1]],[]]`},
		{"non-numeric start", `[[["a",1,"31"]],[]]`},
		{"non-string style", `[[[0,1,31]],[]]`},
		{"not an array", `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewReader(strings.NewReader(header + frame(tt.spans)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.Next(); !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v, want ErrMalformed", err)
			}
		})
	}
}

func TestWriteFrameRejectsInvalidSpans(t *testing.T) {
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	tests := []struct {
		name  string
		spans [][]snapshot.Span
	}{
		{"range beyond line", [][]snapshot.Span{{{Start: 0, End: 5, Style: red}}, nil}},
		{"zero style", [][]snapshot.Span{{{Start: 0, End: 1}}, nil}},
		{"unsorted", [][]snapshot.Span{{{Start: 2, End: 3, Style: red}, {Start: 0, End: 1, Style: red}}, nil}},
		{"wrong row count", [][]snapshot.Span{{{Start: 0, End: 1, Style: red}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, testHeader())
			if err != nil {
				t.Fatal(err)
			}
			headerLen := buf.Len()
			f := testFrame(0, []string{"abc", "d"}, snapshot.Cursor{}, "")
			f.Snapshot.Spans = tt.spans
			if err := w.WriteFrame(f); !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v, want ErrMalformed", err)
			}
			if buf.Len() != headerLen {
				t.Errorf("wrote frame despite error: %q", buf.String())
			}
		})
	}
}

func TestVersion1FrameIgnoresSpansKey(t *testing.T) {
	in := `{"version":1,"cols":10,"rows":1,"started":"2024-05-06T07:08:09Z"}` + "\n" +
		`{"t":0,"lines":["a"],"cx":0,"cy":0,"cv":false,"title":"","spans":[[[0,1,"31"]]]}` + "\n"
	r, err := NewReader(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	f, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if f.Snapshot.Spans != nil {
		t.Errorf("Spans = %v, want nil for version 1", f.Snapshot.Spans)
	}
}
