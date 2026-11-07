package raster

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

var update = flag.Bool("update", false, "regenerate golden images in testdata/")

// Golden comparison tolerance: anti-aliasing can differ slightly across
// architectures (e.g. fused multiply-add in the glyph rasteriser).
const (
	goldenMaxDiffFraction = 0.005
	goldenMaxChannelDiff  = 2
)

func compareGolden(t *testing.T, name string, got *image.RGBA) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, got); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open golden (regenerate with: go test -run Golden -update ./internal/render/raster): %v", err)
	}
	defer f.Close()
	want, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode golden %s: %v", path, err)
	}
	if want.Bounds() != got.Bounds() {
		t.Fatalf("golden bounds %v, got %v", want.Bounds(), got.Bounds())
	}
	if msg, ok := imagesClose(want, got); !ok {
		t.Error(msg)
	}
}

// imagesClose reports whether at most goldenMaxDiffFraction of the pixels
// differ, where a pixel differs if any channel is off by more than
// goldenMaxChannelDiff.
func imagesClose(want, got image.Image) (string, bool) {
	b := got.Bounds()
	bad := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			wr, wg, wb, wa := want.At(x, y).RGBA()
			gr, gg, gb, ga := got.At(x, y).RGBA()
			for _, d := range [4]int{int(wr>>8) - int(gr>>8), int(wg>>8) - int(gg>>8), int(wb>>8) - int(gb>>8), int(wa>>8) - int(ga>>8)} {
				if d > goldenMaxChannelDiff || d < -goldenMaxChannelDiff {
					bad++
					break
				}
			}
		}
	}
	total := b.Dx() * b.Dy()
	if float64(bad) > goldenMaxDiffFraction*float64(total) {
		return fmt.Sprintf("%d of %d pixels differ (limit %.1f%%)", bad, total, goldenMaxDiffFraction*100), false
	}
	return "", true
}

func TestGoldenTextBasic(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(20, "Hello, shellshot!", "$ go test ./... &&ok", "世a e\u0301 (wide, mark)")
	s.Widths = [][]snapshot.Wide{nil, nil, {{Rune: 0, Cells: 2}, {Rune: 4, Cells: 0}}}
	img, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "text_basic.png", img)
}

func TestImagesCloseTolerance(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 10, 10))
	b := image.NewRGBA(a.Bounds())
	copy(b.Pix, a.Pix)
	b.Pix[0] = 2 // within channel tolerance
	if msg, ok := imagesClose(a, b); !ok {
		t.Errorf("small channel diff rejected: %s", msg)
	}
	b.Pix[0] = 3
	b.Pix[4] = 3
	if _, ok := imagesClose(a, b); ok {
		t.Error("2% pixels off by 3 accepted")
	}
}
