package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/mikelorant/shellshot/internal/replay"
)

// replaySummary is what writeReplaySummary reports once playback has ended.
type replaySummary struct {
	stats replay.Stats
	// err is the failure that ended playback, other than an interruption.
	err error
	// incomplete says why the recording stops early ("truncated (N bytes
	// valid)" or "incomplete (no exit event)"); empty for a complete one.
	incomplete string
	speed      float64
	idleLimit  time.Duration
	// recCols and recRows are the size of the recording, cols and rows the
	// terminal it was shown on (0 when frames were not cropped to it).
	recCols, recRows int
	cols, rows       int
	path             string
	fileBytes        int64 // -1 if unknown
}

// writeReplaySummary prints the block shown after a replay that drew in place:
// how playback ended, then one "label  value" line per fact, labels padded to
// a common width as in "shellshot info".
//
//	replay finished
//	frames       74 shown (of 74)
//	hidden       2 spans cut, 3.2s
//	time         11.9s played, 27.6s recorded (15.7s saved; 2 pauses capped at 2s; speed 1x)
//	screen       182x79 recorded, shown at 120x40 (cropped)
//	file         pi.shot, 6.5 KB
//
// The hidden line only appears when hidden spans were cut, and the time they
// took counts as saved. The screen line only appears when frames were cropped. The block is plain
// text.
func writeReplaySummary(w io.Writer, s replaySummary) {
	st := s.stats
	line := func(label, format string, args ...any) {
		fmt.Fprintf(w, "%-13s%s\n", label, fmt.Sprintf(format, args...))
	}

	switch {
	case st.Interrupted && st.Shown == 0:
		fmt.Fprintln(w, "replay stopped before the first frame (interrupted)")
	case st.Interrupted:
		fmt.Fprintf(w, "replay stopped at %s of %s (interrupted)\n", clockSeconds(min(st.Played, st.Planned)), clockSeconds(st.Planned))
	case s.err != nil:
		fmt.Fprintf(w, "replay failed: %s\n", strings.TrimPrefix(s.err.Error(), "replay: "))
	case s.incomplete != "":
		fmt.Fprintf(w, "replay ended early: recording is %s\n", s.incomplete)
	default:
		fmt.Fprintln(w, "replay finished")
	}

	line("frames", "%d shown (of %d)", st.Shown, st.Steps)

	if st.HiddenCut > 0 {
		noun := "spans"
		if st.HiddenCut == 1 {
			noun = "span"
		}
		line("hidden", "%d %s cut, %s", st.HiddenCut, noun, secondsText(st.HiddenTime))
	}

	var notes []string
	if saved := st.Recorded - st.Planned; saved >= 100*time.Millisecond {
		notes = append(notes, secondsText(saved)+" saved")
	}
	if st.IdleCapped > 0 {
		noun := "pauses"
		if st.IdleCapped == 1 {
			noun = "pause"
		}
		notes = append(notes, fmt.Sprintf("%d %s capped at %s", st.IdleCapped, noun, s.idleLimit))
	}
	notes = append(notes, "speed "+strconv.FormatFloat(s.speed, 'f', -1, 64)+"x")
	line("time", "%s played, %s recorded (%s)", secondsText(st.Played), secondsText(st.Recorded), strings.Join(notes, "; "))

	if st.Cropped {
		line("screen", "%dx%d recorded, shown at %dx%d (cropped)", s.recCols, s.recRows, s.cols, s.rows)
	}

	if s.fileBytes >= 0 {
		line("file", "%s, %.1f KB", s.path, float64(s.fileBytes)/1024)
	} else {
		line("file", "%s", s.path)
	}
}

// clockSeconds formats d, rounded to the second, as m:ss.
func clockSeconds(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d/time.Minute), int(d%time.Minute/time.Second))
}
