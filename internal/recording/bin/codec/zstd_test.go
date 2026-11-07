package codec

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// terminalBlocks returns n synthetic terminal-output blocks of roughly size
// bytes: coloured prompts, ls -la listings, git status, compiler diagnostics
// and cursor-addressed redraws, with varying names and numbers.
func terminalBlocks(seed int64, n, size int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	words := []string{"main", "session", "render", "frame", "codec", "reader", "writer", "config",
		"parser", "buffer", "palette", "sprite", "layout", "export", "record", "replay"}
	exts := []string{".go", ".md", ".json", ".txt", "_test.go", ".shot"}
	name := func() string {
		return words[rng.Intn(len(words))] + exts[rng.Intn(len(exts))]
	}
	line := func(b *bytes.Buffer) {
		switch rng.Intn(6) {
		case 0:
			fmt.Fprintf(b, "\x1b[1;32muser@host\x1b[0m:\x1b[1;34m~/src/shellshot\x1b[0m$ %s\r\n",
				[]string{"ls -la", "git status", "go test ./...", "go build ./cmd/shellshot", "git diff"}[rng.Intn(5)])
		case 1:
			fmt.Fprintf(b, "-rw-r--r--  1 user staff %6d Mar %2d %02d:%02d \x1b[0m%s\r\n",
				rng.Intn(100000), 1+rng.Intn(28), rng.Intn(24), rng.Intn(60), name())
		case 2:
			fmt.Fprintf(b, "\x1b[31m\tmodified:   internal/%s/%s\x1b[0m\r\n", words[rng.Intn(len(words))], name())
		case 3:
			fmt.Fprintf(b, "internal/%s/%s:%d:%d: \x1b[1;31merror:\x1b[0m undefined: %s\r\n",
				words[rng.Intn(len(words))], name(), 1+rng.Intn(400), 1+rng.Intn(80), words[rng.Intn(len(words))])
		case 4:
			fmt.Fprintf(b, "\x1b[%d;%dH\x1b[2K\x1b[38;5;%dm%s\x1b[0m", 1+rng.Intn(24), 1+rng.Intn(80), rng.Intn(256), name())
		default:
			fmt.Fprintf(b, "ok  \tgithub.com/michaellorant/shellshot/internal/%s\t%d.%03ds\r\n",
				words[rng.Intn(len(words))], rng.Intn(5), rng.Intn(1000))
		}
	}
	out := make([][]byte, n)
	for i := range out {
		var b bytes.Buffer
		for b.Len() < size {
			line(&b)
		}
		out[i] = b.Bytes()
	}
	return out
}

