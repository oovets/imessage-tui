package tui

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/oovets/imessage-tui/models"
)

type MessagesModel struct {
	viewport        viewport.Model
	messages        []models.Message
	messageKeys     map[string]struct{}
	unseenGUIDs     map[string]struct{}
	chatName        string
	width           int
	height          int
	showTimestamps  bool
	showLineNumbers bool
	showSenderNames bool

	// defaultSenderNames is the app-wide setting this pane falls back to, and
	// senderNamesPinned records that the user answered for this pane by hand.
	// A pinned pane ignores the global toggle until it changes conversation.
	defaultSenderNames bool
	senderNamesPinned  bool

	// Distinct incoming senders seen in this pane, and whether that is more
	// than one. Colouring a name in a one-to-one chat would be decoration;
	// in a group it is the fastest way to see who is talking.
	senders       map[string]struct{}
	colorSenders  bool
	stickToBottom bool
	loading       bool
	showHeader    bool
	lineMessages  []int
	lineLinks     []string

	// Cached render so a single appended message doesn't re-render the whole
	// history. renderedBody is the full viewport content; lastRenderedDay is
	// the day key after the last rendered message; contentRendered is false
	// while the viewport shows a placeholder (empty/loading) rather than a
	// real render.
	renderedBody    string
	lastRenderedDay string
	contentRendered bool
}

func NewMessagesModel() MessagesModel {
	vp := viewport.New(60, 15)
	vp.MouseWheelEnabled = true

	return MessagesModel{
		viewport:           vp,
		messageKeys:        make(map[string]struct{}),
		unseenGUIDs:        make(map[string]struct{}),
		showTimestamps:     true,
		showLineNumbers:    true,
		showSenderNames:    true,
		defaultSenderNames: true,
		stickToBottom:      true,
		showHeader:         true,
	}
}

func (m *MessagesModel) SetMessages(messages []models.Message) {
	prevUnseen := m.unseenGUIDs
	messages = dedupeMessages(messages)
	messages = foldReactionMessages(messages)
	// Always keep the list chronological so the newest message is last and
	// stick-to-bottom points at the most recent entry, regardless of how
	// callers assembled the slice (API, cache, merged sources, ...).
	sort.SliceStable(messages, func(i, j int) bool {
		return messages[i].DateCreated < messages[j].DateCreated
	})
	m.messages = messages
	m.rebuildMessageIndex()
	m.rebuildSenders()
	m.unseenGUIDs = make(map[string]struct{})
	for _, msg := range messages {
		if msg.GUID == "" {
			continue
		}
		if _, ok := prevUnseen[msg.GUID]; ok {
			m.unseenGUIDs[msg.GUID] = struct{}{}
		}
	}
	m.renderContent()
}

// AppendMessage adds a single message to the list, deduplicating by message identity and keeping chronological order.
func (m *MessagesModel) AppendMessage(msg models.Message) {
	if emoji := reactionEmoji(msg); emoji != "" {
		if addReactionToMessages(m.messages, msg.AssociatedMessageGUID, emoji) {
			m.renderContent()
			return
		}
	}
	keys := messageDedupeKeys(msg)
	for _, key := range keys {
		if _, exists := m.messageKeys[key]; exists {
			return
		}
	}
	for _, key := range keys {
		m.messageKeys[key] = struct{}{}
	}

	endAppend := len(m.messages) == 0 || m.messages[len(m.messages)-1].DateCreated <= msg.DateCreated
	if endAppend {
		// Fast path: most WS messages are newest.
		m.messages = append(m.messages, msg)
	} else {
		// Insert sorted by DateCreated.
		pos := sort.Search(len(m.messages), func(i int) bool {
			return m.messages[i].DateCreated > msg.DateCreated
		})
		m.messages = append(m.messages, models.Message{})
		copy(m.messages[pos+1:], m.messages[pos:])
		m.messages[pos] = msg
	}

	// A second distinct sender turns nick colouring on for the whole pane, so
	// the messages already rendered without it have to be drawn again. This
	// happens once per conversation, on the message that makes it a group.
	becameGroup := m.noteSender(msg)

	// Appending at the end can't change any earlier line (line numbers count
	// up, day separators only ever precede the new block), so render just the
	// new message and tack it onto the cached body instead of re-rendering the
	// whole history. Anything else (sorted insert, no prior render) falls back
	// to a full render.
	if endAppend && m.contentRendered && !becameGroup {
		var sb strings.Builder
		i := len(m.messages) - 1
		m.lastRenderedDay = m.renderMessageBlock(&sb, i, msg, m.lastRenderedDay, m.wrapWidth())
		m.renderedBody += sb.String()
		m.viewport.SetContent(m.renderedBody)
		if m.stickToBottom {
			m.viewport.GotoBottom()
		}
		return
	}
	m.renderContent()
}

