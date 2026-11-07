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
// # Cell widths
//
// Snapshot reports in Widths, sparsely and by rune index like Spans, the runes
// of the trimmed lines that do not occupy exactly one cell. The widths come
// from the cells libghostty lays out, not from a width table in shellshot:
//
//   - A wide character's cell is marked wide and its following spacer cell adds
//     no rune, so the character gets one entry of 2 cells. The spacer left at
//     the end of a row when a wide character wraps to the next row adds no rune
//     either.
//   - A cell holding a cluster (a base codepoint plus combining marks, a
//     variation selector, or a zero-width joiner) contributes one rune per
//     codepoint. The first takes the cell's width (1, or 2 for a wide cell, and
//     only wide cells get an entry); every further codepoint gets an entry of
//     0 cells.
//
// Which codepoints share a cell is libghostty's decision and depends on mode
// 2027 (grapheme clustering), which is off by default. With it off, a
// variation selector after a narrow base stays one cell wide (U+2764 U+FE0F is
// 1 cell + 0), a ZWJ joins the cell of the emoji before it (each emoji of a
// family is its own 2-cell cell holding the emoji and its joiner) and a skin
// tone modifier is a separate 2-cell emoji. With the mode on, VS16 widens the
// base to 2 cells and a skin tone modifier joins its base with 0 cells.
// Widths is nil when every rune of every line is one cell, and entries at or
// past the trimmed length of a line are dropped like clipped spans.
//
// # Concurrency
//
// A Terminal is not safe for concurrent use; callers serialise access, as the
// vt.Terminal contract requires.
package ghostty
