package codec

import (
	"bytes"
	"compress/flate"
	"errors"
	"math/rand"
	"runtime"
	"sync"
	"testing"
)

func testCodecs(t *testing.T) []Codec {
	t.Helper()
	fc, err := NewFlateCodec(DefaultFlateOptions())
	if err != nil {
		t.Fatal(err)
	}
	zc, err := NewZstdCodec(ZstdOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return []Codec{NoneCodec{}, fc, zc}
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(1)).Read(b)
	return b
}

func compressible(n int) []byte {
	unit := []byte("\x1b[32muser@host\x1b[0m:~$ ls -la\r\n")
	return bytes.Repeat(unit, n/len(unit)+1)[:n]
}

var testDict = []byte("\x1b[32muser@host\x1b[0m:~$ total drwxr-xr-x\r\n")

func TestRoundTrip(t *testing.T) {
	inputs := map[string][]byte{
		"empty":        {},
		"tiny":         []byte("hi"),
		"random1MiB":   randomBytes(1 << 20),
		"compress1MiB": compressible(1 << 20),
	}
	dicts := map[string][]byte{"nodict": nil, "dict": testDict}
	for _, c := range testCodecs(t) {
		for in, src := range inputs {
			for dn, dict := range dicts {
				t.Run(c.ID().String()+"/"+in+"/"+dn, func(t *testing.T) {
					prefix := []byte("prefix")
					comp, err := c.Compress(append([]byte(nil), prefix...), src, dict)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.HasPrefix(comp, prefix) {
						t.Fatal("Compress did not append to dst")
					}
					comp = comp[len(prefix):]
					got, err := c.Decompress(append([]byte(nil), prefix...), comp, dict, len(src))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got[len(prefix):], src) {
						t.Fatal("round trip mismatch")
					}
					if len(src) > 0 {
						if _, err := c.Decompress(nil, comp, dict, len(src)-1); !errors.Is(err, ErrTooLarge) {
							t.Fatalf("maxLen-1: err = %v, want ErrTooLarge", err)
						}
					}
				})
			}
		}
	}
}

func TestFlateLevels(t *testing.T) {
	src := compressible(64 << 10)
	for _, level := range []int{flate.HuffmanOnly, flate.DefaultCompression, flate.NoCompression, flate.BestSpeed, flate.BestCompression} {
		c, err := NewFlateCodec(FlateOptions{Level: level})
		if err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		comp, _ := c.Compress(nil, src, nil)
		got, err := c.Decompress(nil, comp, nil, len(src))
		if err != nil || !bytes.Equal(got, src) {
			t.Fatalf("level %d: round trip failed: %v", level, err)
		}
	}
	if _, err := NewFlateCodec(FlateOptions{Level: 10}); err == nil {
		t.Fatal("level 10 accepted")
	}
}

func TestDecompressNegativeMaxLen(t *testing.T) {
	for _, c := range testCodecs(t) {
		if _, err := c.Decompress(nil, nil, nil, -1); err == nil {
			t.Fatalf("%s: negative maxLen accepted", c.ID())
		}
	}
}

