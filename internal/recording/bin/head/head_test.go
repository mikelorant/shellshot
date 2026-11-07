package head

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/michaellorant/shellshot/internal/recording/bin/varint"
)

func fullHead() Head {
	return Head{
		FormatMinor:      1,
		RequiredFeatures: FeatureBlockDictionaries,
		Cols:             120,
		Rows:             40,
		TickNS:           50000,
		StartedUnixNS:    1700000000123456789,
		TZOffsetS:        -18000,
		CellPxW:          9,
		CellPxH:          18,
		Engine:           Engine{Name: "libghostty-vt", Commit: "0123456789abcdef0123456789abcdef01234567", BuildFlags: "ReleaseFast"},
		EngineConfig:     EngineConfig{ScrollbackLimit: 10000, KittyStorageLimit: 1 << 20},
		Term:             "xterm-256color",
		Argv:             []string{"ls", "-la", ""},
		Cwd:              "/tmp",
		Env:              []string{"TERM=xterm", "LANG=C"},
		Title:            "t",
		Font:             FontHint{Family: "Mono", SizePt: 12.5, CellAspect: 0.5},
		Flags:            FlagInputRecorded | FlagCoalesced,
		Hostname:         "h",
		SessionID:        [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Recorder:         "shellshot/1",
		Unknown:          []RawField{{Tag: 99, Value: []byte{0xde, 0xad}}},
	}
}

const goldenFullHex = "" +
	"01020101" + // tag 1: format_minor, required_features
	"02027828" + // tag 2: 120x40
	"0303d08603" + // tag 3: tick_ns 50000
	"040caab4aed8c7bfce972f9f9902" + // tag 4: started_unix_ns, tz_offset_s -18000
	"05020912" + // tag 5: cell 9x18
	"06430d6c696267686f737474792d767428303132333435363738396162636465663031323334353637383961626364656630313233343536370b52656c6561736546617374" + // tag 6: engine
	"0705904e808040" + // tag 7: engine config
	"080e787465726d2d323536636f6c6f72" + // tag 8: TERM
	"09026c73" + // tag 9: argv
	"09032d6c61" + // tag 9: argv
	"0900" + // tag 9: argv
	"0a042f746d70" + // tag 10: cwd
	"0b0a5445524d3d787465726d" + // tag 11: env
	"0b064c414e473d43" + // tag 11: env
	"0c0174" + // tag 12: title
	"0d0d044d6f6e6f000048410000003f" + // tag 13: font
	"0e0105" + // tag 14: flags
	"0f0168" + // tag 15: hostname
	"10100102030405060708090a0b0c0d0e0f10" + // tag 16: session id
	"110b7368656c6c73686f742f31" + // tag 17: recorder
	"6302dead" + // tag 99: unknown
	""
const goldenMinimal = "02025018" + "0303a08d06"

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMarshalGoldenMinimal(t *testing.T) {
	got, err := Head{Cols: 80, Rows: 24}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	want := mustHex(t, goldenMinimal)
	if !bytes.Equal(got, want) {
		t.Fatalf("minimal head = %x, want %x", got, want)
	}
	h, err := Unmarshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if h.Cols != 80 || h.Rows != 24 || h.TickNS != DefaultTickNS {
		t.Fatalf("decoded = %+v", h)
	}
}

func TestMarshalGoldenFull(t *testing.T) {
	got, err := fullHead().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	want := mustHex(t, goldenFullHex)
	if !bytes.Equal(got, want) {
		t.Fatalf("full head =\n%x\nwant\n%x", got, want)
	}
	h, err := Unmarshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h, fullHead()) {
		t.Fatalf("decoded = %+v\nwant      %+v", h, fullHead())
	}
}

func TestMarshalDefaultsTick(t *testing.T) {
	a, _ := Head{Cols: 1, Rows: 1}.Marshal()
	b, _ := Head{Cols: 1, Rows: 1, TickNS: DefaultTickNS}.Marshal()
	if !bytes.Equal(a, b) {
		t.Fatalf("zero tick %x != default tick %x", a, b)
	}
}

func TestUnmarshalMissingTickUsesDefault(t *testing.T) {
	h, err := Unmarshal(mustHex(t, "02025018"))
	if err != nil {
		t.Fatal(err)
	}
	if h.TickNS != DefaultTickNS {
		t.Fatalf("TickNS = %d", h.TickNS)
	}
}

func randString(rng *rand.Rand, max int) string {
	b := make([]byte, rng.Intn(max+1))
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	return string(b)
}

