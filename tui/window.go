package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/models"
)

// WindowID uniquely identifies a chat window
type WindowID int

// ChatWindow represents a single chat view with its own messages and input
type ChatWindow struct {
	ID        WindowID
	Chat      *models.Chat // Which chat is displayed (nil = empty window)
	Messages  MessagesModel
	Input     InputModel
	Focused   bool
	PaneIndex int // 1-based index shown in header
	PaneTotal int

	// Calculated dimensions from layout
	x, y, width, height int
}

// NewChatWindow creates a new empty chat window
func NewChatWindow(id WindowID) *ChatWindow {
	return &ChatWindow{
		ID:       id,
		Messages: NewMessagesModel(),
		Input:    NewInputModel(),
		Focused:  false,
	}
}

// SetBounds sets the window position and size
func (w *ChatWindow) SetBounds(x, y, width, height int) {
	w.x = x
	w.y = y
	w.width = width
	w.height = height

	contentWidth := max(1, width-2)
	// Keep at least one line for messages and one line for input.
	maxInputHeight := max(1, height-1)
	w.Input.SetSize(contentWidth, maxInputHeight)
	inputHeight := w.Input.Height()
	gapHeight := messageInputGapHeight(height, inputHeight)

	// Reserve space for input.
	messagesHeight := height - inputHeight - gapHeight
	if messagesHeight < 1 {
		messagesHeight = 1
	}

	// Update sub-component sizes (subtract padding only)
	w.Messages.SetSize(contentWidth, messagesHeight)
}

// SetChat sets the chat displayed in this window.
// It copies the chat to avoid stale pointer issues when the chat list is reordered.
func (w *ChatWindow) SetChat(chat *models.Chat) {
	if chat != nil {
		chatCopy := *chat
		w.Chat = &chatCopy
		w.Messages.SetChatName(chatCopy.GetDisplayName())
		w.Messages.SetMessages(nil) // Clear stale messages before fresh load
	} else {
		w.Chat = nil
		w.Messages.SetChatName("")
		w.Messages.SetMessages(nil)
	}
}

// syncMessagesSizeToInput resizes the message viewport when the input grows or
// shrinks. SetBounds only runs on layout passes, so without this the viewport
// keeps a stale height and stick-to-bottom / AtBottom() can be wrong.
func (w *ChatWindow) syncMessagesSizeToInput() {
	contentWidth := max(1, w.width-2)
	inputHeight := w.Input.Height()
	gapHeight := messageInputGapHeight(w.height, inputHeight)
	messagesHeight := w.height - inputHeight - gapHeight
	if messagesHeight < 1 {
		messagesHeight = 1
	}
	w.Messages.SetSize(contentWidth, messagesHeight)
}

