// Package telegram is the Telegram backend, over MTProto with gotd/td.
// Everything Telegram-shaped stops here — the UI above sees only models.Chat,
// models.Message and provider.Event.
//
// One Provider is one logged-in account, registered under tg:<account id>:.
// TELEGRAM.md has the design and its reasons.
package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/oovets/imessage-tui/models"
	"github.com/oovets/imessage-tui/provider"
)

// callTimeout bounds every call the UI makes. The UI runs them as commands,
// and a call that never returns would leave a pane showing "loading" forever.
const callTimeout = 30 * time.Second

// maxDialogs is what one messages.getDialogs page can return.
const maxDialogs = 100

// historyCacheTTL is the backstop on the history cache. The realtime feed is
// what keeps the cache honest (see cachedHistory); this only bounds how long a
// change the server never pushed — a view count, someone else's reaction in a
// big group — can stay unseen.
const historyCacheTTL = 2 * time.Minute

// tapbacks maps the UI's six reactions onto Telegram's standard set. ‼️ and
// ❓ are not in it, so emphasize and question take the nearest that are. The
// heart is U+2764 without a variation selector: that is the exact string
// Telegram knows, and "❤️" is rejected as an unknown reaction.
var tapbacks = map[string]string{
	"love":      "❤",
	"laugh":     "😁",
	"like":      "👍",
	"dislike":   "👎",
	"emphasize": "🔥",
	"question":  "🤔",
}

// Account is one logged-in Telegram account.
type Account struct {
	// ID is the numeric user id, as it appears in GUIDs.
	ID string
	// Name is the account owner's name, for labels.
	Name string
}

// updatesHandler is the part of gotd's updates.Manager the provider feeds: the
// results of its own calls carry updates too, and the manager has to see them
// to keep its sequence numbers straight and to dispatch the sent message.
type updatesHandler interface {
	Handle(ctx context.Context, u tg.UpdatesClass) error
	HandleAffected(ctx context.Context, channelID int64, pts, ptsCount int) error
}

// Provider is one Telegram account.
type Provider struct {
	account Account
	api     *tg.Client
	sender  *message.Sender
	selfID  int64
	peers   *peerTable
	updates updatesHandler // nil when there is no realtime connection

	// labelAccount prefixes chat names with the account name.
	labelAccount bool

	// stream is the realtime feed; nil without a connection.
	stream *Stream
	// stop shuts the connection down; nil without one.
	stop context.CancelFunc

	mu sync.Mutex
	// latest is the newest message id seen per chat — what readHistory marks
	// read up to.
	latest map[Peer]int
	// history caches each chat's last load; see cachedHistory.
	history map[Peer]cachedHistory
	// live is whether the realtime feed is up. The history cache is only
	// trusted while it is.
	live bool
	// reloadedAt is when Chats last ran, so a burst of lookups for unknown
	// peers costs one dialogs call, not one each.
	reloadedAt time.Time
}

// cachedHistory is one chat's history as last loaded.
//
// The UI re-reads every open pane every poll interval and every cached chat
// after a reconnect. Against Telegram that is a FLOOD_WAIT waiting to happen,
// and with a live feed it is also pointless: any change to the chat arrives as
// an update, and an update for the chat drops its entry.
type cachedHistory struct {
	limit    int
	messages []models.Message
	at       time.Time
}

var (
	_ provider.Provider        = (*Provider)(nil)
	_ provider.AttachmentStore = (*Provider)(nil)
	_ provider.Labeler         = (*Provider)(nil)
)

// newProvider is the one place a Provider is assembled, for New and tests.
func newProvider(account Account, api *tg.Client, selfID int64) *Provider {
	return &Provider{
		account: account,
		api:     api,
		sender:  message.NewSender(api),
		selfID:  selfID,
		peers:   newPeerTable(selfID),
		latest:  make(map[Peer]int),
		history: make(map[Peer]cachedHistory),
	}
}

