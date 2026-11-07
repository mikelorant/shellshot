//go:build darwin || linux

package pty

import "golang.org/x/sys/unix"

// updateTermios applies clearEOTEcho to the terminal settings of fd.
func updateTermios(fd int) error {
	t, err := unix.IoctlGetTermios(fd, getTermios)
	if err != nil {
		return err
	}
	clearEOTEcho(t)
	return unix.IoctlSetTermios(fd, setTermios, t)
}
