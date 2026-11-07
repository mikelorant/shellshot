package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/replay"
)

// newReplayCmd builds "shellshot replay", which plays a recording to stdout
// with its original timing, long pauses capped.
func newReplayCmd() *cobra.Command {
	var (
		speed            float64
		idleLimit        time.Duration
		keepLeadingBlank bool
		clearScreen      bool
		colorMode        string
		fps              int
		noCrop           bool
	)
	cmd := &cobra.Command{
		Use:   "replay FILE [--speed f] [--idle-limit d] [--keep-leading-blank] [--fps n] [--clear] [--no-crop] [--color auto|always|never]",
		Short: "Play a recording to stdout with its original timing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("clear") {
				clearScreen = isTerminal(cmd.OutOrStdout())
			}
			color, err := resolveColor(colorMode, isTerminal(cmd.OutOrStdout()), os.Getenv("NO_COLOR"))
			if err != nil {
				return fmt.Errorf("replay: %w", err)
			}
			opts := replay.Options{
				Speed:            speed,
				IdleLimit:        idleLimit,
				TrimLeadingBlank: !keepLeadingBlank,
				Clear:            clearScreen,
				Color:            color,
			}
			if clearScreen && !noCrop {
				opts.Cols, opts.Rows = terminalSize(cmd.OutOrStdout())
			}
			return runReplay(cmd, args[0], fps, opts)
		},
	}
	cmd.Flags().Float64Var(&speed, "speed", 1, "playback speed multiplier (2 plays twice as fast)")
	cmd.Flags().DurationVar(&idleLimit, "idle-limit", defaultIdleLimit, "longest pause between frames, before --speed (0 for no limit)")
	cmd.Flags().BoolVar(&keepLeadingBlank, "keep-leading-blank", false, keepLeadingBlankHelp)
	cmd.Flags().BoolVar(&clearScreen, "clear", false, "clear the screen before each frame (default: only when stdout is a terminal)")
	cmd.Flags().BoolVar(&noCrop, "no-crop", false, "do not crop frames larger than the terminal (they scroll and wrap on every redraw)")
	addFPSFlag(cmd, &fps)
	addColorFlag(cmd, &colorMode)
	return cmd
}

// runReplay plays the recording at path to cmd's stdout. A recording with a
// torn tail plays up to the tear, and one without an EXIT event (its recorder
// was interrupted) plays to the end; either is noted on stderr and is not an
// error.
func runReplay(cmd *cobra.Command, path string, fps int, opts replay.Options) (err error) {
	if opts.IdleLimit < 0 {
		return fmt.Errorf("replay: invalid --idle-limit %v (must not be negative)", opts.IdleLimit)
	}
	if opts.Speed <= 0 {
		return fmt.Errorf("replay: speed must be greater than zero (got %v)", opts.Speed)
	}
	if err := validateFPS(fps); err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	it, src, closeAll, err := openFrames(path, fps, nil)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	defer func() {
		if cerr := closeAll(); cerr != nil && err == nil {
			err = fmt.Errorf("replay: close %s: %w", path, cerr)
		}
	}()
	if h := src.r.Head(); opts.Cols > 0 && opts.Rows > 0 && (h.Cols > opts.Cols || h.Rows > opts.Rows) {
		fmt.Fprintf(cmd.ErrOrStderr(), "shellshot: replay: recording is %dx%d but the terminal is %dx%d: cropping (use --no-crop to disable)\n",
			h.Cols, h.Rows, opts.Cols, opts.Rows)
	}
	if err := replay.Play(cmd.Context(), it, cmd.OutOrStdout(), opts); err != nil {
		return err
	}
	src.warnIncomplete(cmd.ErrOrStderr())
	return nil
}
