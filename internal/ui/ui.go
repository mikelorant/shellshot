package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Terminaler interface {
	Render() string
	Write(bs []byte) error
}

type UI struct {
	model Model
}

type Model struct {
	term Terminaler
	cols int
	rows int
}

type (
	tickMsg time.Time
	exitMsg error
	ptyMsg  []byte
)

const (
	defaultCols = 80
	defaultRows = 24
)

func New() *UI {
	model := Model{
		cols: defaultCols,
		rows: defaultRows,
	}

	return &UI{
		model: model,
	}
}

func (u *UI) Start(term Terminaler, opts ...func(*UI)) error {
	for _, o := range opts {
		o(u)
	}

	u.model.term = term

	prog := tea.NewProgram(u.model)
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("unable to start program: %w", err)
	}

	return nil
}

func (m Model) Init() tea.Cmd {
	return tick
}

//nolint:ireturn
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tick
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			m.term.Write([]byte{13})
		case "backspace":
			m.term.Write([]byte{8})
		}

		m.term.Write([]byte(string(msg.Runes)))
	case exitMsg:
		return m, tea.Quit
	}

	return m, nil
}

func (m Model) View() string {
	return lipgloss.NewStyle().
		Width(m.cols).
		Height(m.rows).
		BorderStyle(lipgloss.NormalBorder()).
		Render(m.term.Render())
}

func WithCols(cols int) func(*UI) {
	return func(u *UI) {
		u.model.cols = cols
	}
}

func WithRows(rows int) func(*UI) {
	return func(u *UI) {
		u.model.rows = rows
	}
}

//nolint:ireturn
func tick() tea.Msg {
	time.Sleep(50 * time.Millisecond)

	return tickMsg{}
}
