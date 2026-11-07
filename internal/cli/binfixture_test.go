package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
)

// fixedNow is the writer clock for fixtures: flushing is then decided by
// Flush, Close and the size threshold only, so the bytes are deterministic.
func fixedNow() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }

// buildRecording writes a recording with header h to memory. fill adds events
// and may call w.Flush to end a block; it also receives the buffer so it can
// note offsets. The writer is closed afterwards.
func buildRecording(t *testing.T, h head.Head, o bin.WriterOptions, fill func(w *bin.Writer, buf *bytes.Buffer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	o.Now = fixedNow
	w, err := bin.NewWriter(&buf, h, o)
	if err != nil {
		t.Fatal(err)
	}
	fill(w, &buf)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeFile stores data under a fresh temporary directory and returns the
// path.
func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// simpleRecording is a finished 10x2 recording: "first" at 10ms, then "second"
// on the next row 500ms in, then the exit.
func simpleRecording(t *testing.T) []byte {
	return buildRecording(t, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(10*time.Millisecond, []byte("first")))
		must(t, w.Output(500*time.Millisecond, []byte("\r\nsecond")))
		must(t, w.Exit(500*time.Millisecond, 0))
	})
}

// styledRecording is a finished 10x1 recording whose first three characters are red.
func styledRecording(t *testing.T) []byte {
	return buildRecording(t, head.Head{Cols: 10, Rows: 1}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(10*time.Millisecond, []byte("\x1b[31mred\x1b[0m ok")))
		must(t, w.Exit(10*time.Millisecond, 0))
	})
}

// idleGapRecording has output 5 minutes apart.
func idleGapRecording(t *testing.T) []byte {
	return buildRecording(t, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(10*time.Millisecond, []byte("before")))
		must(t, w.Output(5*time.Minute+10*time.Millisecond, []byte("\r\nafter")))
		must(t, w.Exit(5*time.Minute+20*time.Millisecond, 0))
	})
}

// resizingRecording grows from 10x2 to 14x3 half a second in.
func resizingRecording(t *testing.T) []byte {
	return buildRecording(t, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(10*time.Millisecond, []byte("small")))
		must(t, w.Resize(500*time.Millisecond, 14, 3, 0, 0))
		must(t, w.Output(510*time.Millisecond, []byte(" bigger")))
	})
}

// twoBlockRecording ends its first block after "one" and its second after
// "two". It returns the file and the offset at which the first block ends.
func twoBlockRecording(t *testing.T) (data []byte, firstBlockEnd int) {
	data = buildRecording(t, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{}, func(w *bin.Writer, buf *bytes.Buffer) {
		must(t, w.Output(10*time.Millisecond, []byte("one")))
		must(t, w.Flush())
		firstBlockEnd = buf.Len()
		must(t, w.Output(300*time.Millisecond, []byte("\r\ntwo")))
	})
	return data, firstBlockEnd
}

// interruptedRecording is what a recorder killed with SIGKILL right after a
// block was flushed leaves behind: "one" and "two" in two complete blocks, no
// EXIT event, and a writer that is never closed. It returns the file.
func interruptedRecording(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := bin.NewWriter(&buf, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{Now: fixedNow})
	must(t, err)
	must(t, w.Output(10*time.Millisecond, []byte("one")))
	must(t, w.Flush())
	must(t, w.Output(300*time.Millisecond, []byte("\r\ntwo")))
	must(t, w.Flush())
	return buf.Bytes()
}
