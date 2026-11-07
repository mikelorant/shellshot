package webp

import (
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWebpinfoAcceptsOutput checks a file with libwebp's webpinfo, an
// independent parser. It is skipped when webpinfo is not installed.
func TestWebpinfoAcceptsOutput(t *testing.T) {
	t.Parallel()
	tool, err := exec.LookPath("webpinfo")
	if err != nil {
		t.Skip("webpinfo not installed")
	}
	rng := rand.New(rand.NewSource(9))
	s, buf := newSink(t, 37, 21, Options{Loops: 2})
	for _, f := range sequence(rng, 37, 21, 8, false) {
		if err := s.AddFrame(f, 30*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.webp")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(tool, path).CombinedOutput()
	if err != nil {
		t.Fatalf("webpinfo: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "No error detected") {
		t.Errorf("webpinfo did not report success:\n%s", out)
	}
}
