package cli

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kettek "github.com/kettek/apng"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
	"github.com/michaellorant/shellshot/internal/render/font"
	"github.com/michaellorant/shellshot/internal/render/raster"
	"github.com/michaellorant/shellshot/internal/render/theme"
)

const (
	exportFixtureCols = 12
	exportFixtureRows = 2
)

// writeExportFixture writes a 12x2 recording and returns its path. Row 0 is
// "red ok" with the first three runes red; row 1 is a wide rune followed by
// "X" on a red background, so X sits in cell column 2. Half a second later
// row 0 becomes "red done", again with the first three runes red.
func writeExportFixture(t *testing.T) string {
	t.Helper()
	data := buildRecording(t, head.Head{Cols: exportFixtureCols, Rows: exportFixtureRows}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(100*time.Millisecond, []byte("\x1b[31mred\x1b[0m ok\r\n\u4e16\x1b[41mX\x1b[0m")))
		must(t, w.Output(500*time.Millisecond, []byte("\x1b[1;1H\x1b[31mred\x1b[0m done")))
	})
	return writeFile(t, "fixture.shot", data)
}

// compose draws the first n+1 frames of a onto one canvas, the way a viewer
// would, and returns the screen shown after frame n.
func compose(t *testing.T, a kettek.APNG, n int) *image.RGBA {
	t.Helper()
	canvas := image.NewRGBA(a.Frames[0].Image.Bounds())
	for _, f := range a.Frames[:n+1] {
		b := f.Image.Bounds()
		r := image.Rect(f.XOffset, f.YOffset, f.XOffset+b.Dx(), f.YOffset+b.Dy())
		op := draw.Src
		if f.BlendOp == kettek.BLEND_OP_OVER {
			op = draw.Over
		}
		draw.Draw(canvas, r, f.Image, b.Min, op)
	}
	return canvas
}

