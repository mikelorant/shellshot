package frames

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

func mark(at time.Duration, label string) bin.Event {
	return bin.Event{At: at, Kind: event.KindMark, Data: []byte(label)}
}

// TestBoundarySampling checks forced frames against an exact expected stream.
// The grid is 100ms (FPS 10).
func TestBoundarySampling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		evs        []bin.Event
		boundaries []time.Duration
		omitFinal  bool
		want       []string
	}{
		{
			name:       "inside a group, final frame",
			evs:        []bin.Event{out(10*ms, "a"), out(130*ms, "b")},
			boundaries: []time.Duration{50 * ms},
			want:       []string{"0s:", "50ms:a", "130ms:ab"},
		},
		{
			name:       "inside a group, omit final",
			evs:        []bin.Event{out(10*ms, "a"), out(130*ms, "b")},
			boundaries: []time.Duration{50 * ms},
			omitFinal:  true,
			// The group at 100ms is unchanged against the forced frame.
			want: []string{"0s:", "50ms:a", "200ms:ab"},
		},
		{
			name:       "forced frame equal to the previous frame is emitted",
			evs:        []bin.Event{out(10*ms, "a"), out(900*ms, "b")},
			boundaries: []time.Duration{100 * ms, 300 * ms, 301 * ms},
			omitFinal:  true,
			want:       []string{"0s:", "100ms:a", "300ms:a", "301ms:a", "900ms:ab"},
		},
		{
			name:       "coincides with a grid frame: one frame",
			evs:        []bin.Event{out(10*ms, "a"), out(150*ms, "b")},
			boundaries: []time.Duration{100 * ms},
			omitFinal:  true,
			want:       []string{"0s:", "100ms:a", "200ms:ab"},
		},
		{
			name:       "coincides with an unchanged grid group: still emitted",
			evs:        []bin.Event{out(10*ms, "a"), out(110*ms, "")},
			boundaries: []time.Duration{200 * ms},
			omitFinal:  true,
			want:       []string{"0s:", "100ms:a", "200ms:a"},
		},
		{
			name:       "coincident forced grid frame is stamped at the tick, not twice",
			evs:        []bin.Event{out(10*ms, "a"), out(110*ms, "b"), out(310*ms, "c")},
			boundaries: []time.Duration{200 * ms},
			want:       []string{"0s:", "100ms:a", "200ms:ab", "310ms:abc"},
		},
		{
			name:       "event exactly at the boundary is applied",
			evs:        []bin.Event{out(100*ms, "a"), out(120*ms, "b")},
			boundaries: []time.Duration{100 * ms},
			want:       []string{"0s:", "100ms:a", "120ms:ab"},
		},
		{
			name:       "before the first event",
			evs:        []bin.Event{out(500*ms, "a")},
			boundaries: []time.Duration{200 * ms},
			want:       []string{"0s:", "200ms:", "500ms:a"},
		},
		{
			name:       "at 0 without events at 0 is the initial frame",
			evs:        []bin.Event{out(50*ms, "a")},
			boundaries: []time.Duration{0},
			want:       []string{"0s:", "50ms:a"},
		},
		{
			name:       "at 0 with an event at 0",
			evs:        []bin.Event{out(0, "a"), out(250*ms, "b")},
			boundaries: []time.Duration{0},
			want:       []string{"0s:", "0s:a", "250ms:ab"},
		},
		{
			name:       "at 0 with an event at 0 that changes nothing",
			evs:        []bin.Event{out(0, ""), out(250*ms, "b")},
			boundaries: []time.Duration{0},
			want:       []string{"0s:", "250ms:b"},
		},
		{
			name:       "after the end, final frame",
			evs:        []bin.Event{out(10*ms, "a")},
			boundaries: []time.Duration{700 * ms},
			want:       []string{"0s:", "10ms:a", "700ms:a"},
		},
		{
			name:       "after the end, omit final",
			evs:        []bin.Event{out(10*ms, "a")},
			boundaries: []time.Duration{700 * ms},
			omitFinal:  true,
			want:       []string{"0s:", "100ms:a", "700ms:a"},
		},
		{
			name:       "exactly at the last event is the final frame",
			evs:        []bin.Event{out(10*ms, "a")},
			boundaries: []time.Duration{10 * ms},
			want:       []string{"0s:", "10ms:a"},
		},
		{
			name:       "at the last event, omit final",
			evs:        []bin.Event{out(10*ms, "a")},
			boundaries: []time.Duration{10 * ms},
			omitFinal:  true,
			want:       []string{"0s:", "10ms:a"},
		},
		{
			name:       "between the last event and its tick, omit final",
			evs:        []bin.Event{out(10*ms, "a")},
			boundaries: []time.Duration{50 * ms, 100 * ms, 150 * ms},
			omitFinal:  true,
			want:       []string{"0s:", "50ms:a", "100ms:a", "150ms:a"},
		},
		{
			name:       "after the end, several, final frame first",
			evs:        []bin.Event{out(10*ms, "a")},
			boundaries: []time.Duration{10 * ms, 20 * ms, 30 * ms},
			want:       []string{"0s:", "10ms:a", "20ms:a", "30ms:a"},
		},
		{
			name:       "no events",
			boundaries: []time.Duration{300 * ms},
			want:       []string{"0s:", "0s:", "300ms:"},
		},
		{
			name:       "no events, omit final",
			boundaries: []time.Duration{0, 300 * ms},
			omitFinal:  true,
			want:       []string{"0s:", "300ms:"},
		},
		{
			name:       "unsorted and duplicate boundaries",
			evs:        []bin.Event{out(10*ms, "a"), out(500*ms, "b")},
			boundaries: []time.Duration{300 * ms, 100 * ms, 300 * ms, 100 * ms},
			want:       []string{"0s:", "100ms:a", "300ms:a", "500ms:ab"},
		},
		{
			name:       "input and exit events do not move boundaries",
			evs:        []bin.Event{out(10*ms, "a"), {At: 400 * ms, Kind: event.KindIn, Data: []byte("x")}},
			boundaries: []time.Duration{200 * ms},
			want:       []string{"0s:", "100ms:a", "200ms:a", "400ms:a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _ := run(t, newSrc(tc.evs...), Options{OmitFinal: tc.omitFinal, Boundaries: tc.boundaries})
			check(t, got, tc.want...)
		})
	}
}

