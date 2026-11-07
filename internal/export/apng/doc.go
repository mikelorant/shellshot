// Package apng writes animated PNG files (the APNG chunk container) using only
// the standard library. The export format built on it is called png.
//
// Each frame is encoded by image/png; the encoder lifts the IDAT payload out
// of that output and re-wraps it in APNG chunks (acTL, fcTL, IDAT, fdAT).
// AddFrame writes a full-canvas frame with BLEND_OP_SOURCE, DISPOSE_OP_NONE
// and offsets (0,0). AddFrameRect writes a sub-rectangle frame with chosen
// blend and dispose operations, and AddFrameAuto diffs each image against the
// canvas so far and writes only the changed rectangle (or merges identical
// images into the previous frame's delay). All frames are written as 8-bit non-premultiplied RGBA
// (PNG colour type 6) so they share one IHDR.
//
// SetDefaultImage gives the file a default image that is not part of the
// animation (a poster for viewers that show a single frame).
//
// Memory tradeoff: acTL must precede the first IDAT and carries the frame
// count, which is unknown while frames stream in. The Encoder therefore keeps
// every frame's compressed data in memory and writes the whole file in Close.
// Peak memory is the size of the finished file, plus one frame during
// AddFrame.
package apng
