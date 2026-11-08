package cli

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/export/spanplan"
	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/replay"
)

// Replay shares the span plan of export, so these tests check what replay
// does with it: the waits it plans, the frames it draws and the text it shows.
// Every event sits on the 100ms grid of --fps 10, so the waits are plain sums.

// hiddenSceneRecording has a scene at 4x with a hidden span inside it. The
// hidden span types SECRET and erases it again, so the screen after the span
// is the screen before it.
//
//	t=100 "a"  200 scene begin (4x)  300 "b"  400 hidden begin  500 "SECRET"
//	600 erase  700 hidden end  1000 "c"  1200 scene end  1400 "d"  exit
func hiddenSceneRecording(t *testing.T) string {
	t.Helper()
	return spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "a")
		span(t, w, 200, spanBegin(1, event.SpanScene, 0, "main", event.SpanHints{Speed: 4000}))
		out(t, w, 300, "b")
		span(t, w, 400, spanBegin(2, event.SpanHidden, 1, "", event.SpanHints{}))
		out(t, w, 500, "SECRET")
		out(t, w, 600, "\x1b[6D\x1b[K")
		span(t, w, 700, spanEnd(2))
		out(t, w, 1000, "c")
		span(t, w, 1200, spanEnd(1))
		out(t, w, 1400, "d")
		must(t, w.Exit(ms(1400), 0))
	})
}

// plannedWaits replays path with the given options on the 10 fps grid and
// returns the waits it asked for, the text of the frames drawn and the stats.
func plannedWaits(t *testing.T, path string, opts replay.Options, spans spanplan.Options) ([]time.Duration, string, string) {
	t.Helper()
	var waits []time.Duration
	if opts.Speed == 0 {
		opts.Speed = 1
	}
	opts.Sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := runReplay(cmd, path, 10, opts, spans, true); err != nil {
		t.Fatalf("runReplay() error = %v", err)
	}
	return waits, stdout.String(), stderr.String()
}

// durations turns millisecond counts into durations.
func durations(n ...int) []time.Duration {
	var d []time.Duration
	for _, v := range n {
		d = append(d, ms(v))
	}
	return d
}

