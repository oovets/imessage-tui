package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/oovets/imessage-tui/models"
)

func TestMessagesModelSetMessagesDedupesByGUIDAndFingerprint(t *testing.T) {
	model := NewMessagesModel()
	model.SetMessages([]models.Message{
		{
			GUID:        "message-a",
			Text:        "same",
			IsFromMe:    false,
			DateCreated: 1000,
			ChatGUID:    "chat-a",
		},
		{
			GUID:        "message-a",
			Text:        "same",
			IsFromMe:    false,
			DateCreated: 1000,
			ChatGUID:    "chat-a",
			Attachments: []models.Attachment{
				{GUID: "attachment-a", MimeType: "image/png"},
			},
		},
		{
			GUID:        "message-a-alt",
			Text:        "same",
			IsFromMe:    false,
			DateCreated: 1000,
			ChatGUID:    "chat-a",
		},
		{
			GUID:        "message-b",
			Text:        "different",
			IsFromMe:    true,
			DateCreated: 2000,
			ChatGUID:    "chat-a",
		},
	})

	if got, want := len(model.messages), 2; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	if got, want := len(model.messages[0].Attachments), 1; got != want {
		t.Fatalf("merged attachment count = %d, want %d", got, want)
	}
}

func TestMessagesModelAppendMessageDedupesLiveVariant(t *testing.T) {
	model := NewMessagesModel()
	first := models.Message{
		GUID:        "message-a",
		Text:        "same",
		IsFromMe:    true,
		DateCreated: 1000,
		ChatGUID:    "chat-a",
	}
	second := first
	second.GUID = "message-a-alt"

	model.AppendMessage(first)
	model.AppendMessage(second)

	if got, want := len(model.messages), 1; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
}

func TestMessagesModelFoldsReactionMessagesIntoEmojiOnly(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetShowLineNumbers(false)
	model.SetSize(80, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-a",
			Text:        "hello",
			DateCreated: 1000,
		},
		{
			GUID:                  "reaction-a",
			Text:                  "Alice reagerade med hjarta",
			DateCreated:           2000,
			AssociatedMessageGUID: "message-a",
			AssociatedMessageType: "love",
		},
	})

	view := model.View()
	if strings.Contains(view, "reagerade med") {
		t.Fatalf("reaction prose should not render: %q", view)
	}
	if !strings.Contains(view, "❤️") {
		t.Fatalf("reaction emoji should render on original message: %q", view)
	}
	if strings.Contains(view, "×1") {
		t.Fatalf("single reaction should not show explicit count: %q", view)
	}
}

func TestMessagesModelAppendReactionUpdatesExistingMessage(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetShowLineNumbers(false)
	model.SetSize(80, 8)
	model.AppendMessage(models.Message{
		GUID:        "message-a",
		Text:        "hello",
		DateCreated: 1000,
	})
	model.AppendMessage(models.Message{
		GUID:                  "reaction-a",
		Text:                  "Alice reacted with thumbs up",
		DateCreated:           2000,
		AssociatedMessageGUID: "message-a",
		AssociatedMessageType: "like",
	})

	view := model.View()
	if strings.Contains(view, "reacted with") {
		t.Fatalf("live reaction prose should not render: %q", view)
	}
	if !strings.Contains(view, "👍") {
		t.Fatalf("live reaction emoji should render on original message: %q", view)
	}
	if got, want := len(model.messages), 1; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
}

func TestMessagesModelFoldsBlueBubblesNumericTapback(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetShowLineNumbers(false)
	model.SetSize(80, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-a",
			Text:        "hello",
			DateCreated: 1000,
		},
		{
			GUID:                  "reaction-a",
			DateCreated:           2000,
			AssociatedMessageGUID: "p:0/message-a",
			AssociatedMessageType: "2000",
		},
	})

	view := model.View()
	if !strings.Contains(view, "❤️") {
		t.Fatalf("numeric tapback emoji should render on original message: %q", view)
	}
	if got, want := len(model.messages), 1; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
}

