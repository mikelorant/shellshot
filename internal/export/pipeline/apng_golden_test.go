package pipeline

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// patternRenderer draws a 16x8 image whose top-left pixel run is set by the
// first byte of the first line, over a fixed gradient, so consecutive screens
// differ in a small region and APNG delta frames apply.
type patternRenderer struct{}

func (patternRenderer) Size(cols, rows int) (int, int) { return cols * 4, rows * 8 }

func (patternRenderer) Render(s snapshot.Snapshot) (*image.RGBA, error) {
	w, h := s.Cols*4, s.Rows*8
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			o := img.PixOffset(x, y)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = byte(x*9), byte(y*17), 0x40, 0xff
		}
	}
	g := s.Lines[0][0]
	for y := range 3 {
		for x := range 5 {
			o := img.PixOffset(x, y)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2] = g, g, g
		}
	}
	return img, nil
}

// goldenExport exports a fixed recording with the default sink: changing
// screens, a repeated screen and a final hold longer than one APNG frame.
func goldenExport(t *testing.T) []byte {
	t.Helper()
	src := &sliceSource{frames: []snapshot.Frame{
		frame(0, "a"), frame(100*time.Millisecond, "b"), frame(300*time.Millisecond, "b"),
		frame(450*time.Millisecond, "c"), frame(900*time.Millisecond, "a"),
	}}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, patternRenderer{}, &buf, Options{
		Timeline:    timeline.Options{Hold: 70 * time.Second},
		Loops:       3,
		Compression: png.BestCompression,
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The default APNG output must stay byte-identical to what Export wrote
// before the sink refactor (testdata/apng_golden.apng, produced from it).
func TestExportAPNGBytesMatchGolden(t *testing.T) {
	want, err := os.ReadFile("testdata/apng_golden.apng")
	if err != nil {
		t.Fatal(err)
	}
	if got := goldenExport(t); !bytes.Equal(got, want) {
		t.Errorf("APNG output changed: got %d bytes, want %d", len(got), len(want))
	}
}
