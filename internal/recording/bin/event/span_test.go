package event

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func hexBytes(s string) []byte {
	var out []byte
	for _, f := range strings.Fields(s) {
		var b byte
		for _, c := range f {
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= byte(c - '0')
			case c >= 'A' && c <= 'F':
				b |= byte(c-'A') + 10
			default:
				panic("bad hex " + f)
			}
		}
		out = append(out, b)
	}
	return out
}

// The bytes below are the hand-computed examples of docs/research/script-scenes.md §4.3.
func TestSpanDocBytes(t *testing.T) {
	install := Span{
		Op: SpanBegin, ID: 1, Type: SpanScene, Name: "install",
		Hints: SpanHints{Speed: 8000, Idle: 2 * time.Second, Hold: time.Second},
	}
	cases := []struct {
		name string
		span Span
		hex  string
	}{
		{"scene with hints", install, `
			01 01 01 01 00 07 69 6E 73 74 61 6C 6C 03
			01 02 C0 3E
			02 03 80 89 7A
			03 03 C0 84 3D`},
		{"end", Span{Op: SpanEnd, ID: 1}, "01 02 01"},
		{"hidden in scene", Span{Op: SpanBegin, ID: 2, Type: SpanHidden, Parent: 1}, "01 01 02 02 01 00 00"},
		{"end hidden", Span{Op: SpanEnd, ID: 2}, "01 02 02"},
	}
	for _, c := range cases {
		want := hexBytes(c.hex)
		got := AppendSpan(nil, c.span)
		if !bytes.Equal(got, want) {
			t.Errorf("%s: AppendSpan = % X, want % X", c.name, got, want)
		}
		back, err := ParseSpan(want)
		if err != nil {
			t.Errorf("%s: ParseSpan: %v", c.name, err)
		} else if !reflect.DeepEqual(back, c.span) {
			t.Errorf("%s: ParseSpan = %+v, want %+v", c.name, back, c.span)
		}
	}
	if n := len(AppendSpan(nil, install)); n != 28 {
		t.Errorf("scene payload is %d bytes, doc says 28", n)
	}
}

func TestSpanRoundTrip(t *testing.T) {
	spans := []Span{
		{Op: SpanBegin, ID: 1, Type: SpanScene, Name: "héllo ✓"},
		{Op: SpanBegin, ID: 300, Type: SpanBeat, Parent: 2, Name: strings.Repeat("n", MaxSpanName)},
		{Op: SpanBegin, ID: 3, Type: SpanHidden, Hints: SpanHints{
			Speed: MaxSpanSpeedMilli, Idle: MaxSpanDuration, Hold: 1500 * time.Microsecond, Caption: "cap"}},
		{Op: SpanEnd, ID: 1 << 40},
	}
	for _, s := range spans {
		if err := s.Validate(); err != nil {
			t.Fatalf("%+v: Validate: %v", s, err)
		}
		got, err := ParseSpan(AppendSpan(nil, s))
		if err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
		if !reflect.DeepEqual(got, s) {
			t.Errorf("round trip = %+v, want %+v", got, s)
		}
	}
	e := NewSpan(7, spans[0])
	if e.Kind != KindSpan || e.Tick != 7 || !e.Kind.IsAncillary() || e.Kind.String() != "span" {
		t.Errorf("NewSpan = %+v", e)
	}
}

func TestSpanForwardCompat(t *testing.T) {
	// Unknown trailing bytes, an unknown hint tag, and trailing bytes after END.
	begin := hexBytes("01 01 01 01 00 02 68 69 02  09 03 AA BB CC  01 02 C0 3E  FF FF")
	s, err := ParseSpan(begin)
	if err != nil {
		t.Fatal(err)
	}
	want := Span{Op: SpanBegin, ID: 1, Type: SpanScene, Name: "hi", Hints: SpanHints{Speed: 8000}}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("got %+v, want %+v", s, want)
	}
	s, err = ParseSpan(hexBytes("01 02 05 99 99"))
	if err != nil || s != (Span{Op: SpanEnd, ID: 5}) {
		t.Errorf("end with trailing = %+v, %v", s, err)
	}
}

func TestSpanUnknownIsNotMalformed(t *testing.T) {
	for name, hex := range map[string]string{
		"version 2": "02 01 01 01 00 00 00",
		"op 3":      "01 03 01",
		"type 4":    "01 01 01 04 00 00 00",
	} {
		_, err := ParseSpan(hexBytes(hex))
		if !errors.Is(err, ErrUnknownSpan) || errors.Is(err, ErrMalformedSpan) {
			t.Errorf("%s: err = %v, want ErrUnknownSpan only", name, err)
		}
	}
}

