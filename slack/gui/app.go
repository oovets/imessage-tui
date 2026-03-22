package gui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/slack/api"
)

const appID = "com.bluebubbles-tui.slackgui"

const (
	prefShowChatList    = "ui.show_chat_list"
	prefCompactMode     = "ui.compact_mode"
	prefDarkMode        = "ui.dark_mode"
	prefFontSize        = "ui.font_size"
	prefBoldAll         = "ui.bold_all"
	prefFontFamily      = "ui.font_family"
	prefWindowWidth     = "ui.window_width"
	prefWindowHeight    = "ui.window_height"
	prefPaneLayoutState = "ui.pane_layout_state"
)

const (
	sectionThreads = "threads"
	sectionDMs     = "dms"
	sectionGroups  = "groups"
	sectionRooms   = "rooms"
	sectionBots    = "bots"
)

var paneIDCounter int

type App struct {
	client       *api.Client
	info         *api.AuthInfo
	users        map[string]string
	userByHandle map[string]string
	userByID     map[string]string
	userInfoByID map[string]api.UserInfo

	fyneApp  fyne.App
	win      fyne.Window
	appTheme *compactTheme

	channels         []api.Channel
	channelByID      map[string]api.Channel
	listItems        []chatListItem
	recentThreads    []threadListEntry
	sectionCollapsed map[string]bool

	channelSearch *widget.Entry
	channelList   *widget.List
	chatListPane  *fixedWidthWrap
	rootContent   *fyne.Container
	showChatList  bool

	paneManager *paneManager

	windowWidth  float32
	windowHeight float32

	initialChannelID string
	initialThreadTS  string
}

type chatListItem struct {
	header     string
	sectionKey string
	channel    *api.Channel
	thread     *threadListEntry
}

type threadListEntry struct {
	ChannelID    string
	ChannelLabel string
	ThreadTS     string
	Title        string
	LastActivity time.Time
}

func New(c *api.Client, info *api.AuthInfo) *App {
	return &App{
		client:           c,
		info:             info,
		users:            map[string]string{},
		userByHandle:     map[string]string{},
		userByID:         map[string]string{},
		userInfoByID:     map[string]api.UserInfo{},
		channelByID:      map[string]api.Channel{},
		showChatList:     true,
		windowWidth:      896,
		windowHeight:     820,
		sectionCollapsed: map[string]bool{},
	}
}

func (a *App) SetInitialOpen(channelID, threadTS string) {
	a.initialChannelID = strings.TrimSpace(channelID)
	a.initialThreadTS = strings.TrimSpace(threadTS)
}

func (a *App) Run() {
	a.fyneApp = app.NewWithID(appID)
	a.appTheme = newCompactTheme()
	a.loadUIState()
	a.fyneApp.Settings().SetTheme(a.appTheme)

	a.win = a.fyneApp.NewWindow("Slack")
	a.win.Resize(fyne.NewSize(a.windowWidth, a.windowHeight))

	a.buildSidebar()
	a.paneManager = newPaneManager(
		func(p *chatPane) { a.focusPaneInput(p) },
		func() *chatPane {
			return newChatPane(
				func(cp *chatPane) { a.paneManager.setFocus(cp) },
				func(cp *chatPane) { a.sendFromPane(cp) },
				func(cp *chatPane) { a.exitThreadInPane(cp) },
				func(cp *chatPane) { a.clearReplyTarget(cp) },
				a.handleInputShortcut,
			)
		},
	)
	a.restorePaneLayoutState()

	a.rootContent = container.NewBorder(nil, nil, a.chatListPane, nil, a.paneManager.widget())
	a.win.SetContent(a.rootContent)
	a.win.SetMainMenu(a.buildMainMenu())
	a.registerShortcuts()
	a.fyneApp.Lifecycle().SetOnExitedForeground(func() {
		fyne.Do(func() {
			a.paneManager.setAppFocused(false)
		})
	})
	a.fyneApp.Lifecycle().SetOnEnteredForeground(func() {
		fyne.Do(func() {
			a.paneManager.setAppFocused(true)
			a.focusPaneInput(a.paneManager.focusedPane())
		})
	})
	a.win.SetOnClosed(func() {
		a.saveWindowSizePreference()
		a.savePaneLayoutState()
	})

	if err := a.loadInitialData(); err != nil {
		dialog.ShowError(err, a.win)
	}
	a.applyInitialOpen()
	a.refreshPaneTitles()
	a.focusPaneInput(a.paneManager.focusedPane())
	a.win.ShowAndRun()
}

func (a *App) buildSidebar() {
	a.channelSearch = widget.NewEntry()
	a.channelSearch.SetPlaceHolder("Search chats…")
	a.channelSearch.OnChanged = func(_ string) { a.rebuildFilteredChannels() }

	a.channelList = widget.NewList(
		func() int { return len(a.listItems) },
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("channel")
			lbl.Wrapping = fyne.TextTruncate
			return lbl
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(a.listItems) {
				return
			}
			item := a.listItems[id]
			if item.channel == nil && item.thread == nil {
				obj.(*widget.Label).TextStyle = fyne.TextStyle{Bold: true}
				obj.(*widget.Label).SetText(a.sectionHeaderLabel(item.sectionKey, item.header))
				return
			}
			obj.(*widget.Label).TextStyle = fyne.TextStyle{}
			if item.channel != nil {
				obj.(*widget.Label).SetText(a.chatListLabel(*item.channel))
				return
			}
			obj.(*widget.Label).SetText("↳ " + strings.TrimSpace(item.thread.Title))
		},
	)
	a.channelList.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(a.listItems) {
			return
		}
		item := a.listItems[id]
		if item.channel == nil && item.thread == nil {
			a.toggleSection(item.sectionKey)
			a.channelList.Unselect(id)
			return
		}
		if item.channel == nil && item.thread != nil {
			a.openThreadFromList(*item.thread)
			a.channelList.Unselect(id)
			return
		}
		a.assignChannelToFocusedPane(item.channel.ID)
	}

	left := container.NewBorder(
		a.channelSearch,
		nil, nil, nil,
		a.channelList,
	)
	a.chatListPane = newFixedWidthWrap(left, 130)
	a.chatListPane.show = a.showChatList
}

func (a *App) loadInitialData() error {
	users, err := a.client.UserMap()
	if err != nil {
		log.Printf("[SLACK-GUI] users.list failed: %v", err)
	} else {
		a.users = users
	}
	if dir, err := a.client.UserDirectory(); err != nil {
		log.Printf("[SLACK-GUI] users directory failed: %v", err)
	} else {
		a.buildUserDirectory(dir)
	}
	channels, err := a.client.ListChannels(200)
	if err != nil {
		return err
	}
	sort.Slice(channels, func(i, j int) bool {
		return strings.ToLower(channels[i].Name) < strings.ToLower(channels[j].Name)
	})
	a.channels = channels
	a.channelByID = map[string]api.Channel{}
	for _, ch := range channels {
		a.channelByID[ch.ID] = ch
	}
	a.refreshChannelDisplayNames()
	a.refreshRecentThreads()
	a.rebuildFilteredChannels()

	for _, p := range a.paneManager.allPanes() {
		if strings.TrimSpace(p.channelID) != "" {
			go a.loadMessagesForPane(p)
		}
	}
	if idx := a.firstSelectableListIndex(); idx >= 0 {
		a.channelList.Select(idx)
	}
	return nil
}

func (a *App) refreshRecentThreads() {
	entries := make([]threadListEntry, 0, len(a.channels))
	for _, ch := range a.channels {
		if strings.TrimSpace(ch.ID) == "" {
			continue
		}
		msgs, err := a.client.ChannelHistory(ch.ID, 80, a.users)
		if err != nil {
			continue
		}
		byRoot := map[string]threadListEntry{}
		for _, m := range msgs {
			root := strings.TrimSpace(m.ThreadTS)
			if root == "" {
				if m.ReplyCount <= 0 {
					continue
				}
				root = strings.TrimSpace(m.TS)
			}
			if root == "" {
				continue
			}
			existing, ok := byRoot[root]
			if !ok || m.Time.After(existing.LastActivity) {
				title := strings.TrimSpace(m.Text)
				if title == "" {
					title = "(no text)"
				}
				byRoot[root] = threadListEntry{
					ChannelID:    ch.ID,
					ChannelLabel: a.chatListLabel(ch),
					ThreadTS:     root,
					Title:        truncate(title, 70),
					LastActivity: m.Time,
				}
			}
		}
		for _, t := range byRoot {
			entries = append(entries, t)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastActivity.After(entries[j].LastActivity)
	})
	if len(entries) > 10 {
		entries = entries[:10]
	}
	a.recentThreads = entries
}

