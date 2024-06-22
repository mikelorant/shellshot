package visual

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Terminal struct {
	prog *tea.Program
	opts Options
}

type Model struct {
	buf    *bytes.Buffer
	render Renderer
	fwdKey io.Writer
	queue  Queuer
	msg    string
	sigc   bool
	cols   int
	rows   int
}

type signal int

const (
	defaultCols = 80
	defaultRows = 24

	exit signal = iota
	refresh
)

func New(options ...func(*Options)) *Terminal {
	opts := Options{
		cols: defaultCols,
		rows: defaultRows,
	}

	for _, o := range options {
		o(&opts)
	}

	model := Model{
		buf:    new(bytes.Buffer),
		render: opts.render,
		fwdKey: opts.fwdKey,
		queue:  opts.queue,
		cols:   opts.cols,
		rows:   opts.rows,
	}

	return &Terminal{
		prog: tea.NewProgram(model),
		opts: opts,
	}
}

func (t *Terminal) Start() error {
	go func() {
		for {
			t.opts.wait.Wait()

			t.prog.Send(refresh)
		}
	}()

	go func() {
		for {
			if _, err := t.opts.fwdKey.Write(nil); err != nil {
				var pathError *fs.PathError
				if errors.As(err, &pathError) {
					t.prog.Send(exit)

					break
				}
			}

			time.Sleep(20 * time.Millisecond)
		}
	}()

	if _, err := t.prog.Run(); err != nil {
		return fmt.Errorf("unable to start program: %w", err)
	}

	return nil
}

//nolint:ireturn
func (m Model) Init() tea.Cmd {
	return nil
}

//nolint:ireturn
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.String() == "ctrl+c" && m.sigc:
			return m, tea.Quit
		case msg.String() == "ctrl+c":
			m.sigc = true
		default:
			m.sigc = false
		}

		var key string

		switch len(msg.Runes) {
		case 0:
			key = keys[msg.String()]
		default:
			key = string(msg.Runes)
		}

		m.fwdKey.Write([]byte(key))

	case signal:
		switch msg {
		case exit:
			return m, tea.Quit
		default:
		}
	}

	q := m.queue.Queue()
	if len(q) != 0 {
		m.msg = <-q
	}

	return m, nil
}

func (m Model) View() string {
	term := lipgloss.NewStyle().
		Width(m.cols).
		Height(m.rows).
		BorderStyle(lipgloss.NormalBorder()).
		Render(m.render.Render())

	status := lipgloss.NewStyle().
		Width(m.cols).
		Height(1).
		BorderStyle(lipgloss.NormalBorder()).
		MarginTop(1).
		MarginBottom(1).
		Render(m.msg)

	return lipgloss.JoinVertical(lipgloss.Left, term, status)
}