func TestSpanMalformed(t *testing.T) {
	long := strings.Repeat("a", MaxSpanName+1)
	cases := map[string][]byte{
		"empty":             nil,
		"version only":      hexBytes("01"),
		"no id":             hexBytes("01 02"),
		"id zero":           hexBytes("01 02 00"),
		"begin id zero":     hexBytes("01 01 00 01 00 00 00"),
		"type zero":         hexBytes("01 01 01 00 00 00 00"),
		"no type":           hexBytes("01 01 01"),
		"no hints count":    hexBytes("01 01 01 01 00 00"),
		"name too long":     append(hexBytes("01 01 01 01 00 81 02"), []byte(long+"\x00")...),
		"name truncated":    hexBytes("01 01 01 01 00 05 61 62"),
		"name invalid utf8": hexBytes("01 01 01 01 00 01 FF 00"),
		"too many hints":    hexBytes("01 01 01 01 00 00 41"),
		"hint truncated":    hexBytes("01 01 01 01 00 00 01 01"),
		"hint len past end": hexBytes("01 01 01 01 00 00 01 01 05 01"),
		"speed zero":        hexBytes("01 01 01 01 00 00 01 01 01 00"),
		"speed too big":     hexBytes("01 01 01 01 00 00 01 01 04 C1 84 3E"),
		"idle too big":      hexBytes("01 01 01 01 00 00 01 02 08 FF FF FF FF FF FF FF 01"),
		"hint trailing":     hexBytes("01 01 01 01 00 00 01 01 02 01 00"),
		"hint empty":        hexBytes("01 01 01 01 00 00 01 01 00"),
		"hint noncanonical": hexBytes("01 01 01 01 00 00 01 03 02 81 00"),
		"duplicate speed":   hexBytes("01 01 01 01 00 00 02 01 01 01 01 01 02"),
		"caption bad utf8":  hexBytes("01 01 01 01 00 00 01 04 01 FF"),
		"overlong varint":   hexBytes("01 02 80 80 80 80 80 80 80 80 80 80 80"),
	}
	for name, p := range cases {
		_, err := ParseSpan(p)
		if !errors.Is(err, ErrMalformedSpan) {
			t.Errorf("%s: err = %v, want ErrMalformedSpan", name, err)
		}
	}
}

func TestSpanValidate(t *testing.T) {
	good := Span{Op: SpanBegin, ID: 1, Type: SpanScene, Name: "x"}
	bads := map[string]Span{
		"id 0":      {Op: SpanEnd},
		"op 0":      {ID: 1},
		"type 0":    {Op: SpanBegin, ID: 1},
		"type 9":    {Op: SpanBegin, ID: 1, Type: 9},
		"name long": {Op: SpanBegin, ID: 1, Type: SpanScene, Name: strings.Repeat("a", MaxSpanName+1)},
		"name utf8": {Op: SpanBegin, ID: 1, Type: SpanScene, Name: "\xff"},
		"speed":     {Op: SpanBegin, ID: 1, Type: SpanScene, Hints: SpanHints{Speed: MaxSpanSpeedMilli + 1}},
		"idle neg":  {Op: SpanBegin, ID: 1, Type: SpanScene, Hints: SpanHints{Idle: -1}},
		"hold big":  {Op: SpanBegin, ID: 1, Type: SpanScene, Hints: SpanHints{Hold: MaxSpanDuration + 1}},
		"caption":   {Op: SpanBegin, ID: 1, Type: SpanScene, Hints: SpanHints{Caption: "\xff"}},
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, s := range bads {
		if err := s.Validate(); !errors.Is(err, ErrMalformedSpan) {
			t.Errorf("%s: err = %v, want ErrMalformedSpan", name, err)
		}
	}
}

func FuzzParseSpan(f *testing.F) {
	f.Add(hexBytes("01 01 01 01 00 07 69 6E 73 74 61 6C 6C 03 01 02 C0 3E 02 03 80 89 7A 03 03 C0 84 3D"))
	f.Add(hexBytes("01 02 01"))
	f.Add(hexBytes("01 01 02 02 01 00 00"))
	f.Add(hexBytes("01 01 01 01 00 00 01 04 01 61"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, p []byte) {
		s, err := ParseSpan(p)
		if err != nil {
			if !errors.Is(err, ErrMalformedSpan) && !errors.Is(err, ErrUnknownSpan) {
				t.Fatalf("error %v wraps neither sentinel", err)
			}
			return
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("parsed span fails Validate: %v (%+v)", err, s)
		}
		back, err := ParseSpan(AppendSpan(nil, s))
		if err != nil || !reflect.DeepEqual(back, s) {
			t.Fatalf("re-encode = %+v, %v; want %+v", back, err, s)
		}
	})
}
