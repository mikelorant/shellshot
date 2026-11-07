package event

import (
	"reflect"
	"testing"
)

func out(tick uint64, s string) Event { return Event{Tick: tick, Kind: KindOut, Data: []byte(s)} }

// run feeds events then Flush and returns everything emitted, in order.
func run(c *Coalescer, in []Event) []Event {
	var got []Event
	for _, e := range in {
		got = append(got, c.Add(e)...)
	}
	return append(got, c.Flush()...)
}

func TestCoalescerTable(t *testing.T) {
	t.Parallel()
	mark := Event{Tick: 12, Kind: KindMark, Data: []byte("m")}
	cases := []struct {
		name     string
		gap      uint64
		max      int
		in, want []Event
	}{
		{"empty", 10, 0, nil, nil},
		{"single", 10, 0, []Event{out(5, "a")}, []Event{out(5, "a")}},
		{"merge within gap", 10, 0,
			[]Event{out(0, "a"), out(5, "b"), out(7, "c")}, []Event{out(0, "abc")}},
		{"gap boundary equal merges", 10, 0,
			[]Event{out(0, "a"), out(10, "b")}, []Event{out(0, "ab")}},
		{"gap boundary plus one splits", 10, 0,
			[]Event{out(0, "a"), out(11, "b")}, []Event{out(0, "a"), out(11, "b")}},
		{"window anchored at first tick", 10, 0,
			[]Event{out(0, "a"), out(6, "b"), out(12, "c")}, []Event{out(0, "ab"), out(12, "c")}},
		{"zero gap merges same tick only", 0, 0,
			[]Event{out(3, "a"), out(3, "b"), out(4, "c")}, []Event{out(3, "ab"), out(4, "c")}},
		{"never merge across mark", 100, 0,
			[]Event{out(10, "a"), mark, out(13, "b")}, []Event{out(10, "a"), mark, out(13, "b")}},
		{"in does not merge", 100, 0,
			[]Event{out(0, "a"), {Tick: 1, Kind: KindIn, Data: []byte("i")}, {Tick: 2, Kind: KindIn, Data: []byte("j")}, out(3, "b")},
			[]Event{out(0, "a"), {Tick: 1, Kind: KindIn, Data: []byte("i")}, {Tick: 2, Kind: KindIn, Data: []byte("j")}, out(3, "b")}},
		{"ancillary passes through", 100, 0,
			[]Event{out(0, "a"), {Tick: 1, Kind: 70, Data: []byte("z")}, out(2, "b")},
			[]Event{out(0, "a"), {Tick: 1, Kind: 70, Data: []byte("z")}, out(2, "b")}},
		{"non-out first", 100, 0, []Event{mark, out(13, "a")}, []Event{mark, out(13, "a")}},
		{"size cap exact fits", 100, 4,
			[]Event{out(0, "ab"), out(1, "cd")}, []Event{out(0, "abcd")}},
		{"size cap exceeded splits", 100, 4,
			[]Event{out(0, "ab"), out(1, "cde"), out(2, "f")}, []Event{out(0, "ab"), out(1, "cdef")}},
		{"oversize event passes whole", 100, 3,
			[]Event{out(0, "abcdef"), out(1, "g")}, []Event{out(0, "abcdef"), out(1, "g")}},
		{"tick going backwards splits", 100, 0,
			[]Event{out(10, "a"), out(5, "b")}, []Event{out(10, "a"), out(5, "b")}},
		{"empty-data out merges", 100, 0,
			[]Event{out(0, "a"), {Tick: 1, Kind: KindOut}, out(2, "b")}, []Event{out(0, "ab")}},
	}
	for _, tc := range cases {
		got := run(NewCoalescer(tc.gap, tc.max), tc.in)
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}

func TestCoalescerHoldsUntilFlush(t *testing.T) {
	t.Parallel()
	c := NewCoalescer(10, 0)
	if got := c.Add(out(0, "a")); len(got) != 0 {
		t.Fatalf("Add returned %+v", got)
	}
	if got := c.Add(out(5, "b")); len(got) != 0 {
		t.Fatalf("Add returned %+v", got)
	}
	if got := c.Flush(); !reflect.DeepEqual(got, []Event{out(0, "ab")}) {
		t.Fatalf("Flush = %+v", got)
	}
	if got := c.Flush(); len(got) != 0 {
		t.Fatalf("second Flush = %+v", got)
	}
	// Usable after Flush.
	c.Add(out(100, "x"))
	if got := c.Flush(); !reflect.DeepEqual(got, []Event{out(100, "x")}) {
		t.Fatalf("after flush: %+v", got)
	}
}

func TestCoalescerCopiesCallerBuffer(t *testing.T) {
	t.Parallel()
	c := NewCoalescer(10, 0)
	buf := []byte("aa")
	c.Add(Event{Tick: 0, Kind: KindOut, Data: buf})
	buf[0] = 'Z'
	c.Add(Event{Tick: 1, Kind: KindOut, Data: buf})
	got := c.Flush()
	if len(got) != 1 || string(got[0].Data) != "aaZa" {
		t.Fatalf("got %+v", got)
	}
}

func TestCoalescerDefaultMaxBytes(t *testing.T) {
	t.Parallel()
	c := NewCoalescer(1000, 0)
	chunk := make([]byte, DefaultMaxMerged/2)
	var got []Event
	for i := 0; i < 3; i++ {
		got = append(got, c.Add(Event{Tick: uint64(i), Kind: KindOut, Data: chunk})...)
	}
	got = append(got, c.Flush()...)
	if len(got) != 2 || len(got[0].Data) != DefaultMaxMerged || len(got[1].Data) != DefaultMaxMerged/2 {
		t.Fatalf("sizes: %d events", len(got))
	}
}

func TestCoalescedBlockRoundTrips(t *testing.T) {
	t.Parallel()
	c := NewCoalescer(10, 0)
	in := []Event{out(0, "a"), out(3, "b"), NewMark(20, "m"), out(40, "c"), NewExit(50, 3)}
	evs := run(c, in)
	b := Block{StartTick: 0, EndTick: 50, Events: evs}
	got, err := decode(encode(t, b, 0, nil, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 4 || string(got.Events[0].Data) != "ab" {
		t.Fatalf("got %+v", got.Events)
	}
}
