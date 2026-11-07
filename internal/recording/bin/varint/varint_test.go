package varint

import (
	"bytes"
	"errors"
	"math"
	"math/rand"
	"testing"
)

var uvarintGoldens = []struct {
	v   uint64
	enc []byte
}{
	{0, []byte{0x00}},
	{1, []byte{0x01}},
	{127, []byte{0x7f}},
	{128, []byte{0x80, 0x01}},
	{300, []byte{0xac, 0x02}},
	{16383, []byte{0xff, 0x7f}},
	{16384, []byte{0x80, 0x80, 0x01}},
	{1 << 32, []byte{0x80, 0x80, 0x80, 0x80, 0x10}},
	{1 << 63, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}},
	{math.MaxUint64, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}},
}

var svarintGoldens = []struct {
	v   int64
	enc []byte
}{
	{0, []byte{0x00}},
	{-1, []byte{0x01}},
	{1, []byte{0x02}},
	{-2, []byte{0x03}},
	{63, []byte{0x7e}},
	{-64, []byte{0x7f}},
	{64, []byte{0x80, 0x01}},
	{math.MaxInt64, []byte{0xfe, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}},
	{math.MinInt64, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}},
}

func TestUvarintGoldens(t *testing.T) {
	t.Parallel()
	for _, tc := range uvarintGoldens {
		if got := AppendUvarint(nil, tc.v); !bytes.Equal(got, tc.enc) {
			t.Errorf("AppendUvarint(%d) = %x, want %x", tc.v, got, tc.enc)
		}
		v, n, err := Uvarint(tc.enc)
		if err != nil || v != tc.v || n != len(tc.enc) {
			t.Errorf("Uvarint(%x) = %d,%d,%v want %d,%d,nil", tc.enc, v, n, err, tc.v, len(tc.enc))
		}
		// Trailing bytes are not consumed.
		v, n, err = Uvarint(append(append([]byte{}, tc.enc...), 0x55))
		if err != nil || v != tc.v || n != len(tc.enc) {
			t.Errorf("Uvarint(%x+trailer) = %d,%d,%v", tc.enc, v, n, err)
		}
	}
}

func TestSvarintGoldens(t *testing.T) {
	t.Parallel()
	for _, tc := range svarintGoldens {
		if got := AppendSvarint(nil, tc.v); !bytes.Equal(got, tc.enc) {
			t.Errorf("AppendSvarint(%d) = %x, want %x", tc.v, got, tc.enc)
		}
		v, n, err := Svarint(tc.enc)
		if err != nil || v != tc.v || n != len(tc.enc) {
			t.Errorf("Svarint(%x) = %d,%d,%v want %d,%d,nil", tc.enc, v, n, err, tc.v, len(tc.enc))
		}
	}
}

func TestAppendPreservesPrefix(t *testing.T) {
	t.Parallel()
	got := AppendSvarint(AppendUvarint([]byte{0xaa}, 300), -1)
	want := []byte{0xaa, 0xac, 0x02, 0x01}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x want %x", got, want)
	}
}

func TestDecodeErrors(t *testing.T) {
	t.Parallel()
	ten80 := bytes.Repeat([]byte{0x80}, 10)
	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"nil", nil, ErrTruncated},
		{"empty", []byte{}, ErrTruncated},
		{"one continuation", []byte{0x80}, ErrTruncated},
		{"nine continuations", bytes.Repeat([]byte{0xff}, 9), ErrTruncated},
		{"overlong zero", []byte{0x80, 0x00}, ErrNonCanonical},
		{"overlong one", []byte{0x81, 0x00}, ErrNonCanonical},
		{"overlong three bytes", []byte{0xff, 0x80, 0x00}, ErrNonCanonical},
		{"overlong ten bytes", append(bytes.Repeat([]byte{0x80}, 9), 0x00), ErrNonCanonical},
		{"tenth byte 2", append(bytes.Repeat([]byte{0xff}, 9), 0x02), ErrOverflow},
		{"tenth byte 0x7f", append(bytes.Repeat([]byte{0xff}, 9), 0x7f), ErrOverflow},
		{"ten continuations", ten80, ErrOverflow},
		{"eleven bytes", append(ten80, 0x00), ErrOverflow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, n, err := Uvarint(tc.in)
			if !errors.Is(err, tc.want) || v != 0 || n != 0 {
				t.Errorf("Uvarint = %d,%d,%v want 0,0,%v", v, n, err, tc.want)
			}
			sv, sn, serr := Svarint(tc.in)
			if !errors.Is(serr, tc.want) || sv != 0 || sn != 0 {
				t.Errorf("Svarint = %d,%d,%v want 0,0,%v", sv, sn, serr, tc.want)
			}
		})
	}
}

