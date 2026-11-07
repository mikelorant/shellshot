package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/replay"
)

// newReplayCmd builds "shellshot replay", which plays a recording to stdout
// with its original timing.
func newReplayCmd() *cobra.Command {
	var (
		speed       float64
		clearScreen bool
		colorMode   string
		fps         int
	)
	cmd := &cobra.Command{
		Use:   "replay FILE [--speed f] [--fps n] [--clear] [--color auto|always|never]",
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
			return runReplay(cmd, args[0], fps, replay.Options{Speed: speed, Clear: clearScreen, Color: color})
		},
	}
	cmd.Flags().Float64Var(&speed, "speed", 1, "playback speed multiplier (2 plays twice as fast)")
	cmd.Flags().BoolVar(&clearScreen, "clear", false, "clear the screen before each frame (default: only when stdout is a terminal)")
	addFPSFlag(cmd, &fps)
	addColorFlag(cmd, &colorMode)
	return cmd
}

// runReplay plays the recording at path to cmd's stdout. A recording with a
// torn tail plays up to the tear, and one without an EXIT event (its recorder
// was interrupted) plays to the end; either is noted on stderr and is not an
// error.
func runReplay(cmd *cobra.Command, path string, fps int, opts replay.Options) (err error) {
	if opts.Speed <= 0 {
		return fmt.Errorf("replay: speed must be greater than zero (got %v)", opts.Speed)
	}
	if err := validateFPS(fps); err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	it, src, closeAll, err := openFrames(path, fps)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	defer func() {
		if cerr := closeAll(); cerr != nil && err == nil {
			err = fmt.Errorf("replay: close %s: %w", path, cerr)
		}
	}()
	if err := replay.Play(cmd.Context(), it, cmd.OutOrStdout(), opts); err != nil {
		return err
	}
	src.warnIncomplete(cmd.ErrOrStderr())
	return nil
}