func TestReplayWaitsFollowHiddenSpansAndSceneHints(t *testing.T) {
	t.Parallel()
	path := hiddenSceneRecording(t)
	// The screens are blank, "a", "ab", "abc" and "abcd"; the last one has no
	// wait. "ab" is shown 300-400 and 700-1000 (the hidden 400-700 is cut).
	tests := []struct {
		name  string
		opts  replay.Options
		spans spanplan.Options
		want  []time.Duration
	}{
		// The scene runs at 4x from 200: "a" 100 + 100/4, "ab" 100/4 + 300/4,
		// "abc" 200/4 in the scene and 200 after it.
		{"hints and cut", replay.Options{}, spanplan.Options{}, durations(100, 125, 100, 250)},
		{"no hints", replay.Options{}, spanplan.Options{IgnoreHints: true}, durations(100, 200, 400, 400)},
		// --speed 2 replaces the hint and applies to the whole recording.
		{"speed flag", replay.Options{Speed: 2}, spanplan.Options{FlagSpeed: true}, durations(50, 100, 200, 200)},
		// The hidden screens play too: "ab", then "abSECRET" 500-600, then "ab".
		{"show hidden", replay.Options{}, spanplan.Options{ShowHidden: true}, durations(100, 125, 50, 25, 100, 250)},
		{"show hidden without hints", replay.Options{}, spanplan.Options{ShowHidden: true, IgnoreHints: true}, durations(100, 200, 200, 100, 400, 400)},
	}
	for _, tt := range tests {
		got, _, _ := plannedWaits(t, path, tt.opts, tt.spans)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: waits = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// A recording without spans plays as it did before replay knew about them:
// the same waits and frames as playing its frames with no segments.
func TestReplayWithoutSpansIsUnchanged(t *testing.T) {
	t.Parallel()
	path := sceneHintRecording(t, event.SpanHints{}, false)
	opts := replay.Options{Speed: 2, IdleLimit: 300 * time.Millisecond, Clear: true, Color: true}

	want := func() ([]time.Duration, string) {
		it, _, closeAll, err := openFrames(path, 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer closeAll()
		var waits []time.Duration
		var stdout bytes.Buffer
		opts := opts
		opts.Sleep = func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		}
		if _, err := replay.PlayWithStats(t.Context(), it, &stdout, opts); err != nil {
			t.Fatal(err)
		}
		return waits, stdout.String()
	}
	wantWaits, wantOut := want()
	if len(wantWaits) == 0 {
		t.Fatal("the control replay waited never")
	}
	for _, spans := range []spanplan.Options{{}, {IgnoreHints: true}, {ShowHidden: true}, {IgnoreHints: true, ShowHidden: true}} {
		waits, stdout, _ := plannedWaits(t, path, opts, spans)
		if !reflect.DeepEqual(waits, wantWaits) || stdout != wantOut {
			t.Errorf("%+v: waits = %v, want %v; frames equal = %v", spans, waits, wantWaits, stdout == wantOut)
		}
	}
}

// The hidden text never reaches the output unless --show-hidden asks for it,
// and the cut costs no extra frame: the screen after the span is drawn as the
// next frame.
func TestReplayHidesHiddenText(t *testing.T) {
	t.Parallel()
	path := hiddenSceneRecording(t)
	run := func(args ...string) (stdout, stderr string) {
		t.Helper()
		code, stdout, stderr := runReplayCmd(t, append([]string{path, "--speed", "1000", "--fps", "10", "--clear", "--keep-leading-blank"}, args...)...)
		if code != 0 {
			t.Fatalf("replay %v: exit %d, stderr %q", args, code, stderr)
		}
		return stdout, stderr
	}

	cut, stderr := run()
	if strings.Contains(cut, "SECRET") {
		t.Errorf("default replay shows the hidden text: %q", cut)
	}
	// Five screens, one synchronized update each.
	if n := strings.Count(cut, frameStart); n != 5 {
		t.Errorf("default replay drew %d frames, want 5", n)
	}
	for _, want := range []string{"frames       5 shown (of 5)", "hidden       1 span cut, 0.3s"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("summary lacks %q:\n%s", want, stderr)
		}
	}

	noHints, _ := run("--no-hints")
	if strings.Contains(noHints, "SECRET") {
		t.Errorf("--no-hints shows the hidden text: %q", noHints)
	}

	shown, stderr := run("--show-hidden")
	if !strings.Contains(shown, "SECRET") {
		t.Errorf("--show-hidden lacks the hidden text: %q", shown)
	}
	if n := strings.Count(shown, frameStart); n != 7 {
		t.Errorf("--show-hidden drew %d frames, want 7", n)
	}
	if strings.Contains(stderr, "hidden  ") {
		t.Errorf("--show-hidden summary reports a cut:\n%s", stderr)
	}
}

// --speed and --idle-limit given on the command line beat the scene hints;
// replay has no --hold.
func TestReplaySpanFlagOverrides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want spanplan.Options
	}{
		{nil, spanplan.Options{}},
		{[]string{"--speed", "2"}, spanplan.Options{FlagSpeed: true}},
		{[]string{"--idle-limit", "1s"}, spanplan.Options{FlagIdleLimit: true}},
		{[]string{"--no-hints", "--show-hidden"}, spanplan.Options{}},
	}
	for _, tt := range tests {
		cmd := newReplayCmd()
		if err := cmd.ParseFlags(tt.args); err != nil {
			t.Fatal(err)
		}
		var got spanplan.Options
		setSpanFlagOverrides(cmd, &got)
		if got != tt.want {
			t.Errorf("%v: overrides = %+v, want %+v", tt.args, got, tt.want)
		}
	}
	if newReplayCmd().Flags().Lookup("hold") != nil {
		t.Error("replay has a --hold flag")
	}
}
