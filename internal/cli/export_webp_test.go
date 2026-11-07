package cli

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// webpChunks returns the fourCCs of the top-level chunks of a RIFF WebP file.
func webpChunks(t *testing.T, b []byte) []string {
	t.Helper()
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		t.Fatalf("not a WebP file: % x", b[:min(len(b), 12)])
	}
	var chunks []string
	for rest := b[12:]; len(rest) >= 8; {
		size := int(binary.LittleEndian.Uint32(rest[4:]))
		chunks = append(chunks, string(rest[:4]))
		rest = rest[min(len(rest), 8+size+size&1):]
	}
	return chunks
}

func TestExportWebP(t *testing.T) {
	in := writeExportFixture(t)
	dir := t.TempDir()
	for _, tc := range []struct {
		name, out string
		args      []string
	}{
		{"webp extension", "a.webp", nil},
		{"--format webp", "b.bin", []string{"--format", "webp"}},
	} {
		out := filepath.Join(dir, tc.out)
		if code, stderr := runExportCmd(t, append([]string{in, "-o", out, "--loops", "2"}, tc.args...)...); code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", tc.name, code, stderr)
		}
		chunks := webpChunks(t, mustRead(t, out))
		if len(chunks) < 4 || chunks[0] != "VP8X" || chunks[1] != "ANIM" || chunks[2] != "ANMF" {
			t.Errorf("%s: chunks %v, want VP8X ANIM ANMF...", tc.name, chunks)
		}
	}
}

func TestExportWebPDefaultOutputName(t *testing.T) {
	in := writeFile(t, "z.shot", mustRead(t, writeExportFixture(t)))
	if code, stderr := runExportCmd(t, in, "--format", "webp"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(in), "z.webp")); err != nil {
		t.Errorf("default output z.webp missing: %v", err)
	}
}