func TestWindowManagerCacheMessageDedupesLiveVariant(t *testing.T) {
	wm := NewWindowManager()
	first := models.Message{
		GUID:        "message-a",
		Text:        "same",
		IsFromMe:    false,
		DateCreated: 1000,
		ChatGUID:    "chat-a",
	}
	second := first
	second.GUID = "message-a-alt"

	if !wm.CacheMessage("chat-a", first) {
		t.Fatal("first cache insert was treated as duplicate")
	}
	if wm.CacheMessage("chat-a", second) {
		t.Fatal("duplicate live variant was inserted into cache")
	}
	if got, want := len(wm.GetCachedMessages("chat-a")), 1; got != want {
		t.Fatalf("cached message count = %d, want %d", got, want)
	}
}

func TestWindowManagerCacheMessageFoldsLiveReaction(t *testing.T) {
	wm := NewWindowManager()
	wm.CacheMessage("chat-a", models.Message{
		GUID:        "message-a",
		Text:        "hello",
		DateCreated: 1000,
		ChatGUID:    "chat-a",
	})
	if wm.CacheMessage("chat-a", models.Message{
		GUID:                  "reaction-a",
		Text:                  "Alice reacted with thumbs up",
		DateCreated:           2000,
		ChatGUID:              "chat-a",
		AssociatedMessageGUID: "message-a",
		AssociatedMessageType: "like",
	}) {
		t.Fatal("reaction should update cached target, not insert a new row")
	}

	cached := wm.GetCachedMessages("chat-a")
	if got, want := len(cached), 1; got != want {
		t.Fatalf("cached message count = %d, want %d", got, want)
	}
	if got, want := cached[0].ReactionCounts["👍"], 1; got != want {
		t.Fatalf("reaction count = %d, want %d", got, want)
	}
}

func TestMessagesModelFindsImageAttachmentAtVisibleRow(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetSize(40, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-a",
			Text:        "plain",
			DateCreated: 1000,
		},
		{
			GUID:        "message-b",
			Text:        "photo",
			DateCreated: 2000,
			Attachments: []models.Attachment{
				{GUID: "attachment-b", MimeType: "image/png"},
			},
		},
	})

	att, ok := model.FirstImageAttachmentAtViewportY(2)
	if !ok {
		t.Fatal("expected image attachment at visible message row")
	}
	if got, want := att.GUID, "attachment-b"; got != want {
		t.Fatalf("attachment GUID = %q, want %q", got, want)
	}
}

func TestWindowManagerFindsAndResizesDivider(t *testing.T) {
	wm := NewWindowManager()
	if !wm.SplitWindow(SplitHorizontal) {
		t.Fatal("split failed")
	}
	wm.SetSize(100, 20)

	divider := wm.DividerAt(49, 5)
	if divider == nil {
		t.Fatal("expected divider at split boundary")
	}

	wm.SetSplitRatio(divider, 0.7)
	if got, want := divider.SplitRatio, 0.7; got != want {
		t.Fatalf("split ratio = %v, want %v", got, want)
	}
}

func TestAppModelArrowUpDownMoveVerticalPaneFocus(t *testing.T) {
	wm := NewWindowManager()
	if !wm.SplitWindow(SplitVertical) {
		t.Fatal("split failed")
	}
	wm.SetSize(80, 20)

	bottom := wm.FocusedWindow()
	app := AppModel{
		windowManager: wm,
		focused:       focusWindow,
		showChatList:  true,
		persist:       newPersister(),
	}

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyUp})
	app = model.(AppModel)
	top := app.windowManager.FocusedWindow()
	if top == nil || bottom == nil || top.ID == bottom.ID {
		t.Fatalf("up did not move focus to pane above")
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = model.(AppModel)
	if got := app.windowManager.FocusedWindow(); got == nil || got.ID != bottom.ID {
		t.Fatalf("down did not move focus back to pane below")
	}
}

