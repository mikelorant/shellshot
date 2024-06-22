package record

type Options struct {
	cols int
	rows int
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