func randHead(rng *rand.Rand) Head {
	h := Head{Cols: 1 + rng.Intn(MaxDimension), Rows: 1 + rng.Intn(MaxDimension)}
	h.TickNS = 1 + rng.Uint64()>>uint(rng.Intn(64))
	if rng.Intn(2) == 0 {
		h.FormatMinor = rng.Uint64() >> uint(rng.Intn(64))
		h.RequiredFeatures = uint64(rng.Intn(2))
	}
	if rng.Intn(2) == 0 {
		h.StartedUnixNS = int64(rng.Uint64())
		h.TZOffsetS = int64(rng.Intn(100000) - 50000)
	}
	if rng.Intn(2) == 0 {
		h.CellPxW, h.CellPxH = rng.Intn(MaxDimension+1), rng.Intn(MaxDimension+1)
	}
	if rng.Intn(2) == 0 {
		h.Engine = Engine{randString(rng, 50), randString(rng, 50), randString(rng, 50)}
		h.EngineConfig = EngineConfig{rng.Uint64(), rng.Uint64()}
	}
	h.Term, h.Cwd, h.Title = randString(rng, 20), randString(rng, 100), randString(rng, 30)
	h.Hostname, h.Recorder = randString(rng, 20), randString(rng, 20)
	for range rng.Intn(6) {
		h.Argv = append(h.Argv, randString(rng, 30))
	}
	for range rng.Intn(6) {
		h.Env = append(h.Env, randString(rng, 30))
	}
	if rng.Intn(2) == 0 {
		h.Font = FontHint{randString(rng, 20), math.Float32frombits(rng.Uint32()), math.Float32frombits(rng.Uint32())}
		if math.IsNaN(float64(h.Font.SizePt)) || math.IsNaN(float64(h.Font.CellAspect)) {
			h.Font.SizePt, h.Font.CellAspect = 1, 2 // DeepEqual needs comparable floats
		}
	}
	h.Flags = rng.Uint64() >> uint(rng.Intn(64))
	if rng.Intn(2) == 0 {
		rng.Read(h.SessionID[:])
	}
	for range rng.Intn(4) {
		v := make([]byte, rng.Intn(20))
		rng.Read(v)
		h.Unknown = append(h.Unknown, RawField{Tag: uint64(maxKnownTag + 1 + rng.Intn(1000)), Value: v})
	}
	return h
}

func TestRoundTripRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := range 500 {
		h := randHead(rng)
		b, err := h.Marshal()
		if err != nil {
			t.Fatalf("case %d: Marshal: %v", i, err)
		}
		got, err := Unmarshal(b)
		if err != nil {
			t.Fatalf("case %d: Unmarshal: %v", i, err)
		}
		// Unknown fields come back in tag order; normalise the input the same way.
		want := h
		want.Unknown = sortedUnknown(h.Unknown)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d: got %+v\nwant %+v", i, got, want)
		}
		again, _ := got.Marshal()
		if !bytes.Equal(again, b) {
			t.Fatalf("case %d: re-marshal differs", i)
		}
	}
}