func newZstd(t testing.TB, level zstd.EncoderLevel) *ZstdCodec {
	t.Helper()
	c, err := NewZstdCodec(ZstdOptions{Level: level})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestZstdRoundTrip(t *testing.T) {
	train := terminalBlocks(1, 60, 1500)
	rawDict := BuildDict(train, 8<<10)
	fmtDict, err := BuildZstdDict(train, 8<<10)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{
		"empty":        {},
		"tiny":         []byte("hi"),
		"random1MiB":   randomBytes(1 << 20),
		"compress1MiB": compressible(1 << 20),
		"terminal":     terminalBlocks(2, 1, 4000)[0],
	}
	dicts := map[string][]byte{"nodict": nil, "rawdict": rawDict, "zstddict": fmtDict}
	levels := []zstd.EncoderLevel{0, zstd.SpeedFastest, zstd.SpeedDefault, zstd.SpeedBetterCompression, zstd.SpeedBestCompression}
	for _, level := range levels {
		c := newZstd(t, level)
		for in, src := range inputs {
			for dn, dict := range dicts {
				t.Run(fmt.Sprintf("level%d/%s/%s", level, in, dn), func(t *testing.T) {
					prefix := []byte("prefix")
					comp, err := c.Compress(append([]byte(nil), prefix...), src, dict)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.HasPrefix(comp, prefix) {
						t.Fatal("Compress did not append to dst")
					}
					comp = comp[len(prefix):]
					if len(comp) == 0 {
						t.Fatal("empty input must produce a non-empty frame")
					}
					got, err := c.Decompress(append([]byte(nil), prefix...), comp, dict, len(src))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.HasPrefix(got, prefix) || !bytes.Equal(got[len(prefix):], src) {
						t.Fatal("round trip mismatch")
					}
					if len(src) > 0 {
						if _, err := c.Decompress(nil, comp, dict, len(src)-1); !errors.Is(err, ErrTooLarge) {
							t.Fatalf("maxLen-1: err = %v, want ErrTooLarge", err)
						}
					}
					// Spare capacity in dst is reused.
					roomy := make([]byte, 0, len(src)+64)
					got, err = c.Decompress(roomy, comp, dict, len(src))
					if err != nil || !bytes.Equal(got, src) {
						t.Fatalf("roomy dst: %v", err)
					}
				})
			}
		}
	}
}

func TestZstdLevelValidation(t *testing.T) {
	if _, err := NewZstdCodec(ZstdOptions{Level: 99}); err == nil {
		t.Fatal("level 99 accepted")
	}
	if c := newZstd(t, 0); c.level != zstd.SpeedDefault {
		t.Fatalf("zero level = %v, want SpeedDefault", c.level)
	}
	if c := newZstd(t, zstd.SpeedBestCompression); c.ID() != Zstd {
		t.Fatalf("ID = %v", c.ID())
	}
}

func TestZstdWrongDict(t *testing.T) {
	c := newZstd(t, 0)
	train := terminalBlocks(1, 60, 1500)
	dictA := BuildDict(train, 8<<10)
	dictB := BuildDict(terminalBlocks(9, 60, 1500), 8<<10)
	fmtA, err := BuildZstdDict(train, 8<<10)
	if err != nil {
		t.Fatal(err)
	}
	fmtB, err := BuildZstdDict(terminalBlocks(9, 60, 1500), 8<<10)
	if err != nil {
		t.Fatal(err)
	}
	src := terminalBlocks(3, 1, 3000)[0]
	for _, tc := range []struct {
		name       string
		enc, wrong []byte
	}{
		{"raw/other-raw", dictA, dictB},
		{"raw/none", dictA, nil},
		{"raw/formatted", dictA, fmtB},
		{"formatted/none", fmtA, nil},
		{"formatted/other-formatted", fmtA, fmtB},
		{"formatted/other-raw", fmtA, dictB},
	} {
		comp, err := c.Compress(nil, src, tc.enc)
		if err != nil {
			t.Fatal(err)
		}
		// The dictionary id is in the frame header, so the wrong dictionary
		// is detected (the encoder used the dictionary for this block).
		if _, err := c.Decompress(nil, comp, tc.wrong, len(src)); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: err = %v, want ErrCorrupt", tc.name, err)
		}
	}
	// A frame written without a dictionary decodes with any dictionary.
	comp, _ := c.Compress(nil, src, nil)
	if got, err := c.Decompress(nil, comp, dictA, len(src)); err != nil || !bytes.Equal(got, src) {
		t.Errorf("nodict frame with dict: %v", err)
	}
}

func TestZstdInvalidFormattedDict(t *testing.T) {
	c := newZstd(t, 0)
	bad := append(append([]byte(nil), zstdDictMagic...), 1, 2, 3, 4, 5)
	if _, err := c.Compress(nil, []byte("abc"), bad); err == nil {
		t.Fatal("Compress accepted a malformed dictionary")
	}
	good, _ := c.Compress(nil, []byte("abc"), nil)
	if _, err := c.Decompress(nil, good, bad, 3); err == nil {
		t.Fatal("Decompress accepted a malformed dictionary")
	}
}

