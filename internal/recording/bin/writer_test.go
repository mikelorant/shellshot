package bin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin/chunk"
	"github.com/michaellorant/shellshot/internal/recording/bin/codec"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

func mustWriter(t *testing.T, w io.Writer, h head.Head, o WriterOptions) *Writer {
	t.Helper()
	wr, err := NewWriter(w, h, o)
	if err != nil {
		t.Fatal(err)
	}
	return wr
}

func TestRoundTripCodecsAndDictionary(t *testing.T) {
	dict := bytes.Repeat([]byte("\x1b[0m\x1b[32mprompt\x1b[0m$ "), 16)
	for _, tc := range []struct {
		name string
		o    WriterOptions
	}{
		{"none", WriterOptions{Uncompressed: true}},
		{"flate", WriterOptions{Codec: codec.Flate}},
		{"zstd", WriterOptions{Codec: codec.Zstd}},
		{"default", WriterOptions{}},
		{"none+dict", WriterOptions{Uncompressed: true, Dict: dict}},
		{"flate+dict", WriterOptions{Codec: codec.Flate, Dict: dict, DictID: 9}},
		{"zstd+dict", WriterOptions{Codec: codec.Zstd, Dict: dict}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := head.Head{Cols: 120, Rows: 40, TickNS: 1_000_000, Flags: head.FlagInputRecorded}
			tc.o.FlushBytes = 2000 // several blocks
			var buf bytes.Buffer
			w := mustWriter(t, &buf, h, tc.o)

			rng := rand.New(rand.NewSource(1))
			var want []wantEvent
			var at time.Duration
			for i := 0; i < 400; i++ {
				at += time.Duration(rng.Intn(5000)) * time.Millisecond
				var kind event.Kind
				var data []byte
				switch rng.Intn(6) {
				case 0:
					kind, data = event.KindIn, []byte(fmt.Sprintf("key%d", i))
				case 1:
					kind, data = event.KindMark, []byte(fmt.Sprintf("mark %d é", i))
				case 2:
					kind, data = event.KindResize, event.ResizePayload{Cols: uint64(10 + i), Rows: 5, CellPxW: 8, CellPxH: 16}.Append(nil)
				case 3:
					kind, data = event.KindExit, nil
					data = event.NewExit(0, int64(i-200)).Data
				case 4:
					kind = event.Kind(64 + rng.Intn(10)) // ancillary passthrough
					data = bytes.Repeat([]byte{byte(i)}, rng.Intn(5))
				default:
					kind = event.KindOut
					data = bytes.Repeat([]byte("\x1b[32mhello\x1b[0m "), rng.Intn(30))
				}
				if err := w.WriteEvent(at, kind, data); err != nil {
					t.Fatal(err)
				}
				want = append(want, wantEvent{at.Truncate(time.Millisecond), kind, string(data)})
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if w.Stats().Blocks < 3 {
				t.Fatalf("only %d blocks; want several", w.Stats().Blocks)
			}
			if w.Stats().Events != uint64(len(want)) {
				t.Fatalf("Stats.Events = %d, want %d", w.Stats().Events, len(want))
			}

			r, err := NewReader(bytes.NewReader(buf.Bytes()), ReaderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Head().TickNS != 1_000_000 {
				t.Fatalf("TickNS = %d", r.Head().TickNS)
			}
			got, err := drain(t, r)
			if err != io.EOF {
				t.Fatalf("end error = %v", err)
			}
			if !equalWant(toWant(got), want) {
				t.Fatal("events differ after round trip")
			}
		})
	}
}

func TestNewWriterWritesHeaderInOneWrite(t *testing.T) {
	rw := newRecordingWriter()
	th := goldenTheme()
	w := mustWriter(t, rw, goldenHead(), WriterOptions{Dict: goldenDict, Theme: th, Meta: map[string]string{"a": "b"}})
	if len(rw.writes) != 1 {
		t.Fatalf("NewWriter made %d Write calls, want 1", len(rw.writes))
	}
	var types []chunk.Type
	for _, s := range chunkSpans(t, rw.buf.Bytes()) {
		types = append(types, s.typ)
	}
	want := []chunk.Type{chunk.TypeHead, chunk.TypeDict, chunk.TypeTheme, chunk.TypeMeta}
	if fmt.Sprint(types) != fmt.Sprint(want) {
		t.Fatalf("chunks = %v, want %v", types, want)
	}
	if w.Stats().BytesWritten != uint64(rw.buf.Len()) {
		t.Fatalf("BytesWritten = %d, want %d", w.Stats().BytesWritten, rw.buf.Len())
	}
}

func TestOneWritePerChunk(t *testing.T) {
	rw := newRecordingWriter()
	clk := newFakeClock()
	w := mustWriter(t, rw, goldenHead(), goldenOptions(clk))
	headerWrites := len(rw.writes)
	if err := goldenScenario(w, clk); err != nil {
		t.Fatal(err)
	}
	if got := len(rw.writes) - headerWrites; got != 4 {
		t.Fatalf("%d writes for 4 blocks", got)
	}
	for i, p := range rw.writes[headerWrites:] {
		if string(p[4:8]) != "EVNT" {
			t.Fatalf("write %d is not one whole EVNT chunk: % x", i, p[:8])
		}
	}
}

func TestFlushBySize(t *testing.T) {
	rw := newRecordingWriter()
	clk := newFakeClock()
	w := mustWriter(t, rw, testHead(), WriterOptions{Uncompressed: true, FlushBytes: 100, FlushInterval: -1, Now: clk.Now})
	base := len(rw.writes)
	chunkData := bytes.Repeat([]byte("x"), 40) // cost 43 each
	for i := 0; i < 2; i++ {
		if err := w.Output(time.Duration(i)*time.Millisecond, chunkData); err != nil {
			t.Fatal(err)
		}
	}
	if len(rw.writes) != base {
		t.Fatal("flushed before reaching FlushBytes")
	}
	// Third event would make 129 > 100: the pending two go out first.
	if err := w.Output(2*time.Millisecond, chunkData); err != nil {
		t.Fatal(err)
	}
	if len(rw.writes) != base+1 {
		t.Fatalf("writes = %d, want one block flushed", len(rw.writes)-base)
	}
	// One event over the threshold on its own flushes right away.
	if err := w.Output(3*time.Millisecond, bytes.Repeat([]byte("y"), 200)); err != nil {
		t.Fatal(err)
	}
	if len(rw.writes) != base+3 {
		t.Fatalf("writes = %d, want 3 blocks flushed", len(rw.writes)-base)
	}
	if st := w.Stats(); st.Blocks != 3 || st.Events != 4 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestFlushByTime(t *testing.T) {
	rw := newRecordingWriter()
	clk := newFakeClock()
	w := mustWriter(t, rw, testHead(), WriterOptions{Uncompressed: true, FlushInterval: 2 * time.Second, Now: clk.Now})
	base := len(rw.writes)

	// Via Tick.
	if err := w.Output(0, []byte("a")); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Second)
	if err := w.Tick(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if len(rw.writes) != base {
		t.Fatal("Tick flushed before the interval")
	}
	clk.Advance(time.Second)
	if err := w.Tick(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if len(rw.writes) != base+1 {
		t.Fatal("Tick did not flush after the interval")
	}

	// Via WriteEvent: the stale block goes out before the new event joins.
	if err := w.Output(10*time.Second, []byte("b")); err != nil {
		t.Fatal(err)
	}
	clk.Advance(5 * time.Second)
	if err := w.Output(15*time.Second, []byte("c")); err != nil {
		t.Fatal(err)
	}
	if len(rw.writes) != base+2 {
		t.Fatalf("writes = %d, want b flushed alone", len(rw.writes)-base)
	}
	// The interval is measured from the oldest pending event, not the newest.
	clk.Advance(time.Second)
	if err := w.Output(16*time.Second, []byte("d")); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Second)
	if err := w.Tick(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if len(rw.writes) != base+3 {
		t.Fatal("c and d were not flushed 2s after c")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(bytes.NewReader(rw.buf.Bytes()), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := drain(t, r)
	if err != io.EOF || len(evs) != 4 {
		t.Fatalf("read %d events, err %v", len(evs), err)
	}
}

func TestIdleWritesZeroBytes(t *testing.T) {
	for _, coalesce := range []uint64{0, 10} {
		t.Run(fmt.Sprintf("coalesce=%d", coalesce), func(t *testing.T) {
			rw := newRecordingWriter()
			clk := newFakeClock()
			w := mustWriter(t, rw, testHead(), WriterOptions{Now: clk.Now, CoalesceTicks: coalesce})
			header := rw.buf.Len()

			// Five idle minutes with a timer ticking once per second.
			for i := 0; i < 300; i++ {
				clk.Advance(time.Second)
				if err := w.Tick(clk.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			if rw.buf.Len() != header || len(rw.writes) != 1 {
				t.Fatalf("idle wrote %d extra bytes in %d writes", rw.buf.Len()-header, len(rw.writes)-1)
			}

			// One event, flushed; then another long idle emits nothing more.
			if err := w.Output(300*time.Second, []byte("x")); err != nil {
				t.Fatal(err)
			}
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			after := rw.buf.Len()
			for i := 0; i < 300; i++ {
				clk.Advance(time.Second)
				if err := w.Tick(clk.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if rw.buf.Len() != after {
				t.Fatalf("second idle period wrote %d bytes", rw.buf.Len()-after)
			}
			// Closing an empty writer adds nothing either.
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if rw.buf.Len() != after {
				t.Fatal("Close wrote bytes with nothing pending")
			}
			// The five-minute gap costs a few bytes of timestamp, not a block.
			r, err := NewReader(bytes.NewReader(rw.buf.Bytes()), ReaderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			evs, _ := drain(t, r)
			if len(evs) != 1 || evs[0].At != 300*time.Second {
				t.Fatalf("events = %+v", evs)
			}
		})
	}
}

func TestInputRefusedWithoutFlag(t *testing.T) {
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{})
	size := buf.Len()
	if err := w.Input(0, []byte("secret")); !errors.Is(err, ErrInputNotRecorded) {
		t.Fatalf("Input err = %v", err)
	}
	if err := w.WriteEvent(0, event.KindIn, []byte("secret")); !errors.Is(err, ErrInputNotRecorded) {
		t.Fatalf("WriteEvent(KindIn) err = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("a refused Input must not poison the writer: %v", err)
	}
	if buf.Len() != size || w.Stats().Events != 0 {
		t.Fatal("refused input leaked into the file")
	}

	var buf2 bytes.Buffer
	h := testHead()
	h.Flags = head.FlagInputRecorded
	w = mustWriter(t, &buf2, h, WriterOptions{})
	if err := w.Input(0, []byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(&buf2, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if e, err := r.Next(); err != nil || e.Kind != event.KindIn || string(e.Data) != "ok" {
		t.Fatalf("event = %+v, %v", e, err)
	}
}

func TestBackwardsTimestampClamped(t *testing.T) {
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{})
	for _, at := range []time.Duration{time.Second, 500 * time.Millisecond, -time.Second, 2 * time.Second} {
		if err := w.Output(at, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := w.Stats().ClampedTimestamps; got != 2 {
		t.Fatalf("ClampedTimestamps = %d, want 2", got)
	}
	r, err := NewReader(&buf, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := drain(t, r)
	var ats []time.Duration
	for _, e := range evs {
		ats = append(ats, e.At)
	}
	want := []time.Duration{time.Second, time.Second, time.Second, 2 * time.Second}
	if fmt.Sprint(ats) != fmt.Sprint(want) {
		t.Fatalf("times = %v, want %v", ats, want)
	}
}

func TestCoalescing(t *testing.T) {
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{CoalesceTicks: 20}) // 2ms at 100µs
	for i, s := range []string{"a", "b", "c"} {
		if err := w.Output(time.Duration(i)*time.Millisecond, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Mark(3*time.Millisecond, "m"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(&buf, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Head().Flags&head.FlagCoalesced == 0 {
		t.Fatal("HEAD lacks the coalesced flag")
	}
	evs, _ := drain(t, r)
	want := []wantEvent{{0, event.KindOut, "abc"}, {3 * time.Millisecond, event.KindMark, "m"}}
	if !equalWant(toWant(evs), want) {
		t.Fatalf("events = %+v", toWant(evs))
	}
}

func TestSyncOnFlush(t *testing.T) {
	for _, on := range []bool{false, true} {
		sw := &syncingWriter{recordingWriter: *newRecordingWriter()}
		w := mustWriter(t, sw, testHead(), WriterOptions{SyncOnFlush: on})
		_ = w.Output(0, []byte("x"))
		_ = w.Flush()
		_ = w.Close()
		want := 0
		if on {
			want = 2 // header and one block
		}
		if sw.syncs != want {
			t.Fatalf("SyncOnFlush=%v: syncs = %d, want %d", on, sw.syncs, want)
		}
	}
	// A writer without Sync is fine with SyncOnFlush.
	rw := newRecordingWriter()
	w := mustWriter(t, rw, testHead(), WriterOptions{SyncOnFlush: true})
	_ = w.Output(0, []byte("x"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseIdempotentAndErrClosed(t *testing.T) {
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{})
	_ = w.Output(0, []byte("x"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	n := buf.Len()
	if err := w.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if err := w.Output(time.Second, []byte("y")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Output after Close = %v", err)
	}
	if err := w.Flush(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Flush after Close = %v", err)
	}
	if buf.Len() != n {
		t.Fatal("bytes written after Close")
	}
}

func TestWriteErrorIsSticky(t *testing.T) {
	rw := newRecordingWriter()
	w := mustWriter(t, rw, testHead(), WriterOptions{})
	rw.limit = rw.buf.Len() + 5 // dies inside the first block
	_ = w.Output(0, []byte("x"))
	err := w.Flush()
	if !errors.Is(err, errKilled) {
		t.Fatalf("Flush = %v, want the write error", err)
	}
	if err2 := w.Output(time.Second, []byte("y")); !errors.Is(err2, errKilled) {
		t.Fatalf("Output after failure = %v", err2)
	}
	if err3 := w.Close(); !errors.Is(err3, errKilled) {
		t.Fatalf("Close = %v", err3)
	}
	if err4 := w.Close(); !errors.Is(err4, errKilled) {
		t.Fatalf("second Close = %v", err4)
	}
}

func TestWriterOptionErrors(t *testing.T) {
	var buf bytes.Buffer
	empty := codec.NewRegistry()
	if _, err := NewWriter(&buf, testHead(), WriterOptions{Registry: empty}); !errors.Is(err, codec.ErrUnknownCodec) {
		t.Fatalf("unregistered default codec: %v", err)
	}
	if _, err := NewWriter(&buf, testHead(), WriterOptions{Registry: empty, Uncompressed: true}); !errors.Is(err, codec.ErrUnknownCodec) {
		t.Fatalf("unregistered none codec: %v", err)
	}
	if _, err := NewWriter(&buf, testHead(), WriterOptions{FlushBytes: MaxFlushBytes + 1}); err == nil {
		t.Fatal("oversized FlushBytes accepted")
	}
	if _, err := NewWriter(&buf, head.Head{}, WriterOptions{}); !errors.Is(err, head.ErrLimit) {
		t.Fatalf("invalid head: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatal("failed NewWriter wrote bytes")
	}
	w := mustWriter(t, &buf, testHead(), WriterOptions{})
	if err := w.WriteEvent(0, event.Kind(10), nil); !errors.Is(err, event.ErrUnsupported) {
		t.Fatalf("reserved kind: %v", err)
	}
	if err := w.Output(0, make([]byte, MaxEventData+1)); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("huge event: %v", err)
	}
	if err := w.Resize(0, -1, 1, 0, 0); err == nil {
		t.Fatal("negative resize accepted")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("rejected events must not poison the writer: %v", err)
	}
}

func TestWriterDoesNotRetainCallerBuffer(t *testing.T) {
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{Uncompressed: true})
	p := []byte("first")
	_ = w.Output(0, p)
	copy(p, "XXXXX")
	_ = w.Output(time.Millisecond, p)
	_ = w.Close()
	r, _ := NewReader(&buf, ReaderOptions{})
	evs, _ := drain(t, r)
	if got := toWant(evs); len(got) != 2 || got[0].data != "first" || got[1].data != "XXXXX" {
		t.Fatalf("events = %+v", got)
	}
}

func TestStats(t *testing.T) {
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{Codec: codec.Flate})
	_ = w.Output(0, bytes.Repeat([]byte("abc"), 1000))
	_ = w.Close()
	st := w.Stats()
	if st.Blocks != 1 || st.Events != 1 || st.RawBytes < 3000 || st.CompressedBytes == 0 || st.CompressedBytes >= st.RawBytes {
		t.Fatalf("stats = %+v", st)
	}
	if st.BytesWritten != uint64(buf.Len()) {
		t.Fatalf("BytesWritten = %d, buffer %d", st.BytesWritten, buf.Len())
	}
}
