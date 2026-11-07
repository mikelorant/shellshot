package chunk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"runtime"
	"testing"
	"testing/iotest"
	"unicode"
)

// Golden file: magic + HEAD + EVNT + indx. CRCs are CRC-32C over type||payload,
// computed independently of the package (see TestGoldenCRCs).
var (
	goldenHeadPayload = []byte{0x01, 0x02, 0x80, 0x01, 0x18, 0x01}
	goldenEvntPayload = []byte("\x00hello\r\n")
	goldenIndxPayload = []byte{}

	goldenFile = []byte{
		0x89, 0x53, 0x48, 0x4f, 0x54, 0x0d, 0x0a, 0x1a, // magic
		0x06, 0x00, 0x00, 0x00, 'H', 'E', 'A', 'D', 0x01, 0x02, 0x80, 0x01, 0x18, 0x01, 0x85, 0xa9, 0x19, 0xd7,
		0x08, 0x00, 0x00, 0x00, 'E', 'V', 'N', 'T', 0x00, 'h', 'e', 'l', 'l', 'o', 0x0d, 0x0a, 0xfe, 0x2e, 0x35, 0xbf,
		0x00, 0x00, 0x00, 0x00, 'i', 'n', 'd', 'x', 0xf2, 0x32, 0x73, 0xe5,
	}
)

type chunkData struct {
	t Type
	p []byte
}

func goldenChunks() []chunkData {
	return []chunkData{{TypeHead, goldenHeadPayload}, {TypeEvent, goldenEvntPayload}, {TypeIndex, goldenIndxPayload}}
}

func TestMagicBytes(t *testing.T) {
	t.Parallel()
	want := []byte{0x89, 0x53, 0x48, 0x4f, 0x54, 0x0d, 0x0a, 0x1a}
	if Magic != string(want) {
		t.Fatalf("Magic = %x, want %x", Magic, want)
	}
	var b bytes.Buffer
	if err := WriteMagic(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatalf("WriteMagic wrote %x", b.Bytes())
	}
	if err := ReadMagic(&b); err != nil {
		t.Fatalf("ReadMagic: %v", err)
	}
}

func TestReadMagicErrors(t *testing.T) {
	t.Parallel()
	if err := ReadMagic(bytes.NewReader(nil)); err != io.EOF {
		t.Errorf("empty: %v, want io.EOF", err)
	}
	for n := 1; n < len(Magic); n++ {
		err := ReadMagic(bytes.NewReader([]byte(Magic[:n])))
		if !errors.Is(err, ErrTruncated) || !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%d bytes: %v, want ErrTruncated", n, err)
		}
	}
	for i := range len(Magic) {
		b := []byte(Magic)
		b[i] ^= 0x01
		if err := ReadMagic(bytes.NewReader(b)); !errors.Is(err, ErrBadMagic) {
			t.Errorf("flip byte %d: %v, want ErrBadMagic", i, err)
		}
	}
	if err := ReadMagic(bytes.NewReader([]byte("{\"version\":2}"))); !errors.Is(err, ErrBadMagic) {
		t.Errorf("JSON header: %v, want ErrBadMagic", err)
	}
}

func TestCRC32CCheckValue(t *testing.T) {
	t.Parallel()
	// Standard CRC-32C check value for "123456789".
	if got := crc32.Checksum([]byte("123456789"), castagnoli); got != 0xe3069283 {
		t.Fatalf("castagnoli check value = %08x", got)
	}
}

func TestGoldenCRCs(t *testing.T) {
	t.Parallel()
	rest := goldenFile[len(Magic):]
	for _, c := range goldenChunks() {
		n := int(binary.LittleEndian.Uint32(rest))
		if n != len(c.p) {
			t.Fatalf("%s: golden len %d, payload %d", c.t, n, len(c.p))
		}
		want := crc32.Checksum(append(c.t[:len(c.t):len(c.t)], c.p...), crc32.MakeTable(crc32.Castagnoli))
		got := binary.LittleEndian.Uint32(rest[Overhead-CRCSize+n:])
		if got != want {
			t.Errorf("%s: golden crc %08x, computed %08x", c.t, got, want)
		}
		rest = rest[Overhead+n:]
	}
	if len(rest) != 0 {
		t.Fatalf("%d trailing golden bytes", len(rest))
	}
}

