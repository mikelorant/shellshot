package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/mikelorant/shellshot/internal/shell"
	"github.com/mikelorant/shellshot/internal/terminal"

	cc "github.com/ivanpirog/coloredcobra"
	"github.com/spf13/cobra"
)

type Sheller interface {
	Start(...func(*shell.Shell)) (*os.File, error)
	Write([]byte) error
	WriteString(string) error
}

type Terminaler interface {
	Start(...func(*terminal.Terminal))
}

type App struct {
	Shell    Sheller
	Terminal Terminaler
}

func NewRootCmd(a App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shellshot",
		Short: "shellshot is a shell recorder",
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.Start()
		},
	}

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
	if err := NewRootCmd(NewApp()).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func NewApp() App {
	return App{
		Shell:    shell.New(),
		Terminal: terminal.New(),
	}
}

func (a *App) Start() error {
	ptmx, err := a.Shell.Start()
	if err != nil {
		return fmt.Errorf("unable to start shell: %w", err)
	}
	defer ptmx.Close()

	a.Terminal.Start(
		terminal.WithPTMX(ptmx),
		terminal.WithDebug(8080),
	)

	if err := a.Shell.WriteString("nyancat\n"); err != nil {
		return fmt.Errorf("unable to start nyancat: %w", err)
	}

	time.Sleep(1 * time.Minute)

	return nil
}
