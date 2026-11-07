package frames

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
	"github.com/mikelorant/shellshot/internal/snapshot"
	"github.com/mikelorant/shellshot/internal/vt"
)

const ms = time.Millisecond

// fakeTerm is a line-buffer terminal: text is appended to the current line and
// "\n" starts a new one; only the last rows lines are visible.
type fakeTerm struct {
	cols, rows int
	lines      []string
	snapshots  int
	closes     int
	snapErr    error
	writeErr   error
}

func (f *fakeTerm) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	for _, c := range string(p) {
		switch c {
		case '\r':
		case '\n':
			f.lines = append(f.lines, "")
		default:
			f.lines[len(f.lines)-1] += string(c)
		}
	}
	return len(p), nil
}

func (f *fakeTerm) Resize(cols, rows int) error { f.cols, f.rows = cols, rows; return nil }

func (f *fakeTerm) Snapshot() (snapshot.Snapshot, error) {
	f.snapshots++
	if f.snapErr != nil {
		return snapshot.Snapshot{}, f.snapErr
	}
	vis := make([]string, f.rows)
	src := f.lines
	if len(src) > f.rows {
		src = src[len(src)-f.rows:]
	}
	copy(vis, src)
	return snapshot.Snapshot{Cols: f.cols, Rows: f.rows, Lines: vis}, nil
}

func (f *fakeTerm) Close() error { f.closes++; return nil }

func fakeFactory(ft **fakeTerm) vt.Factory {
	return func(cols, rows int) (vt.Terminal, error) {
		t := &fakeTerm{cols: cols, rows: rows, lines: []string{""}}
		*ft = t
		return t, nil
	}
}

// fakeSource replays a fixed event list. It reuses one Data buffer so tests
// catch iterators that keep Data past the next call.
type fakeSource struct {
	h   head.Head
	evs []bin.Event
	i   int
	err error // returned instead of io.EOF once evs is exhausted
	buf []byte
}

func (s *fakeSource) Head() head.Head { return s.h }

func (s *fakeSource) Next() (bin.Event, error) {
	if s.i >= len(s.evs) {
		if s.err != nil {
			return bin.Event{}, s.err
		}
		return bin.Event{}, io.EOF
	}
	ev := s.evs[s.i]
	s.i++
	s.buf = append(s.buf[:0], ev.Data...)
	ev.Data = s.buf
	return ev, nil
}

func out(at time.Duration, s string) bin.Event {
	return bin.Event{At: at, Kind: event.KindOut, Data: []byte(s)}
}

func resize(at time.Duration, cols, rows int) bin.Event {
	p := event.ResizePayload{Cols: uint64(cols), Rows: uint64(rows)}
	return bin.Event{At: at, Kind: event.KindResize, Data: p.Append(nil)}
}

func newSrc(evs ...bin.Event) *fakeSource {
	return &fakeSource{h: head.Head{Cols: 10, Rows: 3}, evs: evs}
}

func run(t *testing.T, src Source, o Options) ([]snapshot.Frame, *fakeTerm, *Iterator) {
	t.Helper()
	var ft *fakeTerm
	o.Factory = fakeFactory(&ft)
	if o.FPS == 0 {
		o.FPS = 10 // a 100ms grid keeps the expected times round
	}
	it, err := New(src, o)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Collect(it)
	if err != nil {
		t.Fatal(err)
	}
	return got, ft, it
}

// summary renders frames as "at:text" with lines joined by "|" and trailing
// empty lines dropped.
func summary(fs []snapshot.Frame) []string {
	var r []string
	for _, f := range fs {
		r = append(r, f.At.String()+":"+strings.TrimRight(strings.Join(f.Snapshot.Lines, "|"), "|"))
	}
	return r
}

func check(t *testing.T, got []snapshot.Frame, want ...string) {
	t.Helper()
	g := summary(got)
	if strings.Join(g, ";") != strings.Join(want, ";") {
		t.Fatalf("frames:\n got %q\nwant %q", g, want)
	}
}

func TestSamplingGrid(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(
		out(10*ms, "a"), out(40*ms, "b"), // boundary 100ms
		out(100*ms, "c"),  // exactly on the boundary: same group
		out(101*ms, "d"),  // boundary 200ms
		out(350*ms, "e"),  // boundary 400ms
		out(399*ms, "f")), // same group, last: final frame at last event time
		Options{})
	check(t, got,
		"0s:",
		"100ms:abc",
		"200ms:abcd",
		"399ms:abcdef", // final frame: last event time replaces the 400ms boundary
	)
}

func TestSamplingGridOmitFinal(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(out(10*ms, "a"), out(130*ms, "b")), Options{OmitFinal: true})
	check(t, got, "0s:", "100ms:a", "200ms:ab")
}

