package cli

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
)

// writeCursorMovesFixture writes a recording whose screens "", "a", "a" with
// the cursor elsewhere (twice) and "ab" differ in the snapshot but, with
// --cursor none, not in the pixels: five timeline steps, three distinct
// pictures.
func writeCursorMovesFixture(t *testing.T) string {
	t.Helper()
	data := buildRecording(t, head.Head{Cols: 10, Rows: 3}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(100*time.Millisecond, []byte("a")))
		must(t, w.Output(300*time.Millisecond, []byte("\x1b[1;1H")))
		must(t, w.Output(500*time.Millisecond, []byte("\x1b[3;1H")))
		must(t, w.Output(700*time.Millisecond, []byte("\x1b[1;2Hb")))
		must(t, w.Exit(700*time.Millisecond, 0))
	})
	return writeFile(t, "cursor-moves.shot", data)
}

// wroteLine is the "wrote N frames" line export prints.
func wroteLine(n int) string { return fmt.Sprintf("wrote %d frames (", n) }

// The frame count export prints is the number of frames in the file, not the
// number of timeline steps: steps that render the same picture are one frame.
func TestExportReportsTheFramesInTheFile(t *testing.T) {
	t.Parallel()
	in := writeCursorMovesFixture(t)
	dir := t.TempDir()
	flags := []string{"--cursor", "none", "--min-delay", "0", "--keep-leading-blank", "--hold", "1s"}

	t.Run("png", func(t *testing.T) {
		// The default poster adds an image that is not a frame.
		for _, extra := range [][]string{nil, {"--poster", "none"}} {
			out := filepath.Join(dir, fmt.Sprintf("%d.png", len(extra)))
			code, stderr := runExportCmd(t, append([]string{in, "-o", out}, append(flags, extra...)...)...)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			a := decodeAnimatedPNG(t, out)
			if len(a.Frames) != 3 {
				t.Fatalf("file holds %d frames, want 3 (fixture)", len(a.Frames))
			}
			if want := wroteLine(len(a.Frames)); !bytes.HasPrefix([]byte(stderr), []byte(want)) {
				t.Errorf("%v: stderr = %q, want it to start with %q", extra, stderr, want)
			}
		}
	})
	t.Run("gif", func(t *testing.T) {
		out := filepath.Join(dir, "a.gif")
		code, stderr := runExportCmd(t, append([]string{in, "-o", out}, flags...)...)
		if code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		n := len(decodeGIFFile(t, out).Image)
		if want := wroteLine(n); !bytes.HasPrefix([]byte(stderr), []byte(want)) || n != 3 {
			t.Errorf("file holds %d frames (want 3); stderr = %q, want it to start with %q", n, stderr, want)
		}
	})
	t.Run("webp", func(t *testing.T) {
		out := filepath.Join(dir, "a.webp")
		code, stderr := runExportCmd(t, append([]string{in, "-o", out}, flags...)...)
		if code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		n := len(slices.DeleteFunc(webpChunks(t, mustRead(t, out)), func(c string) bool { return c != "ANMF" }))
		if want := wroteLine(n); !bytes.HasPrefix([]byte(stderr), []byte(want)) || n != 3 {
			t.Errorf("file holds %d frames (want 3); stderr = %q, want it to start with %q", n, stderr, want)
		}
	})
}
