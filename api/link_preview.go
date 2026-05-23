package api

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type LinkPreview struct {
	Title       string
	AuthorName  string
	Description string
	SiteName    string
	ImageURL    string
}

func (c *Client) SetPreviewProxyURL(raw string) {
	c.previewProxyURL = strings.TrimSpace(raw)
}

func (c *Client) SetOEmbedEndpoint(raw string) {
	c.oembedEndpoint = strings.TrimSpace(raw)
}

func (c *Client) GetLinkPreview(rawURL string) (LinkPreview, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return LinkPreview{}, fmt.Errorf("empty url")
	}

	if c.previewProxyURL != "" {
		if p, err := c.fetchPreviewFromProxy(rawURL); err == nil {
			if isSpotifyURL(rawURL) {
				_ = c.enrichSpotifyPreview(rawURL, &p)
			}
			if isBadHTMLFallbackTitle(rawURL, p.Title) {
				if htmlPreview, htmlErr := c.fetchPreviewFromHTML(rawURL); htmlErr == nil {
					return htmlPreview, nil
				}
			}
			return p, nil
		}
	}

	if prefersHTMLMetadata(rawURL) {
		if p, err := c.fetchPreviewFromHTML(rawURL); err == nil {
			return p, nil
		}
	}

	for _, endpoint := range oEmbedEndpointsForURL(rawURL, c.oembedEndpoint) {
		if p, err := c.fetchPreviewFromOEmbedEndpoint(rawURL, endpoint); err == nil {
			if isSpotifyURL(rawURL) {
				_ = c.enrichSpotifyPreview(rawURL, &p)
			}
			if isBadHTMLFallbackTitle(rawURL, p.Title) {
				if htmlPreview, htmlErr := c.fetchPreviewFromHTML(rawURL); htmlErr == nil {
					return htmlPreview, nil
				}
			}
			return p, nil
		}
	}

	if usesHTMLMetadataFallback(rawURL) {
		if p, err := c.fetchPreviewFromHTML(rawURL); err == nil {
			return p, nil
		}
	}

	return LinkPreview{}, fmt.Errorf("preview unavailable")
}

func (c *Client) fetchPreviewFromProxy(rawURL string) (LinkPreview, error) {
	proxyURL, err := url.Parse(c.previewProxyURL)
	if err != nil {
		return LinkPreview{}, err
	}

	q := proxyURL.Query()
	q.Set("url", rawURL)
	proxyURL.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, proxyURL.String(), nil)
	if err != nil {
		return LinkPreview{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return LinkPreview{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LinkPreview{}, fmt.Errorf("proxy status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return LinkPreview{}, err
	}

	preview := decodePreviewJSON(body)
	if preview.Title == "" && preview.SiteName == "" && preview.ImageURL == "" {
		return LinkPreview{}, fmt.Errorf("proxy returned no preview fields")
	}
	return preview, nil
}

func oEmbedEndpointsForURL(rawURL, configuredEndpoint string) []string {
	var endpoints []string
	if endpoint := providerOEmbedEndpoint(rawURL); endpoint != "" {
		endpoints = append(endpoints, endpoint)
	}

	endpoint := strings.TrimSpace(configuredEndpoint)
	if endpoint == "" {
		endpoint = "https://noembed.com/embed"
	}
	if len(endpoints) == 0 || endpoints[0] != endpoint {
		endpoints = append(endpoints, endpoint)
	}
	return endpoints
}

func providerOEmbedEndpoint(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := normalizedPreviewHost(u.Host)
	switch host {
	case "open.spotify.com", "spotify.com":
		return "https://open.spotify.com/oembed"
	case "youtube.com", "m.youtube.com", "youtu.be":
		return "https://www.youtube.com/oembed"
	default:
		return ""
	}
}

func isSpotifyURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := normalizedPreviewHost(u.Host)
	return host == "open.spotify.com" || host == "spotify.com"
}

func usesHTMLMetadataFallback(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := normalizedPreviewHost(u.Host)
	return host == "instagram.com" ||
		host == "m.instagram.com" ||
		newsSiteNameForHost(host) != ""
}

func prefersHTMLMetadata(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return newsSiteNameForHost(normalizedPreviewHost(u.Host)) != ""
}

func isBadHTMLFallbackTitle(rawURL, title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	return usesHTMLMetadataFallback(rawURL) && (title == "" || title == "search")
}

func (c *Client) fetchPreviewFromOEmbedEndpoint(rawURL, endpoint string) (LinkPreview, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return LinkPreview{}, err
	}
	q := u.Query()
	q.Set("url", rawURL)
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return LinkPreview{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return LinkPreview{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LinkPreview{}, fmt.Errorf("oembed status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return LinkPreview{}, err
	}

	preview := decodePreviewJSON(body)
	if preview.Title == "" && preview.SiteName == "" && preview.ImageURL == "" {
		return LinkPreview{}, fmt.Errorf("oembed returned no preview fields")
	}
	return preview, nil
}

func decodePreviewJSON(body []byte) LinkPreview {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return LinkPreview{}
	}

	get := func(keys ...string) string {
		for _, k := range keys {
			v, ok := m[k]
			if !ok {
				continue
			}
			s, ok := v.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s != "" {
				return s
			}
		}
		return ""
	}

	return LinkPreview{
		Title:       get("title", "og_title"),
		AuthorName:  get("author_name"),
		Description: get("description", "og_description"),
		SiteName:    get("site_name", "provider_name", "og_site_name"),
		ImageURL:    get("image", "image_url", "thumbnail_url", "og_image"),
	}
}

func (c *Client) enrichSpotifyPreview(rawURL string, preview *LinkPreview) error {
	if preview == nil || strings.TrimSpace(preview.AuthorName) != "" {
		return nil
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "imessage-tui link-preview")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("spotify page status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return err
	}

	author := firstHTMLMetaContent(string(body),
		`<meta\s+name="music:musician_description"\s+content="([^"]*)"`,
		`<meta\s+content="([^"]*)"\s+name="music:musician_description"`,
	)
	if author == "" {
		author = spotifyArtistFromTitle(firstHTMLTitle(string(body)), preview.Title)
	}
	if author != "" {
		preview.AuthorName = author
	}
	return nil
}

func (c *Client) fetchPreviewFromHTML(rawURL string) (LinkPreview, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return LinkPreview{}, err
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "imessage-tui link-preview")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return LinkPreview{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LinkPreview{}, fmt.Errorf("html status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return LinkPreview{}, err
	}
	bodyText := string(body)
	preview := LinkPreview{
		Title: firstHTMLMetaContent(bodyText,
			`<meta\s+property="og:title"\s+content="([^"]*)"`,
			`<meta\s+content="([^"]*)"\s+property="og:title"`,
			`<meta\s+name="twitter:title"\s+content="([^"]*)"`,
			`<meta\s+content="([^"]*)"\s+name="twitter:title"`,
		),
		Description: firstHTMLMetaContent(bodyText,
			`<meta\s+property="og:description"\s+content="([^"]*)"`,
			`<meta\s+content="([^"]*)"\s+property="og:description"`,
			`<meta\s+name="description"\s+content="([^"]*)"`,
			`<meta\s+content="([^"]*)"\s+name="description"`,
		),
		SiteName: firstHTMLMetaContent(bodyText,
			`<meta\s+property="og:site_name"\s+content="([^"]*)"`,
			`<meta\s+content="([^"]*)"\s+property="og:site_name"`,
		),
		ImageURL: firstHTMLMetaContent(bodyText,
			`<meta\s+property="og:image"\s+content="([^"]*)"`,
			`<meta\s+content="([^"]*)"\s+property="og:image"`,
		),
	}
	if preview.Title == "" {
		preview.Title = firstHTMLTitle(bodyText)
	}
	if preview.SiteName == "" {
		preview.SiteName = siteNameFromURL(rawURL)
	}
	if preview.Title == "" && preview.SiteName == "" && preview.ImageURL == "" {
		return LinkPreview{}, fmt.Errorf("html returned no preview fields")
	}
	return preview, nil
}

func siteNameFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := normalizedPreviewHost(u.Host)
	switch host {
	case "instagram.com", "m.instagram.com":
		return "Instagram"
	default:
		return newsSiteNameForHost(host)
	}
}

func normalizedPreviewHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	return host
}

func newsSiteNameForHost(host string) string {
	switch normalizedPreviewHost(host) {
	case "aftonbladet.se":
		return "Aftonbladet"
	case "expressen.se":
		return "Expressen"
	case "dn.se":
		return "DN"
	case "svd.se":
		return "SvD"
	case "svt.se":
		return "SVT"
	case "omni.se":
		return "Omni"
	case "gp.se":
		return "GP"
	case "sydsvenskan.se":
		return "Sydsvenskan"
	case "di.se":
		return "Dagens industri"
	default:
		return ""
	}
}

func firstHTMLMetaContent(body string, patterns ...string) string {
	if len(patterns) == 0 {
		return ""
	}
	for _, key := range htmlMetaKeysFromPatterns(patterns) {
		if value := firstHTMLMetaContentByKey(body, key); value != "" {
			return value
		}
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		match := re.FindStringSubmatch(body)
		if len(match) > 1 {
			if s := strings.TrimSpace(html.UnescapeString(match[1])); s != "" {
				return s
			}
		}
	}
	return ""
}

func htmlMetaKeysFromPatterns(patterns []string) []string {
	keys := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		for _, marker := range []string{`property="`, `name="`} {
			idx := strings.Index(pattern, marker)
			if idx < 0 {
				continue
			}
			start := idx + len(marker)
			end := strings.Index(pattern[start:], `"`)
			if end < 0 {
				continue
			}
			keys = append(keys, pattern[start:start+end])
			break
		}
	}
	return keys
}

func firstHTMLMetaContentByKey(body, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return ""
	}
	metaRe := regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	attrRe := regexp.MustCompile(`(?is)([a-zA-Z_:.-]+)\s*=\s*("([^"]*)"|'([^']*)')`)
	for _, tag := range metaRe.FindAllString(body, -1) {
		attrs := make(map[string]string)
		for _, match := range attrRe.FindAllStringSubmatch(tag, -1) {
			if len(match) < 5 {
				continue
			}
			value := match[3]
			if value == "" {
				value = match[4]
			}
			attrs[strings.ToLower(match[1])] = html.UnescapeString(value)
		}
		if strings.ToLower(strings.TrimSpace(attrs["property"])) != key &&
			strings.ToLower(strings.TrimSpace(attrs["name"])) != key {
			continue
		}
		if content := strings.TrimSpace(attrs["content"]); content != "" {
			return content
		}
	}
	return ""
}

func firstHTMLTitle(body string) string {
	re := regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	match := re.FindStringSubmatch(body)
	if len(match) <= 1 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(match[1]))
}

func spotifyArtistFromTitle(pageTitle, trackTitle string) string {
	pageTitle = strings.TrimSpace(pageTitle)
	trackTitle = strings.TrimSpace(trackTitle)
	if pageTitle == "" || trackTitle == "" {
		return ""
	}
	prefix := trackTitle + " - song and lyrics by "
	if !strings.HasPrefix(pageTitle, prefix) {
		return ""
	}
	artist := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(pageTitle, prefix), " | Spotify"))
	return artist
}
