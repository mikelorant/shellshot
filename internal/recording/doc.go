// Package recording only documents the layout of its sub-packages; it has no code.
//
//   - bin is the recording file format: the chunk container, varint, codec
//     (none, flate, zstd), head and event encodings, and the Writer and Reader.
//   - frames replays a bin event log through a vt.Factory into snapshot frames
//     for replay and export.
//
// The format is described in docs/research/binary-format.md.
package recording