func (a *App) refreshChannelDisplayNames() {
	for i := range a.channels {
		ch := &a.channels[i]
		switch {
		case ch.IsIM:
			if n := strings.TrimSpace(a.dmDisplayName(ch.UserID)); n != "" {
				ch.DisplayName = n
			}
		case ch.IsMPIM:
			participants := a.resolveGroupParticipantNames(ch.ID)
			if len(participants) > 0 {
				ch.DisplayName = strings.Join(participants, ", ")
			}
		default:
			ch.DisplayName = ch.Name
		}
	}
	a.channelByID = map[string]api.Channel{}
	for _, ch := range a.channels {
		a.channelByID[ch.ID] = ch
	}
}

func (a *App) resolveGroupParticipantNames(channelID string) []string {
	memberIDs, err := a.client.ConversationMembers(channelID)
	if err != nil {
		log.Printf("[SLACK-GUI] conversations.members failed for %s: %v", channelID, err)
		return nil
	}
	out := make([]string, 0, len(memberIDs))
	selfID := strings.TrimSpace(a.info.UserID)
	for _, id := range memberIDs {
		id = strings.TrimSpace(id)
		if id == "" || id == selfID {
			continue
		}
		name := strings.TrimSpace(a.users[id])
		if name == "" {
			name = id
		}
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func (a *App) rebuildFilteredChannels() {
	query := strings.ToLower(strings.TrimSpace(a.channelSearch.Text))
	dms := make([]api.Channel, 0)
	groups := make([]api.Channel, 0)
	rooms := make([]api.Channel, 0)
	bots := make([]api.Channel, 0)
	threads := make([]threadListEntry, 0, len(a.recentThreads))
	for _, ch := range a.channels {
		if !ch.IsIM && !ch.IsMPIM && !ch.IsMember {
			continue
		}
		if ch.IsIM && strings.TrimSpace(a.chatBaseName(ch)) == "direct message" {
			continue
		}
		candidate := strings.ToLower(strings.TrimSpace(a.chatBaseName(ch)))
		if query != "" && !strings.Contains(candidate, query) {
			continue
		}
		switch {
		case ch.IsIM:
			if a.isBotOrAppDM(ch.UserID) {
				bots = append(bots, ch)
			} else {
				dms = append(dms, ch)
			}
		case ch.IsMPIM:
			groups = append(groups, ch)
		default:
			rooms = append(rooms, ch)
		}
	}
	for _, t := range a.recentThreads {
		match := strings.Contains(strings.ToLower(strings.TrimSpace(t.Title)), query) ||
			strings.Contains(strings.ToLower(strings.TrimSpace(t.ChannelLabel)), query)
		if query == "" || match {
			threads = append(threads, t)
		}
	}
	sortByName := func(items []api.Channel) {
		sort.SliceStable(items, func(i, j int) bool {
			leftUnread := items[i].HasUnread || items[i].UnreadCount > 0
			rightUnread := items[j].HasUnread || items[j].UnreadCount > 0
			if leftUnread != rightUnread {
				return leftUnread
			}
			leftLatest := parseSlackTSValue(items[i].LatestTS)
			rightLatest := parseSlackTSValue(items[j].LatestTS)
			if !leftLatest.Equal(rightLatest) {
				return leftLatest.After(rightLatest)
			}
			return strings.ToLower(a.chatBaseName(items[i])) < strings.ToLower(a.chatBaseName(items[j]))
		})
	}
	sortByName(dms)
	sortByName(groups)
	sortByName(rooms)
	sortByName(bots)
	sort.SliceStable(threads, func(i, j int) bool {
		return threads[i].LastActivity.After(threads[j].LastActivity)
	})

	a.listItems = a.listItems[:0]
	appendSection := func(key, title string, items []api.Channel) {
		if len(items) == 0 && key != sectionThreads {
			return
		}
		a.listItems = append(a.listItems, chatListItem{header: title, sectionKey: key})
		if a.isSectionCollapsed(key) {
			return
		}
		for i := range items {
			ch := items[i]
			a.listItems = append(a.listItems, chatListItem{channel: &ch})
		}
	}
	appendSection(sectionThreads, "Threads", nil)
	if !a.isSectionCollapsed(sectionThreads) {
		for i := range threads {
			t := threads[i]
			a.listItems = append(a.listItems, chatListItem{thread: &t})
		}
	}
	appendSection(sectionDMs, "Direct Messages", dms)
	appendSection(sectionGroups, "Group Chats", groups)
	appendSection(sectionRooms, "Channels", rooms)
	appendSection(sectionBots, "Bots & Apps", bots)
	if a.channelList != nil {
		a.channelList.Refresh()
	}
}

func (a *App) chatListLabel(ch api.Channel) string {
	unread := ""
	if ch.UnreadCount > 0 {
		unread = fmt.Sprintf(" ● %d", ch.UnreadCount)
	} else if ch.HasUnread {
		unread = " ●"
	}
	if ch.IsIM {
		return "@ " + a.chatBaseName(ch) + unread
	}
	if ch.IsMPIM {
		return "◉ " + a.chatBaseName(ch) + unread
	}
	name := a.chatBaseName(ch)
	if name == "" {
		name = "channel"
	}
	return "# " + name + unread
}

func (a *App) chatBaseName(ch api.Channel) string {
	name := strings.TrimSpace(ch.DisplayName)
	if name == "" {
		name = strings.TrimSpace(ch.Name)
	}
	if ch.IsMPIM {
		name = a.stripSelfFromGroupName(name)
	}
	if name == "" {
		switch {
		case ch.IsIM:
			return "direct message"
		case ch.IsMPIM:
			return "group"
		default:
			return "channel"
		}
	}
	return name
}

func (a *App) chatPrefix(ch api.Channel) string {
	if ch.IsIM {
		return "@"
	}
	if ch.IsMPIM {
		return "◉ "
	}
	return "#"
}

func (a *App) chatInputPlaceholder(ch api.Channel) string {
	name := a.chatBaseName(ch)
	if ch.IsIM {
		return "Message @" + name
	}
	if ch.IsMPIM {
		return "Message " + name
	}
	return "Message #" + name
}

func (a *App) stripSelfFromGroupName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	selfCandidates := map[string]struct{}{}
	if self := strings.TrimSpace(a.info.UserName); self != "" {
		selfCandidates[strings.ToLower(self)] = struct{}{}
	}
	if selfDisplay := strings.TrimSpace(a.users[a.info.UserID]); selfDisplay != "" {
		selfCandidates[strings.ToLower(selfDisplay)] = struct{}{}
	}
	parts := strings.Split(name, ",")
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		clean := strings.TrimSpace(p)
		if clean == "" {
			continue
		}
		if _, ok := selfCandidates[strings.ToLower(clean)]; ok {
			continue
		}
		filtered = append(filtered, clean)
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, ", ")
}

func (a *App) refreshPaneTitles() {
	for _, p := range a.paneManager.allPanes() {
		if p.channelID == "" {
			p.setTitle("Select a channel")
			continue
		}
		ch, ok := a.channelByID[p.channelID]
		if !ok {
			p.setTitle("#unknown")
			continue
		}
		name := a.chatBaseName(ch)
		p.channelName = name
		if strings.TrimSpace(p.threadTS) != "" {
			p.setTitle(a.chatPrefix(ch) + name + " — thread")
		} else {
			p.setTitle(a.chatPrefix(ch) + name + " — " + a.info.TeamName)
		}
		p.input.SetPlaceHolder(a.chatInputPlaceholder(ch))
	}
}

func (a *App) assignChannelToFocusedPane(channelID string) {
	p := a.paneManager.focusedPane()
	if p == nil {
		return
	}
	ch, ok := a.channelByID[channelID]
	if !ok {
		return
	}
	p.channelID = channelID
	p.channelName = a.chatBaseName(ch)
	p.threadTS = ""
	p.setThreadBanner("")
	p.replyTarget = nil
	p.setTitle(fmt.Sprintf("%s%s — %s", a.chatPrefix(ch), p.channelName, a.info.TeamName))
	p.input.SetPlaceHolder(a.chatInputPlaceholder(ch))
	p.clearMessages()
	a.focusPaneInput(p)
	a.savePaneLayoutState()
	go a.loadMessagesForPane(p)
}

