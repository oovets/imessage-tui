package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type LinkPreview struct {
	Title       string
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
			return p, nil
		}
	}

	if p, err := c.fetchPreviewFromOEmbed(rawURL); err == nil {
		return p, nil
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

func (c *Client) fetchPreviewFromOEmbed(rawURL string) (LinkPreview, error) {
	endpoint := strings.TrimSpace(c.oembedEndpoint)
	if endpoint == "" {
		endpoint = "https://noembed.com/embed"
	}

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
		Description: get("description", "og_description"),
		SiteName:    get("site_name", "provider_name", "author_name", "og_site_name"),
		ImageURL:    get("image", "image_url", "thumbnail_url", "og_image"),
	}
}
