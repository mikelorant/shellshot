//go:build linux

package pty

import (
	"os"

	"golang.org/x/sys/unix"
)

const (
	getTermios = unix.TCGETS
	setTermios = unix.TCSETS
)

// clearEOTEcho clears the flag that makes the line discipline echo a Ctrl-D
// byte as "^D" (only once the child has left canonical mode; canonical mode
// never echoes it). Clearing ECHOCTL rather than ECHO keeps the echo of
// earlier input, which Linux may still be processing asynchronously, intact.
func clearEOTEcho(t *unix.Termios) { t.Lflag &^= unix.ECHOCTL }

// disableEOTEcho clears ECHOCTL on the pty whose master is ptmx.
func disableEOTEcho(ptmx *os.File) error {
	conn, err := ptmx.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	if err := conn.Control(func(fd uintptr) { ioctlErr = updateTermios(int(fd)) }); err != nil {
		return err
	}
	return ioctlErr
}