func (a *App) loadMessagesForPane(p *chatPane) {
	if p == nil || strings.TrimSpace(p.channelID) == "" {
		return
	}
	channelID := p.channelID
	threadTS := strings.TrimSpace(p.threadTS)
	var (
		msgs []api.Message
		err  error
	)
	if threadTS == "" {
		msgs, err = a.client.ChannelHistory(channelID, 120, a.users)
	} else {
		msgs, err = a.client.ThreadReplies(channelID, threadTS, 120, a.users)
	}
	fyne.Do(func() {
		if p.channelID != channelID || strings.TrimSpace(p.threadTS) != threadTS {
			return
		}
		if err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		if threadTS == "" {
			p.setThreadBanner("")
		} else if len(msgs) > 0 {
			root := msgs[0]
			p.setThreadBanner(fmt.Sprintf("Thread: %s — %s", senderName(root), truncate(strings.TrimSpace(root.Text), 80)))
		} else {
			p.setThreadBanner("Thread")
		}
		for i := range msgs {
			msgs[i].Text = a.formatMentionsForDisplay(msgs[i].Text)
		}
		p.setMessages(msgs, a.info.UserID, a.info.UserID, func(m api.Message) {
			a.openThreadInPane(p, m)
		}, func(m api.Message) {
			a.setReplyTarget(p, m)
		}, func(f api.File) {
			a.openMediaDialog(f)
		})
	})
}

func (a *App) openThreadInPane(p *chatPane, m api.Message) {
	rootTS := threadRootTS(m)
	if p == nil || rootTS == "" {
		return
	}
	p.threadTS = rootTS
	p.replyTarget = nil
	p.replyHolder.Hide()
	p.setThreadBanner(fmt.Sprintf("Thread: %s — %s", senderName(m), truncate(strings.TrimSpace(m.Text), 80)))
	p.setTitle(fmt.Sprintf("#%s — thread", p.channelName))
	a.savePaneLayoutState()
	go a.loadMessagesForPane(p)
}

func (a *App) exitThreadInPane(p *chatPane) {
	if p == nil || strings.TrimSpace(p.threadTS) == "" {
		return
	}
	p.threadTS = ""
	p.setThreadBanner("")
	p.replyTarget = nil
	p.replyHolder.Hide()
	p.setTitle(fmt.Sprintf("#%s — %s", p.channelName, a.info.TeamName))
	a.savePaneLayoutState()
	go a.loadMessagesForPane(p)
}

func (a *App) setReplyTarget(p *chatPane, m api.Message) {
	p.replyTarget = &m
	p.replyLabel.SetText(fmt.Sprintf("Replying to %s: %s", senderName(m), truncate(m.Text, 80)))
	p.replyHolder.Show()
}

func (a *App) clearReplyTarget(p *chatPane) {
	p.replyTarget = nil
	p.replyHolder.Hide()
	p.replyLabel.SetText("")
}

func (a *App) sendFromPane(p *chatPane) {
	if p == nil {
		return
	}
	text := strings.TrimSpace(p.input.Text)
	text = a.expandOutgoingMentions(text)
	if text == "" || strings.TrimSpace(p.channelID) == "" {
		return
	}
	threadTS := p.threadTS
	if p.replyTarget != nil {
		threadTS = threadRootTS(*p.replyTarget)
	}
	if err := a.client.PostMessage(p.channelID, text, threadTS); err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	p.input.SetText("")
	a.clearReplyTarget(p)
	go func() {
		time.Sleep(130 * time.Millisecond)
		a.loadMessagesForPane(p)
	}()
}

func (a *App) buildUserDirectory(dir []api.UserInfo) {
	a.userByHandle = map[string]string{}
	a.userByID = map[string]string{}
	a.userInfoByID = map[string]api.UserInfo{}
	for _, u := range dir {
		id := strings.TrimSpace(u.ID)
		if id == "" {
			continue
		}
		a.userInfoByID[id] = u
		display := strings.TrimSpace(u.DisplayName)
		if display == "" {
			display = strings.TrimSpace(u.RealName)
		}
		if display == "" {
			display = strings.TrimSpace(u.Username)
		}
		if display != "" {
			a.userByID[id] = display
		}
		if h := strings.TrimSpace(u.Username); h != "" {
			a.userByHandle[strings.ToLower(h)] = id
		}
	}
}

func (a *App) dmDisplayName(userID string) string {
	id := strings.TrimSpace(userID)
	if id == "" {
		return ""
	}
	if n := strings.TrimSpace(a.users[id]); n != "" {
		return n
	}
	if n := strings.TrimSpace(a.userByID[id]); n != "" {
		return n
	}
	info, err := a.client.UserInfo(id)
	if err != nil || info == nil {
		return ""
	}
	a.userInfoByID[id] = *info
	if h := strings.TrimSpace(info.Username); h != "" {
		a.userByHandle[strings.ToLower(h)] = id
	}
	name := strings.TrimSpace(info.DisplayName)
	if name == "" {
		name = strings.TrimSpace(info.RealName)
	}
	if name == "" {
		name = strings.TrimSpace(info.Username)
	}
	if name != "" {
		a.userByID[id] = name
		a.users[id] = name
	}
	return name
}

func (a *App) firstSelectableListIndex() int {
	for i, item := range a.listItems {
		if item.channel != nil {
			return i
		}
	}
	return -1
}

func (a *App) isBotOrAppDM(userID string) bool {
	id := strings.TrimSpace(userID)
	if id == "" {
		return false
	}
	info, ok := a.userInfoByID[id]
	if !ok {
		fetched, err := a.client.UserInfo(id)
		if err != nil || fetched == nil {
			return false
		}
		info = *fetched
		a.userInfoByID[id] = info
	}
	return info.IsBot || info.IsAppUser
}

func (a *App) isSectionCollapsed(section string) bool {
	if a.sectionCollapsed == nil {
		a.sectionCollapsed = map[string]bool{}
	}
	collapsed, ok := a.sectionCollapsed[section]
	if !ok {
		return true
	}
	return collapsed
}

func (a *App) toggleSection(section string) {
	if strings.TrimSpace(section) == "" {
		return
	}
	if a.sectionCollapsed == nil {
		a.sectionCollapsed = map[string]bool{}
	}
	a.sectionCollapsed[section] = !a.isSectionCollapsed(section)
	a.rebuildFilteredChannels()
}

func (a *App) sectionHeaderLabel(section, title string) string {
	arrow := "▼"
	if a.isSectionCollapsed(section) {
		arrow = "▶"
	}
	return fmt.Sprintf("%s %s", arrow, title)
}

func (a *App) openThreadFromList(t threadListEntry) {
	if strings.TrimSpace(t.ChannelID) == "" || strings.TrimSpace(t.ThreadTS) == "" {
		return
	}
	p := a.paneManager.focusedPane()
	if p == nil {
		return
	}
	ch, ok := a.channelByID[t.ChannelID]
	if !ok {
		return
	}
	p.channelID = t.ChannelID
	p.channelName = a.chatBaseName(ch)
	p.threadTS = strings.TrimSpace(t.ThreadTS)
	p.replyTarget = nil
	p.setThreadBanner("Thread: " + strings.TrimSpace(t.Title))
	p.input.SetPlaceHolder(a.chatInputPlaceholder(ch))
	p.setTitle(fmt.Sprintf("%s%s — thread", a.chatPrefix(ch), p.channelName))
	a.savePaneLayoutState()
	go a.loadMessagesForPane(p)
}

func parseSlackTSValue(ts string) time.Time {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}
	}
	parts := strings.SplitN(ts, ".", 2)
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}
	}
	nsec := int64(0)
	if len(parts) == 2 && parts[1] != "" {
		frac := parts[1]
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for len(frac) < 9 {
			frac += "0"
		}
		nsec, _ = strconv.ParseInt(frac, 10, 64)
	}
	return time.Unix(sec, nsec)
}