// RemoveMessageByGUID removes a message if present.
func (m *MessagesModel) RemoveMessageByGUID(guid string) bool {
	if strings.TrimSpace(guid) == "" {
		return false
	}
	for i, msg := range m.messages {
		if msg.GUID != guid {
			continue
		}
		m.messages = append(m.messages[:i], m.messages[i+1:]...)
		m.rebuildMessageIndex()
		m.renderContent()
		return true
	}
	return false
}

func (m *MessagesModel) SetLinkPreview(messageGUID string, preview models.LinkPreview) bool {
	messageGUID = strings.TrimSpace(messageGUID)
	if messageGUID == "" || strings.TrimSpace(preview.URL) == "" {
		return false
	}
	for i := range m.messages {
		if m.messages[i].GUID != messageGUID {
			continue
		}
		m.messages[i].LinkPreviews = upsertLinkPreview(m.messages[i].LinkPreviews, preview)
		m.renderContent()
		return true
	}
	return false
}

func (m *MessagesModel) SetChatName(name string) {
	m.chatName = stripEmojis(name)
}

func (m *MessagesModel) SetSize(width, height int) {
	if m.width == width && m.height == height {
		return
	}
	m.width = width
	m.height = height
	m.viewport.Width = width
	// Reserve 1 line for the chat name header
	m.viewport.Height = height - 1
	m.renderContent()
}

func (m *MessagesModel) SetShowTimestamps(show bool) {
	if m.showTimestamps == show {
		return
	}
	m.showTimestamps = show
	m.renderContent()
}

func (m *MessagesModel) SetShowLineNumbers(show bool) {
	if m.showLineNumbers == show {
		return
	}
	m.showLineNumbers = show
	m.renderContent()
}

// SetShowSenderNames applies the app-wide default to this pane. A pane the
// user has answered for by hand keeps its own answer.
func (m *MessagesModel) SetShowSenderNames(show bool) {
	m.defaultSenderNames = show
	if m.senderNamesPinned {
		return
	}
	// A conversation with several people talking keeps its names either way:
	// that is the case the setting exists for, and it is what makes a Slack
	// channel readable without turning names back on by hand every time.
	m.applySenderNames(show || m.colorSenders)
}

// PinShowSenderNames records an explicit choice for this pane, which then wins
// over both the global default and the several-people rule.
func (m *MessagesModel) PinShowSenderNames(show bool) {
	m.senderNamesPinned = true
	m.applySenderNames(show)
}

// ShowingSenderNames reports what this pane currently does.
func (m *MessagesModel) ShowingSenderNames() bool { return m.showSenderNames }

// ResetSenderNamePin drops the pane's own answer. Called when the pane changes
// conversation: a choice made for a two-person chat should not silently carry
// into the channel opened next.
func (m *MessagesModel) ResetSenderNamePin() {
	m.senderNamesPinned = false
}

func (m *MessagesModel) applySenderNames(show bool) {
	if m.showSenderNames == show {
		return
	}
	m.showSenderNames = show
	m.renderContent()
}

func (m *MessagesModel) MarkIncomingUnseen(guid string) {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return
	}
	if _, exists := m.unseenGUIDs[guid]; exists {
		return
	}
	m.unseenGUIDs[guid] = struct{}{}
	m.renderContent()
}

func (m *MessagesModel) ClearIncomingUnseen() {
	if len(m.unseenGUIDs) == 0 {
		return
	}
	m.unseenGUIDs = make(map[string]struct{})
	m.renderContent()
}

// FirstImageAttachmentByNumber returns the first image attachment for message #N
// where N is 1-based according to the rendered message list.
func (m *MessagesModel) FirstImageAttachmentByNumber(n int) (models.Attachment, bool) {
	if n < 1 || n > len(m.messages) {
		return models.Attachment{}, false
	}
	msg := m.messages[n-1]
	for _, att := range msg.Attachments {
		if isImageAttachment(att) {
			return att, true
		}
	}
	return models.Attachment{}, false
}

