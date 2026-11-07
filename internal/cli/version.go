package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the shellshot version; override at build time with
// -ldflags "-X github.com/mikelorant/shellshot/internal/cli.Version=...".
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the shellshot version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), Version)
			return err
		},
	}
}