func TestWriteGolden(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := WriteMagic(&b); err != nil {
		t.Fatal(err)
	}
	for _, c := range goldenChunks() {
		if err := Write(&b, c.t, c.p); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(b.Bytes(), goldenFile) {
		t.Fatalf("written file differs from golden:\n got %x\nwant %x", b.Bytes(), goldenFile)
	}
}

func TestReadGolden(t *testing.T) {
	t.Parallel()
	rd := bytes.NewReader(goldenFile)
	if err := ReadMagic(rd); err != nil {
		t.Fatal(err)
	}
	r := NewReader(rd, Options{})
	var off int64
	for i, c := range goldenChunks() {
		typ, p, err := r.Next()
		if err != nil {
			t.Fatalf("chunk %d: %v", i, err)
		}
		if typ != c.t || !bytes.Equal(p, c.p) || p == nil {
			t.Errorf("chunk %d = %s %x (nil=%v), want %s %x", i, typ, p, p == nil, c.t, c.p)
		}
		off += int64(Overhead + len(c.p))
		if r.Offset() != off || r.LastGoodOffset() != off {
			t.Errorf("chunk %d: Offset %d LastGoodOffset %d, want %d", i, r.Offset(), r.LastGoodOffset(), off)
		}
	}
	for range 2 {
		if _, _, err := r.Next(); err != io.EOF {
			t.Fatalf("after last chunk: %v, want io.EOF", err)
		}
	}
}

func TestWriteSingleCall(t *testing.T) {
	t.Parallel()
	var cw countingWriter
	if err := Write(&cw, TypeEvent, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if cw.calls != 1 || cw.n != Overhead+3 {
		t.Fatalf("calls=%d bytes=%d, want 1 call of %d bytes", cw.calls, cw.n, Overhead+3)
	}
}

type countingWriter struct{ calls, n int }

func (c *countingWriter) Write(p []byte) (int, error) { c.calls++; c.n += len(p); return len(p), nil }

func TestWriteTooLarge(t *testing.T) {
	t.Parallel()
	var cw countingWriter
	err := Write(&cw, TypeBlob, make([]byte, MaxLen+1))
	if !errors.Is(err, ErrTooLarge) || cw.calls != 0 {
		t.Fatalf("err=%v calls=%d, want ErrTooLarge and no write", err, cw.calls)
	}
	if err := Write(io.Discard, TypeBlob, make([]byte, MaxLen)); err != nil {
		t.Fatalf("MaxLen payload: %v", err)
	}
}

func TestWritePropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("disk full")
	if err := Write(errWriter{want}, TypeHead, nil); err != want {
		t.Fatalf("got %v", err)
	}
}

type errWriter struct{ err error }

func (e errWriter) Write([]byte) (int, error) { return 0, e.err }

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	big := make([]byte, 3<<20+17) // forces several buffer growth steps
	for i := range big {
		big[i] = byte(i * 7)
	}
	in := []chunkData{
		{TypeHead, []byte{1}},
		{TypeEvent, big},
		{TypeMeta, nil},
		{TypeDict, bytes.Repeat([]byte{0xff}, 100)},
		{Type{'x', 0, 0xff, '\n'}, []byte("unknown type bytes")},
		{TypeGrid, big[:minGrow]},     // exactly one growth step
		{TypeGrid, big[:minGrow+1]},   // one byte over
		{TypeIndex, big[:2*minGrow]},  // exactly two steps
		{TypeBlob, big[:2*minGrow-1]}, // buffer already larger than need
	}
	var b bytes.Buffer
	for _, c := range in {
		if err := Write(&b, c.t, c.p); err != nil {
			t.Fatal(err)
		}
	}
	for name, src := range map[string]io.Reader{
		"plain":   bytes.NewReader(b.Bytes()),
		"onebyte": iotest.OneByteReader(bytes.NewReader(b.Bytes())),
		"dataerr": iotest.DataErrReader(bytes.NewReader(b.Bytes())),
	} {
		r := NewReader(src, Options{})
		for i, c := range in {
			typ, p, err := r.Next()
			if err != nil {
				t.Fatalf("%s chunk %d: %v", name, i, err)
			}
			if typ != c.t || !bytes.Equal(p, c.p) {
				t.Fatalf("%s chunk %d: got %s len %d, want %s len %d", name, i, typ, len(p), c.t, len(c.p))
			}
		}
		if _, _, err := r.Next(); err != io.EOF {
			t.Fatalf("%s: end: %v, want io.EOF", name, err)
		}
		if r.Offset() != int64(b.Len()) {
			t.Fatalf("%s: Offset %d, want %d", name, r.Offset(), b.Len())
		}
	}
}