// MessageGUIDByNumber returns the GUID of message #N, 1-based against the
// rendered list — the same numbering /img uses.
func (m *MessagesModel) MessageGUIDByNumber(n int) (string, bool) {
	if n < 1 || n > len(m.messages) {
		return "", false
	}
	guid := strings.TrimSpace(m.messages[n-1].GUID)
	return guid, guid != ""
}

func (m *MessagesModel) FirstImageAttachmentAtViewportY(y int) (models.Attachment, bool) {
	n := m.messageNumberAtViewportY(y)
	if n == 0 {
		return models.Attachment{}, false
	}
	return m.FirstImageAttachmentByNumber(n)
}

func (m *MessagesModel) LinkAtViewportY(y int) (string, bool) {
	if y < 0 {
		return "", false
	}
	line := m.viewport.YOffset + y
	if line < 0 || line >= len(m.lineLinks) {
		return "", false
	}
	link := strings.TrimSpace(m.lineLinks[line])
	return link, link != ""
}

func (m *MessagesModel) messageNumberAtViewportY(y int) int {
	if y < 0 {
		return 0
	}
	line := m.viewport.YOffset + y
	if line < 0 || line >= len(m.lineMessages) {
		return 0
	}
	return m.lineMessages[line]
}

func (m *MessagesModel) wrapWidth() int {
	w := m.width
	if w < 1 {
		w = 60
	}
	return w
}

func (m *MessagesModel) renderContent() {
	m.lineMessages = m.lineMessages[:0]
	m.lineLinks = m.lineLinks[:0]
	if len(m.messages) == 0 {
		if m.loading {
			m.viewport.SetContent(lipgloss.NewStyle().Foreground(ColorWindowPlaceholder).Render("Loading messages…"))
		} else {
			m.viewport.SetContent("(No messages yet)")
		}
		m.renderedBody = ""
		m.lastRenderedDay = ""
		m.contentRendered = false
		return
	}

	wrapWidth := m.wrapWidth()
	var sb strings.Builder
	lastDay := ""
	for i, msg := range m.messages {
		lastDay = m.renderMessageBlock(&sb, i, msg, lastDay, wrapWidth)
	}

	m.renderedBody = sb.String()
	m.lastRenderedDay = lastDay
	m.contentRendered = true
	m.viewport.SetContent(m.renderedBody)
	if m.stickToBottom {
		m.viewport.GotoBottom()
	}
}