func TestUnchangedScreenNotEmitted(t *testing.T) {
	t.Parallel()
	// The group at 200ms writes nothing visible (empty OUT), so no frame.
	got, _, _ := run(t, newSrc(
		out(10*ms, "a"), out(150*ms, ""), out(250*ms, "b")), Options{OmitFinal: true})
	check(t, got, "0s:", "100ms:a", "300ms:ab")
}

func TestFinalFrameAlwaysEmittedOnce(t *testing.T) {
	t.Parallel()
	// Only an IN event after the last change: the final frame is still emitted
	// even though it equals the previous one.
	got, _, _ := run(t, newSrc(
		out(10*ms, "a"),
		bin.Event{At: 500 * ms, Kind: event.KindIn, Data: []byte("x")}), Options{})
	check(t, got, "0s:", "100ms:a", "500ms:a")
}

func TestNoEventsEmitsInitialAndFinal(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(), Options{})
	check(t, got, "0s:", "0s:")
	got, _, _ = run(t, newSrc(), Options{OmitFinal: true})
	check(t, got, "0s:")
}

func TestIdleGapIsConstantWork(t *testing.T) {
	t.Parallel()
	src := newSrc(out(50*ms, "a"), out(5*time.Minute, "b"))
	got, ft, _ := run(t, src, Options{})
	check(t, got, "0s:", "100ms:a", "5m0s:ab")
	// initial + group at 100ms + final group = 3, not ~3000.
	if ft.snapshots != 3 {
		t.Fatalf("Snapshot called %d times, want 3", ft.snapshots)
	}
}

func TestIdleGapOmitFinal(t *testing.T) {
	t.Parallel()
	src := newSrc(out(50*ms, "a"), out(5*time.Minute+30*ms, "b"))
	got, ft, _ := run(t, src, Options{OmitFinal: true})
	check(t, got, "0s:", "100ms:a", "5m0.1s:ab")
	if ft.snapshots != 3 {
		t.Fatalf("Snapshot called %d times, want 3", ft.snapshots)
	}
}

func TestResizeMidStream(t *testing.T) {
	t.Parallel()
	got, ft, _ := run(t, newSrc(
		out(10*ms, "hi"),
		resize(150*ms, 40, 5),
		out(160*ms, "\nyo")), Options{})
	if len(got) != 3 {
		t.Fatalf("got %d frames: %q", len(got), summary(got))
	}
	if s := got[0].Snapshot; s.Cols != 10 || s.Rows != 3 {
		t.Errorf("initial size %dx%d", s.Cols, s.Rows)
	}
	if s := got[1].Snapshot; s.Cols != 10 || s.Rows != 3 {
		t.Errorf("frame before resize %dx%d", s.Cols, s.Rows)
	}
	last := got[2]
	if last.Snapshot.Cols != 40 || last.Snapshot.Rows != 5 || len(last.Snapshot.Lines) != 5 {
		t.Errorf("after resize %dx%d lines=%d", last.Snapshot.Cols, last.Snapshot.Rows, len(last.Snapshot.Lines))
	}
	if last.At != 160*ms {
		t.Errorf("final At = %v", last.At)
	}
	if ft.cols != 40 || ft.rows != 5 {
		t.Errorf("terminal size %dx%d", ft.cols, ft.rows)
	}
}

func TestResizeAloneChangesScreen(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(resize(50*ms, 20, 3), out(250*ms, "x")), Options{OmitFinal: true})
	if len(got) != 3 || got[1].Snapshot.Cols != 20 || got[1].At != 100*ms {
		t.Fatalf("frames: %q", summary(got))
	}
}

