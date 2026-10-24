package tui

import (
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// The chat list is loaded once, at startup. A backend that fails then — a
// BlueBubbles server slow to answer is the usual one — would otherwise be
// missing for the whole session, and its saved panes would come back empty and
// be saved that way. So its chats are asked for again on a timer until they
// come, and its panes keep their chats meanwhile.

// chatRetryDelays is how long to wait before each retry; the last repeats.
// Each retry is one chat-list call per failed backend.
var chatRetryDelays = []time.Duration{
	5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute,
}

func chatRetryDelay(attempt int) time.Duration {
	return chatRetryDelays[min(max(attempt, 1), len(chatRetryDelays))-1]
}

func chatRetryCmd(backends []provider.Provider, limit, attempt int) tea.Cmd {
	return tea.Tick(chatRetryDelay(attempt), func(time.Time) tea.Msg {
		return loadChatsCmd(backends, limit, attempt)()
	})
}

// retryFailedChats notes the backends whose chats did not load and asks them
// again after a while; nil when everything loaded. The first failure is said
// on the status line, so a network missing from the list has a reason.
func (m *AppModel) retryFailedChats(msg chatsLoadedMsg) tea.Cmd {
	if len(msg.failed) == 0 {
		return nil
	}
	labels := make([]string, 0, len(msg.failed))
	for _, backend := range msg.failed {
		m.unloadedBackends[backend.ID()] = true
		labels = append(labels, provider.LabelOf(backend))
	}
	if msg.attempt == 0 {
		m.setAppError(errors.New(strings.Join(labels, ", ") + " chats did not load, retrying"))
	}
	return chatRetryCmd(msg.failed, m.chatLimit, msg.attempt+1)
}

// addRetriedChats takes the chats a retry brought in: into the list, and into
// any pane that has been holding a stand-in for one of them.
func (m *AppModel) addRetriedChats(msg chatsLoadedMsg) tea.Cmd {
	for _, backend := range msg.loaded {
		delete(m.unloadedBackends, backend.ID())
	}
	chats := msg.chats
	applyChatOverrides(chats, m.chatOverrides.Aliases)
	m.chatList.AppendChats(chats)
	m.chatList.SetLoading(len(m.unloadedBackends) > 0 && m.chatList.Len() == 0)
	for i := range chats {
		for _, window := range m.windowManager.WindowsShowingChat(chats[i].GUID) {
			m.windowManager.ShowChat(window, &chats[i])
			if cached := m.windowManager.GetCachedMessages(chats[i].GUID); len(cached) > 0 {
				window.Messages.SetMessages(cached)
			}
		}
	}
	m.updateLayout()
	return m.retryFailedChats(msg)
}

// standInChat holds a restored pane's chat while its backend's list has not
// loaded: named after whoever wrote in the cached messages, or else after the
// network, until the real chat arrives.
func (m *AppModel) standInChat(chatGUID string) *models.Chat {
	var names []string
	seen := make(map[string]bool)
	for _, msg := range m.windowManager.GetCachedMessages(chatGUID) {
		if msg.IsFromMe || msg.Handle == nil {
			continue
		}
		name := senderName(msg)
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) > 3 {
		names = names[:3]
	}
	name := strings.Join(names, ", ")
	if name == "" {
		name = m.providers.Label(chatGUID)
	}
	return &models.Chat{GUID: chatGUID, DisplayName: name}
}