// ID identifies this backend in logs and errors.
func (p *Provider) ID() string { return "telegram:" + p.account.ID }

// Label is what the chat list calls this account.
func (p *Provider) Label() string {
	if p.labelAccount && p.account.Name != "" {
		return "Telegram · " + p.account.Name
	}
	return "Telegram"
}

// GUIDPrefix is what this account's chats are registered under.
func (p *Provider) GUIDPrefix() string { return Prefix + p.account.ID + sep }

// ShowAccountInNames labels this account's chats and network with its owner's
// name — off with one account, where it is noise, and the only way to tell two
// apart with more.
func (p *Provider) ShowAccountInNames(show bool) { p.labelAccount = show }

// Close shuts down the connection.
func (p *Provider) Close() {
	if p.stop != nil {
		p.stop()
	}
}

// Chats lists the account's dialogs, most recently active first — the order
// Telegram keeps them in, so unlike Slack the caller's limit means what it says.
func (p *Provider) Chats(limit int) ([]models.Chat, error) {
	if limit <= 0 || limit > maxDialogs {
		limit = maxDialogs
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	result, err := p.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      limit,
	})
	if err != nil {
		return nil, fmt.Errorf("telegram dialogs: %w", err)
	}
	p.mu.Lock()
	p.reloadedAt = time.Now()
	p.mu.Unlock()
	dialogs, ok := result.AsModified()
	if !ok {
		return nil, errors.New("telegram dialogs: server sent no dialogs")
	}
	p.peers.apply(dialogs.GetUsers(), dialogs.GetChats())

	// The dialogs carry the id of their newest message; the messages
	// themselves come in the same response.
	top := make(map[string]*tg.Message)
	for _, m := range dialogs.GetMessages() {
		if msg, ok := m.(*tg.Message); ok {
			if peer, ok := peerOf(msg.PeerID); ok {
				top[peer.String()+sep+strconv.Itoa(msg.ID)] = msg
			}
		}
	}

	chats := make([]models.Chat, 0, len(dialogs.GetDialogs()))
	for _, d := range dialogs.GetDialogs() {
		dialog, ok := d.(*tg.Dialog)
		if !ok {
			// A folder entry: the archive, shown as one row in Telegram.
			continue
		}
		peer, ok := peerOf(dialog.Peer)
		if !ok {
			continue
		}
		chat := models.Chat{
			GUID:           ChatGUID(p.account.ID, peer),
			DisplayName:    p.chatName(peer),
			ChatIdentifier: peer.String(),
			UnreadCount:    dialog.UnreadCount,
		}
		if msg := top[peer.String()+sep+strconv.Itoa(dialog.TopMessage)]; msg != nil {
			converted := p.toMessage(peer, msg)
			chat.LastMessageText = previewText(converted)
			chat.LastMessageTime = converted.DateCreated
		}
		p.rememberLatest(peer, dialog.TopMessage)
		chats = append(chats, chat)
	}
	return chats, nil
}

func (p *Provider) chatName(peer Peer) string {
	name := p.peers.chatName(peer)
	if p.labelAccount && p.account.Name != "" {
		return p.account.Name + " " + name
	}
	return name
}

// previewText is the chat list's one-line summary of a message.
func previewText(msg models.Message) string {
	if text := strings.TrimSpace(msg.Text); text != "" {
		return text
	}
	if len(msg.Attachments) > 0 {
		return "[" + attachmentKind(msg.Attachments[0].MimeType) + "]"
	}
	return ""
}

func attachmentKind(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "photo"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	default:
		return "file"
	}
}

