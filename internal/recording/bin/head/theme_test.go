package head

import (
	"bytes"
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/recording/bin/varint"
)

func sampleTheme() Theme {
	t := Theme{
		FG: Color{0xaa, 0xbb, 0xcc}, BG: Color{1, 2, 3},
		Cursor: Color{4, 5, 6}, HasCursor: true,
		SelectionFG: Color{7, 8, 9}, SelectionBG: Color{10, 11, 12},
		CursorStyle: 2,
	}
	for i := range t.Palette {
		t.Palette[i] = Color{uint8(i), uint8(255 - i), uint8(i * 3)}
	}
	return t
}

func TestThemeGolden(t *testing.T) {
	t.Parallel()
	b := MarshalTheme(sampleTheme())
	if len(b) != ThemeLen || ThemeLen != 785 {
		t.Fatalf("len = %d, ThemeLen = %d", len(b), ThemeLen)
	}
	head := mustHex(t, "aabbcc"+"010203"+"040506"+"01"+"070809"+"0a0b0c")
	if !bytes.Equal(b[:len(head)], head) {
		t.Fatalf("prefix = %x, want %x", b[:len(head)], head)
	}
	pal := b[len(head) : len(head)+768]
	if !bytes.Equal(pal[:6], []byte{0, 255, 0, 1, 254, 3}) || !bytes.Equal(pal[765:], []byte{255, 0, 253}) {
		t.Fatalf("palette ends = %x ... %x", pal[:6], pal[765:])
	}
	if b[len(b)-1] != 2 {
		t.Fatalf("cursor style byte = %d", b[len(b)-1])
	}
	got, err := UnmarshalTheme(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != sampleTheme() {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestThemeRoundTripRandom(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(3))
	for i := range 200 {
		var th Theme
		b := make([]byte, ThemeLen)
		rng.Read(b)
		th.FG, th.BG, th.Cursor = Color{b[0], b[1], b[2]}, Color{b[3], b[4], b[5]}, Color{b[6], b[7], b[8]}
		th.HasCursor = rng.Intn(2) == 0
		for j := range th.Palette {
			th.Palette[j] = Color{b[j], b[j+1], b[j+2]}
		}
		th.CursorStyle = b[9]
		got, err := UnmarshalTheme(MarshalTheme(th))
		if err != nil || got != th {
			t.Fatalf("case %d: got %+v, err %v", i, got, err)
		}
	}
}

func TestThemeErrors(t *testing.T) {
	t.Parallel()
	good := MarshalTheme(sampleTheme())
	for i := 0; i < len(good); i++ {
		if _, err := UnmarshalTheme(good[:i]); !errors.Is(err, ErrMalformed) {
			t.Fatalf("cut at %d: err = %v", i, err)
		}
	}
	if _, err := UnmarshalTheme(append(good[:len(good):len(good)], 0)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("trailing byte: err = %v", err)
	}
	bad := append([]byte(nil), good...)
	bad[9] = 2
	if _, err := UnmarshalTheme(bad); !errors.Is(err, ErrMalformed) {
		t.Fatalf("has-cursor 2: err = %v", err)
	}
}

func FuzzUnmarshalTheme(f *testing.F) {
	f.Add(MarshalTheme(sampleTheme()))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		th, err := UnmarshalTheme(b)
		if err == nil && !bytes.Equal(MarshalTheme(th), b) {
			t.Fatal("accepted theme does not re-encode identically")
		}
	})
}

func TestMetaGolden(t *testing.T) {
	t.Parallel()
	b, err := MarshalMeta(Meta{"b": "2", "a": "1", "": "empty", "zz": ""})
	if err != nil {
		t.Fatal(err)
	}
	want := mustHex(t, "04"+"0005"+"656d707479"+"01610131"+"01620132"+"027a7a00")
	if !bytes.Equal(b, want) {
		t.Fatalf("meta = %x, want %x", b, want)
	}
	got, err := UnmarshalMeta(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Meta{"b": "2", "a": "1", "": "empty", "zz": ""}) {
		t.Fatalf("round trip = %v", got)
	}
}

