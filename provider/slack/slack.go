// Package slack is the Slack backend: Web API for history and sending, Socket
// Mode for realtime. Everything Slack-shaped stops here — the UI above sees
// only models.Chat, models.Message and provider.Event.
//
// One Provider is one workspace. Slack's own client is per-workspace too, and
// a token only ever speaks for one, so several workspaces are several
// providers registered under different GUID prefixes.
package slack

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
	slackapi "github.com/slack-go/slack"
)

// Workspace is one configured Slack login.
//
// Two tokens, because Slack needs two: the user token acts as the person, so
// messages are sent by them and history is what they can see; the app-level
// token only opens the Socket Mode connection and can read nothing on its own.
type Workspace struct {
	// ID is the slug used in GUIDs. Stable across renames.
	ID string
	// Name is what the user calls this workspace.
	Name string
	// Token is the user token (xoxp-).
	Token string
	// AppToken is the app-level token (xapp-), required for realtime.
	AppToken string
}

// maxThreadRoots caps how many threads one history load expands.
//
// conversations.replies is Tier 3 — about fifty calls a minute — and a busy
// channel can easily have more threads than that in view. The newest threads
// are the ones being read, so the cap takes those and logs the rest away
// rather than spending the whole minute's budget on one pane.
const maxThreadRoots = 10

// threadFetchConcurrency keeps the expansion from arriving as a serial stall
// when a pane opens, without burst-tripping the rate limiter.
const threadFetchConcurrency = 4

// conversationTypes is what users.conversations is asked for: everything the
// user is actually in.
var conversationTypes = []string{"public_channel", "private_channel", "mpim", "im"}

// tapbacks maps the six iMessage reactions the UI can send onto Slack emoji
// names. Slack takes any emoji; the UI only offers these.
var tapbacks = map[string]string{
	"love":      "heart",
	"laugh":     "joy",
	"like":      "+1",
	"dislike":   "-1",
	"emphasize": "bangbang",
	"question":  "question",
}

// Provider is one Slack workspace.
type Provider struct {
	ws     Workspace
	api    *slackapi.Client
	users  *userCache
	selfID string

	// labelWorkspace prefixes chat names with the workspace name.
	labelWorkspace bool

	mu sync.RWMutex
	// latest ts seen per channel, which is what conversations.mark needs to
	// mark a conversation read up to.
	latest map[string]string
	// threads caches expanded threads by "channel\x00root ts".
	threads map[string]cachedThread
}

// cachedThread is a thread as last fetched, kept so the refresh loop does not
// re-fetch one that has not changed.
//
// This is what keeps the app inside Slack's rate limits. The UI reconciles
// every open pane on a timer, and each reconcile is one history call — but
// expanding N threads every time would multiply that by N, and
// conversations.replies allows about fifty calls a minute. The reply count
// comes back with the history for free, so an unchanged thread costs nothing.
type cachedThread struct {
	replyCount int
	replies    []slackapi.Msg
}

var (
	_ provider.Provider        = (*Provider)(nil)
	_ provider.AttachmentStore = (*Provider)(nil)
)

// New connects to a workspace and verifies the token. It is a network call:
// auth.test is how the workspace's own user id is learned, and without that id
// every message would look like it came from someone else.
func New(ws Workspace) (*Provider, error) {
	if strings.TrimSpace(ws.Token) == "" {
		return nil, fmt.Errorf("slack workspace %q has no user token", ws.Name)
	}
	if strings.TrimSpace(ws.ID) == "" {
		return nil, errors.New("slack workspace has no id")
	}

	options := []slackapi.Option{}
	if strings.TrimSpace(ws.AppToken) != "" {
		options = append(options, slackapi.OptionAppLevelToken(ws.AppToken))
	}
	api := slackapi.New(ws.Token, options...)

	auth, err := api.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("slack auth failed for %q: %w", ws.Name, err)
	}

	return newProvider(ws, api, auth.UserID), nil
}

// newProvider is the one place a Provider is assembled, so a new piece of
// state cannot be forgotten by a second construction site.
func newProvider(ws Workspace, api *slackapi.Client, selfID string) *Provider {
	return &Provider{
		ws:      ws,
		api:     api,
		users:   newUserCache(api),
		selfID:  selfID,
		latest:  make(map[string]string),
		threads: make(map[string]cachedThread),
	}
}

