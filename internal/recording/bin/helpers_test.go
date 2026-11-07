package bin

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin/chunk"
	"github.com/michaellorant/shellshot/internal/recording/bin/codec"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

var update = flag.Bool("update", false, "regenerate testdata/basic.shot")

const goldenPath = "testdata/basic.shot"

// fakeClock is a manually advanced clock for Writer.Now and Writer.Tick.
type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

var errKilled = errors.New("process killed")

// recordingWriter keeps every Write call and can die after limit bytes,
// leaving a torn chunk. A negative limit never dies.
type recordingWriter struct {
	writes [][]byte
	buf    bytes.Buffer
	limit  int
}

func newRecordingWriter() *recordingWriter { return &recordingWriter{limit: -1} }

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, bytes.Clone(p))
	if w.limit >= 0 && w.buf.Len()+len(p) > w.limit {
		n := w.limit - w.buf.Len()
		w.buf.Write(p[:n])
		return n, errKilled
	}
	return w.buf.Write(p)
}

// syncingWriter adds a Sync method to recordingWriter.
type syncingWriter struct {
	recordingWriter
	syncs int
}

func (w *syncingWriter) Sync() error { w.syncs++; return nil }

// wantEvent is the expected form of a decoded event.
type wantEvent struct {
	at   time.Duration
	kind event.Kind
	data string
}

func drain(t *testing.T, r *Reader) ([]Event, error) {
	t.Helper()
	var evs []Event
	for {
		e, err := r.Next()
		if err != nil {
			return evs, err
		}
		e.Data = bytes.Clone(e.Data)
		evs = append(evs, e)
		if len(evs) > 1<<20 {
			t.Fatal("reader does not terminate")
		}
	}
}

func toWant(evs []Event) []wantEvent {
	out := make([]wantEvent, len(evs))
	for i, e := range evs {
		out[i] = wantEvent{e.At, e.Kind, string(e.Data)}
	}
	return out
}

func equalWant(a, b []wantEvent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func testHead() head.Head {
	return head.Head{Cols: 80, Rows: 24}
}

// Golden recording --------------------------------------------------------

var goldenDict = bytes.Repeat([]byte("\x1b[0m\x1b[1;32muser@host\x1b[0m:\x1b[34m~\x1b[0m$ "), 8)

func goldenHead() head.Head {
	return head.Head{
		Cols: 80, Rows: 24,
		StartedUnixNS: 1_700_000_000_000_000_000,
		Term:          "xterm-256color",
		Recorder:      "shellshot-test",
		Flags:         head.FlagInputRecorded,
	}
}

func goldenTheme() *head.Theme {
	t := &head.Theme{FG: head.Color{R: 0xcc, G: 0xcc, B: 0xcc}, BG: head.Color{R: 0x10, G: 0x10, B: 0x10}, HasCursor: true}
	for i := range t.Palette {
		t.Palette[i] = head.Color{R: uint8(i), G: uint8(255 - i), B: uint8(i * 7)}
	}
	return t
}

func goldenOptions(clk *fakeClock) WriterOptions {
	return WriterOptions{
		Codec:         codec.Zstd,
		Dict:          goldenDict,
		DictID:        7,
		Theme:         goldenTheme(),
		Meta:          map[string]string{"title": "basic", "k": "v"},
		FlushInterval: 2 * time.Second,
		Now:           clk.Now,
	}
}

// goldenScenario writes four blocks: one by Flush, one by Tick, one by the
// interval check in WriteEvent and one by Close.
func goldenScenario(w *Writer, clk *fakeClock) error {
	steps := []func() error{
		func() error { return w.Output(0, []byte("\x1b[2J\x1b[Hhello\r\n")) },
		func() error { return w.Output(10*time.Millisecond, []byte("world\r\n")) },
		func() error { return w.Resize(20*time.Millisecond, 100, 30, 8, 16) },
		w.Flush, // block 1
		func() error { return w.Input(time.Second, []byte("ls\r")) },
		func() error { return w.Output(time.Second+100*time.Microsecond, []byte("ls\r\nfile1 file2\r\n")) },
		func() error { clk.Advance(3 * time.Second); return w.Tick(clk.Now()) }, // block 2
		func() error { return w.Mark(5*time.Second, "checkpoint") },
		func() error { return w.Output(5*time.Second+250*time.Microsecond, []byte("more\r\n")) },
		func() error { clk.Advance(2 * time.Second); return w.Output(7*time.Second, []byte("bye\r\n")) }, // block 3 flushed first
		func() error { return w.Exit(7*time.Second+time.Millisecond, 3) },
		w.Close, // block 4
	}
	for _, s := range steps {
		if err := s(); err != nil {
			return err
		}
	}
	return nil
}

func goldenWant() []wantEvent {
	return []wantEvent{
		{0, event.KindOut, "\x1b[2J\x1b[Hhello\r\n"},
		{10 * time.Millisecond, event.KindOut, "world\r\n"},
		{20 * time.Millisecond, event.KindResize, "\x64\x1e\x08\x10"},
		{time.Second, event.KindIn, "ls\r"},
		{time.Second + 100*time.Microsecond, event.KindOut, "ls\r\nfile1 file2\r\n"},
		{5 * time.Second, event.KindMark, "checkpoint"},
		{5*time.Second + 200*time.Microsecond, event.KindOut, "more\r\n"}, // 250µs floors to 2 ticks
		{7 * time.Second, event.KindOut, "bye\r\n"},
		{7*time.Second + time.Millisecond, event.KindExit, "\x06"},
	}
}

func buildGolden(t testing.TB) []byte {
	t.Helper()
	clk := newFakeClock()
	var buf bytes.Buffer
	w, err := NewWriter(&buf, goldenHead(), goldenOptions(clk))
	if err != nil {
		t.Fatal(err)
	}
	if err := goldenScenario(w, clk); err != nil {
		t.Fatal(err)
	}
	if got := w.Stats().Blocks; got != 4 {
		t.Fatalf("golden scenario wrote %d blocks, want 4", got)
	}
	return buf.Bytes()
}

func readGolden(t testing.TB) []byte {
	t.Helper()
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v (run go test -update ./internal/recording/bin to create it)", err)
	}
	return b
}

