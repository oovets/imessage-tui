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
	host      *fyne.Container
	rowMain   fyne.CanvasObject
	replyBtn  fyne.CanvasObject
	timestamp *widget.Label
}

func newHoverMessageRow(rowMain fyne.CanvasObject, replyBtn fyne.CanvasObject, timestamp *widget.Label) *hoverMessageRow {
	host := container.NewVBox(rowMain)
	r := &hoverMessageRow{host: host, rowMain: rowMain, replyBtn: replyBtn, timestamp: timestamp}
	r.ExtendBaseWidget(r)
	return r
}

func (r *hoverMessageRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.host)
}

func (r *hoverMessageRow) MouseIn(_ *desktop.MouseEvent) {
	left := fyne.CanvasObject(nil)
	right := fyne.CanvasObject(nil)

	if r.replyBtn != nil {
		r.replyBtn.Show()
		if g, ok := r.replyBtn.(*glyphAction); ok {
			g.SetEmphasis(true)
		}
		right = r.replyBtn
	}
	if r.timestamp != nil {
		r.timestamp.Show()
		left = r.timestamp
	}

	r.host.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, left, right, r.rowMain)}
	r.host.Refresh()
}

func (r *hoverMessageRow) MouseOut() {
	if r.replyBtn != nil {
		if g, ok := r.replyBtn.(*glyphAction); ok {
			g.SetEmphasis(false)
		}
		r.replyBtn.Hide()
	}
	if r.timestamp != nil {
		r.timestamp.Hide()
	}
	r.host.Objects = []fyne.CanvasObject{r.rowMain}
	r.host.Refresh()
}

func (r *hoverMessageRow) MouseMoved(_ *desktop.MouseEvent) {}

// MessageView renders the message history for the selected chat.
// All methods must be called from the Fyne main goroutine.
type MessageView struct {
	header   *widget.Label
	vbox     *fyne.Container
	scroll   *container.Scroll
	panel    fyne.CanvasObject
	messages []models.Message
	onReply  func(models.Message)
}

func NewMessageView(onReply func(models.Message)) *MessageView {
	mv := &MessageView{onReply: onReply}
	mv.header = widget.NewLabel("")
	mv.header.TextStyle = fyne.TextStyle{Bold: true}

	mv.vbox = container.NewVBox()
	mv.scroll = container.NewVScroll(mv.vbox)
	mv.panel = container.NewBorder(mv.header, nil, nil, nil, mv.scroll)
	return mv
}

// Widget returns the full message panel (header + scroll area).
func (mv *MessageView) Widget() fyne.CanvasObject {
	return mv.panel
}

// SetChatName updates the chat name header.
func (mv *MessageView) SetChatName(name string) {
	mv.header.SetText(stripEmojis(name))
}