// Messages returns a chat's recent history, oldest first.
func (p *Provider) Messages(chatGUID string, limit int) ([]models.Message, error) {
	peer, err := p.peerOf(chatGUID)
	if err != nil {
		return nil, err
	}
	if cached, ok := p.cachedHistory(peer, limit); ok {
		return cached, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	input, err := p.inputPeer(peer)
	if err != nil {
		return nil, err
	}
	result, err := p.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: input, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("telegram history for %s: %w", peer, err)
	}
	history, ok := result.AsModified()
	if !ok {
		return nil, fmt.Errorf("telegram history for %s: server sent nothing", peer)
	}
	p.peers.apply(history.GetUsers(), history.GetChats())

	out := make([]models.Message, 0, len(history.GetMessages()))
	newest := 0
	for _, m := range history.GetMessages() {
		msg, ok := m.(*tg.Message)
		if !ok {
			// Service messages (joins, pins, title changes) and empty
			// placeholders have nothing the pane can show.
			continue
		}
		out = append(out, p.toMessage(peer, msg))
		if msg.ID > newest {
			newest = msg.ID
		}
	}
	// Telegram returns newest first.
	sort.SliceStable(out, func(i, j int) bool { return out[i].DateCreated < out[j].DateCreated })
	p.rememberLatest(peer, newest)
	p.storeHistory(peer, limit, out)
	return out, nil
}

// toMessage converts a wire message. peer is the chat it belongs to.
func (p *Provider) toMessage(peer Peer, msg *tg.Message) models.Message {
	out := models.Message{
		GUID:        MessageGUID(p.account.ID, peer, msg.ID),
		Text:        msg.Message,
		IsFromMe:    msg.Out,
		DateCreated: int64(msg.Date) * 1000,
		ChatGUID:    ChatGUID(p.account.ID, peer),
	}
	attachment, label := p.media(peer, msg)
	if attachment != nil {
		out.Attachments = []models.Attachment{*attachment}
	}
	if label != "" {
		out.Text = strings.TrimSpace(label + " " + out.Text)
	}
	if !out.IsFromMe {
		if name := p.peers.senderName(p.authorOf(peer, msg)); name != "" {
			out.Handle = &models.Handle{DisplayName: name, Address: p.authorOf(peer, msg).String()}
		}
	}
	if counts := reactionCounts(msg.Reactions); len(counts) > 0 {
		out.ReactionCounts = counts
	}
	return out
}

// authorOf is who wrote a message. In a private chat the sender is often left
// out, because it can only be the other person; a channel post comes from the
// channel.
func (p *Provider) authorOf(chat Peer, msg *tg.Message) Peer {
	if author, ok := peerOf(msg.FromID); ok {
		return author
	}
	return chat
}

// media turns a message's media into an attachment the pane can open, or a
// bracketed label for what has nothing to open.
func (p *Provider) media(peer Peer, msg *tg.Message) (*models.Attachment, string) {
	media := msg.Media
	if media == nil {
		return nil, ""
	}
	guid := FileGUID(p.account.ID, peer, msg.ID)
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		if m.Photo == nil {
			return nil, "[photo expired]"
		}
		return &models.Attachment{GUID: guid, MimeType: "image/jpeg", FileName: fmt.Sprintf("photo-%d.jpg", msg.ID)}, ""
	case *tg.MessageMediaDocument:
		doc, ok := m.Document.(*tg.Document)
		if !ok {
			return nil, "[file expired]"
		}
		name := ""
		for _, attr := range doc.Attributes {
			switch a := attr.(type) {
			case *tg.DocumentAttributeSticker:
				// A sticker is a picture of an emoji; the emoji says the same
				// thing in a terminal.
				return nil, strings.TrimSpace("[sticker " + a.Alt + "]")
			case *tg.DocumentAttributeFilename:
				name = a.FileName
			case *tg.DocumentAttributeAudio:
				if a.Voice && name == "" {
					name = fmt.Sprintf("voice-%d.ogg", msg.ID)
				}
			}
		}
		if name == "" {
			name = fmt.Sprintf("file-%d", msg.ID)
		}
		return &models.Attachment{GUID: guid, MimeType: doc.MimeType, FileName: name}, ""
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		return nil, "[location]"
	case *tg.MessageMediaContact:
		return nil, strings.TrimSpace("[contact " + strings.TrimSpace(m.FirstName+" "+m.LastName) + "]")
	case *tg.MessageMediaPoll:
		return nil, "[poll]"
	case *tg.MessageMediaWebPage:
		// The link is in the text already, and the UI previews links itself.
		return nil, ""
	default:
		return nil, "[" + strings.TrimPrefix(media.TypeName(), "messageMedia") + "]"
	}
}

