//go:build darwin

package pty

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	getTermios = unix.TIOCGETA
	setTermios = unix.TIOCSETA
)

// clearEOTEcho clears the flag that makes the line discipline echo a Ctrl-D
// byte. BSD ttys echo it as "^D" plus backspaces (or as the raw byte once
// ECHOCTL is off), so the only way to silence it is to clear ECHO.
func clearEOTEcho(t *unix.Termios) { t.Lflag &^= unix.ECHO }

// disableEOTEcho clears ECHO on the pty whose master is ptmx. The termios
// ioctl is issued on a freshly opened slave side: on macOS the same ioctl on
// the master discards output the child has written but the master has not
// yet read.
func disableEOTEcho(ptmx *os.File) error {
	conn, err := ptmx.SyscallConn()
	if err != nil {
		return err
	}
	var name [128]byte
	var errno error
	if err := conn.Control(func(fd uintptr) {
		_, _, e := unix.Syscall(unix.SYS_IOCTL, fd, unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0])))
		if e != 0 {
			errno = e
		}
	}); err != nil {
		return err
	}
	if errno != nil {
		return fmt.Errorf("TIOCPTYGNAME: %w", errno)
	}
	n := 0
	for n < len(name) && name[n] != 0 {
		n++
	}
	slave, err := os.OpenFile(string(name[:n]), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return err
	}
	defer slave.Close()
	return updateTermios(int(slave.Fd()))
}
