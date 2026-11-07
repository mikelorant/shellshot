package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/export/format"
	"github.com/mikelorant/shellshot/internal/export/pipeline"
	"github.com/mikelorant/shellshot/internal/export/svg"
	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/render/raster"
)

// svgFlags holds the values of the flags that only SVG output uses.
type svgFlags struct {
	text  string
	frame string
}

// addSVGFlags registers the SVG flags of the export command.
func addSVGFlags(cmd *cobra.Command, f *svgFlags) {
	fl := cmd.Flags()
	fl.StringVar(&f.text, "svg-text", "text", "how svg draws text: text (small, selectable, drawn in the viewer's monospace font) or paths (glyph outlines of the built-in fonts, exact but larger)")
	fl.StringVar(&f.frame, "frame", "", "write one frame as a still instead of the animation (svg only): first, last, or a time on the output clock such as 1.5s")
}

// options validates the SVG flags for output format of, returning the SVG
// options they set. --svg-text and --frame belong to SVG: given with another
// format they are errors, not silently ignored.
func (f svgFlags) options(cmd *cobra.Command, of format.Format) (svg.Options, error) {
	fl := cmd.Flags()
	if of.Name != "svg" {
		for _, name := range []string{"svg-text", "frame"} {
			if fl.Changed(name) {
				return svg.Options{}, fmt.Errorf("export: --%s only applies to svg output, not %s", name, of.Name)
			}
		}
		return svg.Options{}, nil
	}
	var o svg.Options
	var err error
	if o.Text, err = svg.ParseTextMode(f.text); err != nil {
		return svg.Options{}, fmt.Errorf("export: --svg-text: %w", err)
	}
	if f.frame != "" {
		if o.Frame, err = svg.ParseFrame(f.frame); err != nil {
			return svg.Options{}, fmt.Errorf("export: --frame: %w", err)
		}
	}
	return o, nil
}

// svgWriteFunc returns the function that writes the SVG of the recording from
// src to the output file: it reads the timeline steps and hands them to the
// format's StepsWriter. It stores the number of steps it wrote in steps.
func svgWriteFunc(cmd *cobra.Command, of format.Format, settings format.Settings, flags exportFlags, r *raster.Renderer, fonts *font.Set,
	src pipeline.Source, cols, rows int, tl timeline.Options, steps *int) func(*os.File) error {
	return func(w *os.File) error {
		o := settings.SVG
		if o.Text == svg.TextText && (len(flags.fallbackFonts) > 0 || flags.systemFonts) {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: --fallback-font and --system-fonts only change svg output with --svg-text paths; text mode leaves glyphs to the viewer's fonts")
		}
		ts, err := pipeline.ReadSteps(cmd.Context(), src, tl)
		if err != nil {
			return err
		}
		settings.SVG.Geometry = svg.NewGeometry(r, fonts)
		*steps = len(ts)
		if o.Frame.Kind != svg.FrameNone {
			*steps = 1
		}
		if err := of.NewSteps(settings).WriteSteps(w, cols, rows, ts); err != nil {
			return fmt.Errorf("export: %w", err)
		}
		return nil
	}
}
