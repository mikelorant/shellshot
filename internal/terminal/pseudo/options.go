package pseudo

type Options struct {
	Cmd  string
	Cols int
	Rows int
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
