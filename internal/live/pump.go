package live

import (
	"context"
	"io"
)

// PumpOptions tunes Pump.
type PumpOptions struct {
	// OnEOF, when non-empty, is written to dst once after in returns io.EOF
	// (and nothing else is written afterwards). It is not written when the
	// pump ends by cancellation, a read error or a write error, nor when no
	// byte was forwarded to dst before EOF: an empty input needs no
	// terminator, and an EOT written alone would only be echoed back by the
	// terminal. Passing EOT ({0x04}, Ctrl-D) makes a shell fed from a pipe
	// exit like a user pressing Ctrl-D at an empty prompt. Do not use it when
	// in is a terminal: the user types Ctrl-D themselves.
	OnEOF []byte

	// BeforeOnEOF, when non-nil, is called immediately before OnEOF is
	// written, and only if it is written. It lets the caller prepare dst, for
	// example to stop a terminal echoing the terminator.
	BeforeOnEOF func()
}

// Pump copies in to dst in a goroutine until ctx is done, in returns io.EOF,
// or a read or write fails. It returns a buffered channel that receives at
// most one error (nil on clean EOF or cancellation) and is then closed.
//
// Reading os.Stdin blocks and cannot be interrupted, so on cancellation Pump
// does not wait for the pending Read: the result is delivered immediately and
// the copying goroutine lingers until in unblocks, then exits without writing
// to dst. Use Pump only for the lifetime of the process (typically the whole
// program run), not for repeated short-lived pumping of a blocking reader.
func Pump(ctx context.Context, in io.Reader, dst io.Writer) <-chan error {
	return PumpWith(ctx, in, dst, PumpOptions{})
}

// PumpWith is Pump with options.
func PumpWith(ctx context.Context, in io.Reader, dst io.Writer, opts PumpOptions) <-chan error {
	result := make(chan error, 1)
	copied := make(chan error, 1)

	go func() { copied <- copyUntilDone(ctx, in, dst, opts) }()

	go func() {
		defer close(result)
		select {
		case err := <-copied:
			result <- err
		case <-ctx.Done():
			result <- nil
		}
	}()
	return result
}

// copyUntilDone copies in to dst, returning nil on io.EOF or once ctx is
// done, and the first other error otherwise. On io.EOF it first writes
// opts.OnEOF to dst, unless ctx is done or nothing was forwarded.
func copyUntilDone(ctx context.Context, in io.Reader, dst io.Writer, opts PumpOptions) error {
	buf := make([]byte, 32*1024)
	forwarded := false
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if ctx.Err() != nil {
				return nil
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			forwarded = true
		}
		if rerr == io.EOF {
			if len(opts.OnEOF) == 0 || !forwarded || ctx.Err() != nil {
				return nil
			}
			if opts.BeforeOnEOF != nil {
				opts.BeforeOnEOF()
			}
			_, werr := dst.Write(opts.OnEOF)
			return werr
		}
		if rerr != nil {
			return rerr
		}
	}
}
