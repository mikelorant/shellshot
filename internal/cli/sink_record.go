package cli

import (
	"io"

	"github.com/michaellorant/shellshot/internal/recording"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// recordingSink writes every frame it receives to a recording.
type recordingSink struct {
	writer *recording.Writer
	frames int
}

// newRecordingSink writes the recording header h to w and returns a sink that
// appends frames after it.
func newRecordingSink(w io.Writer, h recording.Header) (*recordingSink, error) {
	rw, err := recording.NewWriter(w, h)
	if err != nil {
		return nil, err
	}
	return &recordingSink{writer: rw}, nil
}

// Frame implements session.Sink.
func (s *recordingSink) Frame(f snapshot.Frame) error {
	if err := s.writer.WriteFrame(f); err != nil {
		return err
	}
	s.frames++
	return nil
}
