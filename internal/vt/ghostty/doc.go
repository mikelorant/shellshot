// Package ghostty implements vt.Terminal on top of libghostty-vt through the
// cgo bindings in go.mitchellh.com/libghostty.
//
// # Linkage
//
// This is the only package in the program that imports cgo. The bindings
// locate libghostty-vt with pkg-config, so building or testing anything that
// imports this package needs the library from scripts/fetch-libghostty.sh,
// which unpacks it into third_party/libghostty. Point pkg-config at it:
//
//	PKG_CONFIG_PATH=$PWD/third_party/libghostty/lib/pkgconfig go test ./...
//
// The Makefile sets that variable. By default the static library is linked,
// so the resulting binary has no runtime dependency on libghostty.
//
// # Styling
//
// Snapshot reports styled runs in Spans as rune-index ranges into the
// trimmed lines. Colours keep their kind: palette indexes (SGR 30-37, 90-97,
// 38;5;n) stay palette entries and 38;2 colours stay RGB, so the renderer
// applies its own theme. Underline of any kind maps to Underline; the
// underline colour and overline are dropped.
//
// Limitation: lines are right-trimmed before spans are computed, and spans are
// clipped to the trimmed text. Styled blank cells after the last visible
// character, such as a coloured background filling the end of a line, are
// therefore not reported.
//
// # Concurrency
//
// A Terminal is not safe for concurrent use; callers serialise access, as the
// vt.Terminal contract requires.
package ghostty
