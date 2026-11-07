package font

import (
	"fmt"
	"testing"
)

func TestOutline(t *testing.T) {
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	o, ok, err := s.Outline('M', false, false)
	if err != nil || !ok {
		t.Fatalf("Outline(M) = %v, %v", ok, err)
	}
	if !o.Primary || o.UnitsPerEm <= 0 || len(o.Segments) == 0 || o.Advance <= 0 {
		t.Errorf("Outline(M) = %+v, want a primary glyph with segments", o)
	}
	if b, _, _ := s.Outline('M', true, false); fmt.Sprint(b.Segments) == fmt.Sprint(o.Segments) {
		t.Error("bold M has the outline of regular M")
	}
	cjk, ok, err := s.Outline('世', false, false)
	if err != nil || !ok || cjk.Primary {
		t.Errorf("Outline(世) = primary %v, ok %v, err %v; want a fallback glyph", cjk.Primary, ok, err)
	}
	if _, ok, _ := s.Outline(0x10FFFE, false, false); ok {
		t.Error("Outline of a rune no font has reported ok")
	}
	if !s.Covers('a', false, false) || s.Covers(0x10FFFE, false, false) {
		t.Error("Covers disagrees with the fonts")
	}
}
