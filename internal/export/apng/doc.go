// Package apng writes animated PNG (APNG) files using only the standard
// library.
//
// Each frame is encoded by image/png; the encoder lifts the IDAT payload out
// of that output and re-wraps it in APNG chunks (acTL, fcTL, IDAT, fdAT).
// Every frame is a full canvas frame with BLEND_OP_SOURCE, DISPOSE_OP_NONE
// and offsets (0,0). All frames are written as 8-bit non-premultiplied RGBA
// (PNG colour type 6) so they share one IHDR.
//
// Memory tradeoff: acTL must precede the first IDAT and carries the frame
// count, which is unknown while frames stream in. The Encoder therefore keeps
// every frame's compressed data in memory and writes the whole file in Close.
// Peak memory is the size of the finished file, plus one frame during
// AddFrame.
package apng