var mentionHandleRE = regexp.MustCompile(`(^|[\s(\[{])@([a-zA-Z0-9._-]{1,80})`)
var mentionSlackIDRE = regexp.MustCompile(`<@([A-Z0-9]+)>`)

func (a *App) expandOutgoingMentions(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	return mentionHandleRE.ReplaceAllStringFunc(text, func(m string) string {
		g := mentionHandleRE.FindStringSubmatch(m)
		if len(g) != 3 {
			return m
		}
		prefix := g[1]
		handle := strings.ToLower(g[2])
		id := strings.TrimSpace(a.userByHandle[handle])
		if id == "" {
			return m
		}
		return prefix + "<@" + id + ">"
	})
}

func (a *App) formatMentionsForDisplay(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	return mentionSlackIDRE.ReplaceAllStringFunc(text, func(m string) string {
		g := mentionSlackIDRE.FindStringSubmatch(m)
		if len(g) != 2 {
			return m
		}
		id := strings.TrimSpace(g[1])
		if id == "" {
			return m
		}
		if name := strings.TrimSpace(a.userByID[id]); name != "" {
			return "@" + name
		}
		if name := strings.TrimSpace(a.users[id]); name != "" {
			return "@" + name
		}
		return "@user"
	})
}

func (a *App) openMediaDialog(f api.File) {
	imageURL := strings.TrimSpace(f.BestImageURL())
	if imageURL == "" {
		if strings.TrimSpace(f.Permalink) != "" {
			u := mustParseURL(f.Permalink)
			dialog.ShowCustom("Media", "Close", widget.NewHyperlink(f.Name, u), a.win)
			return
		}
		dialog.ShowInformation("Media", "No preview URL available for this file.", a.win)
		return
	}
	go func() {
		data, _, err := a.client.FetchPrivateURL(imageURL)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, a.win)
				return
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				dialog.ShowError(err, a.win)
				return
			}
			cimg := canvas.NewImageFromImage(img)
			cimg.FillMode = canvas.ImageFillContain
			cimg.SetMinSize(fyne.NewSize(560, 420))
			w := a.fyneApp.NewWindow("Media: " + f.Name)
			w.SetContent(container.NewPadded(cimg))
			w.Resize(fyne.NewSize(700, 520))
			w.Show()
		})
	}()
}

func (a *App) registerShortcuts() {
	c := a.win.Canvas()
	c.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyName("H"), Modifier: fyne.KeyModifierControl}, func(_ fyne.Shortcut) {
		p := a.newPane()
		a.paneManager.splitFocused(splitHorizontal, p)
		a.focusPaneInput(a.paneManager.focusedPane())
		a.savePaneLayoutState()
	})
	c.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyName("J"), Modifier: fyne.KeyModifierControl}, func(_ fyne.Shortcut) {
		p := a.newPane()
		a.paneManager.splitFocused(splitVertical, p)
		a.focusPaneInput(a.paneManager.focusedPane())
		a.savePaneLayoutState()
	})
	c.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyName("W"), Modifier: fyne.KeyModifierControl}, func(_ fyne.Shortcut) {
		a.paneManager.closeFocused()
		a.focusPaneInput(a.paneManager.focusedPane())
		a.savePaneLayoutState()
	})
	c.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyName("S"), Modifier: fyne.KeyModifierControl}, func(_ fyne.Shortcut) {
		a.toggleChatList()
	})
	c.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyName("N"), Modifier: fyne.KeyModifierControl}, func(_ fyne.Shortcut) {
		a.openNewWindow()
	})
}

func (a *App) buildMainMenu() *fyne.MainMenu {
	colorModeLabel := "Switch to Light Mode"
	if !a.appTheme.dark {
		colorModeLabel = "Switch to Dark Mode"
	}
	compactLabel := "Enable Compact Mode"
	if a.appTheme.compactMode {
		compactLabel = "Disable Compact Mode"
	}

	fontItems := make([]*fyne.MenuItem, 0)
	for _, name := range a.appTheme.availableFamilies() {
		n := name
		label := n
		if n == a.appTheme.curFamily {
			label = "✓ " + n
		}
		fontItems = append(fontItems, fyne.NewMenuItem(label, func() {
			a.setFont(n)
			a.win.SetMainMenu(a.buildMainMenu())
		}))
	}
	fontItem := fyne.NewMenuItem("Font", nil)
	fontItem.ChildMenu = fyne.NewMenu("", fontItems...)

	windowMenu := fyne.NewMenu("Window",
		fyne.NewMenuItem("New Window", func() { a.openNewWindow() }),
		fyne.NewMenuItem("Move Focused Pane to New Window", func() { a.moveFocusedPaneToNewWindow() }),
	)
	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("A+ Larger", func() {
			if a.appTheme.fontSize < 20 {
				a.appTheme.fontSize++
				a.fyneApp.Preferences().SetInt(prefFontSize, int(a.appTheme.fontSize))
				a.fyneApp.Settings().SetTheme(a.appTheme)
				a.refreshPanesForTheme()
			}
		}),
		fyne.NewMenuItem("A- Smaller", func() {
			if a.appTheme.fontSize > 8 {
				a.appTheme.fontSize--
				a.fyneApp.Preferences().SetInt(prefFontSize, int(a.appTheme.fontSize))
				a.fyneApp.Settings().SetTheme(a.appTheme)
				a.refreshPanesForTheme()
			}
		}),
		fyne.NewMenuItem("Toggle Bold", func() {
			a.appTheme.boldAll = !a.appTheme.boldAll
			a.fyneApp.Preferences().SetBool(prefBoldAll, a.appTheme.boldAll)
			a.fyneApp.Settings().SetTheme(a.appTheme)
			a.refreshPanesForTheme()
		}),
		fontItem,
		fyne.NewMenuItem("Toggle Channel List", func() { a.toggleChatList() }),
		fyne.NewMenuItem(compactLabel, func() { a.setCompactMode(!a.appTheme.compactMode) }),
		fyne.NewMenuItem(colorModeLabel, func() { a.setDarkMode(!a.appTheme.dark) }),
	)
	return fyne.NewMainMenu(windowMenu, viewMenu)
}

func (a *App) toggleChatList() {
	a.showChatList = !a.showChatList
	a.chatListPane.show = a.showChatList
	a.chatListPane.Refresh()
	a.rootContent.Refresh()
	a.fyneApp.Preferences().SetBool(prefShowChatList, a.showChatList)
}

func (a *App) setCompactMode(enabled bool) {
	a.appTheme.compactMode = enabled
	a.fyneApp.Preferences().SetBool(prefCompactMode, enabled)
	a.fyneApp.Settings().SetTheme(a.appTheme)
	a.win.SetMainMenu(a.buildMainMenu())
	a.refreshPanesForTheme()
}

func (a *App) setDarkMode(enabled bool) {
	a.appTheme.dark = enabled
	a.fyneApp.Preferences().SetBool(prefDarkMode, enabled)
	a.fyneApp.Settings().SetTheme(a.appTheme)
	a.win.SetMainMenu(a.buildMainMenu())
	a.refreshPanesForTheme()
}

func (a *App) setFont(family string) {
	if _, ok := a.appTheme.fonts[family]; !ok {
		return
	}
	a.appTheme.curFamily = family
	a.fyneApp.Preferences().SetString(prefFontFamily, family)
	a.fyneApp.Settings().SetTheme(a.appTheme)
	a.refreshPanesForTheme()
}

func (a *App) refreshPanesForTheme() {
	for _, p := range a.paneManager.allPanes() {
		p.refreshForTheme()
		if strings.TrimSpace(p.channelID) != "" {
			go a.loadMessagesForPane(p)
		}
	}
}

func (a *App) openNewWindow() {
	a.launchWindowForPane("", "")
}

func (a *App) moveFocusedPaneToNewWindow() {
	p := a.paneManager.focusedPane()
	if p == nil {
		a.openNewWindow()
		return
	}
	if !a.launchWindowForPane(p.channelID, p.threadTS) {
		return
	}
	if len(a.paneManager.allPanes()) > 1 {
		a.paneManager.closeFocused()
		a.focusPaneInput(a.paneManager.focusedPane())
		a.savePaneLayoutState()
		return
	}
	p.channelID = ""
	p.channelName = ""
	p.threadTS = ""
	p.setThreadBanner("")
	p.clearMessages()
	p.setTitle("Select a channel")
}

