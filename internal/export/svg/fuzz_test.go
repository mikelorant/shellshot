package svg

import (
	"bytes"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// fuzzRunes is what random lines are made of: ASCII, markup characters, wide
// and combining runes, emoji and sequences, sprite ranges, private use,
// control characters and a rune no font has.
var fuzzRunes = []rune{
	'a', 'Z', ' ', ' ', '0', '~', '<', '>', '&', '"', '\'', '\t', 0x00, 0x07, 0x7f, 0x85,
	'世', '界', 'あ', '한', 'e', 0x0301, 0x0308, 0x200D, 0xFE0F, 0x20E3, 0x1F3FB,
	'😀', '👍', '❤', '🇯', '🇵', '👨', '👩', '#', '*',
	'─', '│', '┌', '╔', '╭', '╱', '█', '▀', '░', '▙', '⣿', '⠁',
	0xE0B0, 0xE0B4, 0xE0A0, 0xF015, 0xFFFE, 0x10FFFE, 0xFFFD, 0xD7FF,
}

// fuzzSnapshot builds a valid snapshot from data: sizes, runes, spans, wide
// markers and the cursor all come from the bytes.
func fuzzSnapshot(data []byte) snapshot.Snapshot {
	next := func() int {
		if len(data) == 0 {
			return 0
		}
		b := data[0]
		data = data[1:]
		return int(b)
	}
	s := snapshot.Snapshot{Cols: 1 + next()%12, Rows: 1 + next()%4}
	for y := 0; y < s.Rows; y++ {
		var line []rune
		for n := next() % 16; n > 0; n-- {
			line = append(line, fuzzRunes[next()%len(fuzzRunes)])
		}
		s.Lines = append(s.Lines, string(line))
		var spans []snapshot.Span
		for i := 0; i < len(line); {
			l := 1 + next()%4
			if i+l > len(line) {
				l = len(line) - i
			}
			st := snapshot.Style{
				Fg:    snapshot.Color{Kind: snapshot.ColorKind(next() % 3), Index: uint8(next()), R: uint8(next()), G: uint8(next()), B: uint8(next())},
				Bg:    snapshot.Color{Kind: snapshot.ColorKind(next() % 3), Index: uint8(next())},
				Attrs: snapshot.Attr(next()) & (snapshot.Bold | snapshot.Faint | snapshot.Italic | snapshot.Underline | snapshot.Inverse | snapshot.Invisible | snapshot.Strike),
			}
			fixColor(&st.Fg)
			fixColor(&st.Bg)
			if next()%2 == 0 && !st.IsZero() {
				spans = append(spans, snapshot.Span{Start: i, End: i + l, Style: st})
			}
			i += l
		}
		s.Spans = append(s.Spans, spans)
		var wides []snapshot.Wide
		for i := range line {
			if next()%5 == 0 {
				wides = append(wides, snapshot.Wide{Rune: i, Cells: uint8(2 * (next() % 2))})
			}
		}
		s.Widths = append(s.Widths, wides)
	}
	s.Cursor = snapshot.Cursor{X: next()%16 - 2, Y: next()%6 - 1, Visible: next()%4 != 0}
	return s
}

// fixColor zeroes the fields a colour of its kind does not use, so that
// equal colours compare equal.
func fixColor(c *snapshot.Color) {
	switch c.Kind {
	case snapshot.ColorDefault:
		*c = snapshot.Color{}
	case snapshot.ColorPalette:
		*c = snapshot.Color{Kind: snapshot.ColorPalette, Index: c.Index}
	default:
		*c = snapshot.Color{Kind: snapshot.ColorRGB, R: c.R, G: c.G, B: c.B}
	}
}

// FuzzWriter checks that any valid snapshots write without panicking, as
// well-formed XML that follows the document rules, in every mode.
func FuzzWriter(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{5, 2, 9, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add([]byte("\x0a\x03\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f"))
	f.Add(bytes.Repeat([]byte{0xff, 0x0f, 0x30, 0x2a}, 20))
	cursors := []raster.CursorStyle{raster.CursorBlock, raster.CursorBar, raster.CursorUnderline, raster.CursorNone}
	f.Fuzz(func(t *testing.T, data []byte) {
		a := fuzzSnapshot(data)
		if len(data) > 8 {
			data = data[8:]
		}
		b := fuzzSnapshot(data)
		b.Cols, b.Rows = a.Cols, a.Rows
		if len(b.Lines) != a.Rows { // the second snapshot must have the first one's size
			b = a
			b.Cursor.X++
		}
		if err := a.Validate(); err != nil {
			t.Fatalf("fuzz snapshot invalid: %v", err)
		}
		_, g := newRenderer(t, raster.Options{Cursor: cursors[len(data)%len(cursors)]})
		steps := []timeline.Step{step(a, 100*time.Millisecond), step(b, 300*time.Millisecond), step(a, time.Second)}
		for _, o := range []Options{
			{Text: TextText}, {Text: TextPaths}, {Emoji: EmojiImage}, {Text: TextPaths, Emoji: EmojiImage}, {Frame: Frame{Kind: FrameAt, At: 200 * time.Millisecond}}, {Loops: 2},
		} {
			o.Geometry = g
			var buf bytes.Buffer
			if err := NewWriter(o).WriteSteps(&buf, a.Cols, a.Rows, steps); err != nil {
				t.Fatalf("options %+v: %v", o, err)
			}
			checkDocument(t, buf.String())
		}
	})
}
