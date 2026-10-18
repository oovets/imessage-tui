package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/oovets/imessage-tui/models"
)

const (
	selfID  = 100
	annaID  = 200
	annaKey = 2001
	boID    = 300
	groupID = 400
	chanID  = 500
	chanKey = 5001
)

// fakeAPI answers RPC calls with canned responses, encoded and decoded the
// way the real connection would, so a wrongly shaped response fails here as
// it would against Telegram.
type fakeAPI struct {
	t        *testing.T
	mu       sync.Mutex
	handlers map[uint32]func(bin.Encoder) (bin.Encoder, error)
	calls    map[uint32]int
	last     map[uint32]bin.Encoder
}

func newFakeAPI(t *testing.T) *fakeAPI {
	return &fakeAPI{
		t:        t,
		handlers: make(map[uint32]func(bin.Encoder) (bin.Encoder, error)),
		calls:    make(map[uint32]int),
		last:     make(map[uint32]bin.Encoder),
	}
}

type typed interface{ TypeID() uint32 }

func (f *fakeAPI) on(request typed, handler func(bin.Encoder) (bin.Encoder, error)) {
	f.handlers[request.TypeID()] = handler
}

func (f *fakeAPI) reply(request typed, response bin.Encoder) {
	f.on(request, func(bin.Encoder) (bin.Encoder, error) { return response, nil })
}

func (f *fakeAPI) count(request typed) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[request.TypeID()]
}

func (f *fakeAPI) request(request typed) bin.Encoder {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[request.TypeID()]
}

func (f *fakeAPI) Invoke(_ context.Context, input bin.Encoder, output bin.Decoder) error {
	id := input.(typed).TypeID()
	f.mu.Lock()
	f.calls[id]++
	f.last[id] = input
	handler, ok := f.handlers[id]
	f.mu.Unlock()
	if !ok {
		return fmt.Errorf("unexpected call %T", input)
	}
	response, err := handler(input)
	if err != nil {
		return err
	}
	var buf bin.Buffer
	if err := response.Encode(&buf); err != nil {
		return err
	}
	return output.Decode(&buf)
}

func newTestProvider(t *testing.T) (*Provider, *fakeAPI) {
	t.Helper()
	api := newFakeAPI(t)
	return newProvider(Account{ID: "100", Name: "Me"}, tg.NewClient(api), selfID), api
}

func users() []tg.UserClass {
	return []tg.UserClass{
		&tg.User{ID: selfID, AccessHash: 1, FirstName: "Me", Self: true},
		&tg.User{ID: annaID, AccessHash: annaKey, FirstName: "Anna", LastName: "Berg"},
		&tg.User{ID: boID, AccessHash: 3, Username: "bo"},
	}
}

func chats() []tg.ChatClass {
	return []tg.ChatClass{
		&tg.Chat{ID: groupID, Title: "Family", Photo: &tg.ChatPhotoEmpty{}},
		&tg.Channel{ID: chanID, AccessHash: chanKey, Title: "News", Broadcast: true, Photo: &tg.ChatPhotoEmpty{}},
	}
}

func dialogsResponse() *tg.MessagesDialogs {
	return &tg.MessagesDialogs{
		Dialogs: []tg.DialogClass{
			&tg.Dialog{Peer: &tg.PeerUser{UserID: annaID}, TopMessage: 12, UnreadCount: 2},
			&tg.DialogFolder{Folder: tg.Folder{ID: 1, Title: "Archive"}, Peer: &tg.PeerUser{UserID: boID}},
			&tg.Dialog{Peer: &tg.PeerChat{ChatID: groupID}, TopMessage: 30},
			&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: chanID}, TopMessage: 7},
			&tg.Dialog{Peer: &tg.PeerUser{UserID: selfID}, TopMessage: 3},
		},
		Messages: []tg.MessageClass{
			&tg.Message{ID: 12, PeerID: &tg.PeerUser{UserID: annaID}, FromID: &tg.PeerUser{UserID: annaID}, Date: 1700000000, Message: "hej"},
			&tg.Message{ID: 30, PeerID: &tg.PeerChat{ChatID: groupID}, FromID: &tg.PeerUser{UserID: boID}, Date: 1700000100,
				Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 9}}},
			&tg.Message{ID: 7, PeerID: &tg.PeerChannel{ChannelID: chanID}, Date: 1700000200, Message: "headline"},
			&tg.Message{ID: 3, PeerID: &tg.PeerUser{UserID: selfID}, Out: true, Date: 1700000300, Message: "note"},
		},
		Users: users(),
		Chats: chats(),
	}
}

