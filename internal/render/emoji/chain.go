package emoji

import (
	"errors"
	"image"
)

// wholeSequencer is implemented by backends that can tell a glyph for the
// whole cluster from the fallback to its first rune.
type wholeSequencer interface {
	// imageExact is Image except that it reports false unless the font has
	// a glyph for the whole cluster (with or without variation selectors).
	imageExact(cluster string, side int) (*image.RGBA, bool)
}

// Chain is an ordered list of backends: Image asks them in turn and returns
// the first image. It is a Backend and safe for concurrent use if its
// members are.
//
// A backend that lacks a cluster passes it on, so a font that covers only
// part of Unicode emoji, or has a glyph id without a bitmap, is completed by
// the next one. Image makes two passes so that a backend that only knows the
// first emoji of a sequence does not shadow a later one that has the whole
// sequence: the first pass takes the first backend with a glyph for the whole
// cluster; if there is none, the second takes the first backend that can
// draw anything for it (its first emoji).
type Chain []Backend

var _ Backend = Chain(nil)
var _ wholeSequencer = Chain(nil)

// Image implements Backend.
func (c Chain) Image(cluster string, side int) (*image.RGBA, bool) {
	if img, ok := c.imageExact(cluster, side); ok {
		return img, true
	}
	for _, b := range c {
		if img, ok := b.Image(cluster, side); ok {
			return img, true
		}
	}
	return nil, false
}

// imageExact asks the members that can answer for a whole cluster.
func (c Chain) imageExact(cluster string, side int) (*image.RGBA, bool) {
	for _, b := range c {
		if w, ok := b.(wholeSequencer); ok {
			if img, ok := w.imageExact(cluster, side); ok {
				return img, true
			}
		}
	}
	return nil, false
}

// LoadErr reports why a backend could not load a font it needs, or nil. Of
// the backends in this package only the lazily loaded default fonts can
// fail (a corrupt embedded asset); the error is reported once, and the
// backend then draws nothing. For a Chain it joins the errors of its members.
func LoadErr(b Backend) error {
	switch b := b.(type) {
	case Chain:
		var errs []error
		for _, m := range b {
			errs = append(errs, LoadErr(m))
		}
		return errors.Join(errs...)
	case interface{ Err() error }:
		return b.Err()
	}
	return nil
}
