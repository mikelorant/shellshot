package cmd_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelorant/shellshot/cmd"
)

func TestRoot(t *testing.T) {
	t.Parallel()

	args := []string{"--shell", "bash", "--columns", "80", "--rows", "24", "--debug"}

	start := func(opts cmd.Options) error {
		assert.Equal(t, "bash", opts.Shell, "Shell")
		assert.Equal(t, 24, opts.Winsize.Rows, "Rows")
		assert.Equal(t, 80, opts.Winsize.Cols, "Cols")
		assert.Equal(t, true, opts.Debug, "Debug")

		return nil
	}

	c := cmd.NewRootCmd(start)

	var buf bytes.Buffer

	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)

	err := c.Execute()

	require.NoError(t, err, "Execution error")
}
