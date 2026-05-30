package tui

import (
	"reflect"
	"testing"

	"github.com/oovets/imessage-tui/models"
)

// renderState captures everything an append must keep consistent with a full
// render: the cached body string and the parallel line-index slices.
func renderState(m *MessagesModel) (string, []int, []string) {
	return m.renderedBody, append([]int(nil), m.lineMessages...), append([]string(nil), m.lineLinks...)
}

func TestAppendMessageIncrementalMatchesFullRender(t *testing.T) {
	const day = int64(24 * 60 * 60 * 1000)
	base := int64(1_700_000_000_000)

	msgs := []models.Message{
		{GUID: "a", Text: "hello there", DateCreated: base, IsFromMe: false, Handle: &models.Handle{Address: "+111", DisplayName: "Alice"}},
		{GUID: "b", Text: "hi back", DateCreated: base + 1000, IsFromMe: true},
		{GUID: "c", Text: "a much longer message that should wrap across the narrow viewport width for sure", DateCreated: base + 2000, IsFromMe: false, Handle: &models.Handle{Address: "+111", DisplayName: "Alice"}},
		// Next day -> forces a day separator on append.
		{GUID: "d", Text: "new day message", DateCreated: base + day, IsFromMe: true},
		{GUID: "e", Text: "check https://youtu.be/abc out", DateCreated: base + day + 1000, IsFromMe: false, Handle: &models.Handle{Address: "+111", DisplayName: "Alice"}},
	}

	// Incremental: append one at a time.
	inc := NewMessagesModel()
	inc.SetSize(40, 20)
	for _, msg := range msgs {
		inc.AppendMessage(msg)
	}

	// Full: render everything in one shot.
	full := NewMessagesModel()
	full.SetSize(40, 20)
	full.SetMessages(append([]models.Message(nil), msgs...))

	if !inc.contentRendered {
		t.Fatalf("incremental model should have a real render")
	}

	incBody, incLM, incLL := renderState(&inc)
	fullBody, fullLM, fullLL := renderState(&full)

	if incBody != fullBody {
		t.Errorf("body mismatch:\n--- incremental ---\n%q\n--- full ---\n%q", incBody, fullBody)
	}
	if !reflect.DeepEqual(incLM, fullLM) {
		t.Errorf("lineMessages mismatch:\n incremental=%v\n full=%v", incLM, fullLM)
	}
	if !reflect.DeepEqual(incLL, fullLL) {
		t.Errorf("lineLinks mismatch:\n incremental=%v\n full=%v", incLL, fullLL)
	}
}

// A message that sorts before the current tail must fall back to a full render
// and still match the full-render reference (line numbers shift).
func TestAppendMessageOutOfOrderMatchesFullRender(t *testing.T) {
	base := int64(1_700_000_000_000)
	a := models.Message{GUID: "a", Text: "first", DateCreated: base + 5000, IsFromMe: true}
	b := models.Message{GUID: "b", Text: "older, inserted before a", DateCreated: base + 1000, IsFromMe: true}

	inc := NewMessagesModel()
	inc.SetSize(40, 20)
	inc.AppendMessage(a)
	inc.AppendMessage(b) // out of order -> full re-render

	full := NewMessagesModel()
	full.SetSize(40, 20)
	full.SetMessages([]models.Message{a, b})

	if inc.renderedBody != full.renderedBody {
		t.Errorf("body mismatch after out-of-order append:\n inc=%q\n full=%q", inc.renderedBody, full.renderedBody)
	}
	if !reflect.DeepEqual(inc.lineMessages, full.lineMessages) {
		t.Errorf("lineMessages mismatch: inc=%v full=%v", inc.lineMessages, full.lineMessages)
	}
}