func TestPayloadValidOnlyUntilNext(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	Write(&b, TypeEvent, []byte("first"))
	Write(&b, TypeEvent, []byte("other"))
	r := NewReader(&b, Options{})
	_, p, _ := r.Next()
	kept := bytes.Clone(p)
	if _, _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if string(kept) != "first" {
		t.Fatalf("clone changed: %q", kept)
	}
	if string(p) != "other" {
		t.Fatalf("expected the buffer to be reused, old slice = %q", p)
	}
}

func TestTypeAncillary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		typ  Type
		name string
		anc  bool
	}{
		{TypeHead, "HEAD", false},
		{TypeDict, "DICT", false},
		{TypeEvent, "EVNT", false},
		{TypeTheme, "them", true},
		{TypeMeta, "meta", true},
		{TypeGrid, "grid", true},
		{TypeIndex, "indx", true},
		{TypeBlob, "blob", true},
		{Type{'a', 'B', 'C', 'D'}, "aBCD", true},
		{Type{'A', 'b', 'c', 'd'}, "Abcd", false},
		{Type{0x20, 0, 0, 0}, `" \x00\x00\x00"`, true},
		{Type{0x00, 0, 0, 0}, `"\x00\x00\x00\x00"`, false},
	}
	for _, tc := range tests {
		if got := tc.typ.Ancillary(); got != tc.anc {
			t.Errorf("%s.Ancillary() = %v, want %v", tc.name, got, tc.anc)
		}
		if got := tc.typ.String(); got != tc.name {
			t.Errorf("String() = %q, want %q", got, tc.name)
		}
	}
	known := []Type{TypeHead, TypeDict, TypeEvent, TypeTheme, TypeMeta, TypeGrid, TypeIndex, TypeBlob}
	for _, typ := range known {
		if got, want := typ.Ancillary(), unicode.IsLower(rune(typ[0])); got != want {
			t.Errorf("%s: Ancillary() = %v disagrees with lowercase-first-letter rule (%v)", typ, got, want)
		}
	}
	// Exact on-disk spelling.
	if got := string(TypeHead[:]) + string(TypeDict[:]) + string(TypeEvent[:]) + string(TypeTheme[:]) +
		string(TypeMeta[:]) + string(TypeGrid[:]) + string(TypeIndex[:]) + string(TypeBlob[:]); got != "HEADDICTEVNTthemmetagridindxblob" {
		t.Errorf("type bytes = %q", got)
	}
}

// goldenBoundaries returns the end offsets of the magic and of each chunk in
// file.
func boundaries(chunks []chunkData) []int {
	ends := []int{len(Magic)}
	for _, c := range chunks {
		ends = append(ends, ends[len(ends)-1]+Overhead+len(c.p))
	}
	return ends
}

// multiChunkFile builds a larger file than the golden one so truncation covers
// more varied lengths.
func multiChunkFile(t testing.TB) ([]byte, []chunkData) {
	t.Helper()
	body := make([]byte, 700)
	for i := range body {
		body[i] = byte(i*31 + 5)
	}
	chunks := append(goldenChunks()[:2:2],
		chunkData{TypeDict, nil},
		chunkData{TypeEvent, body},
		chunkData{TypeTheme, []byte{9}},
		chunkData{TypeIndex, body[:40]},
	)
	var b bytes.Buffer
	WriteMagic(&b)
	for _, c := range chunks {
		if err := Write(&b, c.t, c.p); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes(), chunks
}

func TestTruncationAtEveryOffset(t *testing.T) {
	t.Parallel()
	files := map[string]struct {
		data   []byte
		chunks []chunkData
	}{"golden": {goldenFile, goldenChunks()}}
	mf, mc := multiChunkFile(t)
	files["multi"] = struct {
		data   []byte
		chunks []chunkData
	}{mf, mc}

	for name, f := range files {
		ends := boundaries(f.chunks)
		for cut := 0; cut <= len(f.data); cut++ {
			for _, wrap := range []func(io.Reader) io.Reader{
				func(r io.Reader) io.Reader { return r },
				iotest.OneByteReader,
				iotest.DataErrReader,
			} {
				rd := wrap(bytes.NewReader(f.data[:cut]))
				err := ReadMagic(rd)
				if cut < len(Magic) {
					want := ErrTruncated
					if cut == 0 {
						want = io.EOF
					}
					if !errors.Is(err, want) {
						t.Fatalf("%s cut %d: ReadMagic %v, want %v", name, cut, err, want)
					}
					continue
				}
				if err != nil {
					t.Fatalf("%s cut %d: ReadMagic %v", name, cut, err)
				}
				complete := 0
				for complete < len(f.chunks) && ends[complete+1] <= cut {
					complete++
				}
				r := NewReader(rd, Options{})
				for i := range complete {
					typ, p, err := r.Next()
					if err != nil || typ != f.chunks[i].t || !bytes.Equal(p, f.chunks[i].p) {
						t.Fatalf("%s cut %d chunk %d: %s %x %v", name, cut, i, typ, p, err)
					}
				}
				_, _, err = r.Next()
				if cut == ends[complete] {
					if err != io.EOF {
						t.Fatalf("%s cut %d (boundary): %v, want io.EOF", name, cut, err)
					}
				} else if !errors.Is(err, ErrTruncated) || !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("%s cut %d: %v, want ErrTruncated", name, cut, err)
				}
				if want := int64(ends[complete] - len(Magic)); r.Offset() != want || r.LastGoodOffset() != want {
					t.Fatalf("%s cut %d: Offset %d LastGoodOffset %d, want %d", name, cut, r.Offset(), r.LastGoodOffset(), want)
				}
				if _, _, err2 := r.Next(); err2 != err {
					t.Fatalf("%s cut %d: error not sticky: %v then %v", name, cut, err, err2)
				}
			}
		}
	}
}

