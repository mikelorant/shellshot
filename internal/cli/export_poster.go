package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/export/format"
	"github.com/mikelorant/shellshot/internal/export/pipeline"
)

// defaultPoster is the --poster value of a PNG when the flag is not given:
// a viewer that cannot animate shows the end result of the recording.
const defaultPoster = "last"

// posterOption returns the poster for output format of from the --poster
// value. --poster belongs to PNG: given with another format it is an error,
// not silently ignored; without the flag only a PNG gets the default.
func posterOption(cmd *cobra.Command, value string, of format.Format) (pipeline.Poster, error) {
	given := cmd.Flags().Changed("poster")
	if of.Name != "png" {
		if given {
			return pipeline.Poster{}, fmt.Errorf("export: --poster only applies to png output, not %s", of.Name)
		}
		return pipeline.Poster{}, nil
	}
	p, err := pipeline.ParsePoster(value)
	if err != nil {
		return pipeline.Poster{}, fmt.Errorf("export: --poster: %w", err)
	}
	return p, nil
}