func TestChatsKeepTelegramsOrderAndNames(t *testing.T) {
	p, api := newTestProvider(t)
	api.reply(&tg.MessagesGetDialogsRequest{}, dialogsResponse())

	chats, err := p.Chats(50)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ guid, name, preview string }
	var got []row
	for _, chat := range chats {
		got = append(got, row{chat.GUID, chat.GetDisplayName(), chat.LastMessageText})
	}
	// The archive folder is not a chat; everything else keeps Telegram's
	// most-recent-first order.
	want := []row{
		{"tg:100:u200", "Anna Berg", "hej"},
		{"tg:100:g400", "Family", "[photo]"},
		{"tg:100:c500", "News", "headline"},
		{"tg:100:u100", "Saved Messages", "note"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("chats =\n%v\nwant\n%v", got, want)
	}
	if chats[0].UnreadCount != 2 || chats[0].LastMessageTime != 1700000000*1000 {
		t.Fatalf("anna: unread %d time %d", chats[0].UnreadCount, chats[0].LastMessageTime)
	}
}

func TestChatsAccountLabel(t *testing.T) {
	p, api := newTestProvider(t)
	api.reply(&tg.MessagesGetDialogsRequest{}, dialogsResponse())
	if p.Label() != "Telegram" {
		t.Fatalf("label = %q", p.Label())
	}
	p.ShowAccountInNames(true)
	chats, err := p.Chats(50)
	if err != nil {
		t.Fatal(err)
	}
	if p.Label() != "Telegram · Me" || chats[0].DisplayName != "Me Anna Berg" {
		t.Fatalf("label %q, name %q", p.Label(), chats[0].DisplayName)
	}
}

func historyResponse() *tg.MessagesMessagesSlice {
	return &tg.MessagesMessagesSlice{
		Count: 100,
		// Newest first, as Telegram sends it.
		Messages: []tg.MessageClass{
			&tg.Message{ID: 34, PeerID: &tg.PeerChat{ChatID: groupID}, FromID: &tg.PeerUser{UserID: boID}, Date: 1700000400,
				Media: &tg.MessageMediaDocument{Document: &tg.Document{ID: 1, MimeType: "image/webp",
					Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{Alt: "😀", Stickerset: &tg.InputStickerSetEmpty{}}}}}},
			&tg.MessageService{ID: 33, PeerID: &tg.PeerChat{ChatID: groupID}, Date: 1700000300, Action: &tg.MessageActionChatEditTitle{Title: "Family"}},
			&tg.Message{ID: 32, PeerID: &tg.PeerChat{ChatID: groupID}, Out: true, Date: 1700000200, Message: "on my way"},
			&tg.Message{ID: 31, PeerID: &tg.PeerChat{ChatID: groupID}, FromID: &tg.PeerUser{UserID: annaID}, Date: 1700000100,
				Message: "dinner?",
				Reactions: tg.MessageReactions{Results: []tg.ReactionCount{
					{Reaction: &tg.ReactionEmoji{Emoticon: "👍"}, Count: 2},
					{Reaction: &tg.ReactionCustomEmoji{DocumentID: 77}, Count: 1},
				}}},
			&tg.Message{ID: 30, PeerID: &tg.PeerChat{ChatID: groupID}, FromID: &tg.PeerUser{UserID: boID}, Date: 1700000000,
				Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 9}}},
		},
		Users: users(),
		Chats: chats(),
	}
}

