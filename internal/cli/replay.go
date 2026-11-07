package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/recording"
	"github.com/michaellorant/shellshot/internal/replay"
)

// newReplayCmd builds "shellshot replay", which plays a recording to stdout
// with its original timing.
func newReplayCmd() *cobra.Command {
	var (
		speed       float64
		clearScreen bool
	)
	cmd := &cobra.Command{
		Use:   "replay FILE [--speed f] [--clear]",
		Short: "Play a recording to stdout with its original timing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("clear") {
				clearScreen = isTerminal(cmd.OutOrStdout())
			}
			return runReplay(cmd, args[0], speed, clearScreen)
		},
	}
	cmd.Flags().Float64Var(&speed, "speed", 1, "playback speed multiplier (2 plays twice as fast)")
	cmd.Flags().BoolVar(&clearScreen, "clear", false, "clear the screen before each frame (default: only when stdout is a terminal)")
	return cmd
}

// runReplay plays the recording at path to cmd's stdout.
func runReplay(cmd *cobra.Command, path string, speed float64, clearScreen bool) (err error) {
	if speed <= 0 {
		return fmt.Errorf("replay: speed must be greater than zero (got %v)", speed)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("replay: close %s: %w", path, cerr)
		}
	}()
	r, err := recording.NewReader(f)
	if err != nil {
		return fmt.Errorf("replay: %s: %w", path, err)
	}
	return replay.Play(cmd.Context(), r, cmd.OutOrStdout(), replay.Options{Speed: speed, Clear: clearScreen})
}
