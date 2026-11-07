package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/export/format"
	"github.com/michaellorant/shellshot/internal/export/gif"
)

// addGIFFlags registers the GIF options of export on cmd.
func addGIFFlags(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.Int("gif-colors", gif.MaxColors, "gif: largest number of picture colours in the palette (2 to 255; one more entry is transparent)")
	fl.Bool("gif-dither", false, "gif: dither when the animation has more colours than the palette (adds noise, so files grow)")
}

// exportSettings returns the format settings for flags and the GIF options
// given on cmd.
func exportSettings(cmd *cobra.Command, flags exportFlags) (format.Settings, error) {
	fl := cmd.Flags()
	colors, err := fl.GetInt("gif-colors")
	if err != nil {
		return format.Settings{}, err
	}
	if colors < 2 || colors > gif.MaxColors {
		return format.Settings{}, fmt.Errorf("invalid --gif-colors %d (want 2 to %d)", colors, gif.MaxColors)
	}
	dither, err := fl.GetBool("gif-dither")
	if err != nil {
		return format.Settings{}, err
	}
	return format.Settings{Loops: flags.loops, GIFColors: colors, GIFDither: dither}, nil
}