func TestInvalidResize(t *testing.T) {
	t.Parallel()
	for name, ev := range map[string]bin.Event{
		"zero":      resize(10*ms, 0, 5),
		"malformed": {At: 10 * ms, Kind: event.KindResize, Data: []byte{0x80}},
	} {
		t.Run(name, func(t *testing.T) {
			var ft *fakeTerm
			it, err := New(newSrc(ev), Options{Factory: fakeFactory(&ft)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Collect(it); err == nil {
				t.Fatal("want error")
			}
			if ft.closes != 1 {
				t.Errorf("terminal closed %d times", ft.closes)
			}
		})
	}
}

func TestMarksCollected(t *testing.T) {
	t.Parallel()
	var ft *fakeTerm
	it, err := New(newSrc(
		out(10*ms, "a"),
		bin.Event{At: 20 * ms, Kind: event.KindMark, Data: []byte("one")},
		out(300*ms, "b"),
		bin.Event{At: 400 * ms, Kind: event.KindMark, Data: []byte("two")}),
		Options{Factory: fakeFactory(&ft)})
	if err != nil {
		t.Fatal(err)
	}
	if m := it.Marks(); len(m) != 0 {
		t.Fatalf("marks before reading: %v", m)
	}
	for {
		if _, err := it.Next(); err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
	}
	want := []Mark{{20 * ms, "one"}, {400 * ms, "two"}}
	got := it.Marks()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("marks = %v, want %v", got, want)
	}
}

func TestMarksSoFar(t *testing.T) {
	t.Parallel()
	var ft *fakeTerm
	it, _ := New(newSrc(
		bin.Event{At: 20 * ms, Kind: event.KindMark, Data: []byte("one")},
		out(300*ms, "b"),
		bin.Event{At: 900 * ms, Kind: event.KindMark, Data: []byte("two")}),
		Options{Factory: fakeFactory(&ft)})
	it.Next() // initial frame
	// The mark group is complete once the 300ms event is read, but the screen
	// is unchanged so no frame; the next frame comes after "b".
	if _, err := it.Next(); err != nil {
		t.Fatal(err)
	}
	if m := it.Marks(); len(m) != 1 || m[0].Label != "one" {
		t.Fatalf("marks so far = %v", m)
	}
	it.Close()
}

func TestInputExitAndAncillaryIgnored(t *testing.T) {
	t.Parallel()
	exit := event.NewExit(0, 3)
	got, ft, _ := run(t, newSrc(
		bin.Event{At: 10 * ms, Kind: event.KindIn, Data: []byte("secret\r")},
		bin.Event{At: 20 * ms, Kind: event.KindExit, Data: exit.Data},
		bin.Event{At: 30 * ms, Kind: 64, Data: []byte("zzz")}),
		Options{OmitFinal: true})
	check(t, got, "0s:")
	if ft.snapshots != 1 { // only the initial one: nothing touched the terminal
		t.Fatalf("snapshots = %d", ft.snapshots)
	}
}

func TestBackwardsTimestampClamped(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(
		out(250*ms, "a"),
		out(100*ms, "b"), // goes backwards: treated as 250ms
		out(260*ms, "c")), Options{})
	check(t, got, "0s:", "260ms:abc")
}

func TestBackwardsTimestampAcrossGroupBoundary(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(
		out(250*ms, "a"), out(310*ms, "b"), out(20*ms, "c")), Options{OmitFinal: true})
	check(t, got, "0s:", "300ms:a", "400ms:abc")
}

func TestFPSDefault(t *testing.T) {
	t.Parallel()
	// Not positive selects 60 fps: a period of 16.667ms (rounded to the
	// microsecond), so 1ms lands on 16.667ms and 20ms on 33.334ms.
	for _, fps := range []int{0, -5} {
		var ft *fakeTerm
		it, err := New(newSrc(out(1*ms, "a"), out(20*ms, "b")), Options{FPS: fps, OmitFinal: true, Factory: fakeFactory(&ft)})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Collect(it)
		if err != nil {
			t.Fatal(err)
		}
		check(t, got, "0s:", "16.667ms:a", "33.334ms:ab")
	}
}

func TestCustomFPS(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(out(1*ms, "a"), out(60*ms, "b"), out(1001*ms, "c")),
		Options{FPS: 20, OmitFinal: true})
	check(t, got, "0s:", "50ms:a", "100ms:ab", "1.05s:abc")
}

func TestPeriodRounding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fps  int
		want time.Duration
	}{
		{0, 16667 * time.Microsecond},
		{60, 16667 * time.Microsecond},
		{10, 100 * ms},
		{1000, ms},
		{3_000_000, time.Microsecond}, // never zero
	} {
		if got := period(tc.fps); got != tc.want {
			t.Errorf("period(%d) = %v, want %v", tc.fps, got, tc.want)
		}
	}
}

func TestEventAtZero(t *testing.T) {
	t.Parallel()
	got, _, _ := run(t, newSrc(out(0, "a"), out(0, "b")), Options{OmitFinal: true})
	check(t, got, "0s:", "100ms:ab")
}

func TestDeferredEventDataSurvivesSourceReuse(t *testing.T) {
	t.Parallel()
	// fakeSource overwrites its buffer on every Next; the event that closes a
	// group is read before the group is sampled and must still be intact.
	got, _, _ := run(t, newSrc(out(10*ms, "aaaa"), out(150*ms, "bb"), out(260*ms, "cccccc")), Options{OmitFinal: true})
	check(t, got, "0s:", "100ms:aaaa", "200ms:aaaabb", "300ms:aaaabbcccccc")
}