// ID identifies this backend in logs and errors.
func (p *Provider) ID() string { return "slack:" + p.ws.ID }

// GUIDPrefix is what this workspace's conversations are registered under.
func (p *Provider) GUIDPrefix() string { return Prefix + p.ws.ID + sep }

// SelfUserID is the Slack id of the logged-in user.
func (p *Provider) SelfUserID() string { return p.selfID }

// ShowWorkspaceInNames prefixes this workspace's chats with its name.
//
// Off by default, because with one workspace the prefix is only noise. With
// two it is the difference between two conversations called "Anna" and two
// channels called "#general" being tellable apart at all — the chat list is
// flat, so the name is the only place that distinction can live.
func (p *Provider) ShowWorkspaceInNames(show bool) { p.labelWorkspace = show }

// Chats lists the conversations the user is in.
//
// Ordering is by kind, then name: people and group chats first, then channels,
// each alphabetically. Slack's conversation list carries no last-activity
// timestamp — getting one would cost a history call per conversation — so
// sorting by recency the way iMessage does is not on offer, and a stable
// alphabetical list beats an arbitrary one.
func (p *Provider) Chats(limit int) ([]models.Chat, error) {
	p.users.loadAll()

	var (
		all    []slackapi.Channel
		cursor string
	)
	for {
		page := limit - len(all)
		if page <= 0 || page > 200 {
			page = 200
		}
		// users.conversations, not conversations.list: the latter returns
		// every channel in the workspace, including thousands the user has
		// never joined.
		channels, next, err := p.api.GetConversationsForUser(&slackapi.GetConversationsForUserParameters{
			Types:           conversationTypes,
			ExcludeArchived: true,
			Limit:           page,
			Cursor:          cursor,
		})
		if err != nil {
			if len(all) > 0 {
				// Partial is better than nothing: the pages already fetched
				// are complete conversations, and the next refresh retries.
				log.Printf("[slack] %s: conversations page failed after %d: %v", p.ID(), len(all), err)
				break
			}
			return nil, fmt.Errorf("slack conversations: %w", err)
		}
		all = append(all, channels...)
		cursor = next
		if cursor == "" || (limit > 0 && len(all) >= limit) {
			break
		}
	}

	// The kind is kept beside the chat while sorting rather than read back out
	// of the display name: the name may carry a workspace prefix, and sniffing
	// it for a '#' silently mis-sorted every chat as soon as it did.
	type ranked struct {
		chat models.Chat
		kind kind
	}
	listed := make([]ranked, 0, len(all))
	for i := range all {
		channel := &all[i]
		conversationKind := p.classify(channel)
		if conversationKind == kindBot {
			// Bots and app DMs are noise in a conversation list; the desktop
			// client hides them for the same reason.
			continue
		}
		listed = append(listed, ranked{
			kind: conversationKind,
			chat: models.Chat{
				GUID:           ChatGUID(p.ws.ID, channel.ID),
				DisplayName:    p.chatName(channel, conversationKind),
				ChatIdentifier: channel.ID,
				UnreadCount:    channel.UnreadCount,
			},
		})
	}

	sort.SliceStable(listed, func(i, j int) bool {
		left, right := kindRank(listed[i].kind), kindRank(listed[j].kind)
		if left != right {
			return left < right
		}
		return strings.ToLower(listed[i].chat.DisplayName) < strings.ToLower(listed[j].chat.DisplayName)
	})

	chats := make([]models.Chat, 0, len(listed))
	for _, entry := range listed {
		chats = append(chats, entry.chat)
	}
	return chats, nil
}

// kindRank puts conversations with people above channels: a DM is what you
// scan a list for, a channel is what you go looking for by name.
func kindRank(k kind) int {
	switch k {
	case kindDirect:
		return 0
	case kindGroup:
		return 1
	default:
		return 2
	}
}

