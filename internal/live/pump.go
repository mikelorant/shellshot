package live

import (
	"context"
	"io"
)

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
	result := make(chan error, 1)
	copied := make(chan error, 1)

	go func() { copied <- copyUntilDone(ctx, in, dst) }()

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
// done, and the first other error otherwise.
func copyUntilDone(ctx context.Context, in io.Reader, dst io.Writer) error {
	buf := make([]byte, 32*1024)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if ctx.Err() != nil {
				return nil
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}