func (a *App) launchWindowForPane(channelID, threadTS string) bool {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		log.Printf("[SLACK-GUI] unable to resolve executable: %v", err)
		return false
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(),
		"SLACK_OPEN_CHANNEL_ID="+strings.TrimSpace(channelID),
		"SLACK_OPEN_THREAD_TS="+strings.TrimSpace(threadTS),
	)
	if err := cmd.Start(); err != nil {
		log.Printf("[SLACK-GUI] unable to start new window: %v", err)
		return false
	}
	return true
}

func (a *App) applyInitialOpen() {
	if strings.TrimSpace(a.initialChannelID) == "" {
		return
	}
	p := a.paneManager.focusedPane()
	if p == nil {
		return
	}
	ch, ok := a.channelByID[a.initialChannelID]
	if !ok {
		return
	}
	p.channelID = ch.ID
	p.channelName = a.chatBaseName(ch)
	p.threadTS = strings.TrimSpace(a.initialThreadTS)
	if p.threadTS != "" {
		p.setThreadBanner("Thread view")
	} else {
		p.setThreadBanner("")
	}
	p.input.SetPlaceHolder(a.chatInputPlaceholder(ch))
	p.setTitle(fmt.Sprintf("%s%s — %s", a.chatPrefix(ch), p.channelName, a.info.TeamName))
	a.savePaneLayoutState()
	go a.loadMessagesForPane(p)
}

func (a *App) newPane() *chatPane {
	return newChatPane(
		func(cp *chatPane) { a.paneManager.setFocus(cp) },
		func(cp *chatPane) { a.sendFromPane(cp) },
		func(cp *chatPane) { a.exitThreadInPane(cp) },
		func(cp *chatPane) { a.clearReplyTarget(cp) },
		a.handleInputShortcut,
	)
}

func (a *App) focusPaneInput(p *chatPane) {
	if p == nil || a.win == nil {
		return
	}
	a.win.Canvas().Focus(p.input)
}

func (a *App) loadUIState() {
	if a.fyneApp == nil || a.appTheme == nil {
		return
	}
	prefs := a.fyneApp.Preferences()
	a.showChatList = prefs.BoolWithFallback(prefShowChatList, true)
	a.appTheme.dark = prefs.BoolWithFallback(prefDarkMode, a.appTheme.dark)
	a.appTheme.compactMode = prefs.BoolWithFallback(prefCompactMode, false)
	fontSize := prefs.IntWithFallback(prefFontSize, int(a.appTheme.fontSize))
	if fontSize < 8 {
		fontSize = 8
	}
	if fontSize > 20 {
		fontSize = 20
	}
	a.appTheme.fontSize = float32(fontSize)
	a.appTheme.boldAll = prefs.BoolWithFallback(prefBoldAll, a.appTheme.boldAll)
	if family := prefs.StringWithFallback(prefFontFamily, a.appTheme.curFamily); family != "" {
		if _, ok := a.appTheme.fonts[family]; ok {
			a.appTheme.curFamily = family
		}
	}
	a.windowWidth = float32(prefs.FloatWithFallback(prefWindowWidth, 896))
	a.windowHeight = float32(prefs.FloatWithFallback(prefWindowHeight, 820))
	if a.windowWidth < 700 {
		a.windowWidth = 700
	}
	if a.windowHeight < 500 {
		a.windowHeight = 500
	}
}

func (a *App) saveWindowSizePreference() {
	if a.fyneApp == nil || a.win == nil {
		return
	}
	size := a.win.Canvas().Size()
	prefs := a.fyneApp.Preferences()
	prefs.SetFloat(prefWindowWidth, float64(size.Width))
	prefs.SetFloat(prefWindowHeight, float64(size.Height))
}

func (a *App) savePaneLayoutState() {
	if a.fyneApp == nil || a.paneManager == nil {
		return
	}
	raw, err := a.paneManager.serializeState()
	if err != nil {
		log.Printf("[SLACK-GUI] serialize layout failed: %v", err)
		return
	}
	a.fyneApp.Preferences().SetString(prefPaneLayoutState, raw)
}

func (a *App) restorePaneLayoutState() {
	if a.fyneApp == nil || a.paneManager == nil {
		return
	}
	raw := a.fyneApp.Preferences().StringWithFallback(prefPaneLayoutState, "")
	if strings.TrimSpace(raw) == "" {
		return
	}
	if err := a.paneManager.restoreState(raw); err != nil {
		log.Printf("[SLACK-GUI] restore layout failed: %v", err)
	}
}

type fixedWidthWrap struct {
	widget.BaseWidget
	child fyne.CanvasObject
	width float32
	show  bool
}

func newFixedWidthWrap(child fyne.CanvasObject, width float32) *fixedWidthWrap {
	w := &fixedWidthWrap{child: child, width: width, show: true}
	w.ExtendBaseWidget(w)
	return w
}

func (w *fixedWidthWrap) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(w.child)
}
func (w *fixedWidthWrap) MinSize() fyne.Size {
	if !w.show {
		return fyne.NewSize(0, 0)
	}
	return fyne.NewSize(w.width, w.child.MinSize().Height)
}

type splitDir int

const (
	splitHorizontal splitDir = iota
	splitVertical
)

type paneNode struct {
	pane        *chatPane
	left, right *paneNode
	dir         splitDir
}

func (n *paneNode) isLeaf() bool { return n != nil && n.pane != nil }

func (n *paneNode) allPanes() []*chatPane {
	if n == nil {
		return nil
	}
	if n.isLeaf() {
		return []*chatPane{n.pane}
	}
	return append(n.left.allPanes(), n.right.allPanes()...)
}

func (n *paneNode) buildWidget() fyne.CanvasObject {
	if n.isLeaf() {
		return n.pane.widget()
	}
	l := n.left.buildWidget()
	r := n.right.buildWidget()
	if n.dir == splitHorizontal {
		s := container.NewHSplit(l, r)
		s.SetOffset(0.5)
		return s
	}
	s := container.NewVSplit(l, r)
	s.SetOffset(0.5)
	return s
}

func (n *paneNode) split(target *chatPane, add *chatPane, dir splitDir) bool {
	if n.isLeaf() {
		if n.pane != target {
			return false
		}
		n.left = &paneNode{pane: n.pane}
		n.right = &paneNode{pane: add}
		n.dir = dir
		n.pane = nil
		return true
	}
	return n.left.split(target, add, dir) || n.right.split(target, add, dir)
}

func (n *paneNode) remove(target *chatPane) bool {
	if n == nil || n.isLeaf() {
		return false
	}
	if n.left.isLeaf() && n.left.pane == target {
		*n = *n.right
		return true
	}
	if n.right.isLeaf() && n.right.pane == target {
		*n = *n.left
		return true
	}
	return n.left.remove(target) || n.right.remove(target)
}

type paneManager struct {
	root       paneNode
	focused    *chatPane
	holder     *fyne.Container
	maxPanes   int
	appFocused bool
	onFocused  func(*chatPane)
	makePane   func() *chatPane
}

func newPaneManager(onFocused func(*chatPane), makePane func() *chatPane) *paneManager {
	pm := &paneManager{maxPanes: 8, onFocused: onFocused, appFocused: true}
	pm.makePane = makePane
	if pm.makePane == nil {
		pm.makePane = func() *chatPane {
			return newChatPane(
				func(cp *chatPane) { pm.setFocus(cp) },
				nil,
				nil,
				nil,
				nil,
			)
		}
	}
	first := pm.makePane()
	pm.root = paneNode{pane: first}
	pm.focused = first
	first.setFocused(true)
	pm.holder = container.NewMax(pm.root.buildWidget())
	pm.syncInputVisibility(false)
	return pm
}

func (pm *paneManager) widget() fyne.CanvasObject { return pm.holder }
func (pm *paneManager) focusedPane() *chatPane    { return pm.focused }
func (pm *paneManager) allPanes() []*chatPane     { return pm.root.allPanes() }

func (pm *paneManager) setFocus(p *chatPane) {
	if p == nil || pm.focused == p {
		return
	}
	if pm.focused != nil {
		pm.focused.setFocused(false)
	}
	pm.focused = p
	pm.focused.setFocused(true)
	pm.syncInputVisibility(true)
	if pm.onFocused != nil {
		pm.onFocused(p)
	}
}

