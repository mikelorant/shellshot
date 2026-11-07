package cli

import (
	"bytes"
	"image"
	"image/draw"
	stdgif "image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeGIFFile(t *testing.T, path string) *stdgif.GIF {
	t.Helper()
	g, err := stdgif.DecodeAll(bytes.NewReader(mustRead(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// lastScreen returns the canvas after the last frame of g.
func lastScreen(g *stdgif.GIF) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	for _, f := range g.Image {
		draw.Draw(canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
	}
	return canvas
}

func TestExportGIFIsChosenByNameOrExtension(t *testing.T) {
	in := writeExportFixture(t)
	dir := t.TempDir()
	var first []byte
	for name, args := range map[string][]string{
		"a.gif": nil,
		"b.GIF": nil,
		"c.png": {"--format", "gif"},
		"d.bin": {"--format", "GIF"},
	} {
		out := filepath.Join(dir, name)
		if code, stderr := runExportCmd(t, append([]string{in, "-o", out}, args...)...); code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", name, code, stderr)
		}
		got := mustRead(t, out)
		if !bytes.HasPrefix(got, []byte("GIF89a")) {
			t.Errorf("%s: not a GIF: %q", name, got[:6])
		}
		if first == nil {
			first = got
		} else if !bytes.Equal(got, first) {
			t.Errorf("%s: bytes differ from the first output", name)
		}
	}
}

func TestExportGIFWithoutOutputUsesGIFExtension(t *testing.T) {
	in := writeFile(t, "z.shot", mustRead(t, writeExportFixture(t)))
	if code, stderr := runExportCmd(t, in, "--format", "gif"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(in), "z.gif")); err != nil {
		t.Errorf("default output z.gif missing: %v", err)
	}
}

func TestExportGIFShowsTheRecordedScreens(t *testing.T) {
	in := writeExportFixture(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "o.gif")
	if code, stderr := runExportCmd(t, in, "-o", out); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	g := decodeGIFFile(t, out)
	if len(g.Image) < 2 {
		t.Fatalf("%d frames, want an animation", len(g.Image))
	}
	if g.LoopCount != 0 {
		t.Errorf("LoopCount %d, want 0 (forever)", g.LoopCount)
	}
	// The same screens as the APNG of the same recording.
	png := filepath.Join(dir, "o.png")
	if code, stderr := runExportCmd(t, in, "-o", png); code != 0 {
		t.Fatalf("apng: exit %d, stderr %q", code, stderr)
	}
	a := decodeAPNG(t, png)
	if len(a.Frames) != len(g.Image) {
		t.Fatalf("%d GIF frames, %d APNG frames", len(g.Image), len(a.Frames))
	}
	want, got := compose(t, a, len(a.Frames)-1), lastScreen(g)
	if want.Rect != got.Rect {
		t.Fatalf("GIF canvas %v, APNG canvas %v", got.Rect, want.Rect)
	}
	// Few colours, so the palette is exact: pixel-identical to the APNG.
	if !bytes.Equal(want.Pix, got.Pix) {
		t.Error("the last GIF screen differs from the last APNG screen")
	}
}

func TestExportGIFLoops(t *testing.T) {
	in := writeExportFixture(t)
	for loops, want := range map[string]int{"0": 0, "1": -1, "4": 3} {
		out := filepath.Join(t.TempDir(), "o.gif")
		if code, stderr := runExportCmd(t, in, "-o", out, "--loops", loops); code != 0 {
			t.Fatalf("--loops %s: exit %d, stderr %q", loops, code, stderr)
		}
		if got := decodeGIFFile(t, out).LoopCount; got != want {
			t.Errorf("--loops %s: LoopCount %d, want %d", loops, got, want)
		}
	}
}

func TestExportGIFOptions(t *testing.T) {
	usage := newExportCmd().Flags().FlagUsages()
	for _, want := range []string{"--gif-colors", "--gif-dither"} {
		if !strings.Contains(usage, want) {
			t.Errorf("export flag help lacks %q", want)
		}
	}
	in := writeExportFixture(t)
	dir := t.TempDir()
	for _, args := range [][]string{{"--gif-colors", "2"}, {"--gif-colors", "255"}, {"--gif-dither"}, {"--gif-colors", "16", "--gif-dither"}} {
		out := filepath.Join(dir, "o.gif")
		if code, stderr := runExportCmd(t, append([]string{in, "-o", out}, args...)...); code != 0 {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
}

func TestExportGIFRejectsBadColors(t *testing.T) {
	in := writeExportFixture(t)
	for _, v := range []string{"0", "1", "256", "-5"} {
		out := filepath.Join(t.TempDir(), "o.gif")
		code, stderr := runExportCmd(t, in, "-o", out, "--gif-colors", v)
		if code != 1 || !strings.Contains(stderr, "invalid --gif-colors "+v) {
			t.Errorf("--gif-colors %s: exit %d, stderr %q", v, code, stderr)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("--gif-colors %s: output written", v)
		}
	}
}