// Messages returns a channel's recent history, oldest first, with threads
// expanded inline.
func (p *Provider) Messages(chatGUID string, limit int) ([]models.Message, error) {
	channelID, err := p.channelOf(chatGUID)
	if err != nil {
		return nil, err
	}
	// One users.list beats a users.info per sender; after the first call this
	// is free. A pane can open before the chat list has ever loaded.
	p.users.loadAll()

	history, err := p.api.GetConversationHistory(&slackapi.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("slack history for %s: %w", channelID, err)
	}

	// Slack returns newest first.
	raw := make([]slackapi.Msg, 0, len(history.Messages)*2)
	for i := len(history.Messages) - 1; i >= 0; i-- {
		raw = append(raw, history.Messages[i].Msg)
	}
	raw = append(raw, p.threadReplies(channelID, raw)...)

	names := p.resolveSenders(raw)
	out := make([]models.Message, 0, len(raw))
	for i := range raw {
		out = append(out, p.toMessage(chatGUID, channelID, &raw[i], names))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DateCreated < out[j].DateCreated })

	p.rememberLatest(channelID, out)
	return out, nil
}

// threadReplies expands the newest threads in a history window.
//
// Slack keeps replies out of the channel history unless they were explicitly
// broadcast, so without this a thread reads as a question nobody answered.
func (p *Provider) threadReplies(channelID string, history []slackapi.Msg) []slackapi.Msg {
	type root struct {
		ts    string
		count int
	}
	var roots []root
	// Newest first, so the cap keeps the threads currently being read.
	for i := len(history) - 1; i >= 0; i-- {
		msg := &history[i]
		if msg.ReplyCount > 0 && msg.Timestamp != "" {
			roots = append(roots, root{ts: msg.Timestamp, count: msg.ReplyCount})
		}
	}
	if len(roots) == 0 {
		return nil
	}
	if len(roots) > maxThreadRoots {
		log.Printf("[slack] %s: %d threads in view, expanding the %d newest", channelID, len(roots), maxThreadRoots)
		roots = roots[:maxThreadRoots]
	}

	var (
		mu      sync.Mutex
		replies []slackapi.Msg
		wg      sync.WaitGroup
	)
	slots := make(chan struct{}, threadFetchConcurrency)
	for _, r := range roots {
		if cached, ok := p.cachedThread(channelID, r.ts, r.count); ok {
			replies = append(replies, cached...)
			continue
		}
		wg.Add(1)
		go func(threadTS string, replyCount int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()

			msgs, _, _, err := p.api.GetConversationReplies(&slackapi.GetConversationRepliesParameters{
				ChannelID: channelID,
				Timestamp: threadTS,
			})
			if err != nil {
				log.Printf("[slack] thread %s in %s failed: %v", threadTS, channelID, err)
				return
			}
			fetched := make([]slackapi.Msg, 0, len(msgs))
			for i := range msgs {
				// The first entry is the root, which the history already has.
				if msgs[i].Timestamp == threadTS {
					continue
				}
				fetched = append(fetched, msgs[i].Msg)
			}
			p.rememberThread(channelID, threadTS, replyCount, fetched)

			mu.Lock()
			defer mu.Unlock()
			replies = append(replies, fetched...)
		}(r.ts, r.count)
	}
	wg.Wait()
	return replies
}

// cachedThread returns the stored replies when the thread still has the same
// number of them. A changed count is the signal that it needs refetching.
func (p *Provider) cachedThread(channelID, rootTS string, replyCount int) ([]slackapi.Msg, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	entry, ok := p.threads[threadKey(channelID, rootTS)]
	if !ok || entry.replyCount != replyCount {
		return nil, false
	}
	return entry.replies, true
}

func (p *Provider) rememberThread(channelID, rootTS string, replyCount int, replies []slackapi.Msg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.threads[threadKey(channelID, rootTS)] = cachedThread{replyCount: replyCount, replies: replies}
}

func threadKey(channelID, rootTS string) string { return channelID + "\x00" + rootTS }

// resolveSenders warms the name cache for everyone in a batch before any
// message is rendered, so the mention map is complete when mrkdwn runs.
func (p *Provider) resolveSenders(msgs []slackapi.Msg) map[string]string {
	for i := range msgs {
		if msgs[i].User != "" {
			p.users.name(msgs[i].User)
		}
	}
	return p.users.snapshot()
}

