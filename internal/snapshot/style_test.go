package snapshot

import (
	"errors"
	"testing"
)

func pal(i uint8) Color       { return Color{Kind: ColorPalette, Index: i} }
func rgb(r, g, b uint8) Color { return Color{Kind: ColorRGB, R: r, G: g, B: b} }

const allAttrs = Bold | Faint | Italic | Underline | Blink | Inverse | Invisible | Strike

func TestStyleSGR(t *testing.T) {
	tests := []struct {
		name  string
		style Style
		want  string
	}{
		{"zero", Style{}, ""},
		{"bold red", Style{Fg: pal(1), Attrs: Bold}, "1;31"},
		{"palette 208", Style{Fg: pal(208)}, "38;5;208"},
		{"rgb bg", Style{Bg: rgb(10, 20, 30)}, "48;2;10;20;30"},
		{"rgb fg", Style{Fg: rgb(0, 255, 7)}, "38;2;0;255;7"},
		{"bright fg", Style{Fg: pal(9)}, "91"},
		{"bright bg", Style{Bg: pal(15)}, "107"},
		{"normal bg", Style{Bg: pal(0)}, "40"},
		{"palette 16 bg", Style{Bg: pal(16)}, "48;5;16"},
		{"all attrs", Style{Attrs: allAttrs}, "1;2;3;4;5;7;8;9"},
		{"mixed", Style{Attrs: Bold | Italic | Underline, Fg: pal(9), Bg: rgb(1, 2, 3)}, "1;3;4;91;48;2;1;2;3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.style.SGR(); got != tt.want {
				t.Errorf("SGR() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStyleIsZero(t *testing.T) {
	if !(Style{}).IsZero() {
		t.Error("zero style not zero")
	}
	for _, s := range []Style{{Attrs: Bold}, {Fg: pal(0)}, {Bg: rgb(0, 0, 0)}} {
		if s.IsZero() {
			t.Errorf("%+v reported zero", s)
		}
	}
}

func TestSGRRoundTrip(t *testing.T) {
	styles := []Style{
		{},
		{Fg: pal(0)}, {Fg: pal(7)}, {Fg: pal(8)}, {Fg: pal(15)}, {Fg: pal(16)}, {Fg: pal(255)},
		{Bg: pal(0)}, {Bg: pal(7)}, {Bg: pal(8)}, {Bg: pal(15)}, {Bg: pal(16)}, {Bg: pal(255)},
		{Fg: rgb(0, 0, 0)}, {Fg: rgb(255, 255, 255)}, {Bg: rgb(10, 20, 30)},
		{Fg: rgb(1, 2, 3), Bg: rgb(4, 5, 6)},
		{Attrs: allAttrs},
		{Attrs: allAttrs, Fg: pal(255), Bg: rgb(255, 0, 128)},
		{Attrs: Bold | Italic | Underline, Fg: pal(9), Bg: rgb(1, 2, 3)},
	}
	for _, a := range []Attr{Bold, Faint, Italic, Underline, Blink, Inverse, Invisible, Strike} {
		styles = append(styles, Style{Attrs: a})
	}
	for i := 0; i < 256; i++ {
		styles = append(styles, Style{Fg: pal(uint8(i)), Bg: pal(uint8(255 - i))})
	}
	for _, want := range styles {
		sgr := want.SGR()
		got, err := ParseSGR(sgr)
		if err != nil {
			t.Fatalf("ParseSGR(%q): %v", sgr, err)
		}
		if got != want {
			t.Errorf("ParseSGR(%q) = %+v, want %+v", sgr, got, want)
		}
	}
}

func TestParseSGRZero(t *testing.T) {
	for _, in := range []string{"", "0"} {
		got, err := ParseSGR(in)
		if err != nil || !got.IsZero() {
			t.Errorf("ParseSGR(%q) = %+v, %v; want zero, nil", in, got, err)
		}
	}
}

func TestParseSGRErrors(t *testing.T) {
	for _, in := range []string{
		"x", "1;", ";1", "1;;2", "-1", "+1", "6", "10", "29", "39", "49", "108",
		"38", "38;5", "38;5;256", "38;2;1;2", "38;2;1;2;256", "38;3;1", "48;5;x", "1;0", "99999999999999999999",
	} {
		_, err := ParseSGR(in)
		if !errors.Is(err, ErrBadSGR) {
			t.Errorf("ParseSGR(%q) error = %v, want ErrBadSGR", in, err)
		}
	}
}
