package format

import (
	"bytes"
	"image"
	"slices"
	"testing"
	"time"
)

func TestDefaultRegistersWebP(t *testing.T) {
	r := Default()
	if !slices.Contains(r.Names(), "webp") {
		t.Fatalf("Names = %v, want webp among them", r.Names())
	}
	f, ok := r.Lookup("WebP")
	if !ok || f.Name != "webp" {
		t.Fatalf("Lookup(WebP) = %+v, %v", f, ok)
	}
	if !slices.Equal(f.Extensions, []string{".webp"}) {
		t.Errorf("Extensions = %v, want [.webp]", f.Extensions)
	}
	for _, path := range []string{"a.webp", "dir/A.WEBP"} {
		if got, ok := r.ByExtension(path); !ok || got.Name != "webp" {
			t.Errorf("ByExtension(%q) = %q, %v; want webp", path, got.Name, ok)
		}
	}
}

func TestWebPSinkHonoursLoops(t *testing.T) {
	f, _ := Default().Lookup("webp")
	var buf bytes.Buffer
	sink, err := f.New(Settings{Loops: 5})(&buf, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.AddFrame(image.NewRGBA(image.Rect(0, 0, 4, 4)), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		t.Fatalf("not a WebP: %q", b[:12])
	}
	// RIFF header (12) + VP8X chunk (18) + ANIM header (8) + background (4)
	// puts the 16-bit loop count at byte 42.
	if b[42] != 5 || b[43] != 0 {
		t.Errorf("loop count bytes % x, want 05 00", b[42:44])
	}
}
