package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportHelpListsFormat(t *testing.T) {
	t.Parallel()
	usage := newExportCmd().Flags().FlagUsages()
	for _, want := range []string{"--format", "png", "from the extension of -o"} {
		if !strings.Contains(usage, want) {
			t.Errorf("export flag help lacks %q:\n%s", want, usage)
		}
	}
}

// Every way of choosing png writes the same bytes.
func TestExportFormatPNGIsChosenByNameExtensionOrDefault(t *testing.T) {
	t.Parallel()
	in := writeExportFixture(t)
	dir := t.TempDir()
	cases := []struct {
		name string
		out  string
		args []string
	}{
		{"png extension", "a.png", nil},
		{".apng extension", "b.apng", nil},
		{"upper-case extension", "c.PNG", nil},
		{"--format png", "d.png", []string{"--format", "png"}},
		{"--format PNG", "e.png", []string{"--format", "PNG"}},
		{"--format overrides the extension", "f.bin", []string{"--format", "png"}},
		{"no extension", "g", nil},
	}
	var first []byte
	for _, tc := range cases {
		out := filepath.Join(dir, tc.out)
		if code, stderr := runExportCmd(t, append([]string{in, "-o", out}, tc.args...)...); code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", tc.name, code, stderr)
		}
		got := mustRead(t, out)
		if a := decodeAnimatedPNG(t, out); len(a.Frames) < 2 {
			t.Errorf("%s: %d frames, want an animation", tc.name, len(a.Frames))
		}
		if first == nil {
			first = got
		} else if !bytes.Equal(got, first) {
			t.Errorf("%s: bytes differ from the first output", tc.name)
		}
	}
}

func TestExportFormatFlagWithoutOutputUsesFormatExtension(t *testing.T) {
	t.Parallel()
	in := writeFile(t, "y.shot", mustRead(t, writeExportFixture(t)))
	if code, stderr := runExportCmd(t, in, "--format", "png"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(in), "y.png")); err != nil {
		t.Errorf("default output y.png missing: %v", err)
	}
}

// "apng" is not a format name: --format apng fails and lists the formats.
func TestExportFormatAPNGIsNotAFormatName(t *testing.T) {
	t.Parallel()
	in := writeExportFixture(t)
	code, stderr := runExportCmd(t, in, "--format", "apng")
	if code == 0 || !strings.Contains(stderr, `unknown --format "apng"`) || !strings.Contains(stderr, "supported: png, gif, svg, webp") {
		t.Errorf("exit %d, stderr %q; want an unknown --format error listing png first", code, stderr)
	}
}

func TestExportRejectsUnknownFormat(t *testing.T) {
	t.Parallel()
	in := writeExportFixture(t)
	dir := t.TempDir()
	cases := []struct {
		name string
		out  string
		args []string
		want []string
	}{
		{"unknown --format", "o.png", []string{"--format", "tiff"}, []string{`unknown --format "tiff"`, "supported: png"}},
		{"empty-looking --format", "o.png", []string{"--format", " "}, []string{"unknown --format", "supported: png"}},
		{"unknown extension", "o.tiff", nil, []string{`extension ".tiff"`, "o.tiff", "--format", "supported: png"}},
		{"unknown upper-case extension", "o.TIFF", nil, []string{`extension ".TIFF"`, "supported: png"}},
	}
	for _, tc := range cases {
		out := filepath.Join(dir, tc.out)
		code, stderr := runExportCmd(t, append([]string{in, "-o", out}, tc.args...)...)
		if code == 0 {
			t.Errorf("%s: exit 0, want a failure", tc.name)
		}
		for _, want := range tc.want {
			if !strings.Contains(stderr, want) {
				t.Errorf("%s: stderr %q lacks %q", tc.name, stderr, want)
			}
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: output was written despite the error", tc.name)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("%d files left in the output directory, want none", len(entries))
	}
}
