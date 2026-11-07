package raster

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
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

func palette(i uint8) snapshot.Color { return snapshot.Color{Kind: snapshot.ColorPalette, Index: i} }

func TestGoldenStyleBasic(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(24,
		"regular bold italic both",
		"underline strike inverse",
		"red green on blue faint",
		"hidden:[invisible] 世a",
		"bg 世 span after wide")
	attr := func(a snapshot.Attr) snapshot.Style { return snapshot.Style{Attrs: a} }
	s.Spans = [][]snapshot.Span{
		{{Start: 8, End: 12, Style: attr(snapshot.Bold)}, {Start: 13, End: 19, Style: attr(snapshot.Italic)}, {Start: 20, End: 24, Style: attr(snapshot.Bold | snapshot.Italic)}},
		{{Start: 0, End: 9, Style: attr(snapshot.Underline)}, {Start: 10, End: 16, Style: attr(snapshot.Strike)}, {Start: 17, End: 24, Style: attr(snapshot.Inverse)}},
		{{Start: 0, End: 3, Style: snapshot.Style{Fg: palette(1)}}, {Start: 4, End: 9, Style: snapshot.Style{Fg: palette(2)}},
			{Start: 13, End: 17, Style: snapshot.Style{Fg: palette(15), Bg: palette(4)}}, {Start: 18, End: 23, Style: attr(snapshot.Faint)}},
		{{Start: 7, End: 18, Style: attr(snapshot.Invisible)}},
		{{Start: 0, End: 2, Style: snapshot.Style{Bg: palette(3), Fg: palette(0)}}, {Start: 4, End: 8, Style: snapshot.Style{Bg: palette(5)}}},
	}
	s.Widths = [][]snapshot.Wide{nil, nil, nil, {{Rune: 19, Cells: 2}}, {{Rune: 3, Cells: 2}}}
	compareGolden(t, "style_basic.png", mustRender(t, r, s))
}

// TestGoldenStyledBlanks draws lines that end in styled blanks: a status bar
// filled to the last column, a row of 16 colour swatches made only of blanks,
// a reverse-video bar, and underlined and struck-through trailing blanks.
func TestGoldenStyledBlanks(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	const cols = 32
	swatches := strings.Repeat(" ", 2*16)
	s := plainSnapshot(cols,
		" status bar"+strings.Repeat(" ", cols-len(" status bar")),
		swatches,
		"reverse"+strings.Repeat(" ", cols-len("reverse")),
		"under   ",
		"strike   ")
	s.Spans = [][]snapshot.Span{
		{{Start: 0, End: cols, Style: snapshot.Style{Fg: palette(7), Bg: palette(4)}}},
		nil,
		{{Start: 0, End: cols, Style: snapshot.Style{Attrs: snapshot.Inverse}}},
		{{Start: 5, End: 8, Style: snapshot.Style{Attrs: snapshot.Underline, Fg: palette(2)}}},
		{{Start: 6, End: 9, Style: snapshot.Style{Attrs: snapshot.Strike, Fg: palette(1)}}},
	}
	for i := 0; i < 16; i++ {
		s.Spans[1] = append(s.Spans[1], snapshot.Span{Start: 2 * i, End: 2*i + 2, Style: snapshot.Style{Bg: palette(uint8(i))}})
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "styled_blanks.png", mustRender(t, r, s))
}

// TestGoldenCursorShapes stacks one render per cursor style (block, bar,
// underline, none) over the same text, with the cursor on a letter.
func TestGoldenCursorShapes(t *testing.T) {
	s := plainSnapshot(12, "$ echo hello")
	s.Cursor = snapshot.Cursor{X: 7, Y: 0, Visible: true}
	var imgs []*image.RGBA
	for _, style := range []CursorStyle{CursorBlock, CursorBar, CursorUnderline, CursorNone} {
		r, _ := newTestRenderer(t, Options{Cursor: style})
		imgs = append(imgs, mustRender(t, r, s))
	}
	b := imgs[0].Bounds()
	stack := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()*len(imgs)))
	for i, img := range imgs {
		draw.Draw(stack, image.Rect(0, i*b.Dy(), b.Dx(), (i+1)*b.Dy()), img, b.Min, draw.Src)
	}
	compareGolden(t, "cursor_shapes.png", stack)
}
