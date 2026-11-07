package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

type sliceSource struct {
	frames []snapshot.Frame
	err    error // returned instead of io.EOF once frames are exhausted
}

func (s *sliceSource) Next() (snapshot.Frame, error) {
	if len(s.frames) == 0 {
		if s.err != nil {
			return snapshot.Frame{}, s.err
		}
		return snapshot.Frame{}, io.EOF
	}
	f := s.frames[0]
	s.frames = s.frames[1:]
	return f, nil
}

func frame(at time.Duration, lines ...string) snapshot.Frame {
	return snapshot.Frame{At: at, Snapshot: snapshot.Snapshot{Cols: 10, Rows: len(lines), Lines: lines}}
}

func TestRender(t *testing.T) {
	tests := []struct {
		name  string
		snap  snapshot.Snapshot
		clear bool
		want  string
	}{
		{
			name: "plain lines end with newline",
			snap: snapshot.Snapshot{Lines: []string{"ab", "cd"}},
			want: "ab\ncd\n",
		},
		{
			name:  "clear with visible cursor",
			snap:  snapshot.Snapshot{Lines: []string{"ab", "cd"}, Cursor: snapshot.Cursor{X: 2, Y: 1, Visible: true}},
			clear: true,
			want:  frameHead + "ab\x1b[K\r\n\x1b[Jcd\x1b[2;3H\x1b[?25h" + ansiSyncEnd,
		},
		{
			name:  "clear with hidden cursor",
			snap:  snapshot.Snapshot{Lines: []string{"x"}},
			clear: true,
			want:  frameHead + "\x1b[Jx\x1b[1;1H" + ansiSyncEnd,
		},
		{
			name: "no lines",
			snap: snapshot.Snapshot{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Render(tt.snap, RenderOptions{Clear: tt.clear}); got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlayTiming(t *testing.T) {
	ms := time.Millisecond
	tests := []struct {
		name   string
		speed  float64
		frames []snapshot.Frame
		want   []time.Duration
	}{
		{
			name:   "default speed, first frame not delayed",
			frames: []snapshot.Frame{frame(500*ms, "a"), frame(700*ms, "b"), frame(1000*ms, "c")},
			want:   []time.Duration{200 * ms, 300 * ms},
		},
		{
			name:   "speed 2 halves waits",
			speed:  2,
			frames: []snapshot.Frame{frame(0, "a"), frame(200*ms, "b"), frame(600*ms, "c")},
			want:   []time.Duration{100 * ms, 200 * ms},
		},
		{
			name:   "speed 0.5 doubles waits",
			speed:  0.5,
			frames: []snapshot.Frame{frame(0, "a"), frame(100*ms, "b")},
			want:   []time.Duration{200 * ms},
		},
		{
			name:   "no wait when time does not advance",
			frames: []snapshot.Frame{frame(100*ms, "a"), frame(100*ms, "b"), frame(50*ms, "c")},
			want:   nil,
		},
		{
			name:   "empty source",
			frames: nil,
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []time.Duration
			var out bytes.Buffer
			err := Play(context.Background(), &sliceSource{frames: slices.Clone(tt.frames)}, &out, Options{
				Speed: tt.speed,
				Sleep: func(_ context.Context, d time.Duration) error {
					got = append(got, d)
					return nil
				},
			})
			if err != nil {
				t.Fatalf("Play() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sleeps = %v, want %v", got, tt.want)
			}
			var wantOut strings.Builder
			for _, f := range tt.frames {
				wantOut.WriteString(Render(f.Snapshot, RenderOptions{}))
			}
			if out.String() != wantOut.String() {
				t.Errorf("output = %q, want %q", out.String(), wantOut.String())
			}
		})
	}
}

// TestPlayIdleLimitAndLeadingBlank covers the policy Play shares with export: a
// blank first frame is dropped at any length, and each wait is capped at
// IdleLimit before Speed divides it.
func TestPlayIdleLimitAndLeadingBlank(t *testing.T) {
	sec := time.Second
	frames := []snapshot.Frame{frame(0, ""), frame(30*sec, "a"), frame(31*sec, "b"), frame(90*sec, "c")}
	tests := []struct {
		name      string
		opts      Options
		wantSleep []time.Duration
		wantShown []string
	}{
		{
			name:      "default policy",
			opts:      Options{IdleLimit: 2 * sec, TrimLeadingBlank: true},
			wantSleep: []time.Duration{sec, 2 * sec},
			wantShown: []string{"a", "b", "c"},
		},
		{
			name:      "no idle limit plays the full gaps",
			opts:      Options{TrimLeadingBlank: true},
			wantSleep: []time.Duration{sec, 59 * sec},
			wantShown: []string{"a", "b", "c"},
		},
		{
			name:      "speed divides the capped waits",
			opts:      Options{IdleLimit: 2 * sec, TrimLeadingBlank: true, Speed: 2},
			wantSleep: []time.Duration{sec / 2, sec},
			wantShown: []string{"a", "b", "c"},
		},
		{
			name:      "keeping the leading blank shows it for the capped wait",
			opts:      Options{IdleLimit: 2 * sec},
			wantSleep: []time.Duration{2 * sec, sec, 2 * sec},
			wantShown: []string{"", "a", "b", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []time.Duration
			var out bytes.Buffer
			tt.opts.Sleep = func(_ context.Context, d time.Duration) error {
				got = append(got, d)
				return nil
			}
			if err := Play(t.Context(), &sliceSource{frames: slices.Clone(frames)}, &out, tt.opts); err != nil {
				t.Fatalf("Play() error = %v", err)
			}
			if !slices.Equal(got, tt.wantSleep) {
				t.Errorf("sleeps = %v, want %v", got, tt.wantSleep)
			}
			var want strings.Builder
			for _, l := range tt.wantShown {
				want.WriteString(Render(snapshot.Snapshot{Cols: 10, Rows: 1, Lines: []string{l}}, RenderOptions{}))
			}
			if out.String() != want.String() {
				t.Errorf("output = %q, want %q", out.String(), want.String())
			}
		})
	}
}

// A lone blank frame is still shown, and frames read before a source failure
// are played before the error is returned.
func TestPlayLoneBlankAndSourceError(t *testing.T) {
	var out bytes.Buffer
	if err := Play(t.Context(), &sliceSource{frames: []snapshot.Frame{frame(0, "")}}, &out, Options{TrimLeadingBlank: true}); err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	if want := Render(snapshot.Snapshot{Cols: 10, Rows: 1, Lines: []string{""}}, RenderOptions{}); out.String() != want {
		t.Errorf("output = %q, want the lone blank frame %q", out.String(), want)
	}

	boom := errors.New("boom")
	out.Reset()
	err := Play(t.Context(), &sliceSource{frames: []snapshot.Frame{frame(0, "a")}, err: boom}, &out, Options{})
	if !errors.Is(err, boom) || out.Len() == 0 {
		t.Errorf("error = %v, output = %q, want boom after the first frame was written", err, out.String())
	}
}

func TestPlayClearOutput(t *testing.T) {
	var out bytes.Buffer
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}}
	err := Play(context.Background(), src, &out, Options{
		Clear: true,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	want := frameHead + "\x1b[Ja\x1b[1;1H" + ansiSyncEnd + frameHead + "\x1b[Jb\x1b[1;1H" + ansiSyncEnd + restoreTerminal(1)
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

// countingWriter records the size of every Write.
type countingWriter struct{ sizes []int }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.sizes = append(c.sizes, len(p))
	return len(p), nil
}

// TestPlayWritesOnceAndCropsFrames checks that each frame reaches the writer in
// one Write, and that a frame larger than Cols x Rows is cropped before it is
// written.
func TestPlayWritesOnceAndCropsFrames(t *testing.T) {
	big := snapshot.Snapshot{Cols: 6, Rows: 4, Lines: []string{"abcdef", "ghijkl", "mnopqr", "stuvwx"}}
	// The second frame differs only in a row that is cropped away: equal
	// consecutive frames would be merged into one.
	other := big
	other.Lines = []string{"ABCDEF", "ghijkl", "mnopqr", "stuvwx"}
	src := &sliceSource{frames: []snapshot.Frame{{At: 0, Snapshot: big}, {At: time.Second, Snapshot: other}}}
	var w countingWriter
	err := Play(t.Context(), src, &w, Options{
		Clear: true, Color: true, Cols: 3, Rows: 2,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	want := len(frameHead + "mno\r\n\x1b[Jstu\x1b[1;1H" + ansiSyncEnd)
	// The cropped frame is 2 rows tall; the last write restores the terminal.
	if !slices.Equal(w.sizes, []int{want, want, len(restoreTerminal(2))}) {
		t.Errorf("write sizes = %v, want one write of %d bytes per frame, then the restore", w.sizes, want)
	}
}

func TestPlayInvalidSpeed(t *testing.T) {
	for _, speed := range []float64{-1, -0.001} {
		var out bytes.Buffer
		err := Play(context.Background(), &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}, &out, Options{Speed: speed})
		if !errors.Is(err, ErrInvalidSpeed) {
			t.Errorf("speed %v: error = %v, want ErrInvalidSpeed", speed, err)
		}
		if out.Len() != 0 {
			t.Errorf("speed %v: wrote %q, want nothing", speed, out.String())
		}
	}
}

func TestPlayErrors(t *testing.T) {
	errSource := errors.New("source broke")
	errWrite := errors.New("write broke")
	errSleep := errors.New("sleep broke")

	tests := []struct {
		name string
		src  *sliceSource
		w    io.Writer
		opts Options
		want error
	}{
		{
			name: "source error is wrapped",
			src:  &sliceSource{frames: []snapshot.Frame{frame(0, "a")}, err: errSource},
			w:    io.Discard,
			want: errSource,
		},
		{
			name: "write error is wrapped",
			src:  &sliceSource{frames: []snapshot.Frame{frame(0, "a")}},
			w:    failWriter{errWrite},
			want: errWrite,
		},
		{
			name: "sleep error is wrapped",
			src:  &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}},
			w:    io.Discard,
			opts: Options{Sleep: func(context.Context, time.Duration) error { return errSleep }},
			want: errSleep,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Play(context.Background(), tt.src, tt.w, tt.opts)
			if !errors.Is(err, tt.want) {
				t.Errorf("Play() error = %v, want it to wrap %v", err, tt.want)
			}
		})
	}
}

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

func TestPlayContextCancelled(t *testing.T) {
	t.Run("before start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var out bytes.Buffer
		err := Play(ctx, &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}, &out, Options{})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
		if out.Len() != 0 {
			t.Errorf("wrote %q, want nothing", out.String())
		}
	})

	t.Run("during wait with default sleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Hour, "b")}}
		w := &cancelAfterWrite{cancel: cancel}
		done := make(chan error, 1)
		go func() { done <- Play(ctx, src, w, Options{}) }()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("error = %v, want context.Canceled", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Play did not return after cancel")
		}
		if w.writes != 1 {
			t.Errorf("writes = %d, want 1", w.writes)
		}
	})
}

