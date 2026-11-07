// Package webp writes animated lossless WebP files.
//
// The container is written by this package: RIFF 'WEBP', a VP8X chunk
// (animation flag, alpha flag only if some pixel is not opaque, canvas size),
// an ANIM chunk (transparent black background, loop count) and one ANMF chunk
// per frame. The pixels of each frame are a lossless VP8L bitstream made by
// github.com/HugoSmits86/nativewebp (pure Go, MIT); the package lifts the
// bitstream out of the single-frame file the library writes, the way the apng
// package lifts the IDAT out of image/png.
//
// Frames are deltas. The first frame covers the canvas; each later frame
// holds the bounding rectangle of the pixels that differ from the canvas as
// the previous frames leave it, with "do not blend" and "dispose none", so a
// decoder recomposes every frame exactly. ANMF stores a frame's x and y
// offsets divided by two, so the rectangle starts at the nearest even
// coordinates up and left of the changed pixels. A frame equal to the canvas
// lengthens the previous frame instead of adding one.
//
// Memory tradeoff: the VP8X chunk precedes the frames and the RIFF size
// covers all of them, so the Sink keeps every encoded frame in memory and
// writes the file in Close. Peak memory is the size of the finished file plus
// one frame.
package webp