func decodeAPNG(t *testing.T, path string) kettek.APNG {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := kettek.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// reddish reports whether c is close to the default theme's red (#cd3131).
func reddish(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r>>8 > 0x80 && g>>8 < 0x80 && b>>8 < 0x80
}

func runExportCmd(t *testing.T, args ...string) (code int, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(context.Background(), append([]string{"export"}, args...), &out, &errBuf)
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
	return code, errBuf.String()
}

func TestExportWritesStyledAPNG(t *testing.T) {
	in := writeExportFixture(t)
	out := filepath.Join(t.TempDir(), "out.apng")

	code, stderr := runExportCmd(t, in, "-o", out, "--hold", "1s")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	// Blank screen, the first output, the second output.
	if !strings.HasPrefix(stderr, "wrote 3 frames (") || !strings.HasSuffix(stderr, " KB) to "+out+"\n") {
		t.Errorf("stderr = %q, want a 'wrote 3 frames (N KB) to %s' line", stderr, out)
	}
	a := decodeAPNG(t, out)

	// Expected geometry from the same renderer the command builds.
	fonts, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	r, err := raster.New(fonts, raster.Options{})
	if err != nil {
		t.Fatal(err)
	}
	w, h := r.Size(exportFixtureCols, exportFixtureRows)
	cellW, cellH := (w-2*raster.DefaultPadding)/exportFixtureCols, (h-2*raster.DefaultPadding)/exportFixtureRows

	if len(a.Frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(a.Frames))
	}
	if a.LoopCount != 0 {
		t.Errorf("LoopCount = %d, want 0 (forever)", a.LoopCount)
	}
	if b := a.Frames[0].Image.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("first frame is %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
	// The sampling grid puts the first output a hair after 100ms.
	for i, want := range []time.Duration{100 * time.Millisecond, 400 * time.Millisecond, time.Second} {
		f := a.Frames[i]
		got := time.Duration(f.DelayNumerator) * time.Second / time.Duration(f.DelayDenominator)
		if d := got - want; d < -20*time.Millisecond || d > 20*time.Millisecond {
			t.Errorf("frame %d delay = %v, want about %v", i, got, want)
		}
	}

	cell := func(col, row int) image.Point {
		return image.Pt(raster.DefaultPadding+col*cellW, raster.DefaultPadding+row*cellH)
	}
	// Red text: some pixel in cells 0-2 of row 0 is reddish; the plain "ok"
	// text in cells 4-5 has none.
	count := func(img *image.RGBA, col0, col1, row int) int {
		n := 0
		o0, o1 := cell(col0, row), cell(col1, row)
		for y := o0.Y; y < o0.Y+cellH; y++ {
			for x := o0.X; x < o1.X+cellW; x++ {
				if reddish(img.At(x, y)) {
					n++
				}
			}
		}
		return n
	}
	blank := compose(t, a, 0)
	if n := count(blank, 0, 11, 0) + count(blank, 0, 11, 1); n != 0 {
		t.Errorf("%d reddish pixels on the blank first screen", n)
	}
	first := compose(t, a, 1)
	if n := count(first, 0, 2, 0); n == 0 {
		t.Error("no reddish pixel in the red-span cells of row 0")
	}
	if n := count(first, 4, 5, 0); n != 0 {
		t.Errorf("%d reddish pixels in unstyled cells of row 0", n)
	}
	// Red background: X is in cell column 2 of row 1 because the wide rune
	// takes two cells; column 1 (the wide rune's second half) is not red.
	if p := cell(2, 1); !reddish(first.At(p.X+1, p.Y+1)) {
		t.Errorf("pixel %v in the red-background cell is %v, want reddish", p, first.At(p.X+1, p.Y+1))
	}
	if p := cell(1, 1); reddish(first.At(p.X+1, p.Y+1)) {
		t.Errorf("pixel %v in the wide rune's cell is reddish, want theme background", p)
	}
	if p := cell(3, 1); reddish(first.At(p.X+1, p.Y+1)) {
		t.Errorf("pixel %v after the red cell is reddish, want theme background", p)
	}
	// The last screen still has red in row 0 and differs from the first.
	last := compose(t, a, 2)
	if n := count(last, 0, 2, 0); n == 0 {
		t.Error("no reddish pixel in row 0 of the last frame")
	}
	if bytes.Equal(first.Pix, last.Pix) {
		t.Error("the last frame equals the first screen")
	}
}

func TestExportDefaultOutputPath(t *testing.T) {
	in := writeExportFixture(t)
	code, stderr := runExportCmd(t, in)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	want := strings.TrimSuffix(in, ".shot") + ".apng"
	if _, err := os.Stat(want); err != nil {
		t.Errorf("default output missing: %v", err)
	}
	if !strings.HasSuffix(stderr, " to "+want+"\n") {
		t.Errorf("stderr = %q, want it to end with the output path %s", stderr, want)
	}
}

func TestExportOptionsChangeOutput(t *testing.T) {
	in := writeExportFixture(t)
	dir := t.TempDir()
	themeFile := filepath.Join(dir, "t.json")
	custom := theme.Light()
	custom.Name = "custom"
	writeThemeFile(t, themeFile, custom)

	decodeSize := func(t *testing.T, args ...string) (image.Rectangle, color.Color, uint32) {
		t.Helper()
		out := filepath.Join(dir, "o.apng")
		if code, stderr := runExportCmd(t, append([]string{in, "-o", out}, args...)...); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		a, err := kettek.DecodeAll(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		return a.Frames[0].Image.Bounds(), a.Frames[0].Image.At(0, 0), uint32(a.LoopCount)
	}
	base, bgDark, _ := decodeSize(t)
	scaled, _, _ := decodeSize(t, "--scale", "2")
	// Cell sizes are rounded to whole pixels, so only roughly double.
	if scaled.Dx() < base.Dx()*7/4 || scaled.Dy() < base.Dy()*7/4 {
		t.Errorf("--scale 2 gave %v, want about twice %v", scaled, base)
	}
	padded, _, _ := decodeSize(t, "--padding", "20")
	if padded.Dx() != base.Dx()+24 || padded.Dy() != base.Dy()+24 {
		t.Errorf("--padding 20 gave %v, want 24px more than %v", padded, base)
	}
	_, bgLight, _ := decodeSize(t, "--theme", "light")
	if bgLight == bgDark {
		t.Errorf("--theme light left the background %v", bgDark)
	}
	_, bgFile, _ := decodeSize(t, "--theme", themeFile)
	if bgFile != bgLight {
		t.Errorf("--theme FILE background = %v, want the light theme's %v", bgFile, bgLight)
	}
	if _, _, loops := decodeSize(t, "--loops", "3"); loops != 3 {
		t.Errorf("--loops 3 gave %d", loops)
	}
}

func writeThemeFile(t *testing.T, path string, th theme.Theme) {
	t.Helper()
	hex := func(c color.RGBA) string {
		const digits = "0123456789abcdef"
		b := []byte{'#', 0, 0, 0, 0, 0, 0}
		for i, v := range []uint8{c.R, c.G, c.B} {
			b[1+2*i], b[2+2*i] = digits[v>>4], digits[v&15]
		}
		return `"` + string(b) + `"`
	}
	pal := make([]string, len(th.Palette))
	for i, c := range th.Palette {
		pal[i] = hex(c)
	}
	js := `{"name":"` + th.Name + `","foreground":` + hex(th.Foreground) + `,"background":` + hex(th.Background) +
		`,"cursor":` + hex(th.Cursor) + `,"palette":[` + strings.Join(pal, ",") + `]}`
	if err := os.WriteFile(path, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportRejectsBadValues(t *testing.T) {
	in := writeExportFixture(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"cursor", []string{"--cursor", "blink"}, "invalid --cursor"},
		{"font size", []string{"--font-size", "0"}, "invalid --font-size"},
		{"scale", []string{"--scale", "0"}, "invalid --scale"},
		{"padding", []string{"--padding", "-1"}, "invalid --padding"},
		{"speed", []string{"--speed", "0"}, "invalid --speed"},
		{"idle limit", []string{"--idle-limit", "-1s"}, "invalid --idle-limit"},
		{"hold", []string{"--hold", "-1s"}, "invalid --hold"},
		{"loops", []string{"--loops", "-1"}, "invalid --loops"},
		{"min delay", []string{"--min-delay", "-1ms"}, "invalid --min-delay"},
		{"fps zero", []string{"--fps", "0"}, "invalid --fps 0: must be between 1 and 1000"},
		{"fps too high", []string{"--fps", "1001"}, "invalid --fps 1001"},
		{"unknown theme", []string{"--theme", "no-such-theme"}, `unknown --theme "no-such-theme"`},
		{"missing input", nil, "no such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.apng")
			input := in
			if tt.name == "missing input" {
				input = filepath.Join(t.TempDir(), "missing.shot")
			}
			args := append([]string{input, "-o", out}, tt.args...)
			code, stderr := runExportCmd(t, args...)
			if code != 1 || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, stderr %q, want exit 1 containing %q", code, stderr, tt.want)
			}
			if _, err := os.Stat(out); err == nil {
				t.Error("output file exists after a failed export")
			}
		})
	}
}

