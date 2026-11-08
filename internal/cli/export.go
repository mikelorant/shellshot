package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/export/format"
	"github.com/mikelorant/shellshot/internal/export/pipeline"
	"github.com/mikelorant/shellshot/internal/export/spanplan"
	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/render/emoji"
	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/render/fontchain"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/render/theme"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// exportFlags holds the values of the export command's flags.
type exportFlags struct {
	output string
	// format is the --format name, empty to infer it from output.
	format   string
	theme    string
	fontSize float64
	scale    int
	padding  int
	// adjustWidth and adjustHeight are the --adjust-cell-width/-height values.
	adjustWidth, adjustHeight string
	speed                     float64
	idleLimit                 time.Duration
	hold                      time.Duration
	loops                     int
	minDelay                  time.Duration
	// keepLeadingBlank is --keep-leading-blank: do not drop a blank first
	// frame, whatever its length.
	keepLeadingBlank bool
	// poster is the --poster value.
	poster string
	cursor string
	fps    int
	// fallbackFonts are --fallback-font files, in the order given.
	fallbackFonts []string
	systemFonts   bool
	// spans holds --no-hints, --show-hidden and which of --speed,
	// --idle-limit and --hold were given (see setSpanFlagOverrides).
	spans spanplan.Options
	// emojiSet is the --emoji-set value ("apple" or "noto") and emojiFont
	// the --emoji-font file, empty for none.
	emojiSet  string
	emojiFont string
	svg       svgFlags
}

// newExportCmd builds "shellshot export", which renders a recording as an
// animated PNG.
func newExportCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:   "export FILE [-o OUT.png] [flags]",
		Short: "Render a recording as an animated PNG",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setSpanFlagOverrides(cmd, &f.spans)
			return runExport(cmd, args[0], f)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.output, "output", "o", "", "output file, written as-is (default: FILE with the extension of the format, .png for png)")
	fl.StringVar(&f.format, "format", "", "output format: "+strings.Join(format.Default().Names(), ", ")+" (default: from the extension of -o, else png)")
	fl.StringVar(&f.theme, "theme", "", "built-in theme name ("+strings.Join(theme.Names(), ", ")+") or a theme JSON file (default: "+theme.Default().Name+")")
	fl.Float64Var(&f.fontSize, "font-size", raster.DefaultFontSize, "font size in pixels, before --scale (12 matches Ghostty font-size = 12 on a 2x display with --scale 2)")
	fl.IntVar(&f.scale, "scale", raster.DefaultScale, "integer scale factor for font size and padding")
	fl.StringVar(&f.adjustWidth, "adjust-cell-width", "0", "change the cell width after --scale, in pixels (-1, 2) or percent (-7%), like Ghostty's adjust-cell-width; the result must be 1 px to 10x the font cell")
	fl.StringVar(&f.adjustHeight, "adjust-cell-height", "0", "change the cell height after --scale, in pixels or percent; text stays vertically centred, like Ghostty's adjust-cell-height; the result must be 1 px to 10x the font cell")
	fl.IntVar(&f.padding, "padding", raster.DefaultPadding, "border around the screen in pixels (at least 1), before --scale")
	fl.Float64Var(&f.speed, "speed", 1, "playback speed multiplier (2 plays twice as fast); when given, replaces the speed of every scene")
	fl.DurationVar(&f.idleLimit, "idle-limit", defaultIdleLimit, "longest pause between frames, before --speed (0 for no limit); when given, replaces the idle limit of every scene")
	fl.DurationVar(&f.hold, "hold", 2*time.Second, "how long the final frame stays on screen; when given, scene holds are ignored (without it a scene hold is added to this one where a scene ends)")
	fl.IntVar(&f.loops, "loops", 0, "number of times the animation plays (0 for forever)")
	fl.DurationVar(&f.minDelay, "min-delay", 20*time.Millisecond, "shortest frame delay; shorter frames are merged into the previous one (0 to keep all)")
	fl.BoolVar(&f.keepLeadingBlank, "keep-leading-blank", false, keepLeadingBlankHelp)
	fl.StringVar(&f.poster, "poster", defaultPoster, "png only: the frame viewers without animation support show (Preview, Quick Look, chat previews): first, last, none or a time such as 12s on the output clock. A poster other than the first frame adds one full frame to the file")
	addFPSFlag(cmd, &f.fps)
	fl.StringVar(&f.cursor, "cursor", "block", "cursor shape: block, bar, underline or none")
	fl.StringArrayVar(&f.fallbackFonts, "fallback-font", nil, "TTF/OTF/TTC `file` for glyphs the built-in fonts lack (repeatable; tried in order, after the built-in fonts)")
	fl.BoolVar(&f.systemFonts, "system-fonts", false, "also use installed CJK and symbol fonts (macOS and Linux font folders); the output then depends on this machine's fonts")
	addSpanFlags(cmd, &f.spans, "render")
	fl.StringVar(&f.emojiSet, "emoji-set", emoji.SetApple.String(), "colour emoji artwork: apple or noto. apple draws Apple Color Emoji, whose artwork belongs to Apple Inc. and is not open source, and takes the few emoji it lacks from Noto. noto draws Noto Color Emoji only (SIL OFL 1.1): the licence-clean choice for exports you share")
	fl.StringVar(&f.emojiFont, "emoji-font", "", "COLR v0 or CBDT/CBLC colour emoji font `file`, tried before the --emoji-set fonts for every emoji")
	addGIFFlags(cmd)
	addSVGFlags(cmd, &f.svg)
	return cmd
}

