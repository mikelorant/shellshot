package cli

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/codec"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

// The terminal engine recorded in the HEAD chunk: libghostty-vt as built from
// the third_party/ghostty submodule, pinned to ghosttyCommit. Update the
// commit together with the submodule pointer (see docs/ARCHITECTURE.md).
const (
	engineName    = "libghostty-vt"
	ghosttyCommit = "66af4c7458db7e19ae7adb44aa7054879be58b6b"
)

// recordedEnv lists the only environment variables written to a recording.
// Everything else (and the working directory and host name) is left out so a
// shared recording does not leak the recorder's machine.
var recordedEnv = []string{"TERM", "COLORTERM", "LANG", "SHELL"}

// defaultTerm is the TERM the child sees when the environment has none (see
// pty.Config).
const defaultTerm = "xterm-256color"

// headInputs is what buildHead needs to describe a recording.
type headInputs struct {
	Cols, Rows int
	// Argv is the command as executed.
	Argv []string
	// Started is the wall-clock start of the recording.
	Started time.Time
	// Getenv reads the recorder's environment.
	Getenv func(string) string
	// SessionID identifies the recording.
	SessionID [16]byte
	// RecordInput sets head.FlagInputRecorded.
	RecordInput bool
}

// buildHead returns the HEAD of a recording described by in. The coalesced
// flag is set by bin.NewWriter when coalescing is on.
func buildHead(in headInputs) head.Head {
	term := in.Getenv("TERM")
	if term == "" {
		term = defaultTerm
	}
	var env []string
	for _, name := range recordedEnv {
		v := in.Getenv(name)
		if name == "TERM" {
			v = term
		}
		if v != "" {
			env = append(env, name+"="+v)
		}
	}
	_, tzOffset := in.Started.Zone()
	h := head.Head{
		Cols:          in.Cols,
		Rows:          in.Rows,
		StartedUnixNS: in.Started.UnixNano(),
		TZOffsetS:     int64(tzOffset),
		Engine:        head.Engine{Name: engineName, Commit: ghosttyCommit},
		Term:          term,
		Argv:          in.Argv,
		Env:           env,
		SessionID:     in.SessionID,
		Recorder:      "shellshot " + Version,
	}
	if in.RecordInput {
		h.Flags |= head.FlagInputRecorded
	}
	return h
}

// newSessionID returns 16 random bytes.
func newSessionID() ([16]byte, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return id, fmt.Errorf("session id: %w", err)
	}
	return id, nil
}

// Names accepted by --codec.
const (
	codecNone  = "none"
	codecFlate = "flate"
	codecZstd  = "zstd"
)

// writerOptions returns the bin.WriterOptions for the --codec and --coalesce
// flag values. A positive coalesce shorter than one tick still coalesces, over
// one tick.
func writerOptions(codecName string, coalesce time.Duration) (bin.WriterOptions, error) {
	var o bin.WriterOptions
	switch codecName {
	case codecNone:
		o.Uncompressed = true
	case codecFlate:
		o.Codec = codec.Flate
	case codecZstd:
		o.Codec = codec.Zstd
	default:
		return o, fmt.Errorf("invalid --codec %q: must be %s, %s or %s", codecName, codecNone, codecFlate, codecZstd)
	}
	if coalesce < 0 {
		return o, fmt.Errorf("invalid --coalesce %v: must not be negative", coalesce)
	}
	if coalesce > 0 {
		o.CoalesceTicks = max(1, uint64(coalesce)/head.DefaultTickNS)
	}
	return o, nil
}
