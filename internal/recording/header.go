// Package recording reads and writes shellshot recordings: a JSON Lines
// stream with one header line followed by one line per snapshot.Frame.
//
// Header line:
//
//	{"version":2,"cols":80,"rows":24,"command":["ls"],"started":"RFC3339"}
//
// Frame lines:
//
//	{"t":<ms int>,"lines":[...],"cx":0,"cy":0,"cv":true,"title":"","spans":[...],"widths":[...]}
//
// "spans" is optional (version 2 only) and present only when the frame has
// styled text. It has one entry per row, each an array of [start,end,"<SGR>"]
// triples whose start and end are rune indexes into that row's line, e.g.
// "spans":[[[0,3,"31"]],[],[]]. Version 1 recordings have no spans.
//
// "widths" is optional (version 2 only) and present only when some rune of
// the frame is not one cell wide. It has one entry per row, each an array of
// [runeIndex,cells] pairs where cells is 2 (wide) or 0 (zero-width), e.g.
// "widths":[[[0,2],[1,2]],[],[]]. Version 1 recordings have no widths.
package recording

import (
	"errors"
	"fmt"
	"time"
)

// Version is the recording format version produced by Writer. Reader also
// accepts version 1, which has no spans.
const Version = 2

// minVersion is the oldest format version Reader accepts.
const minVersion = 1

var (
	// ErrVersion is returned when a header carries an unsupported version.
	ErrVersion = errors.New("recording: unsupported version")
	// ErrMalformed is returned when the stream is not a valid recording.
	ErrMalformed = errors.New("recording: malformed data")
)

// Header describes a recording as a whole.
type Header struct {
	// Version is the format version. NewWriter treats zero as Version.
	Version int
	// Cols and Rows are the screen dimensions of every frame.
	Cols, Rows int
	// Command is the program and arguments that were recorded.
	Command []string
	// Started is when the recording began.
	Started time.Time
}

// headerLine is the on-disk form of Header.
type headerLine struct {
	Version int       `json:"version"`
	Cols    int       `json:"cols"`
	Rows    int       `json:"rows"`
	Command []string  `json:"command"`
	Started time.Time `json:"started"`
}

// frameLine is the on-disk form of snapshot.Frame; T is in milliseconds.
type frameLine struct {
	T     int64    `json:"t"`
	Lines []string `json:"lines"`
	CX    int      `json:"cx"`
	CY    int      `json:"cy"`
	CV    bool     `json:"cv"`
	Title string   `json:"title"`
	// Spans is omitted when the frame has no styled text.
	Spans [][]spanJSON `json:"spans,omitempty"`
	// Widths is omitted when every rune of the frame occupies one cell.
	Widths [][]wideJSON `json:"widths,omitempty"`
}

// validate checks the header is one this package can read.
func (h Header) validate() error {
	if h.Version < minVersion || h.Version > Version {
		return fmt.Errorf("%w: %d", ErrVersion, h.Version)
	}
	if h.Cols <= 0 || h.Rows <= 0 {
		return fmt.Errorf("%w: invalid size %dx%d", ErrMalformed, h.Cols, h.Rows)
	}
	return nil
}