func TestBitFlipAtEveryByte(t *testing.T) {
	t.Parallel()
	data, chunks := multiChunkFile(t)
	ends := boundaries(chunks)
	for pos := range data {
		for bit := range 8 {
			b := bytes.Clone(data)
			b[pos] ^= 1 << bit
			rd := bytes.NewReader(b)
			if err := ReadMagic(rd); err != nil {
				if pos < len(Magic) && errors.Is(err, ErrBadMagic) {
					continue
				}
				t.Fatalf("byte %d bit %d: ReadMagic %v", pos, bit, err)
			}
			if pos < len(Magic) {
				t.Fatalf("byte %d bit %d: flipped magic accepted", pos, bit)
			}
			// Index of the chunk containing pos, and the field hit.
			ci := 0
			for ends[ci+1] <= pos {
				ci++
			}
			inLen := pos-ends[ci] < 4
			r := NewReader(rd, Options{})
			for i := range ci {
				typ, p, err := r.Next()
				if err != nil || typ != chunks[i].t || !bytes.Equal(p, chunks[i].p) {
					t.Fatalf("byte %d bit %d: untouched chunk %d: %v", pos, bit, i, err)
				}
			}
			typ, p, err := r.Next()
			if err == nil {
				t.Fatalf("byte %d bit %d: silent success: %s %x", pos, bit, typ, p)
			}
			switch {
			case inLen:
				// A wrong length may read as too large, truncated or corrupt.
				if !errors.Is(err, ErrTooLarge) && !errors.Is(err, ErrTruncated) && !errors.Is(err, ErrCorrupt) {
					t.Fatalf("byte %d bit %d (len): %v", pos, bit, err)
				}
			default:
				if !errors.Is(err, ErrCorrupt) {
					t.Fatalf("byte %d bit %d (chunk %d): %v, want ErrCorrupt", pos, bit, ci, err)
				}
			}
			if want := int64(ends[ci] - len(Magic)); r.LastGoodOffset() != want {
				t.Fatalf("byte %d bit %d: LastGoodOffset %d, want %d", pos, bit, r.LastGoodOffset(), want)
			}
		}
	}
}

func TestMaxLenCap(t *testing.T) {
	t.Parallel()
	mk := func(n int) []byte {
		var b bytes.Buffer
		Write(&b, TypeEvent, make([]byte, n))
		return b.Bytes()
	}
	r := NewReader(bytes.NewReader(mk(16)), Options{MaxLen: 16})
	if _, p, err := r.Next(); err != nil || len(p) != 16 {
		t.Fatalf("len == cap: %d %v", len(p), err)
	}
	r = NewReader(bytes.NewReader(mk(17)), Options{MaxLen: 16})
	if _, _, err := r.Next(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("len == cap+1: %v, want ErrTooLarge", err)
	}
	if r.Offset() != 0 {
		t.Fatalf("Offset after ErrTooLarge = %d", r.Offset())
	}

	// Options.MaxLen above the hard limit is lowered to it; zero is the default.
	if got := NewReader(nil, Options{MaxLen: 1 << 31}).maxLen; got != MaxLen {
		t.Errorf("hard clamp: %d", got)
	}
	if got := NewReader(nil, Options{}).maxLen; got != DefaultMaxLen {
		t.Errorf("default: %d", got)
	}
}