// SetMessages replaces all messages and scrolls to the bottom.
func (mv *MessageView) SetMessages(msgs []models.Message) {
	mv.messages = msgs
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

// SetFocused highlights the header when this pane is the focused one.
func (mv *MessageView) SetFocused(focused bool) {
	if focused {
		mv.header.Importance = widget.HighImportance
	} else {
		mv.header.Importance = widget.MediumImportance
	}
	mv.header.Refresh()
}

// ScrollToBottom scrolls the message list to the bottom after a short layout
// settle delay. Safe to call from the Fyne main goroutine.
func (mv *MessageView) ScrollToBottom() {
	go func() {
		time.Sleep(150 * time.Millisecond)
		fyne.Do(func() { mv.scroll.ScrollToBottom() })
	}()
}

const groupingWindow = 5 * time.Minute

func (mv *MessageView) rebuildVBox() {
	mv.vbox.Objects = nil
	for i, msg := range mv.messages {
		showSender := true
		if i > 0 {
			prev := mv.messages[i-1]
			sameSender := messageSenderName(prev) == messageSenderName(msg)
			closeInTime := msg.ParsedTime().Sub(prev.ParsedTime()) < groupingWindow
			if sameSender && closeInTime {
				showSender = false
			}
		}
		mv.vbox.Add(buildMessageRow(msg, mv.onReply, showSender))
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

func buildMessageRow(msg models.Message, onReply func(models.Message), showSender bool) fyne.CanvasObject {
	timeStr := formatHoverTimestamp(msg.ParsedTime())
	senderName := messageSenderName(msg)

	var objs []fyne.CanvasObject
	if showSender {
		sender := canvas.NewText(senderName, senderNameColor(senderName, msg.IsFromMe))
		sender.TextStyle = fyne.TextStyle{Bold: true}
		var senderRow fyne.CanvasObject
		if msg.IsFromMe {
			senderRow = container.NewHBox(layout.NewSpacer(), sender)
		} else {
			senderRow = container.NewHBox(sender, layout.NewSpacer())
		}
		objs = []fyne.CanvasObject{senderRow}
	}
	if strings.TrimSpace(msg.Text) != "" {
		objs = append(objs, buildMessageContent(msg.Text, msg))
		if previews := buildLinkPreviewRows(msg.Text, msg.IsFromMe); len(previews) > 0 {
			for _, p := range previews {
				objs = append(objs, alignOutgoingRow(p, msg.IsFromMe))
			}
		}
	}
	if attachments := buildAttachmentRows(msg.Attachments); len(attachments) > 0 {
		for _, a := range attachments {
			objs = append(objs, alignOutgoingRow(a, msg.IsFromMe))
		}
	}
	content := container.NewVBox(objs...)

	tsLabel := widget.NewLabel("[" + timeStr + "]")
	tsLabel.Importance = widget.LowImportance
	tsLabel.Alignment = fyne.TextAlignLeading
	tsLabel.Hide()
	if msg.IsFromMe {
		tsLabel = nil
	}

	rowMain := content
	var replyBtn fyne.CanvasObject
	if onReply != nil {
		replyGlyph := newGlyphAction("↩", func() {
			onReply(msg)
		})
		replyGlyph.SetEmphasis(false)
		replyGlyph.Hide()
		if !msg.IsFromMe {
			replyBtn = replyGlyph
		}
	}

	row := newHoverMessageRow(rowMain, replyBtn, tsLabel)
	return applyMessageSideIndent(row, msg.IsFromMe)
}

func applyMessageSideIndent(row fyne.CanvasObject, isFromMe bool) fyne.CanvasObject {
	const indentPx float32 = 24 // requested indentation
	if isFromMe {
		return container.NewBorder(nil, nil, nil, fixedWidthSpacer(indentPx), row)
	}
	return container.NewBorder(nil, nil, fixedWidthSpacer(indentPx), nil, row)
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
	if msg.IsFromMe {
		return rightAlignMessageContent(rich)
	}
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

func buildAttachmentRows(attachments []models.Attachment) []fyne.CanvasObject {
	if len(attachments) == 0 {
		return nil
	}

	rows := make([]fyne.CanvasObject, 0, len(attachments))
	for _, att := range attachments {
		rows = append(rows, buildAttachmentRow(att))
	}
	return rows
}

func buildAttachmentRow(att models.Attachment) fyne.CanvasObject {
	name := attachmentName(att)
	uri := attachmentURI(att)

	prefix := "Attachment"
	if strings.HasPrefix(strings.ToLower(att.MimeType), "image/") {
		prefix = "Image"
	}

	if uri != nil {
		href, err := url.Parse(uri.String())
		if err != nil {
			href = nil
		}
		if img := buildInlineImage(uri); img != nil {
			if href != nil {
				link := widget.NewHyperlink(prefix+": "+name, href)
				return container.NewVBox(img, link)
			}
			return img
		}
		if href != nil {
			return widget.NewHyperlink(prefix+": "+name, href)
		}
	}

	label := widget.NewLabel(fmt.Sprintf("%s: %s", prefix, name))
	label.Importance = widget.LowImportance
	label.Wrapping = fyne.TextWrapWord
	return label
}

func buildLinkPreviewRows(body string, _ bool) []fyne.CanvasObject {
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
		rows = append(rows, buildLinkPreviewCard(raw))
	}
	return rows
}

func buildLinkPreviewCard(rawURL string) fyne.CanvasObject {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return widget.NewLabel(rawURL)
	}

	card := container.NewVBox()
	title := widget.NewLabel("Loading preview...")
	title.Wrapping = fyne.TextWrapWord
	title.TextStyle = fyne.TextStyle{Bold: true}

	site := widget.NewLabel(parsed.Hostname())
	site.Importance = widget.LowImportance

	link := widget.NewHyperlink("Open link", parsed)

	card.Add(title)
	card.Add(site)
	card.Add(link)

	row := fyne.CanvasObject(card)

	go func() {
		meta := getLinkPreview(rawURL)
		fyne.Do(func() {
			if meta.Err != "" {
				title.SetText(parsed.Hostname())
				site.SetText("Preview unavailable")
				return
			}

			if meta.Title != "" {
				title.SetText(meta.Title)
			} else {
				title.SetText(parsed.Hostname())
			}

			if meta.SiteName != "" {
				site.SetText(meta.SiteName)
			} else {
				site.SetText(parsed.Hostname())
			}

			if meta.Description != "" {
				desc := widget.NewLabel(meta.Description)
				desc.Wrapping = fyne.TextWrapWord
				desc.Importance = widget.LowImportance
				card.Objects = append([]fyne.CanvasObject{title, site, desc}, link)
			}

			if meta.ImageURL != "" {
				go func() {
					if img := buildRemoteImagePreview(meta.ImageURL); img != nil {
						fyne.Do(func() {
							card.Objects = prependCanvasObject(card.Objects, img)
							card.Refresh()
						})
					}
				}()
			}

			card.Refresh()
		})
	}()

	return row
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