// reactionCounts folds Telegram's reaction counts into the shape the renderer
// uses for iMessage tapbacks.
func reactionCounts(r tg.MessageReactions) map[string]int {
	if len(r.Results) == 0 {
		return nil
	}
	counts := make(map[string]int, len(r.Results))
	for _, result := range r.Results {
		switch reaction := result.Reaction.(type) {
		case *tg.ReactionEmoji:
			counts[reaction.Emoticon] += result.Count
		case *tg.ReactionCustomEmoji:
			// A Premium custom emoji is an image with no Unicode form.
			counts["★"] += result.Count
		case *tg.ReactionPaid:
			counts["⭐"] += result.Count
		}
	}
	return counts
}

// Send posts text to a chat, as a reply when replyToGUID names a message in
// it.
func (p *Provider) Send(chatGUID, text, replyToGUID, _ string) error {
	peer, err := p.peerOf(chatGUID)
	if err != nil {
		return err
	}
	input, err := p.inputPeer(peer)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	builder := &p.sender.To(input).Builder
	if _, replyPeer, msgID, ok := ParseMessageGUID(replyToGUID); ok && replyPeer == peer {
		builder = builder.Reply(msgID)
	}
	result, err := builder.Text(ctx, text)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	p.handleOwnUpdates(ctx, result)
	if sent, ok := result.(*tg.UpdateShortSentMessage); ok {
		p.onMessage(provider.EventNewMessage, tg.Entities{}, sentMessage(peer, text, sent))
	}
	return nil
}

// sentMessage rebuilds the message a send to a private chat or basic group
// created. Telegram answers those with UpdateShortSentMessage — id, date and
// any media it attached, nothing else, since the caller knows the rest — and
// never pushes the message itself back to this session. gotd passes it on as
// an empty message for that reason, so without this the sent message reaches
// the UI only when the chat's history is next fetched.
func sentMessage(peer Peer, text string, u *tg.UpdateShortSentMessage) *tg.Message {
	return &tg.Message{
		ID:       u.ID,
		Out:      true,
		PeerID:   peer.tgPeer(),
		Date:     u.Date,
		Message:  text,
		Media:    u.Media,
		Entities: u.Entities,
	}
}

// React adds one of the UI's six reactions.
func (p *Provider) React(chatGUID, messageGUID, reaction string) error {
	peer, err := p.peerOf(chatGUID)
	if err != nil {
		return err
	}
	_, msgPeer, msgID, ok := ParseMessageGUID(messageGUID)
	if !ok || msgPeer != peer {
		return fmt.Errorf("telegram react: %q is not a message in %s", messageGUID, chatGUID)
	}
	emoticon, ok := tapbacks[reaction]
	if !ok {
		emoticon = reaction
	}
	input, err := p.inputPeer(peer)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	result, err := p.api.MessagesSendReaction(ctx, &tg.MessagesSendReactionRequest{
		Peer:     input,
		MsgID:    msgID,
		Reaction: []tg.ReactionClass{&tg.ReactionEmoji{Emoticon: emoticon}},
	})
	if err != nil {
		return fmt.Errorf("telegram react: %w", err)
	}
	p.handleOwnUpdates(ctx, result)
	return nil
}

