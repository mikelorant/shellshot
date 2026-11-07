package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestCaptureFlagsParsing(t *testing.T) {
	var got captureOptions
	cmd := &cobra.Command{
		Use: "x",
		RunE: func(_ *cobra.Command, args []string) error {
			got.Command = args
			return nil
		},
	}
	addCaptureFlags(cmd, &got, 80, 24)
	cmd.SetArgs([]string{"--cols", "40", "--rows", "5", "--interval", "50ms", "--", "ls", "-l", "--cols", "9"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Cols != 40 || got.Rows != 5 || got.Interval != 50*time.Millisecond {
		t.Errorf("options = %+v, want 40x5 at 50ms", got)
	}
	if want := "ls -l --cols 9"; strings.Join(got.Command, " ") != want {
		t.Errorf("command = %q, want %q", got.Command, want)
	}
}