func TestCloseIdempotent(t *testing.T) {
	t.Parallel()
	var ft *fakeTerm
	it, err := New(newSrc(out(10*ms, "a")), Options{Factory: fakeFactory(&ft)})
	if err != nil {
		t.Fatal(err)
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	if ft.closes != 1 {
		t.Fatalf("closes = %d", ft.closes)
	}
	if _, err := it.Next(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Next after Close = %v", err)
	}
}

func TestTerminalClosedAtEOF(t *testing.T) {
	t.Parallel()
	_, ft, it := run(t, newSrc(out(10*ms, "a")), Options{})
	if ft.closes != 1 {
		t.Fatalf("closes after Collect = %d", ft.closes)
	}
	if _, err := it.Next(); err != io.EOF {
		t.Fatalf("Next after EOF = %v", err)
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	if ft.closes != 1 {
		t.Fatalf("closes after Close = %d", ft.closes)
	}
}

func TestSourceErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	src := newSrc(out(10*ms, "a"))
	src.err = boom
	var ft *fakeTerm
	it, err := New(src, Options{Factory: fakeFactory(&ft)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := it.Next(); err != nil { // initial frame
		t.Fatal(err)
	}
	if _, err := it.Next(); !errors.Is(err, boom) {
		t.Fatalf("Next = %v, want boom", err)
	}
	if _, err := it.Next(); !errors.Is(err, boom) {
		t.Fatalf("error not sticky: %v", err)
	}
	if ft.closes != 1 {
		t.Fatalf("closes = %d", ft.closes)
	}
}

func TestTruncatedSourceErrorPropagates(t *testing.T) {
	t.Parallel()
	src := newSrc(out(10*ms, "a"))
	src.err = bin.ErrTruncated
	var ft *fakeTerm
	it, _ := New(src, Options{Factory: fakeFactory(&ft)})
	if _, err := Collect(it); !errors.Is(err, bin.ErrTruncated) {
		t.Fatalf("Collect = %v", err)
	}
}

func TestFactoryError(t *testing.T) {
	t.Parallel()
	boom := errors.New("no terminal")
	_, err := New(newSrc(), Options{Factory: func(c, r int) (vt.Terminal, error) { return nil, boom }})
	if !errors.Is(err, boom) {
		t.Fatalf("New = %v", err)
	}
	if _, err := New(newSrc(), Options{}); err == nil {
		t.Fatal("nil Factory accepted")
	}
}

func TestFactoryReceivesHeadSize(t *testing.T) {
	t.Parallel()
	src := newSrc()
	src.h.Cols, src.h.Rows = 132, 43
	var gc, gr int
	_, err := New(src, Options{Factory: func(c, r int) (vt.Terminal, error) {
		gc, gr = c, r
		return &fakeTerm{cols: c, rows: r, lines: []string{""}}, nil
	}})
	if err != nil || gc != 132 || gr != 43 {
		t.Fatalf("factory got %dx%d err=%v", gc, gr, err)
	}
}

func TestInitialSnapshotErrorClosesTerminal(t *testing.T) {
	t.Parallel()
	boom := errors.New("snap")
	ft := &fakeTerm{cols: 1, rows: 1, snapErr: boom}
	_, err := New(newSrc(), Options{Factory: func(c, r int) (vt.Terminal, error) { return ft, nil }})
	if !errors.Is(err, boom) || ft.closes != 1 {
		t.Fatalf("err=%v closes=%d", err, ft.closes)
	}
}

func TestWriteAndSnapshotErrorsAbort(t *testing.T) {
	t.Parallel()
	boom := errors.New("vt")
	var ft *fakeTerm
	it, _ := New(newSrc(out(10*ms, "a")), Options{Factory: fakeFactory(&ft)})
	ft.writeErr = boom
	if _, err := Collect(it); !errors.Is(err, boom) || ft.closes != 1 {
		t.Fatalf("write: err=%v closes=%d", err, ft.closes)
	}

	it, _ = New(newSrc(out(10*ms, "a")), Options{Factory: fakeFactory(&ft)})
	it.Next()
	ft.snapErr = boom
	if _, err := Collect(it); !errors.Is(err, boom) || ft.closes != 1 {
		t.Fatalf("snapshot: err=%v closes=%d", err, ft.closes)
	}
}

func TestManyEventsInOneGroupSnapshotOnce(t *testing.T) {
	t.Parallel()
	var evs []bin.Event
	for i := range 500 {
		evs = append(evs, out(time.Duration(i)*100*time.Microsecond, strconv.Itoa(i%10)))
	}
	_, ft, _ := run(t, newSrc(evs...), Options{OmitFinal: true})
	// All events fall in (0, 100ms]: initial snapshot + one group snapshot.
	if ft.snapshots != 2 {
		t.Fatalf("snapshots = %d, want 2", ft.snapshots)
	}
}