// renderMessageBlock renders message i (whose predecessor ended on day lastDay)
// into sb, appending one entry per visual line to the line-index slices, and
// returns the day key the next message should compare against.
func (m *MessagesModel) renderMessageBlock(sb *strings.Builder, i int, msg models.Message, lastDay string, wrapWidth int) string {
	msgTime := msg.ParsedTime()
	dayKey := msgTime.Format("2006-01-02")
	if dayKey != lastDay {
		if lastDay != "" {
			sb.WriteString("\n")
			m.lineMessages = append(m.lineMessages, 0)
			m.lineLinks = append(m.lineLinks, "")
		}
		sep := "── " + formatDateSeparator(msgTime) + " ──"
		sepStyle := lipgloss.NewStyle().
			Foreground(ColorWindowPlaceholder).
			Width(wrapWidth).
			Align(lipgloss.Center)
		sb.WriteString(sepStyle.Render(sep))
		sb.WriteString("\n")
		m.lineMessages = append(m.lineMessages, 0)
		m.lineLinks = append(m.lineLinks, "")
		lastDay = dayKey
	}

	timeStr := msgTime.Format("15:04")

	sender := senderName(msg)
	if m.colorSenders && !msg.IsFromMe {
		sender = nickStyle(sender).Render(sender)
	}

	prefix := ""
	if m.showTimestamps {
		prefix = formatMessageTimestamp(timeStr)
	}

	previews := linkPreviewsForMessage(msg, 2)
	previewLinks := make([]string, 0, len(previews))
	for _, preview := range previews {
		previewLinks = append(previewLinks, preview.URL)
	}

	bodyLines := splitNonEmptyLines(stripSupportedMediaLinks(msg.Text, previewLinks))
	bodyLineLinks := make([]string, len(bodyLines))
	if attLabel := attachmentLabel(msg, len(previews) > 0); attLabel != "" {
		if len(bodyLines) == 0 {
			bodyLines = append(bodyLines, attLabel)
			bodyLineLinks = append(bodyLineLinks, "")
		} else {
			bodyLines[len(bodyLines)-1] += " " + attLabel
		}
	}
	reactionLabel := formatReactionCounts(msg.ReactionCounts)
	for _, preview := range previews {
		label := linkPreviewLabel(preview)
		if label == "" {
			continue
		}
		bodyLines = append(bodyLines, label)
		bodyLineLinks = append(bodyLineLinks, preview.URL)
	}
	if msg.Pending {
		if len(bodyLines) == 0 {
			bodyLines = append(bodyLines, "…")
			bodyLineLinks = append(bodyLineLinks, "")
		} else {
			bodyLines[0] = "… " + bodyLines[0]
		}
	}

	lineNum := ""
	if m.showLineNumbers {
		lineNum = fmt.Sprintf("#%d ", i+1)
	}
	fullLines, fullLineLinks := messageRenderLines(prefix, lineNum, sender, bodyLines, bodyLineLinks, m.showSenderNames)
	if reactionLabel != "" {
		fullLines = append(fullLines, "  "+reactionLabel)
		fullLineLinks = append(fullLineLinks, "")
	}

	if msg.IsFromMe {
		msgStyle := MyMessageStyle
		if msg.Pending {
			msgStyle = msgStyle.Faint(true)
		}
		reopen := styleOpenSequence(msgStyle)
		wroteLine := false
		for li, rawLine := range fullLines {
			for _, line := range messageWrappedLines(rawLine, wrapWidth, fullLineLinks[li] == "") {
				if wroteLine {
					sb.WriteString("\n")
				}
				content := strings.TrimRight(line, " ")
				if padLen := wrapWidth - lipgloss.Width(content); padLen > 0 {
					sb.WriteString(strings.Repeat(" ", padLen))
				}
				sb.WriteString(msgStyle.Render(reopenAfterResets(content, reopen)))
				m.lineMessages = append(m.lineMessages, i+1)
				m.lineLinks = append(m.lineLinks, fullLineLinks[li])
				wroteLine = true
			}
		}
		sb.WriteString("\n")
	} else {
		style := TheirMessageStyle
		if _, unseen := m.unseenGUIDs[msg.GUID]; unseen {
			style = style.Reverse(true)
		}
		reopen := styleOpenSequence(style)
		wroteLine := false
		for li, rawLine := range fullLines {
			for _, line := range messageWrappedLines(rawLine, wrapWidth, fullLineLinks[li] == "") {
				if wroteLine {
					sb.WriteString("\n")
				}
				sb.WriteString(style.Width(wrapWidth).Render(reopenAfterResets(line, reopen)))
				m.lineMessages = append(m.lineMessages, i+1)
				m.lineLinks = append(m.lineLinks, fullLineLinks[li])
				wroteLine = true
			}
		}
		sb.WriteString("\n")
	}

	return lastDay
}

func attachmentLabel(msg models.Message, hideGeneric bool) string {
	if len(msg.Attachments) == 0 {
		return ""
	}
	if hideGeneric {
		return ""
	}
	images := 0
	others := 0
	var firstName string
	for _, att := range msg.Attachments {
		if isImageAttachment(att) {
			images++
			if firstName == "" {
				firstName = strings.TrimSpace(att.FileName)
			}
		} else {
			others++
			if firstName == "" {
				firstName = strings.TrimSpace(att.FileName)
			}
		}
	}
	if images > 0 && others == 0 {
		if images == 1 && firstName != "" {
			return "[IMG: " + truncatePreview(firstName, 24) + "]"
		}
		return fmt.Sprintf("[%d images]", images)
	}
	total := len(msg.Attachments)
	if total == 1 && firstName != "" {
		return "[" + truncatePreview(firstName, 24) + "]"
	}
	return fmt.Sprintf("[%d attachments]", total)
}

func splitNonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

const continuationMarker = "↳ "

