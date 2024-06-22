package cmd

import (
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/mikelorant/shellshot/internal/record"
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
	Shell  string
	Cols   int
	Rows   int
	Debug  bool
	Record bool
	Output string
}

type Message struct {
	queue chan string
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
		defaultShell  = "sh"
		defaultCols   = 80
		defaultRows   = 24
		defaultDebug  = true
		defaultRecord = false
		defaultOutput = "output.cast"
	)

	cmd.Flags().StringVarP(&opts.Shell, "shell", "s", defaultShell, "Shell to use in terminal")
	cmd.Flags().IntVarP(&opts.Cols, "columns", "c", defaultCols, "Terminal columns")
	cmd.Flags().IntVarP(&opts.Rows, "rows", "r", defaultRows, "Terminal rows")
	cmd.Flags().BoolVarP(&opts.Debug, "debug", "d", defaultDebug, "Enable debug server")
	cmd.Flags().BoolVarP(&opts.Record, "record", "", defaultRecord, "Enable recording output")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", defaultOutput, "Recording output file")

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
	msg := newMessage()

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
		emulator.WithForwardResponses(ps),
		emulator.WithCols(opts.Cols),
		emulator.WithRows(opts.Rows),
	).Start()

	if opts.Debug {
		mux := newMux(em)

		srv := server.New(
			server.WithMux(mux),
			server.WithSender(msg),
		)

		srv.Start()
	}

	var tr io.Reader

	switch opts.Record {
	case true:
		tr = io.TeeReader(ps, em)
	default:
		go io.Copy(em, ps)
	}

	rec := record.New(tr,
		record.WithCols(opts.Cols),
		record.WithRows(opts.Rows),
	)

	if opts.Record {
		rec.Start()
	}

	vs := visual.New(
		visual.WithRender(em),
		visual.WithForwardKeys(ps),
		visual.WithQueue(msg),
		visual.WithCols(opts.Cols),
		visual.WithRows(opts.Rows),
		visual.WithWaiter(em),
	)

	if err := vs.Start(); err != nil {
		return fmt.Errorf("unable to start visual terminal: %w", err)
	}

	if err := ps.Close(); err != nil {
		return fmt.Errorf("unable to close pseudo terminal: %w", err)
	}

	if !opts.Record {
		return nil
	}

	ac, err := rec.ToAsciicast()
	if err != nil {
		return fmt.Errorf("unable to export asciicast: %w", err)
	}

	if err := os.WriteFile(opts.Output, ac, 0o644); err != nil {
		return fmt.Errorf("unable to write file: %w", err)
	}

	return nil
}

func newMux(hs Handlers) *http.ServeMux {
	mux := http.DefaultServeMux
	mux.HandleFunc("/", hs.HandlerForSVG())
	mux.HandleFunc("/html", hs.HandlerForHTML())

	return mux
}

func newMessage() *Message {
	return &Message{
		queue: make(chan string, 1),
	}
}

func (m *Message) Send(str string) {
	m.queue <- str
}

func (m *Message) Queue() chan string {
	return m.queue
}