func TestBoundariesDoNotMutateOption(t *testing.T) {
	t.Parallel()
	in := []time.Duration{300 * ms, 100 * ms, 300 * ms}
	run(t, newSrc(out(10*ms, "a")), Options{Boundaries: in})
	if !slices.Equal(in, []time.Duration{300 * ms, 100 * ms, 300 * ms}) {
		t.Fatalf("Options.Boundaries modified: %v", in)
	}
}

func TestNegativeBoundaryIsError(t *testing.T) {
	t.Parallel()
	var ft *fakeTerm
	_, err := New(newSrc(), Options{Factory: fakeFactory(&ft), Boundaries: []time.Duration{ms, -ms}})
	if err == nil || !strings.Contains(err.Error(), "negative boundary") {
		t.Fatalf("New = %v", err)
	}
	if ft != nil {
		t.Fatal("terminal created before validation")
	}
}

func TestBoundariesInIdleGapAreConstantWork(t *testing.T) {
	t.Parallel()
	src := newSrc(out(50*ms, "a"), out(5*time.Minute, "b"))
	got, ft, _ := run(t, src, Options{Boundaries: []time.Duration{time.Minute, 2*time.Minute + 17*ms, 4 * time.Minute}})
	check(t, got, "0s:", "100ms:a", "1m0s:a", "2m0.017s:a", "4m0s:a", "5m0s:ab")
	// Same as the run without boundaries: initial + group at 100ms + final.
	// Forced frames on a clean terminal reuse the last snapshot.
	if ft.snapshots != 3 {
		t.Fatalf("Snapshot called %d times, want 3", ft.snapshots)
	}
}

func TestBoundariesInIdleGapOmitFinal(t *testing.T) {
	t.Parallel()
	src := newSrc(out(50*ms, "a"), out(5*time.Minute+30*ms, "b"))
	got, ft, _ := run(t, src, Options{OmitFinal: true, Boundaries: []time.Duration{time.Minute, 2 * time.Minute, 3 * time.Minute}})
	check(t, got, "0s:", "100ms:a", "1m0s:a", "2m0s:a", "3m0s:a", "5m0.1s:ab")
	if ft.snapshots != 3 {
		t.Fatalf("Snapshot called %d times, want 3", ft.snapshots)
	}
}

