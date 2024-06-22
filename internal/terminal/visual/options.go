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

type Waiter interface {
	Wait()
}

type Options struct {
	cols   int
	rows   int
	render Renderer
	fwdKey io.Writer
	queue  Queuer
	wait   Waiter
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

func WithRender(render Renderer) func(*Options) {
	return func(o *Options) {
		o.render = render
	}
}

func WithForwardKeys(fk io.Writer) func(*Options) {
	return func(o *Options) {
		o.fwdKey = fk
	}
}

func WithQueue(queue Queuer) func(*Options) {
	return func(o *Options) {
		o.queue = queue
	}
}

func WithWaiter(wait Waiter) func(*Options) {
	return func(o *Options) {
		o.wait = wait
	}
}