func (pm *paneManager) splitFocused(dir splitDir, newPane *chatPane) {
	if pm.focused == nil || len(pm.allPanes()) >= pm.maxPanes || newPane == nil {
		return
	}
	pm.root.split(pm.focused, newPane, dir)
	pm.setFocus(newPane)
	pm.syncInputVisibility(true)
	pm.rebuild()
}

func (pm *paneManager) closeFocused() {
	panes := pm.allPanes()
	if len(panes) <= 1 || pm.focused == nil {
		return
	}
	var next *chatPane
	for i, p := range panes {
		if p == pm.focused {
			if i > 0 {
				next = panes[i-1]
			} else if i+1 < len(panes) {
				next = panes[i+1]
			}
			break
		}
	}
	pm.root.remove(pm.focused)
	pm.focused = next
	if pm.focused != nil {
		pm.focused.setFocused(true)
	}
	pm.syncInputVisibility(false)
	pm.rebuild()
}

func (pm *paneManager) rebuild() {
	pm.holder.Objects = []fyne.CanvasObject{pm.root.buildWidget()}
	pm.holder.Refresh()
}

func (pm *paneManager) setAppFocused(focused bool) {
	if pm.appFocused == focused {
		return
	}
	pm.appFocused = focused
	pm.syncInputVisibility(false)
}

func (pm *paneManager) syncInputVisibility(reveal bool) {
	for _, p := range pm.allPanes() {
		p.setFocused(pm.appFocused && p == pm.focused)
		p.setInputVisible(pm.appFocused && p == pm.focused, reveal)
	}
}

type paneManagerState struct {
	Root          *paneStateNode `json:"root"`
	FocusedPaneID int            `json:"focusedPaneID"`
}

type paneStateNode struct {
	PaneID    int            `json:"paneID,omitempty"`
	ChannelID string         `json:"channelID,omitempty"`
	ThreadTS  string         `json:"threadTS,omitempty"`
	Dir       string         `json:"dir,omitempty"`
	Left      *paneStateNode `json:"left,omitempty"`
	Right     *paneStateNode `json:"right,omitempty"`
}

func (pm *paneManager) serializeState() (string, error) {
	state := paneManagerState{
		Root:          serializePaneNode(&pm.root),
		FocusedPaneID: -1,
	}
	if pm.focused != nil {
		state.FocusedPaneID = pm.focused.id
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (pm *paneManager) restoreState(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var state paneManagerState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return err
	}
	if state.Root == nil {
		return fmt.Errorf("invalid pane state root")
	}
	idMap := map[int]*chatPane{}
	root, err := pm.restoreNode(state.Root, idMap)
	if err != nil {
		return err
	}
	pm.root = *root
	pm.focused = idMap[state.FocusedPaneID]
	if pm.focused == nil {
		all := pm.allPanes()
		if len(all) > 0 {
			pm.focused = all[0]
		}
	}
	for _, p := range pm.allPanes() {
		p.setFocused(false)
	}
	if pm.focused != nil {
		pm.focused.setFocused(true)
	}
	paneIDCounter = maxPaneID(pm.allPanes()) + 1
	pm.rebuild()
	return nil
}

func (pm *paneManager) restoreNode(n *paneStateNode, idMap map[int]*chatPane) (*paneNode, error) {
	if n == nil {
		return nil, fmt.Errorf("nil node")
	}
	if n.Left == nil && n.Right == nil {
		p := pm.makePane()
		p.id = n.PaneID
		p.channelID = n.ChannelID
		p.threadTS = n.ThreadTS
		if strings.TrimSpace(p.threadTS) != "" {
			p.setThreadBanner("Thread view")
		} else {
			p.setThreadBanner("")
		}
		idMap[p.id] = p
		return &paneNode{pane: p}, nil
	}
	if n.Left == nil || n.Right == nil {
		return nil, fmt.Errorf("invalid split node")
	}
	left, err := pm.restoreNode(n.Left, idMap)
	if err != nil {
		return nil, err
	}
	right, err := pm.restoreNode(n.Right, idMap)
	if err != nil {
		return nil, err
	}
	dir := splitHorizontal
	if strings.ToLower(n.Dir) == "vertical" {
		dir = splitVertical
	}
	return &paneNode{left: left, right: right, dir: dir}, nil
}

func serializePaneNode(n *paneNode) *paneStateNode {
	if n == nil {
		return nil
	}
	if n.isLeaf() {
		return &paneStateNode{
			PaneID:    n.pane.id,
			ChannelID: n.pane.channelID,
			ThreadTS:  n.pane.threadTS,
		}
	}
	dir := "horizontal"
	if n.dir == splitVertical {
		dir = "vertical"
	}
	return &paneStateNode{
		Dir:   dir,
		Left:  serializePaneNode(n.left),
		Right: serializePaneNode(n.right),
	}
}

func maxPaneID(panes []*chatPane) int {
	maxID := -1
	for _, p := range panes {
		if p != nil && p.id > maxID {
			maxID = p.id
		}
	}
	return maxID
}

type chatPane struct {
	id           int
	root         *paneSurface
	panel        *fyne.Container
	title        *widget.Label
	viewport     *fyne.Container
	msgBox       *fyne.Container
	msgScroll    *container.Scroll
	input        *focusEntry
	inputCard    *fyne.Container
	inputGap     *canvas.Rectangle
	inputVisible bool
	revealAnim   *fyne.Animation
	replyHolder  *fyne.Container
	replyLabel   *widget.Label
	threadHolder *fyne.Container
	threadLabel  *widget.Label

	channelID   string
	channelName string
	threadTS    string
	replyTarget *api.Message

	inputBg *canvas.Rectangle
}

func newChatPane(onActivate func(*chatPane), onSend func(*chatPane), onExitThread func(*chatPane), onCancelReply func(*chatPane), onShortcut func(fyne.Shortcut) bool) *chatPane {
	p := &chatPane{id: paneIDCounter}
	paneIDCounter++
	p.title = widget.NewLabel("Select a channel")
	p.title.Importance = widget.HighImportance
	p.msgBox = container.NewVBox()
	p.msgScroll = container.NewVScroll(p.msgBox)
	p.input = newFocusEntry(func() {
		if onActivate != nil {
			onActivate(p)
		}
	}, onShortcut)
	p.input.Wrapping = fyne.TextWrapWord
	p.input.SetMinRowsVisible(2)
	p.input.OnSubmitted = func(_ string) {
		if onSend != nil {
			onSend(p)
		}
	}
	p.replyLabel = widget.NewLabel("")
	p.threadLabel = widget.NewLabel("")
	p.threadHolder = container.NewBorder(nil, nil, nil, widget.NewButton("Back", func() {
		if onExitThread != nil {
			onExitThread(p)
		}
	}), p.threadLabel)
	p.threadHolder.Hide()
	p.replyHolder = container.NewBorder(nil, nil, nil, widget.NewButton("×", func() {
		if onCancelReply != nil {
			onCancelReply(p)
		}
	}), p.replyLabel)
	p.replyHolder.Hide()

	p.inputGap = canvas.NewRectangle(color.Transparent)
	p.inputGap.SetMinSize(fyne.NewSize(1, 10))
	p.inputBg = canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	inputHPad := float32(52)
	entryRow := container.NewMax(
		p.inputBg,
		container.NewBorder(nil, nil, fixedWidthSpacer(inputHPad), fixedWidthSpacer(inputHPad), p.input),
	)
	p.inputCard = container.NewVBox(p.threadHolder, p.replyHolder, entryRow, p.inputGap)
	p.inputVisible = true
	p.viewport = container.NewBorder(nil, p.inputCard, nil, nil, p.msgScroll)
	p.viewport.Objects = []fyne.CanvasObject{p.msgScroll, p.inputCard}
	p.panel = container.NewMax(p.viewport)
	p.root = newPaneSurface(p.panel, func() {
		if onActivate != nil {
			onActivate(p)
		}
	})
	return p
}

func (p *chatPane) widget() fyne.CanvasObject { return p.root }

func (p *chatPane) setFocused(focused bool) {
	_ = focused
}

func (p *chatPane) setTitle(t string) { p.title.SetText(t) }

func (p *chatPane) setThreadBanner(text string) {
	if p.threadHolder == nil || p.threadLabel == nil {
		return
	}
	if strings.TrimSpace(text) == "" {
		p.threadLabel.SetText("")
		p.threadHolder.Hide()
		return
	}
	p.threadLabel.SetText(text)
	p.threadHolder.Show()
}

func (p *chatPane) setInputVisible(visible bool, reveal bool) {
	if p.inputVisible == visible {
		return
	}
	if p.revealAnim != nil {
		p.revealAnim.Stop()
		p.revealAnim = nil
	}
	p.inputVisible = visible
	if visible {
		if reveal {
			revealSpacer := canvas.NewRectangle(color.Transparent)
			start := float32(14)
			revealSpacer.SetMinSize(fyne.NewSize(1, start))
			p.viewport.Objects = []fyne.CanvasObject{p.msgScroll, container.NewVBox(revealSpacer, p.inputCard)}
			p.panel.Refresh()
			p.revealAnim = fyne.NewAnimation(130*time.Millisecond, func(f float32) {
				h := start * (1 - f)
				revealSpacer.SetMinSize(fyne.NewSize(1, h))
				p.panel.Refresh()
			})
			p.revealAnim.Curve = fyne.AnimationEaseOut
			p.revealAnim.Start()
			return
		}
		p.viewport.Objects = []fyne.CanvasObject{p.msgScroll, p.inputCard}
	} else {
		hiddenSpacer := canvas.NewRectangle(color.Transparent)
		hiddenSpacer.SetMinSize(fyne.NewSize(1, 10))
		p.viewport.Objects = []fyne.CanvasObject{p.msgScroll, hiddenSpacer}
	}
	p.panel.Refresh()
}

func (p *chatPane) clearMessages() {
	p.msgBox.Objects = nil
	p.msgBox.Refresh()
}

func (p *chatPane) refreshForTheme() {
	if p.inputBg != nil {
		p.inputBg.FillColor = theme.Color(theme.ColorNameInputBackground)
		p.inputBg.Refresh()
	}
	p.replyLabel.Refresh()
	p.threadLabel.Refresh()
	p.title.Refresh()
	p.msgBox.Refresh()
	p.panel.Refresh()
}

func (p *chatPane) setMessages(msgs []api.Message, currentUserID, selfUserID string, onThread func(api.Message), onReply func(api.Message), onMedia func(api.File)) {
	p.msgBox.Objects = nil
	for i, m := range msgs {
		showHeader := isLastInSenderGroup(msgs, i)
		isFromMe := strings.TrimSpace(m.UserID) != "" && strings.TrimSpace(m.UserID) == strings.TrimSpace(currentUserID)
		mentionedMe := messageMentionsUser(m.Text, selfUserID)
		p.msgBox.Add(renderMessageRow(m, isFromMe, mentionedMe, onThread, onReply, onMedia, showHeader))
	}
	p.msgBox.Refresh()
	for _, d := range []time.Duration{60 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond} {
		go func(delay time.Duration) {
			time.Sleep(delay)
			fyne.Do(func() {
				p.msgScroll.ScrollToBottom()
			})
		}(d)
	}
}

func renderMessageRow(m api.Message, isFromMe bool, mentionedMe bool, onThread func(api.Message), onReply func(api.Message), onMedia func(api.File), showHeader bool) fyne.CanvasObject {
	name := senderName(m)
	ts := canvas.NewText(formatHoverTimestamp(m.Time)+" ", color.NRGBA{R: 100, G: 106, B: 130, A: 0})
	ts.TextSize = hoverTimestampTextSize()

	body := widget.NewLabel(m.Text)
	body.Wrapping = fyne.TextWrapWord
	if isFromMe {
		body.Alignment = fyne.TextAlignTrailing
		body.Importance = widget.SuccessImportance
	}
	row := container.NewVBox(alignOutgoingRow(body, isFromMe))
	rowWithMeta := container.NewVBox(row)
	openThread := func() {
		if onThread == nil {
			return
		}
		if m.ReplyCount > 0 || strings.TrimSpace(m.ThreadTS) != "" {
			onThread(m)
		}
	}

	var content *fyne.Container
	if showHeader {
		sender := canvas.NewText(name, senderColor(name, isFromMe))
		sender.TextStyle = fyne.TextStyle{Bold: true}
		sender.TextSize = hoverSenderTextSize()
		if isFromMe {
			sender.Alignment = fyne.TextAlignTrailing
			content = container.NewVBox(alignOutgoingRow(sender, true), rowWithMeta)
		} else {
			content = container.NewVBox(sender, rowWithMeta)
		}
	} else {
		content = container.NewVBox(rowWithMeta)
	}
	_ = onReply
	for _, f := range m.Files {
		ff := f
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = "file"
		}
		if f.IsImage() && strings.TrimSpace(f.BestImageURL()) != "" {
			rowWithMeta.Add(widget.NewHyperlink(name, mustParseURL(f.BestImageURL())))
			rowWithMeta.Add(widget.NewButton("Open image", func() {
				if onMedia != nil {
					onMedia(ff)
				}
			}))
			continue
		}
		if strings.TrimSpace(f.Permalink) != "" {
			rowWithMeta.Add(widget.NewHyperlink(name, mustParseURL(f.Permalink)))
		} else {
			rowWithMeta.Add(widget.NewLabel(name))
		}
	}
	rowObj := newHoverMessageRow(content, ts, openThread)
	rowCanvas := applyMessageSideIndent(rowObj)
	if !isFromMe && mentionedMe {
		bg := canvas.NewRectangle(color.NRGBA{R: 66, G: 53, B: 24, A: 120})
		return container.NewMax(bg, rowCanvas)
	}
	return rowCanvas
}