func (p *Provider) toMessage(chatGUID, channelID string, msg *slackapi.Msg, names map[string]string) models.Message {
	text := TextToPlain(msg.Text, names)
	// A reply carries no visual link to its root in a flat message list, so it
	// gets a marker instead of a thread pane.
	if msg.ThreadTimestamp != "" && msg.ThreadTimestamp != msg.Timestamp {
		text = "↳ " + text
	}

	out := models.Message{
		GUID:        MessageGUID(p.ws.ID, channelID, msg.Timestamp),
		Text:        text,
		IsFromMe:    msg.User != "" && msg.User == p.selfID,
		DateCreated: TSToMillis(msg.Timestamp),
		ChatGUID:    chatGUID,
		Attachments: p.attachments(msg),
	}
	if sender := p.senderName(msg); sender != "" && !out.IsFromMe {
		out.Handle = &models.Handle{DisplayName: sender, Address: msg.User}
	}
	if counts := reactionCounts(msg); len(counts) > 0 {
		out.ReactionCounts = counts
	}
	return out
}

// senderName resolves who wrote a message. Apps and webhooks have no user id,
// only a bot id and sometimes a username of their own.
func (p *Provider) senderName(msg *slackapi.Msg) string {
	if msg.User != "" {
		return p.users.name(msg.User)
	}
	if strings.TrimSpace(msg.Username) != "" {
		return msg.Username
	}
	if msg.BotID != "" {
		return p.users.botName(msg.BotID)
	}
	return ""
}

func (p *Provider) attachments(msg *slackapi.Msg) []models.Attachment {
	if len(msg.Files) == 0 {
		return nil
	}
	out := make([]models.Attachment, 0, len(msg.Files))
	for i := range msg.Files {
		file := &msg.Files[i]
		if file.URLPrivate == "" {
			// Nothing to fetch — a tombstone for a deleted or restricted file.
			continue
		}
		name := file.Name
		if name == "" {
			name = file.Title
		}
		out = append(out, models.Attachment{
			GUID:     FileGUID(p.ws.ID, file.ID),
			MimeType: file.Mimetype,
			FileName: name,
			// URL is deliberately left empty: it is handed to the system
			// opener, and Slack's private URL is a 302 to a login page
			// without the workspace token.
			SourceURL: file.URLPrivate,
		})
	}
	return out
}

// reactionCounts folds Slack reactions into the same shape the renderer uses
// for iMessage tapbacks, so they draw identically.
func reactionCounts(msg *slackapi.Msg) map[string]int {
	if len(msg.Reactions) == 0 {
		return nil
	}
	counts := make(map[string]int, len(msg.Reactions))
	for _, reaction := range msg.Reactions {
		glyph := replaceShortcodes(":" + reaction.Name + ":")
		counts[glyph] += reaction.Count
	}
	return counts
}

// Send posts to a channel. replyToGUID, when it names a Slack message, makes
// this a threaded reply to it.
func (p *Provider) Send(chatGUID, text, replyToGUID, _ string) error {
	channelID, err := p.channelOf(chatGUID)
	if err != nil {
		return err
	}

	options := []slackapi.MsgOption{
		slackapi.MsgOptionText(text, false),
		slackapi.MsgOptionAsUser(true),
	}
	if _, _, ts, ok := ParseMessageGUID(replyToGUID); ok {
		options = append(options, slackapi.MsgOptionTS(ts))
	}

	if _, _, err := p.api.PostMessage(channelID, options...); err != nil {
		return fmt.Errorf("slack send: %w", err)
	}
	return nil
}

// React adds one of the UI's six tapbacks as the matching Slack emoji.
func (p *Provider) React(chatGUID, messageGUID, reaction string) error {
	channelID, err := p.channelOf(chatGUID)
	if err != nil {
		return err
	}
	_, _, ts, ok := ParseMessageGUID(messageGUID)
	if !ok {
		return fmt.Errorf("slack react: %q is not a slack message", messageGUID)
	}
	name, ok := tapbacks[reaction]
	if !ok {
		// Anything else the UI gains later is already a Slack emoji name.
		name = strings.Trim(reaction, ":")
	}
	if err := p.api.AddReaction(name, slackapi.ItemRef{Channel: channelID, Timestamp: ts}); err != nil {
		return fmt.Errorf("slack react: %w", err)
	}
	return nil
}