// MarkRead marks a chat read up to the newest message this client has seen.
// A chat it has seen nothing of has nothing to mark.
func (p *Provider) MarkRead(chatGUID string) error {
	peer, err := p.peerOf(chatGUID)
	if err != nil {
		return err
	}
	p.mu.Lock()
	maxID := p.latest[peer]
	p.mu.Unlock()
	if maxID == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if peer.Kind == kindChannel {
		channel, ok := p.peers.inputChannel(peer.ID)
		if !ok {
			return fmt.Errorf("telegram mark read: channel %d not known yet", peer.ID)
		}
		if _, err := p.api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{Channel: channel, MaxID: maxID}); err != nil {
			return fmt.Errorf("telegram mark read: %w", err)
		}
		return nil
	}
	input, err := p.inputPeer(peer)
	if err != nil {
		return err
	}
	affected, err := p.api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{Peer: input, MaxID: maxID})
	if err != nil {
		return fmt.Errorf("telegram mark read: %w", err)
	}
	if p.updates != nil {
		if err := p.updates.HandleAffected(ctx, 0, affected.Pts, affected.PtsCount); err != nil {
			log.Printf("[telegram] %s: affected messages: %v", p.ID(), err)
		}
	}
	return nil
}

// DownloadAttachment fetches a photo or file.
//
// The message is read again first: the file reference that addresses a file
// on Telegram's servers expires, and one captured when the pane rendered may
// be hours old by the time someone opens it.
func (p *Provider) DownloadAttachment(att models.Attachment) ([]byte, string, error) {
	account, peer, msgID, ok := parseFileGUID(att.GUID)
	if !ok || account != p.account.ID {
		return nil, "", fmt.Errorf("telegram attachment %q is not this account's", att.GUID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*callTimeout)
	defer cancel()

	msg, err := p.fetchMessage(ctx, peer, msgID)
	if err != nil {
		return nil, "", err
	}
	location, mime, err := fileLocation(msg)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	if _, err := downloader.NewDownloader().Download(p.api, location).Stream(ctx, &buf); err != nil {
		return nil, "", fmt.Errorf("telegram download: %w", err)
	}
	if mime == "" {
		mime = att.MimeType
	}
	return buf.Bytes(), mime, nil
}

// fetchMessage reads one message fresh from the server.
func (p *Provider) fetchMessage(ctx context.Context, peer Peer, msgID int) (*tg.Message, error) {
	ids := []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}}
	var (
		result tg.MessagesMessagesClass
		err    error
	)
	if peer.Kind == kindChannel {
		channel, ok := p.peers.inputChannel(peer.ID)
		if !ok {
			return nil, fmt.Errorf("telegram: channel %d not known yet", peer.ID)
		}
		result, err = p.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: channel, ID: ids})
	} else {
		result, err = p.api.MessagesGetMessages(ctx, ids)
	}
	if err != nil {
		return nil, fmt.Errorf("telegram message %d: %w", msgID, err)
	}
	messages, ok := result.AsModified()
	if !ok {
		return nil, fmt.Errorf("telegram message %d: server sent nothing", msgID)
	}
	p.peers.apply(messages.GetUsers(), messages.GetChats())
	for _, m := range messages.GetMessages() {
		if msg, ok := m.(*tg.Message); ok && msg.ID == msgID {
			return msg, nil
		}
	}
	return nil, fmt.Errorf("telegram message %d is gone", msgID)
}

// fileLocation addresses the file behind a message's media: the largest size
// of a photo, the document itself otherwise.
func fileLocation(msg *tg.Message) (tg.InputFileLocationClass, string, error) {
	switch m := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := m.Photo.(*tg.Photo)
		if !ok {
			break
		}
		size := largestPhotoSize(photo.Sizes)
		if size == "" {
			break
		}
		return &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     size,
		}, "image/jpeg", nil
	case *tg.MessageMediaDocument:
		doc, ok := m.Document.(*tg.Document)
		if !ok {
			break
		}
		return &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		}, doc.MimeType, nil
	}
	return nil, "", fmt.Errorf("telegram message %d has no file", msg.ID)
}