func messageWrappedLines(rawLine string, width int, markContinuations bool) []string {
	if width < 1 {
		width = 1
	}
	wrapWidth := width
	if markContinuations {
		wrapWidth = width - lipgloss.Width(continuationMarker)
		if wrapWidth < 1 {
			wrapWidth = width
		}
	}
	wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(rawLine)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
		if markContinuations && i > 0 {
			lines[i] = continuationMarker + strings.TrimLeft(lines[i], " ")
		}
	}
	return lines
}

func formatMessageTimestamp(timeStr string) string {
	timeStr = strings.TrimSpace(timeStr)
	if timeStr == "" {
		return ""
	}
	// No Faint(): the timestamp color is already muted, and faint on top of it
	// washes out to near-invisible on a light-background terminal.
	return TimestampStyle.Render(timeStr)
}

// ansiReset is the sequence lipgloss emits to close a styled span.
const ansiReset = "\x1b[0m"

// styleOpenSequence returns the escape sequence style emits ahead of its
// content, or "" for a style that paints nothing.
func styleOpenSequence(style lipgloss.Style) string {
	rendered := style.Render("x")
	if i := strings.IndexByte(rendered, 'x'); i > 0 {
		return rendered[:i]
	}
	return ""
}

// reopenAfterResets re-arms open after every reset embedded in s, so a
// pre-styled fragment inside a line (a link-preview badge, a muted timestamp)
// does not drop the remainder of that line back to the terminal default.
// Without this the outgoing blue stopped at the first such fragment and only
// reappeared on wrapped continuation lines, which carry no prefix.
func reopenAfterResets(s, open string) string {
	if open == "" || !strings.Contains(s, ansiReset) {
		return s
	}
	return strings.ReplaceAll(s, ansiReset, ansiReset+open)
}

// rebuildSenders recounts the distinct incoming senders in the pane.
func (m *MessagesModel) rebuildSenders() {
	m.senders = make(map[string]struct{})
	m.colorSenders = false
	if !m.senderNamesPinned {
		// Back to the default before counting, so a pane that used to hold a
		// group chat does not keep its names after loading a two-person one.
		m.showSenderNames = m.defaultSenderNames
	}
	for _, msg := range m.messages {
		m.noteSender(msg)
	}
}

// noteSender records an incoming sender and reports whether the pane just
// became a multi-party conversation, which is when colouring starts.
func (m *MessagesModel) noteSender(msg models.Message) bool {
	if msg.IsFromMe {
		return false
	}
	if m.senders == nil {
		m.senders = make(map[string]struct{})
	}
	name := strings.ToLower(strings.TrimSpace(senderName(msg)))
	if name == "" {
		return false
	}
	before := m.colorSenders
	m.senders[name] = struct{}{}
	m.colorSenders = len(m.senders) > 1
	if m.colorSenders && !m.senderNamesPinned {
		// Several people talking: show who, unless the user answered otherwise
		// for this pane. Callers re-render, so this only sets the flag.
		m.showSenderNames = true
	}
	return m.colorSenders != before
}

// senderName is who a message is from, as shown on its first line.
func senderName(msg models.Message) string {
	switch {
	case msg.IsFromMe:
		return "You"
	case msg.Handle != nil && msg.Handle.DisplayName != "":
		return stripEmojis(msg.Handle.DisplayName)
	case msg.Handle != nil:
		return msg.Handle.Address
	default:
		return "Unknown"
	}
}

// nickStyle picks a stable colour for a name.
//
// Stable across restarts and across panes showing the same conversation, which
// rules out anything derived from map iteration or arrival order: the same
// person must keep the same colour or the colour tells you nothing. Matching is
// case-insensitive so a handle that arrives capitalised differently does not
// become a second person.
func nickStyle(name string) lipgloss.Style {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	return lipgloss.NewStyle().Foreground(NickPalette[int(hash.Sum32()%uint32(len(NickPalette)))])
}

