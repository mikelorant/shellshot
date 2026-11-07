package session

import (
	"errors"
	"io"
	"time"
)

// Defaults applied by Config for zero values.
const (
	DefaultFPS  = 60
	DefaultCols = 80
	DefaultRows = 24
)

// MaxFPS is the highest accepted sampling rate.
const MaxFPS = 1000

// ErrFPS is returned by Run when Config.FPS is above MaxFPS.
var ErrFPS = errors.New("session: fps out of range")

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
	// FPS is the screen sampling rate in frames per second. Zero or negative
	// means DefaultFPS; above MaxFPS Run fails with ErrFPS. The screen is only
	// snapshotted on a tick at which output arrived since the previous
	// snapshot, so a high rate costs nothing while the program is idle.
	FPS int
	// Cols and Rows are the terminal size. Zero or negative values mean
	// DefaultCols and DefaultRows.
	Cols, Rows int
	// Clock returns the current time and is used to stamp frames. Nil means
	// time.Now.
	Clock func() time.Time
	// Ticker creates the sampling ticker. Nil means a time.Ticker.
	Ticker Ticker
}

// Period is the time between samples: one second divided by FPS (DefaultFPS
// when FPS is zero or negative), rounded to the nearest microsecond.
func (c Config) Period() time.Duration {
	fps := c.FPS
	if fps <= 0 {
		fps = DefaultFPS
	}
	return time.Duration(float64(time.Second) / float64(fps)).Round(time.Microsecond)
}

// withDefaults returns c with zero values replaced by their defaults.
func (c Config) withDefaults() Config {
	if c.FPS <= 0 {
		c.FPS = DefaultFPS
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