func TestNoneTooLarge(t *testing.T) {
	if _, err := (NoneCodec{}).Decompress(nil, []byte("abc"), nil, 2); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if got, err := (NoneCodec{}).Decompress(nil, []byte("abc"), nil, 3); err != nil || string(got) != "abc" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDecompressionBomb(t *testing.T) {
	c, err := NewFlateCodec(FlateOptions{Level: flate.BestSpeed})
	if err != nil {
		t.Fatal(err)
	}
	const bomb = 100 << 20
	comp, err := c.Compress(nil, make([]byte, bomb), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(comp) > 1<<20 {
		t.Fatalf("zeros compressed to %d bytes; test premise broken", len(comp))
	}

	const maxLen = 1 << 20
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	out, err := c.Decompress(nil, comp, nil, maxLen)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if out != nil {
		t.Fatalf("got %d bytes on error, want nil dst", len(out))
	}
	t.Logf("bomb alloc %d bytes for maxLen %d", after.TotalAlloc-before.TotalAlloc, maxLen)
	// Geometric growth capped at maxLen+1 plus pooled reader state; far below 100 MiB.
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 8*maxLen {
		t.Fatalf("allocated %d bytes decompressing a bomb with maxLen %d", alloc, maxLen)
	}

	// Exactly at the limit succeeds; the codec stays usable after the failure.
	small := make([]byte, maxLen)
	comp, _ = c.Compress(nil, small, nil)
	got, err := c.Decompress(nil, comp, nil, maxLen)
	if err != nil || len(got) != maxLen {
		t.Fatalf("at-limit: len %d err %v", len(got), err)
	}
}

func TestCorruptInput(t *testing.T) {
	c, _ := NewFlateCodec(DefaultFlateOptions())
	src := compressible(4096)
	good, _ := c.Compress(nil, src, nil)
	cases := map[string][]byte{
		"garbage":   bytes.Repeat([]byte{0xff}, 64),
		"truncated": good[:len(good)/2],
		"trailing":  append(append([]byte(nil), good...), 1, 2, 3),
		"empty":     {},
	}
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 50; i++ {
		flipped := append([]byte(nil), good...)
		flipped[rng.Intn(len(flipped))] ^= byte(1 + rng.Intn(255))
		cases["flip"+string(rune('A'+i%26))+string(rune('a'+i/26))] = flipped
	}
	for name, in := range cases {
		out, err := c.Decompress(nil, in, nil, 1<<20)
		if err == nil && (name == "garbage" || name == "truncated" || name == "trailing" || name == "empty") {
			t.Errorf("%s: no error (out %d bytes)", name, len(out))
		}
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: err %v does not wrap ErrCorrupt/ErrTooLarge", name, err)
		}
		if len(out) > 1<<20 {
			t.Errorf("%s: output %d exceeds maxLen", name, len(out))
		}
	}
}

// DEFLATE has no dictionary checksum, so a wrong dictionary is either an
// error or different output; it must never equal the original.
func TestWrongDict(t *testing.T) {
	c, _ := NewFlateCodec(DefaultFlateOptions())
	src := bytes.Repeat(testDict, 20)
	comp, _ := c.Compress(nil, src, testDict)
	wrong := bytes.Repeat([]byte("Z"), len(testDict))
	for name, dict := range map[string][]byte{"wrong": wrong, "none": nil} {
		got, err := c.Decompress(nil, comp, dict, len(src))
		if err == nil && bytes.Equal(got, src) {
			t.Errorf("%s dict: decoded to the original", name)
		}
		if err != nil && !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s dict: err %v does not wrap ErrCorrupt", name, err)
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	c, _ := NewFlateCodec(DefaultFlateOptions())
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				src := compressible(1000 + g*100 + i)
				var dict []byte
				if i%2 == 0 {
					dict = testDict
				}
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

func TestRegistry(t *testing.T) {
	r := DefaultRegistry()
	for _, id := range []ID{None, Flate, Zstd} {
		c, err := r.Get(id)
		if err != nil || c.ID() != id {
			t.Fatalf("Get(%v) = %v, %v", id, c, err)
		}
	}
	if _, err := r.Get(99); !errors.Is(err, ErrUnknownCodec) {
		t.Fatalf("Get(99) err = %v, want ErrUnknownCodec", err)
	}
	if err := r.Register(NoneCodec{}); !errors.Is(err, ErrDuplicateCodec) {
		t.Fatalf("duplicate Register err = %v, want ErrDuplicateCodec", err)
	}
	if err := r.Register(nil); err == nil {
		t.Fatal("nil codec accepted")
	}
	if _, err := NewRegistry().Get(None); !errors.Is(err, ErrUnknownCodec) {
		t.Fatalf("empty registry Get err = %v", err)
	}
	// Registries are independent: registering in one does not affect another.
	if DefaultRegistry() == r {
		t.Fatal("DefaultRegistry returned a shared value")
	}
}

func FuzzDecompress(f *testing.F) {
	c, _ := NewFlateCodec(DefaultFlateOptions())
	good, _ := c.Compress(nil, compressible(2048), nil)
	goodDict, _ := c.Compress(nil, compressible(2048), testDict)
	f.Add(good, false, 4096)
	f.Add(goodDict, true, 100)
	f.Add([]byte{}, false, 0)
	f.Add([]byte{0xff, 0xff}, true, 16)
	f.Fuzz(func(t *testing.T, in []byte, useDict bool, maxLen int) {
		maxLen %= 1 << 16
		if maxLen < 0 {
			maxLen = -maxLen
		}
		var dict []byte
		if useDict {
			dict = testDict
		}
		for _, cc := range []Codec{c, NoneCodec{}} {
			out, err := cc.Decompress(nil, in, dict, maxLen)
			if len(out) > maxLen {
				t.Fatalf("%s: output %d > maxLen %d", cc.ID(), len(out), maxLen)
			}
			if err != nil && out != nil {
				t.Fatalf("%s: non-nil output with error", cc.ID())
			}
		}
	})
}
