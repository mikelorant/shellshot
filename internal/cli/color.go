package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Values accepted by --color.
const (
	colorAuto   = "auto"
	colorAlways = "always"
	colorNever  = "never"
)

// addColorFlag registers --color on cmd, binding it to mode.
func addColorFlag(cmd *cobra.Command, mode *string) {
	cmd.Flags().StringVar(mode, "color", colorAuto, "colour output: auto (terminal and NO_COLOR unset), always or never")
}

// resolveColor decides whether to emit SGR colour. "always" and "never" are
// explicit; "auto" enables colour when output is a terminal and the NO_COLOR
// environment variable is unset or empty. Any other mode is an error.
func resolveColor(mode string, isTTY bool, noColor string) (bool, error) {
	switch mode {
	case colorAlways:
		return true, nil
	case colorNever:
		return false, nil
	case colorAuto:
		return isTTY && noColor == "", nil
	}
	return false, fmt.Errorf("invalid --color %q (want auto, always or never)", mode)
}
