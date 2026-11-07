package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

// newInfoCmd builds "shellshot info", which prints the header and statistics
// of a recording.
func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info FILE",
		Short: "Show the header and statistics of a recording",
		Long: `Show the header and statistics of a recording.

The file is scanned event by event but never replayed through a terminal, so
this is fast even for large recordings.

The status line is one of:
  complete      the recording ends at a chunk boundary after the exit event
  interrupted   no exit event: the recorder was killed or crashed; everything
                up to its last flushed block is intact
  truncated (N bytes valid)
                the file has a torn tail after its last valid chunk
  damaged: ...  the file is corrupt (exit status 1)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfo(cmd, args[0])
		},
	}
}

// recordingStats are the counts gathered by scanning a recording's events.
type recordingStats struct {
	events       [5]uint64 // indexed by event.Kind for the five defined kinds
	otherEvents  uint64    // ancillary kinds
	outBytes     uint64
	payloadBytes uint64 // data bytes of all events
	last         time.Duration
}

func (s *recordingStats) total() uint64 {
	n := s.otherEvents
	for _, c := range s.events {
		n += c
	}
	return n
}

// The status values of a recording that is not damaged or truncated.
const (
	// statusComplete: the file ends at a chunk boundary after an EXIT event.
	statusComplete = "complete"
	// statusInterrupted: the file ends at a chunk boundary but has no EXIT
	// event, so the recorder was killed or crashed.
	statusInterrupted = "interrupted"
)

// runInfo prints the report for the recording at path. A torn tail or a
// missing EXIT event is reported as the status, not as an error; a corrupt
// file is reported in the status and then fails the command.
//
// An EXIT event anywhere counts: the recorder can log a keystroke after the
// child's exit, so EXIT is not always the last event.
func runInfo(cmd *cobra.Command, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("info: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("info: %w", err)
	}
	r, err := bin.OpenAt(f, fi.Size(), bin.ReaderOptions{})
	if err != nil {
		return fmt.Errorf("info: %w", openError(path, err))
	}

	var st recordingStats
	var scanErr error
	for {
		ev, err := r.Next()
		if err != nil {
			if err != io.EOF {
				scanErr = err
			}
			break
		}
		st.last = ev.At
		st.payloadBytes += uint64(len(ev.Data))
		if int(ev.Kind) < len(st.events) {
			st.events[ev.Kind]++
		} else {
			st.otherEvents++
		}
		if ev.Kind == event.KindOut {
			st.outBytes += uint64(len(ev.Data))
		}
	}

	status := statusComplete
	switch {
	case errors.Is(scanErr, bin.ErrTruncated):
		status = fmt.Sprintf("truncated (%d bytes valid)", r.ValidBytes())
		scanErr = nil
	case scanErr != nil:
		status = fmt.Sprintf("damaged: %v", scanErr)
	case st.events[event.KindExit] == 0:
		status = statusInterrupted
	}
	writeInfo(cmd.OutOrStdout(), path, r, st, fi.Size(), status)
	if scanErr != nil {
		return fmt.Errorf("info: %s: %w", path, scanErr)
	}
	return nil
}

// writeInfo prints the report. The layout is fixed: one "label  value" line
// per fact, labels padded to a common width.
func writeInfo(w io.Writer, path string, r *bin.Reader, st recordingStats, fileBytes int64, status string) {
	h := r.Head()
	line := func(label string, format string, args ...any) {
		fmt.Fprintf(w, "%-14s%s\n", label, fmt.Sprintf(format, args...))
	}

	line("file", "%s", path)
	line("size", "%dx%d", h.Cols, h.Rows)
	line("started", "%s", startedText(h))
	line("duration", "%s", st.last)
	line("recorder", "%s", orDash(h.Recorder))
	line("engine", "%s", engineText(h.Engine))
	line("command", "%s", orDash(strings.Join(h.Argv, " ")))
	if h.Flags&head.FlagInputRecorded != 0 {
		line("input", "recorded")
	} else {
		line("input", "not recorded")
	}
	line("blocks", "%d", r.Blocks())
	line("events", "%d", st.total())
	line("  OUT", "%d", st.events[event.KindOut])
	line("  IN", "%d", st.events[event.KindIn])
	line("  RESIZE", "%d", st.events[event.KindResize])
	line("  MARK", "%d", st.events[event.KindMark])
	line("  EXIT", "%d", st.events[event.KindExit])
	line("  other", "%d", st.otherEvents)
	line("out bytes", "%d", st.outBytes)
	line("payload bytes", "%d", st.payloadBytes)
	line("file bytes", "%d", fileBytes)
	if st.payloadBytes > 0 {
		line("file/payload", "%.2f", float64(fileBytes)/float64(st.payloadBytes))
	} else {
		line("file/payload", "-")
	}
	line("status", "%s", status)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// startedText formats the recording start in the recorder's time zone.
func startedText(h head.Head) string {
	if h.StartedUnixNS == 0 && h.TZOffsetS == 0 {
		return "-"
	}
	zone := time.FixedZone("", int(h.TZOffsetS))
	return time.Unix(0, h.StartedUnixNS).In(zone).Format(time.RFC3339)
}

func engineText(e head.Engine) string {
	return orDash(strings.TrimSpace(e.Name + " " + e.Commit))
}
