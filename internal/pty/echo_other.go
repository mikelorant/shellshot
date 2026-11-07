//go:build !darwin && !linux

package pty

import "os"

func disableEOTEcho(*os.File) error { return nil }