func TestMetaDeterministic(t *testing.T) {
	t.Parallel()
	m := Meta{}
	for _, k := range strings.Split("q w e r t y u i o p a s d f g h j k l", " ") {
		m[k] = k + k
	}
	first, _ := MarshalMeta(m)
	for range 50 {
		again, _ := MarshalMeta(m)
		if !bytes.Equal(first, again) {
			t.Fatal("MarshalMeta output varies between calls")
		}
	}
}

func TestMetaRoundTripRandom(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(4))
	for i := range 200 {
		m := Meta{}
		for range rng.Intn(20) {
			m[randString(rng, 20)] = randString(rng, 40)
		}
		b, err := MarshalMeta(m)
		if err != nil {
			t.Fatal(err)
		}
		got, err := UnmarshalMeta(b)
		if err != nil || !reflect.DeepEqual(got, m) {
			t.Fatalf("case %d: got %v, err %v, want %v", i, got, err, m)
		}
	}
}

func TestMetaEmpty(t *testing.T) {
	t.Parallel()
	b, err := MarshalMeta(nil)
	if err != nil || !bytes.Equal(b, []byte{0}) {
		t.Fatalf("empty meta = %x, %v", b, err)
	}
	m, err := UnmarshalMeta(b)
	if err != nil || len(m) != 0 {
		t.Fatalf("decode = %v, %v", m, err)
	}
}

func TestMetaTruncation(t *testing.T) {
	t.Parallel()
	b, _ := MarshalMeta(Meta{"alpha": "one", "beta": "two"})
	for i := 0; i < len(b); i++ {
		if _, err := UnmarshalMeta(b[:i]); !errors.Is(err, ErrMalformed) {
			t.Fatalf("cut at %d: err = %v", i, err)
		}
	}
}

func TestMetaErrors(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", MaxStringLen+1)
	str := func(s string) []byte { return appendString(nil, s) }
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

	manyPairs := Meta{}
	for i := range MaxListLen + 1 {
		manyPairs[string(varint.AppendUvarint(nil, uint64(i)))] = ""
	}
	for name, tc := range map[string]struct {
		m    Meta
		want error
	}{
		"too many pairs": {manyPairs, ErrLimit},
		"key too long":   {Meta{big: ""}, ErrLimit},
		"value too long": {Meta{"k": big}, ErrLimit},
	} {
		if _, err := MarshalMeta(tc.m); !errors.Is(err, tc.want) {
			t.Errorf("marshal %s: err = %v", name, err)
		}
	}
	for name, tc := range map[string]struct {
		in   []byte
		want error
	}{
		"count too large":  {varint.AppendUvarint(nil, MaxListLen+1), ErrLimit},
		"count past data":  {[]byte{3, 0, 0}, ErrMalformed},
		"key too long":     {cat([]byte{1}, varint.AppendUvarint(nil, MaxStringLen+1), []byte(big)), ErrLimit},
		"value too long":   {cat([]byte{1}, str("k"), varint.AppendUvarint(nil, MaxStringLen+1), []byte(big)), ErrLimit},
		"duplicate key":    {cat([]byte{2}, str("k"), str("1"), str("k"), str("2")), ErrMalformed},
		"trailing bytes":   {cat([]byte{1}, str("k"), str("v"), []byte{0}), ErrMalformed},
		"non-canonical n":  {[]byte{0x80, 0x00}, ErrMalformed},
		"empty":            {nil, ErrMalformed},
		"string past data": {cat([]byte{1}, []byte{5, 'a'}), ErrMalformed},
	} {
		if _, err := UnmarshalMeta(tc.in); !errors.Is(err, tc.want) {
			t.Errorf("unmarshal %s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func FuzzUnmarshalMeta(f *testing.F) {
	seed, _ := MarshalMeta(Meta{"a": "1", "bb": "22"})
	f.Add(seed)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := UnmarshalMeta(b)
		if err != nil {
			return
		}
		out, err := MarshalMeta(m)
		if err != nil {
			t.Fatalf("accepted meta does not re-marshal: %v", err)
		}
		m2, err := UnmarshalMeta(out)
		if err != nil || !reflect.DeepEqual(m, m2) {
			t.Fatalf("re-decode = %v, %v; want %v", m2, err, m)
		}
	})
}