func TestMessagesModelRendersSupportedLinkPreviewTitle(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetSize(80, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-video",
			Text:        "https://youtu.be/dQw4w9WgXcQ",
			DateCreated: 1000,
			LinkPreviews: []models.LinkPreview{
				{
					URL:      "https://youtu.be/dQw4w9WgXcQ",
					SiteName: "YouTube",
					Title:    "Official music video",
				},
			},
		},
	})

	view := stripANSI(model.View())
	if !strings.Contains(view, "[YouTube] Official music video") {
		t.Fatalf("view did not contain link preview title: %q", view)
	}
}

func TestMessagesModelRendersInstagramPreviewTitleWithoutURL(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetSize(80, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-instagram",
			Text:        "https://www.instagram.com/reel/C1abcDEFghi/",
			DateCreated: 1000,
			LinkPreviews: []models.LinkPreview{
				{
					URL:      "https://www.instagram.com/reel/C1abcDEFghi/",
					SiteName: "Instagram",
					Title:    "Creator on Instagram: \"A short video\"",
				},
			},
		},
	})

	view := stripANSI(model.View())
	if !strings.Contains(view, "[Instagram] Creator on Instagram") {
		t.Fatalf("view did not contain Instagram preview title: %q", view)
	}
	if strings.Contains(view, "https://www.instagram.com/reel/C1abcDEFghi/") {
		t.Fatalf("expected Instagram URL to be hidden in view: %q", view)
	}
}

func TestMessagesModelRendersNewsPreviewTitleWithoutURL(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetSize(80, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-news",
			Text:        "https://www.aftonbladet.se/nyheter/a/example",
			DateCreated: 1000,
			LinkPreviews: []models.LinkPreview{
				{
					URL:      "https://www.aftonbladet.se/nyheter/a/example",
					SiteName: "Aftonbladet",
					Title:    "Nyhetstitel från Aftonbladet",
				},
			},
		},
	})

	view := stripANSI(model.View())
	if !strings.Contains(view, "[Aftonbladet] Nyhetstitel från Aftonbladet") {
		t.Fatalf("view did not contain news preview title: %q", view)
	}
	if strings.Contains(view, "https://www.aftonbladet.se/nyheter/a/example") {
		t.Fatalf("expected news URL to be hidden in view: %q", view)
	}
}

func TestMessageHasPreviewAttemptRejectsNewsSearchTitle(t *testing.T) {
	rawURL := "https://www.aftonbladet.se/nyheter/a/example"
	if messageHasPreviewAttempt(models.Message{
		Text: rawURL,
		LinkPreviews: []models.LinkPreview{
			{URL: rawURL, SiteName: "Aftonbladet", Title: "search"},
		},
	}, rawURL) {
		t.Fatalf("cached generic search title should be refetched")
	}
}

func TestMessagesModelLinkPreviewFallbackDoesNotRepeatURL(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetSize(80, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-spotify",
			Text:        "https://open.spotify.com/track/example",
			DateCreated: 1000,
		},
	})

	view := stripANSI(model.View())
	if !strings.Contains(view, "[Spotify] Loading preview...") {
		t.Fatalf("view did not contain loading fallback: %q", view)
	}
	if strings.Contains(view, "https://open.spotify.com/track/example") {
		t.Fatalf("expected Spotify URL to be hidden in view: %q", view)
	}
}

func TestMessagesModelRendersSpotifyArtistAndTrackWithoutURL(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetSize(80, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-spotify",
			Text:        "https://open.spotify.com/track/example",
			DateCreated: 1000,
			LinkPreviews: []models.LinkPreview{
				{
					URL:        "https://open.spotify.com/track/example",
					SiteName:   "Spotify",
					Title:      "Track Name",
					AuthorName: "Artist Name",
				},
			},
		},
	})

	view := stripANSI(model.View())
	if !strings.Contains(view, "[Spotify] Artist Name - Track Name") {
		t.Fatalf("view did not contain artist and track: %q", view)
	}
	if strings.Contains(view, "https://open.spotify.com/track/example") {
		t.Fatalf("expected Spotify URL to be hidden in view: %q", view)
	}
}