func TestRoundTripRandom(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		u := rng.Uint64() >> uint(rng.Intn(64))
		enc := AppendUvarint(nil, u)
		got, n, err := Uvarint(enc)
		if err != nil || got != u || n != len(enc) || n > MaxLen {
			t.Fatalf("uvarint %d: got %d,%d,%v enc=%x", u, got, n, err, enc)
		}
		s := int64(rng.Uint64()) >> uint(rng.Intn(64))
		enc = AppendSvarint(nil, s)
		sg, n, err := Svarint(enc)
		if err != nil || sg != s || n != len(enc) {
			t.Fatalf("svarint %d: got %d,%d,%v enc=%x", s, sg, n, err, enc)
		}
	}
}

func TestDecodeDoesNotAllocate(t *testing.T) {
	enc := AppendUvarint(nil, math.MaxUint64)
	allocs := testing.AllocsPerRun(100, func() {
		_, _, _ = Uvarint(enc)
		_, _, _ = Svarint(enc)
		_, _, _ = Uvarint(enc[:3])
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestReaderSequence(t *testing.T) {
	t.Parallel()
	var b []byte
	b = AppendUvarint(b, 1000)
	b = AppendSvarint(b, -5)
	b = append(b, 0x42)
	b = AppendUvarint(b, 3)
	b = append(b, 'a', 'b', 'c')

	r := NewReader(b)
	if r.Remaining() != len(b) {
		t.Fatalf("Remaining = %d", r.Remaining())
	}
	if v, err := r.Uvarint(); err != nil || v != 1000 {
		t.Fatalf("Uvarint = %d,%v", v, err)
	}
	if v, err := r.Svarint(); err != nil || v != -5 {
		t.Fatalf("Svarint = %d,%v", v, err)
	}
	if c, err := r.Byte(); err != nil || c != 0x42 {
		t.Fatalf("Byte = %x,%v", c, err)
	}
	n, err := r.UvarintMax(3)
	if err != nil || n != 3 {
		t.Fatalf("UvarintMax = %d,%v", n, err)
	}
	s, err := r.Bytes(int(n))
	if err != nil || string(s) != "abc" {
		t.Fatalf("Bytes = %q,%v", s, err)
	}
	if r.Remaining() != 0 {
		t.Fatalf("Remaining = %d, want 0", r.Remaining())
	}
	if _, err := r.Byte(); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Byte at end err = %v", err)
	}
}

func TestReaderErrorsLeaveCursor(t *testing.T) {
	t.Parallel()
	r := NewReader([]byte{0x80, 0x00, 0x05})
	if _, err := r.Uvarint(); !errors.Is(err, ErrNonCanonical) {
		t.Fatalf("Uvarint err = %v", err)
	}
	if _, err := r.Svarint(); !errors.Is(err, ErrNonCanonical) {
		t.Fatalf("Svarint err = %v", err)
	}
	if _, err := r.UvarintMax(10); !errors.Is(err, ErrNonCanonical) {
		t.Fatalf("UvarintMax err = %v", err)
	}
	if r.Remaining() != 3 {
		t.Fatalf("cursor moved: Remaining = %d", r.Remaining())
	}

	r = NewReader([]byte{0x80})
	if _, err := r.Uvarint(); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Uvarint err = %v", err)
	}
	if _, err := r.Svarint(); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Svarint err = %v", err)
	}
	if _, err := r.UvarintMax(1); !errors.Is(err, ErrTruncated) {
		t.Fatalf("UvarintMax err = %v", err)
	}
}

func TestReaderUvarintMax(t *testing.T) {
	t.Parallel()
	r := NewReader(AppendUvarint(nil, 100))
	if _, err := r.UvarintMax(99); !errors.Is(err, ErrLimit) {
		t.Fatalf("err = %v, want ErrLimit", err)
	}
	if r.Remaining() != 1 {
		t.Fatalf("cursor moved on ErrLimit")
	}
	if v, err := r.UvarintMax(100); err != nil || v != 100 {
		t.Fatalf("at-limit = %d,%v", v, err)
	}

	r = NewReader(AppendUvarint(nil, math.MaxUint64))
	if v, err := r.UvarintMax(math.MaxUint64); err != nil || v != math.MaxUint64 {
		t.Fatalf("max max = %d,%v", v, err)
	}
}

