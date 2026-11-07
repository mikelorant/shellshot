package event

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"testing"

	"github.com/mikelorant/shellshot/internal/recording/bin/codec"
	"github.com/mikelorant/shellshot/internal/recording/bin/varint"
)

func mustCodec(t testing.TB, id codec.ID) codec.Codec {
	t.Helper()
	c, err := codec.DefaultRegistry().Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func encode(t testing.TB, b Block, id codec.ID, dict []byte, dictID uint64) []byte {
	t.Helper()
	var enc Encoder
	p, err := enc.Encode(b, mustCodec(t, id), dict, dictID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func decode(p []byte) (Block, error) {
	return Decode(p, codec.DefaultRegistry(), nil, Limits{})
}

// rawPayload builds a payload with an arbitrary (possibly lying) header and
// the codec-none body.
func rawPayload(codecID, dictID, start, end, n, rawLen uint64, body []byte) []byte {
	var b []byte
	for _, v := range []uint64{codecID, dictID, start, end, n, rawLen} {
		b = varint.AppendUvarint(b, v)
	}
	return append(b, body...)
}

func goldenBlock() Block {
	return Block{StartTick: 10, EndTick: 15, Events: []Event{
		{Tick: 10, Kind: KindOut, Data: []byte("hi")},
		{Tick: 15, Kind: KindMark, Data: []byte("x")},
	}}
}

func TestEncodeGoldenNone(t *testing.T) {
	t.Parallel()
	want := []byte{
		0x00, 0x00, 0x0a, 0x0f, 0x02, 0x09, // codec dict start end n raw_len
		0x00, 0x05, // dt
		0x00, 0x03, // kind
		0x02, 0x01, // plen
		'h', 'i', 'x',
	}
	got := encode(t, goldenBlock(), codec.None, nil, 0)
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = % x, want % x", got, want)
	}
	blk, err := decode(got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(blk, goldenBlock()) {
		t.Fatalf("decoded %+v", blk)
	}
}

func TestEncodeGoldenFlate(t *testing.T) {
	t.Parallel()
	want := []byte{
		0x01, 0x00, 0x0a, 0x0f, 0x02, 0x09, // codec dict start end n raw_len
		// raw DEFLATE of 00 05 00 03 02 01 'h' 'i' 'x': a stored block, then
		// the final empty block (compress/flate does not shrink 9 bytes).
		0x00, 0x09, 0x00, 0xf6, 0xff,
		0x00, 0x05, 0x00, 0x03, 0x02, 0x01, 'h', 'i', 'x',
		0x03, 0x00,
	}
	got := encode(t, goldenBlock(), codec.Flate, nil, 0)
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
	blk, err := decode(got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(blk, goldenBlock()) {
		t.Fatalf("decoded %+v", blk)
	}
}

func randomBlock(rng *rand.Rand) Block {
	start := uint64(rng.Intn(1000))
	if rng.Intn(10) == 0 {
		start = rng.Uint64() >> 2
	}
	n := rng.Intn(40)
	if rng.Intn(8) == 0 {
		n = 0
	}
	b := Block{StartTick: start, EndTick: start}
	tick := start
	for i := 0; i < n; i++ {
		switch rng.Intn(4) {
		case 0:
		case 1:
			tick += uint64(rng.Intn(200))
		case 2:
			tick += uint64(rng.Intn(5_000_000))
		case 3:
			tick += uint64(rng.Intn(1 << 40))
		}
		var k Kind
		switch rng.Intn(3) {
		case 0:
			k = Kind(rng.Intn(int(KindExit) + 1))
		case 1:
			k = Kind(64 + rng.Intn(192))
		case 2:
			k = KindOut
		}
		var data []byte
		if l := rng.Intn(300); l > 0 && rng.Intn(5) != 0 {
			data = make([]byte, l)
			rng.Read(data)
			if rng.Intn(2) == 0 {
				for j := range data {
					data[j] = 'a' + data[j]%4 // compressible
				}
			}
		}
		b.Events = append(b.Events, Event{Tick: tick, Kind: k, Data: data})
		b.EndTick = tick
	}
	return b
}

func TestRoundTripRandom(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(20240607))
	dict := []byte("\x1b[0m\x1b[1;32m$ ls -la\r\n")
	dicts := func(id uint64) ([]byte, bool) {
		if id == 7 {
			return dict, true
		}
		return nil, false
	}
	reg := codec.DefaultRegistry()
	var enc Encoder
	empty := 0
	for i := 0; i < 300; i++ {
		b := randomBlock(rng)
		if len(b.Events) == 0 {
			empty++
		}
		for _, tc := range []struct {
			id     codec.ID
			dict   []byte
			dictID uint64
		}{{codec.None, nil, 0}, {codec.Flate, nil, 0}, {codec.Flate, dict, 7}} {
			c, _ := reg.Get(tc.id)
			p, err := enc.Encode(b, c, tc.dict, tc.dictID)
			if err != nil {
				t.Fatalf("block %d: encode: %v", i, err)
			}
			h, _, err := ReadHeader(p)
			if err != nil || h.StartTick != b.StartTick || h.EndTick != b.EndTick || h.NEvents != uint64(len(b.Events)) {
				t.Fatalf("block %d: header %+v err %v", i, h, err)
			}
			got, err := Decode(p, reg, dicts, Limits{})
			if err != nil {
				t.Fatalf("block %d: decode: %v", i, err)
			}
			if got.StartTick != b.StartTick || got.EndTick != b.EndTick || len(got.Events) != len(b.Events) {
				t.Fatalf("block %d: got %+v", i, got)
			}
			for j, e := range b.Events {
				g := got.Events[j]
				if g.Tick != e.Tick || g.Kind != e.Kind || !bytes.Equal(g.Data, e.Data) {
					t.Fatalf("block %d event %d: got %+v want %+v", i, j, g, e)
				}
				if len(e.Data) == 0 && g.Data != nil {
					t.Fatalf("block %d event %d: empty payload should decode as nil", i, j)
				}
			}
		}
	}
	if empty == 0 {
		t.Fatal("random generator produced no zero-event block")
	}
}

func TestEmptyBlock(t *testing.T) {
	t.Parallel()
	b := Block{StartTick: 42, EndTick: 42}
	for _, id := range []codec.ID{codec.None, codec.Flate, codec.Zstd} {
		got, err := decode(encode(t, b, id, nil, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got.StartTick != 42 || got.EndTick != 42 || len(got.Events) != 0 {
			t.Fatalf("got %+v", got)
		}
	}
}

func TestTypedPayloads(t *testing.T) {
	t.Parallel()
	r := ResizePayload{Cols: 120, Rows: 40, CellPxW: 9, CellPxH: 18}
	e := NewResize(5, r)
	if e.Kind != KindResize || e.Tick != 5 {
		t.Fatalf("event %+v", e)
	}
	if !bytes.Equal(e.Data, []byte{120, 40, 9, 18}) {
		t.Fatalf("resize bytes % x", e.Data)
	}
	got, err := ParseResize(e.Data)
	if err != nil || got != r {
		t.Fatalf("ParseResize = %+v, %v", got, err)
	}
	if _, err := ParseResize(e.Data[:3]); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("short resize: %v", err)
	}
	if _, err := ParseResize(append(e.Data, 0)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("long resize: %v", err)
	}
	for _, st := range []int64{0, 1, -1, 137, -9223372036854775808, 9223372036854775807} {
		x := NewExit(9, st)
		if x.Kind != KindExit {
			t.Fatal("kind")
		}
		back, err := ParseExit(x.Data)
		if err != nil || back != st {
			t.Fatalf("exit %d -> %d, %v", st, back, err)
		}
	}
	if x := NewExit(0, -1); !bytes.Equal(x.Data, []byte{1}) {
		t.Fatalf("zigzag(-1) = % x", x.Data)
	}
	if _, err := ParseExit(nil); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("empty exit: %v", err)
	}
	if _, err := ParseExit([]byte{2, 0}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("exit trailing: %v", err)
	}
	if m := NewMark(3, "héllo"); m.Kind != KindMark || string(m.Data) != "héllo" {
		t.Fatalf("mark %+v", m)
	}
}

func TestKindRanges(t *testing.T) {
	t.Parallel()
	for k := 0; k < 256; k++ {
		kd := Kind(k)
		wantRes := k >= 5 && k <= 63
		if kd.IsReserved() != wantRes {
			t.Errorf("IsReserved(%d) = %v", k, kd.IsReserved())
		}
		if kd.IsAncillary() != (k >= 64) {
			t.Errorf("IsAncillary(%d)", k)
		}
	}
}

func TestReservedKind(t *testing.T) {
	t.Parallel()
	var enc Encoder
	for _, k := range []Kind{5, 6, 63} {
		b := Block{StartTick: 0, EndTick: 0, Events: []Event{{Kind: k}}}
		if _, err := enc.Encode(b, mustCodec(t, codec.None), nil, 0); !errors.Is(err, ErrUnsupported) {
			t.Errorf("encode kind %d: %v", k, err)
		}
		// dt=0, kind=k, plen=0
		p := rawPayload(0, 0, 0, 0, 1, 3, []byte{0, byte(k), 0})
		if _, err := decode(p); !errors.Is(err, ErrUnsupported) {
			t.Errorf("decode kind %d: %v", k, err)
		}
	}
	for _, k := range []Kind{64, 65, 255} {
		p := rawPayload(0, 0, 0, 0, 1, 4, []byte{0, byte(k), 1, 'z'})
		blk, err := decode(p)
		if err != nil {
			t.Fatalf("kind %d: %v", k, err)
		}
		if blk.Events[0].Kind != k || string(blk.Events[0].Data) != "z" {
			t.Fatalf("kind %d: %+v", k, blk.Events[0])
		}
	}
}

func TestEncodeRejectsInvalidBlocks(t *testing.T) {
	t.Parallel()
	var enc Encoder
	none := mustCodec(t, codec.None)
	ev := func(ticks ...uint64) []Event {
		var es []Event
		for _, tk := range ticks {
			es = append(es, Event{Tick: tk})
		}
		return es
	}
	cases := map[string]Block{
		"start after end":    {StartTick: 5, EndTick: 4},
		"first before start": {StartTick: 5, EndTick: 6, Events: ev(4, 6)},
		"non-monotonic":      {StartTick: 0, EndTick: 3, Events: ev(5, 3)},
		"end not last":       {StartTick: 0, EndTick: 9, Events: ev(1, 2)},
		"empty end != start": {StartTick: 1, EndTick: 2},
	}
	for name, b := range cases {
		if _, err := enc.Encode(b, none, nil, 0); !errors.Is(err, ErrInvalidBlock) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := enc.Encode(Block{}, none, nil, 3); !errors.Is(err, ErrInvalidBlock) {
		t.Errorf("dict id without dict: %v", err)
	}
	if _, err := enc.Encode(Block{}, none, []byte("d"), 0); !errors.Is(err, ErrInvalidBlock) {
		t.Errorf("dict without id: %v", err)
	}
	if _, err := enc.Encode(Block{}, nil, nil, 0); !errors.Is(err, ErrInvalidBlock) {
		t.Errorf("nil codec: %v", err)
	}
}

func TestDecodeRejectsInconsistentHeaders(t *testing.T) {
	t.Parallel()
	good := []byte{0, 5, 0, 3, 2, 1, 'h', 'i', 'x'} // body of goldenBlock
	cases := []struct {
		name string
		p    []byte
		want error
	}{
		{"start after end", rawPayload(0, 0, 16, 15, 2, 9, good), ErrCorrupt},
		{"end not last tick", rawPayload(0, 0, 10, 16, 2, 9, good), ErrCorrupt},
		{"end before last tick", rawPayload(0, 0, 10, 14, 2, 9, good), ErrCorrupt},
		{"raw_len short", rawPayload(0, 0, 10, 15, 2, 8, good), codec.ErrTooLarge},
		{"raw_len long", rawPayload(0, 0, 10, 15, 2, 10, good), ErrCorrupt},
		{"n too big for raw_len", rawPayload(0, 0, 10, 15, 4, 9, good), ErrCorrupt},
		{"n too small", rawPayload(0, 0, 10, 15, 1, 9, good), ErrCorrupt},
		{"empty block end != start", rawPayload(0, 0, 10, 15, 0, 0, nil), ErrCorrupt},
		{"empty block with body", rawPayload(0, 0, 10, 10, 0, 1, []byte{0}), ErrCorrupt},
		{"plen sum short", rawPayload(0, 0, 10, 15, 2, 9, []byte{0, 5, 0, 3, 2, 1, 'h', 'i', 'x', 'y'}), codec.ErrTooLarge},
		{"plen sum mismatch", rawPayload(0, 0, 10, 15, 2, 9, []byte{0, 5, 0, 3, 2, 2, 'h', 'i', 'x'}), ErrCorrupt},
		{"plen beyond body", rawPayload(0, 0, 10, 15, 2, 9, []byte{0, 5, 0, 3, 9, 1, 'h', 'i', 'x'}), ErrCorrupt},
		{"unknown codec", rawPayload(9, 0, 10, 15, 2, 9, good), codec.ErrUnknownCodec},
		{"unknown dict", rawPayload(0, 4, 10, 15, 2, 9, good), ErrUnknownDict},
		{"non-canonical varint", append([]byte{0x80, 0x00}, good...), ErrCorrupt},
		{"empty", nil, ErrCorrupt},
	}
	for _, tc := range cases {
		if _, err := decode(tc.p); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	// dt sum overflow
	over := []byte{}
	over = varint.AppendUvarint(over, ^uint64(0))
	over = varint.AppendUvarint(over, 2)
	over = append(over, 0, 0, 0, 0)
	if _, err := decode(rawPayload(0, 0, 5, ^uint64(0), 2, uint64(len(over)), over)); !errors.Is(err, ErrCorrupt) {
		t.Errorf("tick overflow: %v", err)
	}
}

func TestDecodeDictionary(t *testing.T) {
	t.Parallel()
	dict := []byte("preset dictionary text")
	b := goldenBlock()
	p := encode(t, b, codec.Flate, dict, 3)
	reg := codec.DefaultRegistry()
	if _, err := Decode(p, reg, nil, Limits{}); !errors.Is(err, ErrUnknownDict) {
		t.Errorf("nil dicts: %v", err)
	}
	if _, err := Decode(p, reg, func(uint64) ([]byte, bool) { return nil, false }, Limits{}); !errors.Is(err, ErrUnknownDict) {
		t.Errorf("missing dict: %v", err)
	}
	if _, err := Decode(p, reg, func(uint64) ([]byte, bool) { return dict, true }, Limits{}); err != nil {
		t.Errorf("good dict: %v", err)
	}
}

func TestLimits(t *testing.T) {
	t.Parallel()
	p := encode(t, goldenBlock(), codec.None, nil, 0)
	reg := codec.DefaultRegistry()
	cases := map[string]Limits{
		"events":  {MaxEvents: 1},
		"raw_len": {MaxRawLen: 8},
		"payload": {MaxPayload: uint64(len(p)) - 1},
	}
	for name, lim := range cases {
		if _, err := Decode(p, reg, nil, lim); !errors.Is(err, ErrLimit) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Decode(p, reg, nil, Limits{MaxEvents: 2, MaxRawLen: 9, MaxPayload: uint64(len(p))}); err != nil {
		t.Errorf("exact limits: %v", err)
	}
}

func TestReadHeaderOnly(t *testing.T) {
	t.Parallel()
	b := Block{StartTick: 100, EndTick: 100 + 3_000_000, Events: []Event{
		{Tick: 100, Kind: KindOut, Data: bytes.Repeat([]byte("abc"), 100)},
		{Tick: 100 + 3_000_000, Kind: KindExit, Data: varint.AppendSvarint(nil, 0)},
	}}
	p := encode(t, b, codec.Flate, nil, 0)
	h, rest, err := ReadHeader(p)
	if err != nil {
		t.Fatal(err)
	}
	want := Header{Codec: codec.Flate, StartTick: 100, EndTick: 3_000_100, NEvents: 2, RawLen: h.RawLen}
	if h != want || h.RawLen == 0 {
		t.Fatalf("header = %+v", h)
	}
	if !bytes.Equal(rest, p[len(p)-len(rest):]) || len(rest) == 0 {
		t.Fatal("rest is not the payload suffix")
	}
	// Header is readable from a truncated payload that has lost the body.
	hdrLen := len(p) - len(rest)
	if h2, rest2, err := ReadHeader(p[:hdrLen]); err != nil || h2 != h || len(rest2) != 0 {
		t.Fatalf("header-only prefix: %+v %d %v", h2, len(rest2), err)
	}
	for i := 0; i < hdrLen; i++ {
		if _, _, err := ReadHeader(p[:i]); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("prefix %d: %v", i, err)
		}
	}
	if _, _, err := ReadHeader(rawPayload(0, 0, 9, 8, 0, 0, nil)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("start>end: %v", err)
	}
}

func TestIdleGapCostsFewBytes(t *testing.T) {
	t.Parallel()
	size := func(gap uint64) int {
		b := Block{StartTick: 0, EndTick: gap, Events: []Event{
			{Tick: 0, Kind: KindOut, Data: []byte("a")},
			{Tick: gap, Kind: KindOut, Data: []byte("b")},
		}}
		return len(encode(t, b, codec.None, nil, 0))
	}
	adjacent := size(1)
	// The gap is stored twice: as dt and as end_tick. 3.4 minutes of 100 µs
	// ticks is the largest 3-byte uvarint, so it costs 2+2 extra bytes.
	if delta := size(1<<21-1) - adjacent; delta > 5 {
		t.Errorf("3.4-minute gap costs %d extra bytes, want <= 5", delta)
	}
	// 5 minutes = 3,000,000 ticks is a 4-byte uvarint (the research doc says 3):
	// 3+3 extra bytes.
	if delta := size(5*60*10_000) - adjacent; delta > 6 {
		t.Errorf("5-minute gap costs %d extra bytes, want <= 6", delta)
	}
}

func TestDecodeDataAliasingIsClipped(t *testing.T) {
	t.Parallel()
	blk, err := decode(encode(t, goldenBlock(), codec.None, nil, 0))
	if err != nil {
		t.Fatal(err)
	}
	d := blk.Events[0].Data
	if cap(d) != len(d) {
		t.Fatalf("cap %d != len %d", cap(d), len(d))
	}
	_ = append(d, '!') // must not clobber the next event's payload
	if string(blk.Events[1].Data) != "x" {
		t.Fatalf("neighbour clobbered: %q", blk.Events[1].Data)
	}
}

func TestTruncationAtEveryOffset(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	dict := []byte("dict")
	dicts := func(uint64) ([]byte, bool) { return dict, true }
	reg := codec.DefaultRegistry()
	var blocks []Block
	for range 6 {
		blocks = append(blocks, randomBlock(rng))
	}
	for i, b := range blocks {
		for _, tc := range []struct {
			name   string
			id     codec.ID
			dict   []byte
			dictID uint64
		}{{"none", codec.None, nil, 0}, {"flate", codec.Flate, nil, 0}, {"flate with dict", codec.Flate, dict, 1}} {
			t.Run(fmt.Sprintf("block %d/%s", i, tc.name), func(t *testing.T) {
				t.Parallel()
				p := encode(t, b, tc.id, tc.dict, tc.dictID)
				for n := 0; n < len(p); n++ {
					if _, err := Decode(p[:n], reg, dicts, Limits{}); err == nil {
						t.Fatalf("block %d codec %v: prefix %d of %d decoded without error", i, tc.id, n, len(p))
					}
				}
			})
		}
	}
}

func TestCorruptionAtEveryOffsetNeverPanics(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(2))
	reg := codec.DefaultRegistry()
	for i := 0; i < 4; i++ {
		b := randomBlock(rng)
		for _, id := range []codec.ID{codec.None, codec.Flate, codec.Zstd} {
			p := encode(t, b, id, nil, 0)
			for off := range p {
				for _, flip := range []byte{0x01, 0xff} {
					q := append([]byte(nil), p...)
					q[off] ^= flip
					_, _ = Decode(q, reg, nil, Limits{}) // must not panic
				}
			}
		}
	}
}

func allocated() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}

func TestLyingCountsDoNotAllocate(t *testing.T) {
	big := uint64(1) << 40
	flateBody := func(raw []byte) []byte {
		c := mustCodec(t, codec.Flate)
		out, err := c.Compress(nil, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	// n_events = 1<<20 events fit within the default limit and a 64 MiB
	// raw_len, but the body is 3 bytes.
	lieN := rawPayload(0, 0, 0, 0, DefaultMaxEvents, DefaultMaxRawLen, []byte{0, 0, 0})
	// A flate body with lying plen values.
	hugePlen := varint.AppendUvarint([]byte{0, 0, 0, 0}, 1<<40)
	hugePlen = varint.AppendUvarint(hugePlen, 1<<40)
	zstdBody := func(raw []byte) []byte {
		out, err := mustCodec(t, codec.Zstd).Compress(nil, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	flateBomb := bytes.Repeat([]byte{0}, 8<<20)

	cases := []struct {
		name string
		p    []byte
		lim  Limits
	}{
		{"huge n_events", rawPayload(0, 0, 0, 0, big, 64, nil), Limits{}},
		{"huge raw_len", rawPayload(0, 0, 0, 0, 1, big, []byte{0, 0, 0}), Limits{}},
		{"max n_events and raw_len, tiny body", lieN, Limits{}},
		{"max n_events and raw_len, tiny flate body", append(rawPayload(1, 0, 0, 0, DefaultMaxEvents, DefaultMaxRawLen, nil), flateBody([]byte{0, 0, 0})...), Limits{}},
		{"max raw_len, tiny zstd body", append(rawPayload(uint64(codec.Zstd), 0, 0, 0, 1, DefaultMaxRawLen, nil), zstdBody([]byte{0, 0, 0})...), Limits{}},
		{"huge plen (none)", rawPayload(0, 0, 0, 0, 2, uint64(len(hugePlen)), hugePlen), Limits{}},
		{"huge plen (flate)", append(rawPayload(1, 0, 0, 0, 2, uint64(len(hugePlen)), nil), flateBody(hugePlen)...), Limits{}},
		{"flate bomb under small raw_len", append(rawPayload(1, 0, 0, 0, 1, 10, nil), flateBody(flateBomb)...), Limits{}},
		{"raw_len over caller limit", rawPayload(0, 0, 0, 0, 1, 1<<20, []byte{0, 0, 0}), Limits{MaxRawLen: 1 << 10}},
	}
	reg := codec.DefaultRegistry()
	for _, tc := range cases {
		before := allocated()
		_, err := Decode(tc.p, reg, nil, tc.lim)
		after := allocated()
		if err == nil {
			t.Errorf("%s: decoded without error", tc.name)
		}
		if delta := after - before; delta > 1<<20 {
			t.Errorf("%s: allocated %d bytes", tc.name, delta)
		}
	}
}

func BenchmarkDecodeLyingRawLen(b *testing.B) {
	reg := codec.DefaultRegistry()
	for _, id := range []codec.ID{codec.None, codec.Flate, codec.Zstd} {
		c, _ := reg.Get(id)
		body, err := c.Compress(nil, []byte{0, 0, 0}, nil)
		if err != nil {
			b.Fatal(err)
		}
		p := rawPayload(uint64(id), 0, 0, 0, 1, DefaultMaxRawLen, body)
		b.Run(id.String(), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Decode(p, reg, nil, Limits{}); err == nil {
					b.Fatal("lying raw_len decoded")
				}
			}
		})
	}
}

func FuzzDecode(f *testing.F) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 8; i++ {
		b := randomBlock(rng)
		var enc Encoder
		for _, id := range []codec.ID{codec.None, codec.Flate, codec.Zstd} {
			c, _ := codec.DefaultRegistry().Get(id)
			p, err := enc.Encode(b, c, nil, 0)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(p)
		}
	}
	f.Add(rawPayload(0, 0, 0, 0, DefaultMaxEvents, DefaultMaxRawLen, nil))
	reg := codec.DefaultRegistry()
	lim := Limits{MaxEvents: 1 << 12, MaxRawLen: 1 << 20, MaxPayload: 1 << 20}
	dicts := func(id uint64) ([]byte, bool) { return []byte("dict"), true }
	f.Fuzz(func(t *testing.T, p []byte) {
		blk, err := Decode(p, reg, dicts, lim)
		if err != nil {
			return
		}
		// Anything that decodes must re-encode to an equivalent block.
		h, _, err := ReadHeader(p)
		if err != nil {
			t.Fatalf("Decode ok but ReadHeader failed: %v", err)
		}
		var enc Encoder
		c, _ := reg.Get(h.Codec)
		var dict []byte
		if h.DictID != 0 {
			dict = []byte("dict")
		}
		q, err := enc.Encode(blk, c, dict, h.DictID)
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		again, err := Decode(q, reg, dicts, lim)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		if !reflect.DeepEqual(again, blk) {
			t.Fatalf("re-decode differs")
		}
	})
}
