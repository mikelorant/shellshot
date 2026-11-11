package run

import (
	"fmt"
	"strings"
	"time"

	"github.com/mikelorant/shellshot/internal/script"
)

// ErrorKind classifies why the Executor stopped the script.
type ErrorKind uint8

const (
	// WaitTimeout: a wait did not match within its timeout, or a wait with
	// timeout 0s (the assertion) did not match at once.
	WaitTimeout ErrorKind = iota + 1
	// SettleTimeout: the screen did not become quiet within the timeout, for
	// a run's settle or for "wait idle".
	SettleTimeout
	// ChildExited: the child exited when the script needed it: to type or
	// press a key, or to match a wait that the final screen did not satisfy.
	ChildExited
	// KeyEncode: the Keys adapter could not encode a key or text.
	KeyEncode
	// Internal: the Executor met a state that a valid plan cannot produce.
	Internal
	// ForegroundQuery: "wait done" could not find out which process group is
	// in the foreground of the terminal.
	ForegroundQuery
)

// Exit codes of a failed script (docs/research/script-format.md §7.4).
const (
	ExitRuntimeError  = 1 // Internal, KeyEncode and ForegroundQuery
	ExitScriptFailure = 3 // WaitTimeout, SettleTimeout and ChildExited
)

func (k ErrorKind) String() string {
	switch k {
	case WaitTimeout:
		return "WaitTimeout"
	case SettleTimeout:
		return "SettleTimeout"
	case ChildExited:
		return "ChildExited"
	case ForegroundQuery:
		return "ForegroundQuery"
	case KeyEncode:
		return "KeyEncode"
	case Internal:
		return "Internal"
	}
	return fmt.Sprintf("ErrorKind(%d)", uint8(k))
}

// exitCode returns the process exit code of a script failing with kind k.
func (k ErrorKind) exitCode() int {
	switch k {
	case WaitTimeout, SettleTimeout, ChildExited:
		return ExitScriptFailure
	}
	return ExitRuntimeError
}

// ScriptError is the failure of a running script. The host writes Error() to
// stderr, shows the final screen, finalises the recording and exits with
// ExitCode.
type ScriptError struct {
	Kind ErrorKind
	File string
	// Pos is the source position of the failing step (the opener of a text
	// block); zero for the exit sequence.
	Pos script.Pos
	// Scene is the enclosing scene, or "".
	Scene string
	// Hidden is true inside a hidden block.
	Hidden bool
	// Message is the failure without the location, e.g.
	// "wait timed out after 15s waiting for /re/".
	Message  string
	ExitCode int
}

// Error returns "file:line:col: scene "name": message". The file, the
// position and the scene are left out when unknown.
func (e *ScriptError) Error() string {
	var b strings.Builder
	if e.File != "" {
		b.WriteString(e.File)
		b.WriteByte(':')
	}
	if e.Pos.Line > 0 {
		fmt.Fprintf(&b, "%v:", e.Pos)
	}
	if b.Len() > 0 {
		b.WriteByte(' ')
	}
	switch {
	case e.Scene != "" && e.Hidden:
		fmt.Fprintf(&b, "scene %q (hidden): ", e.Scene)
	case e.Scene != "":
		fmt.Fprintf(&b, "scene %q: ", e.Scene)
	case e.Hidden:
		b.WriteString("hidden: ")
	}
	b.WriteString(e.Message)
	return b.String()
}

// fail builds the ScriptError of op.
func (e *Executor) fail(m Meta, kind ErrorKind, format string, a ...any) *ScriptError {
	return &ScriptError{
		Kind: kind, File: e.file, Pos: m.Pos, Scene: m.Scene, Hidden: m.Hidden,
		Message: fmt.Sprintf(format, a...), ExitCode: kind.exitCode(),
	}
}

// timeoutText renders a timeout for messages: "15s", "1m30s", "250ms".
func timeoutText(d time.Duration) string { return d.String() }
