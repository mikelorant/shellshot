package pseudo

type Options struct {
	cmd  string
	cols int
	rows int
}

func WithCommand(cmd string) func(*Options) {
	return func(o *Options) {
		o.cmd = cmd
	}
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
