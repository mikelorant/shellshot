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
// lines. Colours keep their kind: palette indexes (SGR 30-37, 90-97,
// 38;5;n) stay palette entries and 38;2 colours stay RGB, so the renderer
// applies its own theme. Underline of any kind maps to Underline; the
// underline colour and overline are dropped.
//
// # Trailing blanks
//
// Lines are right-trimmed, except for blank cells that render something. A
// blank cell is kept, as one space, when it has a background colour, is
// inverted, underlined or struck through, and every blank up to the last such
// cell is kept with it, so the line keeps the coloured bar a status line,
// swatch or full-screen background draws to the right of its last character.
// A foreground colour, bold, italic, faint or blink alone show nothing on a
// blank, so blanks with only those, and unstyled blanks, are trimmed. Cells
// cleared by an erase operation (such as CSI K or CSI 2 J while a background
// colour is set) hold only that background colour, so they keep it and
// nothing else: the erase does not copy the foreground or the inverse
// attribute. Spans and Widths are clipped to the kept text.
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
// 2027 (grapheme clustering). libghostty leaves it off, but New turns it on
// as the reset default, as Ghostty and other modern terminals do, so the
// screen layout matches the one the recorded program was written for. With it
// on, a cluster is always one cell: VS16 widens a narrow base to 2 cells, a
// ZWJ sequence (a whole family), a skin tone modifier, a regional indicator
// pair (flag) and a keycap (U+FE0F U+20E3) are all one 2-cell base followed
// by zero-width runes. With the mode off libghostty splits these into several
// wide cells, drops some variation selectors and leaves VS16 sequences narrow.
// A program that switches the mode off (CSI ? 2027 l) gets that layout.
//
// Widths is nil when every rune of every line is one cell, and entries at or
// past the kept length of a line are dropped like clipped spans.
//
// # Concurrency
//
// A Terminal is not safe for concurrent use; callers serialise access, as the
// vt.Terminal contract requires.
package ghostty
