package bin

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin/chunk"
	"github.com/michaellorant/shellshot/internal/recording/bin/codec"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

// openers run each reader test over both constructors.
var openers = []struct {
	name string
	open func(data []byte, o ReaderOptions) (*Reader, error)
}{
	{"NewReader", func(d []byte, o ReaderOptions) (*Reader, error) { return NewReader(bytes.NewReader(d), o) }},
	{"OpenAt", func(d []byte, o ReaderOptions) (*Reader, error) {
		return OpenAt(bytes.NewReader(d), int64(len(d)), o)
	}},
}

// rawChunk builds one chunk's bytes.
func rawChunkBytes(t *testing.T, typ chunk.Type, payload []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := chunk.Write(&b, typ, payload); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// splice returns data with extra inserted at offset off.
func splice(data []byte, off int, extra []byte) []byte {
	out := append([]byte(nil), data[:off]...)
	out = append(out, extra...)
	return append(out, data[off:]...)
}

// eventCountBefore returns, for golden, how many events lie in blocks that end
// at or before each byte offset.
func goldenBlockEnds(t *testing.T, data []byte) (ends []int, counts []int) {
	t.Helper()
	blockEvents := []int{3, 2, 2, 2}
	n, total := 0, 0
	for _, s := range chunkSpans(t, data) {
		if s.typ == chunk.TypeEvent {
			total += blockEvents[n]
			n++
		}
		ends = append(ends, s.end)
		counts = append(counts, total)
	}
	if n != len(blockEvents) {
		t.Fatalf("golden has %d blocks", n)
	}
	return ends, counts
}

func TestTruncationAtEveryOffset(t *testing.T) {
	data := readGolden(t)
	full := goldenWant()
	ends, counts := goldenBlockEnds(t, data)
	headEnd := ends[0]

	for _, op := range openers {
		t.Run(op.name, func(t *testing.T) {
			prevCount := 0
			for n := 0; n <= len(data); n++ {
				r, err := op.open(data[:n], ReaderOptions{})
				if n < headEnd {
					if !errors.Is(err, ErrTruncated) || !errors.Is(err, io.ErrUnexpectedEOF) {
						t.Fatalf("n=%d: open err = %v, want ErrTruncated", n, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("n=%d: open: %v", n, err)
				}
				evs, endErr := drain(t, r)

				// Expected: events of all blocks wholly inside data[:n], and the
				// end of the last chunk at or before n.
				wantCount, wantValid, onBoundary := 0, 0, false
				for i, e := range ends {
					if e <= n {
						wantCount, wantValid = counts[i], e
						onBoundary = e == n
					}
				}
				if len(evs) != wantCount {
					t.Fatalf("n=%d: %d events, want %d", n, len(evs), wantCount)
				}
				if len(evs) < prevCount {
					t.Fatalf("n=%d: events decreased from %d to %d", n, prevCount, len(evs))
				}
				prevCount = len(evs)
				if !equalWant(toWant(evs), full[:wantCount]) {
					t.Fatalf("n=%d: events are not a prefix of the full list", n)
				}
				if onBoundary {
					if endErr != io.EOF {
						t.Fatalf("n=%d: end error = %v, want io.EOF", n, endErr)
					}
				} else if !errors.Is(endErr, ErrTruncated) || !errors.Is(endErr, io.ErrUnexpectedEOF) {
					t.Fatalf("n=%d: end error = %v, want ErrTruncated", n, endErr)
				}
				if r.ValidBytes() != int64(wantValid) {
					t.Fatalf("n=%d: ValidBytes = %d, want %d", n, r.ValidBytes(), wantValid)
				}
				// Errors are sticky.
				if _, err2 := r.Next(); err2 != endErr {
					t.Fatalf("n=%d: second Next = %v, want %v", n, err2, endErr)
				}
			}
		})
	}
}

func TestEmptyAndBadMagic(t *testing.T) {
	for _, op := range openers {
		if _, err := op.open(nil, ReaderOptions{}); !errors.Is(err, ErrTruncated) {
			t.Fatalf("%s: empty = %v", op.name, err)
		}
		bad := append([]byte(nil), readGolden(t)...)
		bad[1] = 'X'
		if _, err := op.open(bad, ReaderOptions{}); !errors.Is(err, ErrBadMagic) {
			t.Fatalf("%s: bad magic = %v", op.name, err)
		}
		if _, err := op.open([]byte("{\"version\":2}\n"), ReaderOptions{}); !errors.Is(err, ErrBadMagic) {
			t.Fatalf("%s: JSON = %v", op.name, err)
		}
	}
}

func TestLastChunkCRCFailureIsTruncation(t *testing.T) {
	data := append([]byte(nil), readGolden(t)...)
	spans := chunkSpans(t, data)
	last := spans[len(spans)-1]
	data[len(data)-1] ^= 0xff
	for _, op := range openers {
		r, err := op.open(data, ReaderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		evs, endErr := drain(t, r)
		if !errors.Is(endErr, ErrTruncated) || len(evs) != 7 || r.ValidBytes() != int64(last.start) {
			t.Fatalf("%s: %d events, err %v, ValidBytes %d (want 7, ErrTruncated, %d)", op.name, len(evs), endErr, r.ValidBytes(), last.start)
		}
	}
}

func TestMidFileCorruptionIsErrCorrupt(t *testing.T) {
	orig := readGolden(t)
	spans := chunkSpans(t, orig)
	ends, counts := goldenBlockEnds(t, orig)
	full := goldenWant()
	for _, op := range openers {
		for si, s := range spans[:len(spans)-1] { // every chunk but the last
			for _, off := range []int{s.start + 8, (s.start + s.end) / 2, s.end - 1} { // payload start, middle, CRC
				data := append([]byte(nil), orig...)
				data[off] ^= 0x10
				r, err := op.open(data, ReaderOptions{})
				if si == 0 { // HEAD
					if !errors.Is(err, ErrCorrupt) {
						t.Fatalf("%s: HEAD flip at %d: %v", op.name, off, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("%s: flip at %d in chunk %d: open: %v", op.name, off, si, err)
				}
				evs, endErr := drain(t, r)
				if !errors.Is(endErr, ErrCorrupt) {
					t.Fatalf("%s: flip at %d in chunk %d: end error %v, want ErrCorrupt", op.name, off, si, endErr)
				}
				if errors.Is(endErr, ErrTruncated) {
					t.Fatal("ErrCorrupt must not match ErrTruncated")
				}
				if len(evs) != counts[si-1] || !equalWant(toWant(evs), full[:len(evs)]) {
					t.Fatalf("%s: flip in chunk %d: %d events, want %d", op.name, si, len(evs), counts[si-1])
				}
				if r.ValidBytes() != int64(ends[si-1]) {
					t.Fatalf("%s: ValidBytes = %d, want %d", op.name, r.ValidBytes(), ends[si-1])
				}
			}
		}
	}
}

func TestEveryBitFlipIsDetected(t *testing.T) {
	orig := readGolden(t)
	full := goldenWant()
	known := []error{ErrCorrupt, ErrTruncated, ErrUnsupported, ErrLimit, ErrBadMagic}
	for i := 0; i < len(orig); i++ {
		for _, mask := range []byte{0x01, 0x80} {
			data := append([]byte(nil), orig...)
			data[i] ^= mask
			r, err := NewReader(bytes.NewReader(data), ReaderOptions{})
			var endErr error
			if err != nil {
				endErr = err
			} else {
				var evs []Event
				evs, endErr = drain(t, r)
				if !equalWant(toWant(evs), full[:len(evs)]) {
					t.Fatalf("flip %#x at %d: delivered events that are not a prefix", mask, i)
				}
			}
			ok := false
			for _, k := range known {
				ok = ok || errors.Is(endErr, k)
			}
			if !ok {
				t.Fatalf("flip %#x at %d went undetected: end error %v", mask, i, endErr)
			}
		}
	}
}

func TestKillSimulation(t *testing.T) {
	golden := readGolden(t)
	ends, counts := goldenBlockEnds(t, golden)
	full := goldenWant()
	headEnd := ends[0]
	for limit := 0; limit <= len(golden); limit++ {
		clk := newFakeClock()
		rw := newRecordingWriter()
		rw.limit = limit
		w, err := NewWriter(rw, goldenHead(), goldenOptions(clk))
		if err == nil {
			err = goldenScenario(w, clk)
		}
		if limit < len(golden) && !errors.Is(err, errKilled) {
			t.Fatalf("limit=%d: writer error = %v, want the kill", limit, err)
		}
		if limit == len(golden) && err != nil {
			t.Fatalf("limit=%d: %v", limit, err)
		}
		got := rw.buf.Bytes()
		if !bytes.Equal(got, golden[:len(got)]) || len(got) != limit {
			t.Fatalf("limit=%d: writer output is not a golden prefix (%d bytes)", limit, len(got))
		}

		r, err := NewReader(bytes.NewReader(got), ReaderOptions{})
		if limit < headEnd {
			if !errors.Is(err, ErrTruncated) {
				t.Fatalf("limit=%d: open = %v", limit, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("limit=%d: open: %v", limit, err)
		}
		evs, endErr := drain(t, r)
		want, valid := 0, 0
		for i, e := range ends {
			if e <= limit {
				want, valid = counts[i], e
			}
		}
		if len(evs) != want || !equalWant(toWant(evs), full[:want]) {
			t.Fatalf("limit=%d: recovered %d events, want %d", limit, len(evs), want)
		}
		if r.ValidBytes() != int64(valid) {
			t.Fatalf("limit=%d: ValidBytes = %d, want %d", limit, r.ValidBytes(), valid)
		}
		if limit == valid {
			if endErr != io.EOF {
				t.Fatalf("limit=%d: end = %v", limit, endErr)
			}
		} else if !errors.Is(endErr, ErrTruncated) {
			t.Fatalf("limit=%d: end = %v", limit, endErr)
		}
	}
}

func TestUnknownAncillaryChunkSkipped(t *testing.T) {
	golden := readGolden(t)
	spans := chunkSpans(t, golden)
	var evnt []chunkSpan
	for _, s := range spans {
		if s.typ == chunk.TypeEvent {
			evnt = append(evnt, s)
		}
	}
	anc := rawChunkBytes(t, chunk.Type{'x', 't', 'r', 'a'}, []byte("whatever"))
	late := rawChunkBytes(t, chunk.TypeTheme, make([]byte, head.ThemeLen))
	data := splice(golden, evnt[0].end, anc) // between blocks 1 and 2
	data = splice(data, evnt[0].end, late)   // a late theme chunk: ignored
	data = splice(data, spans[0].end, anc)   // in the preamble
	data = splice(data, len(data), anc)      // after the last block
	for _, op := range openers {
		r, err := op.open(data, ReaderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		evs, endErr := drain(t, r)
		if endErr != io.EOF || !equalWant(toWant(evs), goldenWant()) {
			t.Fatalf("%s: %d events, err %v", op.name, len(evs), endErr)
		}
		if r.ValidBytes() != int64(len(data)) {
			t.Fatalf("%s: ValidBytes = %d, want %d", op.name, r.ValidBytes(), len(data))
		}
		if th := r.Theme(); th == nil || *th != *goldenTheme() {
			t.Fatalf("%s: a late theme chunk replaced the real one", op.name)
		}
	}
}

func TestUnknownCriticalChunk(t *testing.T) {
	golden := readGolden(t)
	spans := chunkSpans(t, golden)
	crit := rawChunkBytes(t, chunk.Type{'X', 'T', 'R', 'A'}, []byte("needed"))
	full := goldenWant()
	_, counts := goldenBlockEnds(t, golden)

	cases := []struct {
		name   string
		at     int
		events int
	}{
		{"preamble", spans[0].end, 0},
		{"after block 2", spans[len(spans)-3].end, counts[len(spans)-3]},
		{"at end", len(golden), len(full)},
	}
	for _, tc := range cases {
		for _, op := range openers {
			data := splice(golden, tc.at, crit)
			r, err := op.open(data, ReaderOptions{})
			if err != nil {
				t.Fatalf("%s/%s: open: %v", tc.name, op.name, err)
			}
			evs, endErr := drain(t, r)
			if !errors.Is(endErr, ErrUnsupported) {
				t.Fatalf("%s/%s: end = %v, want ErrUnsupported", tc.name, op.name, endErr)
			}
			if len(evs) != tc.events || !equalWant(toWant(evs), full[:len(evs)]) {
				t.Fatalf("%s/%s: %d events, want %d", tc.name, op.name, len(evs), tc.events)
			}
		}
	}
}

func TestStructuralErrors(t *testing.T) {
	golden := readGolden(t)
	spans := chunkSpans(t, golden)
	headChunk := golden[spans[0].start:spans[0].end]

	t.Run("second HEAD", func(t *testing.T) {
		data := splice(golden, spans[len(spans)-2].end, headChunk)
		r, err := NewReader(bytes.NewReader(data), ReaderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		evs, endErr := drain(t, r)
		if !errors.Is(endErr, ErrCorrupt) || len(evs) != 7 {
			t.Fatalf("%d events, %v", len(evs), endErr)
		}
	})
	t.Run("first chunk not HEAD", func(t *testing.T) {
		data := append([]byte(chunk.Magic), rawChunkBytes(t, chunk.TypeMeta, []byte{0})...)
		if _, err := NewReader(bytes.NewReader(data), ReaderOptions{}); !errors.Is(err, ErrCorrupt) {
			t.Fatal(err)
		}
	})
	t.Run("unknown required feature", func(t *testing.T) {
		// tag 1 (version): minor 0, features bit 5; tag 2 (size): 1x1
		payload := []byte{1, 2, 0, 0x20, 2, 2, 1, 1}
		data := append([]byte(chunk.Magic), rawChunkBytes(t, chunk.TypeHead, payload)...)
		if _, err := NewReader(bytes.NewReader(data), ReaderOptions{}); !errors.Is(err, ErrUnsupported) {
			t.Fatal(err)
		}
	})
	t.Run("only magic", func(t *testing.T) {
		if _, err := NewReader(bytes.NewReader([]byte(chunk.Magic)), ReaderOptions{}); !errors.Is(err, ErrTruncated) {
			t.Fatal(err)
		}
	})
	t.Run("missing dictionary", func(t *testing.T) {
		dictSpan := spans[1]
		if dictSpan.typ != chunk.TypeDict {
			t.Fatalf("second chunk is %s", dictSpan.typ)
		}
		data := append(append([]byte(nil), golden[:dictSpan.start]...), golden[dictSpan.end:]...)
		r, err := NewReader(bytes.NewReader(data), ReaderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		// The golden zstd blocks may or may not use the dictionary; either
		// way no error other than ErrCorrupt (unknown dictionary) is allowed.
		_, endErr := drain(t, r)
		if endErr != io.EOF && !errors.Is(endErr, ErrCorrupt) {
			t.Fatal(endErr)
		}
	})
	t.Run("unregistered codec", func(t *testing.T) {
		r, err := NewReader(bytes.NewReader(golden), ReaderOptions{Registry: codec.NewRegistry()})
		if err != nil {
			t.Fatal(err)
		}
		evs, endErr := drain(t, r)
		if !errors.Is(endErr, ErrUnsupported) || len(evs) != 0 {
			t.Fatalf("%d events, %v", len(evs), endErr)
		}
	})
	t.Run("limits", func(t *testing.T) {
		r, err := NewReader(bytes.NewReader(golden), ReaderOptions{Limits: event.Limits{MaxEvents: 1}})
		if err != nil {
			t.Fatal(err)
		}
		_, endErr := drain(t, r)
		if !errors.Is(endErr, ErrLimit) {
			t.Fatal(endErr)
		}
		_, err = NewReader(bytes.NewReader(golden), ReaderOptions{MaxChunkLen: 16})
		if !errors.Is(err, chunk.ErrTooLarge) || !errors.Is(err, ErrCorrupt) {
			t.Fatalf("open with small MaxChunkLen: %v", err)
		}
	})
}

func TestCopyData(t *testing.T) {
	golden := readGolden(t)
	for _, copyData := range []bool{false, true} {
		r, err := NewReader(bytes.NewReader(golden), ReaderOptions{CopyData: copyData})
		if err != nil {
			t.Fatal(err)
		}
		var evs []Event
		for {
			e, err := r.Next()
			if err != nil {
				break
			}
			evs = append(evs, e)
		}
		if copyData {
			if !equalWant(toWant(evs), goldenWant()) {
				t.Fatal("CopyData events differ")
			}
		}
	}
}

func TestSeek(t *testing.T) {
	data := readGolden(t)
	full := goldenWant()
	probes := []time.Duration{
		-time.Second, 0, time.Microsecond, 10 * time.Millisecond, 15 * time.Millisecond,
		20 * time.Millisecond, 21 * time.Millisecond, 999 * time.Millisecond, time.Second,
		time.Second + 50*time.Microsecond, time.Second + 100*time.Microsecond, 2 * time.Second,
		5 * time.Second, 5*time.Second + 150*time.Microsecond, 5*time.Second + 201*time.Microsecond, 6 * time.Second, 7 * time.Second,
		7*time.Second + time.Millisecond, 7*time.Second + 2*time.Millisecond, time.Hour,
	}
	r, err := OpenAt(bytes.NewReader(data), int64(len(data)), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probes {
		if err := r.Seek(p); err != nil {
			t.Fatalf("Seek(%v): %v", p, err)
		}
		evs, endErr := drain(t, r)
		var want []wantEvent
		for _, e := range full {
			if e.at >= p {
				want = append(want, e)
			}
		}
		if endErr != io.EOF || !equalWant(toWant(evs), want) {
			t.Fatalf("Seek(%v): got %+v (%v), want %+v", p, toWant(evs), endErr, want)
		}
	}
	// Read halfway, seek back, read again.
	if err := r.Seek(0); err != nil {
		t.Fatal(err)
	}
	_, _ = r.Next()
	_, _ = r.Next()
	if err := r.Seek(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if e, err := r.Next(); err != nil || e.Kind != event.KindMark {
		t.Fatalf("after reseek: %+v, %v", e, err)
	}

	d, err := r.Duration()
	if err != nil || d != 7*time.Second+time.Millisecond {
		t.Fatalf("Duration = %v, %v", d, err)
	}

	// A stream reader cannot seek.
	sr, _ := NewReader(bytes.NewReader(data), ReaderOptions{})
	if err := sr.Seek(0); !errors.Is(err, ErrNotSeekable) {
		t.Fatalf("Seek on NewReader = %v", err)
	}
}

func TestSeekOnTornFile(t *testing.T) {
	data := readGolden(t)
	spans := chunkSpans(t, data)
	torn := data[:spans[len(spans)-1].end-3] // last block incomplete
	r, err := OpenAt(bytes.NewReader(torn), int64(len(torn)), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Seek(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	evs, endErr := drain(t, r)
	if len(evs) != 2 || !errors.Is(endErr, ErrTruncated) {
		t.Fatalf("%d events, %v", len(evs), endErr)
	}
	// Past every complete block: the torn tail is still reported.
	if err := r.Seek(time.Hour); err != nil {
		t.Fatal(err)
	}
	evs, endErr = drain(t, r)
	if len(evs) != 0 || !errors.Is(endErr, ErrTruncated) {
		t.Fatalf("%d events, %v", len(evs), endErr)
	}
	if d, _ := r.Duration(); d != 5*time.Second+200*time.Microsecond {
		t.Fatalf("Duration over torn file = %v", d)
	}
}

// countingCodec counts Decompress calls.
type countingCodec struct {
	codec.Codec
	decompress *atomic.Int64
}

func (c countingCodec) Decompress(dst, src, dict []byte, maxLen int) ([]byte, error) {
	c.decompress.Add(1)
	return c.Codec.Decompress(dst, src, dict, maxLen)
}

func countingRegistry(t *testing.T) (*codec.Registry, *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	reg := codec.NewRegistry()
	def := codec.DefaultRegistry()
	for _, id := range []codec.ID{codec.None, codec.Flate, codec.Zstd} {
		c, err := def.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Register(countingCodec{c, &n}); err != nil {
			t.Fatal(err)
		}
	}
	return reg, &n
}

func TestSeekDoesNotDecompress(t *testing.T) {
	data := readGolden(t)
	reg, n := countingRegistry(t)
	r, err := OpenAt(bytes.NewReader(data), int64(len(data)), ReaderOptions{Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Seek(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Duration(); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 0 {
		t.Fatalf("%d Decompress calls while building the block table", n.Load())
	}
	e, err := r.Next()
	if err != nil || e.Kind != event.KindMark {
		t.Fatalf("%+v, %v", e, err)
	}
	if n.Load() != 1 {
		t.Fatalf("%d Decompress calls, want exactly the target block", n.Load())
	}
}

// countingReaderAt counts the bytes read through ReadAt.
type countingReaderAt struct {
	r io.ReaderAt
	n atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n.Add(int64(n))
	return n, err
}

func TestSeekReadsHeadersOnly(t *testing.T) {
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{Uncompressed: true, FlushBytes: 64 << 10})
	payload := make([]byte, 60<<10)
	for i := range payload {
		payload[i] = byte(i * 7919)
	}
	for i := 0; i < 40; i++ {
		if err := w.Output(time.Duration(i)*time.Second, payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w.Stats().Blocks != 40 {
		t.Fatalf("blocks = %d", w.Stats().Blocks)
	}
	cr := &countingReaderAt{r: bytes.NewReader(buf.Bytes())}
	r, err := OpenAt(cr, int64(buf.Len()), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before := cr.n.Load()
	if err := r.Seek(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	scanned := cr.n.Load() - before
	if scanned > 8<<10 {
		t.Fatalf("block table scan read %d bytes of a %d byte file", scanned, buf.Len())
	}
	e, err := r.Next()
	if err != nil || e.At != 30*time.Second {
		t.Fatalf("%v, %v", e.At, err)
	}
}

func TestMonotonicTicksAcrossBlocksEnforced(t *testing.T) {
	// Two blocks written by hand, the second starting before the first ended.
	var enc event.Encoder
	reg := codec.DefaultRegistry()
	c, _ := reg.Get(codec.None)
	p1, err := enc.Encode(event.Block{StartTick: 10, EndTick: 10, Events: []event.Event{{Tick: 10}}}, c, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := enc.Encode(event.Block{StartTick: 5, EndTick: 5, Events: []event.Event{{Tick: 5}}}, c, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	hp, _ := testHead().Marshal()
	data := []byte(chunk.Magic)
	data = append(data, rawChunkBytes(t, chunk.TypeHead, hp)...)
	data = append(data, rawChunkBytes(t, chunk.TypeEvent, p1)...)
	data = append(data, rawChunkBytes(t, chunk.TypeEvent, p2)...)
	r, err := NewReader(bytes.NewReader(data), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evs, endErr := drain(t, r)
	if len(evs) != 1 || !errors.Is(endErr, ErrCorrupt) {
		t.Fatalf("%d events, %v", len(evs), endErr)
	}
}

func FuzzReader(f *testing.F) {
	golden := buildGolden(f)
	f.Add(golden)
	f.Add(golden[:len(golden)/2])
	f.Add(golden[:len(chunk.Magic)+3])
	f.Add([]byte(chunk.Magic))
	f.Add([]byte{})
	reg := codec.DefaultRegistry()
	opts := ReaderOptions{
		Registry:    reg,
		MaxChunkLen: 1 << 20,
		Limits:      event.Limits{MaxEvents: 1 << 14, MaxRawLen: 1 << 20, MaxPayload: 1 << 20},
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if r, err := NewReader(bytes.NewReader(data), opts); err == nil {
			_ = r.Head()
			_ = r.Theme()
			_ = r.Meta()
			for i := 0; i < 1<<16; i++ {
				if _, err := r.Next(); err != nil {
					break
				}
			}
			if r.ValidBytes() > int64(len(data)) {
				t.Fatalf("ValidBytes %d beyond input %d", r.ValidBytes(), len(data))
			}
		}
		if r, err := OpenAt(bytes.NewReader(data), int64(len(data)), opts); err == nil {
			_, _ = r.Duration()
			for _, p := range []time.Duration{0, time.Second, time.Hour} {
				if r.Seek(p) != nil {
					break
				}
				for i := 0; i < 1<<16; i++ {
					if _, err := r.Next(); err != nil {
						break
					}
				}
			}
		}
	})
}
