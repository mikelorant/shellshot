package cli

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wellFormed fails the test unless doc is well-formed XML.
func wellFormed(t *testing.T, doc []byte) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(string(doc)))
	for {
		if _, err := d.Token(); err != nil {
			if err.Error() == "EOF" {
				return
			}
			t.Fatalf("svg is not well-formed: %v", err)
		}
	}
}

func TestExportSVGIsAnimatedByDefaultAndStaticWithFrame(t *testing.T) {
	t.Parallel()
	in := writeExportFixture(t)
	dir := t.TempDir()
	animated := filepath.Join(dir, "a.svg")
	if code, stderr := runExportCmd(t, in, "-o", animated); code != 0 {
		t.Fatalf("animated: exit %d, stderr %q", code, stderr)
	}
	a := mustRead(t, animated)
	wellFormed(t, a)
	if !strings.Contains(string(a), "<svg") || !strings.Contains(string(a), "@keyframes") {
		t.Errorf("default .svg output is not an animation:\n%.400s", a)
	}
	for _, frame := range []string{"first", "last", "0.1s"} {
		out := filepath.Join(dir, "s-"+frame+".svg")
		if code, stderr := runExportCmd(t, in, "-o", out, "--frame", frame); code != 0 {
			t.Fatalf("--frame %s: exit %d, stderr %q", frame, code, stderr)
		}
		s := mustRead(t, out)
		wellFormed(t, s)
		if strings.Contains(string(s), "@keyframes") {
			t.Errorf("--frame %s output still animates", frame)
		}
	}
}

func TestExportSVGTextPaths(t *testing.T) {
	t.Parallel()
	in := writeExportFixture(t)
	dir := t.TempDir()
	text := filepath.Join(dir, "t.svg")
	paths := filepath.Join(dir, "p.svg")
	if code, stderr := runExportCmd(t, in, "-o", text, "--frame", "last"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if code, stderr := runExportCmd(t, in, "-o", paths, "--frame", "last", "--svg-text", "paths"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	wellFormed(t, mustRead(t, paths))
	if strings.Contains(string(mustRead(t, paths)), "<text") {
		t.Error("--svg-text paths output has <text> elements")
	}
}

func TestExportSVGFlagsAreRejectedForOtherFormats(t *testing.T) {
	t.Parallel()
	in := writeExportFixture(t)
	dir := t.TempDir()
	cases := []struct {
		name string
		out  string
		args []string
		want string
	}{
		{"--frame with png", "o.png", []string{"--frame", "last"}, "--frame only applies to svg"},
		{"--svg-text with png", "o.png", []string{"--svg-text", "paths"}, "--svg-text only applies to svg"},
		{"bad --frame", "o.svg", []string{"--frame", "soon"}, "--frame"},
		{"bad --svg-text", "o.svg", []string{"--svg-text", "x"}, "--svg-text"},
	}
	for _, tc := range cases {
		out := filepath.Join(dir, tc.out)
		code, stderr := runExportCmd(t, append([]string{in, "-o", out}, tc.args...)...)
		if code == 0 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: exit %d, stderr %q; want a failure mentioning %q", tc.name, code, stderr, tc.want)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: output written despite the error", tc.name)
		}
	}
}

func TestExportFormatErrorListsSVG(t *testing.T) {
	t.Parallel()
	in := writeExportFixture(t)
	code, stderr := runExportCmd(t, in, "-o", filepath.Join(t.TempDir(), "o.png"), "--format", "bogus")
	if code == 0 || !strings.Contains(stderr, "supported: apng, gif, svg, webp") {
		t.Errorf("exit %d, stderr %q; want the supported list with svg", code, stderr)
	}
}
