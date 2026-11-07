package svg

import (
	"bytes"
	"encoding/xml"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

var (
	fontsOnce sync.Once
	fontSet   *font.Set
	fontsErr  error
)

// testFonts returns the shared embedded font set.
func testFonts(t testing.TB) *font.Set {
	t.Helper()
	fontsOnce.Do(func() { fontSet, fontsErr = font.Default() })
	if fontsErr != nil {
		t.Fatal(fontsErr)
	}
	return fontSet
}

// newRenderer returns the raster renderer for o and the Geometry built on it.
func newRenderer(t testing.TB, o raster.Options) (*raster.Renderer, Geometry) {
	t.Helper()
	f := testFonts(t)
	r, err := raster.New(f, o)
	if err != nil {
		t.Fatal(err)
	}
	return r, NewGeometry(r, f)
}

func testGeometry(t testing.TB, o raster.Options) Geometry {
	_, g := newRenderer(t, o)
	return g
}

// cellsOf is the terminal width of r: 2 for East Asian wide and emoji runes,
// 0 for combining marks and joiners, 1 otherwise.
func cellsOf(r rune) uint8 {
	switch {
	case r == 0x200D || r >= 0xFE00 && r <= 0xFE0F || unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	case r >= 0x1100 && r <= 0x115F, r >= 0x2E80 && r <= 0xA4CF, r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF, r >= 0xFF00 && r <= 0xFF60, r >= 0x1F300 && r <= 0x1FAFF:
		return 2
	}
	return 1
}

// screen builds a snapshot from lines that may contain SGR sequences
// (ESC [ ... m). The cursor is hidden unless set by the caller.
func screen(t testing.TB, cols int, lines ...string) snapshot.Snapshot {
	t.Helper()
	s := snapshot.Snapshot{Cols: cols, Rows: len(lines)}
	sgr := regexp.MustCompile("\x1b\\[([0-9;]*)m")
	var anyStyle, anyWide bool
	for _, raw := range lines {
		var (
			plain  strings.Builder
			spans  []snapshot.Span
			wides  []snapshot.Wide
			cur    snapshot.Style
			start  int
			runes  int
			closed = func() {
				if !cur.IsZero() && runes > start {
					spans = append(spans, snapshot.Span{Start: start, End: runes, Style: cur})
				}
			}
		)
		for rest := raw; rest != ""; {
			if loc := sgr.FindStringSubmatchIndex(rest); loc != nil && loc[0] == 0 {
				closed()
				st, err := snapshot.ParseSGR(rest[loc[2]:loc[3]])
				if err != nil {
					t.Fatal(err)
				}
				cur, start = st, runes
				rest = rest[loc[1]:]
				continue
			}
			r := []rune(rest)[0]
			rest = rest[len(string(r)):]
			plain.WriteRune(r)
			if w := cellsOf(r); w != 1 {
				wides = append(wides, snapshot.Wide{Rune: runes, Cells: w})
				anyWide = true
			}
			runes++
		}
		closed()
		s.Lines = append(s.Lines, strings.TrimRight(plain.String(), " "))
		s.Spans = append(s.Spans, spans)
		s.Widths = append(s.Widths, wides)
		anyStyle = anyStyle || len(spans) > 0
	}
	if !anyStyle {
		s.Spans = nil
	}
	if !anyWide {
		s.Widths = nil
	}
	// Lines were right-trimmed: drop spans and widths past the trimmed end.
	for y, l := range s.Lines {
		n := len([]rune(l))
		if y < len(s.Spans) {
			s.Spans[y] = clipSpans(s.Spans[y], n)
		}
		if y < len(s.Widths) {
			s.Widths[y] = clipWides(s.Widths[y], n)
		}
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func clipSpans(spans []snapshot.Span, n int) []snapshot.Span {
	var out []snapshot.Span
	for _, sp := range spans {
		if sp.Start >= n {
			break
		}
		sp.End = min(sp.End, n)
		out = append(out, sp)
	}
	return out
}

func clipWides(wides []snapshot.Wide, n int) []snapshot.Wide {
	var out []snapshot.Wide
	for _, w := range wides {
		if w.Rune < n {
			out = append(out, w)
		}
	}
	return out
}

func withCursor(s snapshot.Snapshot, x, y int) snapshot.Snapshot {
	s.Cursor = snapshot.Cursor{X: x, Y: y, Visible: true}
	return s
}

func step(s snapshot.Snapshot, d time.Duration) timeline.Step {
	return timeline.Step{Snapshot: s, Delay: d}
}

// write runs a Writer over steps and returns the document.
func write(t testing.TB, o Options, cols, rows int, steps ...timeline.Step) string {
	t.Helper()
	var buf bytes.Buffer
	if err := NewWriter(o).WriteSteps(&buf, cols, rows, steps); err != nil {
		t.Fatal(err)
	}
	checkDocument(t, buf.String())
	return buf.String()
}

// checkDocument fails unless doc is well-formed XML with an svg root, has no
// script, no event handler, and only #fragment and data: references; every
// #fragment must name an id of the document.
func checkDocument(t testing.TB, doc string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(doc))
	ids := map[string]bool{}
	var refs []string
	first := true
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("not well-formed XML: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if first {
			first = false
			if se.Name.Local != "svg" || se.Name.Space != "http://www.w3.org/2000/svg" {
				t.Fatalf("root element is %v, want svg in the SVG namespace", se.Name)
			}
		}
		if se.Name.Local == "script" || se.Name.Local == "foreignObject" {
			t.Fatalf("document has <%s>", se.Name.Local)
		}
		for _, a := range se.Attr {
			switch {
			case strings.HasPrefix(a.Name.Local, "on"):
				t.Fatalf("document has event handler %s", a.Name.Local)
			case a.Name.Local == "id":
				ids[a.Value] = true
			case a.Name.Local == "href":
				if strings.HasPrefix(a.Value, "#") {
					refs = append(refs, a.Value[1:])
				} else if !strings.HasPrefix(a.Value, "data:") {
					t.Fatalf("external reference %q", a.Value)
				}
			}
		}
	}
	for _, r := range refs {
		if !ids[r] {
			t.Fatalf("href #%s has no element with that id", r)
		}
	}
	if regexp.MustCompile(`url\(\s*['"]?http`).MatchString(doc) {
		t.Fatal("document has an external url()")
	}
}

var dataURI = regexp.MustCompile(`data:image/png;base64,[A-Za-z0-9+/=]+`)

// normalise puts every element on its own line and replaces the image data
// with an ellipsis, so goldens do not depend on the emoji artwork.
func normalise(doc string) string {
	doc = dataURI.ReplaceAllString(doc, "data:image/png;base64,...")
	doc = strings.ReplaceAll(doc, "><", ">\n<")
	doc = strings.ReplaceAll(doc, "}@keyframes", "}\n@keyframes")
	doc = strings.ReplaceAll(doc, "}.a", "}\n.a")
	return doc
}

// golden compares got with testdata/name, or rewrites it under -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the golden file (go test -update rewrites it); got:\n%s", name, got)
	}
}

type timelineStep struct {
	text  string
	delay time.Duration
}

// stepsOf makes a one-row 10 column step for each text.
func stepsOf(t testing.TB, in []timelineStep) []timeline.Step {
	var out []timeline.Step
	for _, x := range in {
		out = append(out, step(screen(t, 10, x.text), x.delay))
	}
	return out
}