func TestReaderBytesBounds(t *testing.T) {
	t.Parallel()
	r := NewReader([]byte{1, 2, 3})
	for _, n := range []int{4, math.MaxInt, -1, math.MinInt} {
		if s, err := r.Bytes(n); !errors.Is(err, ErrTruncated) || s != nil {
			t.Errorf("Bytes(%d) = %v,%v want nil,ErrTruncated", n, s, err)
		}
	}
	if r.Remaining() != 3 {
		t.Fatalf("cursor moved on error")
	}
	if s, err := r.Bytes(0); err != nil || len(s) != 0 {
		t.Fatalf("Bytes(0) = %v,%v", s, err)
	}
	s, err := r.Bytes(2)
	if err != nil || !bytes.Equal(s, []byte{1, 2}) {
		t.Fatalf("Bytes(2) = %v,%v", s, err)
	}
	if cap(s) != 2 {
		t.Errorf("cap = %d; returned slice must not expose later bytes", cap(s))
	}
	if s, err := r.Bytes(1); err != nil || !bytes.Equal(s, []byte{3}) {
		t.Fatalf("Bytes(1) = %v,%v", s, err)
	}
	if _, err := r.Bytes(1); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Bytes past end err = %v", err)
	}
}

func TestZeroReader(t *testing.T) {
	t.Parallel()
	var r Reader
	if r.Remaining() != 0 {
		t.Fatal("zero Reader not empty")
	}
	if _, err := r.Uvarint(); !errors.Is(err, ErrTruncated) {
		t.Fatalf("err = %v", err)
	}
}

func FuzzUvarint(f *testing.F) {
	for _, tc := range uvarintGoldens {
		f.Add(tc.enc)
	}
	f.Add([]byte{0x80, 0x00})
	f.Add(bytes.Repeat([]byte{0xff}, 11))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, n, err := Uvarint(b)
		if err != nil {
			if v != 0 || n != 0 {
				t.Fatalf("error with v=%d n=%d", v, n)
			}
		} else {
			if n < 1 || n > MaxLen || n > len(b) {
				t.Fatalf("n=%d out of range, len=%d", n, len(b))
			}
			if re := AppendUvarint(nil, v); !bytes.Equal(re, b[:n]) {
				t.Fatalf("not canonical: decoded %d from %x, re-encodes to %x", v, b[:n], re)
			}
		}
		sv, sn, serr := Svarint(b)
		if (serr == nil) != (err == nil) || sn != n {
			t.Fatalf("Svarint disagrees with Uvarint: %v,%d vs %v,%d", serr, sn, err, n)
		}
		if serr == nil {
			if re := AppendSvarint(nil, sv); !bytes.Equal(re, b[:sn]) {
				t.Fatalf("svarint not canonical: %x vs %x", b[:sn], re)
			}
		}
	})
}

// FuzzReader drives a Reader with an op script taken from the first bytes of
// the input over the rest, checking bounds and that decoded varints are
// canonical for the bytes consumed.
func FuzzReader(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 0x05, 0x01, 0x80, 0x00, 'a', 'b'})
	f.Add([]byte{4, 4, 4, 0x7f, 0xff, 0xff})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		split := int(data[0]) % len(data)
		ops, body := data[1:1+min(split, len(data)-1)], data[1+min(split, len(data)-1):]
		r := NewReader(body)
		for _, op := range ops {
			before := r.Remaining()
			consumed := body[len(body)-before:]
			var err error
			switch op % 5 {
			case 0:
				var v uint64
				if v, err = r.Uvarint(); err == nil {
					checkCanon(t, consumed, before-r.Remaining(), AppendUvarint(nil, v))
				}
			case 1:
				var v int64
				if v, err = r.Svarint(); err == nil {
					checkCanon(t, consumed, before-r.Remaining(), AppendSvarint(nil, v))
				}
			case 2:
				_, err = r.Byte()
				if err == nil && before-r.Remaining() != 1 {
					t.Fatal("Byte consumed != 1")
				}
			case 3:
				n := int(op) * 37
				var s []byte
				if s, err = r.Bytes(n); err == nil && (len(s) != n || before-r.Remaining() != n) {
					t.Fatalf("Bytes(%d) returned %d", n, len(s))
				}
				if err != nil && n <= before {
					t.Fatalf("Bytes(%d) failed with %d remaining", n, before)
				}
			case 4:
				max := uint64(op)
				var v uint64
				if v, err = r.UvarintMax(max); err == nil {
					if v > max {
						t.Fatalf("UvarintMax returned %d > %d", v, max)
					}
					checkCanon(t, consumed, before-r.Remaining(), AppendUvarint(nil, v))
				}
			}
			if err != nil && r.Remaining() != before {
				t.Fatalf("cursor moved on error %v", err)
			}
			if r.Remaining() < 0 || r.Remaining() > before {
				t.Fatalf("Remaining %d out of range (before %d)", r.Remaining(), before)
			}
		}
	})
}

func checkCanon(t *testing.T, consumed []byte, n int, re []byte) {
	t.Helper()
	if n != len(re) || !bytes.Equal(consumed[:n], re) {
		t.Fatalf("consumed %x but value re-encodes to %x", consumed[:n], re)
	}
}