func TestZstdDictCache(t *testing.T) {
	c := newZstd(t, zstd.SpeedFastest)
	src := compressible(2000)
	// More distinct dictionaries than cache entries: still correct.
	for i := 0; i < 3*maxZstdDictCacheEntries; i++ {
		dict := bytes.Repeat([]byte{byte(i), 'x', byte(i >> 3)}, 64)
		comp, err := c.Compress(nil, src, dict)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.Decompress(nil, comp, dict, len(src))
		if err != nil || !bytes.Equal(got, src) {
			t.Fatalf("dict %d: %v", i, err)
		}
	}
	c.mu.Lock()
	n := len(c.dicts)
	c.mu.Unlock()
	if n > maxZstdDictCacheEntries {
		t.Fatalf("cache holds %d entries, limit %d", n, maxZstdDictCacheEntries)
	}
	// The cache owns a copy: mutating the caller's slice afterwards must not
	// change how the original dictionary content is used.
	dict := bytes.Repeat([]byte("abcdefgh"), 100)
	orig := bytes.Clone(dict)
	comp, _ := c.Compress(nil, src, dict)
	dict[0] ^= 0xff
	got, err := c.Decompress(nil, comp, orig, len(src))
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("mutated-caller-dict: %v", err)
	}
}

func TestZstdDecompressionBomb(t *testing.T) {
	c := newZstd(t, zstd.SpeedFastest)
	const bomb = 100 << 20
	zeros := make([]byte, bomb)
	comp, err := c.Compress(nil, zeros, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(comp) > 1<<20 {
		t.Fatalf("zeros compressed to %d bytes; test premise broken", len(comp))
	}

	// The same bomb as a streaming frame, which carries no content size, so
	// only the capacity limit can stop it.
	var stream bytes.Buffer
	sw, err := zstd.NewWriter(&stream, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatal(err)
	}
	for off := 0; off < bomb; off += 1 << 20 {
		if _, err := sw.Write(zeros[off : off+1<<20]); err != nil {
			t.Fatal(err)
		}
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
	var h zstd.Header
	if err := h.Decode(stream.Bytes()); err != nil {
		t.Fatal(err)
	}
	t.Logf("declared-size frame %d bytes; streaming frame %d bytes (HasFCS=%v)", len(comp), stream.Len(), h.HasFCS)

	const maxLen = 1 << 20
	for name, in := range map[string][]byte{"declared": comp, "streaming": stream.Bytes()} {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		out, err := c.Decompress(nil, in, nil, maxLen)
		runtime.ReadMemStats(&after)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("%s: err = %v, want ErrTooLarge", name, err)
		}
		if out != nil {
			t.Fatalf("%s: got %d bytes on error, want nil dst", name, len(out))
		}
		alloc := after.TotalAlloc - before.TotalAlloc
		t.Logf("%s bomb: allocated %d bytes for maxLen %d", name, alloc, maxLen)
		if alloc > 8*maxLen {
			t.Fatalf("%s: allocated %d bytes decompressing a bomb with maxLen %d", name, alloc, maxLen)
		}
	}

	// Exactly at the limit succeeds; the codec stays usable after a failure.
	comp, _ = c.Compress(nil, make([]byte, maxLen), nil)
	got, err := c.Decompress(nil, comp, nil, maxLen)
	if err != nil || len(got) != maxLen {
		t.Fatalf("at-limit: len %d err %v", len(got), err)
	}

	// A caller allowing more than the hard ceiling still cannot exceed it.
	if _, err := c.Decompress(nil, comp, nil, 1<<62); err != nil {
		t.Fatalf("huge maxLen with small frame: %v", err)
	}
}

func TestZstdCorruptInput(t *testing.T) {
	c := newZstd(t, 0)
	src := terminalBlocks(4, 1, 4096)[0]
	good, _ := c.Compress(nil, src, nil)
	cases := map[string][]byte{
		"garbage":   bytes.Repeat([]byte{0xff}, 64),
		"truncated": good[:len(good)/2],
		"trailing":  append(append([]byte(nil), good...), 1, 2, 3),
		"empty":     {},
		"magic":     zstdDictMagic,
	}
	mustFail := map[string]bool{"garbage": true, "truncated": true, "trailing": true, "empty": true, "magic": true}
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 200; i++ {
		flipped := bytes.Clone(good)
		flipped[rng.Intn(len(flipped))] ^= byte(1 + rng.Intn(255))
		cases[fmt.Sprintf("flip%03d", i)] = flipped
	}
	for name, in := range cases {
		out, err := c.Decompress(nil, in, nil, 1<<20)
		switch {
		case err == nil && mustFail[name]:
			t.Errorf("%s: no error (out %d bytes)", name, len(out))
		case err == nil && !bytes.Equal(out, src):
			t.Errorf("%s: silently decoded to different data", name)
		case err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrTooLarge):
			t.Errorf("%s: err %v does not wrap ErrCorrupt/ErrTooLarge", name, err)
		case err != nil && out != nil:
			t.Errorf("%s: non-nil output with error", name)
		}
	}
}

