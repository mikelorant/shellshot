package cli

import (
	"bytes"
	"image"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	kettek "github.com/kettek/apng"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
)

// The span fixtures put every event on the 100ms sampling grid of --fps 10,
// so frame times are exact and the expected delays below are plain sums.

const spanFPS = "10"

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func spanBegin(id uint64, typ event.SpanType, parent uint64, name string, h event.SpanHints) event.Span {
	return event.Span{Op: event.SpanBegin, ID: id, Type: typ, Parent: parent, Name: name, Hints: h}
}

func spanEnd(id uint64) event.Span { return event.Span{Op: event.SpanEnd, ID: id} }

// spanRecording writes a 10x2 recording; fill adds the events.
func spanRecording(t *testing.T, fill func(w *bin.Writer)) string {
	t.Helper()
	data := buildRecording(t, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) { fill(w) })
	return writeFile(t, "spans.shot", data)
}

func out(t *testing.T, w *bin.Writer, at int, s string) {
	t.Helper()
	must(t, w.Output(ms(at), []byte(s)))
}

func span(t *testing.T, w *bin.Writer, at int, s event.Span) {
	t.Helper()
	must(t, w.Span(ms(at), s))
}

// sceneHintRecording has one scene [200ms, 1200ms) with the given hints.
//
//	t=100 "a"  200 scene begin  300 "b"  1000 "c"  1200 scene end  1400 "d"  exit
func sceneHintRecording(t *testing.T, h event.SpanHints, withSpans bool) string {
	return spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "a")
		if withSpans {
			span(t, w, 200, spanBegin(1, event.SpanScene, 0, "main", h))
		}
		out(t, w, 300, "b")
		out(t, w, 1000, "c")
		if withSpans {
			span(t, w, 1200, spanEnd(1))
		}
		out(t, w, 1400, "d")
		must(t, w.Exit(ms(1400), 0))
	})
}

// hiddenRecording hides [200ms, 600ms), in which "b" and "c" are printed.
//
//	t=100 "a"  200 hidden begin  300 "b"  500 "c"  600 hidden end  800 "d"  exit
func hiddenRecording(t *testing.T) string {
	return spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "a")
		span(t, w, 200, spanBegin(1, event.SpanHidden, 0, "", event.SpanHints{}))
		out(t, w, 300, "b")
		out(t, w, 500, "c")
		span(t, w, 600, spanEnd(1))
		out(t, w, 800, "d")
		must(t, w.Exit(ms(800), 0))
	})
}

// exportSpans runs export on in with --fps 10 and extra flags and returns the
// decoded animation.
func exportSpans(t *testing.T, in string, flags ...string) (kettek.APNG, string) {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "out.png")
	args := append([]string{in, "-o", outPath, "--fps", spanFPS}, flags...)
	code, stderr := runExportCmd(t, args...)
	if code != 0 {
		t.Fatalf("export %v: exit %d, stderr %q", flags, code, stderr)
	}
	return decodeAPNG(t, outPath), stderr
}

// delaysOf returns the frame delays of a.
func delaysOf(a kettek.APNG) []time.Duration {
	var d []time.Duration
	for _, f := range a.Frames {
		den := f.DelayDenominator
		if den == 0 {
			den = 100
		}
		d = append(d, time.Duration(f.DelayNumerator)*time.Second/time.Duration(den))
	}
	return d
}

func sum(ds []time.Duration) (total time.Duration) {
	for _, d := range ds {
		total += d
	}
	return total
}

func checkDelays(t *testing.T, a kettek.APNG, wantMS ...int) {
	t.Helper()
	var want []time.Duration
	for _, m := range wantMS {
		want = append(want, ms(m))
	}
	got := delaysOf(a)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("delays = %v (total %v), want %v (total %v)", got, sum(got), want, sum(want))
	}
}

func TestExportSceneSpeedHintShortensTheScene(t *testing.T) {
	in := sceneHintRecording(t, event.SpanHints{Speed: 4000}, true)

	// Screens: blank, "a", "ab", "abc", "abcd". Without hints the delays are
	// 100 100+100 700 200+200 and the 2s hold: 3400ms.
	plain, _ := exportSpans(t, in, "--no-hints")
	checkDelays(t, plain, 100, 200, 700, 400, 2000)

	// Inside [200, 1200) every delay is a quarter: 25 175 50 on top of the
	// unscaled 100 100 200.
	scaled, _ := exportSpans(t, in)
	checkDelays(t, scaled, 100, 125, 175, 250, 2000)
	if got, want := sum(delaysOf(scaled)), ms(2650); got != want {
		t.Errorf("total = %v, want %v", got, want)
	}
}