// Update handles messages for this window
func (w *ChatWindow) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	if w.Focused {
		var cmd tea.Cmd
		w.Input, cmd = w.Input.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		w.syncMessagesSizeToInput()

		switch msg.(type) {
		case tea.KeyMsg:
			// keyboard handled by Input only
		default:
			var cmd2 tea.Cmd
			w.Messages, cmd2 = w.Messages.Update(msg)
			if cmd2 != nil {
				cmds = append(cmds, cmd2)
			}
		}
	} else {
		switch msg := msg.(type) {
		case tea.MouseMsg:
			if mouseIsWheel(msg) {
				if msg.Button == tea.MouseButtonWheelUp {
					w.Messages.ScrollUp()
				} else if msg.Button == tea.MouseButtonWheelDown {
					w.Messages.ScrollDown()
				}
			} else {
				var cmd tea.Cmd
				w.Messages, cmd = w.Messages.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		default:
			var cmd tea.Cmd
			w.Messages, cmd = w.Messages.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return tea.Batch(cmds...)
}

func (w *ChatWindow) FirstImageAttachmentAtContentY(y int) (models.Attachment, bool) {
	viewportY, ok := w.messageViewportY(y)
	if !ok {
		return models.Attachment{}, false
	}
	return w.Messages.FirstImageAttachmentAtViewportY(viewportY)
}

func (w *ChatWindow) LinkAtContentY(y int) (string, bool) {
	viewportY, ok := w.messageViewportY(y)
	if !ok {
		return "", false
	}
	return w.Messages.LinkAtViewportY(viewportY)
}

func (w *ChatWindow) messageViewportY(y int) (int, bool) {
	if y < 0 || w.height <= 0 {
		return 0, false
	}

	inputHeight := w.Input.Height()
	if inputHeight > w.height-1 {
		inputHeight = max(1, w.height-1)
	}
	gapHeight := messageInputGapHeight(w.height, inputHeight)
	messageAreaHeight := w.height - inputHeight - gapHeight
	if messageAreaHeight < 1 || y >= messageAreaHeight {
		return 0, false
	}

	headerRows := 0
	if w.Chat != nil {
		headerRows = 1
	}
	viewportY := y - headerRows
	if viewportY < 0 {
		return 0, false
	}
	return viewportY, true
}

// View renders the window
func (w *ChatWindow) View() string {
	// Pick style based on focus
	var style lipgloss.Style
	if w.Focused {
		style = FocusedWindowStyle
	} else {
		style = UnfocusedWindowStyle
	}

	// Calculate content dimensions (inside padding)
	contentWidth := w.width - 2 // subtract padding
	contentHeight := w.height

	if contentWidth < 1 {
		contentWidth = 1
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Handle empty window
	if w.Chat == nil {
		hint := "Select a chat (Enter in chat list)\nTab to switch focus  F1 for help"
		if w.PaneTotal > 1 {
			hint = w.paneHeaderLine(contentWidth) + "\n\n" + hint
		}
		placeholder := lipgloss.NewStyle().
			Foreground(ColorWindowPlaceholder).
			Align(lipgloss.Center).
			Width(contentWidth).
			Height(contentHeight).
			Render(hint)

		return style.
			Width(w.width).
			Height(w.height).
			Render(placeholder)
	}

	// Calculate heights for messages and input
	inputHeight := w.Input.Height()
	if inputHeight > contentHeight-1 {
		inputHeight = max(1, contentHeight-1)
	}
	gapHeight := messageInputGapHeight(contentHeight, inputHeight)
	messagesHeight := contentHeight - inputHeight - gapHeight
	if messagesHeight < 1 {
		messagesHeight = 1
	}

	// Render messages (includes chat name header when single-pane)
	w.Messages.SetShowHeader(w.PaneTotal <= 1)
	messagesView := w.Messages.View()
	if w.PaneTotal > 1 {
		header := w.paneHeaderLine(contentWidth)
		messagesView = header + "\n" + messagesView
		messagesHeight--
		if messagesHeight < 1 {
			messagesHeight = 1
		}
	}

	// Render input
	inputView := w.Input.View()

	// Emoji autocomplete popup, shown directly above the input. It borrows
	// rows from the message area so the window's total height is unchanged.
	popupView := ""
	if w.Focused {
		// Leave at least one row of messages; the border eats two more.
		maxRows := messagesHeight - 1 - 2
		popupView = w.Input.AutocompleteView(contentWidth, maxRows)
	}
	popupHeight := 0
	if popupView != "" {
		popupHeight = lipgloss.Height(popupView)
		messagesHeight -= popupHeight
		if messagesHeight < 1 {
			messagesHeight = 1
		}
	}

	// Stack messages, (optional) popup, gap, and input.
	sections := []string{
		lipgloss.NewStyle().
			Width(contentWidth).
			Height(messagesHeight).
			MaxHeight(messagesHeight).
			Render(messagesView),
	}
	if popupView != "" {
		sections = append(sections, lipgloss.NewStyle().
			Width(contentWidth).
			MaxHeight(popupHeight).
			Render(popupView))
	}
	sections = append(sections,
		lipgloss.NewStyle().
			Width(contentWidth).
			Height(gapHeight).
			MaxHeight(gapHeight).
			Render(""),
		lipgloss.NewStyle().
			Width(contentWidth).
			MaxHeight(inputHeight).
			Render(inputView),
	)
	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	return style.
		Width(w.width).
		Height(w.height).
		Render(content)
}

func messageInputGapHeight(contentHeight, inputHeight int) int {
	if contentHeight-inputHeight > 1 {
		return 1
	}
	return 0
}

func (w *ChatWindow) paneHeaderLine(width int) string {
	label := fmt.Sprintf("[%d", w.PaneIndex)
	if w.PaneTotal > 0 {
		label += fmt.Sprintf("/%d", w.PaneTotal)
	}
	label += "]"
	if w.Chat != nil {
		name := stripEmojis(w.Chat.GetDisplayName())
		label += " " + name
	}
	if w.Messages.HasUnseenIncoming() {
		label += " ●"
	}
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	if w.Focused {
		headerStyle = headerStyle.Bold(true).Foreground(ColorChatListSelectedBackground)
	}
	line := headerStyle.Render(label)
	if lipgloss.Width(line) > width {
		runes := []rune(label)
		if len(runes) > width-1 {
			label = string(runes[:width-1]) + "…"
		}
		line = headerStyle.Render(label)
	}
	return line
}