func TestMessagesModelHidesAttachmentSummaryForMediaPreview(t *testing.T) {
	model := NewMessagesModel()
	model.SetShowTimestamps(false)
	model.SetShowSenderNames(false)
	model.SetSize(80, 8)
	model.SetMessages([]models.Message{
		{
			GUID:        "message-spotify",
			Text:        "https://open.spotify.com/track/example",
			DateCreated: 1000,
			Attachments: []models.Attachment{
				{FileName: "preview.html"},
				{FileName: "preview.jpg"},
			},
			LinkPreviews: []models.LinkPreview{
				{
					URL:        "https://open.spotify.com/track/example",
					SiteName:   "Spotify",
					Title:      "Track Name",
					AuthorName: "Artist Name",
				},
			},
		},
	})

	view := stripANSI(model.View())
	if strings.Contains(view, "attachments") {
		t.Fatalf("expected rich-link attachment summary to be hidden: %q", view)
	}
	if !strings.Contains(view, "[Spotify] Artist Name - Track Name") {
		t.Fatalf("view did not contain Spotify preview: %q", view)
	}
}

func TestLinkPreviewBadgeUsesProviderColor(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(oldProfile)
	})

	badge := linkPreviewBadge("Spotify")
	if !strings.Contains(badge, "\x1b[") {
		t.Fatalf("expected colored badge to contain ANSI escape codes: %q", badge)
	}
	if stripANSI(badge) != "[Spotify]" {
		t.Fatalf("expected badge text to survive styling: %q", badge)
	}
	if strings.Contains(badge, "\x1b[1;") || strings.Contains(badge, "\x1b[1m") {
		t.Fatalf("expected badge to be colored without bold: %q", badge)
	}
}

func TestFormatMessageTimestampKeepsTextAndAddsStyle(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(oldProfile)
	})

	rendered := formatMessageTimestamp("15:04")
	if stripANSI(rendered) != "15:04 " {
		t.Fatalf("timestamp text changed: %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("expected timestamp to be styled: %q", rendered)
	}
}

func TestLinkPreviewAttemptRules(t *testing.T) {
	rawURL := "https://open.spotify.com/track/example"
	if messageHasPreviewAttempt(models.Message{
		LinkPreviews: []models.LinkPreview{{URL: rawURL, SiteName: "Spotify", Title: "Track Name"}},
	}, rawURL) {
		t.Fatalf("Spotify preview without author should be fetched again")
	}
	if !messageHasPreviewAttempt(models.Message{
		LinkPreviews: []models.LinkPreview{{URL: rawURL, SiteName: "Spotify", Unavailable: true}},
	}, rawURL) {
		t.Fatalf("unavailable preview should count as attempted")
	}
}

func TestMergeLoadedMessagesWithCachePreservesLinkPreviews(t *testing.T) {
	rawURL := "https://open.spotify.com/track/example"
	loaded := []models.Message{
		{
			GUID:        "message-spotify",
			Text:        rawURL,
			DateCreated: 1000,
		},
	}
	cached := []models.Message{
		{
			GUID:        "message-spotify",
			Text:        rawURL,
			DateCreated: 1000,
			LinkPreviews: []models.LinkPreview{
				{
					URL:        rawURL,
					SiteName:   "Spotify",
					Title:      "Track Name",
					AuthorName: "Artist Name",
				},
			},
		},
	}

	merged := mergeLoadedMessagesWithCache(loaded, cached)
	if len(merged) != 1 {
		t.Fatalf("merged length = %d, want 1", len(merged))
	}
	if !messageHasPreviewAttempt(merged[0], rawURL) {
		t.Fatalf("expected cached preview to be preserved: %#v", merged[0].LinkPreviews)
	}
}

func TestApplyChatOverridesAddsLocalAlias(t *testing.T) {
	chats := []models.Chat{
		{
			GUID:        "chat-a",
			DisplayName: "Server Name",
			Participants: []models.Handle{
				{DisplayName: "Alice"},
				{DisplayName: "Bob"},
			},
		},
	}

	applyChatOverrides(chats, map[string]string{"chat-a": "Local Alias"})

	if got, want := chats[0].GetDisplayName(), "Local Alias"; got != want {
		t.Fatalf("display name = %q, want %q", got, want)
	}
}

func stripANSI(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(s, "")
}
