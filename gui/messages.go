package gui

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"html"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"github.com/bluebubbles-tui/api"
	"github.com/bluebubbles-tui/models"
)

var urlPattern = regexp.MustCompile(`https?://[^\s]+|www\.[^\s]+|mailto:[^\s]+|[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

var (
	titleTagPattern    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaTagPattern     = regexp.MustCompile(`(?is)<meta[^>]+>`)
	metaAttrPattern    = regexp.MustCompile(`(?i)([a-zA-Z_:][a-zA-Z0-9_:\-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	imageURLPattern    = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|bmp)(\?.*)?$`)
	youtubeWatchIDExpr = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

type linkPreviewData struct {
	Title       string
	Description string
	SiteName    string
	ImageURL    string
	Err         string
}

var previewCache = struct {
	sync.RWMutex
	data map[string]linkPreviewData
}{
	data: make(map[string]linkPreviewData),
}

var previewEnabled atomic.Bool
var previewMaxPerMessage atomic.Int32

var previewFetcher = struct {
	sync.RWMutex
	fn func(string) (linkPreviewData, error)
}{}

// attachmentFetcher is set when an API client is available and used to download
// attachment images that cannot be read from the local filesystem.
var attachmentFetcher = struct {
	sync.RWMutex
	fn func(guid string) ([]byte, string, error)
}{}

func setAttachmentFetcherFromAPI(client *api.Client) {
	attachmentFetcher.Lock()
	defer attachmentFetcher.Unlock()
	if client == nil {
		attachmentFetcher.fn = nil
		return
	}
	attachmentFetcher.fn = client.DownloadAttachment
}

func init() {
	previewEnabled.Store(true)
	previewMaxPerMessage.Store(2)
}

func setLinkPreviewEnabled(enabled bool) {
	previewEnabled.Store(enabled)
}

func setMaxLinkPreviewsPerMessage(max int) {
	if max < 0 {
		max = 0
	}
	previewMaxPerMessage.Store(int32(max))
}

func setLinkPreviewFetcherFromAPI(client *api.Client) {
	previewFetcher.Lock()
	if client == nil {
		previewFetcher.fn = nil
		previewFetcher.Unlock()
		return
	}

	previewFetcher.fn = func(rawURL string) (linkPreviewData, error) {
		p, err := client.GetLinkPreview(rawURL)
		if err != nil {
			return linkPreviewData{}, err
		}
		return linkPreviewData{
			Title:       p.Title,
			Description: p.Description,
			SiteName:    p.SiteName,
			ImageURL:    p.ImageURL,
		}, nil
	}
	previewFetcher.Unlock()
}

var senderNamePalette = []color.NRGBA{
	{R: 122, G: 162, B: 247, A: 255},
	{R: 158, G: 206, B: 106, A: 255},
	{R: 224, G: 175, B: 104, A: 255},
	{R: 247, G: 118, B: 142, A: 255},
	{R: 187, G: 154, B: 247, A: 255},
	{R: 125, G: 207, B: 255, A: 255},
	{R: 231, G: 130, B: 132, A: 255},
	{R: 115, G: 218, B: 202, A: 255},
}

type hoverMessageRow struct {
	widget.BaseWidget
	host     *fyne.Container
	content  *fyne.Container
	replyBtn fyne.CanvasObject
}

func newHoverMessageRow(content *fyne.Container, replyBtn fyne.CanvasObject) *hoverMessageRow {
	host := container.NewVBox(container.NewBorder(nil, nil, nil, nil, content))
	r := &hoverMessageRow{
		host:     host,
		content:  content,
		replyBtn: replyBtn,
	}
	r.ExtendBaseWidget(r)
	return r
}

func (r *hoverMessageRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.host)
}

func (r *hoverMessageRow) MouseIn(_ *desktop.MouseEvent) {
	if r.replyBtn != nil {
		r.replyBtn.Show()
		r.host.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, nil, r.replyBtn, r.content)}
		r.host.Refresh()
	}
}

func (r *hoverMessageRow) MouseOut() {
	if r.replyBtn != nil {
		r.replyBtn.Hide()
		r.host.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, nil, nil, r.content)}
		r.host.Refresh()
	}
}

func (r *hoverMessageRow) MouseMoved(_ *desktop.MouseEvent) {}

// MessageView renders the message history for the selected chat.
// All methods must be called from the Fyne main goroutine.
type MessageView struct {
	vbox      *fyne.Container
	scroll    *container.Scroll
	panel     fyne.CanvasObject
	messages  []models.Message
	onReply   func(models.Message)
	bottomPad float32

	autoScrollUntil atomic.Int64
}

func NewMessageView(onReply func(models.Message), _ func()) *MessageView {
	mv := &MessageView{onReply: onReply}
	mv.vbox = container.NewVBox()
	mv.scroll = container.NewVScroll(mv.vbox)
	mv.panel = mv.scroll
	return mv
}

// SetBottomPad sets a transparent spacer height appended after messages so the
// last message stays visible above a floating input card overlay.
func (mv *MessageView) SetBottomPad(h float32) {
	mv.bottomPad = h
	if len(mv.messages) > 0 {
		mv.rebuildVBox()
	}
}

// Widget returns the full message panel.
func (mv *MessageView) Widget() fyne.CanvasObject {
	return mv.panel
}

// SetChatName is a no-op in GUI mode: pane headers are intentionally hidden.
func (mv *MessageView) SetChatName(_ string) {}

// SetMessages replaces all messages and scrolls to the bottom.
// Passing nil or an empty slice clears the view and resets scroll to the top
// so a freshly selected chat doesn't inherit the previous scroll position.
func (mv *MessageView) SetMessages(msgs []models.Message) {
	mv.messages = msgs
	if len(msgs) == 0 {
		mv.vbox.Objects = nil
		mv.vbox.Refresh()
		mv.scroll.ScrollToTop()
		return
	}
	mv.rebuildVBox()
}

// AppendMessage adds a single message, deduplicating by GUID.
// Ported from tui/messages.go AppendMessage.
func (mv *MessageView) AppendMessage(msg models.Message) {
	for _, existing := range mv.messages {
		if existing.GUID == msg.GUID {
			return
		}
	}
	mv.messages = append(mv.messages, msg)
	sort.Slice(mv.messages, func(i, j int) bool {
		return mv.messages[i].DateCreated < mv.messages[j].DateCreated
	})
	mv.rebuildVBox()
}

// SetFocused is a no-op: sender names are always shown for the last message
// in each sender group regardless of pane focus state.
func (mv *MessageView) SetFocused(_ bool) {}

// ScrollToBottom attempts to scroll to the bottom at several increasing delays
// so it works whether Fyne lays out quickly or slowly.
func (mv *MessageView) ScrollToBottom() {
	for _, d := range []time.Duration{60, 200, 500} {
		go func(delay time.Duration) {
			time.Sleep(delay)
			fyne.Do(func() { mv.scroll.ScrollToBottom() })
		}(d)
	}
}

func (mv *MessageView) extendAutoScrollWindow(d time.Duration) {
	mv.autoScrollUntil.Store(time.Now().Add(d).UnixNano())
}

func (mv *MessageView) maybeScrollAfterAsyncResize() {
	if time.Now().UnixNano() > mv.autoScrollUntil.Load() {
		return
	}
	mv.ScrollToBottom()
}

func (mv *MessageView) rebuildVBox() {
	mv.extendAutoScrollWindow(2 * time.Second)
	mv.vbox.Objects = nil

	for i, msg := range mv.messages {
		showSender := !msg.IsFromMe && isLastInSenderGroup(mv.messages, i)
		mv.vbox.Add(buildMessageRow(msg, mv.onReply, nil, showSender, mv.maybeScrollAfterAsyncResize))
	}
	if mv.bottomPad > 0 {
		spacer := canvas.NewRectangle(color.Transparent)
		spacer.SetMinSize(fyne.NewSize(1, mv.bottomPad))
		mv.vbox.Add(spacer)
	}
	mv.vbox.Refresh()
	mv.ScrollToBottom()
}

func messageSenderName(msg models.Message) string {
	if msg.IsFromMe {
		return "Stefan Larsson"
	}
	if msg.Handle != nil && msg.Handle.DisplayName != "" {
		return stripEmojis(msg.Handle.DisplayName)
	}
	if msg.Handle != nil {
		return msg.Handle.Address
	}
	return "Unknown"
}

func buildMessageRow(msg models.Message, onReply func(models.Message), _ func(), showSender bool, onAsyncResize func()) fyne.CanvasObject {
	var objs []fyne.CanvasObject

	// Sender name: always visible for the last message in each incoming group.
	if showSender {
		senderName := messageSenderName(msg)
		col, _ := senderNameColor(senderName, msg.IsFromMe).(color.NRGBA)
		nameLabel := canvas.NewText(senderName, color.NRGBA{R: col.R, G: col.G, B: col.B, A: 255})
		nameLabel.TextStyle = fyne.TextStyle{Bold: true}
		nameLabel.TextSize = hoverSenderTextSize()
		objs = append(objs, nameLabel)
	}

	if strings.TrimSpace(msg.Text) != "" {
		objs = append(objs, buildMessageContent(msg.Text, msg))
		if previews := buildLinkPreviewRows(msg.Text, msg.IsFromMe, onAsyncResize); len(previews) > 0 {
			for _, p := range previews {
				objs = append(objs, alignOutgoingRow(p, msg.IsFromMe))
			}
		}
	}
	if attachments := buildAttachmentRows(msg.Attachments, onAsyncResize); len(attachments) > 0 {
		for _, a := range attachments {
			objs = append(objs, alignOutgoingRow(a, msg.IsFromMe))
		}
	}
	content := container.NewVBox(objs...)

	var replyBtn fyne.CanvasObject
	if onReply != nil && !msg.IsFromMe {
		replyGlyph := newGlyphAction("↩", func() { onReply(msg) })
		replyGlyph.SetFixedColor(color.Black)
		replyGlyph.Hide()
		replyBtn = replyGlyph
	}

	row := newHoverMessageRow(content, replyBtn)
	return applyMessageSideIndent(row, msg.IsFromMe)
}

// isLastInSenderGroup reports whether message at idx is the last consecutive
// message from that sender before a different sender (or end of list).
func isLastInSenderGroup(msgs []models.Message, idx int) bool {
	if idx+1 >= len(msgs) {
		return true
	}
	cur, next := msgs[idx], msgs[idx+1]
	if cur.IsFromMe != next.IsFromMe {
		return true
	}
	curAddr, nextAddr := "", ""
	if cur.Handle != nil {
		curAddr = cur.Handle.Address
	}
	if next.Handle != nil {
		nextAddr = next.Handle.Address
	}
	return curAddr != nextAddr
}

func applyMessageSideIndent(row fyne.CanvasObject, isFromMe bool) fyne.CanvasObject {
	indentPx := messageSideIndent()
	return container.NewBorder(nil, nil, fixedWidthSpacer(indentPx), fixedWidthSpacer(indentPx), row)
}

func fixedWidthSpacer(width float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(width, 1))
	return r
}

func buildMessageContent(body string, msg models.Message) fyne.CanvasObject {
	if !urlPattern.MatchString(body) {
		label := widget.NewLabel(body)
		label.Wrapping = fyne.TextWrapWord

		if msg.IsFromMe {
			label.Alignment = fyne.TextAlignTrailing
			if strings.HasPrefix(strings.TrimSpace(msg.Text), "> ") {
				label.Importance = widget.MediumImportance
			} else {
				label.Importance = widget.SuccessImportance
			}
		}
		return label
	}

	segments := make([]widget.RichTextSegment, 0)

	last := 0
	for _, m := range urlPattern.FindAllStringIndex(body, -1) {
		if m[0] > last {
			segments = append(segments, &widget.TextSegment{Text: body[last:m[0]]})
		}

		raw := body[m[0]:m[1]]
		linkText, trailing := splitLinkTrailing(raw)
		_, _, ok := normalizeLinkToken(linkText)
		if ok {
			// Hide raw URL tokens in chat text; preview cards render the clickable link.
		} else {
			segments = append(segments, &widget.TextSegment{Text: raw})
			last = m[1]
			continue
		}

		if trailing != "" {
			segments = append(segments, &widget.TextSegment{Text: trailing})
		}
		last = m[1]
	}

	if last < len(body) {
		segments = append(segments, &widget.TextSegment{Text: body[last:]})
	}

	if len(segments) == 0 {
		return widget.NewLabel("")
	}

	rich := widget.NewRichText(segments...)
	rich.Wrapping = fyne.TextWrapWord
	return rich
}

func rightAlignMessageContent(obj fyne.CanvasObject) fyne.CanvasObject {
	// Keep outgoing content anchored to the right edge while allowing wraps on resize.
	return container.NewBorder(nil, nil, layout.NewSpacer(), nil, obj)
}

func alignOutgoingRow(obj fyne.CanvasObject, isFromMe bool) fyne.CanvasObject {
	if !isFromMe {
		return obj
	}
	return rightAlignMessageContent(obj)
}

func senderNameColor(name string, isFromMe bool) color.Color {
	if isFromMe {
		return color.NRGBA{R: 125, G: 207, B: 255, A: 255}
	}

	trimmed := strings.TrimSpace(strings.ToLower(name))
	if trimmed == "" {
		return senderNamePalette[0]
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(trimmed))
	idx := int(h.Sum32()) % len(senderNamePalette)
	return senderNamePalette[idx]
}

func splitLinkTrailing(raw string) (string, string) {
	trimChars := ".,!?;:)"
	idx := len(raw)
	for idx > 0 && strings.ContainsRune(trimChars, rune(raw[idx-1])) {
		idx--
	}
	return raw[:idx], raw[idx:]
}

func normalizeLinkToken(token string) (*url.URL, string, bool) {
	t := strings.TrimSpace(token)
	if t == "" {
		return nil, "", false
	}

	if strings.HasPrefix(strings.ToLower(t), "www.") {
		t = "https://" + t
	}
	if strings.Contains(t, "@") && !strings.Contains(t, "://") && !strings.HasPrefix(strings.ToLower(t), "mailto:") {
		t = "mailto:" + t
	}

	u, err := url.Parse(t)
	if err != nil || u.Scheme == "" {
		return nil, "", false
	}
	return u, token, true
}

func buildAttachmentRows(attachments []models.Attachment, onAsyncResize func()) []fyne.CanvasObject {
	if len(attachments) == 0 {
		return nil
	}

	rows := make([]fyne.CanvasObject, 0, len(attachments))
	for _, att := range attachments {
		rows = append(rows, buildAttachmentRow(att, onAsyncResize))
	}
	return rows
}

func buildAttachmentRow(att models.Attachment, onAsyncResize func()) fyne.CanvasObject {
	name := attachmentName(att)
	isImage := strings.HasPrefix(strings.ToLower(att.MimeType), "image/")

	log.Printf("[attachment] guid=%q name=%q mime=%q url=%q path=%q pathOnDisk=%q",
		att.GUID, name, att.MimeType, att.URL, att.Path, att.PathOnDisk)

	// Try reading from the local filesystem first.
	if uri := attachmentURI(att); uri != nil {
		log.Printf("[attachment] trying local URI: %s", uri.String())
		if img := buildInlineImage(uri); img != nil {
			log.Printf("[attachment] local image OK: %s", uri.String())
			return newCollapsibleCard("Image: "+name, img, nil)
		}
		log.Printf("[attachment] local image failed (not an image or unreadable): %s", uri.String())
	} else {
		log.Printf("[attachment] no usable local URI")
	}

	// Fall back to downloading via the BlueBubbles API.
	if isImage && att.GUID != "" {
		attachmentFetcher.RLock()
		fn := attachmentFetcher.fn
		attachmentFetcher.RUnlock()
		if fn != nil {
			log.Printf("[attachment] queuing async API download for guid=%q", att.GUID)
			return buildAsyncAttachmentImage(att.GUID, name, fn, onAsyncResize)
		}
		log.Printf("[attachment] no API fetcher registered, showing label")
	}

	prefix := "Attachment"
	if isImage {
		prefix = "Image"
	}
	label := widget.NewLabel(fmt.Sprintf("%s: %s", prefix, name))
	label.Importance = widget.LowImportance
	label.Wrapping = fyne.TextWrapWord
	return label
}

// buildAsyncAttachmentImage shows a collapsed card and reveals the image when loaded.
func buildAsyncAttachmentImage(guid, name string, fetch func(string) ([]byte, string, error), onAsyncResize func()) fyne.CanvasObject {
	content := container.NewVBox()
	collapsed := newCollapsibleCard("Image: "+name, content, onAsyncResize)

	go func() {
		data, mimeType, err := fetch(guid)
		if err != nil {
			log.Printf("[attachment] API download error guid=%q: %v", guid, err)
			fyne.Do(func() { collapsed.SetSummary("Image: " + name + " (failed)") })
			return
		}
		log.Printf("[attachment] API download OK guid=%q bytes=%d mime=%q", guid, len(data), mimeType)
		if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
			log.Printf("[attachment] image decode failed guid=%q: %v", guid, err)
			fyne.Do(func() { collapsed.SetSummary("Image: " + name + " (decode failed)") })
			return
		}
		res := fyne.NewStaticResource(guid, data)
		img := canvas.NewImageFromResource(res)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(220, 140))
		fyne.Do(func() {
			content.Objects = []fyne.CanvasObject{img}
			content.Refresh()
			// Only trigger resize if the card is already open.
			if collapsed.expanded && onAsyncResize != nil {
				onAsyncResize()
			}
		})
	}()

	return collapsed
}

func buildLinkPreviewRows(body string, _ bool, onAsyncResize func()) []fyne.CanvasObject {
	if !previewEnabled.Load() {
		return nil
	}

	urls := extractPreviewableURLs(body)
	if len(urls) == 0 {
		return nil
	}

	max := int(previewMaxPerMessage.Load())
	if max == 0 {
		return nil
	}
	if max > 0 && len(urls) > max {
		urls = urls[:max]
	}

	rows := make([]fyne.CanvasObject, 0, len(urls))
	for _, raw := range urls {
		rows = append(rows, buildLinkPreviewCard(raw, onAsyncResize))
	}
	return rows
}

// collapsibleCard is a tappable widget that starts collapsed and reveals
// content on click. The header line shows "▶ summary" / "▼ summary".
type collapsibleCard struct {
	widget.BaseWidget
	summaryText  string
	summaryLabel *widget.Label
	content      fyne.CanvasObject
	expanded     bool
	onResize     func()
	host         *fyne.Container
}

func newCollapsibleCard(summary string, content fyne.CanvasObject, onResize func()) *collapsibleCard {
	c := &collapsibleCard{summaryText: summary, content: content, onResize: onResize}
	c.summaryLabel = widget.NewLabel("▶ " + summary)
	c.summaryLabel.Importance = widget.LowImportance
	c.summaryLabel.Wrapping = fyne.TextWrapWord
	c.host = container.NewVBox(c.summaryLabel)
	c.ExtendBaseWidget(c)
	return c
}

func (c *collapsibleCard) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.host)
}

// SetSummary updates the header text without changing expanded state.
func (c *collapsibleCard) SetSummary(text string) {
	c.summaryText = text
	prefix := "▶ "
	if c.expanded {
		prefix = "▼ "
	}
	c.summaryLabel.SetText(prefix + text)
}

func (c *collapsibleCard) Tapped(_ *fyne.PointEvent) {
	c.expanded = !c.expanded
	if c.expanded {
		c.summaryLabel.SetText("▼ " + c.summaryText)
		c.host.Objects = []fyne.CanvasObject{c.summaryLabel, c.content}
	} else {
		c.summaryLabel.SetText("▶ " + c.summaryText)
		c.host.Objects = []fyne.CanvasObject{c.summaryLabel}
	}
	c.host.Refresh()
	if c.onResize != nil {
		c.onResize()
	}
}

func buildLinkPreviewCard(rawURL string, onAsyncResize func()) fyne.CanvasObject {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return widget.NewLabel(rawURL)
	}

	hostname := parsed.Hostname()
	card := container.NewVBox()
	title := widget.NewLabel(hostname)
	title.Wrapping = fyne.TextWrapWord
	title.TextStyle = fyne.TextStyle{Bold: true}
	site := widget.NewLabel(hostname)
	site.Importance = widget.LowImportance
	site.Wrapping = fyne.TextWrapWord
	link := widget.NewHyperlink("Open link", parsed)
	card.Objects = []fyne.CanvasObject{title, site, link}

	collapsed := newCollapsibleCard(hostname, card, onAsyncResize)

	go func() {
		meta := getLinkPreview(rawURL)
		fyne.Do(func() {
			if meta.Err != "" {
				title.SetText(hostname)
				site.SetText("Preview unavailable")
				card.Refresh()
				return
			}

			displayTitle := hostname
			if meta.Title != "" {
				displayTitle = meta.Title
				title.SetText(meta.Title)
			} else {
				title.SetText(hostname)
			}
			if meta.SiteName != "" {
				site.SetText(meta.SiteName)
			} else {
				site.SetText(hostname)
			}
			// Update collapsed header to show the actual page title.
			collapsed.SetSummary(displayTitle)

			if meta.Description != "" {
				desc := widget.NewLabel(meta.Description)
				desc.Wrapping = fyne.TextWrapWord
				desc.Importance = widget.LowImportance
				card.Objects = []fyne.CanvasObject{title, site, desc, link}
				card.Refresh()
				if collapsed.expanded && onAsyncResize != nil {
					onAsyncResize()
				}
			}

			if meta.ImageURL != "" {
				go func() {
					if img := buildRemoteImagePreview(meta.ImageURL); img != nil {
						fyne.Do(func() {
							card.Objects = prependCanvasObject(card.Objects, img)
							card.Refresh()
							if collapsed.expanded && onAsyncResize != nil {
								onAsyncResize()
							}
						})
					}
				}()
			}

			card.Refresh()
		})
	}()

	return collapsed
}

func prependCanvasObject(objs []fyne.CanvasObject, obj fyne.CanvasObject) []fyne.CanvasObject {
	for _, existing := range objs {
		if existing == obj {
			return objs
		}
	}
	updated := make([]fyne.CanvasObject, 0, len(objs)+1)
	updated = append(updated, obj)
	updated = append(updated, objs...)
	return updated
}

func extractPreviewableURLs(body string) []string {
	matches := urlPattern.FindAllString(body, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	urls := make([]string, 0, len(matches))
	for _, raw := range matches {
		trimmed, _ := splitLinkTrailing(raw)
		u, _, ok := normalizeLinkToken(trimmed)
		if !ok || u == nil {
			continue
		}
		scheme := strings.ToLower(u.Scheme)
		if scheme != "http" && scheme != "https" {
			continue
		}
		key := u.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		urls = append(urls, key)
	}
	return urls
}

func getLinkPreview(rawURL string) linkPreviewData {
	previewCache.RLock()
	if cached, ok := previewCache.data[rawURL]; ok {
		previewCache.RUnlock()
		return cached
	}
	previewCache.RUnlock()

	fetched := fetchLinkPreview(rawURL)
	previewCache.Lock()
	previewCache.data[rawURL] = fetched
	previewCache.Unlock()
	return fetched
}

func fetchLinkPreview(rawURL string) linkPreviewData {
	previewFetcher.RLock()
	fn := previewFetcher.fn
	previewFetcher.RUnlock()
	if fn != nil {
		if meta, err := fn(rawURL); err == nil {
			if meta.Title != "" || meta.SiteName != "" || meta.Description != "" || meta.ImageURL != "" {
				return meta
			}
		}
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return linkPreviewData{Err: "invalid url"}
	}

	if yt := youtubeThumbnailURL(parsed); yt != "" {
		meta := fetchHTMLMetadata(rawURL)
		meta.ImageURL = yt
		if meta.SiteName == "" {
			meta.SiteName = "YouTube"
		}
		if meta.Title == "" {
			meta.Title = "YouTube video"
		}
		return meta
	}

	if isLikelyDirectImageURL(parsed) {
		name := path.Base(parsed.Path)
		if name == "" || name == "/" {
			name = "Image"
		}
		return linkPreviewData{
			Title:    name,
			SiteName: parsed.Hostname(),
			ImageURL: rawURL,
		}
	}

	meta := fetchHTMLMetadata(rawURL)
	if meta.Title == "" && meta.SiteName == "" {
		meta.SiteName = parsed.Hostname()
	}
	if meta.Title == "" {
		meta.Title = parsed.Hostname()
	}
	return meta
}

func fetchHTMLMetadata(rawURL string) linkPreviewData {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return linkPreviewData{Err: "request failed"}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (BlueBubbles-TUI LinkPreview)")

	resp, err := client.Do(req)
	if err != nil {
		return linkPreviewData{Err: "network error"}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return linkPreviewData{Err: "bad status"}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return linkPreviewData{Err: "read failed"}
	}

	htmlText := string(body)
	meta := parseMetaTags(htmlText)
	if meta.Title == "" {
		meta.Title = parseTitleTag(htmlText)
	}
	return meta
}

func parseTitleTag(doc string) string {
	m := titleTagPattern.FindStringSubmatch(doc)
	if len(m) < 2 {
		return ""
	}
	return collapseWhitespace(html.UnescapeString(stripHTML(m[1])))
}

func parseMetaTags(doc string) linkPreviewData {
	out := linkPreviewData{}
	for _, tag := range metaTagPattern.FindAllString(doc, -1) {
		attrs := parseTagAttributes(tag)
		name := strings.ToLower(strings.TrimSpace(attrs["name"]))
		prop := strings.ToLower(strings.TrimSpace(attrs["property"]))
		content := strings.TrimSpace(attrs["content"])
		if content == "" {
			continue
		}
		content = collapseWhitespace(html.UnescapeString(content))

		switch {
		case out.Title == "" && (prop == "og:title" || name == "twitter:title"):
			out.Title = content
		case out.Description == "" && (prop == "og:description" || name == "description" || name == "twitter:description"):
			out.Description = content
		case out.SiteName == "" && prop == "og:site_name":
			out.SiteName = content
		case out.ImageURL == "" && (prop == "og:image" || name == "twitter:image"):
			out.ImageURL = content
		}
	}
	return out
}

func parseTagAttributes(tag string) map[string]string {
	out := make(map[string]string)
	for _, m := range metaAttrPattern.FindAllStringSubmatch(tag, -1) {
		if len(m) < 5 {
			continue
		}
		val := m[2]
		if val == "" {
			val = m[3]
		}
		if val == "" {
			val = m[4]
		}
		out[strings.ToLower(m[1])] = val
	}
	return out
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func youtubeThumbnailURL(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	var id string

	if host == "youtu.be" {
		id = strings.Trim(strings.TrimPrefix(u.Path, "/"), " ")
	} else if strings.Contains(host, "youtube.com") {
		if strings.HasPrefix(u.Path, "/watch") {
			id = u.Query().Get("v")
		} else if strings.HasPrefix(u.Path, "/shorts/") {
			id = strings.TrimPrefix(u.Path, "/shorts/")
		} else if strings.HasPrefix(u.Path, "/embed/") {
			id = strings.TrimPrefix(u.Path, "/embed/")
		}
	}

	id = strings.TrimSpace(strings.Split(id, "/")[0])
	if !youtubeWatchIDExpr.MatchString(id) {
		return ""
	}
	return "https://img.youtube.com/vi/" + id + "/hqdefault.jpg"
}

func isLikelyDirectImageURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	return imageURLPattern.MatchString(strings.ToLower(u.Path + "?" + u.RawQuery))
}

func buildRemoteImagePreview(rawURL string) fyne.CanvasObject {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}

	client := &http.Client{Timeout: 6 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (BlueBubbles-TUI LinkPreview)")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, io.LimitReader(resp.Body, 5*1024*1024)); err != nil {
		return nil
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes())); err != nil {
		return nil
	}

	res := fyne.NewStaticResource(rawURL, buf.Bytes())
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(260, 150))
	return img
}

func buildInlineImage(uri fyne.URI) fyne.CanvasObject {
	if uri == nil {
		return nil
	}
	if !isImageURI(uri) {
		return nil
	}

	readCloser, err := storage.Reader(uri)
	if err != nil {
		return nil
	}
	defer readCloser.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(readCloser); err != nil {
		return nil
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes())); err != nil {
		return nil
	}

	res := fyne.NewStaticResource(uri.Name(), buf.Bytes())
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(220, 140))
	return img
}

func isImageURI(uri fyne.URI) bool {
	ext := strings.ToLower(uri.Extension())
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func attachmentURI(att models.Attachment) fyne.URI {
	for _, raw := range []string{att.URL, att.Path, att.PathOnDisk} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "mailto:") {
			u, err := storage.ParseURI(raw)
			if err == nil {
				return u
			}
			continue
		}
		u := storage.NewFileURI(raw)
		exists, err := storage.Exists(u)
		if err == nil && exists {
			return u
		}
	}
	return nil
}

func attachmentName(att models.Attachment) string {
	if strings.TrimSpace(att.FileName) != "" {
		return att.FileName
	}
	if strings.TrimSpace(att.PathOnDisk) != "" {
		parts := strings.Split(att.PathOnDisk, "/")
		return parts[len(parts)-1]
	}
	if strings.TrimSpace(att.Path) != "" {
		parts := strings.Split(att.Path, "/")
		return parts[len(parts)-1]
	}
	if strings.TrimSpace(att.URL) != "" {
		return att.URL
	}
	if strings.TrimSpace(att.GUID) != "" {
		return att.GUID
	}
	return "file"
}