func messageRenderLines(prefix, lineNum, sender string, bodyLines, bodyLineLinks []string, showSenderNames bool) ([]string, []string) {
	if len(bodyLines) == 0 {
		if showSenderNames {
			return []string{fmt.Sprintf("%s%s%s:", prefix, lineNum, sender)}, []string{""}
		}
		return []string{prefix + lineNum}, []string{""}
	}

	lines := make([]string, 0, len(bodyLines))
	links := make([]string, 0, len(bodyLines))
	if showSenderNames {
		first := fmt.Sprintf("%s%s%s:", prefix, lineNum, sender)
		if bodyLines[0] != "" {
			first += " " + bodyLines[0]
		}
		lines = append(lines, first)
		links = append(links, bodyLineLinks[0])
		for i := 1; i < len(bodyLines); i++ {
			lines = append(lines, bodyLines[i])
			links = append(links, bodyLineLinks[i])
		}
		return lines, links
	}

	lines = append(lines, prefix+lineNum+bodyLines[0])
	links = append(links, bodyLineLinks[0])
	for i := 1; i < len(bodyLines); i++ {
		lines = append(lines, bodyLines[i])
		links = append(links, bodyLineLinks[i])
	}
	return lines, links
}

func formatReactionCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		n := counts[k]
		if n <= 0 {
			continue
		}
		if n == 1 {
			parts = append(parts, k)
		} else {
			parts = append(parts, fmt.Sprintf("%s×%d", k, n))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func (m *MessagesModel) rebuildMessageIndex() {
	m.messageKeys = make(map[string]struct{}, len(m.messages)*2)
	for _, msg := range m.messages {
		for _, key := range messageDedupeKeys(msg) {
			m.messageKeys[key] = struct{}{}
		}
	}
}

func (m *MessagesModel) LatestMessageGUID() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if guid := strings.TrimSpace(m.messages[i].GUID); guid != "" {
			return guid
		}
	}
	return ""
}

func hasImageAttachment(msg models.Message) bool {
	for _, att := range msg.Attachments {
		if isImageAttachment(att) {
			return true
		}
	}
	return false
}

func isImageAttachment(att models.Attachment) bool {
	mime := strings.ToLower(strings.TrimSpace(att.MimeType))
	if strings.HasPrefix(mime, "image/") {
		return true
	}
	for _, raw := range []string{att.FileName, att.URL, att.Path, att.PathOnDisk} {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		switch {
		case strings.Contains(s, ".jpg"),
			strings.Contains(s, ".jpeg"),
			strings.Contains(s, ".png"),
			strings.Contains(s, ".gif"),
			strings.Contains(s, ".webp"),
			strings.Contains(s, ".bmp"),
			strings.Contains(s, ".heic"),
			strings.Contains(s, ".heif"):
			return true
		}
	}
	return false
}

func (m *MessagesModel) ScrollUp() {
	m.viewport.LineUp(3)
	m.stickToBottom = m.viewport.AtBottom()
}

func (m *MessagesModel) ScrollDown() {
	m.viewport.LineDown(3)
	m.stickToBottom = m.viewport.AtBottom()
}

func (m *MessagesModel) ScrollPageUp() {
	m.viewport.ViewUp()
	m.stickToBottom = m.viewport.AtBottom()
}

func (m *MessagesModel) ScrollPageDown() {
	m.viewport.ViewDown()
	m.stickToBottom = m.viewport.AtBottom()
}

func (m *MessagesModel) GotoBottom() {
	m.stickToBottom = true
	m.viewport.GotoBottom()
}

func (m *MessagesModel) HasUnseenIncoming() bool {
	return len(m.unseenGUIDs) > 0
}

func (m *MessagesModel) Loading() bool {
	return m.loading
}

func (m *MessagesModel) SetShowHeader(show bool) {
	m.showHeader = show
}

func (m *MessagesModel) SetLoading(loading bool) {
	if m.loading == loading {
		return
	}
	m.loading = loading
	m.renderContent()
}

func (m MessagesModel) Update(msg tea.Msg) (MessagesModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	// Only let real user scroll input flip stick-to-bottom. Resize passes,
	// ticks and other messages must not clobber the flag, otherwise the
	// viewport can drift off the bottom and hide the newest message.
	if _, ok := msg.(tea.MouseMsg); ok {
		m.stickToBottom = m.viewport.AtBottom()
	}
	return m, cmd
}

func (m MessagesModel) View() string {
	header := ""
	if m.showHeader && (m.chatName != "" || m.loading) {
		var parts []string
		if m.chatName != "" {
			parts = append(parts, m.chatName)
		}
		if m.loading {
			parts = append(parts, "…")
		}
		title := strings.Join(parts, " ")
		header = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			Render(title) + "\n"
	}

	return header + m.viewport.View()
}