// keepLeadingBlankHelp is the --keep-leading-blank help of export and replay.
const keepLeadingBlankHelp = "keep a blank screen at the start of the animation"

// defaultIdleLimit is the --idle-limit default of export and replay.
const defaultIdleLimit = 2 * time.Second

// cursorStyles maps the --cursor values to raster cursor styles.
var cursorStyles = map[string]raster.CursorStyle{
	"block":     raster.CursorBlock,
	"bar":       raster.CursorBar,
	"underline": raster.CursorUnderline,
	"none":      raster.CursorNone,
}

// validate checks the flag values and returns the cursor style.
func (f exportFlags) validate() (raster.CursorStyle, error) {
	if _, err := emoji.ParseSet(f.emojiSet); err != nil {
		return 0, fmt.Errorf("invalid --emoji-set %q (want apple or noto)", f.emojiSet)
	}
	cursor, ok := cursorStyles[f.cursor]
	switch {
	case !ok:
		return 0, fmt.Errorf("invalid --cursor %q (want block, bar, underline or none)", f.cursor)
	case !(f.fontSize > 0):
		return 0, fmt.Errorf("invalid --font-size %v (must be greater than zero)", f.fontSize)
	case f.scale < 1:
		return 0, fmt.Errorf("invalid --scale %d (must be at least 1)", f.scale)
	case f.padding < 1:
		return 0, fmt.Errorf("invalid --padding %d (must be at least 1)", f.padding)
	case !(f.speed > 0):
		return 0, fmt.Errorf("invalid --speed %v (must be greater than zero)", f.speed)
	case f.idleLimit < 0:
		return 0, fmt.Errorf("invalid --idle-limit %v (must not be negative)", f.idleLimit)
	case f.hold < 0:
		return 0, fmt.Errorf("invalid --hold %v (must not be negative)", f.hold)
	case f.loops < 0:
		return 0, fmt.Errorf("invalid --loops %d (must not be negative)", f.loops)
	case f.minDelay < 0:
		return 0, fmt.Errorf("invalid --min-delay %v (must not be negative)", f.minDelay)
	}
	if err := validateFPS(f.fps); err != nil {
		return 0, err
	}
	return cursor, nil
}

