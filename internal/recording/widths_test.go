package recording

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// widthSnapshot has a CJK line and a combining-mark line.
func widthSnapshot() snapshot.Snapshot {
	return snapshot.Snapshot{
		Cols: 10, Rows: 3,
		Lines: []string{"世界ab", "e\u0301x", ""},
		Widths: [][]snapshot.Wide{
			{{Rune: 0, Cells: 2}, {Rune: 1, Cells: 2}},
			{{Rune: 1, Cells: 0}},
			nil,
		},
	}
}

func readOneFrame(t *testing.T, data string) (snapshot.Frame, error) {
	t.Helper()
	r, err := NewReader(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return r.Next()
}

func TestWidthsRoundTrip(t *testing.T) {
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	withSpans := widthSnapshot()
	withSpans.Spans = [][]snapshot.Span{{{Start: 0, End: 2, Style: red}}, nil, nil}
	tests := map[string]snapshot.Snapshot{
		"widths only":      widthSnapshot(),
		"spans and widths": withSpans,
	}
	for name, snap := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, styledHeader())
			if err != nil {
				t.Fatal(err)
			}
			want := snapshot.Frame{At: 40 * time.Millisecond, Snapshot: snap}
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
			if len(got.Snapshot.Widths) != 3 || len(got.Snapshot.Widths[0]) != 2 || got.Snapshot.Widths[1][0].Cells != 0 {
				t.Errorf("Widths = %v, want %v", got.Snapshot.Widths, snap.Widths)
			}
			if (snap.Spans != nil) != (got.Snapshot.Spans != nil) {
				t.Errorf("Spans = %v, want %v", got.Snapshot.Spans, snap.Spans)
			}
		})
	}
}

func TestWidthsFrameExactBytes(t *testing.T) {
	var buf bytes.Buffer
	h := Header{Cols: 5, Rows: 3, Command: []string{"ls"}, Started: time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)}
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	f := snapshot.Frame{Snapshot: snapshot.Snapshot{
		Cols: 5, Rows: 3, Lines: []string{"世界a", "e\u0301", ""},
		Widths: [][]snapshot.Wide{{{Rune: 0, Cells: 2}, {Rune: 1, Cells: 2}}, {{Rune: 1, Cells: 0}}, nil},
	}}
	if err := w.WriteFrame(f); err != nil {
		t.Fatal(err)
	}
	want := `{"version":2,"cols":5,"rows":3,"command":["ls"],"started":"2024-05-06T07:08:09Z"}` + "\n" +
		`{"t":0,"lines":["世界a","e` + "\u0301" + `",""],"cx":0,"cy":0,"cv":false,"title":"","widths":[[[0,2],[1,2]],[[1,0]],[]]}` + "\n"
	if buf.String() != want {
		t.Errorf("output =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestFrameWithoutWidthsOmitsKey(t *testing.T) {
	frames := map[string]snapshot.Snapshot{
		"nil widths":       {Cols: 10, Rows: 2, Lines: []string{"a", "b"}},
		"all-empty widths": {Cols: 10, Rows: 2, Lines: []string{"a", "b"}, Widths: [][]snapshot.Wide{nil, {}}},
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
			if strings.Contains(buf.String(), "widths") {
				t.Errorf("output contains widths key: %s", buf.String())
			}
			got, err := readOneFrame(t, buf.String())
			if err != nil {
				t.Fatal(err)
			}
			if got.Snapshot.Widths != nil {
				t.Errorf("Widths = %v, want nil", got.Snapshot.Widths)
			}
		})
	}
}

func TestReadVersion2WithoutWidths(t *testing.T) {
	in := `{"version":2,"cols":10,"rows":2,"command":["ls"],"started":"2024-05-06T07:08:09Z"}` + "\n" +
		`{"t":0,"lines":["世界","b"],"cx":0,"cy":0,"cv":false,"title":"","spans":[[[0,1,"31"]],[]]}` + "\n"
	f, err := readOneFrame(t, in)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if f.Snapshot.Widths != nil || f.Snapshot.Spans == nil {
		t.Errorf("Widths = %v, Spans = %v; want nil widths and spans kept", f.Snapshot.Widths, f.Snapshot.Spans)
	}
}

func TestReadMalformedWidths(t *testing.T) {
	const header = `{"version":2,"cols":10,"rows":2,"command":["ls"],"started":"2024-05-06T07:08:09Z"}` + "\n"
	frame := func(widths string) string {
		return `{"t":0,"lines":["世界a",""],"cx":0,"cy":0,"cv":false,"title":"","widths":` + widths + `}` + "\n"
	}
	tests := []struct {
		name   string
		widths string
	}{
		{"too few rows", `[[[0,2]]]`},
		{"too many rows", `[[],[],[]]`},
		{"empty widths array", `[]`},
		{"pair too short", `[[[0]],[]]`},
		{"pair too long", `[[[0,2,1]],[]]`},
		{"non-numeric rune", `[[["a",2]],[]]`},
		{"non-numeric cells", `[[[0,"2"]],[]]`},
		{"fractional cells", `[[[0,2.5]],[]]`},
		{"cells one", `[[[0,1]],[]]`},
		{"cells three", `[[[0,3]],[]]`},
		{"negative cells", `[[[0,-2]],[]]`},
		{"cells overflow", `[[[0,258]],[]]`},
		{"unsorted", `[[[1,2],[0,2]],[]]`},
		{"duplicate", `[[[0,2],[0,2]],[]]`},
		{"rune out of range", `[[[3,2]],[]]`},
		{"negative rune", `[[[-1,2]],[]]`},
		{"entry on empty line", `[[],[[0,2]]]`},
		{"row not an array", `[1,[]]`},
		{"not an array", `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := readOneFrame(t, header+frame(tt.widths)); !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v, want ErrMalformed", err)
			}
		})
	}
}

func TestWriteFrameRejectsInvalidWidths(t *testing.T) {
	tests := []struct {
		name   string
		widths [][]snapshot.Wide
	}{
		{"wrong row count", [][]snapshot.Wide{{{Rune: 0, Cells: 2}}}},
		{"cells one", [][]snapshot.Wide{{{Rune: 0, Cells: 1}}, nil}},
		{"cells three", [][]snapshot.Wide{{{Rune: 0, Cells: 3}}, nil}},
		{"unsorted", [][]snapshot.Wide{{{Rune: 1, Cells: 2}, {Rune: 0, Cells: 2}}, nil}},
		{"out of range", [][]snapshot.Wide{{{Rune: 3, Cells: 2}}, nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, testHeader())
			if err != nil {
				t.Fatal(err)
			}
			headerLen := buf.Len()
			f := testFrame(0, []string{"世界a", ""}, snapshot.Cursor{}, "")
			f.Snapshot.Widths = tt.widths
			if err := w.WriteFrame(f); !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v, want ErrMalformed", err)
			}
			if buf.Len() != headerLen {
				t.Errorf("wrote frame despite error: %q", buf.String())
			}
		})
	}
}

func TestVersion1FrameIgnoresWidthsKey(t *testing.T) {
	in := `{"version":1,"cols":10,"rows":1,"started":"2024-05-06T07:08:09Z"}` + "\n" +
		`{"t":0,"lines":["世"],"cx":0,"cy":0,"cv":false,"title":"","widths":[[[0,2]]]}` + "\n"
	f, err := readOneFrame(t, in)
	if err != nil {
		t.Fatal(err)
	}
	if f.Snapshot.Widths != nil {
		t.Errorf("Widths = %v, want nil for version 1", f.Snapshot.Widths)
	}
}