func TestExportSceneIdleAndHoldHints(t *testing.T) {
	in := sceneHintRecording(t, event.SpanHints{Speed: 4000, Idle: ms(200), Hold: ms(500)}, true)
	// "ab" is shown 700ms in the scene, capped to 200 and quartered: 50. The
	// hold lands on "abc", the last screen before the scene end.
	a, _ := exportSpans(t, in)
	checkDelays(t, a, 100, 125, 50, 750, 2000)
}

func TestExportFlagsBeatTheirHints(t *testing.T) {
	in := sceneHintRecording(t, event.SpanHints{Speed: 4000, Idle: ms(200), Hold: ms(500)}, true)
	tests := []struct {
		name  string
		flags []string
		want  []int
	}{
		// The speed hint is replaced by 2x everywhere; idle and hold hints stay.
		{"speed", []string{"--speed", "2"}, []int{50, 100, 100, 700, 2000}},
		// The idle hint is replaced by 10s; speed and hold hints stay.
		{"idle-limit", []string{"--idle-limit", "10s"}, []int{100, 125, 175, 750, 2000}},
		// The hold hint is dropped and the last screen stays for 1s; the
		// others stay.
		{"hold", []string{"--hold", "1s"}, []int{100, 125, 50, 250, 1000}},
		// An explicit flag at its default value still beats the hint.
		{"speed 1", []string{"--speed", "1"}, []int{100, 200, 200, 400 + 500, 2000}},
		{"all", []string{"--speed", "1", "--idle-limit", "2s", "--hold", "2s"}, []int{100, 200, 700, 400, 2000}},
	}
	for _, tt := range tests {
		a, _ := exportSpans(t, in, tt.flags...)
		t.Run(tt.name, func(t *testing.T) { checkDelays(t, a, tt.want...) })
	}
}

func TestExportNoHintsIgnoresSceneHintsButNotHiddenSpans(t *testing.T) {
	in := spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "a")
		span(t, w, 200, spanBegin(1, event.SpanScene, 0, "main", event.SpanHints{Speed: 4000, Hold: sec}))
		out(t, w, 300, "b")
		span(t, w, 400, spanBegin(2, event.SpanHidden, 1, "", event.SpanHints{}))
		out(t, w, 500, "c")
		span(t, w, 800, spanEnd(2))
		out(t, w, 1000, "d")
		span(t, w, 1200, spanEnd(1))
		out(t, w, 1400, "e")
		must(t, w.Exit(ms(1400), 0))
	})
	// Screens: blank 100, a 200 (100..300), ab 100 (300..400), abc cut,
	// abc 200 (800..1000), abcd 400 (1000..1400), abcde 2000.
	a, _ := exportSpans(t, in, "--no-hints")
	checkDelays(t, a, 100, 200, 100, 200, 400, 2000)
}

const sec = time.Second

func TestExportHiddenSpanIsCutAndNextFrameShowsPostSpanScreen(t *testing.T) {
	in := hiddenRecording(t)

	// blank 100, "a" 100 (100..200), then the hidden 200..600 is cut and
	// "abc" is shown from 600 for 200, then "abcd" for the 2s hold.
	a, _ := exportSpans(t, in)
	checkDelays(t, a, 100, 100, 200, 2000)
	if got, want := sum(delaysOf(a)), ms(2400); got != want {
		t.Errorf("total = %v, want %v", got, want)
	}

	// --show-hidden: nothing is cut, so the screens "ab" and "abc" appear.
	shown, _ := exportSpans(t, in, "--show-hidden")
	checkDelays(t, shown, 100, 200, 200, 300, 2000)

	// The frame after the hidden span is the "abc" screen: the same pixels as
	// the "abc" frame of the uncut export, not "a" and not "abcd".
	if len(a.Frames) != 4 || len(shown.Frames) != 5 {
		t.Fatalf("frames = %d and %d, want 4 and 5", len(a.Frames), len(shown.Frames))
	}
	if !samePixels(compose(t, a, 2), compose(t, shown, 3)) {
		t.Error("frame after the hidden span is not the post-span screen")
	}
	if samePixels(compose(t, a, 2), compose(t, a, 1)) {
		t.Error("the frame after the hidden span equals the frame before it")
	}
}

func samePixels(a, b *image.RGBA) bool { return a.Rect == b.Rect && bytes.Equal(a.Pix, b.Pix) }

