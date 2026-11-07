package theme

import (
	"image/color"
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

func pal(i uint8) snapshot.Color { return snapshot.Color{Kind: snapshot.ColorPalette, Index: i} }

func TestPaletteColorComputed(t *testing.T) {
	th := Default()
	tests := []struct {
		index uint8
		want  color.RGBA
	}{
		{16, color.RGBA{0, 0, 0, 255}},
		{17, color.RGBA{0, 0, 95, 255}},
		{21, color.RGBA{0, 0, 255, 255}},
		{22, color.RGBA{0, 95, 0, 255}},
		{52, color.RGBA{95, 0, 0, 255}},
		{196, color.RGBA{255, 0, 0, 255}},
		{231, color.RGBA{255, 255, 255, 255}},
		{232, color.RGBA{8, 8, 8, 255}},
		{244, color.RGBA{128, 128, 128, 255}},
		{255, color.RGBA{238, 238, 238, 255}},
	}
	for _, tt := range tests {
		if got := th.paletteColor(tt.index); got != tt.want {
			t.Errorf("paletteColor(%d) = %v, want %v", tt.index, got, tt.want)
		}
	}
}

func TestResolve(t *testing.T) {
	th := Default()
	tests := []struct {
		name string
		c    snapshot.Color
		role Role
		want color.RGBA
	}{
		{"default fg", snapshot.Color{}, Fg, th.Foreground},
		{"default bg", snapshot.Color{}, Bg, th.Background},
		{"palette 0 fg", pal(0), Fg, th.Palette[0]},
		{"palette 15 bg", pal(15), Bg, th.Palette[15]},
		{"palette cube", pal(21), Fg, color.RGBA{0, 0, 255, 255}},
		{"palette ramp", pal(232), Bg, color.RGBA{8, 8, 8, 255}},
		{"rgb fg", snapshot.Color{Kind: snapshot.ColorRGB, R: 1, G: 2, B: 3}, Fg, color.RGBA{1, 2, 3, 255}},
		{"rgb bg", snapshot.Color{Kind: snapshot.ColorRGB, R: 1, G: 2, B: 3}, Bg, color.RGBA{1, 2, 3, 255}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := th.Resolve(tt.c, tt.role); got != tt.want {
				t.Errorf("Resolve = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestColors(t *testing.T) {
	th := Theme{
		Foreground: color.RGBA{200, 100, 0, 255},
		Background: color.RGBA{0, 0, 100, 255},
	}
	red := snapshot.Color{Kind: snapshot.ColorRGB, R: 255}
	tests := []struct {
		name           string
		style          snapshot.Style
		wantFg, wantBg color.RGBA
	}{
		{"default", snapshot.Style{}, th.Foreground, th.Background},
		{"inverse", snapshot.Style{Attrs: snapshot.Inverse}, th.Background, th.Foreground},
		{"faint", snapshot.Style{Attrs: snapshot.Faint}, color.RGBA{100, 50, 50, 255}, th.Background},
		{"invisible", snapshot.Style{Attrs: snapshot.Invisible}, th.Background, th.Background},
		{"inverse+faint", snapshot.Style{Attrs: snapshot.Inverse | snapshot.Faint},
			color.RGBA{100, 50, 50, 255}, th.Foreground},
		{"inverse+invisible", snapshot.Style{Attrs: snapshot.Inverse | snapshot.Invisible},
			th.Foreground, th.Foreground},
		{"explicit fg inverse", snapshot.Style{Fg: red, Attrs: snapshot.Inverse},
			th.Background, color.RGBA{255, 0, 0, 255}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fg, bg := th.Colors(tt.style)
			if fg != tt.wantFg || bg != tt.wantBg {
				t.Errorf("Colors = %v, %v; want %v, %v", fg, bg, tt.wantFg, tt.wantBg)
			}
		})
	}
}

func TestLookupAndNames(t *testing.T) {
	for _, name := range Names() {
		th, ok := Lookup(name)
		if !ok || th.Name != name {
			t.Errorf("Lookup(%q) = %q, %v", name, th.Name, ok)
		}
	}
	if got := Names(); len(got) != 2 || got[0] != "dark" || got[1] != "light" {
		t.Errorf("Names() = %v, want [dark light]", got)
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup(nope) ok = true, want false")
	}
	if Default().Name != "dark" || Light().Name != "light" {
		t.Error("built-in names mismatch")
	}
	// Mutating a returned theme must not affect later lookups.
	th, _ := Lookup("dark")
	th.Palette[1] = color.RGBA{}
	if again, _ := Lookup("dark"); again.Palette[1] == (color.RGBA{}) {
		t.Error("Lookup result aliases shared state")
	}
}