func TestBoundaryInsideDirtyGroupSnapshotsOncePerForcedFrame(t *testing.T) {
	t.Parallel()
	// Three boundaries inside one group, each after new output: one snapshot
	// each, plus the initial and the closing group.
	src := newSrc(out(10*ms, "a"), out(20*ms, "b"), out(30*ms, "c"), out(40*ms, "d"), out(900*ms, "e"))
	got, ft, _ := run(t, src, Options{OmitFinal: true, Boundaries: []time.Duration{15 * ms, 25 * ms, 35 * ms}})
	check(t, got, "0s:", "15ms:a", "25ms:ab", "35ms:abc", "100ms:abcd", "900ms:abcde")
	if ft.snapshots != 1+3+1+1 {
		t.Fatalf("Snapshot called %d times", ft.snapshots)
	}
}

func TestBoundariesWithResize(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(
		out(10*ms, "a"),
		resize(150*ms, 20, 5),
		out(250*ms, "b")),
		Options{Boundaries: []time.Duration{120 * ms, 150 * ms, 200 * ms}})
	check(t, got, "0s:", "100ms:a", "120ms:a", "150ms:a", "200ms:a", "250ms:ab")
	wantSize := [][2]int{{10, 3}, {10, 3}, {10, 3}, {20, 5}, {20, 5}, {20, 5}}
	for i, f := range got {
		if c, r := f.Snapshot.Cols, f.Snapshot.Rows; [2]int{c, r} != wantSize[i] {
			t.Errorf("frame %d (%v) is %dx%d, want %v", i, f.At, c, r, wantSize[i])
		}
	}
}

func TestBoundariesWithMarks(t *testing.T) {
	t.Parallel()
	evs := []bin.Event{
		out(10*ms, "a"), mark(120*ms, "one"), out(130*ms, "b"), mark(300*ms, "two"), out(400*ms, "c"),
	}
	collect := func(b []time.Duration) ([]snapshot.Frame, []Mark) {
		got, _, it := run(t, newSrc(evs...), Options{Boundaries: b})
		return got, it.Marks()
	}
	_, base := collect(nil)
	got, marks := collect([]time.Duration{120 * ms, 300 * ms})
	if !slices.Equal(base, marks) || len(marks) != 2 {
		t.Fatalf("marks with boundaries %v, without %v", marks, base)
	}
	// A mark at the boundary is applied before the boundary frame; it does not
	// change the screen.
	check(t, got, "0s:", "100ms:a", "120ms:a", "200ms:ab", "300ms:ab", "400ms:abc")
}

func TestBoundaryMarksSoFar(t *testing.T) {
	t.Parallel()
	var ft *fakeTerm
	it, err := New(newSrc(mark(20*ms, "one"), out(900*ms, "b")),
		Options{Factory: fakeFactory(&ft), Boundaries: []time.Duration{500 * ms}})
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	it.Next() // initial
	f, err := it.Next()
	if err != nil || f.At != 500*ms {
		t.Fatalf("Next = %v, %v", f.At, err)
	}
	if m := it.Marks(); len(m) != 1 || m[0].Label != "one" {
		t.Fatalf("marks = %v", m)
	}
}

func TestBoundaryTerminalClosedAfterLastFrame(t *testing.T) {
	t.Parallel()
	_, ft, it := run(t, newSrc(out(10*ms, "a")), Options{Boundaries: []time.Duration{time.Second}})
	if ft.closes != 1 {
		t.Fatalf("closes = %d", ft.closes)
	}
	if _, err := it.Next(); err.Error() != "EOF" {
		t.Fatalf("Next after last = %v", err)
	}
}

func TestBoundarySnapshotErrorAborts(t *testing.T) {
	t.Parallel()
	var ft *fakeTerm
	it, _ := New(newSrc(out(10*ms, "a"), out(500*ms, "b")),
		Options{Factory: fakeFactory(&ft), Boundaries: []time.Duration{50 * ms}})
	it.Next()
	ft.snapErr = fmt.Errorf("vt")
	if _, err := Collect(it); err == nil || ft.closes != 1 {
		t.Fatalf("err=%v closes=%d", err, ft.closes)
	}
}