// allocDelta returns the bytes allocated while running f.
func allocDelta(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestLyingLengthDoesNotAllocate(t *testing.T) {
	// A 20-byte file whose first chunk claims 60 MiB.
	lie := make([]byte, 20)
	binary.LittleEndian.PutUint32(lie, 60<<20)
	copy(lie[4:], "EVNT")
	const budget = 1 << 20

	t.Run("default cap rejects before allocating", func(t *testing.T) {
		var err error
		got := allocDelta(func() { _, _, err = NewReader(bytes.NewReader(lie), Options{}).Next() })
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
		if got > budget {
			t.Fatalf("allocated %d bytes", got)
		}
	})
	t.Run("within cap reads incrementally", func(t *testing.T) {
		var err error
		got := allocDelta(func() { _, _, err = NewReader(bytes.NewReader(lie), Options{MaxLen: MaxLen}).Next() })
		if !errors.Is(err, ErrTruncated) {
			t.Fatalf("err = %v, want ErrTruncated", err)
		}
		if got > budget {
			t.Fatalf("allocated %d bytes for a 20-byte input claiming 60 MiB", got)
		}
	})
	t.Run("many readers", func(t *testing.T) {
		got := allocDelta(func() {
			for range 100 {
				NewReader(bytes.NewReader(lie), Options{MaxLen: MaxLen}).Next()
			}
		})
		if got > 100*budget/4 {
			t.Fatalf("allocated %d bytes over 100 runs", got)
		}
	})
}

func TestTrailer(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := WriteTrailer(&b, 0x01020304); err != nil {
		t.Fatal(err)
	}
	want := []byte{0x04, 0x03, 0x02, 0x01, 'S', 'H', 'T', 'E'}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatalf("trailer = %x, want %x", b.Bytes(), want)
	}
	n, ok := ParseTrailer([8]byte(b.Bytes()))
	if !ok || n != 0x01020304 {
		t.Fatalf("ParseTrailer = %d, %v", n, ok)
	}
	bad := [][8]byte{
		{4, 3, 2, 1, 'S', 'H', 'T', 'F'},
		{4, 3, 2, 1, 's', 'h', 't', 'e'},
		{11, 0, 0, 0, 'S', 'H', 'T', 'E'}, // shorter than the smallest chunk
		{},
	}
	for _, x := range bad {
		if n, ok := ParseTrailer(x); ok {
			t.Errorf("ParseTrailer(%x) = %d, true", x, n)
		}
	}
	if n, ok := ParseTrailer([8]byte{12, 0, 0, 0, 'S', 'H', 'T', 'E'}); !ok || n != 12 {
		t.Errorf("empty-payload footer: %d %v", n, ok)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestUnderlyingErrorIsNotTruncation(t *testing.T) {
	t.Parallel()
	want := errors.New("boom")
	_, _, err := NewReader(failingReader{want}, Options{}).Next()
	if !errors.Is(err, want) || errors.Is(err, ErrTruncated) || err == io.EOF {
		t.Fatalf("err = %v", err)
	}
}

func FuzzReader(f *testing.F) {
	mf, _ := multiChunkFile(f)
	f.Add(goldenFile[len(Magic):])
	f.Add(mf[len(Magic):])
	f.Add(mf[len(Magic) : len(mf)-3])
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0x03, 'E', 'V', 'N', 'T'})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 'E', 'V', 'N', 'T', 1, 2, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		const capLen = 1 << 16
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		r := NewReader(bytes.NewReader(data), Options{MaxLen: capLen})
		var sum int64
		for {
			typ, p, err := r.Next()
			if err != nil {
				if err != io.EOF && !errors.Is(err, ErrTruncated) && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrTooLarge) {
					t.Fatalf("unexpected error %v", err)
				}
				break
			}
			if len(p) > capLen {
				t.Fatalf("payload %d over cap", len(p))
			}
			// Whatever was accepted must re-encode to exactly the input bytes.
			var b bytes.Buffer
			if err := Write(&b, typ, p); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(b.Bytes(), data[sum:sum+int64(b.Len())]) {
				t.Fatalf("accepted chunk does not re-encode to its input bytes")
			}
			sum += int64(b.Len())
			if r.Offset() != sum {
				t.Fatalf("Offset %d, want %d", r.Offset(), sum)
			}
		}
		if r.Offset() != sum || r.Offset() > int64(len(data)) {
			t.Fatalf("Offset %d, consumed %d, input %d", r.Offset(), sum, len(data))
		}
		// Memory must track the input, never the declared lengths: allow the
		// buffer growth steps (at most capLen) plus slack per accepted chunk.
		runtime.ReadMemStats(&after)
		if d := after.TotalAlloc - before.TotalAlloc; d > uint64(4*len(data))+4*capLen+(1<<20) {
			t.Fatalf("allocated %d bytes for %d input bytes", d, len(data))
		}
	})
}
