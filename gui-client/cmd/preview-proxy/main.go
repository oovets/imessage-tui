package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type previewResponse struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

type cacheEntry struct {
	resp      previewResponse
	expiresAt time.Time
}

type serverConfig struct {
	addr           string
	oembedEndpoint string
	timeout        time.Duration
	cacheTTL       time.Duration
}

type previewServer struct {
	cfg        serverConfig
	httpClient *http.Client

	cacheMu sync.RWMutex
	cache   map[string]cacheEntry
}

var (
	titleTagPattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaTagPattern  = regexp.MustCompile(`(?is)<meta[^>]+>`)
	metaAttrPattern = regexp.MustCompile(`(?i)([a-zA-Z_:][a-zA-Z0-9_:\-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	imageURLPattern = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|bmp)(\?.*)?$`)
	youtubeIDExpr   = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

func main() {
	cfg := serverConfig{
		addr:           envOrDefault("PREVIEW_PROXY_ADDR", "127.0.0.1:8090"),
		oembedEndpoint: envOrDefault("PREVIEW_OEMBED_ENDPOINT", "https://noembed.com/embed"),
		timeout:        time.Duration(envIntOrDefault("PREVIEW_TIMEOUT_SEC", 8)) * time.Second,
		cacheTTL:       time.Duration(envIntOrDefault("PREVIEW_CACHE_TTL_SEC", 6*3600)) * time.Second,
	}

	s := &previewServer{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.timeout,
		},
		cache: make(map[string]cacheEntry),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/preview", s.handlePreview)

	log.Printf("preview-proxy listening on %s", cfg.addr)
	log.Printf("oEmbed endpoint: %s", cfg.oembedEndpoint)
	if err := http.ListenAndServe(cfg.addr, mux); err != nil {
		log.Fatalf("preview-proxy failed: %v", err)
	}
}

func (s *previewServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *previewServer) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if rawURL == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}

	normalized, err := normalizeHTTPURL(rawURL)
	if err != nil {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	if cached, ok := s.getCached(normalized); ok {
		writeJSON(w, cached)
		return
	}

	preview := s.fetchPreview(normalized)
	s.setCached(normalized, preview)
	writeJSON(w, preview)
}

func (s *previewServer) fetchPreview(rawURL string) previewResponse {
	u, err := url.Parse(rawURL)
	if err != nil {
		return previewResponse{URL: rawURL}
	}

	resp := previewResponse{URL: rawURL, SiteName: u.Hostname()}

	if imageURLPattern.MatchString(strings.ToLower(u.Path + "?" + u.RawQuery)) {
		resp.Title = imageTitleFromURL(u)
		resp.ImageURL = rawURL
		return resp
	}

	if yt := youtubeThumbnailURL(u); yt != "" {
		resp.ImageURL = yt
		if resp.SiteName == "" {
			resp.SiteName = "YouTube"
		}
	}

	if oembed := s.fetchOEmbed(rawURL); oembed != nil {
		mergePreview(&resp, *oembed)
	}

	if htmlMeta := s.fetchHTMLMetadata(rawURL); htmlMeta != nil {
		mergePreview(&resp, *htmlMeta)
	}

	if resp.Title == "" {
		resp.Title = u.Hostname()
	}
	if resp.SiteName == "" {
		resp.SiteName = u.Hostname()
	}

	return resp
}

func (s *previewServer) fetchOEmbed(rawURL string) *previewResponse {
	endpoint := strings.TrimSpace(s.cfg.oembedEndpoint)
	if endpoint == "" {
		return nil
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil
	}
	q := u.Query()
	q.Set("url", rawURL)
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "BlueBubbles-Preview-Proxy/1.0")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024))
	if err != nil {
		return nil
	}

	decoded := decodeGenericPreviewJSON(body)
	if decoded.Title == "" && decoded.Description == "" && decoded.SiteName == "" && decoded.ImageURL == "" {
		return nil
	}
	decoded.URL = rawURL
	return &decoded
}

func (s *previewServer) fetchHTMLMetadata(rawURL string) *previewResponse {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (BlueBubbles-Preview-Proxy)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil
	}

	contentType := strings.ToLower(strings.TrimSpace(res.Header.Get("Content-Type")))
	if contentType != "" && !strings.Contains(contentType, "text/html") {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024))
	if err != nil {
		return nil
	}
	doc := string(body)
	meta := parseMetaTags(doc)
	if meta.Title == "" {
		meta.Title = parseTitleTag(doc)
	}
	if meta.Title == "" && meta.Description == "" && meta.SiteName == "" && meta.ImageURL == "" {
		return nil
	}
	meta.URL = rawURL
	return &meta
}

func (s *previewServer) getCached(rawURL string) (previewResponse, bool) {
	s.cacheMu.RLock()
	entry, ok := s.cache[rawURL]
	s.cacheMu.RUnlock()
	if !ok {
		return previewResponse{}, false
	}
	if time.Now().After(entry.expiresAt) {
		s.cacheMu.Lock()
		delete(s.cache, rawURL)
		s.cacheMu.Unlock()
		return previewResponse{}, false
	}
	return entry.resp, true
}

func (s *previewServer) setCached(rawURL string, preview previewResponse) {
	s.cacheMu.Lock()
	s.cache[rawURL] = cacheEntry{resp: preview, expiresAt: time.Now().Add(s.cfg.cacheTTL)}
	s.cacheMu.Unlock()
}

func parseTitleTag(doc string) string {
	m := titleTagPattern.FindStringSubmatch(doc)
	if len(m) < 2 {
		return ""
	}
	return collapseWhitespace(html.UnescapeString(stripHTML(m[1])))
}

func parseMetaTags(doc string) previewResponse {
	out := previewResponse{}
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
		case out.SiteName == "" && (prop == "og:site_name" || name == "application-name"):
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
	if !youtubeIDExpr.MatchString(id) {
		return ""
	}
	return "https://img.youtube.com/vi/" + id + "/hqdefault.jpg"
}

func decodeGenericPreviewJSON(body []byte) previewResponse {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return previewResponse{}
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

	return previewResponse{
		Title:       get("title", "og_title"),
		Description: get("description", "og_description"),
		SiteName:    get("site_name", "provider_name", "author_name", "og_site_name"),
		ImageURL:    get("image", "image_url", "thumbnail_url", "og_image"),
	}
}

func mergePreview(dst *previewResponse, src previewResponse) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
	if dst.SiteName == "" {
		dst.SiteName = src.SiteName
	}
	if dst.ImageURL == "" {
		dst.ImageURL = src.ImageURL
	}
}

func imageTitleFromURL(u *url.URL) string {
	name := path.Base(u.Path)
	if strings.TrimSpace(name) == "" || name == "/" || name == "." {
		return "Image"
	}
	return name
}

func normalizeHTTPURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" {
		raw = "https://" + raw
		u, err = url.Parse(raw)
		if err != nil {
			return "", err
		}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported scheme")
	}
	if strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("missing host")
	}
	return u.String(), nil
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func envOrDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func envIntOrDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if parsed <= 0 {
		return fallback
	}
	return parsed
}
