// Package recording reads and writes shellshot recordings: a JSON Lines
// stream with one header line followed by one line per snapshot.Frame.
//
// Header line:
//
//	{"version":1,"cols":80,"rows":24,"command":["ls"],"started":"RFC3339"}
//
// Frame lines:
//
//	{"t":<ms int>,"lines":[...],"cx":0,"cy":0,"cv":true,"title":""}
package recording

import (
	"errors"
	"fmt"
	"time"
)

// Version is the recording format version produced by Writer.
const Version = 1

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
}

// validate checks the header is one this package can read or write.
func (h Header) validate() error {
	if h.Version != Version {
		return fmt.Errorf("%w: %d", ErrVersion, h.Version)
	}
	if h.Cols <= 0 || h.Rows <= 0 {
		return fmt.Errorf("%w: invalid size %dx%d", ErrMalformed, h.Cols, h.Rows)
	}
	return nil
}
