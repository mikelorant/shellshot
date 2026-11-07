package sprite

import (
	"image"
	"image/color"
	"testing"
)

// TestRectsMatchDraw checks that painting Rects gives exactly the pixels Draw
// inks, for every handled rune at several cell sizes.
func TestRectsMatchDraw(t *testing.T) {
	var runes []rune
	for r := rune(0x2500); r <= 0x259F; r++ {
		runes = append(runes, r)
	}
	for r := rune(0x2800); r <= 0x28FF; r += 7 {
		runes = append(runes, r)
	}
	for r := rune(0xE0B0); r <= 0xE0B7; r++ {
		runes = append(runes, r)
	}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	for _, size := range [][2]int{{9, 18}, {7, 15}, {18, 18}, {5, 9}, {1, 1}} {
		w, h := size[0], size[1]
		for _, r := range runes {
			want := image.NewRGBA(image.Rect(0, 0, w, h))
			Draw(want, r, want.Bounds(), white)
			got := image.NewRGBA(image.Rect(0, 0, w, h))
			rects, ok := Rects(r, w, h)
			if !ok {
				t.Fatalf("Rects(%U) not handled", r)
			}
			for _, rc := range rects {
				if rc.X0 < 0 || rc.Y0 < 0 || rc.X1 > w || rc.Y1 > h || rc.X0 >= rc.X1 || rc.Y0 >= rc.Y1 || rc.A == 0 {
					t.Fatalf("Rects(%U, %d, %d): bad rect %+v", r, w, h, rc)
				}
				for y := rc.Y0; y < rc.Y1; y++ {
					for x := rc.X0; x < rc.X1; x++ {
						if got.Pix[got.PixOffset(x, y)+3] != 0 {
							t.Fatalf("Rects(%U, %d, %d): rects overlap at (%d,%d)", r, w, h, x, y)
						}
						got.Pix[got.PixOffset(x, y)+3] = rc.A
					}
				}
			}
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if g, w2 := got.Pix[got.PixOffset(x, y)+3], want.Pix[want.PixOffset(x, y)+3]; g != w2 {
						t.Fatalf("Rects(%U, %d, %d): alpha at (%d,%d) = %d, Draw has %d", r, w, h, x, y, g, w2)
					}
				}
			}
		}
	}
}

func TestRectsSimpleGlyphs(t *testing.T) {
	rects, ok := Rects('█', 9, 18)
	if !ok || len(rects) != 1 || rects[0] != (Rect{0, 0, 9, 18, 255}) {
		t.Errorf("Rects(█) = %+v, %v; want one full-cell rect", rects, ok)
	}
	rects, ok = Rects('▄', 9, 18)
	if !ok || len(rects) != 1 || rects[0] != (Rect{0, 9, 9, 18, 255}) {
		t.Errorf("Rects(▄) = %+v, %v; want the lower half", rects, ok)
	}
	rects, ok = Rects('░', 9, 18)
	if !ok || len(rects) != 1 || rects[0].A != shadeLight {
		t.Errorf("Rects(░) = %+v, %v; want one light-shade rect", rects, ok)
	}
	if rects, ok = Rects('\u2800', 9, 18); !ok || len(rects) != 0 {
		t.Errorf("Rects(blank braille) = %+v, %v; want none", rects, ok)
	}
	if _, ok = Rects('a', 9, 18); ok {
		t.Error("Rects('a') handled")
	}
}