func senderName(m api.Message) string {
	if s := strings.TrimSpace(m.Username); s != "" {
		return s
	}
	if s := strings.TrimSpace(m.UserID); s != "" {
		return s
	}
	if strings.TrimSpace(m.BotID) != "" {
		return "bot"
	}
	return "unknown"
}

func senderColor(name string, isFromMe bool) color.Color {
	if isFromMe {
		return color.NRGBA{R: 125, G: 207, B: 255, A: 255}
	}
	palette := []color.NRGBA{
		{R: 122, G: 162, B: 247, A: 255},
		{R: 158, G: 206, B: 106, A: 255},
		{R: 224, G: 175, B: 104, A: 255},
		{R: 247, G: 118, B: 142, A: 255},
		{R: 187, G: 154, B: 247, A: 255},
		{R: 125, G: 207, B: 255, A: 255},
		{R: 231, G: 130, B: 132, A: 255},
		{R: 115, G: 218, B: 202, A: 255},
	}
	trimmed := strings.TrimSpace(strings.ToLower(name))
	if trimmed == "" {
		return palette[0]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(trimmed))
	return palette[int(h.Sum32())%len(palette)]
}

func threadRootTS(m api.Message) string {
	if ts := strings.TrimSpace(m.ThreadTS); ts != "" {
		return ts
	}
	return strings.TrimSpace(m.TS)
}

func isLastInSenderGroup(msgs []api.Message, idx int) bool {
	if idx+1 >= len(msgs) {
		return true
	}
	cur, next := msgs[idx], msgs[idx+1]
	if senderName(cur) != senderName(next) {
		return true
	}
	return next.Time.Sub(cur.Time) > 4*time.Minute
}

func hoverSenderTextSize() float32 {
	size := float32(theme.TextSize()) - 1
	if size < 8 {
		size = 8
	}
	return size
}

func hoverTimestampTextSize() float32 {
	size := hoverSenderTextSize() - 1
	if size < 8 {
		size = 8
	}
	return size
}

func formatHoverTimestamp(t time.Time) string {
	return t.Format("15:04")
}

func messageMentionsUser(text, userID string) bool {
	id := strings.TrimSpace(userID)
	if id == "" {
		return false
	}
	return strings.Contains(text, "<@"+id+">")
}

func applyMessageSideIndent(row fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil, fixedWidthSpacer(8), fixedWidthSpacer(8), row)
}

func fixedWidthSpacer(width float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(width, 1))
	return r
}

func alignOutgoingRow(obj fyne.CanvasObject, isFromMe bool) fyne.CanvasObject {
	if !isFromMe {
		return obj
	}
	return container.NewBorder(nil, nil, layout.NewSpacer(), nil, obj)
}

func truncate(s string, n int) string {
	if n <= 0 || len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		u, _ = url.Parse("https://slack.com")
	}
	return u
}

