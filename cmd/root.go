package cmd

import (
	"fmt"
	"os"

	"github.com/mikelorant/shellshot/internal/message"
	"github.com/mikelorant/shellshot/internal/shell"
	"github.com/mikelorant/shellshot/internal/terminal"
	"github.com/mikelorant/shellshot/internal/ui"

	cc "github.com/ivanpirog/coloredcobra"
	"github.com/spf13/cobra"
)

type Options struct {
	Shell   string
	Winsize Winsize
	Debug   bool
}

type Winsize struct {
	Rows int
	Cols int
}

func NewRootCmd(exec func(Options) error) *cobra.Command {
	var opts Options

	cmd := &cobra.Command{
		Use:   "shellshot",
		Short: "shellshot is a shell recorder",
		RunE: func(_ *cobra.Command, _ []string) error {
			return exec(opts)
		},
	}

	const (
		defaultShell = "sh"
		defaultCols  = 80
		defaultRows  = 24
		defaultDebug = false
	)

	cmd.Flags().StringVarP(&opts.Shell, "shell", "s", defaultShell, "Shell to use in terminal")
	cmd.Flags().IntVarP(&opts.Winsize.Cols, "columns", "c", defaultCols, "Terminal columns")
	cmd.Flags().IntVarP(&opts.Winsize.Rows, "rows", "r", defaultRows, "Terminal rows")
	cmd.Flags().BoolVarP(&opts.Debug, "debug", "d", defaultDebug, "Enable debug server")

	cc.Init(&cc.Config{
		RootCmd:         cmd,
		Headings:        cc.HiGreen + cc.Bold,
		Commands:        cc.HiYellow + cc.Bold,
		Example:         cc.Italic,
		ExecName:        cc.Bold,
		Flags:           cc.Bold,
		NoExtraNewlines: true,
		NoBottomNewline: true,
	})

	return cmd
}

func Execute() {
	if err := NewRootCmd(start).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func start(opts Options) error {
	msg := message.New()

	ptmx, err := shell.New().Start(
		shell.WithShell(opts.Shell),
		shell.WithCols(opts.Winsize.Cols),
		shell.WithRows(opts.Winsize.Rows),
	)
	if err != nil {
		return fmt.Errorf("unable to start shell: %w", err)
	}
	defer ptmx.Close()

	term := terminal.New()
	term.Start(
		terminal.WithPTMX(ptmx),
		terminal.WithDebug(8080),
		terminal.WithMessager(msg),
		terminal.WithCols(opts.Winsize.Cols),
		terminal.WithRows(opts.Winsize.Rows),
	)

	ui.New().Start(term,
		ui.WithQueue(msg),
		ui.WithCols(opts.Winsize.Cols),
		ui.WithRows(opts.Winsize.Rows),
	)

	return nil
}
