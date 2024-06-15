package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mikelorant/shellshot/internal/message"
	"github.com/mikelorant/shellshot/internal/server"
	"github.com/mikelorant/shellshot/internal/terminal"
	"github.com/mikelorant/shellshot/internal/terminal/emulator"
	"github.com/mikelorant/shellshot/internal/terminal/pseudo"
	"github.com/mikelorant/shellshot/internal/terminal/visual"

	cc "github.com/ivanpirog/coloredcobra"
	"github.com/spf13/cobra"
)

type Terminal interface {
	io.ReadWriteCloser
	Start(...func(*terminal.Options)) error
}

type SnapshotTerminal interface {
	Terminal
	HandlerForHTML() func(http.ResponseWriter, *http.Request)
	HandlerForSVG() func(http.ResponseWriter, *http.Request)
}

type Messager interface {
	Send(string)
	Queue() chan string
}

type Server interface {
	Start(...func(*server.Options))
}

type App struct {
	Pseudo   Terminal
	Emulator SnapshotTerminal
	Visual   Terminal
	Messager Messager
	Server   Server

	options Options
}

type Options struct {
	shell string
	cols  int
	rows  int
	debug bool
}

func NewRootCmd(a App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shellshot",
		Short: "shellshot is a shell recorder",
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.Start()
		},
	}

	const (
		defaultShell = "sh"
		defaultCols  = 80
		defaultRows  = 24
		defaultDebug = true
	)

	cmd.Flags().StringVarP(&a.options.shell, "shell", "s", defaultShell, "Shell to use in terminal")
	cmd.Flags().IntVarP(&a.options.cols, "columns", "c", defaultCols, "Terminal columns")
	cmd.Flags().IntVarP(&a.options.rows, "rows", "r", defaultRows, "Terminal rows")
	cmd.Flags().BoolVarP(&a.options.debug, "debug", "d", defaultDebug, "Enable debug server")

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
		Pseudo:   pseudo.New(),
		Emulator: emulator.New(),
		Visual:   visual.New(),
		Messager: message.New(),
		Server:   server.New(),
	}
}

func (a *App) Start() error {
	if err := a.Pseudo.Start(
		terminal.WithCommand(a.options.shell),
		terminal.WithCols(a.options.cols),
		terminal.WithRows(a.options.rows),
	); err != nil {
		return fmt.Errorf("unable to start pseudo terminal: %w", err)
	}
	defer a.Pseudo.Close()

	if err := a.Emulator.Start(
		terminal.WithOutput(a.Pseudo),
		terminal.WithMessager(a.Messager),
		terminal.WithCols(a.options.cols),
		terminal.WithRows(a.options.rows),
	); err != nil {
		return fmt.Errorf("unable to start emulator terminal: %w", err)
	}

	if a.options.debug {
		mux := NewMux(a.Emulator)

		a.Server.Start(
			server.WithMux(mux),
			server.WithSender(a.Messager),
		)
	}

	go io.Copy(a.Emulator, a.Pseudo)

	if err := a.Visual.Start(
		terminal.WithInput(a.Emulator),
		terminal.WithOutput(a.Pseudo),
		terminal.WithMessager(a.Messager),
		terminal.WithCols(a.options.cols),
		terminal.WithRows(a.options.rows),
	); err != nil {
		return fmt.Errorf("unable to start visual terminal: %w", err)
	}

	return nil
}

func NewMux(st SnapshotTerminal) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", st.HandlerForSVG())
	mux.HandleFunc("/html", st.HandlerForHTML())

	return mux
}
