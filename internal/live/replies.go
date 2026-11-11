package live

import (
	"io"
	"sync"
)

// MaxPendingReplies is how many bytes of replies Replies holds back for a
// process that is not reading its input. Replies beyond it are dropped.
const MaxPendingReplies = 64 * 1024

// Replies delivers the terminal's answers to a program's queries to the
// program's input without ever blocking the caller. Send only queues the
// bytes; a goroutine of its own writes them to the process, so a program that
// asks and then does not read its input cannot stall the loop that is feeding
// the terminal with its output (it would otherwise deadlock once the input
// buffer of the pty is full while the program waits to write its output).
//
// Replies are written in the order sent. A write error ends delivery: later
// replies are discarded. The zero value is not usable; use NewReplies.
type Replies struct {
	w io.Writer

	mu      sync.Mutex
	pending []byte
	closed  bool
	wake    chan struct{} // capacity 1: a signal that pending or closed changed
	done    chan struct{} // closed when the goroutine has returned
}

// NewReplies starts delivering replies to w. w is written only by Replies'
// goroutine; concurrent writers to the same *os.File (such as typed keys)
// cannot interleave inside one reply.
func NewReplies(w io.Writer) *Replies {
	r := &Replies{w: w, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go r.run()
	return r
}

// Send queues a copy of p for delivery and returns at once. p is not retained.
func (r *Replies) Send(p []byte) {
	r.mu.Lock()
	if !r.closed && len(r.pending)+len(p) <= MaxPendingReplies {
		r.pending = append(r.pending, p...)
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Close discards what is still queued and waits for the goroutine to finish.
// A write in progress must end for Close to return, so close the process
// first (closing the process unblocks a pending write). Close is idempotent.
func (r *Replies) Close() {
	r.mu.Lock()
	r.closed = true
	r.pending = nil
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
	<-r.done
}

func (r *Replies) run() {
	defer close(r.done)
	for range r.wake {
		r.mu.Lock()
		batch, closed := r.pending, r.closed
		r.pending = nil
		r.mu.Unlock()
		if closed {
			return
		}
		if len(batch) > 0 {
			if _, err := r.w.Write(batch); err != nil {
				r.mu.Lock()
				r.closed = true
				r.pending = nil
				r.mu.Unlock()
				return
			}
		}
	}
}
