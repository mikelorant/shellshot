package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportHelpListsFormat(t *testing.T) {
	usage := newExportCmd().Flags().FlagUsages()
	for _, want := range []string{"--format", "apng", "from the extension of -o"} {
		if !strings.Contains(usage, want) {
			t.Errorf("export flag help lacks %q:\n%s", want, usage)
		}
	}
}

// Every way of choosing apng writes the same bytes.
func TestExportFormatAPNGIsChosenByNameExtensionOrDefault(t *testing.T) {
	in := writeExportFixture(t)
	dir := t.TempDir()
	cases := []struct {
		name string
		out  string
		args []string
	}{
		{"png extension", "a.png", nil},
		{"apng extension", "b.apng", nil},
		{"upper-case extension", "c.APNG", nil},
		{"--format apng", "d.png", []string{"--format", "apng"}},
		{"--format APNG", "e.png", []string{"--format", "APNG"}},
		{"--format overrides the extension", "f.bin", []string{"--format", "apng"}},
		{"no extension", "g", nil},
	}
	var first []byte
	for _, tc := range cases {
		out := filepath.Join(dir, tc.out)
		if code, stderr := runExportCmd(t, append([]string{in, "-o", out}, tc.args...)...); code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", tc.name, code, stderr)
		}
		got := mustRead(t, out)
		if a := decodeAPNG(t, out); len(a.Frames) < 2 {
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
	in := writeFile(t, "y.shot", mustRead(t, writeExportFixture(t)))
	if code, stderr := runExportCmd(t, in, "--format", "apng"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(in), "y.png")); err != nil {
		t.Errorf("default output y.png missing: %v", err)
	}
}

func TestExportRejectsUnknownFormat(t *testing.T) {
	in := writeExportFixture(t)
	dir := t.TempDir()
	cases := []struct {
		name string
		out  string
		args []string
		want []string
	}{
		{"unknown --format", "o.png", []string{"--format", "gif"}, []string{`unknown --format "gif"`, "supported: apng"}},
		{"empty-looking --format", "o.png", []string{"--format", " "}, []string{"unknown --format", "supported: apng"}},
		{"unknown extension", "o.gif", nil, []string{`extension ".gif"`, "o.gif", "--format", "supported: apng"}},
		{"unknown upper-case extension", "o.GIF", nil, []string{`extension ".GIF"`, "supported: apng"}},
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