func TestExportBadThemeFile(t *testing.T) {
	in := writeExportFixture(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stderr := runExportCmd(t, in, "--theme", bad)
	if code != 1 || !strings.Contains(stderr, "invalid theme file") {
		t.Errorf("exit %d, stderr %q, want an invalid theme file error", code, stderr)
	}
}

func TestExportRefusesToOverwriteInput(t *testing.T) {
	in := writeExportFixture(t)
	before, _ := os.ReadFile(in)
	code, stderr := runExportCmd(t, in, "-o", in)
	if code != 1 || !strings.Contains(stderr, "overwrite the input") {
		t.Errorf("exit %d, stderr %q, want an overwrite error", code, stderr)
	}
	after, _ := os.ReadFile(in)
	if !bytes.Equal(before, after) {
		t.Error("input recording was modified")
	}
}

// A recording that fails midway must leave no output file and no temporary
// file, and must keep an existing output untouched.
func TestExportFailureLeavesNoPartialOutput(t *testing.T) {
	data, firstEnd := twoBlockRecording(t)
	data[firstEnd-10] ^= 0xff // corrupt the first block, which is followed by more data
	in := writeFile(t, "broken.shot", data)
	dir := filepath.Dir(in)
	out := filepath.Join(dir, "out.apng")

	code, stderr := runExportCmd(t, in, "-o", out)
	if code != 1 || !strings.Contains(stderr, "read frame 1") || !strings.Contains(stderr, "corrupt") {
		t.Errorf("exit %d, stderr %q, want a 'read frame 1' corrupt error", code, stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "broken.shot" {
			t.Errorf("unexpected file %s left in the output directory", e.Name())
		}
	}

	if err := os.WriteFile(out, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := runExportCmd(t, in, "-o", out); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if got, _ := os.ReadFile(out); string(got) != "previous" {
		t.Errorf("existing output was replaced by %q", got)
	}
}

func TestExportRejectsNonRecordings(t *testing.T) {
	tests := map[string][]byte{
		"json": []byte(`{"version":1,"cols":10,"rows":2}` + "\n"),
		"text": []byte("hello, this is not a recording\n"),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			in := writeFile(t, name+".shot", data)
			out := filepath.Join(t.TempDir(), "out.apng")
			code, stderr := runExportCmd(t, in, "-o", out)
			if code != 1 || !strings.Contains(stderr, "not a shellshot recording (bad magic)") {
				t.Errorf("exit %d, stderr %q, want a bad magic error", code, stderr)
			}
			if _, err := os.Stat(out); err == nil {
				t.Error("output file exists after a failed export")
			}
		})
	}
}

// Known limitation: the APNG canvas is fixed, so a recording that resizes
// cannot be exported yet.
func TestExportRejectsResizingRecording(t *testing.T) {
	in := writeFile(t, "resize.shot", resizingRecording(t))
	out := filepath.Join(t.TempDir(), "out.apng")
	code, stderr := runExportCmd(t, in, "-o", out)
	want := "recording changes size mid-way (10x2 -> 14x3); export does not support resizes yet"
	if code != 1 || !strings.Contains(stderr, want) {
		t.Errorf("exit %d, stderr %q, want exit 1 containing %q", code, stderr, want)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("output file exists after a failed export")
	}
}

// A torn recording exports up to the tear and says so; it is not an error.
func TestExportTruncatedExportsValidPrefix(t *testing.T) {
	data, firstEnd := twoBlockRecording(t)
	in := writeFile(t, "torn.shot", data[:len(data)-3])
	out := filepath.Join(t.TempDir(), "out.apng")
	code, stderr := runExportCmd(t, in, "-o", out)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if want := fmt.Sprintf("shellshot: recording is truncated (%d bytes valid)\n", firstEnd); !strings.HasPrefix(stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if a := decodeAPNG(t, out); len(a.Frames) == 0 {
		t.Error("no frames exported")
	}
}

// Idle time costs one step: a five minute pause exports a handful of frames,
// and the pause is capped by --idle-limit.
func TestExportIdleGap(t *testing.T) {
	in := writeFile(t, "idle.shot", idleGapRecording(t))
	out := filepath.Join(t.TempDir(), "out.apng")
	start := time.Now()
	code, stderr := runExportCmd(t, in, "-o", out)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("export took %v", elapsed)
	}
	a := decodeAPNG(t, out)
	if n := len(a.Frames); n < 2 || n > 5 {
		t.Errorf("%d frames for a recording with two bursts, want 2 to 5", n)
	}
	var total time.Duration
	for _, f := range a.Frames {
		total += time.Duration(f.DelayNumerator) * time.Second / time.Duration(f.DelayDenominator)
	}
	if total > 10*time.Second {
		t.Errorf("animation lasts %v, want the idle gap capped well below 5 minutes", total)
	}
}

// --fps sets the sampling grid: two writes 20ms apart share one frame at
// 10 fps and get a frame each at 1000 fps.
func TestExportFPSChangesFrameCount(t *testing.T) {
	data := buildRecording(t, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(10*time.Millisecond, []byte("a")))
		must(t, w.Output(30*time.Millisecond, []byte("b")))
	})
	in := writeFile(t, "fps.shot", data)
	for fps, want := range map[string]string{"10": "wrote 2 frames", "1000": "wrote 3 frames"} {
		out := filepath.Join(t.TempDir(), "out.apng")
		code, stderr := runExportCmd(t, in, "-o", out, "--fps", fps, "--min-delay", "0")
		if code != 0 || !strings.HasPrefix(stderr, want) {
			t.Errorf("--fps %s: exit %d, stderr %q, want %q", fps, code, stderr, want)
		}
	}
}
