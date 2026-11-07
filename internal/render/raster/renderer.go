// Package raster draws terminal snapshots as RGBA images. It is pure Go
// (no cgo): glyphs come from golang.org/x/image/font faces and every cell is
// placed on a fixed grid derived from the font metrics.
package raster

import (
	"fmt"
	"image"
	"image/draw"
	"math"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/michaellorant/shellshot/internal/render/font"
	"github.com/michaellorant/shellshot/internal/render/theme"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Defaults applied by New to zero Options fields.
const (
	DefaultFontSize = 15.0
	DefaultPadding  = 8
	DefaultScale    = 1
)

// Fonts is what the renderer needs from a font backend. *font.Set satisfies
// it; tests may supply a fake.
type Fonts interface {
	// Face returns the face for the style at sizePx pixels (em size).
	Face(bold, italic bool, sizePx float64) (xfont.Face, error)
	// FitFace returns the face that draws r in the style: the primary face
	// at sizePx if it has r, otherwise a fallback face scaled to fit the
	// cells (cells wide, cellW x cellH pixels each) r occupies. ok is false
	// if no font has r.
	FitFace(r rune, bold, italic bool, sizePx float64, cellW, cellH, cells int) (face xfont.Face, ok bool, err error)
	// Metrics returns the whole-pixel cell geometry at sizePx.
	Metrics(sizePx float64) (font.Metrics, error)
}

// Options configures a Renderer. Zero fields take the defaults above; a zero
// Theme means theme.Default().
type Options struct {
	Theme theme.Theme
	// FontSize is the em size in pixels before Scale is applied.
	FontSize float64
	// Padding is the border around the cell grid in pixels before Scale.
	Padding int
	// Scale is an integer factor applied to FontSize and Padding.
	Scale int
	// Cursor selects the cursor shape; the zero value is CursorBlock.
	Cursor CursorStyle
	// AdjustCellWidth and AdjustCellHeight change the font-derived cell size
	// after Scale is applied, like Ghostty's adjust-cell-width and
	// adjust-cell-height. The zero values change nothing.
	AdjustCellWidth, AdjustCellHeight font.CellAdjust
}

// Renderer turns snapshots into images. It is not safe for concurrent use
// (font faces are shared and stateful).
type Renderer struct {
	theme   theme.Theme
	fonts   Fonts
	sizePx  float64       // em size of the faces: metrics.EmPx
	pad     int           // Padding * Scale
	metrics font.Metrics  // cell geometry in final pixels (Scale and adjustments applied)
	glyphDX fixed.Int26_6 // glyph offset inside the cell, see font.Metrics.GlyphOffsetX
	regular xfont.Face
	faces   [4]xfont.Face // indexed by faceIndex(bold, italic)
	cursor  CursorStyle

	maskScratch image.Alpha // reused by contrastFace
}

// New validates o, applies defaults and resolves the cell metrics from f.
func New(f Fonts, o Options) (*Renderer, error) {
	if f == nil {
		return nil, fmt.Errorf("raster: nil fonts")
	}
	if o.Theme == (theme.Theme{}) {
		o.Theme = theme.Default()
	}
	if o.FontSize == 0 {
		o.FontSize = DefaultFontSize
	}
	if o.Padding == 0 {
		o.Padding = DefaultPadding
	}
	if o.Scale == 0 {
		o.Scale = DefaultScale
	}
	if !(o.FontSize > 0) || o.Padding < 0 || o.Scale < 1 || o.Cursor > CursorNone {
		return nil, fmt.Errorf("raster: invalid options: font size %v, padding %d, scale %d, cursor %d", o.FontSize, o.Padding, o.Scale, o.Cursor)
	}
	r := &Renderer{
		theme:  o.Theme,
		fonts:  f,
		pad:    o.Padding * o.Scale,
		cursor: o.Cursor,
	}
	r.sizePx = o.FontSize * float64(o.Scale)
	m, err := f.Metrics(r.sizePx)
	if err != nil {
		return nil, fmt.Errorf("raster: font metrics: %w", err)
	}
	if err := m.CheckAdjust(o.AdjustCellWidth, o.AdjustCellHeight); err != nil {
		return nil, fmt.Errorf("raster: %w", err)
	}
	r.metrics = m.Adjust(o.AdjustCellWidth, o.AdjustCellHeight)
	if r.metrics.CellWidth <= 0 || r.metrics.CellHeight <= 0 {
		return nil, fmt.Errorf("raster: degenerate cell size %dx%d", r.metrics.CellWidth, r.metrics.CellHeight)
	}
	r.glyphDX = fixed.Int26_6(math.Round(r.metrics.GlyphOffsetX() * 64))
	for i := range r.faces {
		if r.faces[i], err = f.Face(i&1 != 0, i&2 != 0, r.sizePx); err != nil {
			return nil, fmt.Errorf("raster: face %d: %w", i, err)
		}
	}
	r.regular = r.faces[0]
	return r, nil
}

// Size returns the canvas size in pixels for a cols x rows screen:
// cols*cellW + 2*pad by rows*cellH + 2*pad.
func (r *Renderer) Size(cols, rows int) (w, h int) {
	return cols*r.metrics.CellWidth + 2*r.pad, rows*r.metrics.CellHeight + 2*r.pad
}

// Render draws s into a NEW image on every call; the caller owns the result.
// It returns an error if s fails snapshot validation.
func (r *Renderer) Render(s snapshot.Snapshot) (*image.RGBA, error) {
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("raster: %w", err)
	}
	w, h := r.Size(s.Cols, s.Rows)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(r.theme.Background), image.Point{}, draw.Src)
	cur := r.cursorCell(s)
	for row := range s.Lines {
		r.drawLine(img, s, row, cur)
	}
	r.drawCursorShape(img, cur)
	return img, nil
}