// cancelAfterWrite cancels its context on the first write.
type cancelAfterWrite struct {
	cancel func()
	writes int
}

func (c *cancelAfterWrite) Write(p []byte) (int, error) {
	c.writes++
	c.cancel()
	return len(p), nil
}

func noSleep(context.Context, time.Duration) error { return nil }

func TestPlayWithStats(t *testing.T) {
	ms := time.Millisecond
	tests := []struct {
		name   string
		frames []snapshot.Frame
		opts   Options
		want   Stats
	}{
		{
			name:   "planned follows speed, recorded is the last frame",
			frames: []snapshot.Frame{frame(0, "a"), frame(400*ms, "b"), frame(1000*ms, "c")},
			opts:   Options{Speed: 2},
			want:   Stats{Steps: 3, Shown: 3, Planned: 500 * ms, Recorded: 1000 * ms},
		},
		{
			name:   "idle limit counts the pauses it cuts",
			frames: []snapshot.Frame{frame(0, "a"), frame(10*time.Second, "b"), frame(10*time.Second+ms, "c"), frame(30*time.Second, "d")},
			opts:   Options{IdleLimit: time.Second},
			want:   Stats{Steps: 4, Shown: 4, Planned: time.Second + ms + time.Second, Recorded: 30 * time.Second, IdleCapped: 2},
		},
		{
			name:   "leading blank is trimmed from the steps",
			frames: []snapshot.Frame{frame(0, ""), frame(ms, "a")},
			opts:   Options{TrimLeadingBlank: true},
			want:   Stats{Steps: 1, Shown: 1, Recorded: ms},
		},
		{
			name:   "frames larger than the terminal are cropped",
			frames: []snapshot.Frame{frame(0, "a", "b", "c")},
			opts:   Options{Clear: true, Cols: 10, Rows: 2},
			want:   Stats{Steps: 1, Shown: 1, Cropped: true},
		},
		{
			name:   "frames that fit are not cropped",
			frames: []snapshot.Frame{frame(0, "a", "b")},
			opts:   Options{Clear: true, Cols: 10, Rows: 2},
			want:   Stats{Steps: 1, Shown: 1},
		},
		{
			name: "no frames",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Sleep = noSleep
			var out bytes.Buffer
			got, err := PlayWithStats(t.Context(), &sliceSource{frames: tt.frames}, &out, tt.opts)
			if err != nil {
				t.Fatalf("PlayWithStats() error = %v", err)
			}
			if got.Played < 0 {
				t.Errorf("Played = %v, want it to be measured", got.Played)
			}
			got.Played = 0
			if got != tt.want {
				t.Errorf("stats = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Played is wall time, not the plan: with a sleep that really waits it grows.
func TestPlayWithStatsPlayedIsWallTime(t *testing.T) {
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(30*time.Millisecond, "b")}}
	got, err := PlayWithStats(t.Context(), src, io.Discard, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Played < 30*time.Millisecond {
		t.Errorf("Played = %v, want at least the 30ms wait", got.Played)
	}
}

// Cancelling during a wait stops playback, reports it in the stats and still
// restores the cursor and ends the update, so the terminal is usable.
func TestPlayWithStatsCancelledMidwayRestoresTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a", "b"), frame(time.Second, "c", "d"), frame(2*time.Second, "e", "f")}}
	var out bytes.Buffer
	stats, err := PlayWithStats(ctx, src, &out, Options{
		Clear: true,
		Sleep: func(ctx context.Context, _ time.Duration) error {
			if strings.Contains(out.String(), "c") {
				cancel()
				return ctx.Err()
			}
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !stats.Interrupted || stats.Shown != 2 || stats.Steps != 3 {
		t.Errorf("stats = %+v, want interrupted after 2 of 3 frames", stats)
	}
	if !strings.HasSuffix(out.String(), ansiSyncEnd+ansiShowCursor+ansiReset+"\x1b[2;1H\r\n") {
		t.Errorf("output ends with %q, want the cursor restore below row 2", out.String()[max(0, out.Len()-40):])
	}
}

// A failure while drawing is not an interruption.
func TestPlayWithStatsWriteFailure(t *testing.T) {
	stats, err := PlayWithStats(t.Context(), &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}, failWriter{errors.New("gone")}, Options{Clear: true})
	if err == nil || stats.Interrupted || stats.Shown != 0 {
		t.Errorf("stats = %+v, error = %v, want a plain failure with nothing shown", stats, err)
	}
}

// Cancelled before anything is drawn, nothing is hidden, so nothing is written.
func TestPlayWithStatsCancelledBeforeFirstFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out bytes.Buffer
	stats, err := PlayWithStats(ctx, &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}, &out, Options{Clear: true})
	if !errors.Is(err, context.Canceled) || !stats.Interrupted || out.Len() != 0 {
		t.Errorf("stats = %+v, error = %v, output = %q", stats, err, out.String())
	}
}