// referenceFrames is a deliberately naive model of the documented sampling
// grid without boundaries: it replays every group into a fresh terminal.
func referenceFrames(h head.Head, evs []bin.Event, fps int, omitFinal bool) []snapshot.Frame {
	term := &fakeTerm{cols: h.Cols, rows: h.Rows, lines: []string{""}}
	snap := func() snapshot.Snapshot { s, _ := term.Snapshot(); return s }
	prev := snap()
	got := []snapshot.Frame{{At: 0, Snapshot: prev}}
	interval := period(fps)
	tickOf := func(at time.Duration) time.Duration {
		return max((at+interval-1)/interval*interval, interval)
	}
	var lastAt time.Duration
	for i := 0; i < len(evs); {
		at := max(evs[i].At, lastAt)
		tick := tickOf(at)
		j := i
		for ; j < len(evs) && tickOf(max(evs[j].At, lastAt)) == tick; j++ {
			lastAt = max(evs[j].At, lastAt)
			applyToFake(term, evs[j])
		}
		i = j
		s := snap()
		switch {
		case i == len(evs) && !omitFinal:
			got = append(got, snapshot.Frame{At: lastAt, Snapshot: s})
		case !s.Equal(prev):
			got = append(got, snapshot.Frame{At: tick, Snapshot: s})
			prev = s
		}
	}
	if len(evs) == 0 && !omitFinal {
		got = append(got, snapshot.Frame{At: 0, Snapshot: prev})
	}
	return got
}

func applyToFake(term *fakeTerm, ev bin.Event) {
	switch ev.Kind {
	case event.KindOut:
		term.Write(ev.Data)
	case event.KindResize:
		p, _ := event.ParseResize(ev.Data)
		term.Resize(int(p.Cols), int(p.Rows))
	}
}

// stateAt is the screen with every event at or before at applied, using the
// iterator's clamping of backwards timestamps.
func stateAt(h head.Head, evs []bin.Event, at time.Duration) snapshot.Snapshot {
	term := &fakeTerm{cols: h.Cols, rows: h.Rows, lines: []string{""}}
	var last time.Duration
	for _, ev := range evs {
		last = max(ev.At, last)
		if last > at {
			break
		}
		applyToFake(term, ev)
	}
	s, _ := term.Snapshot()
	return s
}

func randomEvents(r *rand.Rand) []bin.Event {
	n := r.Intn(30)
	var evs []bin.Event
	var at time.Duration
	for range n {
		switch r.Intn(4) {
		case 0:
		case 1:
			at += time.Duration(r.Intn(40)) * ms
		case 2:
			at += time.Duration(r.Intn(400)) * ms
		default:
			at += time.Duration(r.Intn(20)) * time.Second
		}
		ev := at
		if r.Intn(10) == 0 {
			ev = max(0, at-time.Duration(r.Intn(300))*ms) // goes backwards
		}
		switch r.Intn(8) {
		case 0:
			evs = append(evs, resize(ev, 5+r.Intn(15), 2+r.Intn(5)))
		case 1:
			evs = append(evs, mark(ev, "m"))
		case 2:
			evs = append(evs, bin.Event{At: ev, Kind: event.KindIn, Data: []byte("i")})
		case 3:
			evs = append(evs, out(ev, ""))
		default:
			evs = append(evs, out(ev, string(rune('a'+r.Intn(26)))+[]string{"", "", "\n"}[r.Intn(3)]))
		}
	}
	return evs
}

func randomBoundaries(r *rand.Rand, evs []bin.Event, interval time.Duration) []time.Duration {
	var last time.Duration
	for _, ev := range evs {
		last = max(last, ev.At)
	}
	var bs []time.Duration
	for range r.Intn(8) {
		var b time.Duration
		switch r.Intn(5) {
		case 0:
			b = time.Duration(r.Intn(int(last/ms)+2000)) * ms
		case 1: // on a grid tick
			b = time.Duration(r.Intn(int(last/interval)+30)) * interval
		case 2: // on an event time
			if len(evs) > 0 {
				b = evs[r.Intn(len(evs))].At
			}
		case 3:
			b = 0
		default: // after the end
			b = last + time.Duration(r.Intn(5000))*ms
		}
		bs = append(bs, b)
	}
	return bs
}

