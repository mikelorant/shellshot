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
		{"--svg-emoji with png", "o.png", []string{"--svg-emoji", "image"}, "--svg-emoji only applies to svg"},
		{"bad --svg-emoji", "o.svg", []string{"--svg-emoji", "png"}, "--svg-emoji"},
		{"empty --svg-emoji", "o.svg", []string{"--svg-emoji", ""}, "--svg-emoji"},
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
	if code == 0 || !strings.Contains(stderr, "supported: png, gif, svg, webp") {
		t.Errorf("exit %d, stderr %q; want the supported list with svg", code, stderr)
	}
}

func TestExportSVGEmoji(t *testing.T) {
	t.Parallel()
	in := writeEmojiRecording(t)
	dir := t.TempDir()
	export := func(name string, flags ...string) string {
		t.Helper()
		out := filepath.Join(dir, name+".svg")
		code, stderr := runExportCmd(t, append([]string{in, "-o", out}, flags...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", name, code, stderr)
		}
		doc := mustRead(t, out)
		wellFormed(t, doc)
		return string(doc)
	}
	for _, frame := range [][]string{{"--frame", "last"}, nil} {
		kind := "static"
		if frame == nil {
			kind = "animated"
		}
		def := export("default-"+kind, frame...)
		text := export("text-"+kind, append([]string{"--svg-emoji", "text"}, frame...)...)
		if def != text {
			t.Errorf("%s: --svg-emoji text differs from the default", kind)
		}
		if strings.Contains(def, "<image") {
			t.Errorf("%s: the default output has an <image>", kind)
		}
		for _, e := range exportEmoji[:8] {
			if !strings.Contains(def, ">"+e+"<") {
				t.Errorf("%s: the default output lacks %q as text", kind, e)
			}
		}
		img := export("image-"+kind, append([]string{"--svg-emoji", "image"}, frame...)...)
		if !strings.Contains(img, "<image") || strings.Contains(img, "\U0001F600") {
			t.Errorf("%s: --svg-emoji image has no embedded emoji or still has the text", kind)
		}
		if len(img) <= len(def) {
			t.Errorf("%s: image output (%d B) is not larger than text output (%d B)", kind, len(img), len(def))
		}
	}
	paths := export("paths", "--frame", "last", "--svg-text", "paths")
	if strings.Contains(paths, "<image") || !strings.Contains(paths, ">\U0001F600<") {
		t.Error("--svg-text paths with text emoji does not draw the emoji as <text>")
	}
}

func TestExportSVGEmojiFlagsAreNotedWithTextEmoji(t *testing.T) {
	t.Parallel()
	in := writeEmojiRecording(t)
	dir := t.TempDir()
	for _, tc := range []struct {
		name     string
		flags    []string
		wantNote bool
	}{
		{"no flags", nil, false},
		{"set with text", []string{"--emoji-set", "noto"}, true},
		{"font with text", []string{"--emoji-font", notoFontFile(t)}, true},
		{"set with image", []string{"--svg-emoji", "image", "--emoji-set", "noto"}, false},
		{"set with png", nil, false},
	} {
		out := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".svg")
		args := append([]string{in, "-o", out, "--frame", "last"}, tc.flags...)
		if tc.name == "set with png" {
			out = filepath.Join(dir, "set.png")
			args = []string{in, "-o", out, "--emoji-set", "noto"}
		}
		code, stderr := runExportCmd(t, args...)
		if code != 0 {
			t.Errorf("%s: exit %d, stderr %q", tc.name, code, stderr)
		}
		if got := strings.Contains(stderr, "have no effect on svg output with --svg-emoji text"); got != tc.wantNote {
			t.Errorf("%s: note = %v, want %v (stderr %q)", tc.name, got, tc.wantNote, stderr)
		}
	}
}

func TestExportHelpExplainsSVGEmoji(t *testing.T) {
	t.Parallel()
	cmd := newExportCmd()
	if f := cmd.Flags().Lookup("svg-emoji"); f == nil || f.DefValue != "text" {
		t.Errorf("--svg-emoji default = %v, want text", f)
	}
	for _, name := range []string{"emoji-set", "emoji-font"} {
		f := cmd.Flags().Lookup(name)
		if f == nil || !strings.Contains(f.Usage, "--svg-emoji") {
			t.Errorf("--%s help does not mention --svg-emoji: %v", name, f)
		}
	}
}