func sortedUnknown(u []RawField) []RawField {
	out := append([]RawField(nil), u...)
	for i := 1; i < len(out); i++ { // stable insertion sort by tag
		for j := i; j > 0 && out[j].Tag < out[j-1].Tag; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestUnknownTagPreservedAndIgnored(t *testing.T) {
	base, _ := Head{Cols: 80, Rows: 24}.Marshal()
	in := append([]byte(nil), base...)
	in = append(in, 0x7f, 3, 'a', 'b', 'c') // tag 127, unknown
	in = append(in, 0x7f, 0)                // repeated unknown tag, empty value
	h, err := Unmarshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if h.Cols != 80 || h.Rows != 24 {
		t.Fatalf("known fields disturbed: %+v", h)
	}
	want := []RawField{{127, []byte("abc")}, {127, []byte{}}}
	if !reflect.DeepEqual(h.Unknown, want) {
		t.Fatalf("Unknown = %v, want %v", h.Unknown, want)
	}
	in[len(in)-3] = 'X' // Unknown must not alias the input
	if string(h.Unknown[0].Value) != "abc" {
		t.Fatal("Unknown aliases input")
	}
}

func TestMarshalUnknownSortedIntoPlace(t *testing.T) {
	b, err := Head{Cols: 1, Rows: 1, Title: "x", Unknown: []RawField{{Tag: 40, Value: []byte{1}}, {Tag: 13 + 100, Value: nil}}}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var tags []uint64
	r := varint.NewReader(b)
	for r.Remaining() > 0 {
		tag, _ := r.Uvarint()
		n, _ := r.Uvarint()
		r.Bytes(int(n))
		tags = append(tags, tag)
	}
	want := []uint64{2, 3, 12, 40, 113}
	if !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
}

func TestRequiredFeatures(t *testing.T) {
	for _, tc := range []struct {
		features uint64
		ok       bool
	}{{0, true}, {1, true}, {2, false}, {3, false}, {1 << 63, false}} {
		features := varint.AppendUvarint([]byte{0}, tc.features) // minor 0
		in := append([]byte{tagVersion, byte(len(features))}, features...)
		in = append(in, mustHex(t, goldenMinimal)...)
		_, err := Unmarshal(in)
		switch {
		case tc.ok && err != nil:
			t.Errorf("features %#x: %v", tc.features, err)
		case !tc.ok && !errors.Is(err, ErrUnsupported):
			t.Errorf("features %#x: err = %v, want ErrUnsupported", tc.features, err)
		}
	}
}

func TestUnmarshalErrors(t *testing.T) {
	size := mustHex(t, "02025018")
	field := func(tag uint64, v []byte) []byte {
		return append(varint.AppendUvarint(varint.AppendUvarint(nil, tag), uint64(len(v))), v...)
	}
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	big := bytes.Repeat([]byte("a"), MaxStringLen+1)
	ok := bytes.Repeat([]byte("a"), MaxStringLen)
	var manyArgv, manyEnv, manyUnknown, okArgv []byte
	for range MaxListLen + 1 {
		manyArgv = append(manyArgv, field(tagArgv, nil)...)
		manyEnv = append(manyEnv, field(tagEnv, nil)...)
		manyUnknown = append(manyUnknown, field(100, nil)...)
	}
	okArgv = manyArgv[:MaxListLen*2]

	tests := []struct {
		name string
		in   []byte
		want error // nil means must succeed
	}{
		{"empty", nil, ErrMalformed},
		{"missing size", field(tagTick, []byte{1}), ErrMalformed},
		{"size repeated", cat(size, size), ErrMalformed},
		{"tick repeated", cat(size, field(tagTick, []byte{1}), field(tagTick, []byte{1})), ErrMalformed},
		{"cols zero", field(tagSize, []byte{0, 1}), ErrLimit},
		{"rows zero", field(tagSize, []byte{1, 0}), ErrLimit},
		{"cols too big", field(tagSize, cat(varint.AppendUvarint(nil, 65536), []byte{1})), ErrLimit},
		{"rows too big", field(tagSize, cat([]byte{1}, varint.AppendUvarint(nil, 65536))), ErrLimit},
		{"max size", field(tagSize, cat(varint.AppendUvarint(nil, 65535), varint.AppendUvarint(nil, 65535))), nil},
		{"size trailing", field(tagSize, []byte{1, 1, 1}), ErrMalformed},
		{"size short", field(tagSize, []byte{1}), ErrMalformed},
		{"tick zero", cat(size, field(tagTick, []byte{0})), ErrLimit},
		{"cellpx too big", cat(size, field(tagCellPx, cat(varint.AppendUvarint(nil, 65536), []byte{0}))), ErrLimit},
		{"string too long", cat(size, field(tagTitle, big)), ErrLimit},
		{"string max", cat(size, field(tagTitle, ok)), nil},
		{"argv string too long", cat(size, field(tagArgv, big)), ErrLimit},
		{"env string too long", cat(size, field(tagEnv, big)), ErrLimit},
		{"engine string too long", cat(size, field(tagEngine, cat(varint.AppendUvarint(nil, MaxStringLen+1), big))), ErrLimit},
		{"font family too long", cat(size, field(tagFont, cat(varint.AppendUvarint(nil, MaxStringLen+1), big))), ErrLimit},
		{"unknown too long", cat(size, field(100, big)), ErrLimit},
		{"field len huge", cat(size, []byte{100, 0xff, 0xff, 0xff, 0xff, 0x0f}), ErrLimit},
		{"too many argv", cat(size, manyArgv), ErrLimit},
		{"max argv", cat(size, okArgv), nil},
		{"too many env", cat(size, manyEnv), ErrLimit},
		{"too many unknown", cat(size, manyUnknown), ErrLimit},
		{"field len past end", cat(size, []byte{12, 5, 'a'}), ErrMalformed},
		{"tag truncated", cat(size, []byte{0x80}), ErrMalformed},
		{"tag non-canonical", cat(size, []byte{0x80, 0x00, 0}), ErrMalformed},
		{"session id short", cat(size, field(tagSessionID, make([]byte, 15))), ErrMalformed},
		{"font truncated float", cat(size, field(tagFont, []byte{0, 0, 0, 0})), ErrMalformed},
		{"flags trailing", cat(size, field(tagFlags, []byte{1, 1})), ErrMalformed},
		{"features unsupported", cat(size, field(tagVersion, []byte{0, 2})), ErrUnsupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Unmarshal(tc.in)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMarshalErrors(t *testing.T) {
	ok := Head{Cols: 1, Rows: 1}
	tests := []struct {
		name string
		mod  func(*Head)
		want error
	}{
		{"cols zero", func(h *Head) { h.Cols = 0 }, ErrLimit},
		{"rows big", func(h *Head) { h.Rows = 65536 }, ErrLimit},
		{"cellpx negative", func(h *Head) { h.CellPxW = -1 }, ErrLimit},
		{"cellpx big", func(h *Head) { h.CellPxH = 65536 }, ErrLimit},
		{"string long", func(h *Head) { h.Cwd = strings.Repeat("a", MaxStringLen+1) }, ErrLimit},
		{"argv long", func(h *Head) { h.Argv = []string{strings.Repeat("a", MaxStringLen+1)} }, ErrLimit},
		{"argv many", func(h *Head) { h.Argv = make([]string, MaxListLen+1) }, ErrLimit},
		{"env many", func(h *Head) { h.Env = make([]string, MaxListLen+1) }, ErrLimit},
		{"unknown many", func(h *Head) { h.Unknown = make([]RawField, MaxListLen+1) }, ErrLimit},
		{"unknown long", func(h *Head) { h.Unknown = []RawField{{Tag: 50, Value: make([]byte, MaxStringLen+1)}} }, ErrLimit},
		{"unknown known tag", func(h *Head) { h.Unknown = []RawField{{Tag: 8}} }, ErrMalformed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := ok
			tc.mod(&h)
			if _, err := h.Marshal(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// fieldBoundaries returns the offsets at which a well-formed payload splits
// between fields.
func fieldBoundaries(t *testing.T, b []byte) map[int]bool {
	t.Helper()
	out := map[int]bool{0: true}
	r := varint.NewReader(b)
	for r.Remaining() > 0 {
		if _, err := r.Uvarint(); err != nil {
			t.Fatal(err)
		}
		n, err := r.Uvarint()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Bytes(int(n)); err != nil {
			t.Fatal(err)
		}
		out[len(b)-r.Remaining()] = true
	}
	return out
}

func TestUnmarshalTruncation(t *testing.T) {
	b, err := fullHead().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	boundaries := fieldBoundaries(t, b)
	sizeEnd := 0
	r := varint.NewReader(b)
	for sizeEnd == 0 {
		tag, _ := r.Uvarint()
		n, _ := r.Uvarint()
		r.Bytes(int(n))
		if tag == tagSize {
			sizeEnd = len(b) - r.Remaining()
		}
	}
	for i := 0; i < len(b); i++ {
		_, err := Unmarshal(b[:i])
		// A cut between fields is a valid shorter HEAD once the size field is present.
		if boundaries[i] && i >= sizeEnd {
			if err != nil {
				t.Errorf("cut at field boundary %d: %v", i, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("cut at %d of %d: no error", i, len(b))
		}
	}
}

func TestUnmarshalBitFlipsDoNotPanic(t *testing.T) {
	b, _ := fullHead().Marshal()
	rng := rand.New(rand.NewSource(2))
	for range 5000 {
		c := append([]byte(nil), b...)
		for range 1 + rng.Intn(4) {
			c[rng.Intn(len(c))] = byte(rng.Intn(256))
		}
		Unmarshal(c)
	}
}

func FuzzUnmarshal(f *testing.F) {
	full, _ := fullHead().Marshal()
	f.Add(full)
	minimal, _ := hex.DecodeString(goldenMinimal)
	f.Add(minimal)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		h, err := Unmarshal(b)
		if err != nil {
			return
		}
		// Anything accepted must re-encode and decode to the same value.
		out, err := h.Marshal()
		if err != nil {
			t.Fatalf("accepted input does not re-marshal: %v", err)
		}
		h2, err := Unmarshal(out)
		if err != nil {
			t.Fatalf("re-marshalled input rejected: %v", err)
		}
		out2, _ := h2.Marshal()
		if !bytes.Equal(out, out2) {
			t.Fatal("marshal not stable")
		}
	})
}