// resolveTheme returns the theme named by value: empty for the default, a
// built-in theme name, or otherwise the path of a theme JSON file.
func resolveTheme(value string) (theme.Theme, error) {
	if value == "" {
		return theme.Default(), nil
	}
	if t, ok := theme.Lookup(value); ok {
		return t, nil
	}
	f, err := os.Open(value)
	if err != nil {
		return theme.Theme{}, fmt.Errorf("unknown --theme %q: not a built-in theme (%s) and cannot be read as a file: %w",
			value, strings.Join(theme.Names(), ", "), err)
	}
	defer f.Close()
	t, err := theme.Load(f)
	if err != nil {
		return theme.Theme{}, fmt.Errorf("--theme %s: %w", value, err)
	}
	return t, nil
}

// defaultFormat is the format used when --format and the extension of -o do
// not choose one.
const defaultFormat = "png"

// resolveFormat returns the output format: the one named by name, else the
// one that owns the extension of output, else (no -o, or -o without an
// extension) the default. An unknown name or extension is an error that lists
// the supported formats.
func resolveFormat(reg *format.Registry, name, output string) (format.Format, error) {
	supported := "supported: " + strings.Join(reg.Names(), ", ")
	if name != "" {
		f, ok := reg.Lookup(name)
		if !ok {
			return format.Format{}, fmt.Errorf("unknown --format %q (%s)", name, supported)
		}
		return f, nil
	}
	if ext := filepath.Ext(output); ext != "" {
		f, ok := reg.ByExtension(output)
		if !ok {
			return format.Format{}, fmt.Errorf("cannot tell the output format from the extension %q of %s: use --format (%s)", ext, output, supported)
		}
		return f, nil
	}
	f, _ := reg.Lookup(defaultFormat)
	return f, nil
}

// defaultExportPath returns path with its extension replaced by the first
// extension of f (".png" for png). The ".png" extension is the one
// extension operating systems, previews and upload dialogs recognise.
func defaultExportPath(path string, f format.Format) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + f.Extensions[0]
}

