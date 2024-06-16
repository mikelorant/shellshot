package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mikelorant/shellshot/internal/message"
	"github.com/mikelorant/shellshot/internal/server"
	"github.com/mikelorant/shellshot/internal/terminal/emulator"
	"github.com/mikelorant/shellshot/internal/terminal/pseudo"
	"github.com/mikelorant/shellshot/internal/terminal/visual"

	cc "github.com/ivanpirog/coloredcobra"
	"github.com/spf13/cobra"
)

type Handlers interface {
	HandlerForHTML() func(w http.ResponseWriter, _ *http.Request)
	HandlerForSVG() func(w http.ResponseWriter, _ *http.Request)
}

type Options struct {
	Shell string
	Cols  int
	Rows  int
	Debug bool
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
		defaultDebug = true
	)

	cmd.Flags().StringVarP(&opts.Shell, "shell", "s", defaultShell, "Shell to use in terminal")
	cmd.Flags().IntVarP(&opts.Cols, "columns", "c", defaultCols, "Terminal columns")
	cmd.Flags().IntVarP(&opts.Rows, "rows", "r", defaultRows, "Terminal rows")
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

	ps := pseudo.New(
		pseudo.WithCommand(opts.Shell),
		pseudo.WithCols(opts.Cols),
		pseudo.WithRows(opts.Rows),
	)

	if err := ps.Start(); err != nil {
		return fmt.Errorf("unable to start pseudo terminal: %w", err)
	}
	defer ps.Close()

	em := emulator.New(
		emulator.WithOutput(ps),
		emulator.WithCols(opts.Cols),
		emulator.WithRows(opts.Rows),
	)

	em.Start()

	if opts.Debug {
		mux := NewMux(em)

		srv := server.New(
			server.WithMux(mux),
			server.WithSender(msg),
		)

		srv.Start()
	}

	go io.Copy(em, ps)

	vs := visual.New(
		visual.WithInput(em),
		visual.WithOutput(ps),
		visual.WithQueue(msg),
		visual.WithCols(opts.Cols),
		visual.WithRows(opts.Rows),
	)

	if err := vs.Start(); err != nil {
		return fmt.Errorf("unable to start visual terminal: %w", err)
	}

	return nil
}

func NewMux(hs Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", hs.HandlerForSVG())
	mux.HandleFunc("/html", hs.HandlerForHTML())

	return mux
}
