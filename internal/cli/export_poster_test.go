package cli

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kettek "github.com/kettek/apng"
)

// pngPicture decodes data with image/png, as a viewer without animation does.
func pngPicture(t *testing.T, data []byte) *image.RGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	return rgba
}

// animationOf returns a without its default image, if it has one, and
// whether it had one.
func animationOf(a kettek.APNG) (kettek.APNG, bool) {
	if a.Frames[0].IsDefault {
		a.Frames = a.Frames[1:]
		return a, true
	}
	return a, false
}

// exportPoster exports the fixture (the screens "red ok" and "red done", after
// a short blank one that the default trim drops) with extra flags and returns
// the file's bytes.
func exportPoster(t *testing.T, flags ...string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.png")
	args := append([]string{writeExportFixture(t), "-o", out, "--hold", "1s"}, flags...)
	if code, stderr := runExportCmd(t, args...); code != 0 {
		t.Fatalf("export %v: exit %d, stderr %q", flags, code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeAPNGBytes(t *testing.T, data []byte) kettek.APNG {
	t.Helper()
	a, err := kettek.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// The default poster of an APNG is the last frame: a viewer that shows one
// image shows the end result, and the animation is unchanged.
func TestExportPosterDefaultIsLastFrame(t *testing.T) {
	data := exportPoster(t)
	a, hasDefault := animationOf(decodeAPNGBytes(t, data))
	if !hasDefault {
		t.Fatal("output has no default image, want the last frame")
	}
	if len(a.Frames) != 2 {
		t.Fatalf("animation frames = %d, want 2", len(a.Frames))
	}
	last := compose(t, a, 1)
	if !samePixels(pngPicture(t, data), last) {
		t.Error("image/png shows something other than the last frame")
	}
	if samePixels(pngPicture(t, data), compose(t, a, 0)) {
		t.Fatal("test fixture: first and last frame are equal")
	}
}

func TestExportPosterFlagValues(t *testing.T) {
	none := exportPoster(t, "--poster", "none")
	a, hasDefault := animationOf(decodeAPNGBytes(t, none))
	if hasDefault {
		t.Fatal("--poster none wrote a default image")
	}
	first, last := compose(t, a, 0), compose(t, a, 1)
	if !samePixels(pngPicture(t, none), first) {
		t.Error("--poster none: image/png does not show the first frame")
	}
	// The first frame is already the default image, so first is none.
	if got := exportPoster(t, "--poster", "first"); !bytes.Equal(got, none) {
		t.Error("--poster first differs from --poster none")
	}
	if got := exportPoster(t, "--poster", "last"); !bytes.Equal(got, exportPoster(t)) {
		t.Error("--poster last differs from the default")
	}
	// The animation shows "red ok" for 400 ms, then "red done".
	for poster, want := range map[string]*image.RGBA{"0s": first, "300ms": first, "600ms": last, "1h": last} {
		data := exportPoster(t, "--poster", poster)
		if !samePixels(pngPicture(t, data), want) {
			t.Errorf("--poster %s: image/png shows the wrong frame", poster)
		}
		if anim, _ := animationOf(decodeAPNGBytes(t, data)); len(anim.Frames) != 2 {
			t.Errorf("--poster %s: %d animation frames, want 2", poster, len(anim.Frames))
		}
	}
}

func TestExportPosterErrors(t *testing.T) {
	in := writeExportFixture(t)
	dir := t.TempDir()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bad value", []string{"-o", filepath.Join(dir, "a.png"), "--poster", "middle"}, `--poster: invalid poster "middle"`},
		{"negative time", []string{"-o", filepath.Join(dir, "b.png"), "--poster", "-1s"}, "--poster"},
		{"gif", []string{"-o", filepath.Join(dir, "c.gif"), "--poster", "last"}, "--poster only applies to apng output, not gif"},
		{"webp", []string{"-o", filepath.Join(dir, "d.webp"), "--poster", "none"}, "--poster only applies to apng output, not webp"},
		{"svg", []string{"-o", filepath.Join(dir, "e.svg"), "--poster", "first"}, "--poster only applies to apng output, not svg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stderr := runExportCmd(t, append([]string{in}, tt.args...)...)
			if code == 0 || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, stderr %q, want a failure containing %q", code, stderr, tt.want)
			}
			if _, err := os.Stat(tt.args[1]); err == nil {
				t.Errorf("%s was written", tt.args[1])
			}
		})
	}
}

// Formats other than apng take no poster, and need no flag to say so.
func TestExportOtherFormatsIgnoreThePosterDefault(t *testing.T) {
	in := writeExportFixture(t)
	for _, name := range []string{"a.gif", "b.webp", "c.svg"} {
		out := filepath.Join(t.TempDir(), name)
		if code, stderr := runExportCmd(t, in, "-o", out); code != 0 {
			t.Errorf("%s: exit %d, stderr %q", name, code, stderr)
		}
	}
}
