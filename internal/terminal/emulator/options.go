package emulator

import (
	"io"
)

type Options struct {
	cols    int
	rows    int
	fwdResp io.Writer
}

func WithCols(cols int) func(*Options) {
	return func(o *Options) {
		o.cols = cols
	}
}

func WithRows(rows int) func(*Options) {
	return func(o *Options) {
		o.rows = rows
	}
}

func WithForwardResponses(w io.Writer) func(*Options) {
	return func(o *Options) {
		o.fwdResp = w
	}
}
