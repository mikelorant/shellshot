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
	want := frameHead + "\x1b[Ja\x1b[1;1H" + ansiSyncEnd + frameHead + "\x1b[Jb\x1b[1;1H" + ansiSyncEnd
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
	src := &sliceSource{frames: []snapshot.Frame{{At: 0, Snapshot: big}, {At: time.Second, Snapshot: big}}}
	var w countingWriter
	err := Play(t.Context(), src, &w, Options{
		Clear: true, Color: true, Cols: 3, Rows: 2,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatalf("Play() error = %v", err)
	}
	want := len(frameHead + "mno\r\n\x1b[Jstu\x1b[1;1H" + ansiSyncEnd)
	if !slices.Equal(w.sizes, []int{want, want}) {
		t.Errorf("write sizes = %v, want one write of %d bytes per frame", w.sizes, want)
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
