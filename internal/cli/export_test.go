package cli

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kettek "github.com/kettek/apng"

	"github.com/michaellorant/shellshot/internal/recording"
	"github.com/michaellorant/shellshot/internal/render/font"
	"github.com/michaellorant/shellshot/internal/render/raster"
	"github.com/michaellorant/shellshot/internal/render/theme"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

const (
	exportFixtureCols = 12
	exportFixtureRows = 2
)

// writeExportFixture writes a two-frame, 12x2 recording and returns its path.
// Row 0 is "red ok" with the first three runes red; row 1 is a wide rune
// followed by "X" on a red background, so X sits in cell column 2. The second
// frame, 500ms later, changes row 0 only.
func writeExportFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.shot")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := recording.NewWriter(f, recording.Header{
		Cols: exportFixtureCols, Rows: exportFixtureRows,
		Command: []string{"demo"}, Started: time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	red := snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}
	frame := func(at time.Duration, row0 string) snapshot.Frame {
		return snapshot.Frame{At: at, Snapshot: snapshot.Snapshot{
			Cols: exportFixtureCols, Rows: exportFixtureRows,
			Lines: []string{row0, "世X"},
			Spans: [][]snapshot.Span{
				{{Start: 0, End: 3, Style: snapshot.Style{Fg: red}}},
				{{Start: 1, End: 2, Style: snapshot.Style{Bg: red}}},
			},
			Widths: [][]snapshot.Wide{nil, {{Rune: 0, Cells: 2}}},
		}}
	}
	for _, fr := range []snapshot.Frame{frame(0, "red ok"), frame(500*time.Millisecond, "red done")} {
		if err := w.WriteFrame(fr); err != nil {
			t.Fatal(err)
		}
	}
	return path
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
	if !strings.HasPrefix(stderr, "wrote 2 frames (") || !strings.HasSuffix(stderr, " KB) to "+out+"\n") {
		t.Errorf("stderr = %q, want a 'wrote 2 frames (N KB) to %s' line", stderr, out)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	a, err := kettek.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

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

	if len(a.Frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(a.Frames))
	}
	if a.LoopCount != 0 {
		t.Errorf("LoopCount = %d, want 0 (forever)", a.LoopCount)
	}
	first := a.Frames[0].Image
	if b := first.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("first frame is %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
	for i, want := range []time.Duration{500 * time.Millisecond, time.Second} {
		f := a.Frames[i]
		if got := time.Duration(f.DelayNumerator) * time.Second / time.Duration(f.DelayDenominator); got != want {
			t.Errorf("frame %d delay = %v, want %v", i, got, want)
		}
	}
	if b := a.Frames[1].Image.Bounds(); b.Dy() >= h {
		t.Errorf("second frame is %dx%d, want a delta frame shorter than %d", b.Dx(), b.Dy(), h)
	}

	cell := func(col, row int) image.Point {
		return image.Pt(raster.DefaultPadding+col*cellW, raster.DefaultPadding+row*cellH)
	}
	// Red text: some pixel in cells 0-2 of row 0 is reddish; the plain "ok"
	// text in cells 4-5 has none.
	count := func(col0, col1, row int) int {
		n := 0
		o0, o1 := cell(col0, row), cell(col1, row)
		for y := o0.Y; y < o0.Y+cellH; y++ {
			for x := o0.X; x < o1.X+cellW; x++ {
				if reddish(first.At(x, y)) {
					n++
				}
			}
		}
		return n
	}
	if n := count(0, 2, 0); n == 0 {
		t.Error("no reddish pixel in the red-span cells of row 0")
	}
	if n := count(4, 5, 0); n != 0 {
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
	dir := t.TempDir()
	in := filepath.Join(dir, "broken.shot")
	good, err := os.ReadFile(writeExportFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.SplitAfter(good, []byte("\n"))
	broken := append(append([]byte{}, lines[0]...), lines[1]...)
	broken = append(broken, []byte("{not json\n")...)
	if err := os.WriteFile(in, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.apng")

	code, stderr := runExportCmd(t, in, "-o", out)
	if code != 1 || !strings.Contains(stderr, "read frame 1") {
		t.Errorf("exit %d, stderr %q, want a 'read frame 1' error", code, stderr)
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
