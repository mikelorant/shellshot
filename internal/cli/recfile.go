package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
	"github.com/michaellorant/shellshot/internal/recording/frames"
	"github.com/michaellorant/shellshot/internal/session"
	"github.com/michaellorant/shellshot/internal/vt/ghostty"
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

// openFrames opens the recording at path and returns a frame iterator over
// it, sampled at fps and rendered with libghostty, together with the source
// (for the incomplete-recording note) and a function that closes everything.
func openFrames(path string, fps int) (*frames.Iterator, *prefixSource, func() error, error) {
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
	it, err := frames.New(src, frames.Options{FPS: fps, Factory: ghostty.New})
	if err != nil {
		f.Close()
		return nil, nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return it, src, func() error { return errors.Join(it.Close(), f.Close()) }, nil
}