func TestZstdConcurrentUse(t *testing.T) {
	c := newZstd(t, zstd.SpeedFastest)
	dicts := [][]byte{nil, testDict, BuildDict(terminalBlocks(1, 20, 1000), 4<<10)}
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				src := terminalBlocks(int64(g*1000+i), 1, 500+g*50+i)[0]
				dict := dicts[(g+i)%len(dicts)]
				comp, err := c.Compress(nil, src, dict)
				if err != nil {
					t.Error(err)
					return
				}
				got, err := c.Decompress(nil, comp, dict, len(src))
				if err != nil || !bytes.Equal(got, src) {
					t.Errorf("concurrent round trip failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func FuzzZstdDecompress(f *testing.F) {
	c := newZstd(f, 0)
	dict := BuildDict(terminalBlocks(1, 20, 1000), 4<<10)
	src := compressible(2048)
	good, _ := c.Compress(nil, src, nil)
	goodDict, _ := c.Compress(nil, src, dict)
	empty, _ := c.Compress(nil, nil, nil)
	f.Add(good, false, 4096)
	f.Add(goodDict, true, 100)
	f.Add(empty, false, 0)
	f.Add([]byte{}, false, 0)
	f.Add([]byte{0x28, 0xb5, 0x2f, 0xfd}, true, 16)
	f.Fuzz(func(t *testing.T, in []byte, useDict bool, maxLen int) {
		maxLen %= 1 << 16
		if maxLen < 0 {
			maxLen = -maxLen
		}
		var d []byte
		if useDict {
			d = dict
		}
		out, err := c.Decompress(nil, in, d, maxLen)
		if len(out) > maxLen {
			t.Fatalf("output %d > maxLen %d", len(out), maxLen)
		}
		if err != nil && out != nil {
			t.Fatal("non-nil output with error")
		}
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err %v does not wrap ErrCorrupt/ErrTooLarge", err)
		}
	})
}

func TestBuildDict(t *testing.T) {
	if BuildDict(nil, 100) != nil || BuildDict([][]byte{[]byte("abc")}, 0) != nil {
		t.Fatal("empty input or zero size must return nil")
	}
	samples := terminalBlocks(1, 30, 1000)
	a, b := BuildDict(samples, 4096), BuildDict(samples, 4096)
	if !bytes.Equal(a, b) {
		t.Fatal("BuildDict is not deterministic")
	}
	if len(a) != 4096 {
		t.Fatalf("len = %d, want 4096 (samples total more)", len(a))
	}
	if small := BuildDict([][]byte{[]byte("hello world, hello")}, 4096); string(small) != "hello world, hello" {
		t.Fatalf("small = %q", small)
	}
	// The sample sharing the most with the others goes last.
	shared := []byte("common-prefix-common-prefix-common-prefix")
	odd := []byte("zzzzqqqqwwwwxxxxyyyy")
	got := BuildDict([][]byte{odd, shared, shared, append(bytes.Clone(shared), "x"...)}, 1<<10)
	if !bytes.HasSuffix(got, shared) && !bytes.HasSuffix(got, append(bytes.Clone(shared), "x"...)) {
		t.Fatalf("most shared sample is not last: %q", got)
	}
}

// TestDictShrinksSmallBlocks checks that dictionaries built from earlier
// blocks make small similar blocks measurably smaller, with every codec.
func TestDictShrinksSmallBlocks(t *testing.T) {
	train := terminalBlocks(1, 200, 1500)
	test := terminalBlocks(2, 100, 1500)
	rawDict := BuildDict(train, 16<<10)
	fmtDict, err := BuildZstdDict(train, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	fc, _ := NewFlateCodec(DefaultFlateOptions())
	zc := newZstd(t, zstd.SpeedDefault)
	total := func(c Codec, dict []byte) int {
		n := 0
		for _, blk := range test {
			comp, err := c.Compress(nil, blk, dict)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Decompress(nil, comp, dict, len(blk))
			if err != nil || !bytes.Equal(got, blk) {
				t.Fatalf("round trip: %v", err)
			}
			n += len(comp)
		}
		return n
	}
	raw := 0
	for _, b := range test {
		raw += len(b)
	}
	for _, tc := range []struct {
		name    string
		c       Codec
		dict    []byte
		minGain float64 // required reduction versus no dictionary
	}{
		{"flate/raw", fc, rawDict, 0.30},
		{"zstd/raw", zc, rawDict, 0.30},
		{"zstd/formatted", zc, fmtDict, 0.30},
	} {
		without, with := total(tc.c, nil), total(tc.c, tc.dict)
		gain := 1 - float64(with)/float64(without)
		t.Logf("%-15s raw %d B (100 blocks of ~1.5 KiB): no dict %d B (%.2fx), dict %d B (%.2fx), %.1f%% smaller; dict size %d B",
			tc.name, raw, without, float64(raw)/float64(without), with, float64(raw)/float64(with), gain*100, len(tc.dict))
		if gain < tc.minGain {
			t.Errorf("%s: dictionary only %.1f%% smaller, want >= %.0f%%", tc.name, gain*100, tc.minGain*100)
		}
	}
}

// BenchmarkCodecs compares flate and zstd levels, without and with a
// dictionary, on synthetic terminal output in small (2 KiB) and large (64 KiB)
// independent blocks. The ratio metric is raw bytes / compressed bytes.
func BenchmarkCodecs(b *testing.B) {
	type codecCase struct {
		name string
		c    Codec
	}
	mkFlate := func(level int) Codec {
		c, err := NewFlateCodec(FlateOptions{Level: level})
		if err != nil {
			b.Fatal(err)
		}
		return c
	}
	codecs := []codecCase{
		{"flate-1", mkFlate(1)},
		{"flate-6", mkFlate(6)},
		{"flate-9", mkFlate(9)},
		{"zstd-fastest", newZstd(b, zstd.SpeedFastest)},
		{"zstd-default", newZstd(b, zstd.SpeedDefault)},
		{"zstd-better", newZstd(b, zstd.SpeedBetterCompression)},
		{"zstd-best", newZstd(b, zstd.SpeedBestCompression)},
	}
	for _, bs := range []struct {
		name string
		size int
	}{{"2KiB", 2 << 10}, {"64KiB", 64 << 10}} {
		blocks := terminalBlocks(7, 64, bs.size)
		dict := BuildDict(terminalBlocks(8, 200, bs.size), 16<<10)
		raw := 0
		for _, blk := range blocks {
			raw += len(blk)
		}
		for _, withDict := range []bool{false, true} {
			var d []byte
			dn := "nodict"
			if withDict {
				d, dn = dict, "dict"
			}
			for _, cc := range codecs {
				comp := make([][]byte, len(blocks))
				compSize := 0
				for i, blk := range blocks {
					var err error
					if comp[i], err = cc.c.Compress(nil, blk, d); err != nil {
						b.Fatal(err)
					}
					compSize += len(comp[i])
				}
				name := bs.name + "/" + dn + "/" + cc.name
				b.Run("compress/"+name, func(b *testing.B) {
					b.SetBytes(int64(raw))
					var buf []byte
					for i := 0; i < b.N; i++ {
						for _, blk := range blocks {
							buf, _ = cc.c.Compress(buf[:0], blk, d)
						}
					}
					b.ReportMetric(float64(raw)/float64(compSize), "ratio")
				})
				b.Run("decompress/"+name, func(b *testing.B) {
					b.SetBytes(int64(raw))
					var buf []byte
					for i := 0; i < b.N; i++ {
						for j := range comp {
							var err error
							if buf, err = cc.c.Decompress(buf[:0], comp[j], d, bs.size*2); err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}
		}
	}
}
