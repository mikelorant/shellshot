package record

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mrmarble/termsvg/pkg/asciicast"
)

type Record struct {
	stream io.Reader
	events []asciicast.Event
	opts   Options
}

var errNoEvents = errors.New("no events recorded")

const (
	defaultCols = 80
	defaultRows = 24
)

func New(stream io.Reader, options ...func(*Options)) *Record {
	opts := Options{
		cols: defaultCols,
		rows: defaultRows,
	}

	for _, o := range options {
		o(&opts)
	}

	return &Record{
		stream: stream,
		opts:   opts,
	}
}

func (r *Record) Start() *Record {
	go func() {
		start := time.Now()
		buffer := make([]byte, 32*1024)

		for {
			n, err := r.stream.Read(buffer)
			switch {
			case errors.Is(err, io.EOF):
				break
			default:
			}

			if n == 0 {
				continue
			}

			ts := time.Since(start).Milliseconds()
			event := asciicast.Event{
				Time:      float64(ts) / 1000,
				EventType: asciicast.Output,
				EventData: string(buffer[:n]),
			}

			r.events = append(r.events, event)
		}
	}()

	return r
}

func (r *Record) ToAsciicast() ([]byte, error) {
	if len(r.events) == 0 {
		return nil, errNoEvents
	}

	ac := asciicast.New()

	ac.Header.Width = r.opts.cols
	ac.Header.Height = r.opts.rows
	ac.Header.Duration = r.events[len(r.events)-1].Time
	ac.Events = r.events
	ac.Compress()

	json, err := ac.Marshal()
	if err != nil {
		return nil, fmt.Errorf("unable to marshal events: %w", err)
	}

	return json, nil
}
