package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/export/spanplan"
	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
	"github.com/mikelorant/shellshot/internal/recording/frames"
	"github.com/mikelorant/shellshot/internal/recording/spans"
	"github.com/mikelorant/shellshot/internal/session"
	"github.com/mikelorant/shellshot/internal/vt/ghostty"
)

// errNotRecording is what a file that does not start with the recording magic
// (a JSON recording, a text file, ...) reports.
var errNotRecording = errors.New("not a shellshot recording (bad magic)")

// openError turns an error from bin.NewReader or bin.OpenAt into one a user
// can act on.
func openError(path string, err error) error {
	if errors.Is(err, bin.ErrBadMagic) {
		err = errNotRecording
	}
	return fmt.Errorf("%s: %w", path, err)
}

// validateFPS reports an unusable --fps value.
func validateFPS(fps int) error {
	if fps < 1 || fps > session.MaxFPS {
		return fmt.Errorf("invalid --fps %d: must be between 1 and %d", fps, session.MaxFPS)
	}
	return nil
}

// addFPSFlag registers --fps on cmd, binding it to fps.
func addFPSFlag(cmd *cobra.Command, fps *int) {
	cmd.Flags().IntVar(fps, "fps", session.DefaultFPS, "frame sampling rate in frames per second")
}

// incompleteNote is the stderr line for a recording that ends cleanly but has
// no EXIT event: its recorder was killed or crashed.
const incompleteNote = "shellshot: recording is incomplete (no exit event; recorder was interrupted)"

// prefixSource is a frames.Source over a bin.Reader that reports a torn tail
// (bin.ErrTruncated) as a clean end, so the valid prefix of a crashed
// recording is still played. truncated and interrupted tell the caller
// afterwards what happened.
type prefixSource struct {
	r         *bin.Reader
	truncated bool // the file has a torn tail
	sawExit   bool // an EXIT event was read
	ended     bool // the file ended at a chunk boundary
}

func (s *prefixSource) Head() head.Head { return s.r.Head() }

func (s *prefixSource) Next() (bin.Event, error) {
	ev, err := s.r.Next()
	switch {
	case err == nil:
		if ev.Kind == event.KindExit {
			s.sawExit = true
		}
	case errors.Is(err, bin.ErrTruncated):
		s.truncated = true
		return bin.Event{}, io.EOF
	case errors.Is(err, io.EOF):
		s.ended = true
	}
	return ev, err
}

// warnIncomplete writes the note about an interrupted or torn recording to w
// when the source stopped at the end of such a file: a torn tail is reported
// with its valid length, a clean end without an EXIT event as interrupted.
func (s *prefixSource) warnIncomplete(w io.Writer) {
	switch {
	case s.truncated:
		fmt.Fprintf(w, "shellshot: recording is truncated (%d bytes valid)\n", s.r.ValidBytes())
	case s.ended && !s.sawExit:
		fmt.Fprintln(w, incompleteNote)
	}
}

// incompleteReason says why the source ended early, for the replay summary:
// "truncated (N bytes valid)" for a torn tail, "incomplete (no exit event)"
// for a clean end without an EXIT event, and "" otherwise.
func (s *prefixSource) incompleteReason() string {
	switch {
	case s.truncated:
		return fmt.Sprintf("truncated (%d bytes valid)", s.r.ValidBytes())
	case s.ended && !s.sawExit:
		return "incomplete (no exit event)"
	}
	return ""
}

// scanSpans reads the recording at path to its end and returns its scenes and
// hidden spans. A torn tail is not an error (the valid prefix is scanned), and
// neither is damage further in: the spans found so far are returned and the
// frame replay reports the damage. A malformed SPAN event ends the list
// early: the spans found so far are returned together with an error wrapping
// event.ErrMalformedSpan.
func scanSpans(path string) ([]spans.Span, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := bin.NewReader(f, bin.ReaderOptions{})
	if err != nil {
		return nil, openError(path, err)
	}
	found, err := spans.Scan(&prefixSource{r: r})
	if err != nil && !errors.Is(err, event.ErrMalformedSpan) {
		err = nil
	}
	return found, err
}

// openFrames opens the recording at path and returns a frame iterator over
// it, sampled at fps and rendered with libghostty, together with the source
// (for the incomplete-recording note) and a function that closes everything.
// boundaries are recording times at which the iterator forces a frame (see
// frames.Options.Boundaries); nil forces none.
func openFrames(path string, fps int, boundaries []time.Duration) (*frames.Iterator, *prefixSource, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := bin.NewReader(f, bin.ReaderOptions{})
	if err != nil {
		f.Close()
		return nil, nil, nil, openError(path, err)
	}
	src := &prefixSource{r: r}
	it, err := frames.New(src, frames.Options{FPS: fps, Factory: ghostty.New, Boundaries: boundaries})
	if err != nil {
		f.Close()
		return nil, nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return it, src, func() error { return errors.Join(it.Close(), f.Close()) }, nil
}

// addSpanFlags registers --no-hints and --show-hidden on cmd, binding them to
// o. verb is what the command does with a hidden span ("render" or "play").
func addSpanFlags(cmd *cobra.Command, o *spanplan.Options, verb string) {
	fl := cmd.Flags()
	fl.BoolVar(&o.IgnoreHints, "no-hints", false, "ignore the speed, idle and hold hints stored in the recording's scenes (hidden spans are still cut)")
	fl.BoolVar(&o.ShowHidden, "show-hidden", false, verb+" hidden spans like the rest of the recording instead of cutting them")
}

// setSpanFlagOverrides records in o which of --speed, --idle-limit and --hold
// were given on cmd's command line: each beats the scene hint of its name
// (flag, then scene hint, then default). A command without --hold never
// reports it.
func setSpanFlagOverrides(cmd *cobra.Command, o *spanplan.Options) {
	fl := cmd.Flags()
	o.FlagSpeed, o.FlagIdleLimit, o.FlagHold = fl.Changed("speed"), fl.Changed("idle-limit"), fl.Changed("hold")
}

// planSpans reads the scenes and hidden spans of the recording at input and
// plans them as o asks. It does not open the recording when neither hints nor
// hidden spans are in play, and it returns the zero plan for a recording
// without spans. A malformed span ends the list early and is noted on stderr.
func planSpans(cmd *cobra.Command, input string, o spanplan.Options) (spanplan.Plan, error) {
	if o.IgnoreHints && o.ShowHidden {
		return spanplan.Plan{}, nil
	}
	found, err := scanSpans(input)
	if errors.Is(err, event.ErrMalformedSpan) {
		fmt.Fprintf(cmd.ErrOrStderr(), "shellshot: %v; scenes and hidden spans after it are ignored\n", err)
	} else if err != nil {
		return spanplan.Plan{}, err
	}
	return spanplan.Build(found, o)
}