// largestPhotoSize picks the size type of the biggest rendition. Stripped and
// cached sizes are inline thumbnails, not downloadable files.
func largestPhotoSize(sizes []tg.PhotoSizeClass) string {
	best, bestArea := "", -1
	for _, s := range sizes {
		var kind string
		var area int
		switch size := s.(type) {
		case *tg.PhotoSize:
			kind, area = size.Type, size.W*size.H
		case *tg.PhotoSizeProgressive:
			kind, area = size.Type, size.W*size.H
		default:
			continue
		}
		if area > bestArea {
			best, bestArea = kind, area
		}
	}
	return best
}

// handleOwnUpdates passes the updates a call returned to the updates manager.
// The server does not push this session's own sends back to it; they arrive
// only here, which keeps the manager's sequence numbers unbroken. A send to a
// channel or supergroup carries the whole message, which then reaches the UI
// as an event and replaces the optimistic copy; a send anywhere else carries
// only its id, and Send has to rebuild the message (see sentMessage).
func (p *Provider) handleOwnUpdates(ctx context.Context, result tg.UpdatesClass) {
	if p.updates == nil || result == nil {
		return
	}
	if err := p.updates.Handle(ctx, result); err != nil {
		log.Printf("[telegram] %s: own updates: %v", p.ID(), err)
	}
}

// peerOf extracts the chat from a GUID and rejects one from another account —
// with two accounts logged in, a mixed-up id would send from the wrong one.
func (p *Provider) peerOf(chatGUID string) (Peer, error) {
	account, peer, ok := ParseChatGUID(chatGUID)
	if !ok {
		return Peer{}, fmt.Errorf("%q is not a telegram chat guid", chatGUID)
	}
	if account != p.account.ID {
		return Peer{}, fmt.Errorf("chat %s belongs to telegram account %s, not %s", chatGUID, account, p.account.ID)
	}
	return peer, nil
}

// inputPeer addresses a chat, reloading the dialogs once when its access hash
// has not been seen — a pane restored from the last session can ask for its
// history before the chat list has loaded.
func (p *Provider) inputPeer(peer Peer) (tg.InputPeerClass, error) {
	if input, ok := p.peers.inputPeer(peer); ok {
		return input, nil
	}
	p.mu.Lock()
	recent := time.Since(p.reloadedAt) < 10*time.Second
	p.mu.Unlock()
	if !recent {
		if _, err := p.Chats(maxDialogs); err != nil {
			return nil, err
		}
		if input, ok := p.peers.inputPeer(peer); ok {
			return input, nil
		}
	}
	return nil, fmt.Errorf("telegram: %s is not among this account's recent chats", peer)
}

func (p *Provider) rememberLatest(peer Peer, msgID int) {
	if msgID <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if msgID > p.latest[peer] {
		p.latest[peer] = msgID
	}
}

// cachedHistory returns the last load of a chat when it can still be trusted:
// the feed has been up since, nothing has arrived for the chat, it covers the
// limit asked for, and it is younger than the backstop.
func (p *Provider) cachedHistory(peer Peer, limit int) ([]models.Message, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.live {
		return nil, false
	}
	entry, ok := p.history[peer]
	if !ok || entry.limit < limit || time.Since(entry.at) > historyCacheTTL {
		return nil, false
	}
	return append([]models.Message(nil), entry.messages...), true
}

func (p *Provider) storeHistory(peer Peer, limit int, msgs []models.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.live {
		return
	}
	p.history[peer] = cachedHistory{limit: limit, messages: msgs, at: time.Now()}
}

// forget drops one chat's cached history.
func (p *Provider) forget(peer Peer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.history, peer)
}

// setLive records whether the realtime feed is up. Going down drops every
// cached history: whatever happened during the gap is not in any of them.
func (p *Provider) setLive(live bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.live = live
	if !live {
		p.history = make(map[Peer]cachedHistory)
	}
}
