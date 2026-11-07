package sprite

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "regenerate golden images in testdata/")

// Golden comparison tolerance, as in the raster package: at most this
// fraction of pixels may differ, where a pixel differs if any channel is off
// by more than the channel tolerance.
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
		t.Fatalf("open golden (regenerate with: go test -run Golden -update ./internal/render/sprite): %v", err)
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
// differ.
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

func TestImagesCloseTolerance(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 10, 10))
	b := image.NewRGBA(a.Bounds())
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

var (
	sheetBackground = color.RGBA{24, 26, 32, 255}
	sheetCell       = color.RGBA{38, 41, 50, 255}
	sheetInk        = color.RGBA{225, 225, 225, 255}
)

// sheetGrid draws count consecutive runes from first in a 16-wide grid of
// cw x ch cells separated by 2px, at origin, and returns the grid's height.
func sheetGrid(img *image.RGBA, origin image.Point, first rune, count, cw, ch int) int {
	rows := (count + 15) / 16
	for i := 0; i < count; i++ {
		x, y := origin.X+(i%16)*(cw+2), origin.Y+(i/16)*(ch+2)
		cell := image.Rect(x, y, x+cw, y+ch)
		draw.Draw(img, cell, image.NewUniform(sheetCell), image.Point{}, draw.Src)
		Draw(img, first+rune(i), cell, sheetInk)
	}
	return rows * (ch + 2)
}

// sheetText draws lines of runes in adjacent cells, so joins are visible.
func sheetText(img *image.RGBA, origin image.Point, lines []string, cw, ch int) int {
	for row, line := range lines {
		col := 0
		for _, r := range line {
			cell := image.Rect(origin.X+col*cw, origin.Y+row*ch, origin.X+(col+1)*cw, origin.Y+(row+1)*ch)
			Draw(img, r, cell, sheetInk)
			col++
		}
	}
	return len(lines) * ch
}

var sheetLines = []string{
	"┌──┬──┐ ╔══╦══╗ ┏━━┳━━┓ ╭──╮ ╲╱",
	"│  │  │ ║  ║  ║ ┃  ┃  ┃ │  │ ╳",
	"├──┼──┤ ╠══╬══╣ ┣━━╋━━┫ ╰──╯ ╱╲",
	"└──┴──┘ ╚══╩══╝ ┗━━┻━━┛ ┄┄┄┄ ┈┈┈┈ ╌╌╌╌",
	"░▒▓█▀▄▌▐▖▗▘▙▚▛▜▝▞▟▁▂▃▅▆▇▉▊▋▍▎▏▔▕",
	"⣿⠁⠂⠄⡀⠈⠐⠠⢀⣤⠛  \ue0b0\ue0b1\ue0b2\ue0b3\ue0b4\ue0b5\ue0b6\ue0b7",
}

// renderSheet draws every handled rune once, followed by joined samples at
// three cell sizes.
func renderSheet() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 460, 1200))
	draw.Draw(img, img.Bounds(), image.NewUniform(sheetBackground), image.Point{}, draw.Src)
	y := 6
	y += sheetGrid(img, image.Pt(6, y), 0x2500, 0x80, 12, 24) + 6
	y += sheetGrid(img, image.Pt(6, y), 0x2580, 0x20, 12, 24) + 6
	y += sheetGrid(img, image.Pt(6, y), 0xE0B0, 8, 12, 24) + 6
	y += sheetGrid(img, image.Pt(6, y), 0x2800, 0x100, 12, 24) + 6
	for _, sz := range []image.Point{{12, 24}, {9, 20}, {7, 13}} {
		y += sheetText(img, image.Pt(6, y), sheetLines, sz.X, sz.Y) + 6
	}
	return img.SubImage(image.Rect(0, 0, 460, y)).(*image.RGBA)
}

func TestGoldenSheet(t *testing.T) {
	sheet := renderSheet()
	// SubImage keeps the original pixel buffer; copy so the PNG is exact.
	out := image.NewRGBA(image.Rect(0, 0, sheet.Bounds().Dx(), sheet.Bounds().Dy()))
	draw.Draw(out, out.Bounds(), sheet, image.Point{}, draw.Src)
	compareGolden(t, "sprite_sheet.png", out)
}
