package svg

import (
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	"github.com/michaellorant/shellshot/internal/render/raster"
)

// rasteriseSVG draws doc at its viewBox size with oksvg, a small pure-Go SVG
// renderer. It supports paths, rectangles and <use> but neither <text> nor
// <image>, so pixel checks use the paths text mode and no emoji.
func rasteriseSVG(t testing.TB, doc string, w, h int) *image.RGBA {
	t.Helper()
	icon, err := oksvg.ReadIconStream(strings.NewReader(doc), oksvg.WarnErrorMode)
	if err != nil {
		t.Fatalf("oksvg: %v", err)
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	icon.Draw(rasterx.NewDasher(w, h, rasterx.NewScannerGV(w, h, img, img.Bounds())), 1)
	return img
}

// pixelDiff compares two images of one size: the mean absolute channel error
// as a fraction of 255, and the fraction of pixels whose largest channel
// error exceeds 64.
func pixelDiff(a, b *image.RGBA) (mean, bad float64) {
	var sum, n, nbad int
	for i := 0; i < len(a.Pix); i += 4 {
		worst := 0
		for c := 0; c < 3; c++ {
			d := int(a.Pix[i+c]) - int(b.Pix[i+c])
			if d < 0 {
				d = -d
			}
			sum += d
			worst = max(worst, d)
		}
		n += 3
		if worst > 64 {
			nbad++
		}
	}
	return float64(sum) / float64(n) / 255, float64(nbad) / float64(n/3)
}

// pixelScreen has what oksvg can draw: backgrounds, glyph outlines (Latin and
// CJK from the fallback font), box and block sprites, decorations.
func pixelScreen(t testing.TB) (rows []string) {
	return []string{
		"The quick brown fox " + esc + "1mBOLD" + reset + " " + esc + "3mitalic" + reset,
		esc + "31;42mred on green" + reset + " " + esc + "7minverse" + reset + " " + esc + "2mfaint" + reset,
		esc + "4munderline" + reset + " " + esc + "9mstrike" + reset + " 世界 한국",
		"┌──────┐ ╔══╗ █▄▀░▒▓ ⣿",
		"│ box  │ ║  ║ ▌▐▖▗▘▙ ⠁",
		"└──────┘ ╚══╝  ╭──╮ ╱╲ ",
		"e\u0301 a\u0308 " + esc + "34mblue" + reset + " {}[]() ->=>==",
	}
}

func TestSVGPathsMatchesRasterRenderer(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    raster.Options
		cur  raster.CursorStyle
	}{
		{"default", raster.Options{}, raster.CursorBlock},
		{"scale 2 bar", raster.Options{Scale: 2, FontSize: 12, Cursor: raster.CursorBar}, raster.CursorBar},
		{"underline", raster.Options{FontSize: 13, Cursor: raster.CursorUnderline}, raster.CursorUnderline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, g := newRenderer(t, tc.o)
			s := withCursor(screen(t, 30, pixelScreen(t)...), 4, 1)
			ref, err := r.Render(s)
			if err != nil {
				t.Fatal(err)
			}
			doc := write(t, Options{Geometry: g, Text: TextPaths, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
			got := rasteriseSVG(t, doc, ref.Bounds().Dx(), ref.Bounds().Dy())
			mean, bad := pixelDiff(ref, got)
			blank := image.NewRGBA(ref.Bounds())
			for i := 0; i < len(blank.Pix); i += 4 {
				c := r.Layout().Theme.Background
				blank.Pix[i], blank.Pix[i+1], blank.Pix[i+2], blank.Pix[i+3] = c.R, c.G, c.B, 255
			}
			control, _ := pixelDiff(ref, blank)
			t.Logf("%s: mean error %.4f, %.2f%% of pixels off by more than 64 (an empty screen would score %.4f)", tc.name, mean, bad*100, control)
			if mean*5 > control {
				t.Errorf("mean error %.4f is not far below that of an empty screen, %.4f", mean, control)
			}
			if os.Getenv("SVG_DUMP") != "" {
				dump(t, "/tmp/svgpix_"+strings.ReplaceAll(tc.name, " ", "_"), ref, got)
			}
			if mean > 0.02 || bad > 0.04 {
				t.Errorf("SVG differs from the raster render: mean error %.4f (limit 0.02), %.2f%% bad pixels (limit 4%%)", mean, bad*100)
			}
		})
	}
}

// TestSVGSpritesAreIdenticalToRaster compares box, block and braille cells
// only: their geometry is shared, so the pixels must match exactly.
func TestSVGSpritesAreIdenticalToRaster(t *testing.T) {
	r, g := newRenderer(t, raster.Options{})
	s := screen(t, 30,
		"┌─┬─┐╔═╦═╗ ━┃┏┓┗┛ ╭─╮╰─╯ ╱╲╳",
		"├─┼─┤╠═╬═╣ ┄┆┈┊╌╎ ▀▄█▌▐░▒▓ ▖▗▘▙▚▛▜▝▞▟",
		"⠁⠃⠇⡇⣇⣧⣷⣿ \ue0b0\ue0b1\ue0b2\ue0b3\ue0b4\ue0b5\ue0b6\ue0b7")
	ref, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	doc := write(t, Options{Geometry: g, Text: TextPaths, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
	got := rasteriseSVG(t, doc, ref.Bounds().Dx(), ref.Bounds().Dy())
	mean, bad := pixelDiff(ref, got)
	t.Logf("sprites: mean error %.5f, %.3f%% bad pixels", mean, bad*100)
	// oksvg anti-aliases rectangle edges that lie on pixel boundaries only
	// slightly; powerline glyphs are the same pixel steps as raster.
	if mean > 0.002 || bad > 0.002 {
		t.Errorf("sprite pixels differ: mean %.5f, %.3f%% bad", mean, bad*100)
	}
}

func dump(t testing.TB, prefix string, imgs ...*image.RGBA) {
	for i, img := range imgs {
		f, err := os.Create(prefix + string(rune('a'+i)) + ".png")
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
