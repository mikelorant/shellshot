package visual

import (
	"io"
)

type Renderer interface {
	Render() string
}

type Queuer interface {
	Queue() chan string
}

type Options struct {
	Cols   int
	Rows   int
	FPS    int
	Input  Renderer
	Output io.Writer
	Queue  Queuer
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

func WithInput(rend Renderer) func(*Options) {
	return func(o *Options) {
		o.Input = rend
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

func WithQueue(queue Queuer) func(*Options) {
	return func(o *Options) {
		o.Queue = queue
	}
}
