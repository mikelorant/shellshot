package gif

import (
	"io"

	"github.com/mikelorant/shellshot/internal/export/pipeline"
)

var (
	_ pipeline.Sink         = (*Sink)(nil)
	_ pipeline.DelayLimiter = (*Sink)(nil)
)

// SinkFactory returns the pipeline.SinkFactory of a GIF with options o.
func SinkFactory(o Options) pipeline.SinkFactory {
	return func(w io.Writer, width, height int) (pipeline.Sink, error) {
		s, err := NewSink(w, width, height, o)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
}