func TestGoldenRegenerates(t *testing.T) {
	got := buildGolden(t)
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if want := readGolden(t); !bytes.Equal(got, want) {
		t.Fatalf("writer output differs from %s (%d vs %d bytes); inspect, then run with -update", goldenPath, len(got), len(want))
	}
}

func TestGoldenReads(t *testing.T) {
	data := readGolden(t)
	r, err := NewReader(bytes.NewReader(data), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := drain(t, r)
	if err != io.EOF {
		t.Fatalf("end error = %v, want io.EOF", err)
	}
	if got := toWant(evs); !equalWant(got, goldenWant()) {
		t.Fatalf("events = %+v\nwant %+v", got, goldenWant())
	}
	if r.ValidBytes() != int64(len(data)) {
		t.Fatalf("ValidBytes = %d, want %d", r.ValidBytes(), len(data))
	}
	h := r.Head()
	if h.Cols != 80 || h.Rows != 24 || h.Term != "xterm-256color" || h.Flags&head.FlagInputRecorded == 0 {
		t.Fatalf("head = %+v", h)
	}
	if h.RequiredFeatures&head.FeatureBlockDictionaries == 0 {
		t.Fatal("HEAD does not require the dictionary feature")
	}
	if th := r.Theme(); th == nil || *th != *goldenTheme() {
		t.Fatal("theme mismatch")
	}
	if m := r.Meta(); len(m) != 2 || m["title"] != "basic" || m["k"] != "v" {
		t.Fatalf("meta = %v", m)
	}
}

// chunkSpan is one chunk of a file: [start, end).
type chunkSpan struct {
	typ        chunk.Type
	start, end int
}

func chunkSpans(t testing.TB, data []byte) []chunkSpan {
	t.Helper()
	cr := chunk.NewReader(bytes.NewReader(data[len(chunk.Magic):]), chunk.Options{})
	var spans []chunkSpan
	start := len(chunk.Magic)
	for {
		typ, _, err := cr.Next()
		if err == io.EOF {
			return spans
		}
		if err != nil {
			t.Fatal(err)
		}
		end := len(chunk.Magic) + int(cr.Offset())
		spans = append(spans, chunkSpan{typ, start, end})
		start = end
	}
}
