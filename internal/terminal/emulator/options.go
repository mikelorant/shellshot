package emulator

import (
	"io"
)

type Options struct {
	Cols   int
	Rows   int
	Output io.Writer
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

func WithOutput(w io.Writer) func(*Options) {
	return func(o *Options) {
		o.Output = w
	}
}