type paneSurface struct {
	widget.BaseWidget
	content    fyne.CanvasObject
	onActivate func()
}

func newPaneSurface(content fyne.CanvasObject, onActivate func()) *paneSurface {
	s := &paneSurface{content: content, onActivate: onActivate}
	s.ExtendBaseWidget(s)
	return s
}

func (s *paneSurface) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(s.content)
}
func (s *paneSurface) Tapped(_ *fyne.PointEvent) {
	if s.onActivate != nil {
		s.onActivate()
	}
}
func (s *paneSurface) TappedSecondary(_ *fyne.PointEvent) {
	if s.onActivate != nil {
		s.onActivate()
	}
}
func (s *paneSurface) MouseIn(_ *desktop.MouseEvent)    {}
func (s *paneSurface) MouseOut()                        {}
func (s *paneSurface) MouseMoved(_ *desktop.MouseEvent) {}

type glyph struct {
	widget.BaseWidget
	text  *canvas.Text
	onTap func()
}

func newGlyph(label string, onTap func()) *glyph {
	g := &glyph{text: canvas.NewText(label, theme.Color(theme.ColorNameForeground)), onTap: onTap}
	g.text.TextSize = 10
	g.ExtendBaseWidget(g)
	return g
}

func (g *glyph) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(g.text) }
func (g *glyph) MinSize() fyne.Size {
	s := fyne.MeasureText(g.text.Text, g.text.TextSize, fyne.TextStyle{})
	return fyne.NewSize(s.Width+4, s.Height+2)
}
func (g *glyph) Tapped(_ *fyne.PointEvent) {
	if g.onTap != nil {
		g.onTap()
	}
}
func (g *glyph) TappedSecondary(_ *fyne.PointEvent) {}

type focusEntry struct {
	widget.Entry
	onFocused  func()
	onShortcut func(fyne.Shortcut) bool
	focused    bool
}

func newFocusEntry(onFocused func(), onShortcut func(fyne.Shortcut) bool) *focusEntry {
	e := &focusEntry{onFocused: onFocused, onShortcut: onShortcut}
	e.MultiLine = true
	e.ExtendBaseWidget(e)
	return e
}

func (e *focusEntry) FocusGained() {
	e.Entry.FocusGained()
	e.focused = true
	if e.onFocused != nil {
		e.onFocused()
	}
}

func (e *focusEntry) FocusLost() {
	e.Entry.FocusLost()
	e.focused = false
}

func (e *focusEntry) IsFocused() bool { return e.focused }

func (e *focusEntry) CreateRenderer() fyne.WidgetRenderer {
	return &focusEntryRenderer{inner: e.Entry.CreateRenderer()}
}

type focusEntryRenderer struct {
	inner fyne.WidgetRenderer
}

func (r *focusEntryRenderer) Layout(size fyne.Size)        { r.inner.Layout(size) }
func (r *focusEntryRenderer) MinSize() fyne.Size           { return r.inner.MinSize() }
func (r *focusEntryRenderer) Destroy()                     { r.inner.Destroy() }
func (r *focusEntryRenderer) Objects() []fyne.CanvasObject { return r.inner.Objects() }
func (r *focusEntryRenderer) Refresh() {
	r.inner.Refresh()
	for _, obj := range r.inner.Objects() {
		clearStrokeRecursive(obj)
		clearFillRecursive(obj)
	}
}

func clearStrokeRecursive(obj fyne.CanvasObject) {
	if obj == nil {
		return
	}
	if rect, ok := obj.(*canvas.Rectangle); ok {
		if rect.StrokeWidth != 0 || rect.StrokeColor != color.Transparent {
			rect.StrokeWidth = 0
			rect.StrokeColor = color.Transparent
			rect.Refresh()
		}
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			clearStrokeRecursive(child)
		}
	}
}

func clearFillRecursive(obj fyne.CanvasObject) {
	if obj == nil {
		return
	}
	if rect, ok := obj.(*canvas.Rectangle); ok {
		if rect.FillColor != color.Transparent {
			rect.FillColor = color.Transparent
			rect.Refresh()
		}
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			clearFillRecursive(child)
		}
	}
}

func (e *focusEntry) TypedShortcut(shortcut fyne.Shortcut) {
	if e.onShortcut != nil && e.onShortcut(shortcut) {
		return
	}
	e.Entry.TypedShortcut(shortcut)
}

func (a *App) handleInputShortcut(shortcut fyne.Shortcut) bool {
	custom, ok := shortcut.(*desktop.CustomShortcut)
	if !ok {
		return false
	}
	if custom.Modifier&fyne.KeyModifierControl == 0 {
		return false
	}
	key := fyne.KeyName(strings.ToUpper(string(custom.KeyName)))
	switch key {
	case fyne.KeyName("H"):
		p := a.newPane()
		a.paneManager.splitFocused(splitHorizontal, p)
		a.focusPaneInput(a.paneManager.focusedPane())
		a.savePaneLayoutState()
		return true
	case fyne.KeyName("J"):
		p := a.newPane()
		a.paneManager.splitFocused(splitVertical, p)
		a.focusPaneInput(a.paneManager.focusedPane())
		a.savePaneLayoutState()
		return true
	case fyne.KeyName("W"):
		a.paneManager.closeFocused()
		a.focusPaneInput(a.paneManager.focusedPane())
		a.savePaneLayoutState()
		return true
	case fyne.KeyName("S"):
		a.toggleChatList()
		return true
	case fyne.KeyName("N"):
		a.openNewWindow()
		return true
	}
	return false
}

type hoverMessageRow struct {
	widget.BaseWidget
	host      *fyne.Container
	content   *fyne.Container
	tsLabel   *canvas.Text
	tsAnim    *fyne.Animation
	metaShown bool
	onTap     func()
}

func newHoverMessageRow(content *fyne.Container, tsLabel *canvas.Text, onTap func()) *hoverMessageRow {
	tsLabel.Hide()
	host := container.NewVBox(container.NewBorder(nil, nil, tsLabel, nil, content))
	r := &hoverMessageRow{
		content: content,
		tsLabel: tsLabel,
		host:    host,
		onTap:   onTap,
	}
	r.ExtendBaseWidget(r)
	return r
}

func (r *hoverMessageRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.host)
}

func (r *hoverMessageRow) MouseIn(_ *desktop.MouseEvent) {
	if r.metaShown || r.tsLabel == nil {
		return
	}
	r.metaShown = true
	r.animateTs(true)
}

func (r *hoverMessageRow) MouseOut() {
	if !r.metaShown {
		return
	}
	r.metaShown = false
	r.animateTs(false)
}

func (r *hoverMessageRow) MouseMoved(_ *desktop.MouseEvent) {}

func (r *hoverMessageRow) animateTs(visible bool) {
	if r.tsAnim != nil {
		r.tsAnim.Stop()
	}
	col := color.NRGBA{R: 100, G: 106, B: 130, A: 180}
	if visible {
		r.tsLabel.Color = color.NRGBA{R: col.R, G: col.G, B: col.B, A: 0}
		r.tsLabel.Show()
		r.host.Refresh()
		r.tsAnim = fyne.NewAnimation(120*time.Millisecond, func(f float32) {
			r.tsLabel.Color = color.NRGBA{R: col.R, G: col.G, B: col.B, A: uint8(f * 255)}
			canvas.Refresh(r.tsLabel)
		})
		r.tsAnim.Curve = fyne.AnimationEaseOut
		r.tsAnim.Start()
		return
	}
	startA := uint8(255)
	if c, ok := r.tsLabel.Color.(color.NRGBA); ok {
		startA = c.A
	}
	const dur = 110 * time.Millisecond
	r.tsAnim = fyne.NewAnimation(dur, func(f float32) {
		r.tsLabel.Color = color.NRGBA{R: col.R, G: col.G, B: col.B, A: uint8(float32(startA) * (1 - f))}
		canvas.Refresh(r.tsLabel)
	})
	r.tsAnim.Curve = fyne.AnimationEaseIn
	r.tsAnim.Start()
	time.AfterFunc(dur, func() {
		fyne.Do(func() {
			if !r.metaShown {
				r.tsLabel.Hide()
				r.host.Refresh()
			}
		})
	})
}

func (r *hoverMessageRow) Tapped(_ *fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *hoverMessageRow) TappedSecondary(_ *fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}