func TestMessagesAreOldestFirstWithSendersAndMedia(t *testing.T) {
	p, api := newTestProvider(t)
	api.reply(&tg.MessagesGetHistoryRequest{}, historyResponse())

	msgs, err := p.Messages("tg:100:g400", 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, msg := range msgs {
		sender := "me"
		if !msg.IsFromMe {
			sender = msg.Handle.DisplayName
		}
		line := fmt.Sprintf("%s %s: %s", msg.GUID, sender, msg.Text)
		for _, att := range msg.Attachments {
			line += " +" + att.GUID + " " + att.MimeType
		}
		if msg.ReactionCounts != nil {
			line += fmt.Sprintf(" %v", msg.ReactionCounts)
		}
		got = append(got, line)
	}
	want := []string{
		"tg:100:g400:30 @bo:  +tgfile:100:g400:30 image/jpeg",
		"tg:100:g400:31 Anna Berg: dinner? map[★:1 👍:2]",
		"tg:100:g400:32 me: on my way",
		// The title change has nothing to show, and a sticker is its emoji.
		"tg:100:g400:34 @bo: [sticker 😀]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("messages =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	request := api.request(&tg.MessagesGetHistoryRequest{}).(*tg.MessagesGetHistoryRequest)
	if _, ok := request.Peer.(*tg.InputPeerChat); !ok || request.Limit != 50 {
		t.Fatalf("history request = %+v", request)
	}
}

func TestMessagesRejectAnotherAccountsChat(t *testing.T) {
	p, _ := newTestProvider(t)
	if _, err := p.Messages("tg:999:u200", 50); err == nil {
		t.Fatal("read a chat of account 999 through account 100")
	}
}

// A pane restored from the last session asks for history before the chat
// list has loaded, so the access hash it needs has never been seen.
func TestUnknownPeerReloadsDialogsOnce(t *testing.T) {
	p, api := newTestProvider(t)
	api.reply(&tg.MessagesGetDialogsRequest{}, dialogsResponse())
	api.reply(&tg.MessagesGetHistoryRequest{}, &tg.MessagesMessages{})

	if _, err := p.Messages("tg:100:c500", 20); err != nil {
		t.Fatal(err)
	}
	request := api.request(&tg.MessagesGetHistoryRequest{}).(*tg.MessagesGetHistoryRequest)
	if peer, ok := request.Peer.(*tg.InputPeerChannel); !ok || peer.AccessHash != chanKey {
		t.Fatalf("peer = %#v", request.Peer)
	}

	// A chat that is not among the dialogs fails without hammering them.
	if _, err := p.Messages("tg:100:c999", 20); err == nil {
		t.Fatal("history for a chat with no access hash")
	}
	if n := api.count(&tg.MessagesGetDialogsRequest{}); n != 1 {
		t.Fatalf("dialogs loaded %d times, want 1", n)
	}
}

func TestMinUserNeverReplacesAnAccessHash(t *testing.T) {
	table := newPeerTable(selfID)
	table.apply([]tg.UserClass{&tg.User{ID: annaID, AccessHash: annaKey, FirstName: "Anna"}}, nil)
	table.apply([]tg.UserClass{&tg.User{ID: annaID, AccessHash: 999, FirstName: "Anna B", Min: true}}, nil)

	input, ok := table.inputPeer(Peer{Kind: kindUser, ID: annaID})
	if !ok || input.(*tg.InputPeerUser).AccessHash != annaKey {
		t.Fatalf("input peer = %#v", input)
	}
	if got := table.chatName(Peer{Kind: kindUser, ID: annaID}); got != "Anna B" {
		t.Fatalf("name = %q, want the min user's newer name", got)
	}

	// Seen only as min, a user cannot be addressed at all.
	table.apply([]tg.UserClass{&tg.User{ID: boID, AccessHash: 5, FirstName: "Bo", Min: true}}, nil)
	if _, ok := table.inputPeer(Peer{Kind: kindUser, ID: boID}); ok {
		t.Fatal("addressed a user by a min access hash")
	}
}

func TestHistoryCacheOnlyWhileLive(t *testing.T) {
	p, api := newTestProvider(t)
	api.reply(&tg.MessagesGetHistoryRequest{}, historyResponse())
	history := &tg.MessagesGetHistoryRequest{}

	load := func() {
		t.Helper()
		if _, err := p.Messages("tg:100:g400", 50); err != nil {
			t.Fatal(err)
		}
	}

	// Without a feed nothing tells the cache it is stale, so there is none.
	load()
	load()
	if n := api.count(history); n != 2 {
		t.Fatalf("offline: %d calls, want 2", n)
	}

	p.setLive(true)
	load()
	load()
	if n := api.count(history); n != 3 {
		t.Fatalf("live: %d calls, want 3 (one load, one cache hit)", n)
	}
	// A bigger window than the cached one is a miss.
	if _, err := p.Messages("tg:100:g400", 80); err != nil {
		t.Fatal(err)
	}
	if n := api.count(history); n != 4 {
		t.Fatalf("bigger limit: %d calls, want 4", n)
	}

	// An update for the chat drops its entry.
	p.forget(Peer{Kind: kindGroup, ID: groupID})
	load()
	if n := api.count(history); n != 5 {
		t.Fatalf("after an update: %d calls, want 5", n)
	}

	// So does the feed going down, even once it is back.
	p.setLive(false)
	p.setLive(true)
	load()
	if n := api.count(history); n != 6 {
		t.Fatalf("after a reconnect: %d calls, want 6", n)
	}
}

func TestSendRepliesWithinTheChatOnly(t *testing.T) {
	p, api := newTestProvider(t)
	p.peers.apply(users(), chats())
	api.reply(&tg.MessagesSendMessageRequest{}, &tg.UpdateShortSentMessage{ID: 40, Date: 1700000500})

	if err := p.Send("tg:100:u200", "hej", "tg:100:u200:12", "echo"); err != nil {
		t.Fatal(err)
	}
	request := api.request(&tg.MessagesSendMessageRequest{}).(*tg.MessagesSendMessageRequest)
	peer, ok := request.Peer.(*tg.InputPeerUser)
	if !ok || peer.UserID != annaID || peer.AccessHash != annaKey || request.Message != "hej" {
		t.Fatalf("send request = %+v", request)
	}
	reply, ok := request.ReplyTo.(*tg.InputReplyToMessage)
	if !ok || reply.ReplyToMsgID != 12 {
		t.Fatalf("reply to = %#v", request.ReplyTo)
	}

	// A reply target in another chat is not a reply here.
	if err := p.Send("tg:100:u200", "hej", "tg:100:g400:12", ""); err != nil {
		t.Fatal(err)
	}
	request = api.request(&tg.MessagesSendMessageRequest{}).(*tg.MessagesSendMessageRequest)
	if request.ReplyTo != nil {
		t.Fatalf("reply to = %#v, want none", request.ReplyTo)
	}
}

func TestReactMapsTapbacksOntoTelegramReactions(t *testing.T) {
	p, api := newTestProvider(t)
	p.peers.apply(users(), chats())
	api.reply(&tg.MessagesSendReactionRequest{}, &tg.Updates{})

	for tapback, want := range map[string]string{"love": "❤", "emphasize": "🔥", "question": "🤔", "🎉": "🎉"} {
		if err := p.React("tg:100:u200", "tg:100:u200:12", tapback); err != nil {
			t.Fatal(err)
		}
		request := api.request(&tg.MessagesSendReactionRequest{}).(*tg.MessagesSendReactionRequest)
		got := request.Reaction[0].(*tg.ReactionEmoji).Emoticon
		if got != want || request.MsgID != 12 {
			t.Errorf("%s: sent %q on %d, want %q on 12", tapback, got, request.MsgID, want)
		}
	}
	if err := p.React("tg:100:u200", "tg:100:g400:12", "love"); err == nil {
		t.Fatal("reacted to a message in another chat")
	}
}

func TestMarkReadUsesTheChannelMethodForChannels(t *testing.T) {
	p, api := newTestProvider(t)
	api.reply(&tg.MessagesGetDialogsRequest{}, dialogsResponse())
	api.reply(&tg.MessagesReadHistoryRequest{}, &tg.MessagesAffectedMessages{Pts: 1, PtsCount: 1})
	api.reply(&tg.ChannelsReadHistoryRequest{}, &tg.BoolTrue{})

	// Nothing seen yet: nothing to mark.
	if err := p.MarkRead("tg:100:u200"); err != nil || api.count(&tg.MessagesReadHistoryRequest{}) != 0 {
		t.Fatalf("marked an unseen chat: %v", err)
	}
	if _, err := p.Chats(50); err != nil {
		t.Fatal(err)
	}

	if err := p.MarkRead("tg:100:u200"); err != nil {
		t.Fatal(err)
	}
	if request := api.request(&tg.MessagesReadHistoryRequest{}).(*tg.MessagesReadHistoryRequest); request.MaxID != 12 {
		t.Fatalf("read history max id = %d, want 12", request.MaxID)
	}
	if err := p.MarkRead("tg:100:c500"); err != nil {
		t.Fatal(err)
	}
	request := api.request(&tg.ChannelsReadHistoryRequest{}).(*tg.ChannelsReadHistoryRequest)
	if channel := request.Channel.(*tg.InputChannel); channel.ChannelID != chanID || request.MaxID != 7 {
		t.Fatalf("channel read = %+v", request)
	}
}

func TestDownloadRefetchesTheMessageForAFreshReference(t *testing.T) {
	p, api := newTestProvider(t)
	p.peers.apply(users(), chats())
	api.reply(&tg.MessagesGetMessagesRequest{}, &tg.MessagesMessages{
		Messages: []tg.MessageClass{&tg.Message{ID: 30, PeerID: &tg.PeerChat{ChatID: groupID},
			Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 9, AccessHash: 8, FileReference: []byte("fresh"),
				Sizes: []tg.PhotoSizeClass{
					&tg.PhotoStrippedSize{Type: "i", Bytes: []byte{1}},
					&tg.PhotoSize{Type: "m", W: 320, H: 240, Size: 10},
					&tg.PhotoSizeProgressive{Type: "y", W: 1280, H: 960, Sizes: []int{10, 20}},
				}}}}},
	})
	api.reply(&tg.UploadGetFileRequest{}, &tg.UploadFile{Type: &tg.StorageFileJpeg{}, Bytes: []byte("jpeg")})

	data, mime, err := p.DownloadAttachment(attachmentFor("tgfile:100:g400:30"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "jpeg" || mime != "image/jpeg" {
		t.Fatalf("got %q %q", data, mime)
	}
	request := api.request(&tg.UploadGetFileRequest{}).(*tg.UploadGetFileRequest)
	location := request.Location.(*tg.InputPhotoFileLocation)
	if string(location.FileReference) != "fresh" || location.ThumbSize != "y" {
		t.Fatalf("location = %+v, want the fresh reference and the largest size", location)
	}

	if _, _, err := p.DownloadAttachment(attachmentFor("tgfile:999:g400:30")); err == nil {
		t.Fatal("downloaded another account's file")
	}
}

func TestFloodWaitRetriesShortWaitsOnly(t *testing.T) {
	old := floodWaitPadding
	floodWaitPadding = 0
	t.Cleanup(func() { floodWaitPadding = old })

	invoke := func(errs ...error) (int, error) {
		calls := 0
		next := telegram.InvokeFunc(func(context.Context, bin.Encoder, bin.Decoder) error {
			calls++
			if calls <= len(errs) {
				return errs[calls-1]
			}
			return nil
		})
		err := floodWait().Handle(next).Invoke(context.Background(), &tg.HelpGetConfigRequest{}, nil)
		return calls, err
	}

	if calls, err := invoke(tgerr.New(420, "FLOOD_WAIT_0")); err != nil || calls != 2 {
		t.Fatalf("short wait: %d calls, %v", calls, err)
	}
	if calls, err := invoke(tgerr.New(420, "FLOOD_WAIT_60")); err == nil || calls != 1 {
		t.Fatalf("long wait: %d calls, %v; want the error back at once", calls, err)
	}
	waits := []error{tgerr.New(420, "FLOOD_WAIT_0"), tgerr.New(420, "FLOOD_WAIT_0"), tgerr.New(420, "FLOOD_WAIT_0")}
	if calls, err := invoke(waits...); err == nil || calls != maxFloodRetries+1 {
		t.Fatalf("repeated waits: %d calls, %v", calls, err)
	}
	if calls, err := invoke(tgerr.New(400, "PEER_ID_INVALID")); err == nil || calls != 1 {
		t.Fatalf("other error: %d calls, %v", calls, err)
	}
}

func TestUpdatesBecomeEvents(t *testing.T) {
	p, _ := newTestProvider(t)
	p.stream = newStream(p)
	dispatcher := tg.NewUpdateDispatcher()
	p.handle(dispatcher)
	p.setLive(true)
	p.history[Peer{Kind: kindUser, ID: annaID}] = cachedHistory{limit: 50, at: time.Now()}

	entities := tg.Entities{Users: map[int64]*tg.User{annaID: {ID: annaID, AccessHash: annaKey, FirstName: "Anna"}}}
	update := &tg.Updates{
		Updates: []tg.UpdateClass{&tg.UpdateNewMessage{Message: &tg.Message{
			ID: 13, PeerID: &tg.PeerUser{UserID: annaID}, FromID: &tg.PeerUser{UserID: annaID}, Date: 1700000600, Message: "är du där?",
		}}},
		Users: []tg.UserClass{entities.Users[annaID]},
	}
	if err := dispatcher.Handle(context.Background(), update); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-p.stream.Events():
		if event.ChatGUID != "tg:100:u200" || event.Message.GUID != "tg:100:u200:13" || event.Message.Handle.DisplayName != "Anna" {
			t.Fatalf("event = %+v", event)
		}
	default:
		t.Fatal("no event")
	}
	if _, ok := p.history[Peer{Kind: kindUser, ID: annaID}]; ok {
		t.Fatal("the chat's cached history survived a new message")
	}
	if p.latest[Peer{Kind: kindUser, ID: annaID}] != 13 {
		t.Fatalf("latest = %d, want 13 so MarkRead covers the new message", p.latest[Peer{Kind: kindUser, ID: annaID}])
	}
}

func attachmentFor(guid string) models.Attachment {
	return models.Attachment{GUID: guid, MimeType: "image/jpeg"}
}

func TestReactionUpdateDropsTheCachedHistory(t *testing.T) {
	p, _ := newTestProvider(t)
	p.stream = newStream(p)
	dispatcher := tg.NewUpdateDispatcher()
	p.handle(dispatcher)
	p.setLive(true)
	group := Peer{Kind: kindGroup, ID: groupID}
	p.history[group] = cachedHistory{limit: 50, at: time.Now()}

	update := &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageReactions{
		Peer:      &tg.PeerChat{ChatID: groupID},
		MsgID:     31,
		Reactions: tg.MessageReactions{Results: []tg.ReactionCount{{Reaction: &tg.ReactionEmoji{Emoticon: "🔥"}, Count: 1}}},
	}}}
	if err := dispatcher.Handle(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.history[group]; ok {
		t.Fatal("cached history survived a reaction, so the refresh would keep the old counts")
	}
}
