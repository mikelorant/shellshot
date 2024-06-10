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

type Sheller interface {
	Start(...func(*shell.Shell)) (*os.File, error)
	Write([]byte) error
	WriteString(string) error
}

type Terminaler interface {
	Start(...func(*terminal.Terminal))
	Render() string
	Write(bs []byte) error
}

type UIer interface {
	Start(ui.Terminaler, ...func(*ui.UI)) error
}

type Messager interface {
	Send(string)
	Queue() chan string
}

type App struct {
	Shell    Sheller
	Terminal Terminaler
	UI       UIer
	Msg      Messager
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
		UI:       ui.New(),
		Msg:      message.New(),
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
		terminal.WithMessager(a.Msg),
	)

	a.UI.Start(a.Terminal,
		ui.WithQueue(a.Msg),
	)

	return nil
}
