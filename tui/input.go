package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type InputModel struct {
	textarea      textarea.Model
	width         int
	height        int
	minHeight     int
	maxHeight     int
	maxPaneHeight int

	// Cache of inputs to estimateHeight so we can skip the string scan on
	// every keystroke when nothing that affects wrapping changed.
	lastValue  string
	lastWidth  int
	lastHeight int
}

func NewInputModel() InputModel {
	ta := textarea.New()
	ta.Placeholder = ""
	ta.Prompt = " "
	ta.ShowLineNumbers = false
	ta.CharLimit = 10000
	ta.SetWidth(50)
	ta.SetHeight(InputHeight)
	ta.Cursor.SetMode(cursor.CursorStatic)

	// Strip all colors/borders from the textarea
	plain := ta.FocusedStyle
	plain.Base = lipgloss.NewStyle()
	plain.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle = plain

	blurred := ta.BlurredStyle
	blurred.Base = lipgloss.NewStyle()
	blurred.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle = blurred

	m := InputModel{
		textarea:      ta,
		width:         50,
		height:        InputHeight,
		minHeight:     InputHeight,
		maxHeight:     8,
		maxPaneHeight: InputHeight,
	}
	m.reflowHeight()
	return m
}

func (m *InputModel) SetSize(width, maxPaneHeight int) {
	if maxPaneHeight < 1 {
		maxPaneHeight = 1
	}
	if width < 1 {
		width = 1
	}
	m.maxPaneHeight = maxPaneHeight
	m.width = width
	m.textarea.SetWidth(width)
	m.reflowHeight()
}

func (m *InputModel) GetText() string {
	return m.textarea.Value()
}

func (m *InputModel) Clear() {
	m.textarea.Reset()
	// Force reflow - the cached lastValue from before Reset must not win.
	m.lastValue = ""
	m.lastHeight = 0
	m.reflowHeight()
}

func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.reflowHeight()
	return m, cmd
}

func (m InputModel) View() string {
	return m.textarea.View()
}

func (m InputModel) Focused() bool {
	return m.textarea.Focused()
}

func (m *InputModel) Focus() tea.Cmd {
	return m.textarea.Focus()
}

func (m *InputModel) Blur() {
	m.textarea.Blur()
}

func (m InputModel) Height() int {
	return m.height
}

func (m *InputModel) reflowHeight() {
	// Fast path: if neither the value nor the width changed since the last
	// call, the wrap calculation will produce the same answer. Skipping it
	// keeps Update() cheap per keystroke so the Bubble Tea event loop never
	// falls behind the terminal's input buffer.
	value := m.textarea.Value()
	if value == m.lastValue && m.width == m.lastWidth && m.lastHeight != 0 {
		return
	}

	desired := m.estimateHeight()
	limit := m.maxHeight
	if m.maxPaneHeight < limit {
		limit = m.maxPaneHeight
	}
	if limit < 1 {
		limit = 1
	}
	if desired > limit {
		desired = limit
	}
	if desired < 1 {
		desired = 1
	}

	m.lastValue = value
	m.lastWidth = m.width
	m.lastHeight = desired

	if desired != m.height {
		m.height = desired
		m.textarea.SetHeight(desired)
	}
}

func (m *InputModel) estimateHeight() int {
	usableWidth := m.width - lipgloss.Width(m.textarea.Prompt)
	if usableWidth < 1 {
		usableWidth = 1
	}

	lines := 0
	for _, logicalLine := range strings.Split(m.textarea.Value(), "\n") {
		lineWidth := lipgloss.Width(logicalLine)
		visualLines := (lineWidth + usableWidth - 1) / usableWidth
		if visualLines < 1 {
			visualLines = 1
		}
		lines += visualLines
	}
	if lines < m.minHeight {
		lines = m.minHeight
	}
	return lines
}