// MarkRead marks a conversation read up to the newest message this client has
// seen. Slack marks by timestamp, so a conversation whose history was never
// loaded has nothing to mark and is left alone.
func (p *Provider) MarkRead(chatGUID string) error {
	channelID, err := p.channelOf(chatGUID)
	if err != nil {
		return err
	}
	p.mu.RLock()
	ts := p.latest[channelID]
	p.mu.RUnlock()
	if ts == "" {
		return nil
	}
	if err := p.api.MarkConversation(channelID, ts); err != nil {
		return fmt.Errorf("slack mark read: %w", err)
	}
	return nil
}

// DownloadAttachment fetches a Slack file. The URL needs the workspace token,
// which is why this never goes through the system opener.
func (p *Provider) DownloadAttachment(att models.Attachment) ([]byte, string, error) {
	if att.SourceURL == "" {
		return nil, "", fmt.Errorf("slack attachment %s has no url", att.GUID)
	}
	var buf bytes.Buffer
	if err := p.api.GetFile(att.SourceURL, &buf); err != nil {
		return nil, "", fmt.Errorf("slack file: %w", err)
	}
	return buf.Bytes(), att.MimeType, nil
}

// channelOf extracts the channel id from a GUID and rejects one belonging to a
// different workspace — with several workspaces connected, a mixed-up id would
// silently post to the wrong company.
func (p *Provider) channelOf(chatGUID string) (string, error) {
	workspaceID, channelID, ok := ParseChatGUID(chatGUID)
	if !ok {
		return "", fmt.Errorf("%q is not a slack chat guid", chatGUID)
	}
	if workspaceID != p.ws.ID {
		return "", fmt.Errorf("chat %s belongs to workspace %q, not %q", chatGUID, workspaceID, p.ws.ID)
	}
	return channelID, nil
}

func (p *Provider) rememberLatest(channelID string, msgs []models.Message) {
	var newest string
	for i := range msgs {
		if _, _, ts, ok := ParseMessageGUID(msgs[i].GUID); ok && ts > newest {
			newest = ts
		}
	}
	if newest == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if newest > p.latest[channelID] {
		p.latest[channelID] = newest
	}
}

// conversation kinds, mirroring how Slack itself groups a sidebar.
type kind int

const (
	kindPublic kind = iota
	kindPrivate
	kindShared
	kindGroup
	kindDirect
	kindBot
)

func (p *Provider) classify(channel *slackapi.Channel) kind {
	switch {
	case channel.IsIM:
		if p.users.isBot(channel.User) {
			return kindBot
		}
		return kindDirect
	case channel.IsMpIM:
		return kindGroup
	case channel.IsExtShared, channel.IsOrgShared, channel.IsShared:
		return kindShared
	case channel.IsPrivate:
		return kindPrivate
	default:
		return kindPublic
	}
}

// chatName is what the chat list shows. Channels keep Slack's own '#' so they
// read as channels at a glance; people keep their names.
func (p *Provider) chatName(channel *slackapi.Channel, kind kind) string {
	name := p.bareChatName(channel, kind)
	if p.labelWorkspace && p.ws.Name != "" {
		return p.ws.Name + " " + name
	}
	return name
}

func (p *Provider) bareChatName(channel *slackapi.Channel, kind kind) string {
	switch kind {
	case kindDirect:
		if name := p.users.name(channel.User); name != "" {
			return name
		}
		return channel.User
	case kindGroup:
		return prettyGroupName(channel.Name)
	default:
		if channel.Name == "" {
			return channel.ID
		}
		return "#" + channel.Name
	}
}

// prettyGroupName turns Slack's internal group-DM name into the members.
// Slack names them "mpdm-anna--bob--carol-1", which is unreadable in a list.
func prettyGroupName(name string) string {
	if !strings.HasPrefix(name, "mpdm-") {
		return name
	}
	trimmed := strings.TrimPrefix(name, "mpdm-")
	if i := strings.LastIndex(trimmed, "-"); i > 0 {
		trimmed = trimmed[:i]
	}
	members := strings.Split(trimmed, "--")
	if len(members) == 0 || members[0] == "" {
		return name
	}
	return strings.Join(members, ", ")
}