func TestExportHiddenInsideScene(t *testing.T) {
	in := spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "a")
		span(t, w, 200, spanBegin(1, event.SpanScene, 0, "main", event.SpanHints{Speed: 2000}))
		out(t, w, 300, "b")
		span(t, w, 400, spanBegin(2, event.SpanHidden, 1, "", event.SpanHints{}))
		out(t, w, 500, "c")
		span(t, w, 800, spanEnd(2))
		out(t, w, 1000, "d")
		span(t, w, 1200, spanEnd(1))
		out(t, w, 1400, "e")
		must(t, w.Exit(ms(1400), 0))
	})
	// blank 100; "a" 100 + 100/2; "ab" 100/2 (300..400, then cut to 800);
	// "abc" 200/2 (800..1000); "abcd" 200/2 + 200 (1000..1200..1400);
	// "abcde" 2000.
	a, _ := exportSpans(t, in)
	checkDelays(t, a, 100, 150, 50, 100, 300, 2000)
	if got, want := sum(delaysOf(a)), ms(2700); got != want {
		t.Errorf("total = %v, want %v", got, want)
	}
}

// A recorder that was killed leaves a scene open: it ends at the last event.
func TestExportOpenSpanEndsAtLastEvent(t *testing.T) {
	in := spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "a")
		span(t, w, 200, spanBegin(1, event.SpanScene, 0, "main", event.SpanHints{Speed: 4000}))
		out(t, w, 300, "b")
		out(t, w, 1000, "c")
	})
	// blank 100; "a" 100 + 100/4; "ab" 700/4; "abc" is the last frame: the
	// 2s hold.
	a, stderr := exportSpans(t, in)
	checkDelays(t, a, 100, 125, 175, 2000)
	if !strings.Contains(stderr, "incomplete") {
		t.Errorf("stderr = %q, want the incomplete-recording note", stderr)
	}
}

func TestExportSpanFlagsAreAccepted(t *testing.T) {
	in := hiddenRecording(t)
	for _, flag := range []string{"--no-hints", "--show-hidden"} {
		exportSpans(t, in, flag)
	}
	code, stderr := runExportCmd(t, in, "--scene", "x")
	if code == 0 || !strings.Contains(stderr, "unknown flag: --scene") {
		t.Errorf("--scene: exit %d, stderr %q, want an unknown-flag error", code, stderr)
	}
}

// Without spans (or with every span effect switched off) the output is the
// same bytes as before: no segments, no forced frames.
func TestExportWithoutSpansIsUnchanged(t *testing.T) {
	plain := sceneHintRecording(t, event.SpanHints{}, false)
	base := mustRead(t, exportTo(t, plain))
	for _, flags := range [][]string{{"--no-hints"}, {"--show-hidden"}, {"--no-hints", "--show-hidden"}} {
		if got := mustRead(t, exportTo(t, plain, flags...)); !bytes.Equal(got, base) {
			t.Errorf("flags %v change the output of a recording without spans", flags)
		}
	}

	// A recording whose spans have no effect exports to the same bytes as the
	// recording without the span events.
	withSpans := sceneHintRecording(t, event.SpanHints{Speed: 4000, Idle: ms(200), Hold: ms(500)}, true)
	if got := mustRead(t, exportTo(t, withSpans, "--no-hints")); !bytes.Equal(got, base) {
		t.Error("--no-hints output differs from the recording without spans")
	}
	if got := mustRead(t, exportTo(t, withSpans, "--speed", "1", "--idle-limit", "2s", "--hold", "2s")); !bytes.Equal(got, base) {
		t.Error("explicit flags at their defaults differ from the recording without spans")
	}
	hidden := hiddenRecording(t)
	stripped := spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "a")
		out(t, w, 300, "b")
		out(t, w, 500, "c")
		out(t, w, 800, "d")
		must(t, w.Exit(ms(800), 0))
	})
	if got, want := mustRead(t, exportTo(t, hidden, "--show-hidden")), mustRead(t, exportTo(t, stripped)); !bytes.Equal(got, want) {
		t.Error("--show-hidden output differs from the recording without spans")
	}
}

func exportTo(t *testing.T, in string, flags ...string) string {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "out.png")
	args := append([]string{in, "-o", outPath, "--fps", spanFPS}, flags...)
	if code, stderr := runExportCmd(t, args...); code != 0 {
		t.Fatalf("export %v: exit %d, stderr %q", flags, code, stderr)
	}
	return outPath
}

// A malformed SPAN payload ends the span list; the export still succeeds.
func TestExportMalformedSpanWarns(t *testing.T) {
	in := spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "a")
		must(t, w.WriteEvent(ms(200), event.KindSpan, []byte{1, 1}))
		out(t, w, 300, "b")
		must(t, w.Exit(ms(300), 0))
	})
	a, stderr := exportSpans(t, in)
	if !strings.Contains(stderr, "malformed span") || !strings.Contains(stderr, "ignored") {
		t.Errorf("stderr = %q, want a malformed-span warning", stderr)
	}
	checkDelays(t, a, 100, 200, 2000)
}
