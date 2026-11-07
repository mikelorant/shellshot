package session

import (
	"io"
	"time"
)

// Defaults applied by Config for zero values.
const (
	DefaultInterval = 100 * time.Millisecond
	DefaultCols     = 80
	DefaultRows     = 24
)

// Process is the running child program as seen by a session. It is satisfied
// by *pty.Process; it is declared here so session does not import pty (or cgo)
// and can be tested with fakes.
type Process interface {
	// Read returns program output and io.EOF once the output is finished.
	io.Reader
	// Resize changes the terminal size seen by the child.
	Resize(cols, rows int) error
	// Wait blocks until the process exits and returns its exit code.
	Wait() (int, error)
	// Close kills the process if still running and releases it. It must be
	// idempotent and must unblock a pending Read and Wait.
	Close() error
}

// Ticker returns a channel that delivers a value every d, and a function that
// releases the ticker. It is the injection point that lets tests drive
// sampling without real time.
type Ticker func(d time.Duration) (ticks <-chan time.Time, stop func())

// Config controls a session. The zero value is usable.
type Config struct {
	// Interval is the sampling period. Zero or negative means DefaultInterval.
	Interval time.Duration
	// Cols and Rows are the terminal size. Zero or negative values mean
	// DefaultCols and DefaultRows.
	Cols, Rows int
	// Clock returns the current time and is used to stamp frames. Nil means
	// time.Now.
	Clock func() time.Time
	// Ticker creates the sampling ticker. Nil means a time.Ticker.
	Ticker Ticker
	// Events, if non-nil, receives the raw process output. See EventSink.
	Events EventSink
}

// withDefaults returns c with zero values replaced by their defaults.
func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = DefaultInterval
	}
	if c.Cols <= 0 {
		c.Cols = DefaultCols
	}
	if c.Rows <= 0 {
		c.Rows = DefaultRows
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	if c.Ticker == nil {
		c.Ticker = realTicker
	}
	return c
}

// realTicker is the default Ticker, backed by time.NewTicker.
func realTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}