// runExport renders the recording at input to flags.output, or to input with
// the default extension of the output format. An explicit output path is used
// as typed.
func runExport(cmd *cobra.Command, input string, flags exportFlags) error {
	cursor, err := flags.validate()
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	adjustWidth, err := font.ParseCellAdjust(flags.adjustWidth)
	if err != nil {
		return fmt.Errorf("export: invalid --adjust-cell-width: %w", err)
	}
	adjustHeight, err := font.ParseCellAdjust(flags.adjustHeight)
	if err != nil {
		return fmt.Errorf("export: invalid --adjust-cell-height: %w", err)
	}
	th, err := resolveTheme(flags.theme)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	// An -o that is the input is refused before its extension is judged.
	output := flags.output
	if output != "" && sameFile(input, output) {
		return fmt.Errorf("export: output %s would overwrite the input recording", output)
	}
	outFormat, err := resolveFormat(format.Default(), flags.format, output)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	svgOpts, err := flags.svg.options(cmd, outFormat)
	if err != nil {
		return err
	}
	poster, err := posterOption(cmd, flags.poster, outFormat)
	if err != nil {
		return err
	}
	if output == "" {
		output = defaultExportPath(input, outFormat)
		if sameFile(input, output) {
			return fmt.Errorf("export: output %s would overwrite the input recording", output)
		}
	}
	settings, err := exportSettings(cmd, flags)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	settings.SVG = svgOpts
	emojiSet, _ := emoji.ParseSet(flags.emojiSet) // validated above
	fonts, err := fontchain.New(fontchain.Options{
		Files:     flags.fallbackFonts,
		System:    flags.systemFonts,
		EmojiSet:  emojiSet,
		EmojiFile: flags.emojiFont,
	})
	if err != nil {
		var emojiErr *fontchain.EmojiFileError
		if errors.As(err, &emojiErr) {
			return fmt.Errorf("export: --emoji-font: %w", err)
		}
		return fmt.Errorf("export: --fallback-font: %w", err)
	}
	renderer, err := raster.New(fonts, raster.Options{
		Theme:    th,
		FontSize: flags.fontSize,
		Padding:  flags.padding,
		Scale:    flags.scale,
		Cursor:   cursor,

		AdjustCellWidth:  adjustWidth,
		AdjustCellHeight: adjustHeight,
	})
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	plan, err := planSpans(cmd, input, flags.spans)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	it, src, closeAll, err := openFrames(input, flags.fps, plan.Boundaries)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	defer closeAll()
	hdr := src.Head()

	var steps int
	opts := pipeline.Options{
		Timeline: timeline.Options{
			Speed:     flags.speed,
			IdleLimit: flags.idleLimit,
			Hold:      flags.hold,
			MinDelay:  flags.minDelay,
			Segments:  plan.Segments,

			TrimLeadingBlank: !flags.keepLeadingBlank,
		},
		Poster:   poster,
		Progress: func(_, total int) { steps = total },
	}
	// frames is the count to report: for a raster format what its sink wrote,
	// which is fewer than the steps when it merged identical images.
	frames := func() int { return steps }
	write := func(f *os.File) error {
		newSink := outFormat.New(settings)
		var sink pipeline.Sink
		opts.NewSink = func(w io.Writer, width, height int) (pipeline.Sink, error) {
			var err error
			sink, err = newSink(w, width, height)
			return sink, err
		}
		frames = func() int {
			if fc, ok := sink.(pipeline.FrameCounter); ok {
				return fc.Frames()
			}
			return steps
		}
		return pipeline.Export(cmd.Context(), fixedSize{src: it, cols: hdr.Cols, rows: hdr.Rows}, hdr.Cols, hdr.Rows, renderer, f, opts)
	}
	if outFormat.NewSteps != nil {
		write = svgWriteFunc(cmd, outFormat, settings, flags, renderer, fonts, fixedSize{src: it, cols: hdr.Cols, rows: hdr.Rows}, hdr.Cols, hdr.Rows, opts.Timeline, &steps)
	}
	size, err := writeFileAtomic(output, write)
	if err != nil {
		return err
	}
	src.warnIncomplete(cmd.ErrOrStderr())
	if err := fonts.EmojiErr(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: some emoji were not drawn: %v\n", err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "wrote %d frames (%.1f KB) to %s\n", frames(), float64(size)/1024, output)
	return nil
}

// sameFile reports whether a and b name the same existing file, or the same
// cleaned path.
func sameFile(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// writeFileAtomic runs write on a temporary file next to path and renames it
// to path when write and the close succeed, so a failed run leaves no
// partial output (and keeps any existing file). It returns the size written.
// An error from write is returned unchanged; file-system errors are wrapped
// with the output path.
func writeFileAtomic(path string, write func(*os.File) error) (size int64, err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return 0, fmt.Errorf("export: create output: %w", err)
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err = write(tmp); err != nil {
		return 0, err
	}
	if err = tmp.Chmod(0o644); err != nil {
		return 0, fmt.Errorf("export: write %s: %w", path, err)
	}
	info, err := tmp.Stat()
	if err != nil {
		return 0, fmt.Errorf("export: write %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return 0, fmt.Errorf("export: write %s: %w", path, err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return 0, fmt.Errorf("export: write %s: %w", path, err)
	}
	return info.Size(), nil
}

// fixedSize passes frames through and fails on the first one whose size
// differs from the recording's initial size: the PNG canvas has a fixed
// size, so export does not support recordings that resize yet.
type fixedSize struct {
	src        pipeline.Source
	cols, rows int
}

func (s fixedSize) Next() (snapshot.Frame, error) {
	f, err := s.src.Next()
	if err != nil {
		return f, err
	}
	if c, r := f.Snapshot.Cols, f.Snapshot.Rows; c != s.cols || r != s.rows {
		return snapshot.Frame{}, fmt.Errorf("recording changes size mid-way (%dx%d -> %dx%d); export does not support resizes yet",
			s.cols, s.rows, c, r)
	}
	return f, nil
}