// TestRandomWithoutBoundariesMatchesGridModel compares the iterator with a
// naive model of the unchanged sampling grid.
func TestRandomWithoutBoundariesMatchesGridModel(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(1))
	for i := range 500 {
		evs := randomEvents(r)
		fps := []int{10, 60, 25, 1000, 0}[r.Intn(5)]
		omit := r.Intn(2) == 0
		h := head.Head{Cols: 10, Rows: 3}
		src := &fakeSource{h: h, evs: evs}
		var ft *fakeTerm
		it, err := New(src, Options{FPS: fps, OmitFinal: omit, Factory: fakeFactory(&ft)})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Collect(it)
		if err != nil {
			t.Fatal(err)
		}
		want := referenceFrames(h, evs, fps, omit)
		if !framesEqual(got, want) {
			t.Fatalf("case %d (fps %d omit %v): got %q, want %q", i, fps, omit, summary(got), summary(want))
		}
	}
}

func framesEqual(a, b []snapshot.Frame) bool {
	return slices.EqualFunc(a, b, func(x, y snapshot.Frame) bool {
		return x.At == y.At && x.Snapshot.Equal(y.Snapshot)
	})
}

// TestRandomBoundaryInvariants checks, over random recordings, that frame
// times never go backwards, every boundary has a frame at exactly its time
// showing the screen at that time, the final screen is last, and the cost in
// Snapshot calls is bounded by the events and boundaries.
func TestRandomBoundaryInvariants(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(2))
	for i := range 1000 {
		evs := randomEvents(r)
		fps := []int{10, 60, 25, 1000}[r.Intn(4)]
		omit := r.Intn(2) == 0
		h := head.Head{Cols: 10, Rows: 3}
		bs := randomBoundaries(r, evs, period(fps))
		var ft *fakeTerm
		it, err := New(&fakeSource{h: h, evs: evs}, Options{FPS: fps, OmitFinal: omit, Boundaries: bs, Factory: fakeFactory(&ft)})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Collect(it)
		if err != nil {
			t.Fatal(err)
		}
		desc := fmt.Sprintf("case %d fps %d omit %v boundaries %v frames %q", i, fps, omit, bs, summary(got))
		for k := 1; k < len(got); k++ {
			if got[k].At < got[k-1].At {
				t.Fatalf("%s: time goes backwards at %d", desc, k)
			}
		}
		for _, b := range bs {
			ok := false
			want := stateAt(h, evs, b)
			for _, f := range got {
				if f.At == b && f.Snapshot.Equal(want) {
					ok = true
				}
			}
			if !ok {
				t.Fatalf("%s: no frame at %v showing the screen then", desc, b)
			}
		}
		if !omit {
			end := stateAt(h, evs, 1<<62)
			if !got[len(got)-1].Snapshot.Equal(end) {
				t.Fatalf("%s: last frame is not the final screen", desc)
			}
		}
		if limit := 1 + len(evs) + len(bs); ft.snapshots > limit {
			t.Fatalf("%s: %d snapshots for %d events and %d boundaries", desc, ft.snapshots, len(evs), len(bs))
		}
	}
}

// TestRandomBoundariesInOrderAndDeduplicated checks that the order and
// duplicates of the option do not matter.
func TestRandomBoundariesInOrderAndDeduplicated(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(3))
	for range 200 {
		evs := randomEvents(r)
		bs := randomBoundaries(r, evs, period(10))
		sorted := slices.Compact(slices.Sorted(slices.Values(bs)))
		shuffled := slices.Clone(bs)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		shuffled = append(shuffled, bs...)
		a, _, _ := run(t, &fakeSource{h: head.Head{Cols: 10, Rows: 3}, evs: evs}, Options{Boundaries: sorted})
		b, _, _ := run(t, &fakeSource{h: head.Head{Cols: 10, Rows: 3}, evs: evs}, Options{Boundaries: shuffled})
		if !framesEqual(a, b) {
			t.Fatalf("boundaries %v: sorted %q, shuffled %q", bs, summary(a), summary(b))
		}
	}
}
