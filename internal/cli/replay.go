package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/export/spanplan"
	"github.com/mikelorant/shellshot/internal/replay"
)

// newReplayCmd builds "shellshot replay", which plays a recording to stdout
// with its original timing, long pauses capped, hidden spans cut and scene
// hints applied as export does.
func newReplayCmd() *cobra.Command {
	var (
		speed            float64
		idleLimit        time.Duration
		keepLeadingBlank bool
		clearScreen      bool
		colorMode        string
		fps              int
		noCrop           bool
		quiet            bool
		spans            spanplan.Options
	)
	cmd := &cobra.Command{
		Use:   "replay FILE [--speed f] [--idle-limit d] [--keep-leading-blank] [--show-hidden] [--no-hints] [--fps n] [--clear] [--no-crop] [-q] [--color auto|always|never]",
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
			setSpanFlagOverrides(cmd, &spans)
			if clearScreen && !noCrop {
				opts.Cols, opts.Rows = terminalSize(cmd.OutOrStdout())
			}
			return runReplay(cmd, args[0], fps, opts, spans, quiet)
		},
	}
	cmd.Flags().Float64Var(&speed, "speed", 1, "playback speed multiplier (2 plays twice as fast); when given, replaces the speed of every scene")
	cmd.Flags().DurationVar(&idleLimit, "idle-limit", defaultIdleLimit, "longest pause between frames, before --speed (0 for no limit); when given, replaces the idle limit of every scene")
	cmd.Flags().BoolVar(&keepLeadingBlank, "keep-leading-blank", false, keepLeadingBlankHelp)
	cmd.Flags().BoolVar(&clearScreen, "clear", false, "clear the screen before each frame (default: only when stdout is a terminal)")
	cmd.Flags().BoolVar(&noCrop, "no-crop", false, "do not crop frames larger than the terminal (they scroll and wrap on every redraw)")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "do not print the summary shown on stderr after a replay that draws in place")
	addSpanFlags(cmd, &spans, "play")
	addFPSFlag(cmd, &fps)
	addColorFlag(cmd, &colorMode)
	return cmd
}

// runReplay plays the recording at path to cmd's stdout. Like export, it cuts
// hidden spans and applies scene hints as spans asks (opts.Segments is set
// from them). A recording with a
// torn tail plays up to the tear, and one without an EXIT event (its recorder
// was interrupted) plays to the end; either is noted on stderr and is not an
// error.
//
// When the replay draws in place (opts.Clear) and quiet is not set, a summary
// of how it ended goes to stderr, whatever the reason, and replaces the
// incomplete-recording note. Cancelling the replay is a normal end: after the
// summary it exits with exitCancelled and no error line. With a summary, a
// playback failure is reported in the summary and not again by Run.
func runReplay(cmd *cobra.Command, path string, fps int, opts replay.Options, spans spanplan.Options, quiet bool) (err error) {
	if opts.IdleLimit < 0 {
		return fmt.Errorf("replay: invalid --idle-limit %v (must not be negative)", opts.IdleLimit)
	}
	if opts.Speed <= 0 {
		return fmt.Errorf("replay: speed must be greater than zero (got %v)", opts.Speed)
	}
	if err := validateFPS(fps); err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	plan, err := planSpans(cmd, path, spans)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	opts.Segments = plan.Segments
	it, src, closeAll, err := openFrames(path, fps, plan.Boundaries)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	defer func() {
		if cerr := closeAll(); cerr != nil && err == nil {
			err = fmt.Errorf("replay: close %s: %w", path, cerr)
		}
	}()
	h := src.r.Head()
	if opts.Cols > 0 && opts.Rows > 0 && (h.Cols > opts.Cols || h.Rows > opts.Rows) {
		fmt.Fprintf(cmd.ErrOrStderr(), "shellshot: replay: recording is %dx%d but the terminal is %dx%d: cropping (use --no-crop to disable)\n",
			h.Cols, h.Rows, opts.Cols, opts.Rows)
	}
	stats, playErr := replay.PlayWithStats(cmd.Context(), it, cmd.OutOrStdout(), opts)

	summarize := opts.Clear && !quiet
	if summarize {
		s := replaySummary{
			stats:     stats,
			speed:     opts.Speed,
			idleLimit: opts.IdleLimit,
			recCols:   int(h.Cols),
			recRows:   int(h.Rows),
			cols:      opts.Cols,
			rows:      opts.Rows,
			path:      path,
			fileBytes: -1,
		}
		if !stats.Interrupted {
			s.err = playErr
		}
		if playErr == nil {
			s.incomplete = src.incompleteReason()
		}
		if fi, serr := os.Stat(path); serr == nil {
			s.fileBytes = fi.Size()
		}
		writeReplaySummary(cmd.ErrOrStderr(), s)
	}
	switch {
	case stats.Interrupted:
		return ExitError{Code: exitCancelled}
	case playErr != nil && summarize:
		return ExitError{Code: 1}
	case playErr != nil:
		return playErr
	}
	if !summarize {
		src.warnIncomplete(cmd.ErrOrStderr())
	}
	return nil
}
