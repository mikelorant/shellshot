package svg

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

func TestParseFrame(t *testing.T) {
	for in, want := range map[string]Frame{
		"first": {Kind: FrameFirst}, "last": {Kind: FrameLast},
		"1.5s": {Kind: FrameAt, At: 1500 * time.Millisecond}, "800ms": {Kind: FrameAt, At: 800 * time.Millisecond}, "0s": {Kind: FrameAt},
	} {
		if got, err := ParseFrame(in); err != nil || got != want {
			t.Errorf("ParseFrame(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "2", "soon", "-1s", "FIRST"} {
		if _, err := ParseFrame(in); err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("ParseFrame(%q) err = %v, want an error naming it", in, err)
		}
	}
}

func TestParseTextMode(t *testing.T) {
	if m, err := ParseTextMode("text"); err != nil || m != TextText || m.String() != "text" {
		t.Errorf("text: %v, %v", m, err)
	}
	if m, err := ParseTextMode("paths"); err != nil || m != TextPaths || m.String() != "paths" {
		t.Errorf("paths: %v, %v", m, err)
	}
	if _, err := ParseTextMode("woff2"); err == nil || !strings.Contains(err.Error(), "woff2") {
		t.Errorf("woff2: err = %v", err)
	}
}

func TestWriteStepsRejectsBadInput(t *testing.T) {
	_, g := newRenderer(t, raster.Options{})
	good := screen(t, 10, "ok")
	bad := snapshot.Snapshot{Cols: 10, Rows: 2, Lines: []string{"only one line"}}
	for name, tc := range map[string]struct {
		o     Options
		steps []timeline.Step
		want  string
	}{
		"no geometry": {Options{}, []timeline.Step{step(good, time.Second)}, "geometry"},
		"no steps":    {Options{Geometry: g}, nil, "no steps"},
		"invalid":     {Options{Geometry: g}, []timeline.Step{step(bad, time.Second)}, "step 0"},
		"wrong size":  {Options{Geometry: g}, []timeline.Step{step(good, time.Second), step(screen(t, 11, "x"), time.Second)}, "step 1"},
	} {
		var buf bytes.Buffer
		err := NewWriter(tc.o).WriteSteps(&buf, 10, 1, tc.steps)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, tc.want)
		}
		if buf.Len() != 0 {
			t.Errorf("%s: wrote %d bytes on error", name, buf.Len())
		}
	}
}

func TestPathsModeDefinesEachGlyphOnce(t *testing.T) {
	_, g := newRenderer(t, raster.Options{})
	s := screen(t, 20, "aaa "+esc+"1mabab"+reset, "世世 世")
	doc := write(t, Options{Geometry: g, Text: TextPaths, Frame: Frame{Kind: FrameLast}}, 20, 2, step(s, time.Second))
	// a (regular), a and b (bold), 世
	if n := strings.Count(doc, `<path id="g`); n != 4 {
		t.Errorf("%d glyph definitions, want 4\n%s", n, normalise(doc))
	}
	if strings.Contains(doc, "<text") {
		t.Error("paths mode wrote <text>")
	}
	if n := strings.Count(doc, `<use href="#g`); n != 3+4+3 {
		t.Errorf("%d glyph uses, want 10", n)
	}
}

func TestPathsModeLeavesAbsentGlyphsBlank(t *testing.T) {
	_, g := newRenderer(t, raster.Options{})
	s := screen(t, 10, "a\U0010FFFEb")
	doc := write(t, Options{Geometry: g, Text: TextPaths, Frame: Frame{Kind: FrameLast}}, 10, 1, step(s, time.Second))
	if n := strings.Count(doc, `<use `); n != 2 {
		t.Errorf("%d uses, want 2 (a and b)", n)
	}
}
