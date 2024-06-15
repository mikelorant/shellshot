package terminal

import (
	"io"
)

type Messager interface {
	Send(string)
	Queue() chan string
}

type Options struct {
	Cmd      string
	Cols     int
	Rows     int
	FPS      int
	Input    io.Reader
	Output   io.Writer
	Messager Messager
}

func WithCommand(cmd string) func(*Options) {
	return func(o *Options) {
		o.Cmd = cmd
	}
}

func WithCols(cols int) func(*Options) {
	return func(o *Options) {
		o.Cols = cols
	}
}

func WithRows(rows int) func(*Options) {
	return func(o *Options) {
		o.Rows = rows
	}
}

func WithInput(r io.Reader) func(*Options) {
	return func(o *Options) {
		o.Input = r
	}
}

func WithOutput(w io.Writer) func(*Options) {
	return func(o *Options) {
		o.Output = w
	}
}

func WithFPS(fps int) func(*Options) {
	return func(o *Options) {
		o.FPS = fps
	}
}

func WithMessager(msger Messager) func(*Options) {
	return func(o *Options) {
		o.Messager = msger
	}
}
