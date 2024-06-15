package visual

import (
	"bytes"
	"fmt"
	"io"

	"github.com/mikelorant/shellshot/internal/terminal"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Visual struct {
	prog  tea.Program
	model Model
}

type Model struct {
	opts terminal.Options
	buf  *bytes.Buffer
	msg  string
	sigc bool
}

type (
	exitMsg error
	ptyMsg  []byte
)

const (
	defaultCols = 80
	defaultRows = 24
	defaultFPS  = 20
)

func New() *Visual {
	opts := terminal.Options{
		Cols: defaultCols,
		Rows: defaultRows,
		FPS:  defaultFPS,
	}

	model := Model{
		opts: opts,
		buf:  new(bytes.Buffer),
	}

	return &Visual{
		model: model,
	}
}

func (v *Visual) Start(opts ...func(*terminal.Options)) error {
	for _, o := range opts {
		o(&v.model.opts)
	}

	prog := tea.NewProgram(v.model)

	go io.Copy(v, v.model.opts.Input)

	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("unable to start program: %w", err)
	}

	return nil
}

func (v *Visual) Read(_ []byte) (n int, err error) {
	return 0, nil
}

func (v *Visual) Write(p []byte) (n int, err error) {
	return v.model.buf.Write(p)
}

func (v *Visual) Close() error {
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
		if msg.String() == "ctrl+c" {
			if m.sigc {
				return m, tea.Quit
			}

			m.sigc = true
		} else {
			m.sigc = false
		}

		switch len(msg.Runes) {
		case 0:
			m.opts.Output.Write([]byte(keys[msg.String()]))
		default:
			m.opts.Output.Write([]byte(string(msg.Runes)))
		}
	case exitMsg:
		return m, tea.Quit
	}

	queue := m.opts.Messager.Queue()
	if len(queue) != 0 {
		m.msg = <-queue
	}

	return m, nil
}

func (m Model) View() string {
	term := lipgloss.NewStyle().
		Width(m.opts.Cols).
		Height(m.opts.Rows).
		BorderStyle(lipgloss.NormalBorder()).
		Render(m.buf.String())

	if m.opts.Messager == nil {
		return term
	}

	status := lipgloss.NewStyle().
		Width(m.opts.Cols).
		Height(1).
		BorderStyle(lipgloss.NormalBorder()).
		MarginTop(1).
		Render(m.msg)

	return lipgloss.JoinVertical(lipgloss.Left, term, status)
}
