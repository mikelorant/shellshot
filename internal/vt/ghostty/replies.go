package ghostty

import (
	"fmt"
	"image/color"

	libghostty "go.mitchellh.com/libghostty"

	"github.com/mikelorant/shellshot/internal/render/theme"
	"github.com/mikelorant/shellshot/internal/snapshot"
	"github.com/mikelorant/shellshot/internal/vt"
)

// NewAnswering returns a vt.Factory for terminals that answer the queries a
// program makes of its terminal. Every reply is passed to reply as it is
// produced, in order, during Write: reply runs on the goroutine that calls
// Write, must not block for long and must not call back into the terminal.
// The bytes are only valid for the duration of the call.
//
// The terminal answers with libghostty's own replies: cursor position and
// status reports (CSI 5 n, CSI 6 n), device attributes (DA1, DA2, DA3),
// XTVERSION, mode reports (DECRQM), the kitty keyboard flags query, and the
// default foreground, background, cursor and palette colours (OSC 4, 10, 11
// and 12) of the default theme. Size reports (CSI 14 t, 16 t, 18 t) use the
// terminal's size in cells and the nominal cell size, and the colour scheme
// query (CSI ? 996 n) reports dark. ENQ is not answered.
func NewAnswering(reply func([]byte)) vt.Factory {
	return func(cols, rows int) (vt.Terminal, error) {
		return newTerminal(cols, rows, reply)
	}
}

// replyOptions returns the options that make the terminal answer queries,
// sending replies to reply.
func (t *terminal) replyOptions(reply func([]byte)) []libghostty.TerminalOption {
	return []libghostty.TerminalOption{
		libghostty.WithWritePty(func(_ *libghostty.Terminal, data []byte) { reply(data) }),
		libghostty.WithSizeReport(func(*libghostty.Terminal) (libghostty.SizeReportSize, bool) {
			return libghostty.SizeReportSize{
				Rows:       uint16(t.height),
				Columns:    uint16(t.cols),
				CellWidth:  cellWidthPx,
				CellHeight: cellHeightPx,
			}, true
		}),
		libghostty.WithColorScheme(func(*libghostty.Terminal) (libghostty.ColorScheme, bool) {
			return libghostty.ColorSchemeDark, true
		}),
	}
}

// setReplyColors makes the default theme's colours the terminal's defaults,
// which is what colour queries report. The screen itself still holds symbolic
// colours, so snapshots are unaffected.
func (t *terminal) setReplyColors() error {
	th := theme.Default()
	var palette libghostty.Palette
	for i := range palette {
		palette[i] = rgbOf(th.Resolve(snapshot.Color{Kind: snapshot.ColorPalette, Index: uint8(i)}, theme.Fg))
	}
	fg, bg, cursor := rgbOf(th.Foreground), rgbOf(th.Background), rgbOf(th.Cursor)
	if err := t.term.SetColorForeground(&fg); err != nil {
		return fmt.Errorf("ghostty: set foreground: %w", err)
	}
	if err := t.term.SetColorBackground(&bg); err != nil {
		return fmt.Errorf("ghostty: set background: %w", err)
	}
	if err := t.term.SetColorCursor(&cursor); err != nil {
		return fmt.Errorf("ghostty: set cursor colour: %w", err)
	}
	if err := t.term.SetColorPalette(&palette); err != nil {
		return fmt.Errorf("ghostty: set palette: %w", err)
	}
	return nil
}

func rgbOf(c color.RGBA) libghostty.ColorRGB {
	return libghostty.ColorRGB{R: c.R, G: c.G, B: c.B}
}
